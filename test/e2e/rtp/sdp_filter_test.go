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

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

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

// sendNonRTPUDP sends count UDP packets with a non-RTP header (byte[0]=0x00,
// V=0 instead of V=2) to 127.0.0.1:port. A local listener is bound to complete
// the loopback receive cycle (PACKET_IGNORE_OUTGOING sees the RX copy).
func sendNonRTPUDP(t *testing.T, port int, count int) {
	t.Helper()

	listenAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	listener, err := net.ListenUDP("udp4", listenAddr)
	require.NoError(t, err)
	defer listener.Close()

	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
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

	sender, err := net.DialUDP("udp4", nil, listenAddr)
	require.NoError(t, err)
	defer sender.Close()

	pkt := make([]byte, 28)
	pkt[0] = 0x00 // V=0, not RTP V2

	for i := range count {
		_, _ = sender.Write(pkt)
		if i%10 == 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// sendNonRTPToSippPort sends non-RTP UDP to a SIPp-bound port via DialUDP.
// SIPp is listening on the port via -mp, so the loopback RX cycle completes
// without binding a local listener.
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
		_, _ = sender.Write(pkt)
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
					return getMetricByLabel(t, endpoint, "sip_exporter_sessions",
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
					return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
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
				return getMetricByLabel(t, endpoint, "sip_exporter_sessions",
					labelCarrier, labelUAType) >= 1
			}, 10*time.Second, 200*time.Millisecond, "dialog must be established")

			if tt.afterBye {
				wait()
				require.Eventually(t, func() bool {
					return metricLineExists(t, endpoint, "sip_exporter_sessions",
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
	tests := []struct {
		name                       string
		obsoletePacketsPerEndpoint int
		currentPacketsPerEndpoint  int
		minCurrentPackets          float64
		maxCurrentPackets          float64
	}{
		{
			name:                       "late CSeq 1 response preserves CSeq 2 revision",
			obsoletePacketsPerEndpoint: 400,
			currentPacketsPerEndpoint:  20,
			minCurrentPackets:          10,
			maxCurrentPackets:          200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ports := allocatePortsN(3)
			httpPort, uasSIP, uacSIP := ports[0], ports[1], ports[2]
			mediaPorts := allocateMediaPortsN(t, 4, ports...)
			oldOffer, oldAnswer := mediaPorts[0], mediaPorts[1]
			newOffer, newAnswer := mediaPorts[2], mediaPorts[3]
			oldOfferPort, err := strconv.Atoi(oldOffer)
			require.NoError(t, err)
			oldAnswerPort, err := strconv.Atoi(oldAnswer)
			require.NoError(t, err)
			newOfferPort, err := strconv.Atoi(newOffer)
			require.NoError(t, err)
			newAnswerPort, err := strconv.Atoi(newAnswer)
			require.NoError(t, err)

			endpoint := startExporterWithCarrierUA(t.Context(), t,
				httpPort, uasSIP, integrationCarriersYAML, integrationUserAgentsYAML, "")
			finish := startLateCSeqSippContainers(t.Context(), t,
				uasSIP, uacSIP, oldOffer, oldAnswer, newOffer, newAnswer)

			require.Eventually(t, func() bool {
				return metricExists(t, endpoint, "sip_exporter_options_total") &&
					getMetricByLabel(t, endpoint, "sip_exporter_options_total") == 1
			}, 5*time.Second, 200*time.Millisecond,
				"SIPp must complete the CSeq 2 transaction before RTP verification")

			obsoleteSeqs := make([]uint16, tt.obsoletePacketsPerEndpoint)
			for i := range obsoleteSeqs {
				obsoleteSeqs[i] = uint16(i + 1)
			}
			var pcmaPackets, pcmuPackets float64
			var nextSequence uint16 = 1
			t.Cleanup(func() {
				t.Logf("observed current RTP totals: PCMA=%v PCMU=%v", pcmaPackets, pcmuPackets)
			})
			require.Eventually(t, func() bool {
				currentSeqs := make([]uint16, tt.currentPacketsPerEndpoint)
				for i := range currentSeqs {
					currentSeqs[i] = nextSequence + uint16(i)
				}
				nextSequence += uint16(len(currentSeqs))
				sendControlledRTP(t, newOfferPort, currentSeqs)
				sendControlledRTPWithPayloadType(t, newAnswerPort, currentSeqs, 0)
				if !metricExists(t, endpoint, "sip_exporter_rtp_packets_total") {
					return false
				}
				pcmaPackets = getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total")
				pcmuPackets = getMetricByLabel(t, endpoint,
					"sip_exporter_rtp_packets_total", `codec="PCMU"`)
				return pcmaPackets >= tt.minCurrentPackets && pcmaPackets <= tt.maxCurrentPackets &&
					pcmuPackets >= tt.minCurrentPackets && pcmuPackets <= tt.maxCurrentPackets
			}, 5*time.Second, 200*time.Millisecond,
				"both matching CSeq 2 endpoints must accept RTP")

			sendControlledRTP(t, oldOfferPort, obsoleteSeqs)
			sendControlledRTPWithPayloadType(t, oldAnswerPort, obsoleteSeqs, 0)
			pcmaPackets = getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total")
			pcmuPackets = getMetricByLabel(t, endpoint,
				"sip_exporter_rtp_packets_total", `codec="PCMU"`)
			require.LessOrEqual(t, pcmaPackets, tt.maxCurrentPackets,
				"obsolete CSeq 1 offer endpoint must not accept RTP")
			require.LessOrEqual(t, pcmuPackets, tt.maxCurrentPackets,
				"late CSeq 1 answer endpoint must not accept RTP")

			finish()
		})
	}
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
