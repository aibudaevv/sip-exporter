//go:build e2e

package load

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReleaseVQMixed(t *testing.T) {
	profile := releaseVQMixedProfile()
	beginScenario(t)
	env := newTestEnvWithLimits(t.Context(), t, profile.Limits)

	result, generators := runVQMixedLoad(t.Context(), t, env)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_vq_reports_total"))
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_vq_nlr_percent_count"))
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_vq_mos_lq_count"))
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_vq_rlq_count"))
	require.NotEmpty(t, metricSamplesWithLabels(
		readMetricSamples(t, env.endpoint, "sip_exporter_parse_errors_total"), map[string]string{"type": "vq"},
	))

	business := map[string]float64{
		"vq_reports":      getMetric(t, env.endpoint, "sip_exporter_vq_reports_total"),
		"vq_nlr_count":    getMetric(t, env.endpoint, "sip_exporter_vq_nlr_percent_count"),
		"vq_mos_lq_count": getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_count"),
		"vq_rlq_count":    getMetric(t, env.endpoint, "sip_exporter_vq_rlq_count"),
		"vq_parse_errors": getMetricWithLabel(t, env.endpoint, "sip_exporter_parse_errors_total", `type="vq"`),
	}
	require.Equal(t, 10000.0, metricSumOrZero(t, env.endpoint, "sip_exporter_parse_errors_total"))
	require.Equal(t, 20000.0, result.Protocols.VQReports)
	require.NoError(t, validateVQMixedGeneratorOverlap(generators))
	require.NoError(t, validateReleaseRow(
		releaseRowSpec{ExpectedSystemErrors: 10000},
		releaseVQMixedRowFromLoad(profile, result, generators, business),
	))

	recordReleaseResult(t, result, business, nil)
}

func runVQMixedLoad(ctx context.Context, t *testing.T, env *testEnv) (loadResult, [vqMixedGeneratorCount]GeneratorResult) {
	t.Helper()
	profile := releaseVQMixedProfile()
	measurement, err := newSteadyMeasurement(ctx, env)
	require.NoError(t, err)

	recordMetricsSnapshot(t, "metrics-before.prom", env.endpoint)
	protocolsBefore := readProtocolCounters(t, env.endpoint)
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")
	paths := [vqMixedGeneratorCount]string{
		"vq_flood_uac.xml",
		"vq_partial_flood_uac.xml",
		"vq_malformed_flood_uac.xml",
	}
	prefixes := [vqMixedGeneratorCount]string{
		"generator-full",
		"generator-partial",
		"generator-malformed",
	}
	ports := append([]string{env.sippClientPort}, allocatePortsN(vqMixedGeneratorCount-1)...)
	phases := PhaseTimestamps{WarmupStart: time.Now(), Ready: time.Now()}
	var containers [vqMixedGeneratorCount]*startedSippContainer
	for i, path := range paths {
		containers[i] = prepareSippContainer(ctx, t,
			[]string{"-sf", "/scenarios/" + path, "-i", "127.0.0.1", "-p", ports[i],
				"-m", strconv.Itoa(vqMixedCallsPerGenerator), "-r", "10", "-rp", "30",
				"-cid_str", nextSippCallIDFormat(), "-nr", "127.0.0.1:" + env.sippPort},
			filepath.Dir(absScenarioPath(t, path)), prefixes[i],
		)
	}
	require.NoError(t, measurement.Begin(ctx, time.Now()))
	startPreparedSippContainers(ctx, t, containers[:]...)

	var generators [vqMixedGeneratorCount]GeneratorResult
	var measureEnd time.Time
	for i, container := range containers {
		waitForContainerExit(ctx, t, container)
		endedAt := time.Now()
		measureEnd = laterTime(measureEnd, endedAt)
		generatorPhases := phases
		generatorPhases.MeasureStart = container.started
		generatorPhases.MeasureEnd = endedAt
		generatorPhases.DrainEnd = endedAt
		generators[i], err = container.readGeneratorEvidence(ctx, t, generatorPhases)
		require.NoError(t, err)
	}
	resources := finishSteadyMeasurement(ctx, t, measurement, measureEnd)
	waitForExactSIPCapture(ctx, t, env.endpoint, protocolsBefore.SIPPackets,
		float64(profile.Workload.Calls)*profile.PacketsPerCall)
	waitForVQMixedMetrics(ctx, t, env.endpoint)
	drainedAt := time.Now()
	for i := range generators {
		generators[i].Phases.DrainEnd = drainedAt
		require.NoError(t, generators[i].Validate(WorkloadSpec{
			Calls: vqMixedCallsPerGenerator,
			Rate:  vqMixedGeneratorRate,
		}))
	}
	require.NoError(t, validatePostPhaseOrdering(measureEnd, drainedAt))

	protocols := readProtocolCounters(t, env.endpoint).delta(protocolsBefore)
	capture := newCaptureResult(float64(profile.Workload.Calls)*profile.PacketsPerCall, protocols.SIPPackets)
	require.NoError(t, capture.ValidateExact())
	result := loadResult{
		Generator:  aggregateVQMixedGenerators(generators),
		Capture:    capture,
		Protocols:  protocols,
		ErrorCount: getMetric(t, env.endpoint, "sip_exporter_system_error_total") - errorsBefore,
		Resources:  resources,
	}
	if activeRunRecorder != nil {
		require.NoError(t, activeRunRecorder.AttachGenerator(t.Name(), result.Generator))
	}
	recordMetricsSnapshot(t, "metrics-after.prom", env.endpoint)
	recordLoadResultEvidence(t, result)
	return result, generators
}

func waitForVQMixedMetrics(ctx context.Context, t *testing.T, endpoint string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return metricExists(t, endpoint, "sip_exporter_vq_reports_total") &&
			getMetric(t, endpoint, "sip_exporter_vq_reports_total") == 20000 &&
			metricExists(t, endpoint, "sip_exporter_vq_nlr_percent_count") &&
			getMetric(t, endpoint, "sip_exporter_vq_nlr_percent_count") == 10000 &&
			metricExists(t, endpoint, "sip_exporter_vq_mos_lq_count") &&
			getMetric(t, endpoint, "sip_exporter_vq_mos_lq_count") == 20000 &&
			metricExists(t, endpoint, "sip_exporter_vq_rlq_count") &&
			getMetric(t, endpoint, "sip_exporter_vq_rlq_count") == 20000 &&
			len(metricSamplesWithLabels(
				readMetricSamples(t, endpoint, "sip_exporter_parse_errors_total"), map[string]string{"type": "vq"},
			)) > 0 &&
			getMetricWithLabel(t, endpoint, "sip_exporter_parse_errors_total", `type="vq"`) == 10000 &&
			metricSumOrZero(t, endpoint, "sip_exporter_parse_errors_total") == 10000 &&
			metricExists(t, endpoint, "sip_exporter_system_error_total") &&
			getMetric(t, endpoint, "sip_exporter_system_error_total") == 10000
	}, contextTimeout(t, ctx), 25*time.Millisecond, "VQ mixed metrics did not reach exact totals")
}

func aggregateVQMixedGenerators(generators [vqMixedGeneratorCount]GeneratorResult) GeneratorResult {
	var aggregate GeneratorResult
	for _, generator := range generators {
		aggregate.ExitCode = max(aggregate.ExitCode, generator.ExitCode)
		aggregate.SuccessfulCalls += generator.SuccessfulCalls
		aggregate.FailedCalls += generator.FailedCalls
		aggregate.Retransmissions += generator.Retransmissions
		aggregate.ActualRate += generator.ActualRate
		aggregate.Phases.WarmupStart = earlierTime(aggregate.Phases.WarmupStart, generator.Phases.WarmupStart)
		aggregate.Phases.Ready = earlierTime(aggregate.Phases.Ready, generator.Phases.Ready)
		aggregate.Phases.MeasureStart = earlierTime(aggregate.Phases.MeasureStart, generator.Phases.MeasureStart)
		aggregate.Phases.MeasureEnd = laterTime(aggregate.Phases.MeasureEnd, generator.Phases.MeasureEnd)
		aggregate.Phases.DrainEnd = laterTime(aggregate.Phases.DrainEnd, generator.Phases.DrainEnd)
	}
	return aggregate
}
