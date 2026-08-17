//go:build e2e

package load

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReleaseCarrierUA(t *testing.T) {
	profile := releaseCarrierUAProfile()
	carriersYAML := `carriers:
  - name: "loopback-carrier"
    cidrs:
      - "127.0.0.0/8"
`
	userAgentsYAML := `user_agents:
  - regex: '(?i)^Yealink'
    label: yealink
  - regex: '(?i)^Grandstream'
    label: grandstream
`

	beginScenario(t)
	env := newTestEnvWithCarrierAndUA(t.Context(), t, carriersYAML, userAgentsYAML)
	ctx := t.Context()
	measurement, measurementErr := newSteadyMeasurement(ctx, env)
	require.NoError(t, measurementErr)

	recordMetricsSnapshot(t, "metrics-before.prom", env.endpoint)
	protocolsBefore := readProtocolCounters(t, env.endpoint)
	packetsBefore := protocolsBefore.SIPPackets
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")
	expectedTotal := float64(profile.Workload.Calls) * profile.PacketsPerCall
	phases := PhaseTimestamps{WarmupStart: time.Now()}

	uasPath := absScenarioPath(t, "call_highrate_uas.xml")
	sippVol := filepath.Dir(uasPath)
	uasYealink := startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/call_highrate_uas.xml", "-i", "127.0.0.1", "-p", env.sippPort,
			"-m", strconv.Itoa(carrierUACallsPerType), "-nr", "-nostdin"},
		sippVol, "", false,
	)
	uasGrandstream := startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/call_highrate_uas.xml", "-i", "127.0.0.1", "-p", env.sippPort2,
			"-m", strconv.Itoa(carrierUACallsPerType), "-nr", "-nostdin"},
		sippVol, "", false,
	)
	waitForSIPpUDPReady(ctx, t, uasYealink, env.sippPort)
	waitForSIPpUDPReady(ctx, t, uasGrandstream, env.sippPort2)
	phases.Ready = time.Now()

	yealinkPath := absScenarioPath(t, "call_highrate_yealink_uac.xml")
	yealinkUAC := prepareSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/call_highrate_yealink_uac.xml",
			"-i", "127.0.0.1", "-p", env.sippClientPort,
			"-m", strconv.Itoa(carrierUACallsPerType), "-r", strconv.Itoa(carrierUARatePerType),
			"-cid_str", nextSippCallIDFormat(), "-nr",
			"127.0.0.1:" + env.sippPort},
		filepath.Dir(yealinkPath), "generator-yealink",
	)
	grandstreamPath := absScenarioPath(t, "call_highrate_grandstream_uac.xml")
	grandstreamUAC := prepareSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/call_highrate_grandstream_uac.xml",
			"-i", "127.0.0.1", "-p", env.sippClientPort2,
			"-m", strconv.Itoa(carrierUACallsPerType), "-r", strconv.Itoa(carrierUARatePerType),
			"-cid_str", nextSippCallIDFormat(), "-nr",
			"127.0.0.1:" + env.sippPort2},
		filepath.Dir(grandstreamPath), "generator-grandstream",
	)
	require.NoError(t, measurement.Begin(ctx, time.Now()))
	phases.MeasureStart = startPreparedSippContainers(ctx, t, yealinkUAC, grandstreamUAC)

	waitForContainerExit(ctx, t, yealinkUAC)
	yealinkMeasureEnd := time.Now()
	waitForContainerExit(ctx, t, grandstreamUAC)
	grandstreamMeasureEnd := time.Now()
	phases.MeasureEnd = laterTime(yealinkMeasureEnd, grandstreamMeasureEnd)
	resources := finishSteadyMeasurement(ctx, t, measurement, phases.MeasureEnd)
	yealinkPhases := phases
	yealinkPhases.MeasureStart = yealinkUAC.started
	yealinkPhases.MeasureEnd = yealinkMeasureEnd
	yealinkGenerator, generatorErr := yealinkUAC.readGeneratorEvidence(ctx, t, yealinkPhases)
	require.NoError(t, generatorErr)
	yealinkEvidenceAt := time.Now()
	grandstreamPhases := phases
	grandstreamPhases.MeasureStart = grandstreamUAC.started
	grandstreamPhases.MeasureEnd = grandstreamMeasureEnd
	grandstreamGenerator, generatorErr := grandstreamUAC.readGeneratorEvidence(ctx, t, grandstreamPhases)
	require.NoError(t, generatorErr)
	grandstreamEvidenceAt := time.Now()

	waitForContainerExit(ctx, t, uasYealink)
	yealinkUASExitAt := time.Now()
	waitForContainerExit(ctx, t, uasGrandstream)
	grandstreamUASExitAt := time.Now()
	waitForExactSIPCapture(ctx, t, env.endpoint, packetsBefore, expectedTotal)
	phases.DrainEnd = time.Now()
	yealinkGenerator.Phases.DrainEnd = phases.DrainEnd
	grandstreamGenerator.Phases.DrainEnd = phases.DrainEnd
	require.NoError(t, validatePostPhaseOrdering(
		phases.MeasureEnd,
		yealinkEvidenceAt, grandstreamEvidenceAt,
		yealinkUASExitAt, grandstreamUASExitAt, phases.DrainEnd,
	))

	recordMetricsSnapshot(t, "metrics-after.prom", env.endpoint)
	protocols := readProtocolCounters(t, env.endpoint).delta(protocolsBefore)
	result := loadResult{
		Capture:    newCaptureResult(expectedTotal, protocols.SIPPackets),
		Protocols:  protocols,
		ErrorCount: getMetric(t, env.endpoint, "sip_exporter_system_error_total") - errorsBefore,
		Resources:  resources,
	}
	recordLoadResultEvidence(t, result)
	generators := [2]GeneratorResult{yealinkGenerator, grandstreamGenerator}
	aggregateGenerator, aggregateErr := carrierUAAggregateGenerator(generators)
	require.NoError(t, aggregateErr)
	if activeRunRecorder != nil {
		require.NoError(t, activeRunRecorder.AttachGenerator(t.Name(), aggregateGenerator))
	}

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_invite_total"))
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_ser"))
	inviteTotal := getMetricSum(t, env.endpoint, "sip_exporter_invite_total")
	inviteYealink := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total",
		`carrier="loopback-carrier",ua_type="yealink"`)
	inviteGrandstream := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total",
		`carrier="loopback-carrier",ua_type="grandstream"`)
	serYealink := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser",
		`carrier="loopback-carrier",ua_type="yealink"`)
	serGrandstream := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser",
		`carrier="loopback-carrier",ua_type="grandstream"`)

	unexpectedLabelSeries := unexpectedCarrierUASeries(
		readMetricSamples(t, env.endpoint, "sip_exporter_invite_total"),
	) + unexpectedCarrierUASeries(readMetricSamples(t, env.endpoint, "sip_exporter_ser"))

	business := map[string]float64{
		"invites_total":                inviteTotal,
		"invites_loopback_yealink":     inviteYealink,
		"invites_loopback_grandstream": inviteGrandstream,
		"ser_loopback_yealink":         serYealink,
		"ser_loopback_grandstream":     serGrandstream,
		"unexpected_label_series":      unexpectedLabelSeries,
	}
	evidence := releaseCarrierUARowFromLoad(
		profile,
		result,
		generators,
		business,
	)
	require.NoError(t, validateCarrierUAAggregateRate(profile, generators))
	require.NoError(t, validateReleaseRow(releaseRowSpec{}, evidence))

	resultForMetrics := result
	resultForMetrics.Generator = aggregateGenerator
	recordReleaseResult(t, resultForMetrics, business, nil)
	t.Logf("Carrier/UA: actual=%.3f CPS, captured=%.0f/%.0f, cpu=%.2f%%, mem=%.1fMiB",
		resultForMetrics.Generator.ActualRate, result.Capture.Captured, result.Capture.Expected,
		resources.CPUP95Percent, resources.WorkingSetP99MB)
}

func unexpectedCarrierUASeries(samples []metricSample) float64 {
	unexpected := 0
	for _, sample := range samples {
		carrier, hasCarrier := sample.labels["carrier"]
		uaType, hasUAType := sample.labels["ua_type"]
		allowed := hasCarrier && hasUAType && carrier == "loopback-carrier" &&
			(uaType == "yealink" || uaType == "grandstream")
		if !allowed {
			unexpected++
		}
	}
	return float64(unexpected)
}
