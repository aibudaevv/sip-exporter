//go:build e2e

package rtp

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const sipPacketOverwriteBytes = 8192

// startControlledSIPDialog emits each signalling message three times while
// retaining both SIP ports as UDP listeners. This makes the BPF-map lifecycle
// assertion independent of a single missed loopback AF_PACKET copy.
func startControlledSIPDialog(t *testing.T, uasSIP, uacSIP, uasMedia, uacMedia string) func() {
	t.Helper()

	uasPort, err := strconv.Atoi(uasSIP)
	require.NoError(t, err)
	uacPort, err := strconv.Atoi(uacSIP)
	require.NoError(t, err)
	uasConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: uasPort})
	require.NoError(t, err)
	uacConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: uacPort})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = uasConn.Close()
		_ = uacConn.Close()
	})

	callID := fmt.Sprintf("sdp-filter-%s-%s@127.0.0.1", uasSIP, uacSIP)
	uacSDP := fmt.Sprintf("v=0\r\nc=IN IP4 127.0.0.1\r\nm=audio %s RTP/AVP 8\r\na=rtpmap:8 PCMA/8000\r\n", uacMedia)
	uasSDP := fmt.Sprintf("v=0\r\nc=IN IP4 127.0.0.1\r\nm=audio %s RTP/AVP 8\r\na=rtpmap:8 PCMA/8000\r\n", uasMedia)
	invite := fmt.Sprintf("INVITE sip:service@127.0.0.1:%s SIP/2.0\r\nFrom: sipp <sip:sipp@127.0.0.1:%s>;tag=uac\r\nTo: sut <sip:service@127.0.0.1:%s>\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nUser-Agent: SIPp\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", uasSIP, uacSIP, uasSIP, callID, len(uacSDP), uacSDP)
	ok := fmt.Sprintf("SIP/2.0 200 OK\r\nFrom: sipp <sip:sipp@127.0.0.1:%s>;tag=uac\r\nTo: sut <sip:service@127.0.0.1:%s>;tag=uas\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nUser-Agent: SIPp\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", uacSIP, uasSIP, callID, len(uasSDP), uasSDP)
	uasAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: uasPort}
	uacAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: uacPort}
	for range 3 {
		_, err = uacConn.WriteToUDP([]byte(invite), uasAddr)
		require.NoError(t, err)
		_, err = uasConn.WriteToUDP([]byte(ok), uacAddr)
		require.NoError(t, err)
	}

	return func() {
		bye := fmt.Sprintf("BYE sip:service@127.0.0.1:%s SIP/2.0\r\nFrom: sipp <sip:sipp@127.0.0.1:%s>;tag=uac\r\nTo: sut <sip:service@127.0.0.1:%s>;tag=uas\r\nCall-ID: %s\r\nCSeq: 2 BYE\r\nContent-Length: 0\r\n\r\n", uasSIP, uacSIP, uasSIP, callID)
		byeOK := fmt.Sprintf("SIP/2.0 200 OK\r\nFrom: sipp <sip:sipp@127.0.0.1:%s>;tag=uac\r\nTo: sut <sip:service@127.0.0.1:%s>;tag=uas\r\nCall-ID: %s\r\nCSeq: 2 BYE\r\nContent-Length: 0\r\n\r\n", uacSIP, uasSIP, callID)
		for range 3 {
			_, err = uacConn.WriteToUDP([]byte(bye), uasAddr)
			require.NoError(t, err)
			_, err = uasConn.WriteToUDP([]byte(byeOK), uacAddr)
			require.NoError(t, err)
		}
	}
}

type reinviteDialog struct {
	t                      *testing.T
	uasConn, uacConn       *net.UDPConn
	uasAddr, uacAddr       *net.UDPAddr
	uasSIP, uacSIP, callID string
}

func startReinviteDialog(
	t *testing.T, uasSIP, uacSIP, uasMedia, uacMedia string, sessionExpires int,
) *reinviteDialog {
	t.Helper()
	dialog := newReinviteDialog(t, uasSIP, uacSIP)
	dialog.exchangeInvite(1, uacMedia, uasMedia, sessionExpires)
	return dialog
}

func newReinviteDialog(t *testing.T, uasSIP, uacSIP string) *reinviteDialog {
	t.Helper()
	uasPort, err := strconv.Atoi(uasSIP)
	require.NoError(t, err)
	uacPort, err := strconv.Atoi(uacSIP)
	require.NoError(t, err)
	dialog := &reinviteDialog{
		t:       t,
		uasSIP:  uasSIP,
		uacSIP:  uacSIP,
		callID:  fmt.Sprintf("reinvite-%s-%s@127.0.0.1", uasSIP, uacSIP),
		uasAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: uasPort},
		uacAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: uacPort},
	}
	dialog.uasConn, err = net.ListenUDP("udp4", dialog.uasAddr)
	require.NoError(t, err)
	dialog.uacConn, err = net.ListenUDP("udp4", dialog.uacAddr)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = dialog.uasConn.Close()
		_ = dialog.uacConn.Close()
	})
	return dialog
}

func (d *reinviteDialog) exchangeInvite(cseq int, offerMedia, answerMedia string, sessionExpires int) {
	d.t.Helper()
	for range 3 {
		d.sendInvite(cseq, offerMedia)
		d.sendInviteOK(cseq, answerMedia, sessionExpires)
	}
}

func (d *reinviteDialog) sendInvite(cseq int, offerMedia string) {
	d.t.Helper()
	offer := mediaSDP(offerMedia)
	toTag := ""
	if cseq > 1 {
		toTag = ";tag=uas"
	}
	invite := fmt.Sprintf("INVITE sip:service@127.0.0.1:%s SIP/2.0\r\nFrom: sipp <sip:sipp@127.0.0.1:%s>;tag=uac\r\nTo: sut <sip:service@127.0.0.1:%s>%s\r\nCall-ID: %s\r\nCSeq: %d INVITE\r\nUser-Agent: SIPp\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", d.uasSIP, d.uacSIP, d.uasSIP, toTag, d.callID, cseq, len(offer), offer)
	_, err := d.uacConn.WriteToUDP([]byte(invite), d.uasAddr)
	require.NoError(d.t, err)
}

func (d *reinviteDialog) sendInviteOK(cseq int, answerMedia string, sessionExpires int) {
	d.t.Helper()
	answer := mediaSDP(answerMedia)
	expiresHeader := ""
	if sessionExpires > 0 {
		expiresHeader = fmt.Sprintf("Session-Expires: %d\r\n", sessionExpires)
	}
	ok := fmt.Sprintf("SIP/2.0 200 OK\r\nFrom: sipp <sip:sipp@127.0.0.1:%s>;tag=uac\r\nTo: sut <sip:service@127.0.0.1:%s>;tag=uas\r\nCall-ID: %s\r\nCSeq: %d INVITE\r\nUser-Agent: SIPp\r\n%sContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", d.uacSIP, d.uasSIP, d.callID, cseq, expiresHeader, len(answer), answer)
	_, err := d.uasConn.WriteToUDP([]byte(ok), d.uacAddr)
	require.NoError(d.t, err)
}

func (d *reinviteDialog) sendOptionsMarker(cseq int) {
	d.t.Helper()
	options := fmt.Sprintf("OPTIONS sip:service@127.0.0.1:%s SIP/2.0\r\nFrom: sipp <sip:sipp@127.0.0.1:%s>;tag=uac\r\nTo: sut <sip:service@127.0.0.1:%s>;tag=uas\r\nCall-ID: %s\r\nCSeq: %d OPTIONS\r\nUser-Agent: SIPp\r\nContent-Length: 0\r\n\r\n", d.uasSIP, d.uacSIP, d.uasSIP, d.callID, cseq)
	_, err := d.uacConn.WriteToUDP([]byte(options), d.uasAddr)
	require.NoError(d.t, err)
}

func (d *reinviteDialog) sendOverwriteOptionsMarker(cseq int) {
	d.t.Helper()
	overwrite := strings.Repeat("x", sipPacketOverwriteBytes)
	options := fmt.Sprintf("OPTIONS sip:service@127.0.0.1:%s SIP/2.0\r\nFrom: sipp <sip:sipp@127.0.0.1:%s>;tag=uac\r\nTo: sut <sip:service@127.0.0.1:%s>;tag=uas\r\nCall-ID: %s\r\nCSeq: %d OPTIONS\r\nUser-Agent: SIPp\r\nX-Overwrite: %s\r\nContent-Length: 0\r\n\r\n", d.uasSIP, d.uacSIP, d.uasSIP, d.callID, cseq, overwrite)
	_, err := d.uacConn.WriteToUDP([]byte(options), d.uasAddr)
	require.NoError(d.t, err)
}

func waitForOptionsProcessing(t *testing.T, endpoint string, send func()) {
	t.Helper()
	var before float64
	if metricLineExists(t, endpoint, "sip_exporter_options_total", labelCarrier, labelUAType) {
		before = getMetricByLabel(t, endpoint, "sip_exporter_options_total", labelCarrier, labelUAType)
	}
	send()
	require.Eventually(t, func() bool {
		return metricLineExists(t, endpoint, "sip_exporter_options_total", labelCarrier, labelUAType) &&
			getMetricByLabel(t, endpoint, "sip_exporter_options_total", labelCarrier, labelUAType) == before+1
	}, 5*time.Second, 100*time.Millisecond, "OPTIONS marker must be processed")
}

func waitForAnyOptionsProcessing(t *testing.T, endpoint string, send func()) {
	t.Helper()
	var before float64
	if metricLineExists(t, endpoint, "sip_exporter_options_total") {
		before = getMetricByLabel(t, endpoint, "sip_exporter_options_total")
	}
	send()
	require.Eventually(t, func() bool {
		return metricLineExists(t, endpoint, "sip_exporter_options_total") &&
			getMetricByLabel(t, endpoint, "sip_exporter_options_total") == before+1
	}, 5*time.Second, 100*time.Millisecond, "OPTIONS marker must be processed")
}

func newOptionsMarker(t *testing.T, endpoint string, send func(int)) func() {
	cseq := 100
	return func() {
		waitForOptionsProcessing(t, endpoint, func() { send(cseq) })
		cseq++
	}
}

func (d *reinviteDialog) end() {
	d.t.Helper()
	bye := fmt.Sprintf("BYE sip:service@127.0.0.1:%s SIP/2.0\r\nFrom: sipp <sip:sipp@127.0.0.1:%s>;tag=uac\r\nTo: sut <sip:service@127.0.0.1:%s>;tag=uas\r\nCall-ID: %s\r\nCSeq: 9 BYE\r\nContent-Length: 0\r\n\r\n", d.uasSIP, d.uacSIP, d.uasSIP, d.callID)
	ok := fmt.Sprintf("SIP/2.0 200 OK\r\nFrom: sipp <sip:sipp@127.0.0.1:%s>;tag=uac\r\nTo: sut <sip:service@127.0.0.1:%s>;tag=uas\r\nCall-ID: %s\r\nCSeq: 9 BYE\r\nContent-Length: 0\r\n\r\n", d.uacSIP, d.uasSIP, d.callID)
	for range 3 {
		_, err := d.uacConn.WriteToUDP([]byte(bye), d.uasAddr)
		require.NoError(d.t, err)
		_, err = d.uasConn.WriteToUDP([]byte(ok), d.uacAddr)
		require.NoError(d.t, err)
	}
}

func mediaSDP(port string) string {
	return fmt.Sprintf("v=0\r\nc=IN IP4 127.0.0.1\r\nm=audio %s RTP/AVP 8\r\na=rtpmap:8 PCMA/8000\r\n", port)
}

type parseErrorProbe struct {
	t                  *testing.T
	endpoint, syncPort string
	current            float64
}

func newParseErrorProbe(t *testing.T, endpoint, syncPort string) *parseErrorProbe {
	t.Helper()
	probe := &parseErrorProbe{t: t, endpoint: endpoint, syncPort: syncPort}
	if metricLineExists(t, endpoint, "sip_exporter_parse_errors_total", `type="sip"`) {
		probe.current = getMetricByLabel(t, endpoint, "sip_exporter_parse_errors_total", `type="sip"`)
	}
	probe.mark(0)
	return probe
}

func (p *parseErrorProbe) assertEndpointDelta(
	port string, packets, want int, send func(*testing.T, int, int), marker func(),
) {
	p.t.Helper()
	portNumber, err := strconv.Atoi(port)
	require.NoError(p.t, err)
	send(p.t, portNumber, packets)
	marker()
	wantTotal := p.current + float64(want)
	got := getMetricByLabel(p.t, p.endpoint, "sip_exporter_parse_errors_total", `type="sip"`)
	require.Equal(p.t, wantTotal, got, "exact malformed-packet delta after OPTIONS marker")
	p.current = got
}

func (p *parseErrorProbe) mark(wantBeforeMarker int) {
	p.t.Helper()
	sendUDPProbe(p.t, p.syncPort)
	want := p.current + float64(wantBeforeMarker+1)
	require.Eventually(p.t, func() bool {
		return metricLineExists(p.t, p.endpoint, "sip_exporter_parse_errors_total", `type="sip"`) &&
			getMetricByLabel(p.t, p.endpoint, "sip_exporter_parse_errors_total", `type="sip"`) >= want
	}, 5*time.Second, 100*time.Millisecond, "parse-error marker must be processed")
	got := getMetricByLabel(p.t, p.endpoint, "sip_exporter_parse_errors_total", `type="sip"`)
	require.Equal(p.t, want, got, "exact malformed-packet delta through the processing marker")
	p.current = got
}

func sendUDPProbe(t *testing.T, port string) {
	t.Helper()
	sendUDPBytes(t, port, make([]byte, 28))
}

func sendUDPBytes(t *testing.T, port string, payload []byte) {
	t.Helper()
	portNumber, err := strconv.Atoi(port)
	require.NoError(t, err)
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: portNumber})
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write(payload)
	require.NoError(t, err)
}

func sendOptionsProbe(t *testing.T, port string, cseq int) {
	t.Helper()
	message := fmt.Sprintf("OPTIONS sip:service@127.0.0.1:%s SIP/2.0\r\nFrom: marker <sip:marker@127.0.0.1>;tag=marker\r\nTo: service <sip:service@127.0.0.1:%s>\r\nCall-ID: processing-marker-%d\r\nCSeq: %d OPTIONS\r\nUser-Agent: SIPp\r\nContent-Length: 0\r\n\r\n", port, port, cseq, cseq)
	sendUDPBytes(t, port, []byte(message))
}

// sendNonRTPUDP binds a local listener so PACKET_IGNORE_OUTGOING sees the RX copy.
func sendNonRTPUDP(t *testing.T, port int, count int) {
	t.Helper()

	listenAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	listener, err := net.ListenUDP("udp4", listenAddr)
	require.NoError(t, err)

	done := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 1500)
		for {
			select {
			case <-done:
				return
			default:
			}
			_ = listener.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			if _, _, e := listener.ReadFromUDP(buf); e != nil {
				continue
			}
		}
	}()
	defer func() {
		close(done)
		require.NoError(t, listener.Close())
		<-readerDone
	}()

	sender, err := net.DialUDP("udp4", nil, listenAddr)
	require.NoError(t, err)
	defer sender.Close()

	pkt := make([]byte, 28)
	pkt[0] = 0x00 // V=0, not RTP V2

	for i := range count {
		_, err = sender.Write(pkt)
		require.NoError(t, err)
		if i%10 == 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// sendNonRTPToSippPort relies on SIPp's -mp listener for the loopback RX copy.
func sendNonRTPToSippPort(t *testing.T, port int, count int) {
	t.Helper()

	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	sender, err := net.DialUDP("udp4", nil, addr)
	require.NoError(t, err)
	defer sender.Close()

	pkt := make([]byte, 28)
	pkt[0] = 0x00 // V=0 — non-RTP, SDP-driven lookup passes
	for i := range count {
		binary.BigEndian.PutUint16(pkt[2:4], uint16(i+1))
		_, err = sender.Write(pkt)
		require.NoError(t, err)
		if i%10 == 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func allocateMediaPortsN(t *testing.T, n int, reserved ...string) []string {
	t.Helper()

	blocked := make(map[int]struct{}, len(reserved))
	for _, value := range reserved {
		port, err := strconv.Atoi(value)
		require.NoError(t, err)
		blocked[port] = struct{}{}
	}

	portMu.Lock()
	defer portMu.Unlock()

	ports := make([]string, 0, n)
	for len(ports) < n {
		rtp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		require.NoError(t, err)
		port := rtp.LocalAddr().(*net.UDPAddr).Port
		if port >= 65535 {
			_ = rtp.Close()
			continue
		}
		_, rtpUsed := usedPorts[port]
		_, rtcpUsed := usedPorts[port+1]
		_, rtpBlocked := blocked[port]
		_, rtcpBlocked := blocked[port+1]
		if rtpUsed || rtcpUsed || rtpBlocked || rtcpBlocked {
			_ = rtp.Close()
			continue
		}

		rtcp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port + 1})
		if err != nil {
			_ = rtp.Close()
			continue
		}
		_ = rtcp.Close()
		_ = rtp.Close()
		usedPorts[port] = struct{}{}
		usedPorts[port+1] = struct{}{}
		ports = append(ports, strconv.Itoa(port))
	}
	return ports
}

func releaseLateCSeqDialog(t *testing.T, uacSIP int) {
	t.Helper()

	listener, err := net.DialUDP("udp4", nil, &net.UDPAddr{
		IP: net.IPv4(127, 0, 0, 1), Port: uacSIP,
	})
	require.NoError(t, err)
	defer listener.Close()

	localPort := listener.LocalAddr().(*net.UDPAddr).Port
	request := fmt.Sprintf("INFO sip:sipp@127.0.0.1:%d SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP 127.0.0.1:%d;branch=z9hG4bK-test-release\r\n"+
		"From: sut <sip:service@127.0.0.1>;tag=0\r\n"+
		"To: sipp <sip:sipp@127.0.0.1>;tag=0\r\n"+
		"Call-ID: late-cseq-call\r\n"+
		"CSeq: 99 INFO\r\n"+
		"Content-Length: 0\r\n\r\n", uacSIP, localPort)
	_, err = listener.Write([]byte(request))
	require.NoError(t, err)
}

func startLateCSeqSippContainers(
	ctx context.Context, t *testing.T,
	uasSIP, uacSIP, oldOffer, oldAnswer, newOffer, newAnswer string,
) func() {
	t.Helper()

	scenarioDir := filepath.Join(projectRoot(), "test", "e2e", "sipp")
	dumpLogs := func(ctx context.Context, name string, container testcontainers.Container) {
		logs, err := container.Logs(ctx)
		if err != nil {
			t.Logf("SIPp %s logs unavailable: %v", name, err)
			return
		}
		defer logs.Close()
		output, _ := io.ReadAll(logs)
		t.Logf("SIPp %s logs:\n%s", name, strings.TrimSpace(string(output)))
	}
	start := func(cmd []string) testcontainers.Container {
		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:       sippImage,
				NetworkMode: "host",
				Cmd:         cmd,
				Mounts: testcontainers.Mounts(
					testcontainers.BindMount(scenarioDir, "/scenarios"),
				),
			},
			Started: true,
			Logger:  log.New(io.Discard, "", 0),
		})
		require.NoError(t, err)
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if t.Failed() {
				dumpLogs(cleanupCtx, "failure", container)
			}
			_ = container.Terminate(cleanupCtx)
		})
		return container
	}

	uas := start([]string{
		"-sf", "/scenarios/uas_late_invite_200.xml",
		"-i", "127.0.0.1", "-p", uasSIP, "-mp", oldAnswer,
		"-set", "new_answer_port", newAnswer,
		"-m", "1", "-nr", "-nostdin",
	})
	uasReady := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		address, err := net.ResolveUDPAddr("udp4", "127.0.0.1:"+uasSIP)
		if err != nil {
			break
		}
		listener, err := net.ListenUDP("udp4", address)
		if err != nil {
			uasReady = true
			break
		}
		_ = listener.Close()
		time.Sleep(50 * time.Millisecond)
	}
	if !uasReady {
		dumpLogs(t.Context(), "UAS startup", uas)
		state, stateErr := uas.State(t.Context())
		t.Logf("SIPp UAS startup state: %+v (error: %v)", state, stateErr)
	}
	require.True(t, uasReady, "SIPp UAS must listen on %s", uasSIP)

	uac := start([]string{
		"-sf", "/scenarios/uac_late_invite_200.xml",
		"-i", "127.0.0.1", "-p", uacSIP, "-mp", oldOffer,
		"-set", "new_offer_port", newOffer,
		"-cid_str", "late-cseq-call",
		"-m", "1", "-nr", "127.0.0.1:" + uasSIP,
	})

	return func() {
		uacPort, err := strconv.Atoi(uacSIP)
		require.NoError(t, err)
		releaseLateCSeqDialog(t, uacPort)
		for name, container := range map[string]testcontainers.Container{"UAC": uac, "UAS": uas} {
			require.Eventually(t, func() bool {
				state, err := container.State(t.Context())
				return err == nil && !state.Running
			}, 15*time.Second, 100*time.Millisecond, "SIPp %s did not exit", name)
			state, err := container.State(t.Context())
			require.NoError(t, err)
			if state.ExitCode != 0 || os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
				dumpLogs(t.Context(), name, container)
			}
			require.Zero(t, state.ExitCode, "SIPp %s exited with an error", name)
		}
	}
}

// TestSDPFilter verifies the strict SDP-driven BPF filter end-to-end.
//
// MC/DC table: condition C (endpoint in BPF map) × condition D (RTP pattern).
// Pattern matching fallback was removed — only SDP-registered endpoints pass.
//
//	Case                        C (in map)  D (RTP pattern)  Expected
//	sdp_port_non_rtp_passes     true        false (V=0)      BPF passes (SDP lookup)
//	unregistered_port_non_rtp   false       false (V=0)      BPF drops
//	unregistered_port_rtp_drops false       true (V=2)       BPF drops (no fallback)
//	sdp_port_rtp_captured       true        true (V=2)       rtp_packets_total > 0
func TestSDPFilter(t *testing.T) {
	const pktCount = 20

	tests := []struct {
		name            string
		setupDialog     bool // establish SIP dialog with SDP before sending
		sendRTP         bool // true: valid RTP (V=2), false: non-RTP UDP (V=0)
		expectDropped   bool // expect BPF to drop (socket delta ≈ 0)
		expectRTPMetric bool // expect rtp_packets_total > 0
	}{
		{"sdp_port_non_rtp_passes", true, false, false, false},
		{"unregistered_port_non_rtp_drops", false, false, true, false},
		{"unregistered_port_rtp_drops", false, true, true, false},
		{"sdp_port_rtp_captured", true, true, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ports := allocatePortsN(6)
			httpPort := ports[0]
			uasSIP := ports[1]
			uacSIP := ports[3]
			uasMedia := ports[4]
			uacMedia := ports[5]

			endpoint := startExporterWithCarrierUA(t.Context(), t,
				httpPort, uasSIP,
				integrationCarriersYAML, integrationUserAgentsYAML, "")

			var targetPort int
			var wait func()

			if tt.setupDialog {
				targetPort, _ = strconv.Atoi(uasMedia)
				wait = startSippContainers(t.Context(), t,
					"uas_nortp.xml", "uac_nortp.xml",
					uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")

				require.Eventually(t, func() bool {
					return metricWithLabelsExists(t, endpoint, "sip_exporter_sessions",
						labelCarrier, labelUAType) &&
						getMetricByLabel(t, endpoint, "sip_exporter_sessions",
							labelCarrier, labelUAType) >= 1
				}, 10*time.Second, 200*time.Millisecond, "dialog must be established")
			} else {
				targetPort, _ = strconv.Atoi(uacMedia)
			}

			time.Sleep(1500 * time.Millisecond)
			before := getSocketPacketsReceived(t, endpoint)

			switch {
			case tt.sendRTP:
				sendControlledRTP(t, targetPort, []uint16{1, 2, 3, 4, 5})
			case tt.setupDialog:
				sendNonRTPToSippPort(t, targetPort, pktCount)
			default:
				sendNonRTPUDP(t, targetPort, pktCount)
			}

			time.Sleep(2500 * time.Millisecond)
			after := getSocketPacketsReceived(t, endpoint)
			delta := after - before
			t.Logf("%s: socket delta=%v (sent %d)", tt.name, delta, pktCount)

			switch {
			case tt.expectRTPMetric:
				require.Eventually(t, func() bool {
					return rtpMetricExists(t, endpoint, "sip_exporter_rtp_packets_total") &&
						getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
				}, 10*time.Second, 500*time.Millisecond,
					"valid RTP to SDP-registered port must be captured in rtp_packets_total")
			case tt.expectDropped:
				require.Less(t, delta, 3.0,
					"UDP to unregistered port must be dropped by BPF (no pattern fallback)")
			default:
				require.GreaterOrEqual(t, delta, float64(pktCount)*0.5,
					"non-RTP UDP to SDP-registered port must pass BPF via SDP-driven lookup")
			}

			if wait != nil {
				wait()
			}
		})
	}
}

// TestSDPFilterEntryLifecycle verifies the BPF map entry lifecycle (S15-2, S15-3):
// the entry is inserted on INVITE 200 OK (dialog active) and deleted on BYE 200 OK.
//
// MC/DC: condition C (endpoint in BPF map) is the only variable.
// The dialog state (active vs torn down) controls whether the entry exists.
func TestSDPFilterEntryLifecycle(t *testing.T) {
	const pktCount = 20

	tests := []struct {
		name     string
		afterBye bool // true: wait for BYE teardown before sending
		wantPass bool // true: expect BPF to pass (entry in map)
	}{
		{"entry_exists_during_dialog", false, true},
		{"entry_deleted_after_bye", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ports := allocatePortsN(6)
			httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
			uasMediaNum, _ := strconv.Atoi(uasMedia)

			endpoint := startExporterWithCarrierUA(t.Context(), t,
				httpPort, uasSIP,
				integrationCarriersYAML, integrationUserAgentsYAML, "")

			wait := startControlledSIPDialog(t, uasSIP, uacSIP, uasMedia, uacMedia)

			require.Eventually(t, func() bool {
				return metricWithLabelsExists(t, endpoint, "sip_exporter_sessions",
					labelCarrier, labelUAType) &&
					getMetricByLabel(t, endpoint, "sip_exporter_sessions",
						labelCarrier, labelUAType) >= 1
			}, 10*time.Second, 200*time.Millisecond, "dialog must be established")

			if tt.afterBye {
				wait()
				require.Eventually(t, func() bool {
					return metricWithLabelsExists(t, endpoint, "sip_exporter_sessions",
						labelCarrier, labelUAType) &&
						getMetricByLabel(t, endpoint, "sip_exporter_sessions",
							labelCarrier, labelUAType) == 0
				}, 10*time.Second, 200*time.Millisecond, "dialog must be torn down")
			}

			time.Sleep(1500 * time.Millisecond)
			before := getSocketPacketsReceived(t, endpoint)

			if tt.afterBye {
				sendNonRTPUDP(t, uasMediaNum, pktCount)
			} else {
				sendNonRTPUDP(t, uasMediaNum, pktCount)
			}

			time.Sleep(2500 * time.Millisecond)
			after := getSocketPacketsReceived(t, endpoint)
			delta := after - before
			t.Logf("%s: socket delta=%v (sent %d non-RTP)", tt.name, delta, pktCount)

			if tt.wantPass {
				require.GreaterOrEqual(t, delta, float64(pktCount)*0.5,
					"non-RTP UDP must pass BPF while entry is in map")
			} else {
				require.Less(t, delta, 3.0,
					"non-RTP UDP must be dropped after BYE deleted the BPF map entry")
			}

			if !tt.afterBye {
				wait()
			}
		})
	}
}

// TestSDPFilterLateInvite200OKUsesMatchingCSeq verifies that a late response
// from the initial INVITE cannot consume a newer re-INVITE offer. Only the
// matching CSeq 2 media revision must remain in the BPF endpoint map.
func TestSDPFilterLateInvite200OKUsesMatchingCSeq(t *testing.T) {
	const packetsPerEndpoint = 20

	t.Run("late CSeq 1 response preserves CSeq 2 revision", func(t *testing.T) {
		ports := allocatePortsN(3)
		httpPort, uasSIP, uacSIP := ports[0], ports[1], ports[2]
		mediaPorts := allocateMediaPortsN(t, 4, ports...)
		oldOffer, oldAnswer := mediaPorts[0], mediaPorts[1]
		newOffer, newAnswer := mediaPorts[2], mediaPorts[3]

		endpoint := startExporterWithCarrierUA(t.Context(), t,
			httpPort, uasSIP, integrationCarriersYAML, integrationUserAgentsYAML, "")
		finish := startLateCSeqSippContainers(t.Context(), t,
			uasSIP, uacSIP, oldOffer, oldAnswer, newOffer, newAnswer)

		require.Eventually(t, func() bool {
			return metricExists(t, endpoint, "sip_exporter_options_total") &&
				getMetricByLabel(t, endpoint, "sip_exporter_options_total") == 1
		}, 5*time.Second, 200*time.Millisecond,
			"SIPp must complete the CSeq 2 transaction before RTP verification")

		probe := newParseErrorProbe(t, endpoint, uasSIP)
		marker := newOptionsMarker(t, endpoint, func(cseq int) { sendOptionsProbe(t, uasSIP, cseq) })
		probe.assertEndpointDelta(newOffer, packetsPerEndpoint, packetsPerEndpoint, sendNonRTPUDP, marker)
		probe.assertEndpointDelta(newAnswer, packetsPerEndpoint, packetsPerEndpoint, sendNonRTPUDP, marker)
		probe.assertEndpointDelta(oldOffer, packetsPerEndpoint, 0, sendNonRTPToSippPort, marker)
		probe.assertEndpointDelta(oldAnswer, packetsPerEndpoint, 0, sendNonRTPToSippPort, marker)

		finish()
	})
}

// TestSDPFilterInviteSDPSurvivesReceiveBufferReuse smoke-tests the INVITE SDP
// lifecycle with an intervening oversized SIP packet. The deterministic buffer
// ownership regression is covered by TestStoreInviteSDPOwnsPacketBuffer.
func TestSDPFilterInviteSDPSurvivesReceiveBufferReuse(t *testing.T) {
	ports := allocatePortsN(3)
	httpPort, uasSIP, uacSIP := ports[0], ports[1], ports[2]
	media := allocateMediaPortsN(t, 2, ports...)
	uasMedia, uacMedia := media[0], media[1]
	endpoint := startExporterWithExtraEnv(t.Context(), t, httpPort, uasSIP, testInterface, "", map[string]string{
		"GOMAXPROCS": "1",
	})
	dialog := newReinviteDialog(t, uasSIP, uacSIP)

	dialog.sendInvite(1, uacMedia)
	waitForAnyOptionsProcessing(t, endpoint, func() { dialog.sendOverwriteOptionsMarker(100) })
	dialog.sendInviteOK(1, uasMedia, 0)
	waitForAnyOptionsProcessing(t, endpoint, func() { dialog.sendOptionsMarker(101) })

	probe := newParseErrorProbe(t, endpoint, uasSIP)
	marker := func() { waitForAnyOptionsProcessing(t, endpoint, func() { dialog.sendOptionsMarker(102) }) }
	probe.assertEndpointDelta(uacMedia, 20, 20, sendNonRTPUDP, marker)
	probe.assertEndpointDelta(uasMedia, 20, 20, sendNonRTPUDP, marker)
}

// TestSDPFilterLateInvite200OKAfterByeDoesNotRestoreDialog verifies that a
// retransmitted final response cannot resurrect a torn-down dialog or media.
func TestSDPFilterLateInvite200OKAfterByeDoesNotRestoreDialog(t *testing.T) {
	ports := allocatePortsN(3)
	httpPort, uasSIP, uacSIP := ports[0], ports[1], ports[2]
	media := allocateMediaPortsN(t, 2, ports...)
	uasMedia, uacMedia := media[0], media[1]
	endpoint := startExporterWithCarrierUA(t.Context(), t, httpPort, uasSIP,
		integrationCarriersYAML, integrationUserAgentsYAML, "")
	dialog := startReinviteDialog(t, uasSIP, uacSIP, uacMedia, uasMedia, 0)
	marker := newOptionsMarker(t, endpoint, dialog.sendOptionsMarker)

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) == 1
	}, 5*time.Second, 100*time.Millisecond, "initial dialog must be established")
	dialog.end()
	require.Eventually(t, func() bool {
		return metricLineExists(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) &&
			getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) == 0
	}, 5*time.Second, 100*time.Millisecond, "BYE must remove the dialog")
	require.Eventually(t, func() bool {
		return metricLineExists(t, endpoint, "sip_exporter_active_dialogs") &&
			getMetricByLabel(t, endpoint, "sip_exporter_active_dialogs") == 0
	}, 5*time.Second, 100*time.Millisecond, "BYE must remove the active dialog")

	probe := newParseErrorProbe(t, endpoint, uasSIP)
	probe.assertEndpointDelta(uacMedia, 20, 0, sendNonRTPUDP, marker)
	dialog.sendInviteOK(1, uacMedia, 0)
	waitForOptionsProcessing(t, endpoint, func() { dialog.sendOptionsMarker(90) })

	assert.Never(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_active_dialogs") != 0
	}, 2*time.Second, 100*time.Millisecond, "late INVITE 200 OK must not restore the active dialog")
	probe.assertEndpointDelta(uacMedia, 20, 0, sendNonRTPUDP, marker)
}

func TestSDPFilterReinviteLifecycle(t *testing.T) {
	const probePackets = 20

	tests := []struct {
		name                  string
		nextOffer, nextAnswer int
		active, stale         []int
		probeBefore           bool
	}{
		{
			name:        "unchanged revision remains active before and after processing",
			nextOffer:   0,
			nextAnswer:  1,
			active:      []int{0, 1},
			probeBefore: true,
		},
		{
			name:       "changed revision replaces both endpoints",
			nextOffer:  2,
			nextAnswer: 3,
			active:     []int{2, 3},
			stale:      []int{0, 1},
		},
		{
			name:       "hold revision removes both old endpoints",
			nextOffer:  -1,
			nextAnswer: -1,
			stale:      []int{0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ports := allocatePortsN(3)
			httpPort, uasSIP, uacSIP := ports[0], ports[1], ports[2]
			media := allocateMediaPortsN(t, 4, ports...)
			endpoint := startExporterWithCarrierUA(t.Context(), t, httpPort, uasSIP,
				integrationCarriersYAML, integrationUserAgentsYAML, "")
			dialog := startReinviteDialog(t, uasSIP, uacSIP, media[1], media[0], 0)
			marker := newOptionsMarker(t, endpoint, dialog.sendOptionsMarker)
			require.Eventually(t, func() bool {
				return getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) == 1
			}, 5*time.Second, 100*time.Millisecond, "initial dialog must be established")

			waitForOptionsProcessing(t, endpoint, func() { dialog.sendOptionsMarker(90) })
			if tt.probeBefore {
				probe := newParseErrorProbe(t, endpoint, uasSIP)
				for _, index := range tt.active {
					probe.assertEndpointDelta(media[index], probePackets, probePackets, sendNonRTPUDP, marker)
				}
			}
			nextOffer, nextAnswer := "0", "0"
			if tt.nextOffer >= 0 {
				nextOffer, nextAnswer = media[tt.nextOffer], media[tt.nextAnswer]
			}
			dialog.exchangeInvite(2, nextOffer, nextAnswer, 0)
			waitForOptionsProcessing(t, endpoint, func() { dialog.sendOptionsMarker(91) })
			probe := newParseErrorProbe(t, endpoint, uasSIP)
			for _, index := range tt.active {
				probe.assertEndpointDelta(media[index], probePackets, probePackets, sendNonRTPUDP, marker)
			}
			for _, index := range tt.stale {
				probe.assertEndpointDelta(media[index], probePackets, 0, sendNonRTPUDP, marker)
			}

			dialog.end()
			require.Eventually(t, func() bool {
				return metricLineExists(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) &&
					getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) == 0
			}, 5*time.Second, 100*time.Millisecond, "BYE must remove the active revision")
			if len(tt.active) > 0 {
				probe = newParseErrorProbe(t, endpoint, uasSIP)
				for _, index := range tt.active {
					probe.assertEndpointDelta(media[index], probePackets, 0, sendNonRTPUDP, marker)
				}
			}
		})
	}
}

func TestSDPFilterExpiryRemovesMediaRevision(t *testing.T) {
	ports := allocatePortsN(3)
	httpPort, uasSIP, uacSIP := ports[0], ports[1], ports[2]
	media := allocateMediaPortsN(t, 2, ports...)
	uasMedia, uacMedia := media[0], media[1]
	endpoint := startExporterWithCarrierUA(t.Context(), t, httpPort, uasSIP,
		integrationCarriersYAML, integrationUserAgentsYAML, "")
	dialog := startReinviteDialog(t, uasSIP, uacSIP, uasMedia, uacMedia, 1)

	require.Eventually(t, func() bool {
		return metricLineExists(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) &&
			getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) == 0
	}, 5*time.Second, 100*time.Millisecond, "expired dialog must be removed")
	probe := newParseErrorProbe(t, endpoint, uasSIP)
	marker := newOptionsMarker(t, endpoint, dialog.sendOptionsMarker)
	probe.assertEndpointDelta(uasMedia, 20, 0, sendNonRTPUDP, marker)
	probe.assertEndpointDelta(uacMedia, 20, 0, sendNonRTPUDP, marker)
}

// TestSDPFilterSharedEntrySurvivesLatestOwnerBye verifies that a shared media
// endpoint remains in the BPF map when the latest registered dialog ends while
// an earlier dialog still owns it.
func TestSDPFilterSharedEntrySurvivesLatestOwnerBye(t *testing.T) {
	const pktCount = 20

	ports := allocatePortsN(8)
	httpPort := ports[0]
	uasSIPA, uacSIPA := ports[1], ports[2]
	uasSIPB, uacSIPB := ports[3], ports[4]
	sharedMedia, uacMediaA, uacMediaB := ports[5], ports[6], ports[7]
	sharedMediaPort, err := strconv.Atoi(sharedMedia)
	require.NoError(t, err)

	endpoint := startExporterWithCarrierUA(t.Context(), t,
		httpPort, uasSIPA+","+uasSIPB,
		integrationCarriersYAML, integrationUserAgentsYAML, "")

	endFirstDialog := startControlledSIPDialog(t, uasSIPA, uacSIPA, sharedMedia, uacMediaA)
	endSecondDialog := startControlledSIPDialog(t, uasSIPB, uacSIPB, sharedMedia, uacMediaB)

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions",
			labelCarrier, labelUAType) >= 2
	}, 10*time.Second, 200*time.Millisecond, "both dialogs must be established")

	endSecondDialog()
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions",
			labelCarrier, labelUAType) == 1
	}, 10*time.Second, 200*time.Millisecond, "first dialog must remain after the latest owner BYE")

	time.Sleep(1500 * time.Millisecond)
	before := getSocketPacketsReceived(t, endpoint)
	sendNonRTPUDP(t, sharedMediaPort, pktCount)
	time.Sleep(2500 * time.Millisecond)
	after := getSocketPacketsReceived(t, endpoint)

	require.GreaterOrEqual(t, after-before, float64(pktCount)*0.5,
		"shared media endpoint must remain in the BPF map while the first dialog is active")

	endFirstDialog()
}
