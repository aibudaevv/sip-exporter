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
	dualUAPacketsPerCall = 14.0
	dualUATestTimeout    = 30 * time.Second
)

func TestLoad_DualUAType(t *testing.T) {
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
			env := newTestEnvWithCarrierAndUA(context.Background(), t, carriersYAML, userAgentsYAML)

			ctx, cancel := context.WithTimeout(context.Background(), dualUATestTimeout)
			defer cancel()

			callCountPerType := rate * 5
			totalCallCount := callCountPerType * 2

			stats, statsErr := newStatsCollector(env.exporterContainer.GetContainerID())
			require.NoError(t, statsErr)

			statsCtx, statsCancel := context.WithCancel(ctx)
			stats.start(statsCtx, env.exporterContainer.GetContainerID())

			packetsBefore := getMetric(t, env.endpoint, "sip_exporter_packets_total")
			errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

			start := time.Now()

			uasPath := absScenarioPath(t, "call_highrate_uas.xml")
			sippVol := filepath.Dir(uasPath)
			uasFile := "call_highrate_uas.xml"

			uasYealink := startSippContainer(ctx, t,
				[]string{"-sf", "/scenarios/" + uasFile, "-i", "127.0.0.1", "-p", env.sippPort,
					"-m", strconv.Itoa(callCountPerType), "-nostdin"},
				sippVol, false,
			)

			uasGrandstream := startSippContainer(ctx, t,
				[]string{"-sf", "/scenarios/" + uasFile, "-i", "127.0.0.1", "-p", env.sippPort2,
					"-m", strconv.Itoa(callCountPerType), "-nostdin"},
				sippVol, false,
			)

			time.Sleep(500 * time.Millisecond)

			yealinkUacPath := absScenarioPath(t, "call_highrate_yealink_uac.xml")
			yealinkVol := filepath.Dir(yealinkUacPath)

			startSippContainer(ctx, t,
				[]string{"-sf", "/scenarios/call_highrate_yealink_uac.xml",
					"-i", "127.0.0.1", "-p", env.sippClientPort,
					"-m", strconv.Itoa(callCountPerType), "-r", strconv.Itoa(rate),
					"127.0.0.1:" + env.sippPort},
				yealinkVol, true,
			)

			grandstreamUacPath := absScenarioPath(t, "call_highrate_grandstream_uac.xml")
			grandstreamVol := filepath.Dir(grandstreamUacPath)

			startSippContainer(ctx, t,
				[]string{"-sf", "/scenarios/call_highrate_grandstream_uac.xml",
					"-i", "127.0.0.1", "-p", env.sippClientPort2,
					"-m", strconv.Itoa(callCountPerType), "-r", strconv.Itoa(rate),
					"127.0.0.1:" + env.sippPort2},
				grandstreamVol, true,
			)

			waitForContainerExit(ctx, t, uasYealink)
			waitForContainerExit(ctx, t, uasGrandstream)

			sippEnd := time.Now()
			sippDuration := sippEnd.Sub(start)

			waitForMetricStable(t, env.endpoint)

			statsCancel()
			cpuAvg, cpuPeak, memMaxMB := stats.stop()

			packetsAfter := getMetric(t, env.endpoint, "sip_exporter_packets_total")
			errorsAfter := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

			totalCaptured := packetsAfter - packetsBefore
			actualPPS := 0.0
			if sippDuration.Seconds() > 0 {
				actualPPS = totalCaptured / sippDuration.Seconds()
			}
			expectedTotal := float64(totalCallCount) * dualUAPacketsPerCall
			lossRate := 0.0
			if expectedTotal > 0 {
				lossRate = 1 - totalCaptured/expectedTotal
				if lossRate < 0 {
					lossRate = 0
				}
			}

			errorCount := errorsAfter - errorsBefore

			inviteYealink := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `ua_type="yealink"`)
			inviteGrandstream := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `ua_type="grandstream"`)
			serYealink := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", `ua_type="yealink"`)
			serGrandstream := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", `ua_type="grandstream"`)

			t.Logf("Dual UA rate=%d: actual=%.0f PPS, captured=%.0f, expected=%.0f, loss=%.2f%%, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB, errors=%.0f",
				rate, actualPPS, totalCaptured, expectedTotal, lossRate*100, cpuAvg, cpuPeak, memMaxMB, errorCount)
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

			recordResult(t.Name(), map[string]MetricEntry{
				"actual_pps":          {Value: actualPPS, Unit: "pps", Direction: dirHigherIsBetter},
				"loss_rate":           {Value: lossRate * 100, Unit: "%", Direction: dirLowerIsBetter},
				"ser_yealink":         {Value: serYealink, Unit: "%", Direction: dirHigherIsBetter},
				"ser_grandstream":     {Value: serGrandstream, Unit: "%", Direction: dirHigherIsBetter},
				"invites_yealink":     {Value: inviteYealink, Unit: "count", Direction: dirHigherIsBetter},
				"invites_grandstream": {Value: inviteGrandstream, Unit: "count", Direction: dirHigherIsBetter},
				"cpu_peak":            {Value: cpuPeak, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_avg":             {Value: cpuAvg, Unit: "%", Direction: dirLowerIsBetter},
				"mem_mb":              {Value: memMaxMB, Unit: "MB", Direction: dirLowerIsBetter},
			})
		})
	}
}
