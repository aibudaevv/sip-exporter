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

func openRTPPeer(t *testing.T, port string) *net.UDPConn {
	t.Helper()
	portNumber, err := strconv.Atoi(port)
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: portNumber})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func sendRTPFrom(t *testing.T, conn *net.UDPConn, destination string, firstSequence uint16, count int) {
	t.Helper()
	port, err := strconv.Atoi(destination)
	require.NoError(t, err)
	address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
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
	require.Equal(t, want, getRTPMetric(t, endpoint, metric), "exact PCMA total")
	labels := []string{`carrier="loopback"`, pcmaFilter, `source_country="unknown"`, `ua_type="sipp"`}
	require.Equal(t, want, getMetricByLabel(t, endpoint, metric,
		append(labels, `direction="inbound"`)...), "exact inbound PCMA series")
}

func assertRTPPacketCount(t *testing.T, endpoint string, want float64, send func()) {
	t.Helper()
	send()
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") >= want
	}, 5*time.Second, 100*time.Millisecond, "PCMA total must become %v", want)
	requireRTPPacketCount(t, endpoint, want)
}

func TestSymmetricRTPAlias(t *testing.T) {
	const packetCount = 12

	ports := allocatePortsN(3)
	httpPort, uasSIP, uacSIP := ports[0], ports[1], ports[2]
	media := allocateMediaPortsN(t, 4, ports...)
	advertisedA, advertisedB, remappedA, probeSource := media[0], media[1], media[2], media[3]

	endpoint := startExporterWithCarrierUA(t.Context(), t, httpPort, uasSIP,
		integrationCarriersYAML, integrationUserAgentsYAML, "")
	endDialog := startControlledSIPDialog(t, uasSIP, uacSIP, advertisedB, advertisedA)
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) == 1
	}, 5*time.Second, 100*time.Millisecond, "dialog must be established")

	peerA := openRTPPeer(t, remappedA)
	peerB := openRTPPeer(t, advertisedB)
	probePeer := openRTPPeer(t, probeSource)
	requireRTPPacketCount(t, endpoint, 0)
	assertRTPPacketCount(t, endpoint, packetCount, func() {
		sendRTPFrom(t, peerA, advertisedB, 1, packetCount)
	})

	assertRTPPacketCount(t, endpoint, packetCount+1, func() {
		sendRTPFrom(t, probePeer, remappedA, packetCount+1, 1)
	})
	assertRTPPacketCount(t, endpoint, 2*packetCount+1, func() {
		sendRTPFrom(t, peerB, remappedA, packetCount+2, packetCount)
	})

	endDialog()
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) == 0
	}, 5*time.Second, 100*time.Millisecond, "dialog must be torn down")
	require.False(t, metricLineExists(t, endpoint, "sip_exporter_rtp_oneway_calls_total"),
		"both observed legs must not be classified as one-way RTP")
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
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) == 2
	}, 5*time.Second, 100*time.Millisecond, "both dialogs must be established")

	aliasPeer := openRTPPeer(t, candidateAlias)
	_ = openRTPPeer(t, sharedB)
	probePeer := openRTPPeer(t, probeSource)
	requireRTPPacketCount(t, endpoint, 0)
	assertRTPPacketCount(t, endpoint, packetCount, func() {
		sendRTPFrom(t, aliasPeer, sharedB, 1, packetCount)
	})

	barrier := newParseErrorProbe(t, endpoint, uasSIPA)
	assertRTPPacketCount(t, endpoint, packetCount+1, func() {
		sendRTPFrom(t, probePeer, candidateAlias, packetCount+1, packetCount)
		sendRTPFrom(t, aliasPeer, sharedB, 2*packetCount+1, 1)
		barrier.mark(0)
	})

	endDialogB()
	endDialogA()
}
