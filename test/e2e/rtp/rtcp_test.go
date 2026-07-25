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
	pkt[0] = 0x81                                                                     // V=2, P=0, RC=1 (one report block)
	pkt[1] = 201                                                                      // PT = RR
	binary.BigEndian.PutUint16(pkt[2:4], 7)                                           // length = 32/4 - 1
	binary.BigEndian.PutUint32(pkt[4:8], 0x5A5A5A5A)                                  // reporter (sender) SSRC
	binary.BigEndian.PutUint32(pkt[8:12], ssrc)                                       // reported source SSRC
	pkt[12] = 10                                                                      // fraction lost (10/256 ≈ 3.9%)
	pkt[13] = byte(cumLost >> 16)                                                     // 24-bit cumulative number of packets lost
	pkt[14] = byte(cumLost >> 8)
	pkt[15] = byte(cumLost)
	binary.BigEndian.PutUint32(pkt[16:20], 500)                                       // extended highest sequence
	binary.BigEndian.PutUint32(pkt[20:24], 1600)                                      // jitter ticks → 200 ms at 8000 Hz
	binary.BigEndian.PutUint32(pkt[24:28], pastNTP32(time.Now().Add(-5*time.Second))) // LSR
	binary.BigEndian.PutUint32(pkt[28:32], 0)                                         // DLSR

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
	time.Sleep(300 * time.Millisecond) // let RTP reach the tracker before the RR
	sendRTCPRR(t, uasMediaNum, testSSRC, 0)   // first RR: establishes the loss baseline (delta=0)
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

	// RTT value check (not just count): LSR was 5s ago, DLSR=0 → RTT ≈ 5000ms.
	// Proves the formula end-to-end, not merely that an observation landed.
	rttSum := getRTPMetric(t, endpoint, "sip_exporter_rtcp_rtt_milliseconds_sum")
	rttCount := getRTPMetric(t, endpoint, "sip_exporter_rtcp_rtt_milliseconds_count")
	require.Greater(t, rttCount, 0.0)
	avgRTT := rttSum / rttCount
	require.Greater(t, avgRTT, 4000.0, "RTT should be ~5000ms (LSR 5s ago, DLSR=0)")
	require.Less(t, avgRTT, 6000.0, "RTT should be ~5000ms (LSR 5s ago, DLSR=0)")
}
