//go:build e2e

package load

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	floodPacketsPerCall    = 2.0
	fullCallPacketsPerCall = 14.0
	subtestTimeout         = 12 * time.Second
)

func TestLoad_INVITEFlood(t *testing.T) {
	rates := []int{100, 500, 1000, 2000, 5000}
	for _, rate := range rates {
		t.Run(fmt.Sprintf("rate_%d", rate), func(t *testing.T) {
			env := newTestEnv(context.Background(), t)

			ctx, cancel := context.WithTimeout(context.Background(), subtestTimeout)
			defer cancel()

			callCount := rate * 5
			result := runSippLoad(ctx, t, "", "flood_uac.xml", callCount, rate, floodPacketsPerCall, env)

			t.Logf("INVITE flood rate=%d: actual=%.0f PPS, expected=%.0f PPS, loss=%.2f%%, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB",
				rate, result.ActualPPS, result.ExpectedPPS, result.LossRate*100,
				result.DrainTime, result.CPUAvg, result.CPUPeak, result.MemMaxMB)

			require.Equal(t, float64(0), result.ErrorCount,
				"no parse errors expected during INVITE flood")
			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets")
		})
	}
}

func TestLoad_FullCallFlow(t *testing.T) {
	rates := []int{100, 500, 1000, 1200, 1400, 1600, 1800, 2000}
	for _, rate := range rates {
		t.Run(fmt.Sprintf("rate_%d", rate), func(t *testing.T) {
			env := newTestEnv(context.Background(), t)

			ctx, cancel := context.WithTimeout(context.Background(), subtestTimeout)
			defer cancel()

			callCount := rate * 5
			result := runSippLoad(ctx, t, "call_highrate_uas.xml", "call_highrate_uac.xml",
				callCount, rate, fullCallPacketsPerCall, env)

			t.Logf("Full call rate=%d: actual=%.0f PPS, expected=%.0f PPS, loss=%.2f%%, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB",
				rate, result.ActualPPS, result.ExpectedPPS, result.LossRate*100,
				result.DrainTime, result.CPUAvg, result.CPUPeak, result.MemMaxMB)

			require.Equal(t, float64(0), result.ErrorCount,
				"no parse errors expected during full call flow")
			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets")

			ser := getMetric(t, env.endpoint, "sip_exporter_ser")
			require.Equal(t, 100.0, ser, "SER should be 100%% (got %.2f%%)", ser)
		})
	}
}

func TestLoad_ConcurrentSessions(t *testing.T) {
	limits := []int{500, 1000, 2000}
	for _, limit := range limits {
		t.Run(fmt.Sprintf("concurrent_%d", limit), func(t *testing.T) {
			env := newTestEnv(context.Background(), t)

			callCount := limit * 2
			rate := 100

			result := runConcurrentLoad(context.Background(), t,
				"concurrent_uas.xml", "concurrent_uac.xml",
				callCount, rate, limit, env)

			sessions := getMetric(t, env.endpoint, "sip_exporter_sessions")
			inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")

			t.Logf("Concurrent %d: sessions_peak=%.0f, invites=%.0f, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB, duration=%v",
				limit, sessions, inviteTotal, result.DrainTime,
				result.CPUAvg, result.CPUPeak, result.MemMaxMB, result.Duration)

			require.Greater(t, inviteTotal, float64(0),
				"should have INVITE requests")
			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets")
		})
	}
}
