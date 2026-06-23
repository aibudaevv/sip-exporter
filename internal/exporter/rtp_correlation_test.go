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
	e.handleRTP(net.IPv4(10, 0, 0, 1), 5004, makeRTPPayload(0xAABBCCDD))
	require.Equal(t, 1, e.mediaTracker.StreamCount(), "caller RTP must be observed")

	// Callee-side RTP (matches the 200 OK SDP endpoint).
	e.handleRTP(net.IPv4(10, 0, 0, 2), 5006, makeRTPPayload(0x11223344))
	require.Equal(t, 2, e.mediaTracker.StreamCount(), "callee RTP must be observed")

	// RTP with no correlated endpoint is dropped.
	e.handleRTP(net.IPv4(9, 9, 9, 9), 1234, makeRTPPayload(0xCAFEBABE))
	require.Equal(t, 2, e.mediaTracker.StreamCount(), "uncorrelated RTP must be dropped")

	// Labels (carrier/call-id) and codec propagate to the tracked streams.
	for _, s := range e.mediaTracker.Snapshot() {
		require.Equal(t, "carrier-x", s.Carrier)
		require.Equal(t, "rtp-corr-1", s.CallID)
		require.Equal(t, "PCMU", s.Codec)
	}
}
