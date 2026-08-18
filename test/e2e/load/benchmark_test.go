//go:build e2e

package load

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	floodPacketsPerCall       = 1.0
	fullCallPacketsPerCall    = 7.0
	subtestTimeout            = 20 * time.Second
	releaseDuration           = 30 * time.Second
	releaseSoakDuration       = 10 * time.Minute
	releaseSoakWarmupDuration = time.Minute
	releaseSoakRate           = 500
	nominalFullCallRate       = 1000
	peakFullCallRate          = 1800
	inviteFloodRate           = 5000
	releaseConcurrentDialogs  = 2000
	concurrentCreationRate    = 100
	carrierUARatePerType      = 900
	carrierUACallsPerType     = carrierUARatePerType * int(releaseDuration/time.Second)
	carrierUAStartSkewLimit   = time.Duration(float64(releaseDuration) * (1 - minOfferedRateRatio))
	multiNICCount             = 3
	multiNICRatePerInterface  = 500
	multiNICCallsPerInterface = multiNICRatePerInterface *
		int(releaseDuration/time.Second)
	multiNICStartSkewLimit   = 600 * time.Millisecond
	vqMixedGeneratorCount    = 3
	vqMixedCallsPerGenerator = 10000
	vqMixedGeneratorRate     = 1000.0 / vqMixedGeneratorCount
	vqMixedStartSkewLimit    = 600 * time.Millisecond
)

type releaseProfileSpec struct {
	Workload       WorkloadSpec
	PacketsPerCall float64
	Limits         WorkloadLimits
	RequireScrapes bool
	Business       map[string]float64
}

type soakWarmupEvidence struct {
	Generator  GeneratorResult
	Capture    CaptureResult
	Protocols  ProtocolCounters
	ErrorCount float64
}

func releaseFullCallNominalProfile() releaseProfileSpec {
	return releaseProfileSpec{
		Workload:       WorkloadSpec{Calls: nominalFullCallRate * int(releaseDuration/time.Second), Rate: nominalFullCallRate},
		PacketsPerCall: fullCallPacketsPerCall,
		Limits:         nominalLimits,
		Business:       map[string]float64{"invites": float64(nominalFullCallRate * int(releaseDuration/time.Second)), "ser": 100},
	}
}

func releaseFullCallPeakProfile() releaseProfileSpec {
	return releaseProfileSpec{
		Workload:       WorkloadSpec{Calls: peakFullCallRate * int(releaseDuration/time.Second), Rate: peakFullCallRate},
		PacketsPerCall: fullCallPacketsPerCall,
		Limits:         peakLimits,
		RequireScrapes: true,
		Business:       map[string]float64{"invites": float64(peakFullCallRate * int(releaseDuration/time.Second)), "ser": 100},
	}
}

func releaseSoakProfile() releaseProfileSpec {
	return releaseProfileSpec{
		Workload:       WorkloadSpec{Calls: releaseSoakRate * int(releaseSoakDuration/time.Second), Rate: releaseSoakRate},
		PacketsPerCall: fullCallPacketsPerCall,
		Limits:         nominalLimits,
		Business:       map[string]float64{"invites": float64(releaseSoakRate * int(releaseSoakDuration/time.Second)), "ser": 100},
	}
}

func releaseSoakWarmupProfile() releaseProfileSpec {
	return releaseProfileSpec{
		Workload: WorkloadSpec{
			Calls: releaseSoakRate * int(releaseSoakWarmupDuration/time.Second),
			Rate:  releaseSoakRate,
		},
		PacketsPerCall: fullCallPacketsPerCall,
	}
}

func validateReleaseSoakWarmup(profile releaseProfileSpec, evidence soakWarmupEvidence) error {
	if err := evidence.Generator.Validate(profile.Workload); err != nil {
		return fmt.Errorf("warmup generator: %w", err)
	}
	expected := float64(profile.Workload.Calls) * profile.PacketsPerCall
	if evidence.Capture.Expected != expected {
		return fmt.Errorf("warmup expected capture: got %.0f, want %.0f", evidence.Capture.Expected, expected)
	}
	if err := validateReleaseCapture(evidence.Capture); err != nil {
		return fmt.Errorf("warmup capture: %w", err)
	}
	if err := validateReleaseProtocolCounters(evidence.Protocols); err != nil {
		return fmt.Errorf("warmup protocols: %w", err)
	}
	wantProtocols := ProtocolCounters{SIPPackets: expected, SocketReceived: expected}
	if evidence.Protocols != wantProtocols {
		return fmt.Errorf("warmup protocols: got %+v, want %+v", evidence.Protocols, wantProtocols)
	}
	if evidence.ErrorCount != 0 {
		return fmt.Errorf("warmup system errors: %.0f", evidence.ErrorCount)
	}
	return nil
}

func releaseINVITEFloodProfile() releaseProfileSpec {
	return releaseProfileSpec{
		Workload:       WorkloadSpec{Calls: inviteFloodRate * int(releaseDuration/time.Second), Rate: inviteFloodRate},
		PacketsPerCall: floodPacketsPerCall,
		Limits:         peakLimits,
		Business:       map[string]float64{"invites": float64(inviteFloodRate * int(releaseDuration/time.Second))},
	}
}

func releaseConcurrentDialogsProfile() releaseProfileSpec {
	return releaseProfileSpec{
		Workload:       WorkloadSpec{Calls: releaseConcurrentDialogs, Rate: concurrentCreationRate},
		PacketsPerCall: fullCallPacketsPerCall,
		Limits:         peakLimits,
		Business:       map[string]float64{"invites": releaseConcurrentDialogs, "peak_sessions": releaseConcurrentDialogs},
	}
}

func releaseCarrierUAProfile() releaseProfileSpec {
	return releaseProfileSpec{
		Workload:       WorkloadSpec{Calls: carrierUACallsPerType * 2, Rate: carrierUARatePerType * 2},
		PacketsPerCall: fullCallPacketsPerCall,
		Limits:         peakLimits,
		Business: map[string]float64{
			"invites_total":                float64(carrierUACallsPerType * 2),
			"invites_loopback_yealink":     float64(carrierUACallsPerType),
			"invites_loopback_grandstream": float64(carrierUACallsPerType),
			"ser_loopback_yealink":         100,
			"ser_loopback_grandstream":     100,
			"unexpected_label_series":      0,
		},
	}
}

func releaseMultiNICProfile() releaseProfileSpec {
	return releaseProfileSpec{
		Workload:       WorkloadSpec{Calls: 45000, Rate: 1500},
		PacketsPerCall: floodPacketsPerCall,
		Limits:         peakLimits,
		Business: map[string]float64{
			"invites_iface_1": 15000, "invites_iface_2": 15000, "invites_iface_3": 15000,
			"cross_interface_series": 0, "unexpected_series": 0,
		},
	}
}

func releaseVQMixedProfile() releaseProfileSpec {
	return releaseProfileSpec{
		Workload: WorkloadSpec{
			Calls: vqMixedGeneratorCount * vqMixedCallsPerGenerator,
			Rate:  1000,
		},
		PacketsPerCall: vqFloodPacketsPerCall,
		Limits:         peakLimits,
		Business: map[string]float64{
			"vq_reports":      20000,
			"vq_nlr_count":    10000,
			"vq_mos_lq_count": 20000,
			"vq_rlq_count":    20000,
			"vq_parse_errors": 10000,
		},
	}
}

func releaseMultiNICRowFromLoad(
	profile releaseProfileSpec,
	result loadResult,
	generators []GeneratorResult,
	actualBusiness map[string]float64,
) releaseRowEvidence {
	evidence := releaseRowFromLoad(profile, result, actualBusiness, nil)
	generatorSpec := WorkloadSpec{Calls: multiNICCallsPerInterface, Rate: multiNICRatePerInterface}
	evidence.Generators = make([]releaseGeneratorEvidence, len(generators))
	for i, generator := range generators {
		evidence.Generators[i] = releaseGeneratorEvidence{Spec: generatorSpec, Result: generator}
	}
	return evidence
}

func releaseVQMixedRowFromLoad(
	profile releaseProfileSpec,
	result loadResult,
	generators [vqMixedGeneratorCount]GeneratorResult,
	actualBusiness map[string]float64,
) releaseRowEvidence {
	evidence := releaseRowFromLoad(profile, result, actualBusiness, nil)
	generatorSpec := WorkloadSpec{Calls: vqMixedCallsPerGenerator, Rate: vqMixedGeneratorRate}
	evidence.Generators = make([]releaseGeneratorEvidence, len(generators))
	for i, generator := range generators {
		evidence.Generators[i] = releaseGeneratorEvidence{Spec: generatorSpec, Result: generator}
	}
	return evidence
}

func validateMultiNICGeneratorOverlap(generators []GeneratorResult) error {
	if len(generators) == 0 {
		return fmt.Errorf("multi-NIC generators are missing")
	}
	earliestStart := generators[0].Phases.WarmupStart
	latestStart := earliestStart
	latestMeasureStart := generators[0].Phases.MeasureStart
	earliestEnd := generators[0].Phases.MeasureEnd
	for _, generator := range generators {
		if generator.Phases.WarmupStart.IsZero() || generator.Phases.MeasureStart.IsZero() ||
			generator.Phases.MeasureEnd.IsZero() {
			return fmt.Errorf("multi-NIC generator interval is missing")
		}
		earliestStart = earlierTime(earliestStart, generator.Phases.WarmupStart)
		latestStart = laterTime(latestStart, generator.Phases.WarmupStart)
		latestMeasureStart = laterTime(latestMeasureStart, generator.Phases.MeasureStart)
		earliestEnd = earlierTime(earliestEnd, generator.Phases.MeasureEnd)
	}
	if startSkew := latestStart.Sub(earliestStart); startSkew > multiNICStartSkewLimit {
		return fmt.Errorf("multi-NIC generator start skew %v exceeds %v",
			startSkew, multiNICStartSkewLimit)
	}
	if !latestMeasureStart.Before(earliestEnd) {
		return fmt.Errorf("multi-NIC generator intervals do not overlap")
	}
	return nil
}

func validateVQMixedGeneratorOverlap(generators [vqMixedGeneratorCount]GeneratorResult) error {
	earliestStart := generators[0].Phases.MeasureStart
	latestStart := earliestStart
	earliestEnd := generators[0].Phases.MeasureEnd
	for _, generator := range generators {
		if generator.Phases.MeasureStart.IsZero() || generator.Phases.MeasureEnd.IsZero() {
			return fmt.Errorf("VQ mixed generator interval is missing")
		}
		earliestStart = earlierTime(earliestStart, generator.Phases.MeasureStart)
		latestStart = laterTime(latestStart, generator.Phases.MeasureStart)
		earliestEnd = earlierTime(earliestEnd, generator.Phases.MeasureEnd)
	}
	if startSkew := latestStart.Sub(earliestStart); startSkew > vqMixedStartSkewLimit {
		return fmt.Errorf("VQ mixed generator start skew %v exceeds %v", startSkew, vqMixedStartSkewLimit)
	}
	if !latestStart.Before(earliestEnd) {
		return fmt.Errorf("VQ mixed generator intervals do not overlap")
	}
	return nil
}

func aggregateMultiNICGenerators(generators []GeneratorResult) GeneratorResult {
	var aggregate GeneratorResult
	for _, generator := range generators {
		aggregate.ExitCode = max(aggregate.ExitCode, generator.ExitCode)
		aggregate.SuccessfulCalls += generator.SuccessfulCalls
		aggregate.FailedCalls += generator.FailedCalls
		aggregate.Retransmissions += generator.Retransmissions
		aggregate.Phases.WarmupStart = earlierTime(
			aggregate.Phases.WarmupStart, generator.Phases.WarmupStart,
		)
		aggregate.Phases.Ready = earlierTime(aggregate.Phases.Ready, generator.Phases.Ready)
		aggregate.Phases.MeasureStart = earlierTime(
			aggregate.Phases.MeasureStart, generator.Phases.MeasureStart,
		)
		aggregate.Phases.MeasureEnd = laterTime(
			aggregate.Phases.MeasureEnd, generator.Phases.MeasureEnd,
		)
		aggregate.Phases.DrainEnd = laterTime(aggregate.Phases.DrainEnd, generator.Phases.DrainEnd)
	}
	measureDuration := aggregate.Phases.MeasureEnd.Sub(aggregate.Phases.MeasureStart)
	if measureDuration > 0 {
		aggregate.ActualRate = float64(aggregate.SuccessfulCalls) / measureDuration.Seconds()
	}
	return aggregate
}

func releaseCarrierUARowFromLoad(
	profile releaseProfileSpec,
	result loadResult,
	generators [2]GeneratorResult,
	actualBusiness map[string]float64,
) releaseRowEvidence {
	evidence := releaseRowFromLoad(profile, result, actualBusiness, nil)
	generatorSpec := WorkloadSpec{Calls: carrierUACallsPerType, Rate: carrierUARatePerType}
	evidence.Generators = []releaseGeneratorEvidence{
		{Spec: generatorSpec, Result: generators[0]},
		{Spec: generatorSpec, Result: generators[1]},
	}
	return evidence
}

func validateCarrierUAAggregateRate(
	profile releaseProfileSpec,
	generators [2]GeneratorResult,
) error {
	aggregate, err := carrierUAAggregateGenerator(generators)
	if err != nil {
		return err
	}
	return aggregate.Validate(profile.Workload)
}

func carrierUAAggregateGenerator(generators [2]GeneratorResult) (GeneratorResult, error) {
	startSkew := generators[0].Phases.MeasureStart.Sub(generators[1].Phases.MeasureStart)
	if startSkew < 0 {
		startSkew = -startSkew
	}
	if startSkew > carrierUAStartSkewLimit {
		return GeneratorResult{}, fmt.Errorf("carrier/UA generator start skew %v exceeds %v",
			startSkew, carrierUAStartSkewLimit)
	}
	latestStart := laterTime(generators[0].Phases.MeasureStart, generators[1].Phases.MeasureStart)
	earliestEnd := earlierTime(generators[0].Phases.MeasureEnd, generators[1].Phases.MeasureEnd)
	if !latestStart.Before(earliestEnd) {
		return GeneratorResult{}, fmt.Errorf("carrier/UA generator intervals do not overlap")
	}
	aggregate := generators[0]
	aggregate.ExitCode = max(aggregate.ExitCode, generators[1].ExitCode)
	aggregate.SuccessfulCalls += generators[1].SuccessfulCalls
	aggregate.FailedCalls += generators[1].FailedCalls
	aggregate.Retransmissions += generators[1].Retransmissions
	aggregate.ActualRate += generators[1].ActualRate
	aggregate.Phases.WarmupStart = earlierTime(
		aggregate.Phases.WarmupStart, generators[1].Phases.WarmupStart,
	)
	aggregate.Phases.Ready = earlierTime(aggregate.Phases.Ready, generators[1].Phases.Ready)
	aggregate.Phases.MeasureEnd = laterTime(
		aggregate.Phases.MeasureEnd, generators[1].Phases.MeasureEnd,
	)
	aggregate.Phases.DrainEnd = laterTime(aggregate.Phases.DrainEnd, generators[1].Phases.DrainEnd)
	return aggregate, nil
}

func releaseRowFromLoad(
	profile releaseProfileSpec,
	result loadResult,
	actualBusiness map[string]float64,
	scrapes *ScrapeSummary,
) releaseRowEvidence {
	business := make(map[string]releaseBusinessEvidence, len(profile.Business))
	for name, expected := range profile.Business {
		actual, ok := actualBusiness[name]
		if !ok {
			actual = math.NaN()
		}
		business[name] = releaseBusinessEvidence{Expected: expected, Actual: actual}
	}
	return releaseRowEvidence{
		Generators: []releaseGeneratorEvidence{{Spec: profile.Workload, Result: result.Generator}},
		Capture:    result.Capture,
		Protocols:  result.Protocols,
		ErrorCount: result.ErrorCount,
		Resources:  result.Resources,
		Limits:     profile.Limits,
		Business:   business,
		Scrapes:    scrapes,
	}
}

func TestReleaseFullCallNominal(t *testing.T) {
	profile := releaseFullCallNominalProfile()
	beginScenario(t)
	env := newTestEnvWithLimits(t.Context(), t, profile.Limits)
	result := runSippLoad(t.Context(), t, "call_highrate_uas.xml", "call_highrate_uac.xml",
		profile.Workload.Calls, int(profile.Workload.Rate), profile.PacketsPerCall, env)
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_invite_total"))
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_ser"))
	invites := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	ser := getMetric(t, env.endpoint, "sip_exporter_ser")
	require.NoError(t, validateReleaseRow(releaseRowSpec{}, releaseRowFromLoad(profile, result,
		map[string]float64{"invites": invites, "ser": ser}, nil)))
	recordReleaseResult(t, result, map[string]float64{"invites": invites, "ser": ser}, nil)
}

func TestReleaseSoak(t *testing.T) {
	profile := releaseSoakProfile()
	beginScenario(t)
	env := newTestEnvWithLimits(t.Context(), t, profile.Limits)
	runSippWarmup(t.Context(), t, "call_highrate_uas.xml", "call_highrate_uac.xml",
		releaseSoakWarmupProfile(), env)
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_invite_total"))
	invitesBefore := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	result := runSippLoad(t.Context(), t, "call_highrate_uas.xml", "call_highrate_uac.xml",
		profile.Workload.Calls, int(profile.Workload.Rate), profile.PacketsPerCall, env)
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_invite_total"))
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_ser"))
	invites := getMetric(t, env.endpoint, "sip_exporter_invite_total") - invitesBefore
	ser := getMetric(t, env.endpoint, "sip_exporter_ser")
	growth, err := summarizeSoakWorkingSet(result.ResourceSamples.Resources,
		result.Generator.Phases.MeasureStart, result.Generator.Phases.MeasureEnd)
	if err != nil && !errors.Is(err, errSoakWorkingSetGrowth) {
		require.NoError(t, err)
	}
	growthErr := err
	postDrain, postDrainBody, err := waitForPostDrainSnapshot(t.Context(), env.endpoint)
	require.NoError(t, err)
	recordScenarioArtifact(t, "metrics-post-drain.prom", postDrainBody)
	require.NoError(t, validateReleaseRow(releaseRowSpec{}, releaseRowFromLoad(profile, result,
		map[string]float64{"invites": invites, "ser": ser}, nil)))
	require.NoError(t, recordSoakReleaseOutcome(t, result,
		map[string]float64{"invites": invites, "ser": ser}, growth, postDrain, growthErr))
}

func TestReleaseINVITEFlood(t *testing.T) {
	profile := releaseINVITEFloodProfile()
	beginScenario(t)
	env := newTestEnvWithLimits(t.Context(), t, profile.Limits)
	result := runSippLoad(t.Context(), t, "", "flood_uac.xml",
		profile.Workload.Calls, int(profile.Workload.Rate), profile.PacketsPerCall, env)
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_invite_total"))
	invites := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	require.NoError(t, validateReleaseRow(releaseRowSpec{}, releaseRowFromLoad(profile, result,
		map[string]float64{"invites": invites}, nil)))
	recordReleaseResult(t, result, map[string]float64{"invites": invites}, nil)
}

func TestReleaseConcurrentDialogs(t *testing.T) {
	profile := releaseConcurrentDialogsProfile()
	beginScenario(t)
	env := newTestEnvWithLimits(t.Context(), t, profile.Limits)
	result := runConcurrentLoad(t.Context(), t, "concurrent_uas.xml", "concurrent_uac.xml",
		profile.Workload.Calls, int(profile.Workload.Rate), releaseConcurrentDialogs, env)
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_invite_total"))
	invites := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	require.NoError(t, validateReleaseRow(releaseRowSpec{}, releaseRowFromLoad(profile, result,
		map[string]float64{"invites": invites, "peak_sessions": result.PeakSessions}, nil)))
	recordReleaseResult(t, result, map[string]float64{"invites": invites, "peak_sessions": result.PeakSessions}, nil)
}

func recordReleaseResult(
	t *testing.T,
	result loadResult,
	business map[string]float64,
	scrapes *ScrapeSummary,
) {
	t.Helper()
	metrics := resourceMetricEntries(result.Resources)
	metrics["generator_cps"] = MetricEntry{Value: result.Generator.ActualRate, Unit: "cps", Direction: dirHigherIsBetter}
	metrics["system_errors"] = MetricEntry{Value: result.ErrorCount, Unit: "count", Direction: dirLowerIsBetter}
	for name, value := range business {
		metrics[name] = releaseBusinessMetricEntry(name, value)
	}
	if scrapes != nil {
		metrics["scrape_p50_ms"] = MetricEntry{Value: scrapes.P50MS, Unit: "ms", Direction: dirLowerIsBetter}
		metrics["scrape_p95_ms"] = MetricEntry{Value: scrapes.P95MS, Unit: "ms", Direction: dirLowerIsBetter}
		metrics["scrape_p99_ms"] = MetricEntry{Value: scrapes.P99MS, Unit: "ms", Direction: dirLowerIsBetter}
	}
	recordResult(t, metrics)
}

func recordSoakReleaseResult(
	t *testing.T,
	result loadResult,
	business map[string]float64,
	growth soakWorkingSetGrowth,
	postDrain postDrainSnapshot,
) {
	t.Helper()
	metrics := resourceMetricEntries(result.Resources)
	metrics["generator_cps"] = MetricEntry{Value: result.Generator.ActualRate, Unit: "cps", Direction: dirHigherIsBetter}
	metrics["system_errors"] = MetricEntry{Value: result.ErrorCount, Unit: "count", Direction: dirLowerIsBetter}
	for name, value := range business {
		metrics[name] = releaseBusinessMetricEntry(name, value)
	}
	metrics["working_set_first_minute_median_mb"] = MetricEntry{
		Value: growth.FirstMinuteMedianMB, Unit: "MiB", Direction: dirLowerIsBetter,
	}
	metrics["working_set_last_minute_median_mb"] = MetricEntry{
		Value: growth.LastMinuteMedianMB, Unit: "MiB", Direction: dirLowerIsBetter,
	}
	metrics["working_set_growth_mb"] = MetricEntry{
		Value: growth.GrowthMB, Unit: "MiB", Direction: dirLowerIsBetter,
	}
	metrics["post_drain_channel_length"] = MetricEntry{
		Value: postDrain.ChannelLength, Unit: "count", Direction: dirLowerIsBetter,
	}
	metrics["post_drain_active_dialogs"] = MetricEntry{
		Value: postDrain.ActiveDialogs, Unit: "count", Direction: dirLowerIsBetter,
	}
	metrics["post_drain_active_trackers"] = MetricEntry{
		Value: postDrain.ActiveTrackers, Unit: "count", Direction: dirLowerIsBetter,
	}
	recordResult(t, metrics)
}

func recordSoakReleaseOutcome(
	t *testing.T,
	result loadResult,
	business map[string]float64,
	growth soakWorkingSetGrowth,
	postDrain postDrainSnapshot,
	gateErr error,
) error {
	t.Helper()
	recordSoakReleaseResult(t, result, business, growth, postDrain)
	if gateErr != nil && activeRunRecorder != nil {
		if err := activeRunRecorder.Fail(t.Name(), gateErr.Error()); err != nil {
			return fmt.Errorf("record soak failure: %w", err)
		}
	}
	return gateErr
}

func releaseBusinessMetricEntry(name string, value float64) MetricEntry {
	switch {
	case name == "ser" || strings.HasPrefix(name, "ser_"):
		return MetricEntry{Value: value, Unit: "%", Direction: dirHigherIsBetter}
	case strings.HasPrefix(name, "cross_") || strings.HasPrefix(name, "unexpected_"):
		return MetricEntry{Value: value, Unit: "count", Direction: dirLowerIsBetter}
	default:
		return MetricEntry{Value: value, Unit: "count", Direction: dirHigherIsBetter}
	}
}
