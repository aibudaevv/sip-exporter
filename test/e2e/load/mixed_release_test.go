//go:build e2e

package load

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const mixedScrapePeriod = 100 * time.Millisecond

type (
	mixedRTPPorts struct {
		UASSIP   string
		UACSIP   string
		UASMedia string
		UACMedia string
	}

	mixedSippPlan struct {
		Spec    mixedGeneratorSpec
		Ports   mixedRTPPorts
		UASArgs []string
		UACArgs []string
	}

	mixedLifecycleEvidence struct {
		UASReady          []time.Time
		MeasurementStart  time.Time
		GeneratorStarts   []time.Time
		MeasurementEnd    time.Time
		EvidenceCollected time.Time
		CleanupStart      time.Time
		ScrapeInterval    time.Duration
	}

	mixedReleaseResult struct {
		Load          loadResult
		Generators    []GeneratorResult
		Scrapes       *ScrapeSummary
		InvitesBefore float64
		InvitesAfter  float64
		SER           float64
		RTPLost       float64
		RTPDuplicate  float64
		RTPOutOfOrder float64
		Lifecycle     mixedLifecycleEvidence
	}
)

func mixedSippPlans(profile mixedReleaseProfileSpec, ports []mixedRTPPorts) ([]mixedSippPlan, error) {
	if len(profile.Generators) == 0 || len(profile.Generators) != len(ports) {
		return nil, fmt.Errorf("mixed generator/port count mismatch: %d/%d",
			len(profile.Generators), len(ports))
	}
	seenPorts := make(map[string]struct{}, len(ports)*4)
	plans := make([]mixedSippPlan, len(profile.Generators))
	for i, generator := range profile.Generators {
		portSet := ports[i]
		for _, port := range []string{portSet.UASSIP, portSet.UACSIP, portSet.UASMedia, portSet.UACMedia} {
			if port == "" {
				return nil, fmt.Errorf("mixed generator %d has an empty port", i)
			}
			if _, exists := seenPorts[port]; exists {
				return nil, fmt.Errorf("mixed generator port %s is reused", port)
			}
			seenPorts[port] = struct{}{}
		}
		calls := strconv.Itoa(generator.Workload.Calls)
		rate := strconv.Itoa(int(generator.Workload.Rate))
		plans[i] = mixedSippPlan{
			Spec:  generator,
			Ports: portSet,
			UASArgs: []string{
				"-sf", "/scenarios/uas_rtp.xml", "-i", "127.0.0.1", "-mi", "127.0.0.1",
				"-p", portSet.UASSIP, "-mp", portSet.UASMedia, "-m", calls,
				"-l", strconv.Itoa(rtpMaxConcurrent), "-nr", "-nostdin",
			},
			UACArgs: []string{
				"-sf", "/scenarios/uac_rtp.xml", "-i", "127.0.0.1", "-mi", "127.0.0.1",
				"-p", portSet.UACSIP, "-mp", portSet.UACMedia, "-m", calls, "-r", rate,
				"-l", strconv.Itoa(rtpMaxConcurrent), "-cid_str", nextSippCallIDFormat(), "-nr",
				"-key", "user_agent", generator.UserAgent,
				net.JoinHostPort("127.0.0.1", portSet.UASSIP),
			},
		}
	}
	return plans, nil
}

func mixedScrapeInterval(profile mixedReleaseProfileSpec) time.Duration {
	if profile.RequireScrapes {
		return mixedScrapePeriod
	}
	return 0
}

func mixedWarmupPhase(profile mixedReleaseProfileSpec) (mixedReleaseProfileSpec, bool) {
	if profile.Warmup == 0 {
		return mixedReleaseProfileSpec{}, false
	}
	warmup := profile
	warmup.Generators = append([]mixedGeneratorSpec(nil), profile.Generators...)
	warmup.Warmup = 0
	warmup.RequireScrapes = false
	for i := range warmup.Generators {
		warmup.Generators[i].Workload.Calls = int(
			warmup.Generators[i].Workload.Rate * profile.Warmup.Seconds(),
		)
	}
	return warmup, true
}

func validateMixedLifecycle(profile mixedReleaseProfileSpec, evidence mixedLifecycleEvidence) error {
	if len(evidence.UASReady) != len(profile.Generators) ||
		len(evidence.GeneratorStarts) != len(profile.Generators) {
		return fmt.Errorf("mixed lifecycle generator count mismatch")
	}
	if evidence.MeasurementStart.IsZero() {
		return fmt.Errorf("mixed measurement start is missing")
	}
	for i, ready := range evidence.UASReady {
		if ready.IsZero() || !evidence.MeasurementStart.After(ready) {
			return fmt.Errorf("mixed measurement starts before UAS %d is ready", i)
		}
	}
	for i, started := range evidence.GeneratorStarts {
		if started.IsZero() || started.Before(evidence.MeasurementStart) {
			return fmt.Errorf("mixed generator %d starts before measurement", i)
		}
	}
	if err := validateMixedGeneratorStartSkew(evidence.GeneratorStarts); err != nil {
		return err
	}
	if !evidence.MeasurementEnd.After(evidence.MeasurementStart) {
		return fmt.Errorf("mixed measurement end must follow start")
	}
	if !evidence.EvidenceCollected.After(evidence.MeasurementEnd) {
		return fmt.Errorf("mixed evidence must follow measurement")
	}
	if !evidence.CleanupStart.After(evidence.EvidenceCollected) {
		return fmt.Errorf("mixed cleanup must follow evidence")
	}
	if evidence.ScrapeInterval != mixedScrapeInterval(profile) {
		return fmt.Errorf("mixed scrape interval: got %v, want %v",
			evidence.ScrapeInterval, mixedScrapeInterval(profile))
	}
	return nil
}

func newMixedRTPTestEnv(ctx context.Context, t *testing.T, profile mixedReleaseProfileSpec) *testEnv {
	t.Helper()
	httpPort, rtpPorts := allocateMixedRTPPorts(len(profile.Generators))
	sipPorts := make([]string, len(profile.Generators))
	for i := range profile.Generators {
		sipPorts[i] = rtpPorts[i].UASSIP
	}
	req := exporterContainerRequest(ctx, t, testInterface, httpPort,
		strings.Join(sipPorts, ","), profile.Limits)
	addMixedConfigMount(t, &req, "carriers", "/etc/sip-exporter/carriers.yaml",
		"SIP_EXPORTER_CARRIERS_CONFIG", `carriers:
  - name: "loopback-carrier"
    cidrs:
      - "127.0.0.0/8"
`)
	addMixedConfigMount(t, &req, "user-agents", "/etc/sip-exporter/user_agents.yaml",
		"SIP_EXPORTER_USER_AGENTS_CONFIG", `user_agents:
  - regex: '(?i)^Yealink'
    label: yealink
  - regex: '(?i)^Grandstream'
    label: grandstream
`)

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           log.New(io.Discard, "", 0),
	})
	require.NoError(t, err)
	require.NoError(t, verifyContainerLimits(ctx, c.GetContainerID(), profile.Limits))
	recordScenarioLimits(t, profile.Limits)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		recordContainerLogs(cleanupCtx, t, "exporter.log", c)
		_ = c.Stop(cleanupCtx, nil)
		_ = c.Terminate(cleanupCtx)
	})

	env := &testEnv{
		endpoint: fmt.Sprintf("http://localhost:%s", httpPort), rtpPorts: rtpPorts,
		exporterContainer: c, limits: profile.Limits,
	}
	if len(rtpPorts) > 0 {
		env.sippPort = rtpPorts[0].UASSIP
		env.sippClientPort = rtpPorts[0].UACSIP
		env.uasMediaPort = rtpPorts[0].UASMedia
		env.uacMediaPort = rtpPorts[0].UACMedia
	}
	return env
}

func allocateMixedRTPPorts(generatorCount int) (string, []mixedRTPPorts) {
	const generatorPortBlock = 32
	portMu.Lock()
	defer portMu.Unlock()
	base := nextBasePort
	nextBasePort += 1 + generatorCount*generatorPortBlock
	ports := make([]mixedRTPPorts, generatorCount)
	for i := range ports {
		block := base + 1 + i*generatorPortBlock
		ports[i] = mixedRTPPorts{
			UASSIP: strconv.Itoa(block), UACSIP: strconv.Itoa(block + 1),
			UASMedia: strconv.Itoa(block + 8), UACMedia: strconv.Itoa(block + 16),
		}
	}
	return strconv.Itoa(base), ports
}

func addMixedConfigMount(
	t *testing.T,
	req *testcontainers.ContainerRequest,
	prefix, target, envName, content string,
) {
	t.Helper()
	file, err := os.CreateTemp("", prefix+"-*.yaml")
	require.NoError(t, err)
	_, err = file.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	t.Cleanup(func() { _ = os.Remove(file.Name()) })
	req.Mounts = append(req.Mounts, testcontainers.BindMount(
		file.Name(), testcontainers.ContainerMountTarget(target),
	))
	req.Env[envName] = target
}

func runMixedReleaseLoad(
	ctx context.Context,
	t *testing.T,
	profile mixedReleaseProfileSpec,
	env *testEnv,
) mixedReleaseResult {
	t.Helper()
	if warmup, ok := mixedWarmupPhase(profile); ok {
		runMixedWarmup(ctx, t, warmup, env)
	}

	plans, err := mixedSippPlans(profile, env.rtpPorts)
	require.NoError(t, err)
	invitesBefore := metricSumOrZero(t, env.endpoint, "sip_exporter_invite_total")
	rtpLostBefore := metricSumOrZero(t, env.endpoint, "sip_exporter_rtp_packets_lost_total")
	rtpDuplicateBefore := metricSumOrZero(t, env.endpoint, "sip_exporter_rtp_duplicate_packets_total")
	rtpOutOfOrderBefore := metricSumOrZero(t, env.endpoint, "sip_exporter_rtp_out_of_order_total")
	measurement, err := newSteadyMeasurement(ctx, env)
	require.NoError(t, err)
	recordMetricsSnapshot(t, "metrics-before.prom", env.endpoint)
	protocolsBefore := readProtocolCounters(t, env.endpoint)
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	uasContainers, readyTimes := startMixedUAS(ctx, t, plans)
	uacContainers := prepareMixedUAC(ctx, t, plans)
	measureStart := time.Now()
	require.NoError(t, measurement.Begin(ctx, measureStart))
	startPreparedSippContainers(ctx, t, uacContainers...)
	generatorStarts := mixedContainerStarts(uacContainers)
	observations := waitMixedUAC(ctx, t, uacContainers, mixedScrapeInterval(profile), env.endpoint)
	measureEnd := time.Now()
	resources, resourceSamples := finishSteadyMeasurementWithSamples(ctx, t, measurement, measureEnd)
	rtpPPSP95, err := rtpRateP95(resourceSamples.Metrics)
	require.NoError(t, err)

	readyAt := latestTimestamp(readyTimes)
	generators := make([]GeneratorResult, len(uacContainers))
	for i, container := range uacContainers {
		phases := PhaseTimestamps{
			WarmupStart: readyAt, Ready: readyAt, MeasureStart: container.started,
			MeasureEnd: measureEnd, DrainEnd: measureEnd,
		}
		generators[i], err = container.readGeneratorEvidence(ctx, t, phases)
		require.NoError(t, err)
		generators[i].ActualRate, err = sippRampRate(
			profile.Generators[i].Workload.Calls,
			generators[i].startedAt, generators[i].rampEndAt,
		)
		require.NoError(t, err)
	}
	for _, container := range uasContainers {
		waitForContainerExit(ctx, t, container)
	}
	expectedSIP := mixedExpectedSIPPackets(profile)
	waitForExactSIPCapture(ctx, t, env.endpoint, protocolsBefore.SIPPackets, expectedSIP)
	drainAt := time.Now()
	for i := range generators {
		generators[i].Phases.DrainEnd = drainAt
		require.NoError(t, generators[i].Validate(profile.Generators[i].Workload))
	}

	recordMetricsSnapshot(t, "metrics-after.prom", env.endpoint)
	protocolsAfter := readProtocolCounters(t, env.endpoint)
	protocols := protocolsAfter.delta(protocolsBefore)
	capture := newCaptureResult(expectedSIP, protocols.SIPPackets)
	require.NoError(t, capture.ValidateExact())
	aggregate := aggregateMixedGenerators(generators)
	require.NoError(t, aggregate.Validate(mixedAggregateWorkload(profile)))
	duration := measureEnd.Sub(measureStart)
	actualPPS := 0.0
	if duration > 0 {
		actualPPS = capture.Captured / duration.Seconds()
	}
	load := loadResult{
		Duration: duration, Generator: aggregate, Capture: capture, Protocols: protocols,
		PacketsBefore: protocolsBefore.SIPPackets, PacketsAfter: protocolsAfter.SIPPackets,
		ActualPPS: actualPPS, ExpectedPPS: mixedAggregateWorkload(profile).Rate * rtpSipPacketsPerCall,
		ErrorCount: getMetric(t, env.endpoint, "sip_exporter_system_error_total") - errorsBefore,
		DrainTime:  drainAt.Sub(measureEnd), RTPPPSP95: rtpPPSP95,
		Resources: resources, ResourceSamples: resourceSamples,
	}
	recordLoadResultEvidence(t, load)

	var scrapes *ScrapeSummary
	if profile.RequireScrapes {
		summary, summaryErr := summarizeScrapes(observations, measureStart, measureEnd)
		require.NoError(t, summaryErr)
		scrapes = &summary
	}
	result := mixedReleaseResult{
		Load: load, Generators: generators, Scrapes: scrapes,
		InvitesBefore: invitesBefore,
		InvitesAfter:  metricSumOrZero(t, env.endpoint, "sip_exporter_invite_total"),
		SER:           mixedSER(readMetricSamples(t, env.endpoint, "sip_exporter_ser")),
		RTPLost: metricSumOrZero(t, env.endpoint,
			"sip_exporter_rtp_packets_lost_total") - rtpLostBefore,
		RTPDuplicate: metricSumOrZero(t, env.endpoint,
			"sip_exporter_rtp_duplicate_packets_total") - rtpDuplicateBefore,
		RTPOutOfOrder: metricSumOrZero(t, env.endpoint,
			"sip_exporter_rtp_out_of_order_total") - rtpOutOfOrderBefore,
	}
	evidenceAt := time.Now()
	cleanupAt := time.Now()
	result.Lifecycle = mixedLifecycleEvidence{
		UASReady: readyTimes, MeasurementStart: measureStart, GeneratorStarts: generatorStarts,
		MeasurementEnd: measureEnd, EvidenceCollected: evidenceAt, CleanupStart: cleanupAt,
		ScrapeInterval: mixedScrapeInterval(profile),
	}
	require.NoError(t, validateMixedLifecycle(profile, result.Lifecycle))
	return result
}

func mixedDualUABusiness(invites, ser []metricSample) map[string]float64 {
	return map[string]float64{
		"invites_total": metricSampleSum(invites),
		"invites_loopback_yealink": mixedLabelValue(invites,
			map[string]string{"carrier": "loopback-carrier", "ua_type": "yealink"}),
		"invites_loopback_grandstream": mixedLabelValue(invites,
			map[string]string{"carrier": "loopback-carrier", "ua_type": "grandstream"}),
		"ser_loopback_yealink": mixedLabelValue(ser,
			map[string]string{"carrier": "loopback-carrier", "ua_type": "yealink"}),
		"ser_loopback_grandstream": mixedLabelValue(ser,
			map[string]string{"carrier": "loopback-carrier", "ua_type": "grandstream"}),
		"unexpected_label_series": unexpectedCarrierUASeries(invites) + unexpectedCarrierUASeries(ser),
	}
}

func metricSampleSum(samples []metricSample) float64 {
	var total float64
	for _, sample := range samples {
		total += sample.value
	}
	return total
}

func mixedLabelValue(samples []metricSample, labels map[string]string) float64 {
	value, err := singleMetricValue(metricSamplesWithLabels(samples, labels))
	if err != nil {
		return math.NaN()
	}
	return value
}

func mixedSER(samples []metricSample) float64 {
	if len(samples) == 0 {
		return math.NaN()
	}
	for _, sample := range samples {
		if sample.value != 100 {
			return sample.value
		}
	}
	return 100
}

func mixedMeasuredInvites(result mixedReleaseResult) float64 {
	return result.InvitesAfter - result.InvitesBefore
}

func mixedReleaseEvidenceFromResult(
	profile mixedReleaseProfileSpec,
	result mixedReleaseResult,
	business map[string]float64,
) mixedReleaseEvidence {
	generators := make([]releaseGeneratorEvidence, len(profile.Generators))
	starts := make([]time.Time, len(profile.Generators))
	for i, generator := range profile.Generators {
		generators[i] = releaseGeneratorEvidence{Spec: generator.Workload, Result: result.Generators[i]}
		starts[i] = result.Generators[i].Phases.MeasureStart
	}
	businessEvidence := make(map[string]releaseBusinessEvidence)
	for name, expected := range mixedBusinessExpectations(profile) {
		actual, ok := business[name]
		if !ok {
			actual = math.NaN()
		}
		businessEvidence[name] = releaseBusinessEvidence{Expected: expected, Actual: actual}
	}
	expectedPackets, expectedPPS, err := mixedRTPExpectations(profile)
	if err != nil {
		expectedPackets = math.NaN()
		expectedPPS = math.NaN()
	}
	return mixedReleaseEvidence{
		Row: releaseRowEvidence{
			Generators: generators, Capture: result.Load.Capture, Protocols: result.Load.Protocols,
			ErrorCount: result.Load.ErrorCount, Resources: result.Load.Resources,
			Limits: profile.Limits, Business: businessEvidence, Scrapes: result.Scrapes,
		},
		SER: result.SER, ExpectedRTPPackets: expectedPackets,
		ActualRTPPackets:  result.Load.Protocols.RTPPackets,
		ExpectedRTPPPSP95: expectedPPS, ActualRTPPPSP95: result.Load.RTPPPSP95,
		GeneratorStarts: starts, RTPLost: result.RTPLost,
		RTPDuplicate: result.RTPDuplicate, RTPOutOfOrder: result.RTPOutOfOrder,
	}
}

func mixedBusinessExpectations(profile mixedReleaseProfileSpec) map[string]float64 {
	if len(profile.Generators) == 2 {
		return map[string]float64{
			"invites_total":                float64(mixedAggregateWorkload(profile).Calls),
			"invites_loopback_yealink":     float64(profile.Generators[0].Workload.Calls),
			"invites_loopback_grandstream": float64(profile.Generators[1].Workload.Calls),
			"ser_loopback_yealink":         100, "ser_loopback_grandstream": 100,
			"unexpected_label_series": 0,
		}
	}
	return map[string]float64{
		"invites": float64(mixedAggregateWorkload(profile).Calls), "ser": 100,
	}
}

func summarizeMixedSoakWorkingSet(result mixedReleaseResult) (soakWorkingSetGrowth, error) {
	return summarizeSoakWorkingSet(result.Load.ResourceSamples.Resources,
		result.Load.Generator.Phases.MeasureStart, result.Load.Generator.Phases.MeasureEnd)
}

func recordMixedReleaseResult(t *testing.T, result mixedReleaseResult, business map[string]float64) {
	t.Helper()
	recordResult(t, mixedReleaseMetricEntries(result, business))
}

func recordMixedSoakReleaseOutcome(
	t *testing.T,
	profile mixedReleaseProfileSpec,
	result mixedReleaseResult,
	business map[string]float64,
	growth soakWorkingSetGrowth,
	postDrain postDrainSnapshot,
	gateErr error,
) error {
	t.Helper()
	metrics := mixedReleaseMetricEntries(result, business)
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
	gateErr = errors.Join(validateMixedRelease(profile,
		mixedReleaseEvidenceFromResult(profile, result, business)), gateErr)
	if gateErr != nil && activeRunRecorder != nil {
		if err := activeRunRecorder.Fail(t.Name(), gateErr.Error()); err != nil {
			return fmt.Errorf("record mixed soak failure: %w", err)
		}
	}
	return gateErr
}

func mixedReleaseMetricEntries(
	result mixedReleaseResult,
	business map[string]float64,
) map[string]MetricEntry {
	metrics := resourceMetricEntries(result.Load.Resources)
	metrics["generator_cps"] = MetricEntry{
		Value: result.Load.Generator.ActualRate, Unit: "cps", Direction: dirHigherIsBetter,
	}
	metrics["system_errors"] = MetricEntry{
		Value: result.Load.ErrorCount, Unit: "count", Direction: dirLowerIsBetter,
	}
	metrics["rtp_packets"] = MetricEntry{
		Value: result.Load.Protocols.RTPPackets, Unit: "count", Direction: dirHigherIsBetter,
	}
	metrics["rtp_pps_p95"] = MetricEntry{
		Value: result.Load.RTPPPSP95, Unit: "pps", Direction: dirHigherIsBetter,
	}
	metrics["rtp_lost"] = MetricEntry{Value: result.RTPLost, Unit: "count", Direction: dirLowerIsBetter}
	metrics["rtp_duplicate"] = MetricEntry{Value: result.RTPDuplicate, Unit: "count", Direction: dirLowerIsBetter}
	metrics["rtp_out_of_order"] = MetricEntry{Value: result.RTPOutOfOrder, Unit: "count", Direction: dirLowerIsBetter}
	for name, value := range business {
		metrics[name] = releaseBusinessMetricEntry(name, value)
	}
	if result.Scrapes != nil {
		metrics["scrape_p50_ms"] = MetricEntry{Value: result.Scrapes.P50MS, Unit: "ms", Direction: dirLowerIsBetter}
		metrics["scrape_p95_ms"] = MetricEntry{Value: result.Scrapes.P95MS, Unit: "ms", Direction: dirLowerIsBetter}
		metrics["scrape_p99_ms"] = MetricEntry{Value: result.Scrapes.P99MS, Unit: "ms", Direction: dirLowerIsBetter}
	}
	return metrics
}

func TestReleaseMixedNominal(t *testing.T) {
	runMixedReleaseScenario(t, mixedNominalProfile())
}

func TestReleaseMixedPeak(t *testing.T) {
	runMixedReleaseScenario(t, mixedPeakProfile())
}

func TestReleaseMixedSoak(t *testing.T) {
	profile := mixedSoakProfile()
	beginScenario(t)
	env := newMixedRTPTestEnv(t.Context(), t, profile)
	result := runMixedReleaseLoad(t.Context(), t, profile, env)
	business := mixedReleaseBusiness(t, profile, result, env.endpoint)
	growth, err := summarizeMixedSoakWorkingSet(result)
	if err != nil && !errors.Is(err, errSoakWorkingSetGrowth) {
		require.NoError(t, err)
	}
	growthErr := err
	postDrain, postDrainBody, err := waitForPostDrainSnapshot(t.Context(), env.endpoint)
	require.NoError(t, err)
	recordScenarioArtifact(t, "metrics-post-drain.prom", postDrainBody)
	require.NoError(t, recordMixedSoakReleaseOutcome(t, profile, result, business,
		growth, postDrain, growthErr))
	t.Logf("Mixed soak: actual=%.1f CPS, SIP=%.0f, RTP=%.0f, RTP p95=%.0f pps, "+
		"cpu=%.2f%%, mem=%.1fMiB, growth=%.1fMiB",
		result.Load.Generator.ActualRate, result.Load.Capture.Captured,
		result.Load.Protocols.RTPPackets, result.Load.RTPPPSP95,
		result.Load.Resources.CPUP95Percent, result.Load.Resources.WorkingSetP99MB,
		growth.GrowthMB)
}

func runMixedReleaseScenario(t *testing.T, profile mixedReleaseProfileSpec) {
	t.Helper()
	beginScenario(t)
	env := newMixedRTPTestEnv(t.Context(), t, profile)
	result := runMixedReleaseLoad(t.Context(), t, profile, env)
	business := mixedReleaseBusiness(t, profile, result, env.endpoint)
	require.NoError(t, validateMixedRelease(profile,
		mixedReleaseEvidenceFromResult(profile, result, business)))
	recordMixedReleaseResult(t, result, business)
	if result.Scrapes == nil {
		t.Logf("Mixed release: actual=%.1f CPS, SIP=%.0f, RTP=%.0f, "+
			"RTP p95=%.0f pps, cpu=%.2f%%, mem=%.1fMiB",
			result.Load.Generator.ActualRate, result.Load.Capture.Captured,
			result.Load.Protocols.RTPPackets, result.Load.RTPPPSP95,
			result.Load.Resources.CPUP95Percent, result.Load.Resources.WorkingSetP99MB)
		return
	}
	t.Logf("Mixed release: actual=%.1f CPS, SIP=%.0f, RTP=%.0f, "+
		"RTP p95=%.0f pps, cpu=%.2f%%, mem=%.1fMiB, scrape p50/p95/p99=%.2f/%.2f/%.2fms",
		result.Load.Generator.ActualRate, result.Load.Capture.Captured,
		result.Load.Protocols.RTPPackets, result.Load.RTPPPSP95,
		result.Load.Resources.CPUP95Percent, result.Load.Resources.WorkingSetP99MB,
		result.Scrapes.P50MS, result.Scrapes.P95MS, result.Scrapes.P99MS)
}

func mixedReleaseBusiness(
	t *testing.T,
	profile mixedReleaseProfileSpec,
	result mixedReleaseResult,
	endpoint string,
) map[string]float64 {
	t.Helper()
	if len(profile.Generators) == 2 {
		return mixedDualUABusiness(
			readMetricSamples(t, endpoint, "sip_exporter_invite_total"),
			readMetricSamples(t, endpoint, "sip_exporter_ser"),
		)
	}
	return map[string]float64{"invites": mixedMeasuredInvites(result), "ser": result.SER}
}

func runMixedWarmup(ctx context.Context, t *testing.T, profile mixedReleaseProfileSpec, env *testEnv) {
	t.Helper()
	plans, err := mixedSippPlans(profile, env.rtpPorts)
	require.NoError(t, err)
	protocolsBefore := readProtocolCounters(t, env.endpoint)
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")
	uasContainers, readyTimes := startMixedUAS(ctx, t, plans)
	uacContainers := prepareMixedUAC(ctx, t, plans)
	startPreparedSippContainers(ctx, t, uacContainers...)
	for _, container := range uacContainers {
		waitForContainerExit(ctx, t, container)
	}
	measureEnd := time.Now()
	readyAt := latestTimestamp(readyTimes)
	for i, container := range uacContainers {
		phases := PhaseTimestamps{
			WarmupStart: readyAt, Ready: readyAt, MeasureStart: container.started,
			MeasureEnd: measureEnd, DrainEnd: measureEnd,
		}
		generator, readErr := container.readGeneratorEvidence(ctx, t, phases)
		require.NoError(t, readErr)
		generator.ActualRate, readErr = sippRampRate(
			profile.Generators[i].Workload.Calls, generator.startedAt, generator.rampEndAt,
		)
		require.NoError(t, readErr)
		require.NoError(t, generator.Validate(profile.Generators[i].Workload))
	}
	for _, container := range uasContainers {
		waitForContainerExit(ctx, t, container)
	}
	waitForExactSIPCapture(ctx, t, env.endpoint, protocolsBefore.SIPPackets,
		mixedExpectedSIPPackets(profile))
	require.Equal(t, errorsBefore,
		getMetric(t, env.endpoint, "sip_exporter_system_error_total"),
		"mixed warmup must not add exporter errors")
}

func startMixedUAS(
	ctx context.Context,
	t *testing.T,
	plans []mixedSippPlan,
) ([]*startedSippContainer, []time.Time) {
	t.Helper()
	scenarioDir := filepath.Dir(absScenarioPath(t, "uas_rtp.xml"))
	containers := make([]*startedSippContainer, len(plans))
	ready := make([]time.Time, len(plans))
	for i, plan := range plans {
		containers[i] = startSippContainer(ctx, t, plan.UASArgs, scenarioDir, "", false)
		waitForSIPpUDPReady(ctx, t, containers[i], plan.Ports.UASSIP)
		ready[i] = time.Now()
	}
	return containers, ready
}

func prepareMixedUAC(
	ctx context.Context,
	t *testing.T,
	plans []mixedSippPlan,
) []*startedSippContainer {
	t.Helper()
	scenarioDir := filepath.Dir(absScenarioPath(t, "uac_rtp.xml"))
	containers := make([]*startedSippContainer, len(plans))
	for i, plan := range plans {
		containers[i] = prepareSippContainer(ctx, t, plan.UACArgs, scenarioDir,
			"generator-"+plan.Spec.Name)
	}
	return containers
}

func waitMixedUAC(
	ctx context.Context,
	t *testing.T,
	containers []*startedSippContainer,
	scrapeInterval time.Duration,
	endpoint string,
) []scrapeObservation {
	t.Helper()
	if scrapeInterval == 0 {
		for _, container := range containers {
			waitForContainerExit(ctx, t, container)
		}
		return nil
	}
	client := &http.Client{Timeout: 5 * time.Second}
	observations := make([]scrapeObservation, 0, 500)
	for {
		running := false
		for _, container := range containers {
			state, err := container.State(ctx)
			require.NoError(t, err)
			running = running || state.Running
		}
		if !running {
			return observations
		}
		observations = append(observations, scrapeOnce(ctx, client, endpoint+"/metrics"))
		timer := time.NewTimer(scrapeInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			require.NoError(t, ctx.Err())
		case <-timer.C:
		}
	}
}

func mixedContainerStarts(containers []*startedSippContainer) []time.Time {
	starts := make([]time.Time, len(containers))
	for i, container := range containers {
		starts[i] = container.started
	}
	return starts
}

func latestTimestamp(times []time.Time) time.Time {
	latest := time.Time{}
	for _, at := range times {
		if at.After(latest) {
			latest = at
		}
	}
	return latest
}

func mixedExpectedSIPPackets(profile mixedReleaseProfileSpec) float64 {
	return float64(mixedAggregateWorkload(profile).Calls) * rtpSipPacketsPerCall
}

func mixedAggregateWorkload(profile mixedReleaseProfileSpec) WorkloadSpec {
	var aggregate WorkloadSpec
	for _, generator := range profile.Generators {
		aggregate.Calls += generator.Workload.Calls
		aggregate.Rate += generator.Workload.Rate
	}
	return aggregate
}

func aggregateMixedGenerators(generators []GeneratorResult) GeneratorResult {
	aggregate := GeneratorResult{}
	for i, generator := range generators {
		aggregate.SuccessfulCalls += generator.SuccessfulCalls
		aggregate.FailedCalls += generator.FailedCalls
		aggregate.Retransmissions += generator.Retransmissions
		aggregate.ActualRate += generator.ActualRate
		if aggregate.ExitCode == 0 && generator.ExitCode != 0 {
			aggregate.ExitCode = generator.ExitCode
		}
		if i == 0 || generator.Phases.WarmupStart.Before(aggregate.Phases.WarmupStart) {
			aggregate.Phases.WarmupStart = generator.Phases.WarmupStart
		}
		if generator.Phases.Ready.After(aggregate.Phases.Ready) {
			aggregate.Phases.Ready = generator.Phases.Ready
		}
		if i == 0 || generator.Phases.MeasureStart.Before(aggregate.Phases.MeasureStart) {
			aggregate.Phases.MeasureStart = generator.Phases.MeasureStart
		}
		if generator.Phases.MeasureEnd.After(aggregate.Phases.MeasureEnd) {
			aggregate.Phases.MeasureEnd = generator.Phases.MeasureEnd
		}
		if generator.Phases.DrainEnd.After(aggregate.Phases.DrainEnd) {
			aggregate.Phases.DrainEnd = generator.Phases.DrainEnd
		}
	}
	return aggregate
}
