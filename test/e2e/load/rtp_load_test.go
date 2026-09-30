//go:build e2e

package load

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	// SIP packets per call for the RTP scenarios (INVITE, 100, 200, ACK, BYE,
	// 200 — no 180 Ringing). sip_exporter_packets_total counts SIP only; RTP
	// packets are verified separately via rtp_packets_total.
	rtpSipPacketsPerCall = 6.0
	rtpLoadTimeout       = 120 * time.Second
	rtpMediaSeconds      = 18
	rtpPacketsPerSecond  = 100
	rtpOfferSeconds      = 33
	rtpMaxConcurrent     = 1024
)

// allocateRTPPorts reserves the HTTP, SIP, and fixed SDP [media_port] values.
// SIPp 3.5.1 does not increment -mp for concurrent calls.
func allocateRTPPorts() (http, uasSIP, uacSIP, uasMedia, uacMedia string) {
	portMu.Lock()
	defer portMu.Unlock()
	base := nextBasePort
	nextBasePort += 5
	return strconv.Itoa(base), strconv.Itoa(base + 1), strconv.Itoa(base + 2),
		strconv.Itoa(base + 3), strconv.Itoa(base + 4)
}

// newRTPTestEnv starts the exporter with RTP capture enabled and allocates
// separate media ports for SIPp's -mp flag.
func newRTPTestEnv(ctx context.Context, t *testing.T) *testEnv {
	return newRTPTestEnvWithLimits(ctx, t, peakLimits)
}

func newRTPTestEnvWithLimits(
	ctx context.Context,
	t *testing.T,
	limits WorkloadLimits,
) *testEnv {
	t.Helper()

	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := allocateRTPPorts()

	req := exporterContainerRequest(ctx, t, testInterface, httpPort, uasSIP, limits)

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           log.New(io.Discard, "", 0),
	})
	if err != nil && c != nil {
		logs, logErr := c.Logs(ctx)
		if logErr == nil {
			defer logs.Close()
			logBytes, _ := io.ReadAll(logs)
			t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
		}
	}
	require.NoError(t, err)
	require.NoError(t, verifyContainerLimits(ctx, c.GetContainerID(), limits))
	recordScenarioLimits(t, limits)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		recordContainerLogs(cleanupCtx, t, "exporter.log", c)
		if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
			logs, logErr := c.Logs(cleanupCtx)
			if logErr == nil {
				defer logs.Close()
				logBytes, _ := io.ReadAll(logs)
				t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
			}
		}
		_ = c.Stop(cleanupCtx, nil)
		_ = c.Terminate(cleanupCtx)
	})

	return &testEnv{
		endpoint:          fmt.Sprintf("http://localhost:%s", httpPort),
		sippPort:          uasSIP,
		sippClientPort:    uacSIP,
		uasMediaPort:      uasMedia,
		uacMediaPort:      uacMedia,
		exporterContainer: c,
		limits:            limits,
	}
}

// runSippRTPLoad drives a UAS+UAC SIPp pair with RTP media scenarios,
// collecting CPU/RAM/loss statistics like runSippLoad but with -mi/-mp/-nr.
func runSippRTPLoad(
	ctx context.Context,
	t *testing.T,
	callCount, rate int,
	env *testEnv,
) loadResult {
	t.Helper()

	measurement, measurementErr := newSteadyMeasurement(ctx, env)
	require.NoError(t, measurementErr)

	recordMetricsSnapshot(t, "metrics-before.prom", env.endpoint)
	protocolsBefore := readProtocolCounters(t, env.endpoint)
	packetsBefore := protocolsBefore.SIPPackets
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")
	expectedTotal := float64(callCount) * rtpSipPacketsPerCall

	uasPath := absScenarioPath(t, "uas_rtp.xml")
	sippVol := filepath.Dir(uasPath)

	uasContainer := startSippContainer(ctx, t,
		[]string{
			"-sf", "/scenarios/uas_rtp.xml",
			"-i", "127.0.0.1",
			"-mi", "127.0.0.1",
			"-p", env.sippPort,
			"-mp", env.uasMediaPort,
			"-m", strconv.Itoa(callCount),
			"-l", strconv.Itoa(rtpMaxConcurrent),
			"-nr",
			"-nostdin",
		},
		sippVol, "", false,
	)

	waitForSIPpUDPReady(ctx, t, uasContainer, env.sippPort)

	uacPath := absScenarioPath(t, "uac_rtp.xml")
	sippVol = filepath.Dir(uacPath)

	measureStart := time.Now()
	require.NoError(t, measurement.Begin(ctx, measureStart))
	uacContainer := startSippContainer(ctx, t,
		[]string{
			"-sf", "/scenarios/uac_rtp.xml",
			"-i", "127.0.0.1",
			"-mi", "127.0.0.1",
			"-p", env.sippClientPort,
			"-mp", env.uacMediaPort,
			"-m", strconv.Itoa(callCount),
			"-r", strconv.Itoa(rate),
			"-l", strconv.Itoa(rtpMaxConcurrent),
			"-nr",
			"127.0.0.1:" + env.sippPort,
		},
		sippVol, "generator", true,
	)
	measureEnd := time.Now()
	resourceSummary, resourceSamples := finishSteadyMeasurementWithSamples(
		ctx, t, measurement, measureEnd,
	)
	rtpPPSP95, rtpRateErr := rtpRateP95(resourceSamples.Metrics)
	require.NoError(t, rtpRateErr)
	generator, generatorErr := uacContainer.readGeneratorEvidence(ctx, t, PhaseTimestamps{
		WarmupStart: measureStart, Ready: measureStart, MeasureStart: measureStart,
		MeasureEnd: measureEnd, DrainEnd: measureEnd,
	})
	require.NoError(t, generatorErr)
	evidenceAt := time.Now()

	waitForContainerExit(ctx, t, uasContainer)
	uasExitAt := time.Now()
	sippDuration := measureEnd.Sub(measureStart)

	waitForExactSIPCapture(ctx, t, env.endpoint, packetsBefore, expectedTotal)

	stableTime := time.Now()
	require.NoError(t, validatePostPhaseOrdering(measureEnd, evidenceAt, uasExitAt, stableTime))
	drainTime := stableTime.Sub(measureEnd)
	generator.Phases.DrainEnd = stableTime
	generator.ActualRate, generatorErr = sippRampRate(
		callCount, generator.startedAt, generator.rampEndAt,
	)
	require.NoError(t, generatorErr)
	require.NoError(t, generator.Validate(WorkloadSpec{
		Calls: callCount,
		Rate:  float64(rate),
	}))

	recordMetricsSnapshot(t, "metrics-after.prom", env.endpoint)
	protocolsAfter := readProtocolCounters(t, env.endpoint)
	protocols := protocolsAfter.delta(protocolsBefore)
	packetsAfter := protocolsAfter.SIPPackets
	errorsAfter := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	capture := newCaptureResult(expectedTotal, protocols.SIPPackets)
	require.NoError(t, capture.ValidateExact())
	totalCaptured := capture.Captured
	actualPPS := 0.0
	if sippDuration.Seconds() > 0 {
		actualPPS = totalCaptured / sippDuration.Seconds()
	}
	expectedPPS := float64(rate) * rtpSipPacketsPerCall

	result := loadResult{
		Duration:        sippDuration,
		Generator:       generator,
		Capture:         capture,
		Protocols:       protocols,
		PacketsBefore:   packetsBefore,
		PacketsAfter:    packetsAfter,
		ActualPPS:       actualPPS,
		ExpectedPPS:     expectedPPS,
		LossRate:        capture.LossPct / 100,
		ErrorCount:      errorsAfter - errorsBefore,
		DrainTime:       drainTime,
		RTPPPSP95:       rtpPPSP95,
		Resources:       resourceSummary,
		ResourceSamples: resourceSamples,
	}
	recordLoadResultEvidence(t, result)

	t.Logf("RTP load: offered_cps=%.1f, sip_pps=%.0f, rtp_pps_p95=%.0f, captured=%.0f, expected=%.0f, loss=%.2f%%, drain=%v, cpu_p95=%.2f%% quota (%.3f cores), mem_p99=%.1fMB, errors=%.0f",
		result.Generator.ActualRate, result.ActualPPS, result.RTPPPSP95, totalCaptured, expectedTotal,
		result.LossRate*100, result.DrainTime, result.Resources.CPUP95Percent,
		result.Resources.CPUP95Percent*result.Resources.Limits.CPUCores/100,
		result.Resources.WorkingSetP99MB, result.ErrorCount)

	return result
}

// TestLoadFullCallWithRTP measures combined SIP+RTP throughput. Each call
// runs a full SIP dialog (INVITE→200→ACK→BYE→200) plus 18s of G.711a RTP media
// in both directions, yielding about 1800 packets per call (2 × 50pps × 18s).
func TestLoadFullCallWithRTP(t *testing.T) {
	rates := []int{25, 50}
	for _, rate := range rates {
		t.Run(fmt.Sprintf("rate_%d", rate), func(t *testing.T) {
			beginScenario(t)
			env := newRTPTestEnvWithLimits(t.Context(), t, diagnosticMemoryLimits)

			ctx, cancel := context.WithTimeout(t.Context(), rtpLoadTimeout)
			defer cancel()

			callCount := rate * rtpOfferSeconds
			result := runSippRTPLoad(ctx, t, callCount, rate, env)

			require.Zero(t, result.ErrorCount,
				"exporter must not report system errors")
			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets")

			ser := getMetric(t, env.endpoint, "sip_exporter_ser")
			require.GreaterOrEqual(t, ser, 99.0,
				"SER SLO: >= 99%% with RTP capture enabled (got %.2f%%)", ser)

			rtpPackets := result.Protocols.RTPPackets
			require.Greater(t, rtpPackets, 0.0,
				"RTP packets must be captured")
			expectedRTPPackets := float64(callCount * rtpMediaSeconds * rtpPacketsPerSecond)
			require.InDelta(t, expectedRTPPackets, rtpPackets, expectedRTPPackets*0.02,
				"captured RTP volume must cover every call's media interval")
			expectedSteadyRTPPPS := float64(rate * rtpMediaSeconds * rtpPacketsPerSecond)
			require.InDelta(t, expectedSteadyRTPPPS, result.RTPPPSP95,
				expectedSteadyRTPPPS*0.10,
				"observed RTP p95 must reach the steady offered packet rate")
			for _, metricName := range []string{
				"sip_exporter_rtp_packets_lost_total",
				"sip_exporter_rtp_duplicate_packets_total",
				"sip_exporter_rtp_out_of_order_total",
			} {
				require.Zero(t, metricSumOrZero(t, env.endpoint, metricName),
					"healthy generated RTP must not increment %s", metricName)
			}

			t.Logf("Full call + RTP rate=%d: offered_cps=%.1f, sip_pps=%.0f, rtp_pps_p95=%.0f, rtp_packets=%.0f, ser=%.1f%%, loss=%.2f%%, cpu_p95=%.2f%% quota (%.3f cores), mem_p99=%.1fMB",
				rate, result.Generator.ActualRate, result.ActualPPS, result.RTPPPSP95,
				rtpPackets, ser, result.LossRate*100,
				result.Resources.CPUP95Percent,
				result.Resources.CPUP95Percent*result.Resources.Limits.CPUCores/100,
				result.Resources.WorkingSetP99MB)

			metrics := resourceMetricEntries(result.Resources)
			for name, metric := range map[string]MetricEntry{
				"actual_pps":    {Value: result.ActualPPS, Unit: "pps", Direction: dirHigherIsBetter},
				"generator_cps": {Value: result.Generator.ActualRate, Unit: "cps", Direction: dirHigherIsBetter},
				"loss_rate":     {Value: result.LossRate * 100, Unit: "%", Direction: dirLowerIsBetter},
				"ser":           {Value: ser, Unit: "%", Direction: dirHigherIsBetter},
				"rtp_packets":   {Value: rtpPackets, Unit: "count", Direction: dirHigherIsBetter},
				"rtp_pps_p95":   {Value: result.RTPPPSP95, Unit: "pps", Direction: dirHigherIsBetter},
				"cpu_cores_p95": {
					Value: result.Resources.CPUP95Percent * result.Resources.Limits.CPUCores / 100,
					Unit:  "cores", Direction: dirLowerIsBetter,
				},
			} {
				metrics[name] = metric
			}
			recordResult(t, metrics)
		})
	}
}

// TestBenchmarkMemoryPerRTPStream measures memory overhead per active RTP
// stream. Each concurrent SIPp call produces 2 RTP streams (UAC→UAS and
// UAS→UAC). The -l flag limits concurrent calls; RTP streams persist for the
// tracker TTL after the call ends, so the gauge reflects both directions.
func TestBenchmarkMemoryPerRTPStream(t *testing.T) {
	limits := []int{0, 50, 100, 200, 500}
	type streamMeasurement struct {
		streams int
		memMB   float64
	}

	measurements := make([]streamMeasurement, 0, len(limits))

	for _, limit := range limits {
		t.Run(fmt.Sprintf("streams_%d", limit), func(t *testing.T) {
			beginScenario(t)
			env := newRTPTestEnvWithLimits(t.Context(), t, diagnosticMemoryLimits)
			recordMetricsSnapshot(t, "metrics-before.prom", env.endpoint)

			var streams float64
			if limit > 0 {
				// Each call lives ~18s (RTP streaming pause) → concurrent = rate × 18.
				rate := limit / rtpMediaSeconds
				if rate < 10 {
					rate = 10
				}
				callCount := limit * 3

				ctx, cancel := context.WithTimeout(t.Context(), 300*time.Second)
				defer cancel()

				uasPath := absScenarioPath(t, "uas_rtp.xml")
				sippVol := filepath.Dir(uasPath)
				uasContainer := startSippContainer(ctx, t,
					[]string{
						"-sf", "/scenarios/uas_rtp.xml",
						"-i", "127.0.0.1",
						"-mi", "127.0.0.1",
						"-p", env.sippPort,
						"-mp", env.uasMediaPort,
						"-m", strconv.Itoa(callCount),
						"-nr", "-nostdin",
					},
					sippVol, "", false,
				)

				waitForSIPpUDPReady(ctx, t, uasContainer, env.sippPort)

				uacPath := absScenarioPath(t, "uac_rtp.xml")
				sippVol = filepath.Dir(uacPath)
				uacContainer := startSippContainer(ctx, t,
					[]string{
						"-sf", "/scenarios/uac_rtp.xml",
						"-i", "127.0.0.1",
						"-mi", "127.0.0.1",
						"-p", env.sippClientPort,
						"-mp", env.uacMediaPort,
						"-m", strconv.Itoa(callCount),
						"-r", strconv.Itoa(rate),
						"-l", strconv.Itoa(limit),
						"-nr",
						"127.0.0.1:" + env.sippPort,
					},
					sippVol, "generator", false,
				)

				// Each call = 2 RTP streams (both directions).
				targetStreams := float64(limit) * 2 * 0.8
				require.Eventually(t, func() bool {
					if !metricExists(t, env.endpoint, "sip_exporter_rtp_active_streams") {
						return false
					}
					streams = getMetricSum(t, env.endpoint, "sip_exporter_rtp_active_streams")
					return streams >= targetStreams
				}, contextTimeout(t, ctx), 500*time.Millisecond,
					"RTP streams did not reach %.0f (got %.0f)", targetStreams, streams)

				resources := measureSteadySnapshot(ctx, t, env)
				memMB := resources.WorkingSetP99MB
				recordMetricsSnapshot(t, "metrics-after.prom", env.endpoint)
				waitForContainerExit(ctx, t, uacContainer)
				uacContainer.recordEvidence(ctx, t, time.Now())

				t.Logf("Streams: limit=%d, actual_streams=%.0f, mem=%.1fMB",
					limit, streams, memMB)

				measurements = append(measurements, streamMeasurement{
					streams: int(streams),
					memMB:   memMB,
				})

				metrics := resourceMetricEntries(resources)
				metrics["streams"] = MetricEntry{
					Value: streams, Unit: "count", Direction: dirHigherIsBetter,
				}
				recordResult(t, metrics)

				waitForContainerExit(ctx, t, uasContainer)
			} else {
				resources := measureSteadySnapshot(t.Context(), t, env)
				memMB := resources.WorkingSetP99MB
				recordMetricsSnapshot(t, "metrics-after.prom", env.endpoint)
				t.Logf("Baseline (no traffic): %.1f MB", memMB)
				measurements = append(measurements, streamMeasurement{
					streams: 0,
					memMB:   memMB,
				})

				recordResult(t, resourceMetricEntries(resources))
			}
		})
	}

	t.Run("summary", func(t *testing.T) {
		if len(measurements) < 2 {
			t.Fatal("need at least baseline + 1 measurement")
		}

		baseline := measurements[0].memMB
		t.Logf("=== Memory Per RTP Stream Summary ===")
		t.Logf("Baseline: %.1f MB (no streams)", baseline)
		t.Logf("%-12s %-12s %-12s %-12s", "Streams", "Total MB", "Delta MB", "Bytes/stream")

		for _, m := range measurements[1:] {
			if m.streams == 0 {
				continue
			}
			delta := (m.memMB - baseline) * 1024 * 1024
			bytesPerStream := delta / float64(m.streams)
			t.Logf("%-12d %-12.1f %-12.1f %-12.0f",
				m.streams, m.memMB, m.memMB-baseline, bytesPerStream)
		}
	})
}
