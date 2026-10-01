package exporter

import (
	"math"
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
