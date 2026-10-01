package exporter

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

const (
	packetRingBlockSize       = 1 << 20
	packetRingBlockCount      = 8
	packetRingFrameSize       = 1 << 11
	packetRingRetireTimeoutMS = 60
	maxPacketRingBytes        = 8 * miB
	tpacketV3BlockHeaderLen   = 48 // sizeof struct tpacket_block_desc in the Linux UAPI.
	tpacketV3HeaderLen        = (unix.SizeofTpacket3Hdr+unix.TPACKET_ALIGNMENT-1) &
		^(unix.TPACKET_ALIGNMENT-1) + unix.SizeofSockaddrLinklayer
)

type packetRingGeometry struct {
	blockSize, blockCount, frameSize, retireTimeoutMS uint32
}

type packetRing struct {
	fd               int
	memory           []byte
	fdOpen           bool
	rxRingConfigured bool
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
