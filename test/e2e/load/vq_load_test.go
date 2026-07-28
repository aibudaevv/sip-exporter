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
	vqFloodPacketsPerCall    = 1.0
	vqHighratePacketsPerCall = 2.0
	fullCallVQPacketsPerCall = 9.0
)

func TestLoadVQScenarios(t *testing.T) {
	tests := []struct {
		name           string
		uasScenario    string
		uacScenario    string
		packetsPerCall float64
		rates          []int
		extraChecks    func(t *testing.T, env *testEnv, callCount int) map[string]MetricEntry
	}{
		{
			name:           "VQReportFlood",
			uasScenario:    "",
			uacScenario:    "vq_flood_uac.xml",
			packetsPerCall: vqFloodPacketsPerCall,
			rates:          []int{100, 500, 1000, 2000},
			extraChecks: func(t *testing.T, env *testEnv, callCount int) map[string]MetricEntry {
				moslqCount := getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_count")
				t.Logf("vq_mos_lq_count = %.0f (want %d)", moslqCount, callCount)
				require.Equal(t, float64(callCount), moslqCount)
				return nil
			},
		},
		{
			name:           "VQHighRateWithResponse",
			uasScenario:    "vq_highrate_uas.xml",
			uacScenario:    "vq_highrate_uac.xml",
			packetsPerCall: vqHighratePacketsPerCall,
			rates:          []int{100, 500, 1000},
			extraChecks: func(t *testing.T, env *testEnv, callCount int) map[string]MetricEntry {
				publishTotal := getMetric(t, env.endpoint, "sip_exporter_publish_total")
				t.Logf("publish_total = %.0f (want %d)", publishTotal, callCount)
				require.Equal(t, float64(callCount), publishTotal)
				return nil
			},
		},
		{
			name:           "FullCallWithVQReport",
			uasScenario:    "fullcall_vq_uas.xml",
			uacScenario:    "fullcall_vq_uac.xml",
			packetsPerCall: fullCallVQPacketsPerCall,
			rates:          []int{100, 500, 1000},
			extraChecks: func(t *testing.T, env *testEnv, callCount int) map[string]MetricEntry {
				ser := getMetric(t, env.endpoint, "sip_exporter_ser")
				require.GreaterOrEqual(t, ser, 99.0,
					"SER SLO: >= 99%% (got %.2f%%)", ser)

				inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
				t.Logf("invite_total = %.0f (want %d)", inviteTotal, callCount)
				require.Equal(t, float64(callCount), inviteTotal)

				require.Eventually(t, func() bool {
					return metricExists(t, env.endpoint, "sip_exporter_sessions") &&
						getMetric(t, env.endpoint, "sip_exporter_sessions") == 0
				}, 5*time.Second, 300*time.Millisecond, "sessions should reach 0")

				return map[string]MetricEntry{
					"ser": {Value: ser, Unit: "%", Direction: dirHigherIsBetter},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, rate := range tt.rates {
				t.Run(fmt.Sprintf("rate_%d", rate), func(t *testing.T) {
					env := newTestEnv(t.Context(), t)

					ctx, cancel := context.WithTimeout(t.Context(), subtestTimeout)
					defer cancel()

					callCount := rate * 5
					result := runSippLoad(ctx, t, tt.uasScenario, tt.uacScenario,
						callCount, rate, tt.packetsPerCall, env)

					t.Logf("%s rate=%d: actual=%.0f PPS, expected=%.0f PPS, loss=%.2f%%, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB",
						tt.name, rate, result.ActualPPS, result.ExpectedPPS, result.LossRate*100,
						result.DrainTime, result.CPUAvg, result.CPUPeak, result.MemMaxMB)

					totalPackets := result.PacketsAfter - result.PacketsBefore
					maxErrors := totalPackets * 0.001
					require.LessOrEqual(t, result.ErrorCount, maxErrors,
						"error rate SLO: < 0.1%% of processed packets")
					require.Greater(t, result.PacketsAfter, result.PacketsBefore,
						"exporter should have processed packets")

					vqReports := getMetric(t, env.endpoint, "sip_exporter_vq_reports_total")
					expectedReports := float64(callCount)
					t.Logf("vq_reports_total = %.0f (want %.0f)", vqReports, expectedReports)
					require.Equal(t, expectedReports, vqReports)

					extras := tt.extraChecks(t, env, callCount)

					resultMetrics := map[string]MetricEntry{
						"actual_pps": {Value: result.ActualPPS, Unit: "pps", Direction: dirHigherIsBetter},
						"loss_rate":  {Value: result.LossRate * 100, Unit: "%", Direction: dirLowerIsBetter},
						"cpu_peak":   {Value: result.CPUPeak, Unit: "%", Direction: dirLowerIsBetter},
						"cpu_avg":    {Value: result.CPUAvg, Unit: "%", Direction: dirLowerIsBetter},
						"mem_mb":     {Value: result.MemMaxMB, Unit: "MB", Direction: dirLowerIsBetter},
						"vq_reports": {Value: vqReports, Unit: "count", Direction: dirHigherIsBetter},
					}
					for k, v := range extras {
						resultMetrics[k] = v
					}
					recordResult(t.Name(), resultMetrics)
				})
			}
		})
	}
}
