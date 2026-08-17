//go:build e2e

package load

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	floodPacketsPerCall      = 1.0
	fullCallPacketsPerCall   = 7.0
	subtestTimeout           = 20 * time.Second
	releaseDuration          = 30 * time.Second
	nominalFullCallRate      = 1000
	peakFullCallRate         = 1800
	inviteFloodRate          = 5000
	releaseConcurrentDialogs = 2000
	concurrentCreationRate   = 100
	carrierUARatePerType     = 900
	carrierUACallsPerType    = carrierUARatePerType * int(releaseDuration/time.Second)
	carrierUAStartSkewLimit  = time.Duration(float64(releaseDuration) * (1 - minOfferedRateRatio))
)

type releaseProfileSpec struct {
	Workload       WorkloadSpec
	PacketsPerCall float64
	Limits         WorkloadLimits
	RequireScrapes bool
	Business       map[string]float64
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

func releaseBusinessMetricEntry(name string, value float64) MetricEntry {
	switch {
	case name == "ser" || strings.HasPrefix(name, "ser_"):
		return MetricEntry{Value: value, Unit: "%", Direction: dirHigherIsBetter}
	case strings.HasPrefix(name, "unexpected_"):
		return MetricEntry{Value: value, Unit: "count", Direction: dirLowerIsBetter}
	default:
		return MetricEntry{Value: value, Unit: "count", Direction: dirHigherIsBetter}
	}
}
