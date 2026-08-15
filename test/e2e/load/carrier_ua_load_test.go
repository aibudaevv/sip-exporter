//go:build e2e

package load

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	dualUAPacketsPerCall = 7.0
	dualUATestTimeout    = 30 * time.Second
)

func TestLoadDualUAType(t *testing.T) {
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

	rates := []int{500, 1000, 1800}
	for _, rate := range rates {
		t.Run(fmt.Sprintf("rate_%d", rate), func(t *testing.T) {
			beginScenario(t)
			env := newTestEnvWithCarrierAndUA(t.Context(), t, carriersYAML, userAgentsYAML)

			ctx, cancel := context.WithTimeout(t.Context(), dualUATestTimeout)
			defer cancel()

			callCountPerType := rate * 5
			totalCallCount := callCountPerType * 2
			expectedTotal := float64(totalCallCount) * dualUAPacketsPerCall

			measurement, measurementErr := newSteadyMeasurement(ctx, env)
			require.NoError(t, measurementErr)

			recordMetricsSnapshot(t, "metrics-before.prom", env.endpoint)
			protocolsBefore := readProtocolCounters(t, env.endpoint)
			packetsBefore := protocolsBefore.SIPPackets
			errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

			uasPath := absScenarioPath(t, "call_highrate_uas.xml")
			sippVol := filepath.Dir(uasPath)
			uasFile := "call_highrate_uas.xml"

			uasYealink := startSippContainer(ctx, t,
				[]string{"-sf", "/scenarios/" + uasFile, "-i", "127.0.0.1", "-p", env.sippPort,
					"-m", strconv.Itoa(callCountPerType), "-nr", "-nostdin"},
				sippVol, "", false,
			)

			uasGrandstream := startSippContainer(ctx, t,
				[]string{"-sf", "/scenarios/" + uasFile, "-i", "127.0.0.1", "-p", env.sippPort2,
					"-m", strconv.Itoa(callCountPerType), "-nr", "-nostdin"},
				sippVol, "", false,
			)

			waitForSIPpUDPReady(ctx, t, uasYealink, env.sippPort)
			waitForSIPpUDPReady(ctx, t, uasGrandstream, env.sippPort2)
			measureStart := time.Now()
			require.NoError(t, measurement.Begin(ctx, measureStart))

			yealinkUacPath := absScenarioPath(t, "call_highrate_yealink_uac.xml")
			yealinkVol := filepath.Dir(yealinkUacPath)

			yealinkUAC := startSippContainer(ctx, t,
				[]string{"-sf", "/scenarios/call_highrate_yealink_uac.xml",
					"-i", "127.0.0.1", "-p", env.sippClientPort,
					"-m", strconv.Itoa(callCountPerType), "-r", strconv.Itoa(rate),
					"-cid_str", nextSippCallIDFormat(),
					"-nr",
					"127.0.0.1:" + env.sippPort},
				yealinkVol, "generator-yealink", true,
			)

			grandstreamUacPath := absScenarioPath(t, "call_highrate_grandstream_uac.xml")
			grandstreamVol := filepath.Dir(grandstreamUacPath)

			grandstreamUAC := startSippContainer(ctx, t,
				[]string{"-sf", "/scenarios/call_highrate_grandstream_uac.xml",
					"-i", "127.0.0.1", "-p", env.sippClientPort2,
					"-m", strconv.Itoa(callCountPerType), "-r", strconv.Itoa(rate),
					"-cid_str", nextSippCallIDFormat(),
					"-nr",
					"127.0.0.1:" + env.sippPort2},
				grandstreamVol, "generator-grandstream", true,
			)

			waitForContainerExit(ctx, t, yealinkUAC)
			waitForContainerExit(ctx, t, grandstreamUAC)
			measureEnd := time.Now()
			sippDuration := measureEnd.Sub(measureStart)
			resourceSummary := finishSteadyMeasurement(ctx, t, measurement, measureEnd)

			generatorPhases := PhaseTimestamps{
				WarmupStart: measureStart, Ready: measureStart, MeasureStart: measureStart,
				MeasureEnd: measureEnd, DrainEnd: measureEnd,
			}
			yealinkGenerator, generatorErr := yealinkUAC.readGeneratorEvidence(ctx, t, generatorPhases)
			require.NoError(t, generatorErr)
			yealinkEvidenceAt := time.Now()
			grandstreamGenerator, generatorErr := grandstreamUAC.readGeneratorEvidence(ctx, t, generatorPhases)
			require.NoError(t, generatorErr)
			grandstreamEvidenceAt := time.Now()
			waitForContainerExit(ctx, t, uasYealink)
			yealinkUASExitAt := time.Now()
			waitForContainerExit(ctx, t, uasGrandstream)
			grandstreamUASExitAt := time.Now()

			waitForExactSIPCapture(ctx, t, env.endpoint, packetsBefore, expectedTotal)
			drainAt := time.Now()
			yealinkGenerator.Phases.DrainEnd = drainAt
			grandstreamGenerator.Phases.DrainEnd = drainAt
			require.NoError(t, validatePostPhaseOrdering(
				measureEnd,
				yealinkEvidenceAt, grandstreamEvidenceAt,
				yealinkUASExitAt, grandstreamUASExitAt, drainAt,
			))

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
			lossRate := capture.LossPct / 100

			errorCount := errorsAfter - errorsBefore
			recordLoadResultEvidence(t, loadResult{
				Capture: capture, Protocols: protocols,
				Resources: resourceSummary,
			})
			if activeRunRecorder != nil {
				require.NoError(t, activeRunRecorder.AttachGenerator(t.Name(), yealinkGenerator))
				require.NoError(t, activeRunRecorder.AttachGenerator(t.Name(), grandstreamGenerator))
			}

			inviteYealink := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `ua_type="yealink"`)
			inviteGrandstream := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `ua_type="grandstream"`)
			serYealink := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", `ua_type="yealink"`)
			serGrandstream := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", `ua_type="grandstream"`)

			t.Logf("Dual UA rate=%d: actual=%.0f PPS, captured=%.0f, expected=%.0f, loss=%.2f%%, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB, errors=%.0f",
				rate, actualPPS, totalCaptured, expectedTotal, lossRate*100,
				resourceSummary.CPUP95Percent, resourceSummary.CPUP95Percent,
				resourceSummary.WorkingSetP99MB, errorCount)
			t.Logf("  Yealink: invites=%.0f, ser=%.2f%%", inviteYealink, serYealink)
			t.Logf("  Grandstream: invites=%.0f, ser=%.2f%%", inviteGrandstream, serGrandstream)

			totalPackets := totalCaptured
			maxErrors := totalPackets * 0.001
			require.LessOrEqual(t, errorCount, maxErrors,
				"error rate SLO: < 0.1%% of processed packets")
			require.Greater(t, packetsAfter, packetsBefore,
				"exporter should have processed packets")

			require.Greater(t, inviteYealink, float64(0),
				"Yealink INVITE count should be > 0")
			require.Greater(t, inviteGrandstream, float64(0),
				"Grandstream INVITE count should be > 0")

			require.GreaterOrEqual(t, serYealink, 49.0,
				"SER Yealink SLO: >= 49%% on loopback at rate %d (got %.2f%%)", rate, serYealink)
			require.GreaterOrEqual(t, serGrandstream, 49.0,
				"SER Grandstream SLO: >= 49%% on loopback at rate %d (got %.2f%%)", rate, serGrandstream)

			metrics := resourceMetricEntries(resourceSummary)
			for name, metric := range map[string]MetricEntry{
				"actual_pps":          {Value: actualPPS, Unit: "pps", Direction: dirHigherIsBetter},
				"loss_rate":           {Value: lossRate * 100, Unit: "%", Direction: dirLowerIsBetter},
				"ser_yealink":         {Value: serYealink, Unit: "%", Direction: dirHigherIsBetter},
				"ser_grandstream":     {Value: serGrandstream, Unit: "%", Direction: dirHigherIsBetter},
				"invites_yealink":     {Value: inviteYealink, Unit: "count", Direction: dirHigherIsBetter},
				"invites_grandstream": {Value: inviteGrandstream, Unit: "count", Direction: dirHigherIsBetter},
			} {
				metrics[name] = metric
			}
			recordResult(t, metrics)
		})
	}
}
