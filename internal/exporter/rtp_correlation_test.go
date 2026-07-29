package exporter

import (
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aibudaevv/sip-exporter/internal/mediatracker"
	"github.com/aibudaevv/sip-exporter/internal/service"
)

type blockingRefreshDialoger struct {
	service.Dialoger

	started         chan struct{}
	continueRefresh chan struct{}
}

func (d *blockingRefreshDialoger) Refresh(dialogID string, expiresAt time.Time) bool {
	close(d.started)
	<-d.continueRefresh
	return d.Dialoger.Refresh(dialogID, expiresAt)
}

func makeRTPPayload(ssrc uint32) []byte {
	p := make([]byte, 12)
	p[0] = 0x80 // V=2, P=0, X=0, CC=0
	p[1] = 0x00 // M=0, PT=0 (PCMU)
	binary.BigEndian.PutUint16(p[2:4], 1)
	binary.BigEndian.PutUint32(p[4:8], 160)
	binary.BigEndian.PutUint32(p[8:12], ssrc)
	return p
}

// TestRTPCorrelationViaSDP verifies the full Group-4 pipeline: an INVITE with an
// SDP offer and a 200 OK with an SDP answer register two media endpoints, RTP
// from either endpoint is observed, and RTP without a correlated dialog is dropped.
func TestRTPCorrelationViaSDP(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}
	e := &exporter{
		services:       services{metricser: mm, dialoger: md},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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
	e.handleMessage("carrier-x", "", invite)

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
	e.handleMessage("carrier-x", "", ok200)

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

func TestLateInvite200OKDoesNotConsumeNewerSDP(t *testing.T) {
	e := newSDPCSeqTestExporter()

	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPMessage(
		"cseq-call", "1", "", mediaSDP("10.0.0.1", 4000),
	)))
	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPResponse(
		"cseq-call", "1", mediaSDP("10.0.0.2", 5000),
	)))
	requireEndpoint(t, e, "10.0.0.1", 4000, true)
	requireEndpoint(t, e, "10.0.0.2", 5000, true)

	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPMessage(
		"cseq-call", "2", "to-tag", mediaSDP("10.0.0.3", 6000),
	)))
	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPResponse(
		"cseq-call", "1", mediaSDP("10.0.0.2", 5000),
	)))
	requireEndpoint(t, e, "10.0.0.3", 6000, false)

	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPResponse(
		"cseq-call", "2", mediaSDP("10.0.0.4", 7000),
	)))
	requireEndpoint(t, e, "10.0.0.1", 4000, false)
	requireEndpoint(t, e, "10.0.0.2", 5000, false)
	requireEndpoint(t, e, "10.0.0.3", 6000, true)
	requireEndpoint(t, e, "10.0.0.4", 7000, true)
	require.Equal(t, map[rtpEndpointKey]uint{
		{IP: 0x0A000003, Port: 6000}: 1,
		{IP: 0x0A000003, Port: 6001}: 1,
		{IP: 0x0A000004, Port: 7000}: 1,
		{IP: 0x0A000004, Port: 7001}: 1,
	}, e.rtpEndpointRefs)
}

func TestSDPCSeqRejectedOfferIsNotUsedByOfferlessRefresh(t *testing.T) {
	e := newSDPCSeqTestExporter()

	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPMessage(
		"rejected-call", "1", "", mediaSDP("10.0.1.1", 4000),
	)))
	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPResponse(
		"rejected-call", "1", mediaSDP("10.0.1.2", 5000),
	)))
	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPMessage(
		"rejected-call", "2", "to-tag", mediaSDP("10.0.1.3", 6000),
	)))
	require.NoError(t, e.handleMessage("carrier-x", "", inviteResponse(
		"488 Not Acceptable Here", "rejected-call", "2",
	)))
	require.NoError(t, e.handleMessage("carrier-x", "", inviteMessage(
		"rejected-call", "3", "to-tag",
	)))
	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPResponse(
		"rejected-call", "3", mediaSDP("10.0.1.4", 7000),
	)))

	requireEndpoint(t, e, "10.0.1.1", 4000, true)
	requireEndpoint(t, e, "10.0.1.2", 5000, true)
	requireEndpoint(t, e, "10.0.1.3", 6000, false)
	requireEndpoint(t, e, "10.0.1.4", 7000, false)
}

func TestReinviteAfterExpiryDoesNotReplaceMedia(t *testing.T) {
	dialoger := &blockingRefreshDialoger{
		Dialoger:        service.NewDialoger(),
		started:         make(chan struct{}),
		continueRefresh: make(chan struct{}),
	}
	e := &exporter{
		services:        services{metricser: &mockMetricser{}, dialoger: dialoger},
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker:  make(map[string]optionsEntry),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
		rtpEndpointRefs: make(map[rtpEndpointKey]uint),
	}

	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPMessage(
		"expired-call", "1", "", mediaSDP("10.0.2.1", 4000),
	)))
	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPResponse(
		"expired-call", "1", mediaSDP("10.0.2.2", 5000),
	)))
	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPMessage(
		"expired-call", "2", "to-tag", mediaSDP("10.0.2.3", 6000),
	)))

	done := make(chan error, 1)
	go func() {
		done <- e.handleMessage("carrier-x", "", inviteSDPResponse(
			"expired-call", "2", mediaSDP("10.0.2.4", 7000),
		))
	}()
	<-dialoger.started
	dialogID, err := normalizeDialogID([]byte("expired-call"), []byte("from-tag"), []byte("to-tag"))
	require.NoError(t, err)
	dialoger.Delete(dialogID)
	e.unregisterMedia("expired-call")
	close(dialoger.continueRefresh)
	require.NoError(t, <-done)

	require.Empty(t, e.rtpEndpointRefs)
	requireEndpoint(t, e, "10.0.2.3", 6000, false)
	requireEndpoint(t, e, "10.0.2.4", 7000, false)
	offer, ok := e.takeInviteSDP("expired-call", "2")
	require.True(t, ok, "failed refresh must preserve the matching offer")
	require.Equal(t, mediaSDP("10.0.2.3", 6000), string(offer))
}

func TestReinviteRefreshAppliesMatchingRevisionOnce(t *testing.T) {
	e := newSDPCSeqTestExporter()

	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPMessage(
		"refresh-call", "1", "", mediaSDP("10.0.3.1", 4000),
	)))
	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPResponse(
		"refresh-call", "1", mediaSDP("10.0.3.2", 5000),
	)))
	require.NoError(t, e.handleMessage("carrier-x", "", inviteSDPMessage(
		"refresh-call", "2", "to-tag", mediaSDP("10.0.3.3", 6000),
	)))
	response := inviteSDPResponse("refresh-call", "2", mediaSDP("10.0.3.4", 7000))
	require.NoError(t, e.handleMessage("carrier-x", "", response))
	require.NoError(t, e.handleMessage("carrier-x", "", response))

	requireEndpoint(t, e, "10.0.3.1", 4000, false)
	requireEndpoint(t, e, "10.0.3.2", 5000, false)
	requireEndpoint(t, e, "10.0.3.3", 6000, true)
	requireEndpoint(t, e, "10.0.3.4", 7000, true)
	require.Equal(t, map[rtpEndpointKey]uint{
		{IP: 0x0A000303, Port: 6000}: 1,
		{IP: 0x0A000303, Port: 6001}: 1,
		{IP: 0x0A000304, Port: 7000}: 1,
		{IP: 0x0A000304, Port: 7001}: 1,
	}, e.rtpEndpointRefs)
	_, ok := e.takeInviteSDP("refresh-call", "2")
	require.False(t, ok, "successful refresh must consume the matching offer once")
}

func newSDPCSeqTestExporter() *exporter {
	return &exporter{
		services:       services{metricser: &mockMetricser{}, dialoger: &mockDialoger{}},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}
}

func inviteSDPMessage(callID, cseq, toTag, body string) []byte {
	return append(inviteMessage(callID, cseq, toTag), []byte(
		"Content-Type: application/sdp\r\n\r\n"+body,
	)...)
}

func inviteMessage(callID, cseq, toTag string) []byte {
	to := "To: <sip:b@example.com>"
	if toTag != "" {
		to += ";tag=" + toTag
	}
	return []byte(fmt.Sprintf("INVITE sip:b@example.com SIP/2.0\r\n"+
		"From: <sip:a@example.com>;tag=from-tag\r\n"+
		"%s\r\nCall-ID: %s\r\nCSeq: %s INVITE\r\n", to, callID, cseq))
}

func inviteSDPResponse(callID, cseq, body string) []byte {
	return append(inviteResponse("200 OK", callID, cseq), []byte(
		"Content-Type: application/sdp\r\n\r\n"+body,
	)...)
}

func inviteResponse(status, callID, cseq string) []byte {
	return []byte(fmt.Sprintf("SIP/2.0 %s\r\n"+
		"From: <sip:a@example.com>;tag=from-tag\r\n"+
		"To: <sip:b@example.com>;tag=to-tag\r\n"+
		"Call-ID: %s\r\nCSeq: %s INVITE\r\n", status, callID, cseq))
}

func mediaSDP(ip string, port uint16) string {
	return fmt.Sprintf("v=0\r\nc=IN IP4 %s\r\nm=audio %d RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n", ip, port)
}

func requireEndpoint(t *testing.T, e *exporter, ip string, port uint16, want bool) {
	t.Helper()
	_, got := e.mediaTracker.Lookup(ip, port)
	require.Equal(t, want, got, "%s:%d lookup", ip, port)
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

// TestRTPHandleRTPBranches exercises the four decision branches in handleRTP:
// ParseHeader error (drop), Counted=true (UpdateRTPPackets), Counted=false (skip),
// and Lost>0 (UpdateRTPLoss).
func TestRTPHandleRTPBranches(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}
	e := &exporter{
		services:       services{metricser: mm, dialoger: md},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestRTPSourceAliasLearning(t *testing.T) {
	tests := []struct {
		name         string
		sourceIP     net.IP
		wantAlias    bool
		wantRefCount uint
	}{
		{
			name:         "destination match learns remapped peer port",
			sourceIP:     net.IPv4(10, 0, 0, 1),
			wantAlias:    true,
			wantRefCount: 1,
		},
		{
			name:         "source IP mismatch counts packet without alias",
			sourceIP:     net.IPv4(10, 0, 0, 99),
			wantAlias:    false,
			wantRefCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := &mockMetricser{}
			e := &exporter{
				services:        services{metricser: mm, dialoger: &mockDialoger{}},
				mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
				rtpEndpointRefs: make(map[rtpEndpointKey]uint),
			}
			e.mediaTracker.Register("10.0.0.1", 4000, mediatracker.MediaLabels{
				Carrier: "carrier", UAType: "ua", CallID: "call-1",
				SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000},
			})
			e.mediaTracker.Register("10.0.0.2", 5000, mediatracker.MediaLabels{
				Carrier: "carrier", UAType: "ua", CallID: "call-1",
				SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000},
			})

			_, err := e.handleRTP(tt.sourceIP, 4100, net.IPv4(10, 0, 0, 2), 5000, makeRTPPayload(1))
			require.NoError(t, err)
			require.Equal(t, 1, mm.rtpPacketsCalls)
			if tt.wantAlias {
				_, err = e.handleRTP(
					tt.sourceIP, 4100, net.IPv4(10, 0, 0, 2), 5000, makeRTPPayloadSeq(1, 2),
				)
				require.NoError(t, err)
				require.Equal(t, 2, mm.rtpPacketsCalls)
			}

			alias := rtpEndpointKey{IP: 0x0A000001, Port: 4100}
			_, gotAlias := e.rtpEndpointRefs[alias]
			require.Equal(t, tt.wantAlias, gotAlias)
			require.Equal(t, tt.wantRefCount, e.rtpEndpointRefs[alias])
			require.Len(t, e.rtpEndpointRefs, int(tt.wantRefCount))
		})
	}
}

func TestRTPSourceAliasSharedDestinationCounted(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services:        services{metricser: mm, dialoger: &mockDialoger{}},
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
		rtpEndpointRefs: make(map[rtpEndpointKey]uint),
	}
	for _, registration := range []struct {
		ip     string
		port   uint16
		callID string
	}{
		{ip: "10.0.0.1", port: 4000, callID: "call-1"},
		{ip: "10.0.0.2", port: 5000, callID: "call-1"},
		{ip: "10.0.0.1", port: 4001, callID: "call-2"},
		{ip: "10.0.0.2", port: 5000, callID: "call-2"},
	} {
		e.mediaTracker.Register(registration.ip, registration.port, mediatracker.MediaLabels{
			Carrier: "carrier", UAType: "ua", CallID: registration.callID,
			SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000},
		})
	}

	_, err := e.handleRTP(
		net.IPv4(10, 0, 0, 1), 4100, net.IPv4(10, 0, 0, 2), 5000, makeRTPPayload(1),
	)
	require.NoError(t, err)
	require.Equal(t, 1, mm.rtpPacketsCalls)
	require.NotContains(t, e.rtpEndpointRefs, rtpEndpointKey{IP: 0x0A000001, Port: 4100})
}

func TestReinviteHoldReleasesLearnedAlias(t *testing.T) {
	dialoger := &mockDialoger{}
	e := &exporter{
		services:        services{metricser: &mockMetricser{}, dialoger: dialoger},
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
		rtpEndpointRefs: make(map[rtpEndpointKey]uint),
	}
	createActiveTestDialog(t, dialoger, "call-1")
	labels := mediatracker.MediaLabels{
		Carrier: "carrier", UAType: "ua", CallID: "call-1",
		SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000},
	}
	for _, endpoint := range []mediatracker.MediaEndpoint{
		{IP: "10.0.0.1", Port: 4000},
		{IP: "10.0.0.2", Port: 5000},
	} {
		e.mediaTracker.Register(endpoint.IP, endpoint.Port, labels)
		e.retainRTPEndpoint(endpoint.IP, endpoint.Port)
	}
	_, err := e.handleRTP(
		net.IPv4(10, 0, 0, 1), 4100, net.IPv4(10, 0, 0, 2), 5000, makeRTPPayload(1),
	)
	require.NoError(t, err)
	require.Len(t, e.rtpEndpointRefs, 3)
	e.storeInviteSDP("call-1", "", []byte(fasSdpHeld))

	require.NoError(t, e.handleInvite200OK(
		"carrier", "ua", "", "", fasInvite200OK("call-1", fasSdpHeld), true,
	))
	require.Empty(t, e.rtpEndpointRefs)
	_, ok := e.mediaTracker.Lookup("10.0.0.1", 4000)
	require.False(t, ok)
}

func TestInvite200OKRetransmitPreservesMediaRevision(t *testing.T) {
	dialoger := &mockDialoger{}
	e := &exporter{
		services:        services{metricser: &mockMetricser{}, dialoger: dialoger},
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
		rtpEndpointRefs: make(map[rtpEndpointKey]uint),
	}
	createActiveTestDialog(t, dialoger, "call-1")
	labels := mediatracker.MediaLabels{CallID: "call-1"}
	for _, endpoint := range []mediatracker.MediaEndpoint{
		{IP: "10.0.0.1", Port: 4000},
		{IP: "10.0.0.2", Port: 5000},
	} {
		e.mediaTracker.Register(endpoint.IP, endpoint.Port, labels)
		e.retainRTPEndpoint(endpoint.IP, endpoint.Port)
	}

	require.NoError(t, e.handleInvite200OK(
		"carrier", "ua", "", "", fasInvite200OK("call-1", fasSdpNormal), true,
	))
	require.Equal(t, map[rtpEndpointKey]uint{
		{IP: 0x0A000001, Port: 4000}: 1,
		{IP: 0x0A000002, Port: 5000}: 1,
	}, e.rtpEndpointRefs)
}

// TestParseRawPacketRTPDetection verifies that parseRawPacket routes
// packets with RTP version-2 prefix byte to handleRTP (not SIP parsing).
func TestParseRawPacketRTPDetection(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services:       services{metricser: mm, dialoger: &mockDialoger{}},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

// TestIsRTCPPayload verifies the RTCP/RTP disambiguation by payload-type byte.
// RTP and RTCP share V=2; the PT range is disjoint (RFC 5761). isRTCPPayload is
// called inside the V=2 branch of parseRawPacket, so it checks only PT + length.
func TestIsRTCPPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    bool
	}{
		{"SR (PT 200)", []byte{0x80, 200}, true},
		{"RR (PT 201)", []byte{0x80, 201}, true},
		{"APP (PT 204, upper bound)", []byte{0x80, 204}, true},
		{"below range (PT 199)", []byte{0x80, 199}, false},
		{"above range (PT 205)", []byte{0x80, 205}, false},
		{"RTP PCMU (PT 0)", []byte{0x80, 0}, false},
		{"RTP dynamic (PT 127)", []byte{0x80, 127}, false},
		{"single byte (no PT byte)", []byte{0x80}, false},
		{"empty", []byte{}, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isRTCPPayload(tt.payload))
		})
	}
}
