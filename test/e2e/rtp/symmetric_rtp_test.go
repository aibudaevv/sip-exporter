//go:build e2e

package rtp

import (
	"encoding/binary"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openRTPPeer(t *testing.T, ip, port string) *net.UDPConn {
	t.Helper()
	portNumber, err := strconv.Atoi(port)
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(ip), Port: portNumber})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func sendRTPFrom(
	t *testing.T, conn *net.UDPConn, destinationIP, destinationPort string, firstSequence uint16, count int,
) {
	t.Helper()
	port, err := strconv.Atoi(destinationPort)
	require.NoError(t, err)
	address := &net.UDPAddr{IP: net.ParseIP(destinationIP), Port: port}
	packet := make([]byte, 28)
	packet[0] = 0x80
	packet[1] = 8
	binary.BigEndian.PutUint32(packet[8:12], 0x53594d4d)
	for i := range count {
		binary.BigEndian.PutUint16(packet[2:4], firstSequence+uint16(i))
		_, err = conn.WriteToUDP(packet, address)
		require.NoError(t, err)
		time.Sleep(5 * time.Millisecond)
	}
}

func requireRTPPacketCount(t *testing.T, endpoint string, want float64) {
	t.Helper()
	const metric = "sip_exporter_rtp_packets_total"
	if want == 0 {
		require.False(t, metricExists(t, endpoint, metric), "RTP metric must not exist before first packet")
		return
	}
	labels := []string{`carrier="loopback"`, pcmaFilter, `source_country="unknown"`, `ua_type="sipp"`}
	exactLabels := append(labels, `direction="inbound"`)
	require.True(t, metricExists(t, endpoint, metric))
	require.True(t, metricLineExists(t, endpoint, metric, exactLabels...))
	require.Equal(t, want, getRTPMetric(t, endpoint, metric), "exact PCMA total")
	require.Equal(t, want, getMetricByLabel(t, endpoint, metric,
		exactLabels...), "exact inbound PCMA series")
}

func assertRTPPacketCount(t *testing.T, endpoint string, want float64, send func()) {
	t.Helper()
	send()
	require.Eventually(t, func() bool {
		return metricExists(t, endpoint, "sip_exporter_rtp_packets_total") &&
			getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") >= want
	}, 5*time.Second, 100*time.Millisecond, "PCMA total must become %v", want)
	requireRTPPacketCount(t, endpoint, want)
}

func TestSymmetricRTPAlias(t *testing.T) {
	const packetCount = 12

	tests := []struct {
		name            string
		sourceIP        string
		wantAfterProbe  float64
		wantPackets     float64
		wantMismatch    float64
		wantOneWay      float64
		wantDiagnostics bool
	}{
		{
			name:            "source_port_remap",
			sourceIP:        "127.0.0.1",
			wantAfterProbe:  packetCount + 1,
			wantPackets:     2*packetCount + 1,
			wantMismatch:    1,
			wantDiagnostics: true,
		},
		{
			name:           "source_ip_mismatch",
			sourceIP:       "127.0.0.2",
			wantAfterProbe: packetCount,
			wantPackets:    2 * packetCount,
			wantOneWay:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sipLabels := []string{labelCarrier, labelUAType,
				`source_country="unknown"`, `direction="inbound"`}
			ports := allocatePortsN(3)
			httpPort, uasSIP, uacSIP := ports[0], ports[1], ports[2]
			media := allocateMediaPortsN(t, 4, ports...)
			advertisedA, advertisedB, remappedA, probeSource := media[0], media[1], media[2], media[3]

			endpoint := startExporterWithCarrierUA(t.Context(), t, httpPort, uasSIP,
				integrationCarriersYAML, integrationUserAgentsYAML, "")
			endDialog := startControlledSIPDialog(t, uasSIP, uacSIP, advertisedB, advertisedA)
			require.Eventually(t, func() bool {
				return metricLineExists(t, endpoint, "sip_exporter_sessions", sipLabels...) &&
					getMetricByLabel(t, endpoint, "sip_exporter_sessions", sipLabels...) == 1
			}, 5*time.Second, 100*time.Millisecond, "dialog must be established")

			peerA := openRTPPeer(t, tt.sourceIP, remappedA)
			peerB := openRTPPeer(t, "127.0.0.1", advertisedB)
			probePeer := openRTPPeer(t, "127.0.0.1", probeSource)
			requireRTPPacketCount(t, endpoint, 0)
			assertRTPPacketCount(t, endpoint, packetCount, func() {
				sendRTPFrom(t, peerA, "127.0.0.1", advertisedB, 1, packetCount)
			})

			if tt.wantDiagnostics {
				assertRTPPacketCount(t, endpoint, tt.wantAfterProbe, func() {
					sendRTPFrom(t, probePeer, tt.sourceIP, remappedA, packetCount+1, 1)
				})
			} else {
				sendRTPFrom(t, probePeer, tt.sourceIP, remappedA, packetCount+1, 1)
				newParseErrorProbe(t, endpoint, uasSIP).mark(0)
				requireRTPPacketCount(t, endpoint, tt.wantAfterProbe)
			}
			assertRTPPacketCount(t, endpoint, tt.wantPackets, func() {
				sendRTPFrom(t, peerB, tt.sourceIP, remappedA, packetCount+2, packetCount)
			})

			const mismatchMetric = "sip_exporter_rtp_endpoint_mismatch_total"
			const aliasMetric = "sip_exporter_rtp_alias_active"
			if tt.wantDiagnostics {
				require.True(t, metricLineExists(t, endpoint, mismatchMetric,
					labelCarrier, `direction="inbound"`, `type="source_port"`))
				require.Equal(t, tt.wantMismatch, getMetricByLabel(t, endpoint, mismatchMetric,
					labelCarrier, `direction="inbound"`, `type="source_port"`))
				require.True(t, metricLineExists(t, endpoint, aliasMetric,
					labelCarrier, `direction="inbound"`))
				require.Equal(t, 1.0, getMetricByLabel(t, endpoint, aliasMetric,
					labelCarrier, `direction="inbound"`))
			} else {
				require.False(t, metricExists(t, endpoint, mismatchMetric))
				require.False(t, metricExists(t, endpoint, aliasMetric))
			}

			endDialog()
			require.Eventually(t, func() bool {
				return metricLineExists(t, endpoint, "sip_exporter_sessions", sipLabels...) &&
					getMetricByLabel(t, endpoint, "sip_exporter_sessions", sipLabels...) == 0
			}, 5*time.Second, 100*time.Millisecond, "dialog must be torn down")
			if tt.wantDiagnostics {
				require.True(t, metricLineExists(t, endpoint, aliasMetric,
					labelCarrier, `direction="inbound"`))
				require.Equal(t, 0.0, getMetricByLabel(t, endpoint, aliasMetric,
					labelCarrier, `direction="inbound"`))
			}
			if tt.wantOneWay == 0 {
				require.False(t, metricLineExists(t, endpoint, "sip_exporter_rtp_oneway_calls_total"))
			} else {
				require.True(t, metricLineExists(t, endpoint, "sip_exporter_rtp_oneway_calls_total",
					sipLabels...))
				require.Equal(t, tt.wantOneWay, getMetricByLabel(t, endpoint,
					"sip_exporter_rtp_oneway_calls_total", sipLabels...))
			}
		})
	}
}

func TestSymmetricRTPSharedDestinationRejectsAlias(t *testing.T) {
	const packetCount = 12

	ports := allocatePortsN(5)
	httpPort := ports[0]
	uasSIPA, uacSIPA, uasSIPB, uacSIPB := ports[1], ports[2], ports[3], ports[4]
	media := allocateMediaPortsN(t, 5, ports...)
	sharedB, advertisedA, advertisedC, candidateAlias, probeSource := media[0], media[1], media[2], media[3], media[4]

	endpoint := startExporterWithCarrierUA(t.Context(), t, httpPort, uasSIPA+","+uasSIPB,
		integrationCarriersYAML, integrationUserAgentsYAML, "")
	endDialogA := startControlledSIPDialog(t, uasSIPA, uacSIPA, sharedB, advertisedA)
	endDialogB := startControlledSIPDialog(t, uasSIPB, uacSIPB, sharedB, advertisedC)
	sipLabels := []string{labelCarrier, labelUAType, `source_country="unknown"`, `direction="inbound"`}
	require.Eventually(t, func() bool {
		return metricLineExists(t, endpoint, "sip_exporter_sessions", sipLabels...) &&
			getMetricByLabel(t, endpoint, "sip_exporter_sessions", sipLabels...) == 2
	}, 5*time.Second, 100*time.Millisecond, "both dialogs must be established")

	aliasPeer := openRTPPeer(t, "127.0.0.1", candidateAlias)
	_ = openRTPPeer(t, "127.0.0.1", sharedB)
	probePeer := openRTPPeer(t, "127.0.0.1", probeSource)
	requireRTPPacketCount(t, endpoint, 0)
	assertRTPPacketCount(t, endpoint, packetCount, func() {
		sendRTPFrom(t, aliasPeer, "127.0.0.1", sharedB, 1, packetCount)
	})

	barrier := newParseErrorProbe(t, endpoint, uasSIPA)
	assertRTPPacketCount(t, endpoint, packetCount+1, func() {
		sendRTPFrom(t, probePeer, "127.0.0.1", candidateAlias, packetCount+1, packetCount)
		sendRTPFrom(t, aliasPeer, "127.0.0.1", sharedB, 2*packetCount+1, 1)
		barrier.mark(0)
	})

	endDialogB()
	endDialogA()
}
