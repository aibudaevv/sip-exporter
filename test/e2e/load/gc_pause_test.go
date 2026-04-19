//go:build e2e

package load

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var gcLineRe = regexp.MustCompile(`gc \d+ @[\d.]+s.*?([\d.]+)\+[\d.]+\+([\d.]+) ms clock`)

func parseGCPauses(logs string) []float64 {
	var pauses []float64
	for _, line := range strings.Split(logs, "\n") {
		matches := gcLineRe.FindStringSubmatch(line)
		if len(matches) == 3 {
			sweepStop, markStop := matches[1], matches[2]
			sweep, err1 := strconv.ParseFloat(sweepStop, 64)
			mark, err2 := strconv.ParseFloat(markStop, 64)
			if err1 == nil && err2 == nil {
				pauses = append(pauses, sweep+mark)
			}
		}
	}
	return pauses
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := p / 100.0 * float64(len(sorted)-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

func TestBenchmark_GCPauseDuration(t *testing.T) {
	t.Setenv("SIP_EXPORTER_E2E_GODEBUG", "gctrace=1")

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

	waitForContainerExit(ctx, t, uasContainer)
	waitForMetricStable(t, env.endpoint)

	logsReader, err := env.exporterContainer.Logs(context.Background())
	require.NoError(t, err)
	defer logsReader.Close()

	logBytes, err := io.ReadAll(logsReader)
	require.NoError(t, err)

	pauses := parseGCPauses(string(logBytes))
	require.NotEmpty(t, pauses, "expected GC trace output in container logs")

	sort.Float64s(pauses)

	minPause := pauses[0]
	maxPause := pauses[len(pauses)-1]
	var sum float64
	for _, p := range pauses {
		sum += p
	}
	avgPause := sum / float64(len(pauses))
	p95 := percentile(pauses, 95)

	t.Logf("=== GC Pause Summary (2000 CPS, %d GC cycles) ===", len(pauses))
	t.Logf("Min: %.3f ms", minPause)
	t.Logf("Avg: %.3f ms", avgPause)
	t.Logf("P95: %.3f ms", p95)
	t.Logf("Max: %.3f ms", maxPause)

	require.Less(t, maxPause, 50.0, "max STW pause SLO: < 50ms")

	recordResult(t.Name(), map[string]MetricEntry{
		"avg_ms": {Value: avgPause, Unit: "ms", Direction: dirLowerIsBetter},
		"p95_ms": {Value: p95, Unit: "ms", Direction: dirLowerIsBetter},
		"max_ms": {Value: maxPause, Unit: "ms", Direction: dirLowerIsBetter},
		"min_ms": {Value: minPause, Unit: "ms", Direction: dirLowerIsBetter},
	})
}
