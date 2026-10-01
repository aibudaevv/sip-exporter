package exporter

import (
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestDefaultPacketRingGeometryRequest(t *testing.T) {
	req, err := defaultPacketRingGeometry().request(4096)
	require.NoError(t, err)
	require.Equal(t, unix.TpacketReq3{
		Block_size: 1 << 20, Block_nr: 8, Frame_size: 1 << 11, Frame_nr: 4096,
		Retire_blk_tov: 60,
	}, req)
}

func TestPacketRingGeometryValidation(t *testing.T) {
	tests := []struct {
		name     string
		geometry packetRingGeometry
		pageSize uint32
		wantErr  bool
	}{
		{name: "valid", geometry: packetRingGeometry{1 << 17, 8, 1 << 11, 60}, pageSize: 4096},
		{
			name:     "zero page size",
			geometry: packetRingGeometry{1 << 17, 8, 1 << 11, 60},
			wantErr:  true,
		},
		{
			name:     "zero block size",
			geometry: packetRingGeometry{0, 8, 1 << 11, 60},
			pageSize: 4096,
			wantErr:  true,
		},
		{
			name: "block not page aligned", geometry: packetRingGeometry{(1 << 17) + 2048, 1, 1 << 11, 60},
			pageSize: 4096, wantErr: true,
		},
		{
			name:     "zero frame size",
			geometry: packetRingGeometry{1 << 17, 8, 0, 60},
			pageSize: 4096,
			wantErr:  true,
		},
		{
			name:     "frame not tpacket aligned",
			geometry: packetRingGeometry{512000, 1, 1000, 60},
			pageSize: 4096,
			wantErr:  true,
		},
		{
			name:     "frame below header",
			geometry: packetRingGeometry{1 << 17, 8, 16, 60},
			pageSize: 4096,
			wantErr:  true,
		},
		{
			name: "frame larger than block", geometry: packetRingGeometry{1 << 17, 1, 1 << 18, 60},
			pageSize: 4096, wantErr: true,
		},
		{
			name: "frame does not divide block", geometry: packetRingGeometry{1 << 17, 1, 3072, 60},
			pageSize: 4096, wantErr: true,
		},
		{
			name:     "zero block count",
			geometry: packetRingGeometry{1 << 17, 0, 1 << 11, 60},
			pageSize: 4096,
			wantErr:  true,
		},
		{
			name:     "zero retire timeout",
			geometry: packetRingGeometry{1 << 17, 8, 1 << 11, 0},
			pageSize: 4096,
			wantErr:  true,
		},
		{
			name: "full SIP snapshot does not fit", geometry: packetRingGeometry{1 << 16, 1, 1 << 11, 60},
			pageSize: 4096, wantErr: true,
		},
		{
			name: "reservation exceeds budget", geometry: packetRingGeometry{1 << 20, 9, 1 << 11, 60},
			pageSize: 4096, wantErr: true,
		},
		{
			name:     "reservation multiplication cannot wrap",
			geometry: packetRingGeometry{1 << 17, math.MaxUint32, 1 << 11, 60},
			pageSize: 4096, wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.geometry.request(tt.pageSize)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestDefaultPacketRingGeometryMemoryBudget(t *testing.T) {
	req, err := defaultPacketRingGeometry().request(4096)
	require.NoError(t, err)

	perInterface := uint64(req.Block_size) * uint64(req.Block_nr)
	require.Equal(t, uint64(8*miB), perInterface)
	require.LessOrEqual(t, perInterface, uint64(128*miB))
	require.Equal(t, uint64(24*miB), perInterface*3)
	require.LessOrEqual(t, perInterface*3, uint64(256*miB))
}

func TestSetupPacketRingPartialFailureClosesFD(t *testing.T) {
	injectedErr := errors.New("injected setup failure")
	tests := []struct {
		name      string
		geometry  packetRingGeometry
		failAt    string
		wantCalls []string
		wantErr   error
		wantText  string
	}{
		{
			name: "invalid geometry", geometry: packetRingGeometry{},
			wantErr: unix.EINVAL, wantText: "build packet ring request",
		},
		{
			name: "packet version", geometry: defaultPacketRingGeometry(), failAt: "version",
			wantCalls: []string{"version"}, wantErr: injectedErr, wantText: "set PACKET_VERSION",
		},
		{
			name: "RX ring", geometry: defaultPacketRingGeometry(), failAt: "ring",
			wantCalls: []string{"version", "ring"}, wantErr: injectedErr, wantText: "configure PACKET_RX_RING",
		},
		{
			name: "mmap", geometry: defaultPacketRingGeometry(), failAt: "mmap",
			wantCalls: []string{"version", "ring", "mmap"}, wantErr: injectedErr,
			wantText: "mmap PACKET_RX_RING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
			require.NoError(t, err)
			t.Cleanup(func() { _ = unix.Close(fds[1]) })
			var calls []string
			ops := packetRingSetupOps{
				setVersion: func(fd, level, option, version int) error {
					calls = append(calls, "version")
					require.Equal(t, fds[0], fd)
					require.Equal(t, unix.SOL_PACKET, level)
					require.Equal(t, unix.PACKET_VERSION, option)
					require.Equal(t, unix.TPACKET_V3, version)
					if tt.failAt == "version" {
						return injectedErr
					}
					return nil
				},
				setRXRing: func(fd, level, option int, req *unix.TpacketReq3) error {
					calls = append(calls, "ring")
					require.Equal(t, fds[0], fd)
					require.Equal(t, unix.SOL_PACKET, level)
					require.Equal(t, unix.PACKET_RX_RING, option)
					require.Equal(t, &unix.TpacketReq3{
						Block_size: 1 << 20, Block_nr: 8, Frame_size: 1 << 11,
						Frame_nr: 4096, Retire_blk_tov: 60,
					}, req)
					if tt.failAt == "ring" {
						return injectedErr
					}
					return nil
				},
				mmap: func(fd int, offset int64, length, prot, flags int) ([]byte, error) {
					calls = append(calls, "mmap")
					require.Equal(t, fds[0], fd)
					require.Zero(t, offset)
					require.Equal(t, 8*miB, length)
					require.Equal(t, unix.PROT_READ|unix.PROT_WRITE, prot)
					require.Equal(t, unix.MAP_SHARED, flags)
					return nil, injectedErr
				},
			}

			ring, setupErr := setupPacketRing(fds[0], tt.geometry, ops)
			require.Nil(t, ring)
			require.ErrorIs(t, setupErr, tt.wantErr)
			require.ErrorContains(t, setupErr, tt.wantText)
			if tt.failAt == "mmap" {
				require.ErrorContains(t, setupErr, "disable packet RX ring")
			}
			require.Equal(t, tt.wantCalls, calls)
			_, err = unix.FcntlInt(uintptr(fds[0]), unix.F_GETFD, 0)
			require.ErrorIs(t, err, unix.EBADF)
		})
	}
}

func TestSetupPacketRingSuccess(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = unix.Close(fds[1]) })
	calls := make([]string, 0, 3)
	ops := packetRingSetupOps{
		setVersion: func(int, int, int, int) error {
			calls = append(calls, "version")
			return nil
		},
		setRXRing: func(int, int, int, *unix.TpacketReq3) error {
			calls = append(calls, "ring")
			return nil
		},
		mmap: func(_ int, _ int64, length int, _, _ int) ([]byte, error) {
			calls = append(calls, "mmap")
			return unix.Mmap(-1, 0, length, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
		},
	}

	ring, err := setupPacketRing(fds[0], defaultPacketRingGeometry(), ops)
	require.NoError(t, err)
	t.Cleanup(func() {
		ring.rxRingConfigured = false
		_ = ring.close()
	})
	require.Equal(t, []string{"version", "ring", "mmap"}, calls)
	require.True(t, ring.fdOpen)
	require.True(t, ring.rxRingConfigured)
	require.Len(t, ring.memory, 8*miB)
	require.Equal(t, uint32(1<<20), ring.blockSize)
	require.Equal(t, uint32(8), ring.blockCount)
}

func TestPacketRingCloseZeroValue(t *testing.T) {
	var ring packetRing

	require.NoError(t, ring.close())
	require.NoError(t, ring.close())
}

func TestPacketRingCloseUnmapsMemory(t *testing.T) {
	memory, err := unix.Mmap(-1, 0, unix.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANON|unix.MAP_PRIVATE)
	require.NoError(t, err)
	ring := packetRing{memory: memory}

	require.NoError(t, ring.close())
	require.Nil(t, ring.memory)
	require.ErrorIs(t, unix.Mprotect(memory, unix.PROT_NONE), unix.ENOMEM)
}

func TestPacketRingCloseClosesFD(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = unix.Close(fds[1]) })
	ring := packetRing{fd: fds[0], fdOpen: true}

	require.NoError(t, ring.close())
	_, err = unix.FcntlInt(uintptr(fds[0]), unix.F_GETFD, 0)
	require.ErrorIs(t, err, unix.EBADF)
	require.NoError(t, ring.close())
}

func TestPacketRingCloseContinuesAfterRingDisableError(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = unix.Close(fds[1]) })
	ring := packetRing{fd: fds[0], fdOpen: true, rxRingConfigured: true}

	require.ErrorContains(t, ring.close(), "disable packet RX ring")
	_, err = unix.FcntlInt(uintptr(fds[0]), unix.F_GETFD, 0)
	require.ErrorIs(t, err, unix.EBADF)
	require.NoError(t, ring.close())
}

func TestSetupPacketRingConfiguredRXRing(t *testing.T) {
	if syscall.Geteuid() != 0 {
		t.Skip("requires root privileges for AF_PACKET")
	}

	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(ethPAll)))
	require.NoError(t, err)
	fdOpen := true
	t.Cleanup(func() {
		if fdOpen {
			_ = unix.Close(fd)
		}
	})
	fdOpen = false
	ring, err := setupPacketRing(fd, defaultPacketRingGeometry(), linuxPacketRingSetupOps())
	require.NoError(t, err)
	t.Cleanup(func() { _ = ring.close() })
	version, err := unix.GetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_VERSION)
	require.NoError(t, err)
	require.Equal(t, unix.TPACKET_V3, version)
	require.Len(t, ring.memory, 8*miB)
	ring.memory[0] = 1

	require.NoError(t, ring.close())
	_, err = unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	require.ErrorIs(t, err, unix.EBADF)
	require.NoError(t, ring.close())
}

func TestPacketRingDecodeBlockStatusAndWrap(t *testing.T) {
	const blockSize = 128
	memory := make([]byte, 2*blockSize)
	binary.NativeEndian.PutUint32(memory[8:12], unix.TP_STATUS_USER|unix.TP_STATUS_COPY)
	binary.NativeEndian.PutUint32(memory[12:16], 1)
	binary.NativeEndian.PutUint32(memory[16:20], tpacketV3BlockHeaderLen)
	binary.NativeEndian.PutUint32(memory[blockSize+12:blockSize+16], math.MaxUint32)
	ring := packetRing{memory: memory, blockSize: blockSize, blockCount: 2}

	block, ready, err := ring.decodeBlock(0)
	require.NoError(t, err)
	require.True(t, ready)
	require.Len(t, block.data, blockSize)
	require.Equal(t, uint32(1), block.packetCount)
	require.Equal(t, uint32(tpacketV3BlockHeaderLen), block.firstPacketOffset)
	block.data[blockSize-1] = 42
	require.Equal(t, byte(42), ring.memory[blockSize-1])
	require.Equal(t, uint32(unix.TP_STATUS_USER|unix.TP_STATUS_COPY),
		binary.NativeEndian.Uint32(memory[8:12]))

	block, ready, err = ring.decodeBlock(1)
	require.NoError(t, err)
	require.False(t, ready)
	require.Zero(t, block)

	wrapped, ready, err := ring.decodeBlock(2)
	require.NoError(t, err)
	require.True(t, ready)
	require.Len(t, wrapped.data, blockSize)
	require.Equal(t, byte(42), wrapped.data[blockSize-1])
}

func TestPacketRingDecodeBlockBounds(t *testing.T) {
	tests := []struct {
		name            string
		memorySize      int
		blockSize       uint32
		blockCount      uint32
		packetCount     uint32
		firstOffset     uint32
		wantPacketCount uint32
		wantErr         bool
	}{
		{name: "zero packets", memorySize: 128, blockSize: 128, blockCount: 1},
		{name: "multiple packets", memorySize: 256, blockSize: 256, blockCount: 1,
			packetCount: 2, firstOffset: tpacketV3BlockHeaderLen, wantPacketCount: 2},
		{name: "block size shorter than header", memorySize: tpacketV3BlockHeaderLen - 1,
			blockSize: tpacketV3BlockHeaderLen - 1, blockCount: 1, wantErr: true},
		{name: "zero block count", memorySize: 128, blockSize: 128, wantErr: true},
		{name: "truncated block header", memorySize: tpacketV3BlockHeaderLen - 1,
			blockSize: 128, blockCount: 1, wantErr: true},
		{name: "first frame overlaps block header", memorySize: 128, blockSize: 128,
			blockCount: 1, packetCount: 1,
			firstOffset: tpacketV3BlockHeaderLen - 1, wantErr: true},
		{name: "first frame outside block", memorySize: 128, blockSize: 128,
			blockCount: 1, packetCount: 1,
			firstOffset: 128, wantErr: true},
		{name: "packet count exceeds selected block capacity", memorySize: 192, blockSize: 128,
			blockCount: 1, packetCount: 2,
			firstOffset: tpacketV3BlockHeaderLen, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory := make([]byte, tt.memorySize)
			if len(memory) >= tpacketV3BlockHeaderLen {
				binary.NativeEndian.PutUint32(memory[8:12], unix.TP_STATUS_USER)
				binary.NativeEndian.PutUint32(memory[12:16], tt.packetCount)
				binary.NativeEndian.PutUint32(memory[16:20], tt.firstOffset)
			}
			ring := packetRing{memory: memory, blockSize: tt.blockSize, blockCount: tt.blockCount}

			block, ready, err := ring.decodeBlock(0)
			if tt.wantErr {
				require.Error(t, err)
				require.False(t, ready)
				require.Zero(t, block)
				return
			}
			require.NoError(t, err)
			require.True(t, ready)
			require.Equal(t, tt.wantPacketCount, block.packetCount)
		})
	}
}

func TestPacketRingFrameIterator(t *testing.T) {
	memory := make([]byte, 320)
	binary.NativeEndian.PutUint32(memory[8:12], unix.TP_STATUS_USER)
	putPacketRingFrame(memory, 48, 88, time.Unix(100, 200), unix.PACKET_OUTGOING, []byte("one"))
	putPacketRingFrame(memory, 136, 0, time.Unix(300, 400), unix.PACKET_HOST, []byte("two"))
	iterator := (packetRingBlock{data: memory, packetCount: 2, firstPacketOffset: 48}).frames()

	first, ok, err := iterator.next()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("one"), first.data)
	require.Equal(t, time.Unix(100, 200), first.ts)
	require.Equal(t, uint8(unix.PACKET_OUTGOING), first.pkttype)
	first.data[0] = 'O'
	require.Equal(t, byte('O'), memory[128])

	second, ok, err := iterator.next()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("two"), second.data)
	require.Equal(t, time.Unix(300, 400), second.ts)
	require.Equal(t, uint8(unix.PACKET_HOST), second.pkttype)

	frame, ok, err := iterator.next()
	require.NoError(t, err)
	require.False(t, ok)
	require.Zero(t, frame)
	require.Equal(t, uint32(unix.TP_STATUS_USER), binary.NativeEndian.Uint32(memory[8:12]))
}

func TestPacketRingFrameIteratorBounds(t *testing.T) {
	tests := []struct {
		name        string
		memorySize  int
		packetCount uint32
		firstOffset uint32
		nextOffset  uint32
		macOffset   uint16
		snaplen     uint32
	}{
		{name: "truncated frame metadata", memorySize: 100, packetCount: 1, firstOffset: 48},
		{name: "packet data overlaps metadata", memorySize: 128, packetCount: 1,
			firstOffset: 48, macOffset: tpacketV3HeaderLen - 1},
		{name: "snap length outside block", memorySize: 128, packetCount: 1,
			firstOffset: 48, macOffset: tpacketV3HeaderLen, snaplen: 13},
		{name: "zero next offset before last frame", memorySize: 256, packetCount: 2,
			firstOffset: 48, macOffset: tpacketV3HeaderLen, snaplen: 1},
		{name: "next offset overlaps packet data", memorySize: 256, packetCount: 2,
			firstOffset: 48, nextOffset: 64, macOffset: tpacketV3HeaderLen, snaplen: 1},
		{name: "unaligned next offset", memorySize: 256, packetCount: 2,
			firstOffset: 48, nextOffset: 84, macOffset: tpacketV3HeaderLen, snaplen: 1},
		{name: "next frame metadata outside block", memorySize: 256, packetCount: 2,
			firstOffset: 48, nextOffset: 160, macOffset: tpacketV3HeaderLen, snaplen: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory := make([]byte, tt.memorySize)
			if int(tt.firstOffset)+unix.SizeofTpacket3Hdr <= len(memory) {
				header := memory[tt.firstOffset:]
				binary.NativeEndian.PutUint32(header[0:4], tt.nextOffset)
				binary.NativeEndian.PutUint32(header[12:16], tt.snaplen)
				binary.NativeEndian.PutUint16(header[24:26], tt.macOffset)
			}
			iterator := (packetRingBlock{
				data: memory, packetCount: tt.packetCount, firstPacketOffset: tt.firstOffset,
			}).frames()

			frame, ok, err := iterator.next()
			require.ErrorIs(t, err, unix.EINVAL)
			require.False(t, ok)
			require.Zero(t, frame)
			frame, ok, err = iterator.next()
			require.NoError(t, err)
			require.False(t, ok)
			require.Zero(t, frame)
		})
	}
}

func TestPacketRingFrameIteratorZeroPackets(t *testing.T) {
	iterator := (packetRingBlock{data: make([]byte, 48)}).frames()

	frame, ok, err := iterator.next()
	require.NoError(t, err)
	require.False(t, ok)
	require.Zero(t, frame)
}

func TestPacketRingFrameIteratorAllocations(t *testing.T) {
	memory := make([]byte, 160)
	putPacketRingFrame(memory, 48, 0, time.Unix(100, 200), unix.PACKET_HOST, []byte{42})
	block := packetRingBlock{data: memory, packetCount: 1, firstPacketOffset: 48}

	allocations := testing.AllocsPerRun(1000, func() {
		iterator := block.frames()
		frame, ok, err := iterator.next()
		if err != nil || !ok || len(frame.data) != 1 || frame.data[0] != 42 {
			panic("unexpected packet ring frame")
		}
	})
	require.Zero(t, allocations)
}

func TestPacketRingBlockRelease(t *testing.T) {
	memory := make([]byte, 128)
	binary.NativeEndian.PutUint32(memory[8:12], unix.TP_STATUS_USER|unix.TP_STATUS_COPY)
	block := packetRingBlock{data: memory}

	block.release()

	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(memory[8:12]))
}

func TestPacketRingBlockReferenceReleasesAfterLastOwner(t *testing.T) {
	memory := make([]byte, 128)
	binary.NativeEndian.PutUint32(memory[8:12], unix.TP_STATUS_USER)
	block := packetRingBlock{data: memory}
	var releaseCalls atomic.Int32
	root := newPacketRingBlockReference(func() {
		releaseCalls.Add(1)
		block.release()
	})
	first := root.retain()
	second := root.retain()
	require.NotNil(t, first)
	require.NotNil(t, second)

	root.release()
	require.Equal(t, uint32(unix.TP_STATUS_USER), binary.NativeEndian.Uint32(memory[8:12]))
	first.release()
	require.Equal(t, uint32(unix.TP_STATUS_USER), binary.NativeEndian.Uint32(memory[8:12]))
	second.release()

	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(memory[8:12]))
	require.Equal(t, int32(1), releaseCalls.Load())
}

func TestPacketRingBlockReferenceCannotBeReusedAfterRelease(t *testing.T) {
	var releaseCalls atomic.Int32
	root := newPacketRingBlockReference(func() { releaseCalls.Add(1) })
	borrowed := root.retain()
	require.NotNil(t, borrowed)

	root.release()
	root.release()
	require.Nil(t, root.retain())
	require.Zero(t, releaseCalls.Load())

	borrowed.release()
	borrowed.release()
	require.Nil(t, borrowed.retain())
	require.Equal(t, int32(1), releaseCalls.Load())
}

func TestPacketRingBlockReferenceConcurrentRelease(t *testing.T) {
	const borrowedCount = 32
	var releaseCalls atomic.Int32
	root := newPacketRingBlockReference(func() { releaseCalls.Add(1) })
	borrowed := make([]*packetRingBlockReference, borrowedCount)
	for i := range borrowed {
		borrowed[i] = root.retain()
		require.NotNil(t, borrowed[i])
	}
	root.release()

	var wg sync.WaitGroup
	wg.Add(len(borrowed))
	for _, reference := range borrowed {
		go func() {
			defer wg.Done()
			reference.release()
			reference.release()
		}()
	}
	wg.Wait()

	require.Equal(t, int32(1), releaseCalls.Load())
}

func TestPacketBatchQueueAccountingReservesAvailablePackets(t *testing.T) {
	accounting := newPacketBatchQueueAccounting(5)

	require.Zero(t, accounting.reserve(0))
	require.Equal(t, 0, accounting.length())
	require.Equal(t, 5, accounting.capacity())

	require.Equal(t, uint32(3), accounting.reserve(3))
	require.Equal(t, 3, accounting.length())

	require.Equal(t, uint32(2), accounting.reserve(3))
	require.Equal(t, 5, accounting.length())
	require.Zero(t, accounting.reserve(1))
}

func TestPacketBatchQueueAccountingCountsPacketsAcrossReservations(t *testing.T) {
	oneReservation := newPacketBatchQueueAccounting(5)
	multipleReservations := newPacketBatchQueueAccounting(5)

	require.Equal(t, uint32(5), oneReservation.reserve(5))
	require.Equal(t, uint32(2), multipleReservations.reserve(2))
	require.Equal(t, uint32(3), multipleReservations.reserve(3))
	require.Equal(t, 5, oneReservation.length())
	require.Equal(t, 5, multipleReservations.length())

	oneReservation.release(5)
	multipleReservations.release(2)
	require.Equal(t, 3, multipleReservations.length())
	multipleReservations.release(3)
	require.Zero(t, oneReservation.length())
	require.Zero(t, multipleReservations.length())
}

func TestPacketBatchQueueAccountingConcurrentReservation(t *testing.T) {
	const (
		capacity = 1000
		workers  = 64
		request  = 31
	)
	accounting := newPacketBatchQueueAccounting(capacity)
	accepted := make([]uint32, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func() {
			defer wg.Done()
			accepted[i] = accounting.reserve(request)
		}()
	}
	wg.Wait()

	var total uint32
	for _, packetCount := range accepted {
		total += packetCount
	}
	require.Equal(t, uint32(capacity), total)
	require.Equal(t, capacity, accounting.length())

	wg.Add(workers)
	for _, packetCount := range accepted {
		go func() {
			defer wg.Done()
			accounting.release(packetCount)
		}()
	}
	wg.Wait()
	require.Zero(t, accounting.length())
}

func TestPacketBatchQueueAdmitsPureRTPByPacketCapacity(t *testing.T) {
	tests := []struct {
		name, iface            string
		capacity, accepted     uint32
		wantDrops, wantBatches int
	}{
		{name: "full capacity", iface: "eth-full", capacity: 3, accepted: 3, wantBatches: 1},
		{name: "partial capacity", iface: "eth-partial", capacity: 2, accepted: 2, wantDrops: 1, wantBatches: 1},
		{name: "zero capacity", iface: "eth-zero", wantDrops: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := packetRingBlockWithPackets(t, []packetRingFrame{
				{data: buildUDPPacket(10000, 5004)},
				{data: buildUDPPacket(10001, 5005)},
				{data: buildUDPPacket(10002, 5006)},
			})
			drops := 0
			queue := newPacketBatchQueue(tt.capacity, func() { drops++ })

			keepReading, err := queue.enqueueBlock(block, tt.iface, []uint16{5060})

			require.NoError(t, err)
			require.True(t, keepReading)
			require.Equal(t, tt.wantDrops, drops)
			require.Equal(t, int(tt.accepted), queue.accounting.length())
			require.Len(t, queue.runs, tt.wantBatches)
			if tt.accepted == 0 {
				require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))
				return
			}

			run := <-queue.runs
			require.Equal(t, tt.iface, run.iface)
			require.Len(t, run.frames, int(tt.accepted))
			for i, frame := range run.frames {
				require.Equal(t, uint16(5004+i), binary.BigEndian.Uint16(frame.data[36:38]))
			}
			require.Equal(t, uint32(unix.TP_STATUS_USER), binary.NativeEndian.Uint32(block.data[8:12]))

			run.release()
			require.Zero(t, queue.accounting.length())
			require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))
		})
	}
}

func TestPacketBatchQueueBlocksSIPUntilConsumerReleasesCapacity(t *testing.T) {
	var drops atomic.Int32
	queue := newPacketBatchQueue(2, func() { drops.Add(1) })
	firstBlock := packetRingBlockWithPackets(t, []packetRingFrame{
		{data: buildUDPPacket(10000, 5004)},
		{data: buildUDPPacket(10001, 5005)},
	})
	keepReading, err := queue.enqueueBlock(firstBlock, "eth-rtp", []uint16{5060})
	require.NoError(t, err)
	require.True(t, keepReading)
	firstRun := <-queue.runs

	sipBlock := packetRingBlockWithPackets(t, []packetRingFrame{
		{data: buildUDPPacket(20000, 5060)},
		{data: buildUDPPacket(20001, 5060)},
	})
	type enqueueResult struct {
		keepReading bool
		err         error
	}
	result := make(chan enqueueResult, 1)
	go func() {
		keep, enqueueErr := queue.enqueueBlock(sipBlock, "eth-sip", []uint16{5060})
		result <- enqueueResult{keepReading: keep, err: enqueueErr}
	}()

	select {
	case <-result:
		t.Fatal("SIP run was admitted without packet capacity")
	case <-time.After(20 * time.Millisecond):
	}
	firstRun.release()

	select {
	case got := <-result:
		require.NoError(t, got.err)
		require.True(t, got.keepReading)
	case <-time.After(time.Second):
		t.Fatal("SIP run did not resume after consumer released capacity")
	}
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(firstBlock.data[8:12]))
	sipRun := <-queue.runs
	require.Len(t, sipRun.frames, 2)
	require.Equal(t, uint32(unix.TP_STATUS_USER), binary.NativeEndian.Uint32(sipBlock.data[8:12]))
	sipRun.release()
	require.Zero(t, drops.Load())
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(sipBlock.data[8:12]))
}

func TestPacketBatchQueueRTPYieldsToWaitingSIP(t *testing.T) {
	var drops atomic.Int32
	queue := newPacketBatchQueue(2, func() { drops.Add(1) })
	t.Cleanup(queue.stop)
	require.Equal(t, uint32(2), queue.accounting.reserve(2))

	sipBlock := packetRingBlockWithPackets(t, []packetRingFrame{
		{data: buildUDPPacket(20000, 5060)},
		{data: buildUDPPacket(20001, 5060)},
	})
	type enqueueResult struct {
		keepReading bool
		err         error
	}
	result := make(chan enqueueResult, 1)
	go func() {
		keep, err := queue.enqueueBlock(sipBlock, "eth-sip", []uint16{5060})
		result <- enqueueResult{keepReading: keep, err: err}
	}()
	require.Eventually(t, func() bool {
		return packetBatchQueueSIPWaiters(queue) == 1
	}, time.Second, time.Millisecond)

	queue.admissionMu.Lock()
	queue.accounting.release(1)
	queue.admissionMu.Unlock()
	rtpBlock := packetRingBlockWithPackets(t,
		[]packetRingFrame{{data: buildUDPPacket(10000, 5004)}})
	keepReading, err := queue.enqueueBlock(rtpBlock, "eth-rtp", []uint16{5060})

	require.NoError(t, err)
	require.True(t, keepReading)
	require.Equal(t, int32(1), drops.Load())
	require.Equal(t, 1, queue.accounting.length())
	require.Empty(t, queue.runs)
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(rtpBlock.data[8:12]))

	queue.release(1)
	select {
	case got := <-result:
		require.NoError(t, got.err)
		require.True(t, got.keepReading)
	case <-time.After(time.Second):
		t.Fatal("waiting SIP run did not reserve released capacity")
	}
	require.Zero(t, packetBatchQueueSIPWaiters(queue))
	sipRun := <-queue.runs
	require.Len(t, sipRun.frames, 2)
	sipRun.release()
	require.Zero(t, queue.accounting.length())
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(sipBlock.data[8:12]))
}

func TestPacketBatchQueuePreservesSIPWaiterOrder(t *testing.T) {
	var drops atomic.Int32
	queue := newPacketBatchQueue(2, func() { drops.Add(1) })
	t.Cleanup(queue.stop)
	require.Equal(t, uint32(2), queue.accounting.reserve(2))
	type enqueueResult struct {
		keepReading bool
		err         error
	}

	olderBlock := packetRingBlockWithPackets(t, []packetRingFrame{
		{data: buildUDPPacket(20000, 5060)},
		{data: buildUDPPacket(20001, 5060)},
	})
	olderResult := make(chan enqueueResult, 1)
	go func() {
		keep, err := queue.enqueueBlock(olderBlock, "eth-older", []uint16{5060})
		olderResult <- enqueueResult{keepReading: keep, err: err}
	}()
	require.Eventually(t, func() bool {
		return packetBatchQueueSIPWaiters(queue) == 1
	}, time.Second, time.Millisecond)

	queue.admissionMu.Lock()
	queue.accounting.release(1)
	queue.admissionMu.Unlock()
	newerBlock := packetRingBlockWithPackets(t,
		[]packetRingFrame{{data: buildUDPPacket(30000, 5060)}})
	newerResult := make(chan enqueueResult, 1)
	go func() {
		keep, err := queue.enqueueBlock(newerBlock, "eth-newer", []uint16{5060})
		newerResult <- enqueueResult{keepReading: keep, err: err}
	}()
	require.Eventually(t, func() bool {
		return packetBatchQueueSIPWaiters(queue) == 2
	}, time.Second, time.Millisecond)

	queue.release(1)
	select {
	case got := <-olderResult:
		require.NoError(t, got.err)
		require.True(t, got.keepReading)
	case <-time.After(time.Second):
		t.Fatal("older SIP run did not reserve full capacity")
	}
	olderRun := <-queue.runs
	require.Equal(t, "eth-older", olderRun.iface)
	require.Len(t, olderRun.frames, 2)
	require.Equal(t, uint32(1), packetBatchQueueSIPWaiters(queue))
	olderRun.release()

	select {
	case got := <-newerResult:
		require.NoError(t, got.err)
		require.True(t, got.keepReading)
	case <-time.After(time.Second):
		t.Fatal("newer SIP run did not follow the older run")
	}
	newerRun := <-queue.runs
	require.Equal(t, "eth-newer", newerRun.iface)
	require.Len(t, newerRun.frames, 1)
	newerRun.release()

	require.Zero(t, packetBatchQueueSIPWaiters(queue))
	require.Zero(t, queue.accounting.length())
	require.Zero(t, drops.Load())
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(olderBlock.data[8:12]))
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(newerBlock.data[8:12]))
}

func TestPacketBatchQueueSplitsSIPRunLargerThanCapacity(t *testing.T) {
	var drops atomic.Int32
	queue := newPacketBatchQueue(2, func() { drops.Add(1) })
	t.Cleanup(queue.stop)
	block := packetRingBlockWithPackets(t, []packetRingFrame{
		{data: buildUDPPacket(20000, 5060)},
		{data: buildUDPPacket(20001, 5060)},
		{data: buildUDPPacket(20002, 5060)},
	})
	type enqueueResult struct {
		keepReading bool
		err         error
	}
	result := make(chan enqueueResult, 1)
	go func() {
		keep, err := queue.enqueueBlock(block, "eth-sip", []uint16{5060})
		result <- enqueueResult{keepReading: keep, err: err}
	}()

	require.Eventually(t, func() bool { return len(queue.runs) == 1 }, time.Second, time.Millisecond)
	first := <-queue.runs
	require.Len(t, first.frames, 2)
	first.release()

	select {
	case got := <-result:
		require.NoError(t, got.err)
		require.True(t, got.keepReading)
	case <-time.After(time.Second):
		t.Fatal("SIP suffix was not admitted after consumer released capacity")
	}
	second := <-queue.runs
	require.Len(t, second.frames, 1)
	require.Equal(t, uint16(5060), binary.BigEndian.Uint16(second.frames[0].data[36:38]))
	require.Equal(t, uint32(unix.TP_STATUS_USER), binary.NativeEndian.Uint32(block.data[8:12]))
	second.release()
	require.Zero(t, drops.Load())
	require.Zero(t, queue.accounting.length())
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))
}

func TestPacketBatchQueuePreservesMixedRunOrderAndRTPPolicy(t *testing.T) {
	drops := 0
	queue := newPacketBatchQueue(2, func() { drops++ })
	block := packetRingBlockWithPackets(t, []packetRingFrame{
		{data: buildUDPPacket(10000, 5060)},
		{data: buildUDPPacket(10001, 5004)},
		{data: buildUDPPacket(10002, 5060)},
		{data: buildUDPPacket(10003, 5005)},
	})
	type enqueueResult struct {
		keepReading bool
		err         error
	}
	result := make(chan enqueueResult, 1)
	go func() {
		keep, err := queue.enqueueBlock(block, "eth-mixed", []uint16{5060})
		result <- enqueueResult{keepReading: keep, err: err}
	}()

	require.Eventually(t, func() bool { return len(queue.runs) == 2 }, time.Second, time.Millisecond)
	first := <-queue.runs
	wantPorts := []uint16{5060}
	require.Equal(t, wantPorts[0], binary.BigEndian.Uint16(first.frames[0].data[36:38]))
	first.release()

	select {
	case got := <-result:
		require.NoError(t, got.err)
		require.True(t, got.keepReading)
	case <-time.After(time.Second):
		t.Fatal("mixed block did not resume after SIP capacity became available")
	}
	for len(queue.runs) != 0 {
		run := <-queue.runs
		for _, frame := range run.frames {
			wantPorts = append(wantPorts, binary.BigEndian.Uint16(frame.data[36:38]))
		}
		run.release()
	}

	require.Equal(t, []uint16{5060, 5004, 5060}, wantPorts)
	require.Equal(t, 1, drops)
	require.Zero(t, queue.accounting.length())
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))
}

func TestPacketBatchQueueStopsBlockedSIPOnShutdown(t *testing.T) {
	var drops atomic.Int32
	queue := newPacketBatchQueue(0, func() { drops.Add(1) })
	block := packetRingBlockWithPackets(t, []packetRingFrame{{data: buildUDPPacket(10000, 5060)}})
	type enqueueResult struct {
		keepReading bool
		err         error
	}
	result := make(chan enqueueResult, 1)
	go func() {
		keepReading, err := queue.enqueueBlock(block, "eth-sip", []uint16{5060})
		result <- enqueueResult{keepReading: keepReading, err: err}
	}()
	require.Eventually(t, func() bool {
		return packetBatchQueueSIPWaiters(queue) == 1
	}, time.Second, time.Millisecond)
	queue.stop()

	select {
	case got := <-result:
		require.NoError(t, got.err)
		require.False(t, got.keepReading)
	case <-time.After(time.Second):
		t.Fatal("blocked SIP admission ignored shutdown")
	}
	require.Zero(t, queue.accounting.length())
	require.Empty(t, queue.runs)
	require.Zero(t, drops.Load())
	require.Zero(t, packetBatchQueueSIPWaiters(queue))
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))
}

func TestPacketBatchQueueRejectsRunsAfterStop(t *testing.T) {
	tests := []struct {
		name     string
		capacity uint32
		port     uint16
	}{
		{name: "SIP with capacity", capacity: 1, port: 5060},
		{name: "RTP with capacity", capacity: 1, port: 5004},
		{name: "RTP without capacity", port: 5004},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var drops atomic.Int32
			queue := newPacketBatchQueue(tt.capacity, func() { drops.Add(1) })
			queue.stop()
			block := packetRingBlockWithPackets(t,
				[]packetRingFrame{{data: buildUDPPacket(10000, tt.port)}})

			keepReading, err := queue.enqueueBlock(block, "eth-stopped", []uint16{5060})

			require.NoError(t, err)
			require.False(t, keepReading)
			require.Zero(t, drops.Load())
			require.Zero(t, queue.accounting.length())
			require.Empty(t, queue.runs)
			require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))
		})
	}
}

func packetBatchQueueSIPWaiters(queue *packetBatchQueue) uint32 {
	queue.admissionMu.Lock()
	defer queue.admissionMu.Unlock()
	return queue.pendingSIP
}

func TestPacketBatchQueueAdmitsValidPrefixBeforeIteratorError(t *testing.T) {
	tests := []struct {
		name                  string
		prefixPort, capacity  uint16
		wantBatches, wantDrop int
	}{
		{name: "SIP prefix", prefixPort: 5060, capacity: 1, wantBatches: 1},
		{name: "RTP prefix without capacity", prefixPort: 5004, wantDrop: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := packetRingBlockWithPackets(t, []packetRingFrame{
				{data: buildUDPPacket(10000, tt.prefixPort)},
				{data: buildUDPPacket(10001, 5005)},
			})
			const secondFrameOffset = tpacketV3BlockHeaderLen + 128
			binary.NativeEndian.PutUint16(
				block.data[secondFrameOffset+24:], uint16(tpacketV3HeaderLen-1),
			)
			drops := 0
			queue := newPacketBatchQueue(uint32(tt.capacity), func() { drops++ })

			keepReading, err := queue.enqueueBlock(block, "eth-prefix", []uint16{5060})

			require.ErrorIs(t, err, unix.EINVAL)
			require.True(t, keepReading)
			require.Equal(t, tt.wantDrop, drops)
			require.Len(t, queue.runs, tt.wantBatches)
			if tt.wantBatches == 0 {
				require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))
				return
			}
			run := <-queue.runs
			require.Len(t, run.frames, 1)
			require.Equal(t, tt.prefixPort, binary.BigEndian.Uint16(run.frames[0].data[36:38]))
			require.Equal(t, uint32(unix.TP_STATUS_USER), binary.NativeEndian.Uint32(block.data[8:12]))
			run.release()
			require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))
		})
	}
}

func TestConsumePacketRingBlockCopiesFramesAndMetadata(t *testing.T) {
	sip := buildUDPPacket(12345, 5060)
	rtp := buildUDPPacket(12345, 5004)
	rtcp := buildUDPPacket(12345, 5005)
	block := packetRingBlockWithPackets(t, []packetRingFrame{
		{data: sip, ts: time.Unix(100, 200), pkttype: unix.PACKET_HOST},
		{data: rtp, ts: time.Unix(300, 400), pkttype: unix.PACKET_OUTGOING},
		{data: rtcp, ts: time.Unix(500, 600), pkttype: unix.PACKET_HOST},
	})
	e := &exporter{
		messages: make(chan *rawPacket, 3), done: make(chan struct{}),
		services: services{metricser: &mockMetricser{}},
	}

	keepReading, err := e.consumePacketRingBlock(block, "eth-test", []uint16{5060})
	require.NoError(t, err)
	require.True(t, keepReading)
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))

	packets := make([]*rawPacket, 0, 3)
	for i, want := range []packetRingFrame{
		{data: sip, ts: time.Unix(100, 200), pkttype: unix.PACKET_HOST},
		{data: rtp, ts: time.Unix(300, 400), pkttype: unix.PACKET_OUTGOING},
		{data: rtcp, ts: time.Unix(500, 600), pkttype: unix.PACKET_HOST},
	} {
		packet := <-e.messages
		packets = append(packets, packet)
		require.Equal(t, want.data, packet.data, "packet %d payload", i)
		require.Equal(t, "eth-test", packet.iface)
		require.Equal(t, want.ts, packet.ts)
		require.Equal(t, want.pkttype, packet.pkttype)
	}

	block.data[128] ^= 0xff
	require.Equal(t, sip, packets[0].data)
}

func TestConsumePacketRingBlockCountsEveryRTPDrop(t *testing.T) {
	block := packetRingBlockWithPackets(t, []packetRingFrame{
		{data: buildUDPPacket(12345, 5004)},
		{data: buildUDPPacket(12345, 5005)},
	})
	metricser := &mockMetricser{}
	e := &exporter{
		messages: make(chan *rawPacket, 1), done: make(chan struct{}),
		services: services{metricser: metricser},
	}
	e.messages <- &rawPacket{}

	keepReading, err := e.consumePacketRingBlock(block, "eth-test", []uint16{5060})
	require.NoError(t, err)
	require.True(t, keepReading)
	require.Equal(t, 2, metricser.rtpDroppedCount)
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))
}

func TestConsumePacketRingBlockReleasesOnShutdown(t *testing.T) {
	block := packetRingBlockWithPackets(t, []packetRingFrame{{data: buildUDPPacket(12345, 5060)}})
	e := &exporter{
		messages: make(chan *rawPacket), done: make(chan struct{}),
		services: services{metricser: &mockMetricser{}},
	}
	close(e.done)

	keepReading, err := e.consumePacketRingBlock(block, "eth-test", []uint16{5060})
	require.NoError(t, err)
	require.False(t, keepReading)
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))
}

func TestConsumePacketRingBlockReleasesMalformedBlock(t *testing.T) {
	block := packetRingBlockWithPackets(t, []packetRingFrame{{data: buildUDPPacket(12345, 5060)}})
	binary.NativeEndian.PutUint16(block.data[tpacketV3BlockHeaderLen+24:], tpacketV3HeaderLen-1)
	e := &exporter{
		messages: make(chan *rawPacket, 1), done: make(chan struct{}),
		services: services{metricser: &mockMetricser{}},
	}

	keepReading, err := e.consumePacketRingBlock(block, "eth-test", []uint16{5060})

	require.ErrorIs(t, err, unix.EINVAL)
	require.True(t, keepReading)
	require.Empty(t, e.messages)
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))
}

func TestReadPacketRingConsumesBlocksBeforeWaiting(t *testing.T) {
	first := packetRingBlockWithPackets(t, []packetRingFrame{{data: buildUDPPacket(10000, 5060)}})
	second := packetRingBlockWithPackets(t, []packetRingFrame{{data: buildUDPPacket(10001, 5060)}})
	memory := append(append([]byte(nil), first.data...), second.data...)
	ring := &packetRing{
		fd: 42, memory: memory, blockSize: uint32(len(first.data)), blockCount: 2,
	}
	for i := range uint32(2) {
		block, ready, err := ring.decodeBlock(i)
		require.NoError(t, err, "block %d", i)
		require.True(t, ready, "block %d", i)
		require.Equal(t, uint32(1), block.packetCount, "block %d", i)
	}
	metricser := &mockMetricser{}
	e := &exporter{
		messages: make(chan *rawPacket, 2), done: make(chan struct{}),
		services: services{metricser: metricser},
	}
	waits := 0

	e.readPacketRing(sockEntry{ring: ring, iface: "eth-test"}, []uint16{5060}, func(fd int) error {
		require.Equal(t, 42, fd)
		waits++
		return unix.ENODEV
	})

	require.Equal(t, 1, waits)
	require.Len(t, e.messages, 2)
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(memory[8:12]))
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(memory[len(first.data)+8:]))
	require.False(t, metricser.systemErrorCalled)
}

func TestReadPacketRingContinuesAfterNonDeliveringBlock(t *testing.T) {
	tests := []struct {
		name            string
		first           func(*testing.T) packetRingBlock
		wantSystemError bool
	}{
		{
			name: "empty block",
			first: func(t *testing.T) packetRingBlock {
				return packetRingBlockWithPackets(t, nil)
			},
		},
		{
			name: "malformed frame",
			first: func(t *testing.T) packetRingBlock {
				block := packetRingBlockWithPackets(t,
					[]packetRingFrame{{data: buildUDPPacket(10000, 5060)}})
				binary.NativeEndian.PutUint16(
					block.data[tpacketV3BlockHeaderLen+24:], tpacketV3HeaderLen-1)
				return block
			},
			wantSystemError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := tt.first(t)
			second := packetRingBlockWithPackets(t,
				[]packetRingFrame{{data: buildUDPPacket(10001, 5060)}})
			blockSize := len(second.data)
			memory := make([]byte, blockSize*2)
			copy(memory, first.data)
			copy(memory[blockSize:], second.data)
			ring := &packetRing{
				fd: 42, memory: memory, blockSize: uint32(blockSize), blockCount: 2,
			}
			metricser := &mockMetricser{}
			e := &exporter{
				messages: make(chan *rawPacket, 1), done: make(chan struct{}),
				services: services{metricser: metricser},
			}

			e.readPacketRing(sockEntry{ring: ring, iface: "eth-test"}, []uint16{5060},
				func(int) error { return unix.ENODEV })

			require.Len(t, e.messages, 1)
			require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(memory[8:12]))
			require.Equal(t, uint32(unix.TP_STATUS_KERNEL),
				binary.NativeEndian.Uint32(memory[blockSize+8:]))
			require.Equal(t, tt.wantSystemError, metricser.systemErrorCalled)
		})
	}
}

func TestReadPacketRingHotUnplugStopsWithoutSystemError(t *testing.T) {
	metricser := &mockMetricser{}
	e := &exporter{
		messages: make(chan *rawPacket, 1), done: make(chan struct{}),
		services: services{metricser: metricser},
	}
	ring := &packetRing{fd: 42, memory: make([]byte, 128), blockSize: 128, blockCount: 1}

	e.readPacketRing(sockEntry{ring: ring, iface: "eth-test"}, []uint16{5060}, func(int) error {
		return unix.ENETDOWN
	})

	require.False(t, metricser.systemErrorCalled)
}

func TestWaitPacketRingRejectsInvalidFD(t *testing.T) {
	require.ErrorIs(t, waitPacketRing(-1), unix.EBADF)
}

func TestPacketRingPollResult(t *testing.T) {
	tests := []struct {
		name      string
		revents   int16
		socketErr int
		want      error
	}{
		{name: "ready", revents: unix.POLLIN},
		{name: "invalid fd", revents: unix.POLLNVAL, want: unix.EBADF},
		{name: "hot unplug", revents: unix.POLLERR, socketErr: int(unix.ENETDOWN), want: unix.ENETDOWN},
		{name: "unexpected socket error", revents: unix.POLLERR, socketErr: int(unix.EIO), want: unix.EIO},
		{name: "hangup without socket error", revents: unix.POLLHUP, want: unix.ENODEV},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, packetRingPollResult(tt.revents, tt.socketErr), tt.want)
		})
	}
}

func TestReadSocketUsesPacketRing(t *testing.T) {
	payload := buildUDPPacket(10000, 5060)
	block := packetRingBlockWithPackets(t, []packetRingFrame{{data: payload}})
	ring := &packetRing{
		fd: -1, memory: block.data, blockSize: uint32(len(block.data)), blockCount: 1,
	}
	e := &exporter{
		socks:       []sockEntry{{fd: -1, iface: "eth-test", ring: ring}},
		sipPortSets: [][]uint16{{5060}},
		messages:    make(chan *rawPacket, 1),
		done:        make(chan struct{}),
		services:    services{metricser: &mockMetricser{}},
	}

	e.wg.Add(1)
	go e.readSocket(0)

	select {
	case packet := <-e.messages:
		require.Equal(t, payload, packet.data)
		require.Equal(t, "eth-test", packet.iface)
	case <-time.After(time.Second):
		t.Fatal("readSocket did not consume the ready packet ring block")
	}
	e.wg.Wait()
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(block.data[8:12]))
}

func TestExporterCloseUnmapsPacketRings(t *testing.T) {
	memory, err := unix.Mmap(-1, 0, unix.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANON|unix.MAP_PRIVATE)
	require.NoError(t, err)
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = unix.Close(fds[1]) })
	ring := &packetRing{fd: fds[0], memory: memory, fdOpen: true}
	e := &exporter{
		socks: []sockEntry{{fd: fds[0], ring: ring}},
		done:  make(chan struct{}), messages: make(chan *rawPacket),
	}

	e.Close()

	require.False(t, ring.fdOpen)
	require.Nil(t, ring.memory)
	require.ErrorIs(t, unix.Mprotect(memory, unix.PROT_NONE), unix.ENOMEM)
}

func TestExporterCloseStopsBlockedPacketRingReaderBeforeTeardown(t *testing.T) {
	block := packetRingBlockWithPackets(t, []packetRingFrame{{data: buildUDPPacket(12345, 5060)}})
	fd, err := unix.MemfdCreate("packet-ring-close-test", unix.MFD_CLOEXEC)
	require.NoError(t, err)
	fdOwnedByRing := false
	t.Cleanup(func() {
		if !fdOwnedByRing {
			_ = unix.Close(fd)
		}
	})
	require.NoError(t, unix.Ftruncate(fd, int64(unix.Getpagesize())))
	ringMemory, err := unix.Mmap(fd, 0, unix.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_SHARED)
	require.NoError(t, err)
	memoryOwnedByRing := false
	t.Cleanup(func() {
		if !memoryOwnedByRing {
			_ = unix.Munmap(ringMemory)
		}
	})
	observerMemory, err := unix.Mmap(fd, 0, unix.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_SHARED)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, unix.Munmap(observerMemory)) })
	copy(ringMemory, block.data)
	ring := &packetRing{
		fd: fd, memory: ringMemory, blockSize: uint32(len(ringMemory)), blockCount: 1, fdOpen: true,
	}
	fdOwnedByRing = true
	memoryOwnedByRing = true
	t.Cleanup(func() { _ = ring.close() })
	acquired := make(chan struct{})
	e := &exporter{
		socks:       []sockEntry{{fd: -1, iface: "eth-test", ring: ring}},
		sipPortSets: [][]uint16{{5060}},
		messages:    make(chan *rawPacket, 1),
		done:        make(chan struct{}),
		services:    services{metricser: &mockMetricser{}},
		packetPool: sync.Pool{New: func() any {
			close(acquired)
			return &rawPacket{data: make([]byte, 0, readBufSize)}
		}},
	}
	e.messages <- &rawPacket{}
	e.wg.Add(1)
	go e.readSocket(0)
	<-acquired
	closed := make(chan struct{})
	go func() {
		e.Close()
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not stop the blocked packet ring reader")
	}
	require.Equal(t, uint32(unix.TP_STATUS_KERNEL), binary.NativeEndian.Uint32(observerMemory[8:12]))
	require.Nil(t, ring.memory)
	require.False(t, ring.fdOpen)
	require.ErrorIs(t, unix.Mprotect(ringMemory, unix.PROT_NONE), unix.ENOMEM)
}

func packetRingBlockWithPackets(t *testing.T, frames []packetRingFrame) packetRingBlock {
	t.Helper()
	const frameStride = 128
	memory := make([]byte, tpacketV3BlockHeaderLen+frameStride*len(frames))
	binary.NativeEndian.PutUint32(memory[8:12], unix.TP_STATUS_USER)
	binary.NativeEndian.PutUint32(memory[12:16], uint32(len(frames)))
	binary.NativeEndian.PutUint32(memory[16:20], tpacketV3BlockHeaderLen)
	for i, frame := range frames {
		offset := uint32(tpacketV3BlockHeaderLen + frameStride*i)
		nextOffset := uint32(frameStride)
		if i == len(frames)-1 {
			nextOffset = 0
		}
		putPacketRingFrame(memory, offset, nextOffset, frame.ts, frame.pkttype, frame.data)
	}
	return packetRingBlock{
		data: memory, packetCount: uint32(len(frames)), firstPacketOffset: tpacketV3BlockHeaderLen,
	}
}

func putPacketRingFrame(
	memory []byte, offset, nextOffset uint32, timestamp time.Time, pkttype uint8, payload []byte,
) {
	header := memory[offset:]
	binary.NativeEndian.PutUint32(header[0:4], nextOffset)
	binary.NativeEndian.PutUint32(header[4:8], uint32(timestamp.Unix()))
	binary.NativeEndian.PutUint32(header[8:12], uint32(timestamp.Nanosecond()))
	binary.NativeEndian.PutUint32(header[12:16], uint32(len(payload)))
	binary.NativeEndian.PutUint16(header[24:26], 80)
	header[unix.SizeofTpacket3Hdr+10] = pkttype
	copy(header[80:], payload)
}
