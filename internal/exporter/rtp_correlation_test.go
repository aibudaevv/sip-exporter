package exporter

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aibudaevv/sip-exporter/internal/mediatracker"
)

func makeRTPPayload(ssrc uint32) []byte {
	p := make([]byte, 12)
	p[0] = 0x80 // V=2, P=0, X=0, CC=0
	p[1] = 0x00 // M=0, PT=0 (PCMU)
	binary.BigEndian.PutUint16(p[2:4], 1)
	binary.BigEndian.PutUint32(p[4:8], 160)
	binary.BigEndian.PutUint32(p[8:12], ssrc)
	return p
}

// TestRTP_CorrelationViaSDP verifies the full Group-4 pipeline: an INVITE with an
// SDP offer and a 200 OK with an SDP answer register two media endpoints, RTP
// from either endpoint is observed, and RTP without a correlated dialog is dropped.
func TestRTP_CorrelationViaSDP(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}
	e := &exporter{
		services:       services{metricser: mm, dialoger: md},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	invite := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:a@b>;tag=fromtag\r\n" +
		"To: <sip:c@d>\r\n" +
		"Call-ID: rtp-corr-1\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Type: application/sdp\r\n" +
		"\r\n" +
		"v=0\r\n" +
		"o=- 1 1 IN IP4 10.0.0.1\r\n" +
		"s=-\r\n" +
		"c=IN IP4 10.0.0.1\r\n" +
		"t=0 0\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n")
	e.handleMessage("carrier-x", invite)

	ok200 := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:a@b>;tag=fromtag\r\n" +
		"To: <sip:c@d>;tag=totag\r\n" +
		"Call-ID: rtp-corr-1\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Type: application/sdp\r\n" +
		"\r\n" +
		"c=IN IP4 10.0.0.2\r\n" +
		"m=audio 5006 RTP/AVP 0\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n")
	e.handleMessage("carrier-x", ok200)

	require.Len(t, md.created, 1, "INVITE 200 OK must create a dialog")

	// Caller-side RTP (matches the INVITE SDP endpoint).
	e.handleRTP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(0, 0, 0, 0), 0, makeRTPPayload(0xAABBCCDD))
	require.Equal(t, 1, e.mediaTracker.StreamCount(), "caller RTP must be observed")

	// Callee-side RTP (matches the 200 OK SDP endpoint).
	e.handleRTP(net.IPv4(10, 0, 0, 2), 5006, net.IPv4(0, 0, 0, 0), 0, makeRTPPayload(0x11223344))
	require.Equal(t, 2, e.mediaTracker.StreamCount(), "callee RTP must be observed")

	// RTP with no correlated endpoint is dropped.
	e.handleRTP(net.IPv4(9, 9, 9, 9), 1234, net.IPv4(0, 0, 0, 0), 0, makeRTPPayload(0xCAFEBABE))
	require.Equal(t, 2, e.mediaTracker.StreamCount(), "uncorrelated RTP must be dropped")

	// Labels (carrier/call-id) and codec propagate to the tracked streams.
	for _, s := range e.mediaTracker.Snapshot() {
		require.Equal(t, "carrier-x", s.Carrier)
		require.Equal(t, "rtp-corr-1", s.CallID)
		require.Equal(t, "PCMU", s.Codec)
	}
}

func makeRTPPayloadSeq(ssrc uint32, seq uint16) []byte {
	p := make([]byte, 12)
	p[0] = 0x80
	p[1] = 0x00
	binary.BigEndian.PutUint16(p[2:4], seq)
	binary.BigEndian.PutUint32(p[4:8], 160)
	binary.BigEndian.PutUint32(p[8:12], ssrc)
	return p
}

// TestRTP_HandleRTP_Branches exercises the four decision branches in handleRTP:
// ParseHeader error (drop), Counted=true (UpdateRTPPackets), Counted=false (skip),
// and Lost>0 (UpdateRTPLoss).
func TestRTP_HandleRTP_Branches(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}
	e := &exporter{
		services:       services{metricser: mm, dialoger: md},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}
	e.mediaTracker.Register("10.0.0.1", 5004, mediatracker.MediaLabels{
		Carrier:    "c",
		UAType:     "u",
		CallID:     "call-x",
		SDPCodecs:  map[uint8]string{0: "PCMU"},
		ClockRates: map[uint8]uint32{0: 8000},
	})

	// 1. ParseHeader error: 5-byte payload with V=2 but too short
	shortPayload := []byte{0x80, 0x00, 0x00, 0x01, 0x00}
	e.handleRTP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(0, 0, 0, 0), 0, shortPayload)
	require.Equal(t, 0, e.mediaTracker.StreamCount(), "invalid RTP header must be dropped")
	require.Equal(t, 0, mm.rtpPacketsCalls, "ParseHeader error must not call UpdateRTPPackets")

	// 2. First packet: Counted=true → UpdateRTPPackets called, Lost=0
	e.handleRTP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(0, 0, 0, 0), 0, makeRTPPayloadSeq(0xAA, 1))
	require.Equal(t, 1, mm.rtpPacketsCalls, "first packet must be counted")
	require.Equal(t, 0, mm.rtpLossCalls, "first packet must not report loss")

	// 3. Gap packet (seq=5): Counted=true, Lost>0 → UpdateRTPLoss called
	e.handleRTP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(0, 0, 0, 0), 0, makeRTPPayloadSeq(0xAA, 5))
	require.Equal(t, 2, mm.rtpPacketsCalls)
	require.Equal(t, 1, mm.rtpLossCalls, "gap must report loss")
	require.Equal(t, uint64(3), mm.rtpLossValue)

	// 4. Duplicate (seq=5): Counted=false → UpdateRTPPackets NOT called
	e.handleRTP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(0, 0, 0, 0), 0, makeRTPPayloadSeq(0xAA, 5))
	require.Equal(t, 2, mm.rtpPacketsCalls, "duplicate must not be counted")
}

// TestParseRawPacket_RTPDetection verifies that parseRawPacket routes
// packets with RTP version-2 prefix byte to handleRTP (not SIP parsing).
func TestParseRawPacket_RTPDetection(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services:       services{metricser: mm, dialoger: &mockDialoger{}},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}
	e.mediaTracker.Register("10.0.0.1", 5004, mediatracker.MediaLabels{
		Carrier: "c", UAType: "u", CallID: "call-r",
		SDPCodecs:  map[uint8]string{0: "PCMU"},
		ClockRates: map[uint8]uint32{0: 8000},
	})

	// Build raw Ethernet/IPv4/UDP/RTP packet
	pkt := make([]byte, 54) // 14 eth + 20 ip + 8 udp + 12 rtp
	pkt[12] = 0x08          // IPv4
	pkt[13] = 0x00
	pkt[14] = 0x45 // IPv4, IHL=5
	pkt[23] = 17   // UDP
	pkt[26] = 10   // src IP 10.0.0.9
	pkt[30] = 10   // dst IP 10.0.0.1 (registered endpoint)
	pkt[31] = 0
	pkt[32] = 0
	pkt[33] = 1
	binary.BigEndian.PutUint16(pkt[34:36], 12345) // src port
	binary.BigEndian.PutUint16(pkt[36:38], 5004)  // dst port (RTP endpoint)
	// RTP header at offset 42
	rtpHdr := makeRTPPayloadSeq(0xBB, 1)
	copy(pkt[42:], rtpHdr)

	errType, err := e.parseRawPacket(pkt)
	require.NoError(t, err, "RTP packet must not produce parse error")
	require.Empty(t, errType)
	require.Equal(t, 1, e.mediaTracker.StreamCount(), "RTP must be observed via parseRawPacket")
}
