//go:build e2e

package load

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// gcLineRe matches Go runtime gctrace lines: "gc N @Ts ... A+B+C ms clock".
// A = sweep-termination STW (start of GC), B = concurrent mark (NOT STW),
// C = mark-termination STW (end of GC). Groups 1+2 capture A and C;
// their sum is the total stop-the-world duration per GC cycle.
var gcLineRe = regexp.MustCompile(`gc \d+ @([\d.]+)s.*?([\d.]+)\+[\d.]+\+([\d.]+) ms clock`)
var gcFrameRe = regexp.MustCompile(`^gc \d+ @`)
var gcTimestampRe = regexp.MustCompile(`^gc \d+ @(\S+)s(?:\s|$)`)

func parseGCPauseSamples(
	logs string, containerStart, start, end time.Time,
) ([]gcPauseSample, error) {
	var samples []gcPauseSample
	for _, rawLine := range strings.Split(logs, "\n") {
		line := strings.TrimSpace(rawLine)
		if !gcFrameRe.MatchString(line) {
			continue
		}
		timestampMatch := gcTimestampRe.FindStringSubmatch(line)
		if len(timestampMatch) != 2 {
			return nil, fmt.Errorf("parse gctrace timestamp from %q", line)
		}
		uptimeSeconds, err := strconv.ParseFloat(timestampMatch[1], 64)
		if err != nil || math.IsNaN(uptimeSeconds) || math.IsInf(uptimeSeconds, 0) || uptimeSeconds < 0 {
			return nil, fmt.Errorf("parse gctrace timestamp %q", timestampMatch[1])
		}
		at := containerStart.Add(time.Duration(uptimeSeconds * float64(time.Second)))
		if at.Before(start) || !at.Before(end) {
			continue
		}
		matches := gcLineRe.FindStringSubmatch(line)
		if len(matches) != 4 {
			return nil, fmt.Errorf("parse in-phase gctrace frame %q", line)
		}
		sweep, sweepErr := strconv.ParseFloat(matches[2], 64)
		mark, markErr := strconv.ParseFloat(matches[3], 64)
		if sweepErr != nil || markErr != nil || !finiteFloats(sweep, mark) || sweep < 0 || mark < 0 {
			return nil, fmt.Errorf("parse in-phase gctrace pauses %q", line)
		}
		samples = append(samples, gcPauseSample{
			At:         at,
			DurationMS: sweep + mark,
		})
	}
	return samples, nil
}

func TestBenchmarkGCPauseDuration(t *testing.T) {
	beginScenario(t)
	env := newTestEnv(t.Context(), t)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	callCount := 10000
	rate := 2000
	result := runSippLoad(ctx, t, "call_highrate_uas.xml", "call_highrate_uac.xml",
		callCount, rate, fullCallPacketsPerCall, env)

	t.Logf("GC max STW at %d CPS: %.3f ms", rate, result.Resources.GCMaxSTWMS)
	metrics := resourceMetricEntries(result.Resources)
	metrics["actual_pps"] = MetricEntry{
		Value: result.ActualPPS, Unit: "pps", Direction: dirHigherIsBetter,
	}
	recordResult(t, metrics)
}
