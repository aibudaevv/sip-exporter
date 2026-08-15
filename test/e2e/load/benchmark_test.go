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
	floodPacketsPerCall    = 1.0
	fullCallPacketsPerCall = 7.0
	subtestTimeout         = 20 * time.Second
)

func TestLoadINVITEFlood(t *testing.T) {
	rates := []int{100, 500, 1000, 2000, 5000}
	for _, rate := range rates {
		t.Run(fmt.Sprintf("rate_%d", rate), func(t *testing.T) {
			beginScenario(t)
			env := newTestEnv(t.Context(), t)

			ctx, cancel := context.WithTimeout(t.Context(), subtestTimeout)
			defer cancel()

			callCount := rate * 5
			result := runSippLoad(ctx, t, "", "flood_uac.xml", callCount, rate, floodPacketsPerCall, env)

			t.Logf("INVITE flood rate=%d: actual=%.0f PPS, expected=%.0f PPS, loss=%.2f%%, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB",
				rate, result.ActualPPS, result.ExpectedPPS, result.LossRate*100,
				result.DrainTime, result.Resources.CPUP95Percent,
				result.Resources.CPUP95Percent, result.Resources.WorkingSetP99MB)

			totalPackets := result.PacketsAfter - result.PacketsBefore
			maxErrors := totalPackets * 0.001
			require.LessOrEqual(t, result.ErrorCount, maxErrors,
				"error rate SLO: < 0.1%% of processed packets")
			require.LessOrEqual(t, result.LossRate, 0.01,
				"packet loss SLO: < 1%% at rate %d (got %.2f%%)", rate, result.LossRate*100)
			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets")

			metrics := resourceMetricEntries(result.Resources)
			metrics["actual_pps"] = MetricEntry{Value: result.ActualPPS, Unit: "pps", Direction: dirHigherIsBetter}
			metrics["loss_rate"] = MetricEntry{Value: result.LossRate * 100, Unit: "%", Direction: dirLowerIsBetter}
			recordResult(t, metrics)
		})
	}
}

func TestLoadFullCallFlow(t *testing.T) {
	rates := []int{100, 500, 1000, 1200, 1400, 1600, 1800}
	for _, rate := range rates {
		t.Run(fmt.Sprintf("rate_%d", rate), func(t *testing.T) {
			beginScenario(t)
			limits := peakLimits
			if rate == 1000 {
				limits = nominalLimits
			}
			env := newTestEnvWithLimits(t.Context(), t, limits)

			ctx, cancel := context.WithTimeout(t.Context(), subtestTimeout)
			defer cancel()

			callCount := rate * 5
			result := runSippLoad(ctx, t, "call_highrate_uas.xml", "call_highrate_uac.xml",
				callCount, rate, fullCallPacketsPerCall, env)

			t.Logf("Full call rate=%d: actual=%.0f PPS, expected=%.0f PPS, loss=%.2f%%, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB",
				rate, result.ActualPPS, result.ExpectedPPS, result.LossRate*100,
				result.DrainTime, result.Resources.CPUP95Percent,
				result.Resources.CPUP95Percent, result.Resources.WorkingSetP99MB)

			totalPackets := result.PacketsAfter - result.PacketsBefore
			maxErrors := totalPackets * 0.001
			require.LessOrEqual(t, result.ErrorCount, maxErrors,
				"error rate SLO: < 0.1%% of processed packets")
			require.LessOrEqual(t, result.LossRate, 0.01,
				"packet loss SLO: < 1%% at rate %d (got %.2f%%)", rate, result.LossRate*100)
			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets")

			ser := getMetric(t, env.endpoint, "sip_exporter_ser")
			require.GreaterOrEqual(t, ser, 99.0,
				"SER SLO: >= 99%% at rate %d (got %.2f%%)", rate, ser)

			metrics := resourceMetricEntries(result.Resources)
			metrics["actual_pps"] = MetricEntry{Value: result.ActualPPS, Unit: "pps", Direction: dirHigherIsBetter}
			metrics["loss_rate"] = MetricEntry{Value: result.LossRate * 100, Unit: "%", Direction: dirLowerIsBetter}
			metrics["ser"] = MetricEntry{Value: ser, Unit: "%", Direction: dirHigherIsBetter}
			recordResult(t, metrics)
		})
	}
}

func TestLoadConcurrentSessions(t *testing.T) {
	limits := []int{500, 1000, 2000}
	for _, limit := range limits {
		t.Run(fmt.Sprintf("concurrent_%d", limit), func(t *testing.T) {
			beginScenario(t)
			env := newTestEnv(t.Context(), t)

			callCount := limit * 2
			rate := 100

			result := runConcurrentLoad(t.Context(), t,
				"concurrent_uas.xml", "concurrent_uac.xml",
				callCount, rate, limit, env)

			inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")

			t.Logf("Concurrent %d: peak_sessions=%.0f, invites=%.0f, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB, duration=%v",
				limit, result.PeakSessions, inviteTotal, result.DrainTime,
				result.Resources.CPUP95Percent, result.Resources.CPUP95Percent,
				result.Resources.WorkingSetP99MB, result.Duration)

			require.Greater(t, result.PeakSessions, float64(0),
				"peak sessions should be > 0 during concurrent load")
			require.GreaterOrEqual(t, result.PeakSessions, float64(limit)*0.5,
				"peak sessions should reach >= 50%% of limit %d (got %.0f)", limit, result.PeakSessions)
			require.Greater(t, inviteTotal, float64(0),
				"should have INVITE requests")
			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets")

			metrics := resourceMetricEntries(result.Resources)
			metrics["sessions"] = MetricEntry{Value: result.PeakSessions, Unit: "count", Direction: dirHigherIsBetter}
			metrics["invites"] = MetricEntry{Value: inviteTotal, Unit: "count", Direction: dirHigherIsBetter}
			recordResult(t, metrics)
		})
	}
}
