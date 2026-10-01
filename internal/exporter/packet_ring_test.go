package exporter

import (
	"encoding/binary"
	"errors"
	"math"
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
