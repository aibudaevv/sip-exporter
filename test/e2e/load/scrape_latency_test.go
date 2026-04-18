//go:build e2e

package load

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBenchmark_ScrapeLatencyUnderLoad(t *testing.T) {
	env := newTestEnv(context.Background(), t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	callCount := 10000
	rate := 2000

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

	type scrapeResult struct {
		duration time.Duration
		err      error
	}

	var (
		results []scrapeResult
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	const numScrapes = 50
	scrapeInterval := 100 * time.Millisecond

	client := &http.Client{Timeout: 5 * time.Second}

	time.Sleep(2 * time.Second)

	wg.Add(numScrapes)
	for i := 0; i < numScrapes; i++ {
		go func(idx int) {
			defer wg.Done()
			start := time.Now()
			resp, err := client.Get(env.endpoint + "/metrics")
			elapsed := time.Since(start)
			if err == nil {
				resp.Body.Close()
			}
			mu.Lock()
			results = append(results, scrapeResult{duration: elapsed, err: err})
			mu.Unlock()
		}(i)
		time.Sleep(scrapeInterval)
	}
	wg.Wait()

	var durations []float64
	var errors int
	for _, r := range results {
		if r.err != nil {
			errors++
			continue
		}
		durations = append(durations, float64(r.duration.Microseconds())/1000.0)
	}

	maxErrors := numScrapes / 20 //nolint:mnd // 5% error tolerance
	require.LessOrEqual(t, errors, maxErrors,
		"scrape error rate SLO: < 5%% (%d/%d failed)", errors, numScrapes)
	require.NotEmpty(t, durations, "should have successful scrapes for latency measurement")

	sort.Float64s(durations)

	minMs := durations[0]
	maxMs := durations[len(durations)-1]
	var sum float64
	for _, d := range durations {
		sum += d
	}
	avgMs := sum / float64(len(durations))
	p95 := percentile(durations, 95)

	t.Logf("=== Scrape Latency Summary (2000 CPS, %d scrapes) ===", numScrapes)
	t.Logf("Min: %.2f ms", minMs)
	t.Logf("Avg: %.2f ms", avgMs)
	t.Logf("P95: %.2f ms", p95)
	t.Logf("Max: %.2f ms", maxMs)

	require.Less(t, p95, 100.0, "P95 scrape latency SLO: < 100ms")

	recordResult(t.Name(), map[string]MetricEntry{
		"p95_ms": {Value: p95, Unit: "ms", Direction: dirLowerIsBetter},
		"avg_ms": {Value: avgMs, Unit: "ms", Direction: dirLowerIsBetter},
		"max_ms": {Value: maxMs, Unit: "ms", Direction: dirLowerIsBetter},
		"min_ms": {Value: minMs, Unit: "ms", Direction: dirLowerIsBetter},
	})

	waitForContainerExit(ctx, t, uasContainer)
}
