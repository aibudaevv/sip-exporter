//go:build e2e

package rtp

import (
	"context"
	"encoding/binary"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// sendControlledRTP sends RTP packets with specific sequence numbers to
// 127.0.0.1:port. The port must already be bound by SIPp's -mp so packets
// complete the loopback receive cycle (captured by the exporter's AF_PACKET
// socket with PACKET_IGNORE_OUTGOING). SSRC is fixed so the media tracker
// creates a single stream entry.
func sendControlledRTP(t *testing.T, port int, seqNums []uint16) {
	t.Helper()

	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	sender, err := net.DialUDP("udp4", nil, addr)
	require.NoError(t, err)
	defer sender.Close()

	pkt := make([]byte, 28)
	pkt[0] = 0x80 // V=2, P=0, X=0, CC=0
	pkt[1] = 0x08 // M=0, PT=8 (PCMA)
	binary.BigEndian.PutUint32(pkt[4:8], 160)
	binary.BigEndian.PutUint32(pkt[8:12], 0x53535243) // SSRC

	for _, seq := range seqNums {
		binary.BigEndian.PutUint16(pkt[2:4], seq)
		_, _ = sender.Write(pkt)
		time.Sleep(5 * time.Millisecond)
	}
}

// TestRTP_QualityMetrics_Baseline verifies that clean G.711a RTP produces
// r_factor, mos_f1, mos_f2, and mos_adaptive histograms with sane values.
func TestRTP_QualityMetrics_Baseline(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, "0", testInterface, true, "")

	runSippRTP(context.Background(), t, uasSIP, uacSIP, uasMedia, uacMedia)

	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_r_factor_count") > 0
	}, 10*time.Second, 500*time.Millisecond, "r_factor histogram must have samples")

	rAvg := avgHistogramValue(t, endpoint, "sip_exporter_rtp_r_factor")
	t.Logf("Baseline: avg R-factor=%.1f (clean G.711)", rAvg)
	require.Greater(t, rAvg, 85.0, "clean G.711 R-factor should be >85")

	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_mos_f1_count") > 0
	}, 10*time.Second, 500*time.Millisecond, "mos_f1 must have samples")

	f1Avg := avgHistogramValue(t, endpoint, "sip_exporter_rtp_mos_f1")
	f2Avg := avgHistogramValue(t, endpoint, "sip_exporter_rtp_mos_f2")
	adaptAvg := avgHistogramValue(t, endpoint, "sip_exporter_rtp_mos_adaptive")
	t.Logf("Baseline MOS: f1=%.2f f2=%.2f adaptive=%.2f", f1Avg, f2Avg, adaptAvg)
	for _, v := range []float64{f1Avg, f2Avg, adaptAvg} {
		require.Greater(t, v, 3.0, "clean MOS variant should be >3.0")
	}
}

// TestRTP_QualityMetrics_Degraded verifies that degraded RTP produces a lower
// R-factor and all MOS variant histograms are populated. The ordering assertion
// (f1 ≤ f2 ≤ adaptive) is mathematically guaranteed by the E-model formula;
// strict separation requires jitter > 50ms (F1's JB threshold), which netem at
// this scale does not reach. Strict f1 < f2 differentiation is covered by unit
// tests with controlled jitter inputs.
func TestRTP_QualityMetrics_Degraded(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, "0", testInterface, true, "")

	applyNetem(t, []string{"delay", "30ms", "10ms", "loss", "30%"}, uasMedia, uacMedia)
	runSippRTP(context.Background(), t, uasSIP, uacSIP, uasMedia, uacMedia)

	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_r_factor_count") > 0
	}, 15*time.Second, 500*time.Millisecond, "r_factor must have samples under netem")

	rAvg := avgHistogramValue(t, endpoint, "sip_exporter_rtp_r_factor")
	t.Logf("Degraded: avg R-factor=%.1f", rAvg)
	require.Less(t, rAvg, 85.0, "degraded R-factor should be <85")

	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_mos_f1_count") > 0
	}, 10*time.Second, 500*time.Millisecond, "mos_f1 must have samples under netem")

	f1Avg := avgHistogramValue(t, endpoint, "sip_exporter_rtp_mos_f1")
	f2Avg := avgHistogramValue(t, endpoint, "sip_exporter_rtp_mos_f2")
	adaptAvg := avgHistogramValue(t, endpoint, "sip_exporter_rtp_mos_adaptive")
	t.Logf("Degraded MOS: f1=%.2f f2=%.2f adaptive=%.2f", f1Avg, f2Avg, adaptAvg)

	require.LessOrEqual(t, f1Avg, f2Avg, "F1 (JB=50) should be <= F2 (JB=200)")
	require.LessOrEqual(t, f2Avg, adaptAvg, "F2 (JB=200) should be <= Adaptive (JB=500)")
}

// TestRTP_DuplicatePackets verifies that duplicate RTP sequence numbers
// increment the rtp_duplicate_packets_total counter.
func TestRTP_DuplicatePackets(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uasMediaNum, _ := strconv.Atoi(uasMedia)

	endpoint := startExporterWithCarrierUA(context.Background(), t, httpPort, uasSIP, "0",
		integrationCarriersYAML, integrationUserAgentsYAML, "")

	wait := startSippContainers(context.Background(), t,
		"uas_nortp.xml", "uac_nortp.xml", uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) >= 1
	}, 10*time.Second, 200*time.Millisecond, "dialog must be established")

	sendControlledRTP(t, uasMediaNum, []uint16{1, 2, 2, 3, 4, 4, 5})

	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_duplicate_packets_total") >= 2
	}, 10*time.Second, 500*time.Millisecond, "duplicate packets must be counted")

	wait()
}

// TestRTP_BurstGapLoss verifies that consecutive losses (burst, run ≥3) and
// isolated losses (gap, run <3) populate the respective loss density histograms.
func TestRTP_BurstGapLoss(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uasMediaNum, _ := strconv.Atoi(uasMedia)

	endpoint := startExporterWithCarrierUA(context.Background(), t, httpPort, uasSIP, "0",
		integrationCarriersYAML, integrationUserAgentsYAML, "")

	wait := startSippContainers(context.Background(), t,
		"uas_nortp.xml", "uac_nortp.xml", uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) >= 1
	}, 10*time.Second, 200*time.Millisecond, "dialog must be established")

	// Loss pattern: seq [1-5, 10-12, 14-15]
	//   Lost 6,7,8,9 → run=4 ≥3 → burst
	//   Lost 13      → run=1 <3 → gap
	sendControlledRTP(t, uasMediaNum, []uint16{1, 2, 3, 4, 5, 10, 11, 12, 14, 15})

	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_burst_loss_density_count") > 0
	}, 10*time.Second, 500*time.Millisecond, "burst_loss_density must have samples")

	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_gap_loss_density_count") > 0
	}, 10*time.Second, 500*time.Millisecond, "gap_loss_density must have samples")

	burstAvg := avgHistogramValue(t, endpoint, "sip_exporter_rtp_burst_loss_density")
	gapAvg := avgHistogramValue(t, endpoint, "sip_exporter_rtp_gap_loss_density")
	t.Logf("Loss distribution: burst=%.1f%% gap=%.1f%%", burstAvg, gapAvg)
	require.Greater(t, burstAvg, gapAvg, "burst density should exceed gap density")

	wait()
}

// TestRTP_OneWayCall verifies that a dialog with RTP in only one direction
// increments the rtp_oneway_calls_total counter at teardown.
func TestRTP_OneWayCall(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uasMediaNum, _ := strconv.Atoi(uasMedia)

	endpoint := startExporterWithCarrierUA(context.Background(), t, httpPort, uasSIP, "0",
		integrationCarriersYAML, integrationUserAgentsYAML, "")

	wait := startSippContainers(context.Background(), t,
		"uas_nortp.xml", "uac_nortp.xml", uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) >= 1
	}, 10*time.Second, 200*time.Millisecond, "dialog must be established")

	sendControlledRTP(t, uasMediaNum, []uint16{1, 2, 3, 4, 5})

	wait()

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtp_oneway_calls_total", labelCarrier, labelUAType) >= 1
	}, 10*time.Second, 500*time.Millisecond, "one-way call must be detected at teardown")
}

// TestRTP_MissingRTP verifies that a dialog with SDP but no RTP increments
// the sessions_missing_rtp_total counter at teardown.
func TestRTP_MissingRTP(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]

	endpoint := startExporterWithCarrierUA(context.Background(), t, httpPort, uasSIP, "0",
		integrationCarriersYAML, integrationUserAgentsYAML, "")

	wait := startSippContainers(context.Background(), t,
		"uas_nortp.xml", "uac_nortp.xml", uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) >= 1
	}, 10*time.Second, 200*time.Millisecond, "dialog must be established")

	wait()

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions_missing_rtp_total", labelCarrier, labelUAType) >= 1
	}, 10*time.Second, 500*time.Millisecond, "missing RTP must be detected at teardown")
}
