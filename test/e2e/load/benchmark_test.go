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
	subtestTimeout         = 20 * time.Second
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

			totalPackets := result.PacketsAfter - result.PacketsBefore
			maxErrors := totalPackets * 0.001
			require.LessOrEqual(t, result.ErrorCount, maxErrors,
				"error rate SLO: < 0.1%% of processed packets")
			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets")

			recordResult(t.Name(), map[string]MetricEntry{
				"actual_pps": {Value: result.ActualPPS, Unit: "pps", Direction: dirHigherIsBetter},
				"loss_rate":  {Value: result.LossRate * 100, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_peak":   {Value: result.CPUPeak, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_avg":    {Value: result.CPUAvg, Unit: "%", Direction: dirLowerIsBetter},
				"mem_mb":     {Value: result.MemMaxMB, Unit: "MB", Direction: dirLowerIsBetter},
			})
		})
	}
}

func TestLoad_FullCallFlow(t *testing.T) {
	rates := []int{100, 500, 1000, 1200, 1400, 1600, 1800}
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

			totalPackets := result.PacketsAfter - result.PacketsBefore
			maxErrors := totalPackets * 0.001
			require.LessOrEqual(t, result.ErrorCount, maxErrors,
				"error rate SLO: < 0.1%% of processed packets")
			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets")

			ser := getMetric(t, env.endpoint, "sip_exporter_ser")
			require.GreaterOrEqual(t, ser, 99.0,
				"SER SLO: >= 99%% at rate %d (got %.2f%%)", rate, ser)

			recordResult(t.Name(), map[string]MetricEntry{
				"actual_pps": {Value: result.ActualPPS, Unit: "pps", Direction: dirHigherIsBetter},
				"loss_rate":  {Value: result.LossRate * 100, Unit: "%", Direction: dirLowerIsBetter},
				"ser":        {Value: ser, Unit: "%", Direction: dirHigherIsBetter},
				"cpu_peak":   {Value: result.CPUPeak, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_avg":    {Value: result.CPUAvg, Unit: "%", Direction: dirLowerIsBetter},
				"mem_mb":     {Value: result.MemMaxMB, Unit: "MB", Direction: dirLowerIsBetter},
			})
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

			recordResult(t.Name(), map[string]MetricEntry{
				"sessions": {Value: sessions, Unit: "count", Direction: dirHigherIsBetter},
				"invites":  {Value: inviteTotal, Unit: "count", Direction: dirHigherIsBetter},
				"cpu_peak": {Value: result.CPUPeak, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_avg":  {Value: result.CPUAvg, Unit: "%", Direction: dirLowerIsBetter},
				"mem_mb":   {Value: result.MemMaxMB, Unit: "MB", Direction: dirLowerIsBetter},
			})
		})
	}
}
