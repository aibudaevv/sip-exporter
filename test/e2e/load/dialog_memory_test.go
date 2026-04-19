//go:build e2e

package load

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func getSingleMemSample(t *testing.T, containerID string) float64 {
	t.Helper()

	stats, err := newStatsCollector(containerID)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats.start(ctx, containerID)
	time.Sleep(2 * time.Second)
	cancel()
	<-stats.done

	_, _, memMaxMB := stats.stop()
	return memMaxMB
}

func waitForSessions(t *testing.T, endpoint string, target float64) {
	t.Helper()
	require.Eventually(t, func() bool {
		sessions := getMetric(t, endpoint, "sip_exporter_sessions")
		return sessions >= target*0.8
	}, 120*time.Second, 500*time.Millisecond, "sessions did not reach %.0f", target)
}

func TestBenchmark_MemoryPerDialog(t *testing.T) {
	limits := []int{0, 100, 500, 1000, 2000, 5000}
	type dialogMeasurement struct {
		dialogs int
		memMB   float64
	}

	measurements := make([]dialogMeasurement, 0, len(limits))

	for _, limit := range limits {
		t.Run(fmt.Sprintf("dialogs_%d", limit), func(t *testing.T) {
			env := newTestEnv(context.Background(), t)

			var sessions float64
			if limit > 0 {
				callCount := limit * 3
				rate := limit / 10
				if rate < 50 {
					rate = 50
				}

				ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
				defer cancel()

				uasPath := absScenarioPath(t, "concurrent_uas.xml")
				sippVol := uasPath[:len(uasPath)-len("/concurrent_uas.xml")]
				uasContainer := startSippContainer(ctx, t,
					[]string{"-sf", "/scenarios/concurrent_uas.xml", "-i", "127.0.0.1", "-p", env.sippPort,
						"-m", fmt.Sprintf("%d", callCount), "-nostdin"},
					sippVol, false,
				)

				time.Sleep(500 * time.Millisecond)

				uacPath := absScenarioPath(t, "concurrent_uac.xml")
				sippVol = uacPath[:len(uacPath)-len("/concurrent_uac.xml")]
				startSippContainer(ctx, t,
					[]string{"-sf", "/scenarios/concurrent_uac.xml", "-i", "127.0.0.1", "-p", env.sippClientPort,
						"-m", fmt.Sprintf("%d", callCount), "-r", fmt.Sprintf("%d", rate),
						"-l", fmt.Sprintf("%d", limit),
						"127.0.0.1:" + env.sippPort},
					sippVol, false,
				)

				waitForSessions(t, env.endpoint, float64(limit))
				sessions = getMetric(t, env.endpoint, "sip_exporter_sessions")
				t.Logf("Sessions reached: %.0f (target: %d)", sessions, limit)

				memMB := getSingleMemSample(t, env.exporterContainer.GetContainerID())

				t.Logf("Dialogs: limit=%d, actual_sessions=%.0f, mem=%.1fMB",
					limit, sessions, memMB)

				measurements = append(measurements, dialogMeasurement{
					dialogs: int(sessions),
					memMB:   memMB,
				})

				recordResult(t.Name(), map[string]MetricEntry{
					"sessions": {Value: sessions, Unit: "count", Direction: dirHigherIsBetter},
					"mem_mb":   {Value: memMB, Unit: "MB", Direction: dirLowerIsBetter},
				})

				waitForContainerExit(ctx, t, uasContainer)
			} else {
				memMB := getSingleMemSample(t, env.exporterContainer.GetContainerID())
				t.Logf("Baseline (no traffic): %.1f MB", memMB)
				measurements = append(measurements, dialogMeasurement{
					dialogs: 0,
					memMB:   memMB,
				})

				recordResult(t.Name(), map[string]MetricEntry{
					"mem_mb": {Value: memMB, Unit: "MB", Direction: dirLowerIsBetter},
				})
			}
		})
	}

	t.Run("summary", func(t *testing.T) {
		if len(measurements) < 2 {
			t.Fatal("need at least baseline + 1 measurement")
		}

		baseline := measurements[0].memMB
		t.Logf("=== Memory Per Dialog Summary ===")
		t.Logf("Baseline: %.1f MB (no dialogs)", baseline)
		t.Logf("%-10s %-12s %-12s %-12s", "Dialogs", "Total MB", "Delta MB", "Bytes/dialog")

		for _, m := range measurements[1:] {
			if m.dialogs == 0 {
				continue
			}
			delta := (m.memMB - baseline) * 1024 * 1024
			bytesPerDialog := delta / float64(m.dialogs)
			t.Logf("%-10d %-12.1f %-12.1f %-12.0f",
				m.dialogs, m.memMB, m.memMB-baseline, bytesPerDialog)
		}
	})
}
