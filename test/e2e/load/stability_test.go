//go:build e2e

package load

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBenchmark_MemoryStability(t *testing.T) {
	env := newTestEnv(context.Background(), t)

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	const testDurationSec = 120
	const rate = 500
	callCount := rate * testDurationSec

	stats, err := newStatsCollector(env.exporterContainer.GetContainerID())
	require.NoError(t, err)

	statsCtx, statsCancel := context.WithCancel(ctx)
	stats.start(statsCtx, env.exporterContainer.GetContainerID())

	uasPath := absScenarioPath(t, "call_highrate_uas.xml")
	sippVol := uasPath[:len(uasPath)-len("/call_highrate_uas.xml")]
	uasContainer := startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/call_highrate_uas.xml", "-i", "127.0.0.1", "-p", env.sippPort,
			"-m", fmt.Sprintf("%d", callCount), "-nostdin"},
		sippVol, false,
	)

	time.Sleep(500 * time.Millisecond)

	uacPath := absScenarioPath(t, "call_highrate_uac.xml")
	sippVol = uacPath[:len(uacPath)-len("/call_highrate_uac.xml")]
	startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/call_highrate_uac.xml", "-i", "127.0.0.1", "-p", env.sippClientPort,
			"-m", fmt.Sprintf("%d", callCount), "-r", fmt.Sprintf("%d", rate),
			"127.0.0.1:" + env.sippPort},
		sippVol, true,
	)

	sampleTicker := time.NewTicker(30 * time.Second)
	defer sampleTicker.Stop()

	sampleCount := 0
	sampleDone := make(chan struct{})
	go func() {
		defer close(sampleDone)
		for {
			select {
			case <-sampleTicker.C:
				sampleCount++
				memMB := getSingleMemSample(t, env.exporterContainer.GetContainerID())
				packets := getMetric(t, env.endpoint, "sip_exporter_packets_total")
				t.Logf("Sample %d (%.0fs): mem=%.1fMB, packets=%.0f",
					sampleCount, time.Since(time.Now()).Seconds(), memMB, packets)
			case <-ctx.Done():
				return
			}
		}
	}()

	waitForContainerExit(ctx, t, uasContainer)
	waitForMetricStable(t, env.endpoint)

	statsCancel()
	cpuAvg, cpuPeak, _ := stats.stop()

	sampleTicker.Stop()
	cancel()
	<-sampleDone

	packetsTotal := getMetric(t, env.endpoint, "sip_exporter_packets_total")

	stats.mu.Lock()
	memSamples := make([]float64, len(stats.memSamples))
	copy(memSamples, stats.memSamples)
	stats.mu.Unlock()

	var memMin, memMax, memFirst, memLast float64
	if len(memSamples) > 0 {
		memFirst = memSamples[0]
		memLast = memSamples[len(memSamples)-1]
		memMin = memSamples[0]
		memMax = memSamples[0]
		for _, m := range memSamples {
			if m < memMin {
				memMin = m
			}
			if m > memMax {
				memMax = m
			}
		}
	}

	durationMin := testDurationSec / 60.0
	growthRate := 0.0
	if durationMin > 0 && len(memSamples) > 1 {
		growthRate = (memLast - memFirst) / durationMin
	}

	t.Logf("=== Memory Stability Summary (%d min at %d CPS) ===", int(durationMin), rate)
	t.Logf("Duration: %d min", int(durationMin))
	t.Logf("Packets: %.0f", packetsTotal)
	t.Logf("CPU: avg=%.2f%% peak=%.2f%%", cpuAvg, cpuPeak)
	t.Logf("Memory min: %.1f MB", memMin)
	t.Logf("Memory max: %.1f MB", memMax)
	t.Logf("Memory first sample: %.1f MB", memFirst)
	t.Logf("Memory last sample: %.1f MB", memLast)
	t.Logf("Memory growth rate: %.2f MB/min", growthRate)
	t.Logf("Memory samples: %d", len(memSamples))

	require.Greater(t, packetsTotal, float64(0), "should have processed packets")

	if growthRate > 1.0 {
		t.Logf("WARNING: memory growth rate %.2f MB/min exceeds 1 MB/min threshold", growthRate)
	}
}
