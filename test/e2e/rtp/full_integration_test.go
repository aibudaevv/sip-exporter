//go:build e2e

package rtp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRTP_BothDirections verifies that both legs of the media flow are captured.
// SIPp UAC and UAS each stream G.711a RTP from their own media port, so the
// exporter must track two distinct streams (keyed by media endpoint + SSRC).
// Streams persist for the tracker TTL after the call, so once the dialog
// completes the rtp_active_streams gauge reflects both directions.
func TestRTP_BothDirections(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, "0", true)

	runSippRTP(context.Background(), t, uasSIP, uacSIP, uasMedia, uacMedia)

	// Both UAC→UAS and UAS→UAC legs observed → at least two active PCMA streams.
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_active_streams") >= 2
	}, 15*time.Second, 500*time.Millisecond,
		"rtp_active_streams{codec=PCMA} must reflect both media directions (>=2)")
}

// Configs used by the full-integration test: the SIPp loopback traffic
// (127.0.0.1, User-Agent "sipp-rtp-*") is mapped to concrete carrier/ua labels
// so the labelled RTP and SIP metrics can be asserted precisely.
const (
	integrationCarriersYAML   = "carriers:\n  - name: loopback\n    cidrs:\n      - \"127.0.0.0/8\"\n"
	integrationUserAgentsYAML = "user_agents:\n  - regex: '(?i)^SIPp'\n    label: sipp\n"

	// Label values emitted for the SIPp dialog.
	labelCarrier = `carrier="loopback"`
	labelUAType  = `ua_type="sipp"`
	labelCodec   = `codec="PCMA"`
)

// TestRTP_FullIntegration_MetricsVerified drives a complete SIP dialog with real
// G.711a RTP through the exporter (carrier+UA config mounted) and asserts:
//   - all RTP metrics are present with the concrete carrier/ua_type/codec labels
//     (packets, jitter, MOS, active_streams; loss is 0 on clean G.711a), and
//   - the SIP signalling path still produces correct metrics with RTP capture ON
//     (SIP regression: INVITE counted, SER ≈ 100%).
func TestRTP_FullIntegration_MetricsVerified(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	endpoint := startExporterWithCarrierUA(context.Background(), t, httpPort, uasSIP, "0",
		integrationCarriersYAML, integrationUserAgentsYAML)

	runSippRTP(context.Background(), t, uasSIP, uacSIP, uasMedia, uacMedia)

	// --- RTP metrics with concrete labels ---
	rtpLabels := []string{labelCarrier, labelUAType, labelCodec}

	// Packets counter (cumulative) must be observed for the dialog's labels.
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtp_packets_total", rtpLabels...) > 0
	}, 10*time.Second, 500*time.Millisecond, "rtp_packets_total must be observed with labels")

	// No spurious loss on clean G.711a. The lost counter is only created on the
	// first detected gap, so for clean traffic the series is absent and the
	// scraped value is 0; asserting 0 verifies the loss algorithm does not
	// miscount on a lossless stream.
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtp_packets_lost_total", rtpLabels...) == 0
	}, 10*time.Second, 500*time.Millisecond, "no RTP loss should be detected on clean G.711a")

	// Jitter histogram (emitted by the 1s snapshot).
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtp_jitter_milliseconds_count", rtpLabels...) > 0
	}, 10*time.Second, 500*time.Millisecond, "rtp_jitter histogram must have samples")

	// MOS histogram + sane E-model range for clean G.711 (~3.9-4.4).
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtp_mos_score_count", rtpLabels...) > 0
	}, 10*time.Second, 500*time.Millisecond, "rtp_mos histogram must have samples")
	mosSum := getMetricByLabel(t, endpoint, "sip_exporter_rtp_mos_score_sum", rtpLabels...)
	mosCount := getMetricByLabel(t, endpoint, "sip_exporter_rtp_mos_score_count", rtpLabels...)
	require.Greater(t, mosCount, 0.0)
	avgMOS := mosSum / mosCount
	t.Logf("Full integration RTP: avg MOS=%.2f (carrier=loopback, ua_type=sipp, codec=PCMA)", avgMOS)
	require.Greater(t, avgMOS, 3.5, "clean G.711 MOS should be > 3.5")
	require.Less(t, avgMOS, 4.6)

	// Active streams gauge: both media directions tracked.
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtp_active_streams", rtpLabels...) >= 2
	}, 15*time.Second, 500*time.Millisecond, "rtp_active_streams must reflect both directions")

	// --- SIP regression: signalling metrics correct with RTP capture ON ---
	sipLabels := []string{labelCarrier, labelUAType}

	// INVITE request counted for the dialog's labels.
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_invite_total", sipLabels...) >= 1
	}, 10*time.Second, 500*time.Millisecond, "invite_total must be counted")

	// SER = (INVITE→200 OK)/(INVITE - INVITE→3xx)*100 = 100 for one successful
	// call. RTP capture being enabled must not break SIP metric computation.
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_ser", sipLabels...) >= 99.0
	}, 10*time.Second, 500*time.Millisecond, "SER must be ~100%% (RTP capture must not break SIP metrics)")
}
