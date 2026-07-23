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

// sendNonRtpUDP sends count UDP packets with a non-RTP header (byte[0]=0x00,
// V=0 instead of V=2) to 127.0.0.1:port. A local listener is bound to complete
// the loopback receive cycle (PACKET_IGNORE_OUTGOING sees the RX copy).
func sendNonRtpUDP(t *testing.T, port int, count int) {
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

// sendNonRtpToSippPort sends non-RTP UDP to a SIPp-bound port via DialUDP.
// SIPp is listening on the port via -mp, so the loopback RX cycle completes
// without binding a local listener.
func sendNonRtpToSippPort(t *testing.T, port int, count int) {
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
		expectRtpMetric bool // expect rtp_packets_total > 0
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
			sipsPort := ports[2]
			uacSIP := ports[3]
			uasMedia := ports[4]
			uacMedia := ports[5]

			endpoint := startExporterWithCarrierUA(context.Background(), t,
				httpPort, uasSIP, sipsPort,
				integrationCarriersYAML, integrationUserAgentsYAML, "")

			var targetPort int
			var wait func()

			if tt.setupDialog {
				targetPort, _ = strconv.Atoi(uasMedia)
				wait = startSippContainers(context.Background(), t,
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
				sendNonRtpToSippPort(t, targetPort, pktCount)
			default:
				sendNonRtpUDP(t, targetPort, pktCount)
			}

			time.Sleep(2500 * time.Millisecond)
			after := getSocketPacketsReceived(t, endpoint)
			delta := after - before
			t.Logf("%s: socket delta=%v (sent %d)", tt.name, delta, pktCount)

			switch {
			case tt.expectRtpMetric:
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

// TestSDPFilter_EntryLifecycle verifies the BPF map entry lifecycle (S15-2, S15-3):
// the entry is inserted on INVITE 200 OK (dialog active) and deleted on BYE 200 OK.
//
// MC/DC: condition C (endpoint in BPF map) is the only variable.
// The dialog state (active vs torn down) controls whether the entry exists.
func TestSDPFilter_EntryLifecycle(t *testing.T) {
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
			httpPort, uasSIP, sipsPort, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4], ports[5]
			uasMediaNum, _ := strconv.Atoi(uasMedia)

			endpoint := startExporterWithCarrierUA(context.Background(), t,
				httpPort, uasSIP, sipsPort,
				integrationCarriersYAML, integrationUserAgentsYAML, "")

			wait := startSippContainers(context.Background(), t,
				"uas_nortp.xml", "uac_nortp.xml",
				uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")

			require.Eventually(t, func() bool {
				return getMetricByLabel(t, endpoint, "sip_exporter_sessions",
					labelCarrier, labelUAType) >= 1
			}, 10*time.Second, 200*time.Millisecond, "dialog must be established")

			if tt.afterBye {
				wait()
				require.Eventually(t, func() bool {
					return getMetricByLabel(t, endpoint, "sip_exporter_sessions",
						labelCarrier, labelUAType) == 0
				}, 10*time.Second, 200*time.Millisecond, "dialog must be torn down")
			}

			time.Sleep(1500 * time.Millisecond)
			before := getSocketPacketsReceived(t, endpoint)

			if tt.afterBye {
				sendNonRtpUDP(t, uasMediaNum, pktCount)
			} else {
				sendNonRtpToSippPort(t, uasMediaNum, pktCount)
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
