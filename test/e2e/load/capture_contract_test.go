//go:build e2e

package load

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewCaptureResultAccountsForMissingAndExcess(t *testing.T) {
	tests := []struct {
		name     string
		expected float64
		captured float64
		want     CaptureResult
		wantErr  bool
	}{
		{
			name: "zero expected and captured",
			want: CaptureResult{},
		},
		{
			name:     "one missing",
			expected: 100,
			captured: 99,
			want: CaptureResult{
				Expected: 100, Captured: 99, Missing: 1, LossPct: 1,
			},
			wantErr: true,
		},
		{
			name:     "exact",
			expected: 100,
			captured: 100,
			want:     CaptureResult{Expected: 100, Captured: 100},
		},
		{
			name:     "one excess",
			expected: 100,
			captured: 101,
			want: CaptureResult{
				Expected: 100, Captured: 101, Excess: 1, ExcessPct: 1,
			},
			wantErr: true,
		},
		{
			name:     "double expected",
			expected: 100,
			captured: 200,
			want: CaptureResult{
				Expected: 100, Captured: 200, Excess: 100, ExcessPct: 100,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := newCaptureResult(tt.expected, tt.captured)

			require.Equal(t, tt.want, result)
			if tt.wantErr {
				require.Error(t, result.ValidateExact())
			} else {
				require.NoError(t, result.ValidateExact())
			}
		})
	}
}

func TestProtocolCountersDeltaKeepsProtocolsIndependent(t *testing.T) {
	before := ProtocolCounters{
		SIPPackets: 10, RTPPackets: 20, RTCPReports: 30,
		VQReports: 40, SocketReceived: 50, SocketDropped: 60,
	}
	after := ProtocolCounters{
		SIPPackets: 11, RTPPackets: 22, RTCPReports: 33,
		VQReports: 44, SocketReceived: 55, SocketDropped: 66,
	}

	require.Equal(t, ProtocolCounters{
		SIPPackets: 1, RTPPackets: 2, RTCPReports: 3,
		VQReports: 4, SocketReceived: 5, SocketDropped: 6,
	}, after.delta(before))
}

func TestExactCaptureCompleteRequiresCountAndEmptyChannel(t *testing.T) {
	tests := []struct {
		name          string
		expected      float64
		captured      float64
		channelLength float64
		want          bool
	}{
		{name: "exact and drained", expected: 100, captured: 100, want: true},
		{name: "missing", expected: 100, captured: 99},
		{name: "excess", expected: 100, captured: 101},
		{name: "exact but queued", expected: 100, captured: 100, channelLength: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want,
				exactCaptureComplete(tt.expected, tt.captured, tt.channelLength))
		})
	}
}
