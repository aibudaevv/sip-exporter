package exporter

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	packetRingBlockSize       = 1 << 20
	packetRingBlockCount      = 8
	packetRingFrameSize       = 1 << 11
	packetRingRetireTimeoutMS = 60
	maxPacketRingBytes        = 8 * miB
	tpacketV3BlockHeaderLen   = 48 // sizeof struct tpacket_block_desc in the Linux UAPI.
	tpacketV3BlockStatusOff   = 8
	tpacketV3FrameAlignment   = 8 // V3_ALIGNMENT in Linux net/packet/af_packet.c.
	tpacketV3PacketTypeOff    = unix.SizeofTpacket3Hdr + 10
	tpacketV3HeaderLen        = (unix.SizeofTpacket3Hdr+unix.TPACKET_ALIGNMENT-1) &
		^(unix.TPACKET_ALIGNMENT-1) + unix.SizeofSockaddrLinklayer
)

type packetRingGeometry struct {
	blockSize, blockCount, frameSize, retireTimeoutMS uint32
}

type packetRing struct {
	fd               int
	blockSize        uint32
	blockCount       uint32
	memory           []byte
	fdOpen           bool
	rxRingConfigured bool
}

type packetRingBlock struct {
	data              []byte
	packetCount       uint32
	firstPacketOffset uint32
}

type packetRingFrame struct {
	data    []byte
	ts      time.Time
	pkttype uint8
}

type packetRingFrameIterator struct {
	block         packetRingBlock
	index, offset uint32
}

type packetRingSetupOps struct {
	setVersion func(int, int, int, int) error
	setRXRing  func(int, int, int, *unix.TpacketReq3) error
	mmap       func(int, int64, int, int, int) ([]byte, error)
}

func linuxPacketRingSetupOps() packetRingSetupOps {
	return packetRingSetupOps{
		setVersion: unix.SetsockoptInt,
		setRXRing:  unix.SetsockoptTpacketReq3,
		mmap:       unix.Mmap,
	}
}

func defaultPacketRingGeometry() packetRingGeometry {
	return packetRingGeometry{
		packetRingBlockSize, packetRingBlockCount, packetRingFrameSize, packetRingRetireTimeoutMS,
	}
}

func (g packetRingGeometry) request(pageSize uint32) (unix.TpacketReq3, error) {
	if pageSize == 0 || g.blockSize == 0 || g.blockSize%pageSize != 0 {
		return unix.TpacketReq3{}, unix.EINVAL
	}
	if g.frameSize <= tpacketV3HeaderLen || g.frameSize%unix.TPACKET_ALIGNMENT != 0 {
		return unix.TpacketReq3{}, unix.EINVAL
	}
	if g.blockSize%g.frameSize != 0 || g.blockCount == 0 {
		return unix.TpacketReq3{}, unix.EINVAL
	}
	if g.retireTimeoutMS == 0 ||
		uint64(g.blockSize) < uint64(readBufSize+tpacketV3BlockHeaderLen+tpacketV3HeaderLen) {
		return unix.TpacketReq3{}, unix.EINVAL
	}
	totalBytes := uint64(g.blockSize) * uint64(g.blockCount)
	frameCount := uint64(g.blockSize/g.frameSize) * uint64(g.blockCount)
	if totalBytes > maxPacketRingBytes {
		return unix.TpacketReq3{}, unix.EINVAL
	}
	return unix.TpacketReq3{
		Block_size: g.blockSize, Block_nr: g.blockCount, Frame_size: g.frameSize,
		Frame_nr: uint32(frameCount), Retire_blk_tov: g.retireTimeoutMS,
	}, nil
}

func setupPacketRing(
	fd int, geometry packetRingGeometry, ops packetRingSetupOps,
) (*packetRing, error) {
	ring := &packetRing{fd: fd, fdOpen: true}
	req, err := geometry.request(uint32(unix.Getpagesize()))
	if err != nil {
		return rollbackPacketRingSetup(ring, fmt.Errorf("build packet ring request: %w", err))
	}
	ring.blockSize, ring.blockCount = req.Block_size, req.Block_nr
	if err = ops.setVersion(fd, unix.SOL_PACKET, unix.PACKET_VERSION, unix.TPACKET_V3); err != nil {
		return rollbackPacketRingSetup(ring, fmt.Errorf("set PACKET_VERSION: %w", err))
	}
	if err = ops.setRXRing(fd, unix.SOL_PACKET, unix.PACKET_RX_RING, &req); err != nil {
		return rollbackPacketRingSetup(ring, fmt.Errorf("configure PACKET_RX_RING: %w", err))
	}
	ring.rxRingConfigured = true
	ring.memory, err = ops.mmap(fd, 0, int(req.Block_size)*int(req.Block_nr),
		unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return rollbackPacketRingSetup(ring, fmt.Errorf("mmap PACKET_RX_RING: %w", err))
	}
	return ring, nil
}

func (r *packetRing) decodeBlock(index uint32) (packetRingBlock, bool, error) {
	if r.blockSize < tpacketV3BlockHeaderLen || r.blockCount == 0 {
		return packetRingBlock{}, false, fmt.Errorf("invalid packet ring geometry: %w", unix.EINVAL)
	}
	physicalIndex := index % r.blockCount
	start := uint64(physicalIndex) * uint64(r.blockSize)
	end := start + uint64(r.blockSize)
	if end > uint64(len(r.memory)) {
		return packetRingBlock{}, false, fmt.Errorf("truncated packet ring block: %w", unix.EINVAL)
	}
	block := r.memory[int(start):int(end)]
	status := atomic.LoadUint32((*uint32)(unsafe.Pointer(&block[tpacketV3BlockStatusOff])))
	if status&unix.TP_STATUS_USER == 0 {
		return packetRingBlock{}, false, nil
	}
	packetCount := binary.NativeEndian.Uint32(block[12:16])
	firstPacketOffset := binary.NativeEndian.Uint32(block[16:20])
	if packetCount == 0 {
		return packetRingBlock{data: block}, true, nil
	}
	if firstPacketOffset < tpacketV3BlockHeaderLen ||
		uint64(firstPacketOffset)+uint64(tpacketV3HeaderLen) > uint64(len(block)) {
		return packetRingBlock{}, false, fmt.Errorf("invalid first packet offset: %w", unix.EINVAL)
	}
	capacity := (uint32(len(block)) - firstPacketOffset) / uint32(tpacketV3HeaderLen)
	if packetCount > capacity {
		return packetRingBlock{}, false, fmt.Errorf("invalid packet count: %w", unix.EINVAL)
	}
	return packetRingBlock{
		data: block, packetCount: packetCount, firstPacketOffset: firstPacketOffset,
	}, true, nil
}

func (b packetRingBlock) frames() packetRingFrameIterator {
	return packetRingFrameIterator{block: b, offset: b.firstPacketOffset}
}

func (i *packetRingFrameIterator) next() (packetRingFrame, bool, error) {
	if i.index >= i.block.packetCount {
		return packetRingFrame{}, false, nil
	}
	start := uint64(i.offset)
	headerEnd := start + uint64(tpacketV3HeaderLen)
	if headerEnd > uint64(len(i.block.data)) {
		return i.invalid("truncated packet frame metadata")
	}
	header := i.block.data[int(start):int(headerEnd)]
	nextOffset := binary.NativeEndian.Uint32(header[0:4])
	snaplen := binary.NativeEndian.Uint32(header[12:16])
	macOffset := binary.NativeEndian.Uint16(header[24:26])
	payloadStart := start + uint64(macOffset)
	payloadEnd := payloadStart + uint64(snaplen)
	if macOffset < uint16(tpacketV3HeaderLen) {
		return i.invalid("invalid packet data offset")
	}
	if payloadEnd > uint64(len(i.block.data)) {
		return i.invalid("truncated packet frame data")
	}
	if i.index+1 < i.block.packetCount {
		nextStart := start + uint64(nextOffset)
		if nextOffset%tpacketV3FrameAlignment != 0 ||
			nextStart < payloadEnd || nextStart+uint64(tpacketV3HeaderLen) > uint64(len(i.block.data)) {
			return i.invalid("invalid next packet offset")
		}
		i.offset += nextOffset
	}
	i.index++
	return packetRingFrame{
		data: i.block.data[int(payloadStart):int(payloadEnd)],
		ts: time.Unix(int64(binary.NativeEndian.Uint32(header[4:8])),
			int64(binary.NativeEndian.Uint32(header[8:12]))),
		pkttype: header[tpacketV3PacketTypeOff],
	}, true, nil
}

func (i *packetRingFrameIterator) invalid(reason string) (packetRingFrame, bool, error) {
	i.index = i.block.packetCount
	return packetRingFrame{}, false, fmt.Errorf("%s: %w", reason, unix.EINVAL)
}

func rollbackPacketRingSetup(ring *packetRing, setupErr error) (*packetRing, error) {
	return nil, errors.Join(setupErr, ring.close())
}

func (r *packetRing) close() error {
	var closeErr error
	if len(r.memory) != 0 {
		if err := unix.Munmap(r.memory); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("unmap packet ring: %w", err))
		}
		r.memory = nil
	}
	if r.fdOpen && r.rxRingConfigured {
		if err := unix.SetsockoptTpacketReq3(
			r.fd, unix.SOL_PACKET, unix.PACKET_RX_RING, &unix.TpacketReq3{},
		); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("disable packet RX ring: %w", err))
		}
	}
	r.rxRingConfigured = false
	if r.fdOpen {
		if err := unix.Close(r.fd); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close packet socket: %w", err))
		}
		r.fdOpen = false
	}
	return closeErr
}
