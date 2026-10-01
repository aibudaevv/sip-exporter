package exporter

import "golang.org/x/sys/unix"

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
