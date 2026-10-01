package exporter

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
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

type packetRingBlockOwnership struct {
	references atomic.Int64
	release    func()
}

type packetRingBlockReference struct {
	ownership *packetRingBlockOwnership
	released  atomic.Bool
}

type packetBatchQueueAccounting struct {
	maxPackets    int64
	queuedPackets atomic.Int64
}

type packetRingFrameRun struct {
	frames    []packetRingFrame
	iface     string
	reference *packetRingBlockReference
	queue     *packetBatchQueue
}

type packetBatchQueue struct {
	runs       chan *packetRingFrameRun
	accounting *packetBatchQueueAccounting
	rtpDropped func()

	admissionMu      sync.RWMutex
	admissionChanged chan struct{}
	pendingSIP       uint32
	nextSIP          uint64
	servingSIP       uint64
	stopped          bool
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

func waitPacketRing(fd int) error {
	if fd < 0 {
		return unix.EBADF
	}
	pollFDs := [1]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(pollFDs[:], int(socketRcvTimeo/time.Millisecond))
	if err != nil {
		return err
	}
	if n == 0 {
		return unix.EAGAIN
	}
	revents := pollFDs[0].Revents
	var socketErr int
	if revents&(unix.POLLERR|unix.POLLHUP) != 0 {
		socketErr, err = unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
		if err != nil {
			return err
		}
	}
	return packetRingPollResult(revents, socketErr)
}

func packetRingPollResult(revents int16, socketErr int) error {
	if revents&unix.POLLNVAL != 0 {
		return unix.EBADF
	}
	if socketErr != 0 {
		return syscall.Errno(socketErr)
	}
	if revents&(unix.POLLERR|unix.POLLHUP) != 0 {
		return unix.ENODEV
	}
	return nil
}

func (b packetRingBlock) frames() packetRingFrameIterator {
	return packetRingFrameIterator{block: b, offset: b.firstPacketOffset}
}

func (b packetRingBlock) release() {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&b.data[tpacketV3BlockStatusOff])), unix.TP_STATUS_KERNEL)
}

func newPacketRingBlockReference(release func()) *packetRingBlockReference {
	ownership := &packetRingBlockOwnership{release: release}
	ownership.references.Store(1)
	return &packetRingBlockReference{ownership: ownership}
}

func (r *packetRingBlockReference) retain() *packetRingBlockReference {
	if r.released.Load() {
		return nil
	}
	for {
		references := r.ownership.references.Load()
		if references == 0 {
			return nil
		}
		if !r.ownership.references.CompareAndSwap(references, references+1) {
			continue
		}
		if r.released.Load() {
			r.ownership.releaseReference()
			return nil
		}
		return &packetRingBlockReference{ownership: r.ownership}
	}
}

func (r *packetRingBlockReference) release() {
	if !r.released.CompareAndSwap(false, true) {
		return
	}
	r.ownership.releaseReference()
}

func (o *packetRingBlockOwnership) releaseReference() {
	if o.references.Add(-1) == 0 {
		o.release()
	}
}

func newPacketBatchQueueAccounting(capacity uint32) *packetBatchQueueAccounting {
	return &packetBatchQueueAccounting{maxPackets: int64(capacity)}
}

func (a *packetBatchQueueAccounting) reserve(requested uint32) uint32 {
	for requested != 0 {
		queued := a.queuedPackets.Load()
		available := a.maxPackets - queued
		if available <= 0 {
			break
		}
		accepted := min(int64(requested), available)
		if a.queuedPackets.CompareAndSwap(queued, queued+accepted) {
			return uint32(accepted)
		}
	}
	return 0
}

func (a *packetBatchQueueAccounting) reserveAll(requested uint32) bool {
	for {
		queued := a.queuedPackets.Load()
		if int64(requested) > a.maxPackets-queued {
			return false
		}
		if a.queuedPackets.CompareAndSwap(queued, queued+int64(requested)) {
			return true
		}
	}
}

func (a *packetBatchQueueAccounting) release(packetCount uint32) {
	a.queuedPackets.Add(-int64(packetCount))
}

func (a *packetBatchQueueAccounting) length() int {
	return int(a.queuedPackets.Load())
}

func (a *packetBatchQueueAccounting) capacity() int {
	return int(a.maxPackets)
}

func newPacketBatchQueue(capacity uint32, rtpDropped func()) *packetBatchQueue {
	return &packetBatchQueue{
		runs: make(chan *packetRingFrameRun, capacity), accounting: newPacketBatchQueueAccounting(capacity),
		rtpDropped: rtpDropped, admissionChanged: make(chan struct{}),
	}
}

func (q *packetBatchQueue) enqueueBlock(
	block packetRingBlock, iface string, ports []uint16,
) (bool, error) {
	reference := newPacketRingBlockReference(block.release)
	defer reference.release()
	frames := make([]packetRingFrame, 0, block.packetCount)
	iterator := block.frames()
	runStart := 0
	runSIP := false
	for {
		frame, ok, err := iterator.next()
		if err != nil {
			if runStart < len(frames) && !q.enqueueRun(reference, iface, frames[runStart:], runSIP) {
				return false, nil
			}
			return true, err
		}
		if !ok {
			break
		}
		isSIP := isSIPPacket(frame.data, ports)
		if len(frames) == 0 {
			runSIP = isSIP
		} else if isSIP != runSIP {
			if !q.enqueueRun(reference, iface, frames[runStart:], runSIP) {
				return false, nil
			}
			runStart = len(frames)
			runSIP = isSIP
		}
		frames = append(frames, frame)
	}
	if runStart < len(frames) && !q.enqueueRun(reference, iface, frames[runStart:], runSIP) {
		return false, nil
	}
	return true, nil
}

func (q *packetBatchQueue) enqueueRun(
	reference *packetRingBlockReference, iface string, frames []packetRingFrame, sip bool,
) bool {
	if sip && q.accounting.capacity() > 0 && len(frames) > q.accounting.capacity() {
		capacity := q.accounting.capacity()
		for start := 0; start < len(frames); start += capacity {
			end := min(start+capacity, len(frames))
			if !q.enqueueRun(reference, iface, frames[start:end:end], true) {
				return false
			}
		}
		return true
	}
	packetCount := uint32(len(frames))
	if sip {
		return q.enqueueSIPRun(reference, iface, frames)
	}
	return q.enqueueRTPRun(reference, iface, frames, packetCount)
}

func (q *packetBatchQueue) enqueueSIPRun(
	reference *packetRingBlockReference, iface string, frames []packetRingFrame,
) bool {
	packetCount := uint32(len(frames))
	q.admissionMu.Lock()
	if q.stopped {
		q.admissionMu.Unlock()
		return false
	}
	ticket := q.nextSIP
	q.nextSIP++
	q.pendingSIP++
	for {
		if q.stopped {
			q.pendingSIP--
			q.admissionMu.Unlock()
			return false
		}
		if ticket == q.servingSIP && q.accounting.reserveAll(packetCount) {
			borrowed := reference.retain()
			q.runs <- &packetRingFrameRun{
				frames: frames, iface: iface, reference: borrowed, queue: q,
			}
			q.finishSIPLocked()
			q.admissionMu.Unlock()
			return true
		}
		changed := q.admissionChanged
		q.admissionMu.Unlock()
		<-changed
		q.admissionMu.Lock()
	}
}

func (q *packetBatchQueue) finishSIPLocked() {
	q.pendingSIP--
	q.servingSIP++
	q.signalAdmissionLocked()
}

func (q *packetBatchQueue) enqueueRTPRun(
	reference *packetRingBlockReference, iface string, frames []packetRingFrame, packetCount uint32,
) bool {
	q.admissionMu.RLock()
	if q.stopped {
		q.admissionMu.RUnlock()
		return false
	}
	accepted := uint32(0)
	if q.pendingSIP == 0 {
		accepted = q.accounting.reserve(packetCount)
	}
	for dropped := accepted; dropped < packetCount; dropped++ {
		q.rtpDropped()
	}
	if accepted != 0 {
		borrowed := reference.retain()
		q.runs <- &packetRingFrameRun{
			frames: frames[:accepted:accepted], iface: iface, reference: borrowed, queue: q,
		}
	}
	q.admissionMu.RUnlock()
	return true
}

func (q *packetBatchQueue) release(packetCount uint32) {
	q.admissionMu.Lock()
	q.accounting.release(packetCount)
	q.signalAdmissionLocked()
	q.admissionMu.Unlock()
}

func (q *packetBatchQueue) signalAdmissionLocked() {
	close(q.admissionChanged)
	q.admissionChanged = make(chan struct{})
}

func (q *packetBatchQueue) stop() {
	q.admissionMu.Lock()
	if !q.stopped {
		q.stopped = true
		q.signalAdmissionLocked()
	}
	q.admissionMu.Unlock()
}

func (r *packetRingFrameRun) release() {
	r.queue.release(uint32(len(r.frames)))
	r.reference.release()
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
