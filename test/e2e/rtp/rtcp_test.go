//go:build e2e

package rtp

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRTCP_NonMux_LegacyPort probes the S12-14 legacy RTCP capture path: when the
// SDP declares neither a=rtcp nor a=rtcp-mux, the exporter registers port+1
// (RFC 3550 §9). RTCP injected to that adjacent port (NOT the RTP port) must be
// captured by eBPF and correlated by SSRC. A dummy UDP listener represents the
// endpoint's RTCP socket (SIPp does not bind the RTCP port) so the loopback
// packet completes the PACKET_HOST receive cycle the exporter — with
// PACKET_IGNORE_OUTGOING — sees, and avoids ICMP port-unreachable.
func TestRTCP_NonMux_LegacyPort(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uacMedia := ports[0], ports[1], ports[2], ports[3]
	uasMediaNum := allocateMediaPortWithAdjacent(t)
	uasMedia := strconv.Itoa(uasMediaNum)
	legacyRTCPPort := uasMediaNum + 1 // RFC 3550 §9: RTCP on the adjacent port
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, testInterface, "")

	wait := startSippContainers(
		context.Background(), t,
		"uas_rtp.xml", "uac_rtp.xml",
		uasSIP, uacSIP, uasMedia, uacMedia,
		"127.0.0.1", "127.0.0.1",
	)

	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
	}, 15*time.Second, 500*time.Millisecond, "RTP must flow before injecting RTCP")

	// Bind a dummy listener on the legacy RTCP port (port+1) — SIPp does not bind
	// it — so the loopback packet completes the PACKET_HOST receive cycle.
	dummy, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: legacyRTCPPort})
	require.NoError(t, err)
	defer dummy.Close()

	// Inject RTP to the RTP port (establishes the stream), then RTCP to port+1.
	sendRTPWithSSRC(t, uasMediaNum, testSSRC, 20)
	time.Sleep(300 * time.Millisecond)
	sendRTCPRR(t, legacyRTCPPort, testSSRC, 0)
	time.Sleep(100 * time.Millisecond)
	sendRTCPRR(t, legacyRTCPPort, testSSRC, 5)
	time.Sleep(300 * time.Millisecond)

	wait()

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtcp_reports_total", `type="rr"`) > 0
	}, 10*time.Second, 500*time.Millisecond,
		"RTCP injected to the legacy port+1 must be captured (S12-14 synthesis)")
}

// rtcpAttrPort is the explicit RTCP port declared via a=rtcp in
// uas_rtcp_attr.xml (RFC 3605). SIPp has no [rtcp_port] template keyword, so the
// scenario carries a fixed port; the test reserves it with a dummy listener so a
// collision fails loudly rather than producing a silent capture miss.
const rtcpAttrPort = 16008

// TestRTCP_ExplicitAttrPort probes the S12-1c explicit a=rtcp capture path
// (RFC 3605): when the SDP declares a=rtcp:<port>, the exporter registers that
// port in the BPF map. RTCP injected to that explicit port (which is NEITHER the
// RTP port NOR port+1) must be captured and correlated by SSRC. This closes the
// S12-12 acceptance gap — TestRTCP_NonMux_LegacyPort covers only the legacy
// port+1 (default) branch, not the m.RTCPPort != 0 branch.
func TestRTCP_ExplicitAttrPort(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uasMediaNum, err := strconv.Atoi(uasMedia)
	require.NoError(t, err)

	// Reserve the fixed a=rtcp port early (fail fast on collision). SIPp does not
	// bind the RTCP port; this dummy also lets the loopback RTCP packet complete
	// the PACKET_HOST receive cycle the exporter — with PACKET_IGNORE_OUTGOING — sees.
	dummy, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: rtcpAttrPort})
	require.NoError(t, err, "rtcpAttrPort %d must be free", rtcpAttrPort)
	defer dummy.Close()

	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, testInterface, "")

	wait := startSippContainers(
		context.Background(), t,
		"uas_rtcp_attr.xml", "uac_rtp.xml",
		uasSIP, uacSIP, uasMedia, uacMedia,
		"127.0.0.1", "127.0.0.1",
	)

	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
	}, 15*time.Second, 500*time.Millisecond, "RTP must flow before injecting RTCP")

	// Inject RTP to the RTP port (establishes the stream under testSSRC), then
	// RTCP to the explicit a=rtcp port (NOT the RTP port, NOT port+1).
	sendRTPWithSSRC(t, uasMediaNum, testSSRC, 20)
	time.Sleep(300 * time.Millisecond)
	sendRTCPRR(t, rtcpAttrPort, testSSRC, 0)
	time.Sleep(100 * time.Millisecond)
	sendRTCPRR(t, rtcpAttrPort, testSSRC, 5)
	time.Sleep(300 * time.Millisecond)

	wait()

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtcp_reports_total", `type="rr"`) > 0
	}, 10*time.Second, 500*time.Millisecond,
		"RTCP injected to the explicit a=rtcp port must be captured (RFC 3605)")
}

// allocateMediaPortWithAdjacent finds a UDP port whose adjacent port+1 is also
// free and returns it. The legacy RTCP port (RFC 3550 §9) is port+1, which the
// test binds as a dummy listener; both must be free at allocation time.
func allocateMediaPortWithAdjacent(t *testing.T) int {
	t.Helper()
	for range 50 {
		l, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		if err != nil {
			continue
		}
		port := l.LocalAddr().(*net.UDPAddr).Port
		adjacent, adjErr := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port + 1})
		if adjErr != nil {
			l.Close()
			continue
		}
		adjacent.Close()
		l.Close()
		return port
	}
	t.Fatal("failed to allocate a media port with an adjacent free port")
	return 0
}

// testSSRC is the deterministic SSRC used by the injected RTP+RTCP pair. The
// exporter tracks the injected RTP stream under this SSRC, then the injected RR
// for the same SSRC correlates to it. (SIPp may rewrite its pcap's SSRC, so the
// test injects its own RTP rather than relying on SIPp's SSRC.)
const testSSRC uint32 = 0xCAFEBABE

// ntpEpochOffset and the middle-32 NTP formula mirror exporter.nowNTP32 (which is
// unexported and lives in another package). Used to build an RR LSR in the past so
// the exporter computes a positive RTT.
const ntpEpochOffset uint64 = 2208988800

func pastNTP32(t time.Time) uint32 {
	secs := uint64(t.Unix()) + ntpEpochOffset
	return uint32((secs & 0xFFFF) << 16) // fraction 0 is precise enough for an e2e RTT
}

// sendRTPWithSSRC sends count RTPv2/PCMA packets with the given SSRC to
// 127.0.0.1:port. Relies on SIPp's UAS media socket being bound to port (the UAS
// is started with -mp) so the loopback packet completes the PACKET_HOST receive
// cycle that the exporter — with PACKET_IGNORE_OUTGOING — actually sees.
func sendRTPWithSSRC(t *testing.T, port int, ssrc uint32, count int) {
	t.Helper()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	conn, err := net.DialUDP("udp4", nil, addr)
	require.NoError(t, err)
	defer conn.Close()

	pkt := make([]byte, 28)
	pkt[0] = 0x80                             // V=2, P=0, X=0, CC=0
	pkt[1] = 0x08                             // M=0, PT=8 (PCMA)
	binary.BigEndian.PutUint32(pkt[4:8], 160) // timestamp
	binary.BigEndian.PutUint32(pkt[8:12], ssrc)
	for i := range count {
		binary.BigEndian.PutUint16(pkt[2:4], uint16(i+1)) // sequence number
		_, _ = conn.Write(pkt)
		time.Sleep(2 * time.Millisecond)
	}
}

// sendRTCPRR injects a single RTCP Receiver Report (RFC 3550 §6.4.2) for ssrc to
// 127.0.0.1:port (rtcp-mux: the RTP port). One report block carries jitter / loss
// / LSR / DLSR values the test asserts on. cumLost sets the 24-bit cumulative-lost
// field (the exporter diffs it between RRs). Must be called while a SIPp media
// socket is bound to port (so the loopback packet completes the PACKET_HOST
// receive cycle the exporter — with PACKET_IGNORE_OUTGOING — actually sees).
func sendRTCPRR(t *testing.T, port int, ssrc, cumLost uint32) {
	t.Helper()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	conn, err := net.DialUDP("udp4", nil, addr)
	require.NoError(t, err)
	defer conn.Close()

	// RR: common header (4) + sender SSRC (4) + one report block (24) = 32 bytes.
	pkt := make([]byte, 32)
	pkt[0] = 0x81                                    // V=2, P=0, RC=1 (one report block)
	pkt[1] = 201                                     // PT = RR
	binary.BigEndian.PutUint16(pkt[2:4], 7)          // length = 32/4 - 1
	binary.BigEndian.PutUint32(pkt[4:8], 0x5A5A5A5A) // reporter (sender) SSRC
	binary.BigEndian.PutUint32(pkt[8:12], ssrc)      // reported source SSRC
	pkt[12] = 10                                     // fraction lost (10/256 ≈ 3.9%)
	pkt[13] = byte(cumLost >> 16)                    // 24-bit cumulative number of packets lost
	pkt[14] = byte(cumLost >> 8)
	pkt[15] = byte(cumLost)
	binary.BigEndian.PutUint32(pkt[16:20], 500)                                       // extended highest sequence
	binary.BigEndian.PutUint32(pkt[20:24], 1600)                                      // jitter ticks → 200 ms at 8000 Hz
	binary.BigEndian.PutUint32(pkt[24:28], pastNTP32(time.Now().Add(-5*time.Second))) // LSR
	binary.BigEndian.PutUint32(pkt[28:32], 0x10000)                                   // DLSR = 1 s (NTP32)

	_, err = conn.Write(pkt)
	require.NoError(t, err)
}

// sendRTCPSR injects a single RTCP Sender Report (RFC 3550 §6.4.1, PT 200) for
// ssrc to 127.0.0.1:port. Structure: header(4) + sender SSRC(4) + sender
// info(20) + one report block(24) = 52 bytes. The sender info is left zeroed
// (the exporter does not consume it); the report block carries the same fields
// as an RR so correlation + quality metrics fire. cumLost sets the 24-bit
// cumulative-lost field.
func sendRTCPSR(t *testing.T, port int, ssrc, cumLost uint32) {
	t.Helper()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	conn, err := net.DialUDP("udp4", nil, addr)
	require.NoError(t, err)
	defer conn.Close()

	pkt := make([]byte, 52)
	pkt[0] = 0x81                                    // V=2, P=0, RC=1 (one report block)
	pkt[1] = 200                                     // PT = SR
	binary.BigEndian.PutUint16(pkt[2:4], 12)         // length = 52/4 - 1
	binary.BigEndian.PutUint32(pkt[4:8], 0x5A5A5A5A) // sender SSRC
	// sender info [8:28] left zeroed (NTP/RTP-ts/pkt/octet counts unused).
	binary.BigEndian.PutUint32(pkt[28:32], ssrc) // reported source SSRC
	pkt[32] = 10                                 // fraction lost (10/256 ≈ 3.9%)
	pkt[33] = byte(cumLost >> 16)                // 24-bit cumulative lost
	pkt[34] = byte(cumLost >> 8)
	pkt[35] = byte(cumLost)
	binary.BigEndian.PutUint32(pkt[40:44], 1600)                                      // jitter ticks → 200 ms at 8000 Hz
	binary.BigEndian.PutUint32(pkt[44:48], pastNTP32(time.Now().Add(-5*time.Second))) // LSR
	binary.BigEndian.PutUint32(pkt[48:52], 0)                                         // DLSR

	_, err = conn.Write(pkt)
	require.NoError(t, err)
}

// TestRTCP_MetricsFromInjectedRR proves the full RTCP path end to end: SIPp
// establishes a real SIP dialog (SDP → media endpoint registered, UAS media socket
// bound); the test then injects its own RTP (deterministic SSRC) so the exporter
// tracks a stream, and an RTCP Receiver Report for that SSRC, both captured by
// eBPF (rtcp-mux, on the RTP port), routed to handleRTCP, correlated by SSRC, and
// exported as the rtcp_* metrics. SIPp itself does not emit RTCP and may rewrite
// the pcap SSRC, so both packets are injected by the test with a fixed SSRC.
func TestRTCP_MetricsFromInjectedRR(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uasMediaNum, err := strconv.Atoi(uasMedia)
	require.NoError(t, err)
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, testInterface, "")

	// Start the SIPp UAS+UAC pair but do not block: inject RTCP during the call.
	wait := startSippContainers(
		context.Background(), t,
		"uas_rtp.xml", "uac_rtp.xml",
		uasSIP, uacSIP, uasMedia, uacMedia,
		"127.0.0.1", "127.0.0.1",
	)

	// Wait until RTP has flowed: confirms the media endpoint is registered AND the
	// UAS media socket is bound (so injected packets complete the HOST cycle).
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
	}, 15*time.Second, 500*time.Millisecond, "RTP must flow before injecting RTCP")

	// Inject a few RTP packets (paced — the AF_PACKET socket drops under a burst)
	// so a stream with a deterministic SSRC is tracked, then RR(s) for that SSRC.
	// Two RRs guard against a single dropped packet at the socket.
	sendRTPWithSSRC(t, uasMediaNum, testSSRC, 20)
	time.Sleep(300 * time.Millisecond)      // let RTP reach the tracker before the RR
	sendRTCPRR(t, uasMediaNum, testSSRC, 0) // first RR: establishes the loss baseline (delta=0)
	time.Sleep(100 * time.Millisecond)
	sendRTCPRR(t, uasMediaNum, testSSRC, 500) // second RR: cumulative=500 → delta=500 emitted
	time.Sleep(300 * time.Millisecond)        // let the RTCP be processed before BYE

	// Let SIPp finish (BYE tears down the dialog; the injected RTCP already ran).
	wait()

	// The RTCP path is asynchronous (AF_PACKET → handleRTCP); poll until all five
	// RTCP metric families are observed — reports, jitter, RTT, loss-fraction, and
	// cumulative-loss (the delta from the second RR proves D3 baseline semantics).
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtcp_reports_total", `type="rr"`) > 0 &&
			getRTPMetric(t, endpoint, "sip_exporter_rtcp_jitter_milliseconds_count") > 0 &&
			getRTPMetric(t, endpoint, "sip_exporter_rtcp_rtt_milliseconds_count") > 0 &&
			getRTPMetric(t, endpoint, "sip_exporter_rtcp_loss_fraction_percent_count") > 0 &&
			getRTPMetric(t, endpoint, "sip_exporter_rtcp_cumulative_loss_total") > 0
	}, 10*time.Second, 500*time.Millisecond,
		"all five RTCP metrics must be observed")

	// RTT value check (not just count): LSR was 5s ago, DLSR=1s → RTT ≈ 4000ms.
	// Proves the formula end-to-end, not merely that an observation landed.
	require.True(t, metricExists(t, endpoint, "sip_exporter_rtcp_rtt_milliseconds"),
		"rtt histogram must exist before reading its value")
	rttSum := getRTPMetric(t, endpoint, "sip_exporter_rtcp_rtt_milliseconds_sum")
	rttCount := getRTPMetric(t, endpoint, "sip_exporter_rtcp_rtt_milliseconds_count")
	require.Greater(t, rttCount, 0.0)
	avgRTT := rttSum / rttCount
	require.Greater(t, avgRTT, 3000.0, "RTT should be ~4000ms (LSR 5s ago, DLSR=1s)")
	require.Less(t, avgRTT, 5500.0, "RTT should be ~4000ms (LSR 5s ago, DLSR=1s)")

	// Loss-fraction value check (not just count): the RR carries fracLost=10 →
	// 10/256*100 ≈ 3.9%. Proves the fracLost byte extraction + scale end-to-end,
	// not merely that an observation landed (guards against a byte-offset regression).
	require.True(t, metricExists(t, endpoint, "sip_exporter_rtcp_loss_fraction_percent"),
		"loss-fraction histogram must exist before reading its value")
	lossSum := getRTPMetric(t, endpoint, "sip_exporter_rtcp_loss_fraction_percent_sum")
	lossCount := getRTPMetric(t, endpoint, "sip_exporter_rtcp_loss_fraction_percent_count")
	require.Greater(t, lossCount, 0.0)
	avgLoss := lossSum / lossCount
	require.InDelta(t, 10.0/256*100, avgLoss, 0.5, "loss fraction should be ~3.9%% (fracLost=10)")

	// Jitter value check (not just count): every RR carries jitter=1600 ticks →
	// 200ms at clockRate 8000. Proves the tick→ms conversion end-to-end, not
	// merely that an observation landed.
	require.True(t, metricExists(t, endpoint, "sip_exporter_rtcp_jitter_milliseconds"),
		"jitter histogram must exist before reading its value")
	jitSum := getRTPMetric(t, endpoint, "sip_exporter_rtcp_jitter_milliseconds_sum")
	jitCount := getRTPMetric(t, endpoint, "sip_exporter_rtcp_jitter_milliseconds_count")
	require.Greater(t, jitCount, 0.0)
	avgJitter := jitSum / jitCount
	require.InDelta(t, 200.0, avgJitter, 50.0, "jitter should be ~200ms (1600 ticks at 8000Hz)")

	// Cumulative-loss delta check: first RR (cumLost=0) establishes the baseline
	// (delta=0, not emitted); second RR (cumLost=500) emits delta=500. Proves the
	// baseline-diff semantics end-to-end, not merely that a non-zero value landed.
	lossTotal := getRTPMetric(t, endpoint, "sip_exporter_rtcp_cumulative_loss_total")
	require.Equal(t, 500.0, lossTotal, "cumulative-loss delta must be exactly 500")
}

// TestRTCP_SenderReportCaptured proves the SR (PT 200) path end to end. SR is
// the dominant RTCP type from active senders, but the prior e2e suite injected
// only RR. The SR carries its report blocks after a 20-byte sender-info block;
// this test confirms the exporter parses past the sender info, correlates the
// block by SSRC, and labels the report type=sr.
func TestRTCP_SenderReportCaptured(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uasMediaNum, err := strconv.Atoi(uasMedia)
	require.NoError(t, err)
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, testInterface, "")

	wait := startSippContainers(
		context.Background(), t,
		"uas_rtp.xml", "uac_rtp.xml",
		uasSIP, uacSIP, uasMedia, uacMedia,
		"127.0.0.1", "127.0.0.1",
	)

	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
	}, 15*time.Second, 500*time.Millisecond, "RTP must flow before injecting RTCP")

	sendRTPWithSSRC(t, uasMediaNum, testSSRC, 20)
	time.Sleep(300 * time.Millisecond)
	sendRTCPSR(t, uasMediaNum, testSSRC, 0)
	time.Sleep(300 * time.Millisecond)

	wait()

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtcp_reports_total", `type="sr"`) > 0
	}, 10*time.Second, 500*time.Millisecond,
		"RTCP Sender Report (PT 200) must be captured and labelled type=sr")
}

// TestRTCP_BothDirections proves per-stream SSRC isolation: a single dialog has
// two media legs, each emitting RTCP for a distinct SSRC. Both RRs must
// correlate independently — a bug in the rtcpLossSeen baseline (shared state
// across streams) would cross-contaminate the two legs. RTCP is injected to each
// leg's RTP port (rtcp-mux capture path) for two different SSRCs.
func TestRTCP_BothDirections(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uasMediaNum, err := strconv.Atoi(uasMedia)
	require.NoError(t, err)
	uacMediaNum, err := strconv.Atoi(uacMedia)
	require.NoError(t, err)
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, testInterface, "")

	wait := startSippContainers(
		context.Background(), t,
		"uas_rtp.xml", "uac_rtp.xml",
		uasSIP, uacSIP, uasMedia, uacMedia,
		"127.0.0.1", "127.0.0.1",
	)

	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
	}, 15*time.Second, 500*time.Millisecond, "RTP must flow before injecting RTCP")

	const ssrcA uint32 = 0xCAFEBABE
	const ssrcB uint32 = 0xDEADBEEF
	sendRTPWithSSRC(t, uasMediaNum, ssrcA, 20)
	sendRTPWithSSRC(t, uacMediaNum, ssrcB, 20)
	time.Sleep(300 * time.Millisecond)
	sendRTCPRR(t, uasMediaNum, ssrcA, 0)
	sendRTCPRR(t, uacMediaNum, ssrcB, 0)
	time.Sleep(300 * time.Millisecond)

	wait()

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtcp_reports_total", `type="rr"`) >= 2
	}, 10*time.Second, 500*time.Millisecond,
		"RTCP from both legs must be captured (per-stream SSRC isolation)")
}

// getRTCPOrphanCount scrapes the label-less rtcp_orphan_reports_total counter.
func getRTCPOrphanCount(t *testing.T, endpoint string) float64 {
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

	re := regexp.MustCompile(`^sip_exporter_rtcp_orphan_reports_total\s+([0-9.]+)`)
	for _, line := range strings.Split(string(body), "\n") {
		m := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) == 2 {
			v, parseErr := strconv.ParseFloat(m[1], 64)
			require.NoError(t, parseErr)
			return v
		}
	}
	return 0
}

// TestRTCP_RefreshesStreamTTL proves end-to-end that RTCP reports refresh the
// stream TTL (S12-52): when RTP pauses but RTCP keeps arriving, the stream
// survives the 1s cleanup cycle past the RTP-idle TTL window. Without the fix,
// Cleanup evicts based solely on lastArrival (set only by RTP), so RRs sent
// after the TTL become orphans — quality metrics are lost during hold/mute.
//
// The counterpart negative case (no RTCP → stream expires at TTL) is covered
// by TestRTP_StreamExpiry. Together they form the MC/DC pair for the
// max(lastArrival, lastRTCP) condition in Cleanup.
func TestRTCP_RefreshesStreamTTL(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uasMediaNum, err := strconv.Atoi(uasMedia)
	require.NoError(t, err)

	// TTL=3s: short enough for a fast test, long enough to separate from the
	// RTCP injection cadence. The 1s cleanup ticker evicts streams whose
	// max(lastArrival, lastRTCP) is older than 3s.
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, testInterface, "3s")

	wait := startSippContainers(
		context.Background(), t,
		"uas_rtp.xml", "uac_rtp.xml",
		uasSIP, uacSIP, uasMedia, uacMedia,
		"127.0.0.1", "127.0.0.1",
	)

	// Wait until RTP flows: media endpoint registered, UAS socket bound.
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
	}, 15*time.Second, 500*time.Millisecond, "RTP must flow before injecting RTCP")

	// Establish a stream under testSSRC, then STOP injecting RTP. SIPp's own
	// RTP continues under a different SSRC — it does not refresh testSSRC.
	sendRTPWithSSRC(t, uasMediaNum, testSSRC, 20)
	time.Sleep(300 * time.Millisecond)

	// Baseline RRs while the stream is well within TTL.
	sendRTCPRR(t, uasMediaNum, testSSRC, 0)
	time.Sleep(100 * time.Millisecond)
	sendRTCPRR(t, uasMediaNum, testSSRC, 0)

	// Mid-window RR: refreshes lastRTCP at ~2.4s (still within 3s TTL from RTP).
	time.Sleep(2 * time.Second)
	sendRTCPRR(t, uasMediaNum, testSSRC, 0)

	// Wait past the 3s RTP-idle TTL, then send RRs that — without the fix —
	// would find no tracked stream (evicted at the cleanup tick ≈3-4s) and
	// increment the orphan counter instead of correlating.
	time.Sleep(2 * time.Second) // now ~4.4s since last RTP — past TTL
	sendRTCPRR(t, uasMediaNum, testSSRC, 0)
	time.Sleep(100 * time.Millisecond)
	sendRTCPRR(t, uasMediaNum, testSSRC, 0) // guard against AF_PACKET drop
	time.Sleep(500 * time.Millisecond)

	// With the fix: all RRs correlate (RTCP refreshed lastRTCP), so
	// rtcp_reports_total grew to 5 and rtcp_orphan_reports_total stayed at 0.
	// Without the fix: the last two RRs are orphans → reports stalls at 3.
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtcp_reports_total", `type="rr"`) >= 4
	}, 5*time.Second, 500*time.Millisecond,
		"RRs sent past the RTP-idle TTL must correlate (RTCP refreshed stream TTL)")

	require.Zero(t, getRTCPOrphanCount(t, endpoint),
		"no orphan reports: RTCP kept the stream alive through the RTP-idle window")

	wait()
}
