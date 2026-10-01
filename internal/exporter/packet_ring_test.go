package exporter

import (
	"errors"
	"math"
	"syscall"
	"testing"

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
