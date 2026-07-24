//go:build e2e

package rtp

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRTP_BothDirections verifies that both legs of the media flow are captured.
// A SIPp dialog establishes SDP-registered endpoints, then sendControlledRTP
// sends deterministic RTP to both media ports. The exporter must track two
// distinct streams (keyed by media endpoint + SSRC).
func TestRTP_BothDirections(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uasMediaNum, _ := strconv.Atoi(uasMedia)
	uacMediaNum, _ := strconv.Atoi(uacMedia)
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, testInterface, "")

	wait := startSippContainers(context.Background(), t,
		"uas_nortp.xml", "uac_nortp.xml", uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_invite_total") >= 1
	}, 10*time.Second, 200*time.Millisecond, "dialog must be established")

	sendControlledRTP(t, uacMediaNum, []uint16{1, 2, 3, 4, 5})
	sendControlledRTP(t, uasMediaNum, []uint16{1, 2, 3, 4, 5})

	// Both UAC→UAS and UAS→UAC legs observed → at least two active PCMA streams.
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_active_streams") >= 2
	}, 15*time.Second, 500*time.Millisecond,
		"rtp_active_streams{codec=PCMA} must reflect both media directions (>=2)")

	wait()
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

// TestRTP_FullIntegration_MetricsVerified drives a complete SIP dialog with
// controlled RTP through the exporter (carrier+UA config mounted) and asserts:
//   - all RTP metrics are present with the concrete carrier/ua_type/codec labels
//     (packets, jitter, MOS, active_streams; loss is 0 on clean G.711a), and
//   - the SIP signalling path still produces correct metrics with RTP capture ON
//     (SIP regression: INVITE counted, SER ≈ 100%).
//
// Uses sendControlledRTP (deterministic) instead of SIPp's rtp_stream (which
// intermittently fails to start in one direction).
func TestRTP_FullIntegration_MetricsVerified(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uasMediaNum, _ := strconv.Atoi(uasMedia)
	uacMediaNum, _ := strconv.Atoi(uacMedia)
	endpoint := startExporterWithCarrierUA(context.Background(), t, httpPort, uasSIP,
		integrationCarriersYAML, integrationUserAgentsYAML, "")

	wait := startSippContainers(context.Background(), t,
		"uas_nortp.xml", "uac_nortp.xml", uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) >= 1
	}, 10*time.Second, 200*time.Millisecond, "dialog must be established")

	// Send RTP in both directions (deterministic, unlike SIPp's rtp_stream).
	sendControlledRTP(t, uacMediaNum, []uint16{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	sendControlledRTP(t, uasMediaNum, []uint16{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})

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

	wait()

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

// TestRTP_StreamExpiry verifies the RFC 3550 §6.3.5 idle-timeout end-to-end:
// with a short SIP_EXPORTER_RTP_STREAM_TTL (2s) the rtp_active_streams gauge
// rises to >=2 during the SIPp dialog, then falls back to 0 once the streams
// have been idle past the TTL and the 1s snapshot cycle has run Cleanup().
// A hardcoded 30s TTL previously made this path too slow to cover on e2e.
func TestRTP_StreamExpiry(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, testInterface, "2s")

	runSippRTP(context.Background(), t, uasSIP, uacSIP, uasMedia, uacMedia)

	// During the call both media directions are tracked.
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_active_streams") >= 2
	}, 15*time.Second, 500*time.Millisecond,
		"rtp_active_streams must reflect both media directions during the call")

	// After the idle TTL (2s) + the 1s snapshot cycle, streams expire and the
	// gauge returns to 0 (the background ticker runs Cleanup() every second).
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_active_streams") == 0
	}, 12*time.Second, 500*time.Millisecond,
		"rtp_active_streams must drop to 0 after SIP_EXPORTER_RTP_STREAM_TTL idle window")
}
