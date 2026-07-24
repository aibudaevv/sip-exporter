//go:build e2e

package rtp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// metricLineExists scrapes /metrics and returns true when a sample line
// matching metricName AND all labelSubstrings is present. This distinguishes
// "gauge present with value 0" from "gauge absent" — the core of the Reset() fix.
func metricLineExists(t *testing.T, endpoint, metricName string, labelSubstrings ...string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/metrics", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	prefix := metricName + "{"
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		matches := true
		for _, s := range labelSubstrings {
			if !strings.Contains(trimmed, s) {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

// TestRTP_MetricsAfterCallCompletion verifies that after a SIPp dialog with RTP
// completes (BYE processed), ALL RTP metrics are in the correct post-call state:
//
//   - rtp_active_streams gauge is PRESENT at 0 (not absent — the Reset() bug
//     previously made it disappear, leaving stale data in Prometheus).
//   - sessions gauge is PRESENT at 0 (same Reset() issue).
//   - Cumulative counters and histograms retain their accumulated values.
//   - active_dialogs is 0.
func TestRTP_MetricsAfterCallCompletion(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	endpoint := startExporterWithCarrierUA(context.Background(), t, httpPort, uasSIP,
		integrationCarriersYAML, integrationUserAgentsYAML, "")

	runSippRTP(context.Background(), t, uasSIP, uacSIP, uasMedia, uacMedia)

	rtpLabels := []string{labelCarrier, labelUAType, labelCodec}
	sipLabels := []string{labelCarrier, labelUAType}

	// Phase 1: wait for the call to be processed (packets observed).
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtp_packets_total", rtpLabels...) > 0
	}, 15*time.Second, 500*time.Millisecond, "rtp_packets_total must be observed")

	// Phase 2: wait for BYE to be processed — active streams drop to 0.
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtp_active_streams", rtpLabels...) == 0
	}, 15*time.Second, 500*time.Millisecond, "rtp_active_streams must drop to 0 after BYE")

	// Allow the 1-second periodic metrics cycle to run after streams hit 0.
	time.Sleep(2 * time.Second)

	// --- Assert: gauges are PRESENT at 0, not absent (the Reset() fix) ---
	require.True(t,
		metricLineExists(t, endpoint, "sip_exporter_rtp_active_streams", rtpLabels...),
		"rtp_active_streams gauge must be present at 0 (not absent after Reset)")
	require.True(t,
		metricLineExists(t, endpoint, "sip_exporter_sessions", sipLabels...),
		"sessions gauge must be present at 0 (not absent after Reset)")

	// --- Assert: gauge values are exactly 0 ---
	require.InDelta(t, 0.0,
		getMetricByLabel(t, endpoint, "sip_exporter_rtp_active_streams", rtpLabels...), 0.01)
	require.InDelta(t, 0.0,
		getMetricByLabel(t, endpoint, "sip_exporter_sessions", sipLabels...), 0.01)

	// --- Assert: cumulative counter retains data ---
	require.Greater(t,
		getMetricByLabel(t, endpoint, "sip_exporter_rtp_packets_total", rtpLabels...), 0.0,
		"rtp_packets_total must retain accumulated count after call ends")

	// --- Assert: histograms retain samples ---
	require.Greater(t,
		getMetricByLabel(t, endpoint, "sip_exporter_rtp_jitter_milliseconds_count", rtpLabels...), 0.0,
		"rtp_jitter histogram must retain samples after call ends")
	require.Greater(t,
		getMetricByLabel(t, endpoint, "sip_exporter_rtp_mos_score_count", rtpLabels...), 0.0,
		"rtp_mos histogram must retain samples after call ends")

	// --- Assert: no spurious loss on clean G.711a ---
	require.InDelta(t, 0.0,
		getMetricByLabel(t, endpoint, "sip_exporter_rtp_packets_lost_total", rtpLabels...), 0.01,
		"no RTP loss expected on clean G.711a stream")

	// --- Assert: dialogs cleaned up ---
	require.InDelta(t, 0.0,
		getMetricByLabel(t, endpoint, "sip_exporter_active_dialogs"), 0.01)
}
