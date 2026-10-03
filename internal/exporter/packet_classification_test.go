package exporter

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aibudaevv/sip-exporter/internal/mediatracker"
)

func TestParseRawPacketClassifiesBeforeSIPSourceEnrichment(t *testing.T) {
	const sentinelSourceIP = "unchanged"
	tests := []struct {
		name        string
		packet      []byte
		setup       func(*exporter)
		wantErrType string
		wantErr     bool
		wantSource  string
		verify      func(*testing.T, *mockMetricser)
	}{
		{
			name:       "SIP enriches source IP",
			packet:     rawUDPPacket(optionsPayload("sip")),
			wantSource: "10.0.0.1",
			verify: func(t *testing.T, mm *mockMetricser) {
				require.Equal(t, 1, mm.requestCount)
			},
		},
		{
			name:       "RTP bypasses SIP source enrichment",
			packet:     rawUDPPacket(makeRTPPayloadSeq(0xAABBCCDD, 1)),
			wantSource: sentinelSourceIP,
			setup: func(e *exporter) {
				e.mediaTracker.Register("10.0.0.2", 5004, mediatracker.MediaLabels{
					Carrier: "carrier-a", UAType: "phone", CallID: "call-1",
					SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000},
				})
			},
			verify: func(t *testing.T, mm *mockMetricser) {
				require.Equal(t, 1, mm.rtpPacketsCalls)
			},
		},
		{
			name:       "RTCP bypasses SIP source enrichment",
			packet:     rawUDPPacket(buildRR(buildRTCPBlock(0xDEADBEEF, 0, 0, 0, 0, 0, 0))),
			wantSource: sentinelSourceIP,
			verify: func(t *testing.T, mm *mockMetricser) {
				require.Equal(t, 1, mm.rtcpOrphanCalls)
			},
		},
		{
			name:        "malformed UDP bypasses SIP source enrichment",
			packet:      malformedUDPPacket(),
			wantErrType: parseErrTypeL4,
			wantErr:     true,
			wantSource:  sentinelSourceIP,
		},
		{
			name:        "non-RTP UDP bypasses SIP source enrichment",
			packet:      rawUDPPacket([]byte(strings.Repeat("G", minSIPDataLen))),
			wantErrType: parseErrTypeSIP,
			wantErr:     true,
			wantSource:  sentinelSourceIP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := &mockMetricser{}
			e := &exporter{
				services:       services{metricser: mm, dialoger: &mockDialoger{}},
				mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
				optionsTracker: make(map[string]optionsEntry),
				pktSrcIP:       sentinelSourceIP,
			}
			if tt.setup != nil {
				tt.setup(e)
			}

			errType, err := e.parseRawPacket(tt.packet)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantErrType, errType)
			require.Equal(t, tt.wantSource, e.pktSrcIP)
			if tt.verify != nil {
				tt.verify(t, mm)
			}
		})
	}
}

func optionsPayload(callID string) []byte {
	return []byte("OPTIONS sip:test SIP/2.0\r\nFrom: <sip:a@test>;tag=1\r\nTo: <sip:b@test>\r\nCall-ID: " +
		callID + "\r\nCSeq: 1 OPTIONS\r\n\r\n")
}

func rawUDPPacket(payload []byte) []byte {
	packet := make([]byte, ethHeaderLen+ipV4MinHeaderLen+udpHeaderLen+len(payload))
	packet[12] = ethTypeIPv4Hi
	packet[13] = ethTypeIPv4Lo
	packet[14] = 0x45
	packet[23] = ipProtoUDP
	copy(packet[26:30], net.IPv4(10, 0, 0, 1).To4())
	copy(packet[30:34], net.IPv4(10, 0, 0, 2).To4())
	binary.BigEndian.PutUint16(packet[34:36], 4000)
	binary.BigEndian.PutUint16(packet[36:38], 5004)
	copy(packet[42:], payload)
	return packet
}

func malformedUDPPacket() []byte {
	packet := make([]byte, minRawPacketLen)
	packet[12] = ethTypeIPv4Hi
	packet[13] = ethTypeIPv4Lo
	packet[14] = 0x49
	packet[23] = ipProtoUDP
	copy(packet[26:30], net.IPv4(10, 0, 0, 1).To4())
	copy(packet[30:34], net.IPv4(10, 0, 0, 2).To4())
	return packet
}
