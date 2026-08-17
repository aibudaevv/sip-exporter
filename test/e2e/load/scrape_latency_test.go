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
	if summary.Count <= 0 || !finiteFloats(summary.P50MS, summary.P95MS, summary.P99MS) ||
		summary.P50MS < 0 || summary.P50MS > summary.P95MS || summary.P95MS > summary.P99MS {
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

func TestReleaseFullCallPeak(t *testing.T) {
	profile := releaseFullCallPeakProfile()
	beginScenario(t)
	env := newTestEnvWithLimits(t.Context(), t, profile.Limits)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	measurement, err := newSteadyMeasurement(ctx, env)
	require.NoError(t, err)
	recordMetricsSnapshot(t, "metrics-before.prom", env.endpoint)
	protocolsBefore := readProtocolCounters(t, env.endpoint)
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	expectedTotal := float64(profile.Workload.Calls) * profile.PacketsPerCall
	phases := PhaseTimestamps{WarmupStart: time.Now()}

	uasPath := absScenarioPath(t, "call_highrate_uas.xml")
	sippVol := filepath.Dir(uasPath)
	uasContainer := startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/call_highrate_uas.xml", "-i", "127.0.0.1", "-p", env.sippPort,
			"-m", strconv.Itoa(profile.Workload.Calls), "-nr", "-nostdin"},
		sippVol, "", false,
	)
	waitForSIPpUDPReady(ctx, t, uasContainer, env.sippPort)
	phases.Ready = time.Now()

	uacPath := absScenarioPath(t, "call_highrate_uac.xml")
	sippVol = filepath.Dir(uacPath)
	measureStart := time.Now()
	phases.MeasureStart = measureStart
	require.NoError(t, measurement.Begin(ctx, measureStart))
	uacContainer := startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/call_highrate_uac.xml", "-i", "127.0.0.1", "-p", env.sippClientPort,
			"-m", strconv.Itoa(profile.Workload.Calls), "-r", strconv.Itoa(int(profile.Workload.Rate)), "-nr",
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
	phases.MeasureEnd = measureEnd
	resources := finishSteadyMeasurement(ctx, t, measurement, measureEnd)
	generator, generatorErr := uacContainer.readGeneratorEvidence(ctx, t, phases)
	require.NoError(t, generatorErr)
	evidenceAt := time.Now()
	scrapes, err := summarizeScrapes(observations, measureStart, measureEnd)
	require.NoError(t, err)
	waitForContainerExit(ctx, t, uasContainer)
	uasExitAt := time.Now()
	waitForExactSIPCapture(ctx, t, env.endpoint, protocolsBefore.SIPPackets, expectedTotal)
	drainAt := time.Now()
	require.NoError(t, validatePostPhaseOrdering(measureEnd, evidenceAt, uasExitAt, drainAt))
	generator.Phases.DrainEnd = drainAt
	require.NoError(t, generator.Validate(profile.Workload))
	recordMetricsSnapshot(t, "metrics-after.prom", env.endpoint)
	protocols := readProtocolCounters(t, env.endpoint).delta(protocolsBefore)
	capture := newCaptureResult(expectedTotal, protocols.SIPPackets)
	result := loadResult{
		Generator: generator, Capture: capture, Protocols: protocols, Resources: resources,
		ErrorCount: getMetric(t, env.endpoint, "sip_exporter_system_error_total") - errorsBefore,
	}
	recordLoadResultEvidence(t, result)
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_invite_total"))
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_ser"))
	invites := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	ser := getMetric(t, env.endpoint, "sip_exporter_ser")
	require.NoError(t, validateReleaseRow(releaseRowSpec{RequireScrapes: profile.RequireScrapes},
		releaseRowFromLoad(profile, result, map[string]float64{"invites": invites, "ser": ser}, &scrapes)))
	recordReleaseResult(t, result, map[string]float64{"invites": invites, "ser": ser}, &scrapes)
}
