//go:build e2e

package load

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type (
	scrapeObservation struct {
		StartedAt  time.Time
		Duration   time.Duration
		StatusCode int
		BodyBytes  int64
		Err        string
	}

	ScrapeSummary struct {
		Count int
		P50MS float64
		P95MS float64
		P99MS float64
	}
)

func scrapeOnce(ctx context.Context, client *http.Client, endpoint string) scrapeObservation {
	observation := scrapeObservation{StartedAt: time.Now()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		observation.Err = err.Error()
		return observation
	}
	resp, err := client.Do(req)
	if err != nil {
		observation.Duration = time.Since(observation.StartedAt)
		observation.Err = err.Error()
		return observation
	}
	observation.StatusCode = resp.StatusCode
	observation.BodyBytes, err = io.Copy(io.Discard, resp.Body)
	closeErr := resp.Body.Close()
	observation.Duration = time.Since(observation.StartedAt)
	switch {
	case err != nil:
		observation.Err = err.Error()
	case closeErr != nil:
		observation.Err = closeErr.Error()
	case resp.StatusCode != http.StatusOK:
		observation.Err = fmt.Sprintf("status %d", resp.StatusCode)
	}
	return observation
}

func summarizeScrapes(
	observations []scrapeObservation,
	start, end time.Time,
) (ScrapeSummary, error) {
	selected, err := phaseSamples(observations, func(observation scrapeObservation) time.Time {
		return observation.StartedAt
	}, start, end)
	if err != nil {
		return ScrapeSummary{}, err
	}
	if len(selected) == 0 {
		return ScrapeSummary{}, fmt.Errorf("measurement has no scrape observations")
	}
	durations := make([]float64, len(selected))
	for i, observation := range selected {
		if observation.Err != "" || observation.StatusCode != http.StatusOK || observation.BodyBytes <= 0 {
			return ScrapeSummary{}, fmt.Errorf("incomplete scrape observation")
		}
		durations[i] = float64(observation.Duration) / float64(time.Millisecond)
	}
	p50, err := percentile(durations, 50)
	if err != nil {
		return ScrapeSummary{}, err
	}
	p95, err := percentile(durations, 95)
	if err != nil {
		return ScrapeSummary{}, err
	}
	p99, err := percentile(durations, 99)
	if err != nil {
		return ScrapeSummary{}, err
	}
	return ScrapeSummary{Count: len(selected), P50MS: p50, P95MS: p95, P99MS: p99}, nil
}

func validateScrapeGates(summary ScrapeSummary) error {
	if summary.Count <= 0 || !finiteFloats(summary.P50MS, summary.P95MS, summary.P99MS) {
		return fmt.Errorf("invalid scrape summary")
	}
	if summary.P95MS >= 100 {
		return fmt.Errorf("scrape p95 %.3f ms is not below 100 ms", summary.P95MS)
	}
	if summary.P99MS >= 200 {
		return fmt.Errorf("scrape p99 %.3f ms is not below 200 ms", summary.P99MS)
	}
	return nil
}

func TestBenchmarkScrapeLatencyUnderLoad(t *testing.T) {
	beginScenario(t)
	env := newTestEnv(t.Context(), t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	measurement, err := newSteadyMeasurement(ctx, env)
	require.NoError(t, err)
	recordMetricsSnapshot(t, "metrics-before.prom", env.endpoint)
	protocolsBefore := readProtocolCounters(t, env.endpoint)
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	callCount := 10000
	rate := 2000
	expectedTotal := float64(callCount) * fullCallPacketsPerCall

	uasPath := absScenarioPath(t, "call_highrate_uas.xml")
	sippVol := filepath.Dir(uasPath)
	uasContainer := startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/call_highrate_uas.xml", "-i", "127.0.0.1", "-p", env.sippPort,
			"-m", strconv.Itoa(callCount), "-nr", "-nostdin"},
		sippVol, "", false,
	)
	waitForSIPpUDPReady(ctx, t, uasContainer, env.sippPort)

	uacPath := absScenarioPath(t, "call_highrate_uac.xml")
	sippVol = filepath.Dir(uacPath)
	measureStart := time.Now()
	require.NoError(t, measurement.Begin(ctx, measureStart))
	uacContainer := startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/call_highrate_uac.xml", "-i", "127.0.0.1", "-p", env.sippClientPort,
			"-m", strconv.Itoa(callCount), "-r", strconv.Itoa(rate), "-nr",
			"127.0.0.1:" + env.sippPort},
		sippVol, "generator", false,
	)

	const scrapeInterval = 100 * time.Millisecond
	client := &http.Client{Timeout: 5 * time.Second}
	observations := make([]scrapeObservation, 0, 50)
	for {
		state, stateErr := uacContainer.State(ctx)
		require.NoError(t, stateErr)
		if !state.Running {
			break
		}
		observations = append(observations, scrapeOnce(ctx, client, env.endpoint+"/metrics"))
		timer := time.NewTimer(scrapeInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			require.NoError(t, ctx.Err())
		case <-timer.C:
		}
	}
	measureEnd := time.Now()
	resources := finishSteadyMeasurement(ctx, t, measurement, measureEnd)
	uacContainer.recordEvidence(ctx, t, measureEnd)
	evidenceAt := time.Now()
	scrapes, err := summarizeScrapes(observations, measureStart, measureEnd)
	require.NoError(t, err)
	require.NoError(t, validateScrapeGates(scrapes))
	waitForContainerExit(ctx, t, uasContainer)
	uasExitAt := time.Now()
	waitForExactSIPCapture(ctx, t, env.endpoint, protocolsBefore.SIPPackets, expectedTotal)
	drainAt := time.Now()
	require.NoError(t, validatePostPhaseOrdering(measureEnd, evidenceAt, uasExitAt, drainAt))
	recordMetricsSnapshot(t, "metrics-after.prom", env.endpoint)
	protocols := readProtocolCounters(t, env.endpoint).delta(protocolsBefore)
	capture := newCaptureResult(expectedTotal, protocols.SIPPackets)
	require.NoError(t, capture.ValidateExact())
	result := loadResult{
		Capture: capture, Protocols: protocols, Resources: resources,
		ErrorCount: getMetric(t, env.endpoint, "sip_exporter_system_error_total") - errorsBefore,
	}
	recordLoadResultEvidence(t, result)

	t.Logf("Scrapes at %d CPS: count=%d p50=%.2fms p95=%.2fms p99=%.2fms",
		rate, scrapes.Count, scrapes.P50MS, scrapes.P95MS, scrapes.P99MS)
	metrics := resourceMetricEntries(resources)
	metrics["scrape_p50_ms"] = MetricEntry{Value: scrapes.P50MS, Unit: "ms", Direction: dirLowerIsBetter}
	metrics["scrape_p95_ms"] = MetricEntry{Value: scrapes.P95MS, Unit: "ms", Direction: dirLowerIsBetter}
	metrics["scrape_p99_ms"] = MetricEntry{Value: scrapes.P99MS, Unit: "ms", Direction: dirLowerIsBetter}
	recordResult(t, metrics)
}
