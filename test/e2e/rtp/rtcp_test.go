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
// / LSR / DLSR values the test asserts on. Must be called while a SIPp media
// socket is bound to port (so the loopback packet completes the PACKET_HOST
// receive cycle the exporter — with PACKET_IGNORE_OUTGOING — actually sees).
func sendRTCPRR(t *testing.T, port int, ssrc uint32) {
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
	sendRTCPRR(t, uasMediaNum, testSSRC)
	time.Sleep(100 * time.Millisecond)
	sendRTCPRR(t, uasMediaNum, testSSRC)
	time.Sleep(300 * time.Millisecond) // let the RTCP be processed before BYE

	// Let SIPp finish (BYE tears down the dialog; the injected RTCP already ran).
	wait()

	// The RTCP path is asynchronous (AF_PACKET → handleRTCP); poll until all three
	// metrics are observed (mirrors the Eventually pattern used by the RTP suite).
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtcp_reports_total", `type="rr"`) > 0 &&
			getRTPMetric(t, endpoint, "sip_exporter_rtcp_jitter_milliseconds_count") > 0 &&
			getRTPMetric(t, endpoint, "sip_exporter_rtcp_rtt_milliseconds_count") > 0
	}, 10*time.Second, 500*time.Millisecond,
		"rtcp_reports_total{type=rr}, rtcp_jitter_count, rtcp_rtt_count must all be > 0")
}
