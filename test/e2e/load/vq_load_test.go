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
	vqFloodPacketsPerCall    = 2.0
	vqHighratePacketsPerCall = 4.0
	fullCallVQPacketsPerCall = 18.0
)

func TestLoad_VQReportFlood(t *testing.T) {
	rates := []int{100, 500, 1000, 2000}
	for _, rate := range rates {
		t.Run(fmt.Sprintf("rate_%d", rate), func(t *testing.T) {
			env := newTestEnv(context.Background(), t)

			ctx, cancel := context.WithTimeout(context.Background(), subtestTimeout)
			defer cancel()

			callCount := rate * 5
			result := runSippLoad(ctx, t, "", "vq_flood_uac.xml", callCount, rate, vqFloodPacketsPerCall, env)

			t.Logf("VQ flood rate=%d: actual=%.0f PPS, expected=%.0f PPS, loss=%.2f%%, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB",
				rate, result.ActualPPS, result.ExpectedPPS, result.LossRate*100,
				result.DrainTime, result.CPUAvg, result.CPUPeak, result.MemMaxMB)

			totalPackets := result.PacketsAfter - result.PacketsBefore
			maxErrors := totalPackets * 0.001
			require.LessOrEqual(t, result.ErrorCount, maxErrors,
				"error rate SLO: < 0.1%% of processed packets")
			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets")

			vqReports := getMetric(t, env.endpoint, "sip_exporter_vq_reports_total")
			expectedReports := float64(callCount * 2)
			t.Logf("vq_reports_total = %.0f (want %.0f, loopback x2)", vqReports, expectedReports)
			require.Equal(t, expectedReports, vqReports)

			moslqCount := getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_count")
			require.Equal(t, expectedReports, moslqCount)

			recordResult(t.Name(), map[string]MetricEntry{
				"actual_pps":  {Value: result.ActualPPS, Unit: "pps", Direction: dirHigherIsBetter},
				"loss_rate":   {Value: result.LossRate * 100, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_peak":    {Value: result.CPUPeak, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_avg":     {Value: result.CPUAvg, Unit: "%", Direction: dirLowerIsBetter},
				"mem_mb":      {Value: result.MemMaxMB, Unit: "MB", Direction: dirLowerIsBetter},
				"vq_reports":  {Value: vqReports, Unit: "count", Direction: dirHigherIsBetter},
			})
		})
	}
}

func TestLoad_VQHighRateWithResponse(t *testing.T) {
	rates := []int{100, 500, 1000}
	for _, rate := range rates {
		t.Run(fmt.Sprintf("rate_%d", rate), func(t *testing.T) {
			env := newTestEnv(context.Background(), t)

			ctx, cancel := context.WithTimeout(context.Background(), subtestTimeout)
			defer cancel()

			callCount := rate * 5
			result := runSippLoad(ctx, t, "vq_highrate_uas.xml", "vq_highrate_uac.xml",
				callCount, rate, vqHighratePacketsPerCall, env)

			t.Logf("VQ highrate rate=%d: actual=%.0f PPS, expected=%.0f PPS, loss=%.2f%%, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB",
				rate, result.ActualPPS, result.ExpectedPPS, result.LossRate*100,
				result.DrainTime, result.CPUAvg, result.CPUPeak, result.MemMaxMB)

			totalPackets := result.PacketsAfter - result.PacketsBefore
			maxErrors := totalPackets * 0.001
			require.LessOrEqual(t, result.ErrorCount, maxErrors,
				"error rate SLO: < 0.1%% of processed packets")
			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets")

			vqReports := getMetric(t, env.endpoint, "sip_exporter_vq_reports_total")
			expectedReports := float64(callCount * 2)
			t.Logf("vq_reports_total = %.0f (want %.0f, loopback x2)", vqReports, expectedReports)
			require.Equal(t, expectedReports, vqReports)

			publishTotal := getMetric(t, env.endpoint, "sip_exporter_publish_total")
			t.Logf("publish_total = %.0f (want %.0f)", publishTotal, expectedReports)
			require.Equal(t, expectedReports, publishTotal)

			recordResult(t.Name(), map[string]MetricEntry{
				"actual_pps":  {Value: result.ActualPPS, Unit: "pps", Direction: dirHigherIsBetter},
				"loss_rate":   {Value: result.LossRate * 100, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_peak":    {Value: result.CPUPeak, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_avg":     {Value: result.CPUAvg, Unit: "%", Direction: dirLowerIsBetter},
				"mem_mb":      {Value: result.MemMaxMB, Unit: "MB", Direction: dirLowerIsBetter},
				"vq_reports":  {Value: vqReports, Unit: "count", Direction: dirHigherIsBetter},
			})
		})
	}
}

func TestLoad_FullCallWithVQReport(t *testing.T) {
	rates := []int{100, 500, 1000}
	for _, rate := range rates {
		t.Run(fmt.Sprintf("rate_%d", rate), func(t *testing.T) {
			env := newTestEnv(context.Background(), t)

			ctx, cancel := context.WithTimeout(context.Background(), subtestTimeout)
			defer cancel()

			callCount := rate * 5
			result := runSippLoad(ctx, t, "fullcall_vq_uas.xml", "fullcall_vq_uac.xml",
				callCount, rate, fullCallVQPacketsPerCall, env)

			t.Logf("Full call+VQ rate=%d: actual=%.0f PPS, expected=%.0f PPS, loss=%.2f%%, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB",
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

			vqReports := getMetric(t, env.endpoint, "sip_exporter_vq_reports_total")
			expectedVQReports := float64(callCount * 2)
			t.Logf("vq_reports_total = %.0f (want %.0f, loopback x2)", vqReports, expectedVQReports)
			require.Equal(t, expectedVQReports, vqReports)

			inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
			expectedInvites := float64(callCount * 2)
			t.Logf("invite_total = %.0f (want %.0f)", inviteTotal, expectedInvites)
			require.Equal(t, expectedInvites, inviteTotal)

			sessions := getMetric(t, env.endpoint, "sip_exporter_sessions")
			t.Logf("sessions (should be 0 after all calls terminated) = %.0f", sessions)
			require.Eventually(t, func() bool {
				return getMetric(t, env.endpoint, "sip_exporter_sessions") == 0
			}, 5*time.Second, 300*time.Millisecond, "sessions should reach 0")

			recordResult(t.Name(), map[string]MetricEntry{
				"actual_pps":  {Value: result.ActualPPS, Unit: "pps", Direction: dirHigherIsBetter},
				"loss_rate":   {Value: result.LossRate * 100, Unit: "%", Direction: dirLowerIsBetter},
				"ser":         {Value: ser, Unit: "%", Direction: dirHigherIsBetter},
				"cpu_peak":    {Value: result.CPUPeak, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_avg":     {Value: result.CPUAvg, Unit: "%", Direction: dirLowerIsBetter},
				"mem_mb":      {Value: result.MemMaxMB, Unit: "MB", Direction: dirLowerIsBetter},
				"vq_reports":  {Value: vqReports, Unit: "count", Direction: dirHigherIsBetter},
			})
		})
	}
}
