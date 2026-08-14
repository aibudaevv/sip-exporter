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
)

// allocateRTPPorts reserves a port block wide enough for SIPp media port
// increment (SIPp increments -mp by 2 per concurrent call: RTP+RTCP pair).
// Layout: [0]=HTTP [1]=UAS-SIP [2]=UAC-SIP [3]=UAS-media [1003]=UAC-media.
// 1000-port gap between media bases covers up to 500 concurrent calls.
func allocateRTPPorts() (http, uasSIP, uacSIP, uasMedia, uacMedia string) {
	portMu.Lock()
	defer portMu.Unlock()
	base := nextBasePort
	nextBasePort += 2004
	return strconv.Itoa(base), strconv.Itoa(base + 1), strconv.Itoa(base + 2),
		strconv.Itoa(base + 3), strconv.Itoa(base + 1003)
}

// newRTPTestEnv starts the exporter with RTP capture enabled and allocates
// separate media ports for SIPp's -mp flag.
func newRTPTestEnv(ctx context.Context, t *testing.T) *testEnv {
	t.Helper()

	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := allocateRTPPorts()

	req := exporterContainerRequest(ctx, t, testInterface, httpPort, uasSIP)

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

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
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

	stats, statsErr := newStatsCollector(env.exporterContainer.GetContainerID())
	require.NoError(t, statsErr)

	statsCtx, statsCancel := context.WithCancel(ctx)
	stats.start(statsCtx, env.exporterContainer.GetContainerID())

	packetsBefore := getMetric(t, env.endpoint, "sip_exporter_packets_total")
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	start := time.Now()

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
			"-nr",
			"-nostdin",
		},
		sippVol, false,
	)

	time.Sleep(500 * time.Millisecond)

	uacPath := absScenarioPath(t, "uac_rtp.xml")
	sippVol = filepath.Dir(uacPath)

	startSippContainer(ctx, t,
		[]string{
			"-sf", "/scenarios/uac_rtp.xml",
			"-i", "127.0.0.1",
			"-mi", "127.0.0.1",
			"-p", env.sippClientPort,
			"-mp", env.uacMediaPort,
			"-m", strconv.Itoa(callCount),
			"-r", strconv.Itoa(rate),
			"-nr",
			"127.0.0.1:" + env.sippPort,
		},
		sippVol, true,
	)

	waitForContainerExit(ctx, t, uasContainer)

	sippEnd := time.Now()
	sippDuration := sippEnd.Sub(start)

	waitForMetricStable(ctx, t, env.endpoint)

	stableTime := time.Now()
	drainTime := stableTime.Sub(sippEnd)

	statsCancel()
	cpuAvg, cpuPeak, memMaxMB := stats.stop()

	packetsAfter := getMetric(t, env.endpoint, "sip_exporter_packets_total")
	errorsAfter := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	totalCaptured := packetsAfter - packetsBefore
	actualPPS := 0.0
	if sippDuration.Seconds() > 0 {
		actualPPS = totalCaptured / sippDuration.Seconds()
	}
	expectedTotal := float64(callCount) * rtpSipPacketsPerCall
	expectedPPS := float64(rate) * rtpSipPacketsPerCall
	lossRate := 0.0
	if expectedTotal > 0 {
		lossRate = 1 - totalCaptured/expectedTotal
		if lossRate < 0 {
			lossRate = 0
		}
	}

	result := loadResult{
		Duration:      sippDuration,
		PacketsBefore: packetsBefore,
		PacketsAfter:  packetsAfter,
		ActualPPS:     actualPPS,
		ExpectedPPS:   expectedPPS,
		LossRate:      lossRate,
		ErrorCount:    errorsAfter - errorsBefore,
		DrainTime:     drainTime,
		CPUAvg:        cpuAvg,
		CPUPeak:       cpuPeak,
		MemMaxMB:      memMaxMB,
	}

	t.Logf("RTP load: actual=%.0f PPS, captured=%.0f, expected=%.0f, loss=%.2f%%, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB, errors=%.0f",
		result.ActualPPS, totalCaptured, expectedTotal, result.LossRate*100, result.DrainTime,
		result.CPUAvg, result.CPUPeak, result.MemMaxMB, result.ErrorCount)

	return result
}

// TestLoadFullCallWithRTP measures combined SIP+RTP throughput. Each call
// runs a full SIP dialog (INVITE→200→ACK→BYE→200) plus 4s of G.711a RTP media
// in both directions. Rates are 10× lower than SIP-only tests because RTP adds
// ~400 packets per call (2 × 50pps × 4s).
func TestLoadFullCallWithRTP(t *testing.T) {
	rates := []int{10, 25, 50, 100}
	for _, rate := range rates {
		t.Run(fmt.Sprintf("rate_%d", rate), func(t *testing.T) {
			env := newRTPTestEnv(t.Context(), t)

			ctx, cancel := context.WithTimeout(t.Context(), rtpLoadTimeout)
			defer cancel()

			callCount := rate * 5
			result := runSippRTPLoad(ctx, t, callCount, rate, env)

			totalPackets := result.PacketsAfter - result.PacketsBefore
			maxErrors := totalPackets * 0.001
			require.LessOrEqual(t, result.ErrorCount, maxErrors,
				"error rate SLO: < 0.1%% of processed packets")
			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets")

			ser := getMetric(t, env.endpoint, "sip_exporter_ser")
			require.GreaterOrEqual(t, ser, 99.0,
				"SER SLO: >= 99%% with RTP capture enabled (got %.2f%%)", ser)

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_rtp_packets_total"),
				"rtp_packets_total must have at least one series")
			rtpPackets := getMetricSum(t, env.endpoint, "sip_exporter_rtp_packets_total")
			require.Greater(t, rtpPackets, 0.0,
				"RTP packets must be captured")

			t.Logf("Full call + RTP rate=%d: actual=%.0f PPS, rtp_packets=%.0f, ser=%.1f%%, loss=%.2f%%, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB",
				rate, result.ActualPPS, rtpPackets, ser, result.LossRate*100,
				result.CPUAvg, result.CPUPeak, result.MemMaxMB)

			recordResult(t.Name(), map[string]MetricEntry{
				"actual_pps":  {Value: result.ActualPPS, Unit: "pps", Direction: dirHigherIsBetter},
				"loss_rate":   {Value: result.LossRate * 100, Unit: "%", Direction: dirLowerIsBetter},
				"ser":         {Value: ser, Unit: "%", Direction: dirHigherIsBetter},
				"cpu_peak":    {Value: result.CPUPeak, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_avg":     {Value: result.CPUAvg, Unit: "%", Direction: dirLowerIsBetter},
				"mem_mb":      {Value: result.MemMaxMB, Unit: "MB", Direction: dirLowerIsBetter},
				"rtp_packets": {Value: rtpPackets, Unit: "count", Direction: dirHigherIsBetter},
			})
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
			env := newRTPTestEnv(t.Context(), t)

			var streams float64
			if limit > 0 {
				// Each call lives ~4s (RTP streaming pause) → concurrent = rate × 4.
				rate := limit / 4
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
					sippVol, false,
				)

				time.Sleep(500 * time.Millisecond)

				uacPath := absScenarioPath(t, "uac_rtp.xml")
				sippVol = filepath.Dir(uacPath)
				startSippContainer(ctx, t,
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
					sippVol, false,
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

				memMB := getSingleMemSample(t, env.exporterContainer.GetContainerID())

				t.Logf("Streams: limit=%d, actual_streams=%.0f, mem=%.1fMB",
					limit, streams, memMB)

				measurements = append(measurements, streamMeasurement{
					streams: int(streams),
					memMB:   memMB,
				})

				recordResult(t.Name(), map[string]MetricEntry{
					"streams": {Value: streams, Unit: "count", Direction: dirHigherIsBetter},
					"mem_mb":  {Value: memMB, Unit: "MB", Direction: dirLowerIsBetter},
				})

				waitForContainerExit(ctx, t, uasContainer)
			} else {
				memMB := getSingleMemSample(t, env.exporterContainer.GetContainerID())
				t.Logf("Baseline (no traffic): %.1f MB", memMB)
				measurements = append(measurements, streamMeasurement{
					streams: 0,
					memMB:   memMB,
				})

				recordResult(t.Name(), map[string]MetricEntry{
					"mem_mb": {Value: memMB, Unit: "MB", Direction: dirLowerIsBetter},
				})
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
