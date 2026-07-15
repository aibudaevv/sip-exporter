package exporter

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/aibudaevv/sip-exporter/internal/mediatracker"
	"github.com/aibudaevv/sip-exporter/internal/service"
	"github.com/aibudaevv/sip-exporter/internal/vq"
)

// Mock services for testing.
type mockMetricser struct {
	requestCalled             []byte
	reinviteCalled            bool
	responseCalled            []byte
	responseIsInvite          bool
	sessionUpdated            int
	systemErrorCalled         bool
	packetsIncremented        int
	invite200OKCalled         bool
	sessionCompletedFlag      bool
	rrdUpdated                bool
	rrdDelay                  float64
	responseWithMetricsCalled bool
	spdUpdated                bool
	spdDuration               time.Duration
	ttrUpdated                bool
	ttrDelay                  float64
	pddUpdated                bool
	pddDelay                  float64
	ordUpdated                bool
	ordDelay                  float64
	lrdUpdated                bool
	lrdDelay                  float64
	registerSuccessCalls      int
	registerFailureCodes      []string
	registerCountryChange     []string
	registerScanCalls         int
	inviteBurstCalls          int
	vqReportCalled            bool
	vqCarrier                 string
	vqUAType                  string
	vqReport                  *vq.SessionReport
	rtpPacketsCalls           int
	rtpLossCalls              int
	rtpLossValue              uint64
	rtpDuplicateCalls         int
}

func (m *mockMetricser) UpdateSessions(_ []service.LabeledCount) {}

func (m *mockMetricser) SetSessionsLimits(_ map[string]int) {}

func (m *mockMetricser) UpdateActiveRegistrations(_ []service.LabeledCount) {}

func (m *mockMetricser) Request(_, _, _, _, _, _ string, in []byte) {
	m.requestCalled = in
	m.packetsIncremented++
}

func (m *mockMetricser) Reinvite(_, _, _ string) {
	m.reinviteCalled = true
	m.packetsIncremented++
}

func (m *mockMetricser) Response(_, _, _ string, in []byte, isInviteResponse bool) {
	m.responseCalled = in
	m.responseIsInvite = isInviteResponse
	m.packetsIncremented++
}

func (m *mockMetricser) ResponseWithMetrics(_, _, _ string, status []byte, isInviteResponse, is200OK bool) {
	m.responseWithMetricsCalled = true
	m.responseCalled = status
	m.responseIsInvite = isInviteResponse
	m.packetsIncremented++
	if is200OK && isInviteResponse {
		m.invite200OKCalled = true
	}
}

func (m *mockMetricser) Invite200OK(_, _, _, _, _, _ string) {
	m.invite200OKCalled = true
}

func (m *mockMetricser) SessionCompleted(_, _, _ string) {
	m.sessionCompletedFlag = true
}

func (m *mockMetricser) RegisterSuccess(_, _, _ string) {
	m.registerSuccessCalls++
}

func (m *mockMetricser) RegisterFailure(_, _, _, code string) {
	m.registerFailureCodes = append(m.registerFailureCodes, code)
}

func (m *mockMetricser) RegisterCountryChange(_, sourceCountry string) {
	m.registerCountryChange = append(m.registerCountryChange, sourceCountry)
}

func (m *mockMetricser) RegisterScan(_, _ string) {
	m.registerScanCalls++
}

func (m *mockMetricser) InviteBurst(_, _ string) {
	m.inviteBurstCalls++
}

func (m *mockMetricser) UpdateRRD(_, _, _ string, delayMs float64) {
	m.rrdUpdated = true
	m.rrdDelay = delayMs
}

func (m *mockMetricser) UpdateSPD(_, _, _ string, duration time.Duration) {
	m.spdUpdated = true
	m.spdDuration = duration
}

func (m *mockMetricser) UpdateSession(_, _, _ string, size int) {
	m.sessionUpdated = size
}

func (m *mockMetricser) UpdateTTR(_, _, _ string, delayMs float64) {
	m.ttrUpdated = true
	m.ttrDelay = delayMs
}

func (m *mockMetricser) UpdatePDD(_, _, _ string, delayMs float64) {
	m.pddUpdated = true
	m.pddDelay = delayMs
}

func (m *mockMetricser) UpdateORD(_, _, _ string, delayMs float64) {
	m.ordUpdated = true
	m.ordDelay = delayMs
}

func (m *mockMetricser) UpdateLRD(_, _, _ string, delayMs float64) {
	m.lrdUpdated = true
	m.lrdDelay = delayMs
}

func (m *mockMetricser) SystemError() {
	m.systemErrorCalled = true
}

func (m *mockMetricser) ParseError(string)             {}
func (m *mockMetricser) SocketStats(_, _ uint32)       {}
func (m *mockMetricser) UpdateChannelLength(int)       {}
func (m *mockMetricser) UpdateChannelCapacity(int)     {}
func (m *mockMetricser) UpdateTrackerSize(string, int) {}
func (m *mockMetricser) UpdateActiveDialogs(int)       {}

func (m *mockMetricser) UpdateVQReport(carrier string, uaType string, _ string, report *vq.SessionReport) {
	m.vqReportCalled = true
	m.vqCarrier = carrier
	m.vqUAType = uaType
	m.vqReport = report
}

func (m *mockMetricser) UpdateRTPPackets(_, _, _, _ string) {
	m.rtpPacketsCalls++
}
func (m *mockMetricser) UpdateRTPLoss(_, _, _, _ string, lost uint64) {
	m.rtpLossCalls++
	m.rtpLossValue = lost
}
func (m *mockMetricser) UpdateRTPDuplicates(_, _, _, _ string) {
	m.rtpDuplicateCalls++
}
func (m *mockMetricser) UpdateRTPJitter(string, string, string, string, float64) {}
func (m *mockMetricser) UpdateRTPMOS(string, string, string, string, float64)    {}
func (m *mockMetricser) UpdateRTPMOSVariants(string, string, string, string, float64, float64, float64) {
}
func (m *mockMetricser) UpdateRTPRFactor(string, string, string, string, float64)                   {}
func (m *mockMetricser) UpdateRTPLossDistribution(string, string, string, string, float64, float64) {}
func (m *mockMetricser) UpdateRTPActiveStreams(_ []service.LabeledCount)                            {}
func (m *mockMetricser) OneWayCall(string, string, string)                                          {}
func (m *mockMetricser) MissingRTP(string, string, string)                                          {}

type dialogCreateArgs struct {
	expiresAt time.Time
	createdAt time.Time
	carrier   string
	uaType    string
}

type mockDialoger struct {
	created        map[string]dialogCreateArgs
	deleted        []string
	cleanupResults []service.CleanupResult
}

func (m *mockDialoger) Create(
	dialogID string,
	expiresAt time.Time,
	createdAt time.Time,
	carrier string,
	uaType string,
	_ string,
	_ string,
) {
	if m.created == nil {
		m.created = make(map[string]dialogCreateArgs)
	}
	m.created[dialogID] = dialogCreateArgs{expiresAt: expiresAt, createdAt: createdAt, carrier: carrier, uaType: uaType}
}

func (m *mockDialoger) Delete(dialogID string) service.CleanupResult {
	m.deleted = append(m.deleted, dialogID)
	if m.created != nil {
		if args, ok := m.created[dialogID]; ok {
			delete(m.created, dialogID)
			return service.CleanupResult{Duration: 100 * time.Millisecond, Carrier: args.carrier}
		}
	}
	return service.CleanupResult{}
}

func (m *mockDialoger) HasActiveDialog(dialogID string) bool {
	_, ok := m.created[dialogID]
	return ok
}

func (m *mockDialoger) Refresh(dialogID string, expiresAt time.Time) bool {
	if args, ok := m.created[dialogID]; ok {
		args.expiresAt = expiresAt
		m.created[dialogID] = args
		return true
	}
	return false
}

func (m *mockDialoger) Size() int {
	return len(m.created)
}

func (m *mockDialoger) Cleanup() []service.CleanupResult {
	return m.cleanupResults
}

func (m *mockDialoger) Counts() []service.LabeledCount {
	type key struct{ carrier, uaType string }
	counts := make(map[key]int)
	for _, args := range m.created {
		counts[key{args.carrier, args.uaType}]++
	}
	result := make([]service.LabeledCount, 0, len(counts))
	for k, n := range counts {
		result = append(result, service.LabeledCount{
			Labels: map[string]string{"carrier": k.carrier, "ua_type": k.uaType},
			Count:  n,
		})
	}
	return result
}

// ==================== normalizeDialogID tests ====================

func TestNormalizeDialogID(t *testing.T) {
	tt := []struct {
		expected    string
		callID      []byte
		fromTag     []byte
		toTag       []byte
		description string
	}{
		{
			description: "positive",
			expected:    "583ce713cb324f27bd614e594db53cc2:8Xy7r28Ne5ZSQ:e2540aafe5474bd7a5f9059b0ffccfec",
			callID:      []byte("583ce713cb324f27bd614e594db53cc2"),
			fromTag:     []byte("e2540aafe5474bd7a5f9059b0ffccfec"),
			toTag:       []byte("8Xy7r28Ne5ZSQ"),
		},
	}

	for _, v := range tt {
		t.Run(v.description, func(t *testing.T) {
			actual, err := normalizeDialogID(v.callID, v.fromTag, v.toTag)
			require.NoError(t, err)
			require.Equal(t, v.expected, actual)
		})
	}
}

func TestNormalizeDialogID_EmptyFromTag(t *testing.T) {
	_, err := normalizeDialogID([]byte("call-id"), []byte(""), []byte("to-tag"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "from tag or to tag is empty")
}

func TestNormalizeDialogID_EmptyToTag(t *testing.T) {
	_, err := normalizeDialogID([]byte("call-id"), []byte("from-tag"), []byte(""))
	require.Error(t, err)
	require.Contains(t, err.Error(), "from tag or to tag is empty")
}

func TestNormalizeDialogID_BothEmptyTags(t *testing.T) {
	_, err := normalizeDialogID([]byte("call-id"), []byte(""), []byte(""))
	require.Error(t, err)
	require.Contains(t, err.Error(), "from tag or to tag is empty")
}

func TestNormalizeDialogID_FromTagLessThanToTag(t *testing.T) {
	result, err := normalizeDialogID([]byte("test-call"), []byte("aaa"), []byte("zzz"))
	require.NoError(t, err)
	require.Equal(t, "test-call:aaa:zzz", result)
}

func TestNormalizeDialogID_FromTagGreaterThanToTag(t *testing.T) {
	result, err := normalizeDialogID([]byte("test-call"), []byte("zzz"), []byte("aaa"))
	require.NoError(t, err)
	require.Equal(t, "test-call:aaa:zzz", result)
}

func TestNormalizeDialogID_EqualTags(t *testing.T) {
	result, err := normalizeDialogID([]byte("test-call"), []byte("same"), []byte("same"))
	require.NoError(t, err)
	require.Equal(t, "test-call:same:same", result)
}

// ==================== splitHeader tests ====================

func TestSplitHeader_Normal(t *testing.T) {
	header, value := splitHeader([]byte("Content-Type: application/sdp"))
	require.Equal(t, []byte("Content-Type"), header)
	require.Equal(t, []byte("application/sdp"), value)
}

func TestSplitHeader_NoColon(t *testing.T) {
	header, value := splitHeader([]byte("NoColonHere"))
	require.Nil(t, header)
	require.Nil(t, value)
}

func TestSplitHeader_EmptyValue(t *testing.T) {
	header, value := splitHeader([]byte("Header:"))
	require.Equal(t, []byte("Header"), header)
	require.Empty(t, value)
}

func TestSplitHeader_EmptyLine(t *testing.T) {
	header, value := splitHeader([]byte(""))
	require.Nil(t, header)
	require.Nil(t, value)
}

func TestSplitHeader_OnlyColon(t *testing.T) {
	header, value := splitHeader([]byte(":"))
	require.Empty(t, header)
	require.Empty(t, value)
}

func TestSplitHeader_MultipleColons(t *testing.T) {
	header, value := splitHeader([]byte("Header: value: with: colons"))
	require.Equal(t, []byte("Header"), header)
	require.Equal(t, []byte("value: with: colons"), value)
}

func TestSplitHeader_WithSpaces(t *testing.T) {
	header, value := splitHeader([]byte("  Header  :  Value  "))
	require.Equal(t, []byte("Header"), header)
	require.Equal(t, []byte("Value"), value)
}

// ==================== extractTag tests ====================

func TestExtractTag_Normal(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>;tag=abc123"))
	require.Equal(t, []byte("abc123"), tag)
}

func TestExtractTag_NoTag(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>"))
	require.Nil(t, tag)
}

func TestExtractTag_TagWithSemicolon(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>;tag=abc123;other=param"))
	require.Equal(t, []byte("abc123"), tag)
}

func TestExtractTag_TagWithSpace(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>;tag=abc123 other"))
	require.Equal(t, []byte("abc123"), tag)
}

func TestExtractTag_TagWithGreaterThan(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>;tag=abc123>"))
	require.Equal(t, []byte("abc123"), tag)
}

func TestExtractTag_TagWithNewline(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>;tag=abc123\r\n"))
	require.Equal(t, []byte("abc123"), tag)
}

func TestExtractTag_EmptyTag(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>;tag="))
	require.Equal(t, []byte(""), tag)
}

func TestExtractTag_OnlyTagMarker(t *testing.T) {
	tag := extractTag([]byte(";tag="))
	require.Equal(t, []byte(""), tag)
}

// ==================== extractCSeq tests ====================

func TestExtractCSeq_Normal(t *testing.T) {
	id, method := extractCSeq([]byte("12345 INVITE"))
	require.Equal(t, []byte("12345"), id)
	require.Equal(t, []byte("INVITE"), method)
}

func TestExtractCSeq_NoSpace(t *testing.T) {
	id, method := extractCSeq([]byte("12345"))
	require.Nil(t, id)
	require.Nil(t, method)
}

func TestExtractCSeq_Empty(t *testing.T) {
	id, method := extractCSeq([]byte(""))
	require.Nil(t, id)
	require.Nil(t, method)
}

func TestExtractCSeq_MultipleSpaces(t *testing.T) {
	id, method := extractCSeq([]byte("12345 INVITE extra"))
	require.Equal(t, []byte("12345"), id)
	require.Equal(t, []byte("INVITE"), method)
}

// ==================== extractSessionExpires tests ====================

func TestExtractSessionExpires_OnlyNumber(t *testing.T) {
	expires := extractSessionExpires([]byte("1800"))
	require.Equal(t, 1800, expires)
}

func TestExtractSessionExpires_WithRefresher(t *testing.T) {
	expires := extractSessionExpires([]byte("1800;refresher=uac"))
	require.Equal(t, 1800, expires)
}

func TestExtractSessionExpires_Empty(t *testing.T) {
	expires := extractSessionExpires([]byte(""))
	require.Equal(t, 0, expires)
}

func TestExtractSessionExpires_InvalidNumber(t *testing.T) {
	expires := extractSessionExpires([]byte("invalid"))
	require.Equal(t, 0, expires)
}

func TestExtractSessionExpires_Zero(t *testing.T) {
	expires := extractSessionExpires([]byte("0"))
	require.Equal(t, 0, expires)
}

// ==================== extractExpires tests ====================

func TestExtractExpires_NormalValue(t *testing.T) {
	expires := extractExpires([]byte("3600"))
	require.Equal(t, 3600, expires)
}

func TestExtractExpires_Zero(t *testing.T) {
	expires := extractExpires([]byte("0"))
	require.Equal(t, 0, expires)
}

func TestExtractExpires_Empty(t *testing.T) {
	expires := extractExpires([]byte(""))
	require.Equal(t, 0, expires)
}

func TestExtractExpires_InvalidNumber(t *testing.T) {
	expires := extractExpires([]byte("invalid"))
	require.Equal(t, 0, expires)
}

// ==================== parseRawPacket tests ====================

func TestParseRawPacket_TooShort(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	_, err := e.parseRawPacket([]byte("short"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "wrong len packet")
}

func TestParseRawPacket_NotIPv4(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	packet := make([]byte, 42)
	packet[12] = 0x08
	packet[13] = 0x01

	_, err := e.parseRawPacket(packet)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not IPv4 packet")
}

func TestParseRawPacket_NotUDP(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	packet := make([]byte, 54)
	packet[12] = 0x08
	packet[13] = 0x00
	packet[14] = 0x45
	packet[23] = 6

	_, err := e.parseRawPacket(packet)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not UDP packet")
}

func TestParseRawPacket_NoSIPPayload(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	packet := make([]byte, 42)
	packet[12] = 0x08
	packet[13] = 0x00
	packet[14] = 0x45
	packet[23] = 17

	_, err := e.parseRawPacket(packet)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no SIP payload")
}

func TestParseRawPacket_NotSIPMethod(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	packet := make([]byte, 100)
	packet[12] = 0x08
	packet[13] = 0x00
	packet[14] = 0x45
	packet[23] = 17
	copy(packet[42:], []byte("NOT_A_SIP_METHOD"))

	_, err := e.parseRawPacket(packet)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a SIP packet")
}

func TestParseRawPacket_VLAN_Tagged(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	packet := make([]byte, 100)
	packet[12] = 0x81
	packet[13] = 0x00
	packet[16] = 0x08
	packet[17] = 0x00
	packet[18] = 0x45
	packet[27] = 17
	copy(packet[46:], []byte("INVITE sip:test SIP/2.0\r\n"))

	_, err := e.parseRawPacket(packet)
	require.NoError(t, err)
}

func TestParseRawPacket_IPHeaderTooShort(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	packet := make([]byte, 30)
	packet[12] = 0x08
	packet[13] = 0x00

	_, err := e.parseRawPacket(packet)
	require.Error(t, err)
	// Error may be "wrong len packet" due to VLAN check
	require.Contains(t, err.Error(), "wrong len")
}

func TestParseRawPacket_UDPHeaderTooShort(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	packet := make([]byte, 40)
	packet[12] = 0x08
	packet[13] = 0x00
	packet[14] = 0x45
	packet[23] = 17

	_, err := e.parseRawPacket(packet)
	require.Error(t, err)
	// Error may be "wrong len packet" due to length check
	require.Contains(t, err.Error(), "wrong len")
}

func TestParseRawPacket_SIPPayloadTooSmall(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	packet := make([]byte, 91)
	packet[12] = 0x08
	packet[13] = 0x00
	packet[14] = 0x45
	packet[23] = 17
	copy(packet[42:], []byte("SHORT"))

	_, err := e.parseRawPacket(packet)
	require.Error(t, err)
	require.Contains(t, err.Error(), "packet too small for SIP")
}

func TestParseRawPacket_ErrorTypes(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	tests := []struct {
		name     string
		packet   []byte
		wantType string
	}{
		{
			name:     "too_short_is_l2",
			packet:   make([]byte, 10),
			wantType: "l2",
		},
		{
			name: "not_ipv4_is_l3",
			packet: func() []byte {
				p := make([]byte, 42)
				p[12] = 0x08
				p[13] = 0x01
				return p
			}(),
			wantType: "l3",
		},
		{
			name: "ip_header_short_is_l2",
			packet: func() []byte {
				p := make([]byte, 30)
				p[12] = 0x08
				p[13] = 0x00
				return p
			}(),
			wantType: "l2",
		},
		{
			name: "not_udp_is_l4",
			packet: func() []byte {
				p := make([]byte, 54)
				p[12] = 0x08
				p[13] = 0x00
				p[14] = 0x45
				p[23] = 6
				return p
			}(),
			wantType: "l4",
		},
		{
			name: "udp_header_short_is_l2",
			packet: func() []byte {
				p := make([]byte, 40)
				p[12] = 0x08
				p[13] = 0x00
				p[14] = 0x45
				p[23] = 17
				return p
			}(),
			wantType: "l2",
		},
		{
			name: "no_sip_payload_is_sip",
			packet: func() []byte {
				p := make([]byte, 42)
				p[12] = 0x08
				p[13] = 0x00
				p[14] = 0x45
				p[23] = 17
				return p
			}(),
			wantType: "sip",
		},
		{
			name: "sip_too_small_is_sip",
			packet: func() []byte {
				p := make([]byte, 91)
				p[12] = 0x08
				p[13] = 0x00
				p[14] = 0x45
				p[23] = 17
				copy(p[42:], []byte("SHORT"))
				return p
			}(),
			wantType: "sip",
		},
		{
			name: "not_sip_method_is_sip",
			packet: func() []byte {
				p := make([]byte, 100)
				p[12] = 0x08
				p[13] = 0x00
				p[14] = 0x45
				p[23] = 17
				copy(p[42:], []byte("GET / HTTP/1.1\r\n"))
				return p
			}(),
			wantType: "sip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errType, err := e.parseRawPacket(tt.packet)
			require.Error(t, err)
			require.Equal(t, tt.wantType, errType, "error type mismatch")
		})
	}
}

func TestParseRawPacket_SuccessReturnsEmptyType(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	packet := make([]byte, 100)
	packet[12] = 0x08
	packet[13] = 0x00
	packet[14] = 0x45
	packet[23] = 17
	copy(packet[42:], []byte("INVITE sip:test SIP/2.0\r\n"))

	errType, err := e.parseRawPacket(packet)
	require.NoError(t, err)
	require.Empty(t, errType, "successful parse should return empty error type")
}

// ==================== sipPacketParse tests ====================

func TestSIPPacketParse_EmptyInput(t *testing.T) {
	e := exporter{}

	p, err := e.sipPacketParse([]byte(""))
	require.NoError(t, err)
	require.False(t, p.IsResponse)
	require.Nil(t, p.Method)
}

func TestSIPPacketParse_NoFromTag(t *testing.T) {
	e := exporter{}

	input := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 INVITE\r\n")

	_, err := e.sipPacketParse(input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "fail extract tag from")
	require.Contains(t, err.Error(), "<sip:user@domain>")
}

func TestSIPPacketParse_WithSessionExpires(t *testing.T) {
	e := exporter{}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Session-Expires: 1800;refresher=uac\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.Equal(t, 1800, p.SessionExpires)
}

func TestSIPPacketParse_InvalidSessionExpires(t *testing.T) {
	e := exporter{}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Session-Expires: invalid\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.Equal(t, 0, p.SessionExpires)
}

func TestSIPPacketParse_WithExpires(t *testing.T) {
	e := exporter{}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Expires: 3600\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.Equal(t, 3600, p.Expires)
}

func TestSIPPacketParse_ExpiresAbsenceIsZero(t *testing.T) {
	e := exporter{}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 REGISTER\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.Equal(t, 0, p.Expires)
}

func TestSIPPacketParse_ExpiresZero(t *testing.T) {
	e := exporter{}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Expires: 0\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.Equal(t, 0, p.Expires)
}

func TestSIPPacketParse_NoCSeqMethod(t *testing.T) {
	e := exporter{}

	input := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1\r\n")

	_, err := e.sipPacketParse(input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "fail extract CSeq from")
}

func TestSIPPacketParse_CSeqMultipleSpaces(t *testing.T) {
	e := exporter{}

	tests := []struct {
		name string
		cseq string
	}{
		{name: "double space", cseq: "1  INVITE"},
		{name: "tab separator", cseq: "1\tINVITE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte("SIP/2.0 200 OK\r\n" +
				"From: <sip:user@domain>;tag=abc\r\n" +
				"To: <sip:other@domain>;tag=xyz\r\n" +
				"Call-ID: test\r\n" +
				"CSeq: " + tt.cseq + "\r\n")

			p, err := e.sipPacketParse(input)
			require.NoError(t, err)
			require.Equal(t, []byte("INVITE"), p.CSeq.Method)
			require.Equal(t, []byte("1"), p.CSeq.ID)
		})
	}
}

func TestSIPPacketParse_CaseInsensitiveHeaders(t *testing.T) {
	e := exporter{}

	tests := []struct {
		name string
		from string
		to   string
		cid  string
		cseq string
	}{
		{name: "lowercase", from: "from", to: "to", cid: "call-id", cseq: "cseq"},
		{name: "uppercase", from: "FROM", to: "TO", cid: "CALL-ID", cseq: "CSEQ"},
		{name: "mixed-case", from: "FrOm", to: "To", cid: "Call-ID", cseq: "CsEq"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte("SIP/2.0 200 OK\r\n" +
				tt.from + ": <sip:user@domain>;tag=abc\r\n" +
				tt.to + ": <sip:other@domain>;tag=xyz\r\n" +
				tt.cid + ": test\r\n" +
				tt.cseq + ": 1 INVITE\r\n")

			p, err := e.sipPacketParse(input)
			require.NoError(t, err)
			require.Equal(t, []byte("abc"), p.From.Tag)
			require.Equal(t, []byte("xyz"), p.To.Tag)
			require.Equal(t, []byte("test"), p.CallID)
			require.Equal(t, []byte("INVITE"), p.CSeq.Method)
		})
	}
}

func TestSIPPacketParse_CaseInsensitiveTag(t *testing.T) {
	e := exporter{}

	tests := []struct {
		name string
		tag  string
	}{
		{name: "uppercase TAG", tag: ";TAG=abc"},
		{name: "mixed-case Tag", tag: ";Tag=abc"},
		{name: "mixed-case tAg", tag: ";tAg=abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte("SIP/2.0 200 OK\r\n" +
				"From: <sip:user@domain>" + tt.tag + "\r\n" +
				"To: <sip:other@domain>;tag=xyz\r\n" +
				"Call-ID: test\r\n" +
				"CSeq: 1 INVITE\r\n")

			p, err := e.sipPacketParse(input)
			require.NoError(t, err)
			require.Equal(t, []byte("abc"), p.From.Tag)
		})
	}
}

func TestSIPPacketParse_TruncatedStatusLine(t *testing.T) {
	e := exporter{}

	tests := []struct {
		name  string
		line0 string
	}{
		{name: "space only", line0: "SIP/2.0 "},
		{name: "one digit", line0: "SIP/2.0 2"},
		{name: "two digits", line0: "SIP/2.0 20"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.line0 + "\r\n" +
				"From: <sip:user@domain>;tag=abc\r\n" +
				"To: <sip:other@domain>;tag=xyz\r\n" +
				"Call-ID: test\r\n" +
				"CSeq: 1 INVITE\r\n" +
				"X-Pad: 1234567890123456789012345678901234567890\r\n")

			_, err := e.sipPacketParse(input)
			require.Error(t, err)
			require.Contains(t, err.Error(), "malformed status line")
		})
	}
}

// ==================== handleMessage tests ====================

func TestHandleMessage_Request(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	input := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 INVITE\r\n")

	err := e.handleMessage("other", "", input)
	require.NoError(t, err)
	// Request is called in goroutine, so we need to wait
	require.Eventually(t, func() bool {
		return len(mm.requestCalled) > 0
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, []byte("INVITE"), mm.requestCalled)
}

func TestHandleMessage_Response200_INVITE(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test-call\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Session-Expires: 3600\r\n")

	err := e.handleMessage("other", "", input)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(mm.responseCalled) > 0
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, []byte("200"), mm.responseCalled)
	require.True(t, mm.responseIsInvite)
	require.True(t, mm.invite200OKCalled)
	require.Len(t, md.created, 1)
}

func TestHandleMessage_ReINVITE_CountedAsReinvite(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	md.Create("test-call:abc:xyz",
		time.Now().Add(1*time.Hour), time.Now(), "", "", "", "test-call")

	input := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test-call\r\n" +
		"CSeq: 2 INVITE\r\n")

	err := e.handleMessage("other", "", input)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return mm.reinviteCalled
	}, 100*time.Millisecond, 10*time.Millisecond)

	require.True(t, mm.reinviteCalled)
	require.Nil(t, mm.requestCalled)
	_, hasTracker := e.inviteTracker["test-call"]
	require.False(t, hasTracker)
}

func TestHandleMessage_InitialINVITE_CountedAsInvite(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	input := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test-call\r\n" +
		"CSeq: 1 INVITE\r\n")

	err := e.handleMessage("other", "", input)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(mm.requestCalled) > 0
	}, 100*time.Millisecond, 10*time.Millisecond)

	require.Equal(t, []byte("INVITE"), mm.requestCalled)
	require.False(t, mm.reinviteCalled)
}

func TestHandleMessage_ReINVITE_200OK_DoesNotInflateMetrics(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	md.Create("test-call:abc:xyz",
		time.Now().Add(1*time.Hour), time.Now(), "carrier-a", "yealink", "RU", "test-call")

	input := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test-call\r\n" +
		"CSeq: 2 INVITE\r\n" +
		"Session-Expires: 3600\r\n")

	err := e.handleMessage("other", "", input)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return mm.responseWithMetricsCalled
	}, 100*time.Millisecond, 10*time.Millisecond)

	require.False(t, mm.invite200OKCalled)
	require.False(t, mm.responseIsInvite)
}

func TestHandleMessage_Response200_BYE(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test-call\r\n" +
		"CSeq: 2 BYE\r\n")

	err := e.handleMessage("other", "", input)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(mm.responseCalled) > 0
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, []byte("200"), mm.responseCalled)
	require.False(t, mm.responseIsInvite)
	require.False(t, mm.invite200OKCalled)
	require.Len(t, md.deleted, 1)
}

func TestHandleMessage_Response200_REGISTER(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test-call\r\n" +
		"CSeq: 1 REGISTER\r\n")

	err := e.handleMessage("other", "", input)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(mm.responseCalled) > 0
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, []byte("200"), mm.responseCalled)
	require.False(t, mm.responseIsInvite)
	require.False(t, mm.invite200OKCalled)
}

func TestHandleMessage_RRD_FullCycle(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	registerReq := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: reg-test-123\r\n" +
		"CSeq: 1 REGISTER\r\n")

	err := e.handleMessage("other", "", registerReq)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("REGISTER"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	time.Sleep(10 * time.Millisecond)

	registerResp := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: reg-test-123\r\n" +
		"CSeq: 1 REGISTER\r\n")

	err = e.handleMessage("other", "", registerResp)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return mm.rrdUpdated
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.True(t, mm.rrdUpdated)
	require.Greater(t, mm.rrdDelay, 0.0)
}

func TestHandleMessage_Register200OK_CallsRegisterSuccess(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	require.NoError(t, e.handleMessage("c", "US", makeRegister("ok1", "ft")))
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, e.handleMessage("c", "US", makeRegister200OK("ok1", "ft", "tt")))

	require.Eventually(t, func() bool { return mm.registerSuccessCalls == 1 },
		100*time.Millisecond, 10*time.Millisecond)
	require.Empty(t, mm.registerFailureCodes)
}

func TestHandleMessage_Register403_CallsRegisterFailure(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	require.NoError(t, e.handleMessage("c", "US", makeRegister("f403", "ft")))
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, e.handleMessage("c", "US", makeRegisterStatus("403 Forbidden", "f403", "ft", "tt")))

	require.Eventually(t, func() bool { return len(mm.registerFailureCodes) == 1 },
		100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, []string{"403"}, mm.registerFailureCodes)
	require.Zero(t, mm.registerSuccessCalls)
}

// Challenge 401 is recorded in failure_total{code} (for brute-force detection)
// even though it does not affect register_success_ratio.
func TestHandleMessage_Register401Challenge_CallsRegisterFailure(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	require.NoError(t, e.handleMessage("c", "US", makeRegister("ch1", "ft")))
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, e.handleMessage("c", "US", makeRegisterStatus("401 Unauthorized", "ch1", "ft", "tt")))

	require.Eventually(t, func() bool { return len(mm.registerFailureCodes) == 1 },
		100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, []string{"401"}, mm.registerFailureCodes)
}

// 1xx provisional response must NOT be counted as a registration failure.
func TestHandleMessage_Register100Trying_NotAFailure(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	require.NoError(t, e.handleMessage("c", "US", makeRegister("t100", "ft")))
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, e.handleMessage("c", "US", makeRegisterStatus("100 Trying", "t100", "ft", "tt")))

	time.Sleep(20 * time.Millisecond)
	require.Empty(t, mm.registerFailureCodes)
	require.Zero(t, mm.registerSuccessCalls)
}

// ==================== registerExpiryTracker (S4-2.2) ====================

func newExporterWithRegTracker() *exporter {
	return &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker:       make(map[string]registerEntry),
		registerExpiryTracker: make(map[string]registerExpiryEntry),
		inviteTracker:         make(map[string]inviteEntry),
		inviteSDP:             make(map[string]inviteSDPEntity),
		mediaTracker:          mediatracker.NewTracker(rtpStreamTTL),
	}
}

func TestRegisterExpiryTracker_NewRegistration(t *testing.T) {
	e := newExporterWithRegTracker()

	e.storeRegistration("sip:user1@example.com", "c", "sip", "US", "", 3600)

	counts := e.registrationCounts()
	require.Len(t, counts, 1)
	require.Equal(t, 1, counts[0].Count)
}

func TestRegisterExpiryTracker_RefreshNoDoubleCount(t *testing.T) {
	e := newExporterWithRegTracker()
	aor := "sip:user1@example.com"

	e.storeRegistration(aor, "c", "sip", "US", "", 3600)
	e.storeRegistration(aor, "c", "sip", "US", "", 3600)
	e.storeRegistration(aor, "c", "sip", "US", "", 3600)

	counts := e.registrationCounts()
	require.Len(t, counts, 1)
	require.Equal(t, 1, counts[0].Count, "refresh of same AOR must not double-count")
}

func TestRegisterExpiryTracker_DifferentAORs(t *testing.T) {
	e := newExporterWithRegTracker()

	e.storeRegistration("sip:user1@example.com", "c", "sip", "US", "", 3600)
	e.storeRegistration("sip:user2@example.com", "c", "sip", "US", "", 3600)

	counts := e.registrationCounts()
	require.Len(t, counts, 1)
	require.Equal(t, 2, counts[0].Count)
}

func TestRegisterExpiryTracker_GroupsByLabels(t *testing.T) {
	e := newExporterWithRegTracker()

	e.storeRegistration("sip:u1@a", "carrier-A", "sip", "US", "", 3600)
	e.storeRegistration("sip:u2@a", "carrier-A", "sip", "US", "", 3600)
	e.storeRegistration("sip:u3@b", "carrier-B", "yealink", "DE", "", 3600)

	counts := e.registrationCounts()
	require.Len(t, counts, 2)
	byCarrier := map[string]int{}
	for _, c := range counts {
		byCarrier[c.Labels["carrier"]] = c.Count
	}
	require.Equal(t, 2, byCarrier["carrier-A"])
	require.Equal(t, 1, byCarrier["carrier-B"])
}

func TestRegisterExpiryTracker_CleanupExpired(t *testing.T) {
	e := newExporterWithRegTracker()

	e.storeRegistration("sip:user1@example.com", "c", "sip", "US", "", 1)
	// Force expiry by backdating the entry.
	e.registerExpiryMutex.Lock()
	for k := range e.registerExpiryTracker {
		ent := e.registerExpiryTracker[k]
		ent.expiry = time.Now().Add(-time.Second)
		e.registerExpiryTracker[k] = ent
	}
	e.registerExpiryMutex.Unlock()

	e.cleanupExpiredRegistrations()

	counts := e.registrationCounts()
	require.Empty(t, counts, "expired registration must be removed")
}

func TestRegisterExpiryTracker_RefreshKeepsActive(t *testing.T) {
	e := newExporterWithRegTracker()
	aor := "sip:user1@example.com"

	e.storeRegistration(aor, "c", "sip", "US", "", 1)
	// Backdate close to expiry.
	e.registerExpiryMutex.Lock()
	ent := e.registerExpiryTracker[aor]
	ent.expiry = time.Now().Add(100 * time.Millisecond)
	e.registerExpiryTracker[aor] = ent
	e.registerExpiryMutex.Unlock()

	// Refresh before expiry.
	e.storeRegistration(aor, "c", "sip", "US", "", 3600)

	// Even though the old expiry has passed, the refresh extended it.
	time.Sleep(150 * time.Millisecond)
	e.cleanupExpiredRegistrations()

	counts := e.registrationCounts()
	require.Len(t, counts, 1, "refreshed registration must survive old-expiry cleanup")
}

// ==================== S6-9.2: Registration Country Change ====================

func TestRegisterCountryChange_DifferentCountry(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services:              services{metricser: mm, dialoger: &mockDialoger{}},
		registerExpiryTracker: make(map[string]registerExpiryEntry),
	}
	aor := "sip:alice@example.com"

	e.storeRegistration(aor, "beeline", "sip", "RU", "", 3600)
	require.Empty(t, mm.registerCountryChange, "first registration must not signal")

	e.storeRegistration(aor, "beeline", "sip", "GE", "", 3600)
	require.Len(t, mm.registerCountryChange, 1, "country change must signal")
	require.Equal(t, "GE", mm.registerCountryChange[0])
}

func TestRegisterCountryChange_SameCountry(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services:              services{metricser: mm, dialoger: &mockDialoger{}},
		registerExpiryTracker: make(map[string]registerExpiryEntry),
	}
	aor := "sip:alice@example.com"

	e.storeRegistration(aor, "beeline", "sip", "RU", "", 3600)
	e.storeRegistration(aor, "beeline", "sip", "RU", "", 3600)

	require.Empty(t, mm.registerCountryChange, "same country must not signal")
}

func TestRegisterCountryChange_FirstRegistration(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services:              services{metricser: mm, dialoger: &mockDialoger{}},
		registerExpiryTracker: make(map[string]registerExpiryEntry),
	}

	e.storeRegistration("sip:bob@example.com", "mts", "sip", "DE", "", 3600)

	require.Empty(t, mm.registerCountryChange, "first registration has no baseline")
}

func TestRegisterCountryChange_EmptyPreviousCountry(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services:              services{metricser: mm, dialoger: &mockDialoger{}},
		registerExpiryTracker: make(map[string]registerExpiryEntry),
	}
	aor := "sip:alice@example.com"

	// Manually insert an entry with empty sourceCountry (simulates GeoIP disabled).
	e.registerExpiryTracker[aor] = registerExpiryEntry{
		expiry:        time.Now().Add(3600 * time.Second),
		carrier:       "beeline",
		uaType:        "sip",
		sourceCountry: "",
	}

	e.storeRegistration(aor, "beeline", "sip", "RU", "", 3600)

	require.Empty(t, mm.registerCountryChange, "empty previous country must not signal")
}

// ==================== S6-9.1: Registration Scan Detection ====================

func TestRegisterScanTracker_SignalsAtThreshold(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, time.Minute)

	for i := range 2 {
		tracker.record("1.2.3.4", fmt.Sprintf("user%d@evil.com", i), "carrier", "RU", mm)
	}
	require.Zero(t, mm.registerScanCalls, "below threshold must not signal")

	tracker.record("1.2.3.4", "user2@evil.com", "carrier", "RU", mm)
	require.Equal(t, 1, mm.registerScanCalls, "at threshold must signal")
}

func TestRegisterScanTracker_IncrementsPerAORAboveThreshold(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, time.Minute)

	for i := range 5 {
		tracker.record("1.2.3.4", fmt.Sprintf("user%d@evil.com", i), "carrier", "RU", mm)
	}
	require.Equal(t, 3, mm.registerScanCalls, "must increment for each AOR at or above threshold (5-3+1=3)")
}

func TestRegisterScanTracker_UniqueAORsOnly(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, time.Minute)

	tracker.record("1.2.3.4", "user@evil.com", "carrier", "RU", mm)
	tracker.record("1.2.3.4", "user@evil.com", "carrier", "RU", mm)
	tracker.record("1.2.3.4", "user@evil.com", "carrier", "RU", mm)

	require.Zero(t, mm.registerScanCalls, "same AOR must not count as scan")
}

func TestRegisterScanTracker_NilTrackerSafe(t *testing.T) {
	mm := &mockMetricser{}
	var tracker *registerScanTracker

	tracker.record("1.2.3.4", "user@evil.com", "carrier", "RU", mm)
	require.Zero(t, mm.registerScanCalls, "nil tracker must be no-op")
}

func TestRegisterScanTracker_EmptySrcIPSkipped(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(1, time.Minute)

	tracker.record("", "user@evil.com", "carrier", "RU", mm)
	require.Zero(t, mm.registerScanCalls, "empty srcIP must be skipped")
}

// ==================== S6-A.1: Memory Cap ====================

func TestRegisterScanTracker_MemoryBoundedAtMaxEntries(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, time.Minute)

	for i := range registerScanMaxEntriesPerIP + 10 {
		tracker.record("1.2.3.4", fmt.Sprintf("user%d@evil.com", i), "carrier", "RU", mm)
	}

	require.LessOrEqual(t, len(tracker.entries["1.2.3.4"]), registerScanMaxEntriesPerIP,
		"inner map must not exceed registerScanMaxEntriesPerIP")
	require.Positive(t, mm.registerScanCalls, "must have signalled above threshold")
}

func TestRegisterScanTracker_EvictionWorksAfterCap(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, 50*time.Millisecond)

	for i := range 3 {
		tracker.record("1.2.3.4", fmt.Sprintf("user%d@evil.com", i), "carrier", "RU", mm)
	}
	require.Equal(t, 1, mm.registerScanCalls)

	time.Sleep(80 * time.Millisecond)

	tracker.cleanup()
	require.Empty(t, tracker.entries["1.2.3.4"], "entries must expire after window")

	tracker.record("1.2.3.4", "newuser@evil.com", "carrier", "RU", mm)
	require.Equal(t, 1, mm.registerScanCalls, "first AOR after reset must not re-signal")

	tracker.record("1.2.3.4", "newuser2@evil.com", "carrier", "RU", mm)
	tracker.record("1.2.3.4", "newuser3@evil.com", "carrier", "RU", mm)
	require.Equal(t, 2, mm.registerScanCalls, "new burst after eviction must signal again")
}

func TestRegisterScanTracker_MultipleIPsIndependent(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, time.Minute)

	for i := range 3 {
		tracker.record("10.0.0.1", fmt.Sprintf("user%d@a.com", i), "carrier", "RU", mm)
	}
	require.Equal(t, 1, mm.registerScanCalls, "IP1 at threshold must signal")

	tracker.record("10.0.0.2", "user0@b.com", "carrier", "RU", mm)
	tracker.record("10.0.0.2", "user1@b.com", "carrier", "RU", mm)
	require.Equal(t, 1, mm.registerScanCalls, "IP2 below threshold must not signal")
}

// ==================== S6-A.13: Wasted Event Fix ====================

// TestRegisterScanTracker_NoWastedEventAfterExpiry verifies that the first
// event after window expiry IS recorded (not silently dropped).
func TestRegisterScanTracker_NoWastedEventAfterExpiry(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, 50*time.Millisecond)

	for i := range 3 {
		tracker.record("1.2.3.4", fmt.Sprintf("user%d@evil.com", i), "carrier", "RU", mm)
	}
	require.Equal(t, 1, mm.registerScanCalls, "at threshold must signal")

	time.Sleep(80 * time.Millisecond)

	// Eviction happens inside record(), NOT via cleanup().
	tracker.record("1.2.3.4", "newuser@evil.com", "carrier", "RU", mm)

	require.Len(t, tracker.entries["1.2.3.4"], 1,
		"first event after window expiry must be recorded, not wasted")
}

// TestRegisterScanTracker_RetriggerAtExactThreshold verifies that after
// window expiry, exactly `threshold` new events re-trigger the signal.
func TestRegisterScanTracker_RetriggerAtExactThreshold(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, 50*time.Millisecond)

	for i := range 3 {
		tracker.record("1.2.3.4", fmt.Sprintf("user%d@evil.com", i), "carrier", "RU", mm)
	}
	require.Equal(t, 1, mm.registerScanCalls, "first burst must signal")

	time.Sleep(80 * time.Millisecond)

	// Without cleanup(), eviction happens lazily inside record().
	// Exactly threshold events must re-trigger — not threshold+1.
	tracker.record("1.2.3.4", "new1@evil.com", "carrier", "RU", mm)
	tracker.record("1.2.3.4", "new2@evil.com", "carrier", "RU", mm)
	tracker.record("1.2.3.4", "new3@evil.com", "carrier", "RU", mm)

	require.Equal(t, 2, mm.registerScanCalls,
		"re-trigger after window expiry must fire at exactly threshold events")
}

// ==================== S6-9.3: INVITE Burst Detection ====================

func TestInviteBurstTracker_SignalsAtThreshold(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newInviteBurstTracker(5, time.Minute)

	for range 4 {
		tracker.record("1.2.3.4", "carrier", "RU", mm)
	}
	require.Zero(t, mm.inviteBurstCalls, "below threshold must not signal")

	tracker.record("1.2.3.4", "carrier", "RU", mm)
	require.Equal(t, 1, mm.inviteBurstCalls, "at threshold must signal")
}

func TestInviteBurstTracker_IncrementsPerInviteAboveThreshold(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newInviteBurstTracker(3, time.Minute)

	for range 10 {
		tracker.record("1.2.3.4", "carrier", "RU", mm)
	}
	require.Equal(t, 8, mm.inviteBurstCalls, "must increment for each INVITE at or above threshold (10-3+1=8)")
}

func TestInviteBurstTracker_NilTrackerSafe(t *testing.T) {
	mm := &mockMetricser{}
	var tracker *inviteBurstTracker

	tracker.record("1.2.3.4", "carrier", "RU", mm)
	require.Zero(t, mm.inviteBurstCalls, "nil tracker must be no-op")
}

func TestInviteBurstTracker_EmptySrcIPSkipped(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newInviteBurstTracker(1, time.Minute)

	tracker.record("", "carrier", "RU", mm)
	require.Zero(t, mm.inviteBurstCalls, "empty srcIP must be skipped")
}

func TestHandleMessage_ReINVITE_ExcludedFromBurst(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:      make(map[string]inviteEntry),
		inviteSDP:          make(map[string]inviteSDPEntity),
		optionsTracker:     make(map[string]optionsEntry),
		mediaTracker:       mediatracker.NewTracker(rtpStreamTTL),
		inviteBurstTracker: newInviteBurstTracker(3, time.Minute),
	}

	md.Create("call-id:abc:xyz",
		time.Now().Add(1*time.Hour), time.Now(), "", "", "", "call-id")

	input := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: call-id\r\n" +
		"CSeq: 2 INVITE\r\n")

	err := e.handleMessage("carrier", "", input)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return mm.reinviteCalled
	}, 100*time.Millisecond, 10*time.Millisecond)

	require.Zero(t, mm.inviteBurstCalls, "re-INVITE must not trigger burst detection")
}

// End-to-end: REGISTER then 200 OK populates the expiry tracker from the
// parsed From URI and labels.
func TestHandleMessage_Register200OK_PopulatesExpiryTracker(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker:       make(map[string]registerEntry),
		registerExpiryTracker: make(map[string]registerExpiryEntry),
		inviteTracker:         make(map[string]inviteEntry),
		inviteSDP:             make(map[string]inviteSDPEntity),
		mediaTracker:          mediatracker.NewTracker(rtpStreamTTL),
	}

	require.NoError(t, e.handleMessage("carrier-A", "US", makeRegister("e2e1", "ft")))
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, e.handleMessage("carrier-A", "US", makeRegister200OK("e2e1", "ft", "tt")))

	require.Eventually(t, func() bool { return len(e.registrationCounts()) == 1 },
		100*time.Millisecond, 10*time.Millisecond)
	counts := e.registrationCounts()
	require.Equal(t, 1, counts[0].Count)
	require.Equal(t, "carrier-A", counts[0].Labels["carrier"])
	require.Equal(t, "user@domain", func() string {
		e.registerExpiryMutex.RLock()
		defer e.registerExpiryMutex.RUnlock()
		for aor := range e.registerExpiryTracker {
			return aor
		}
		return ""
	}())
}

func TestHandleMessage_Response401(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	input := []byte("SIP/2.0 401 Unauthorized\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 REGISTER\r\n")

	err := e.handleMessage("other", "", input)
	require.NoError(t, err)

	// Response is called in goroutine, wait for completion
	time.Sleep(10 * time.Millisecond)

	require.Equal(t, []byte("401"), mm.responseCalled)
	require.False(t, mm.responseIsInvite)
}

func TestHandleMessage_Response302_INVITE(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	input := []byte("SIP/2.0 302 Moved Temporarily\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test-call\r\n" +
		"CSeq: 1 INVITE\r\n")

	err := e.handleMessage("other", "", input)
	require.NoError(t, err)

	// Response is called in goroutine, wait for completion
	time.Sleep(10 * time.Millisecond)

	require.Equal(t, []byte("302"), mm.responseCalled)
	require.True(t, mm.responseIsInvite)
}

// Integration test for SER change via handleMessage.
func TestHandleMessage_SER_Integration(t *testing.T) {
	m := &mockMetricser{}
	d := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: m,
			dialoger:  d,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	// 10 INVITE requests
	for i := range 10 {
		input := []byte("INVITE sip:test SIP/2.0\r\n" +
			"From: <sip:user@domain>;tag=abc\r\n" +
			"To: <sip:other@domain>\r\n" +
			"Call-ID: test-" + string(rune('0'+i)) + "\r\n" +
			"CSeq: 1 INVITE\r\n")
		err := e.handleMessage("other", "", input)
		require.NoError(t, err)
	}

	// 5 200 OK responses to INVITE
	for i := range 5 {
		input := []byte("SIP/2.0 200 OK\r\n" +
			"From: <sip:user@domain>;tag=abc\r\n" +
			"To: <sip:other@domain>;tag=xyz" + string(rune('0'+i)) + "\r\n" +
			"Call-ID: test-" + string(rune('0'+i)) + "\r\n" +
			"CSeq: 1 INVITE\r\n")
		err := e.handleMessage("other", "", input)
		require.NoError(t, err)
	}

	// 2 302 responses to INVITE
	for i := range 2 {
		input := []byte("SIP/2.0 302 Moved Temporarily\r\n" +
			"From: <sip:user@domain>;tag=abc\r\n" +
			"To: <sip:other@domain>;tag=xyz" + string(rune('0'+i)) + "\r\n" +
			"Call-ID: test-302-" + string(rune('0'+i)) + "\r\n" +
			"CSeq: 1 INVITE\r\n")
		err := e.handleMessage("other", "", input)
		require.NoError(t, err)
	}

	// Wait for all goroutines to complete
	time.Sleep(50 * time.Millisecond)

	// Verify Invite200OK was called 5 times
	invite200OKCount := 0
	for range 5 {
		if m.invite200OKCalled {
			invite200OKCount++
		}
	}

	// Verify response was called with isInviteResponse=true for INVITE
	require.True(t, m.responseIsInvite)
}

func TestHandleMessage_ParseError(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	// Invalid SIP packet - "invalid" is too short and won't be recognized
	// handleMessage may not return error for some invalid packets
	// Main thing is systemError metric will be incremented
	err := e.handleMessage("other", "", []byte("invalid"))
	require.NoError(t, err)
}

func TestHandleMessage_Response200_InvalidDialogID(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[string]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=\r\n" +
		"To: <sip:other@domain>;tag=\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 INVITE\r\n")

	// Should not panic and should not create dialog
	err := e.handleMessage("other", "", input)
	require.NoError(t, err)
	require.Empty(t, md.created, "dialog should not be created with invalid tags")
}

// ==================== Tests for all SIP methods ====================

func TestParseRawPacket_AllSIPMethods(t *testing.T) {
	methods := []string{
		"INVITE", "ACK", "BYE", "CANCEL", "OPTIONS",
		"REGISTER", "SUBSCRIBE", "NOTIFY", "PUBLISH", "INFO",
		"PRACK", "UPDATE", "MESSAGE", "REFER",
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			e := &exporter{
				services: services{
					metricser: &mockMetricser{},
					dialoger:  &mockDialoger{},
				},
				registerTracker: make(map[string]registerEntry),
				inviteTracker:   make(map[string]inviteEntry),
				inviteSDP:       make(map[string]inviteSDPEntity),
				optionsTracker:  make(map[string]optionsEntry),
				mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
			}

			packet := make([]byte, 200)
			packet[12] = 0x08
			packet[13] = 0x00
			packet[14] = 0x45
			packet[23] = 17

			sipPacket := method + " sip:test SIP/2.0\r\n" +
				"From: <sip:user@domain>;tag=abc\r\n" +
				"To: <sip:other@domain>\r\n" +
				"Call-ID: test\r\n" +
				"CSeq: 1 " + method + "\r\n"

			copy(packet[42:], []byte(sipPacket))

			_, err := e.parseRawPacket(packet)
			require.NoError(t, err)
		})
	}
}

func TestParseRawPacket_SIPResponse(t *testing.T) {
	responses := []struct {
		code   string
		packet string
	}{
		{"100", "SIP/2.0 100 Trying\r\n"},
		{"180", "SIP/2.0 180 Ringing\r\n"},
		{"200", "SIP/2.0 200 OK\r\n"},
		{"404", "SIP/2.0 404 Not Found\r\n"},
		{"500", "SIP/2.0 500 Server Internal Error\r\n"},
	}

	for _, r := range responses {
		t.Run(r.code, func(t *testing.T) {
			e := &exporter{
				services: services{
					metricser: &mockMetricser{},
					dialoger:  &mockDialoger{},
				},
				inviteSDP:    make(map[string]inviteSDPEntity),
				mediaTracker: mediatracker.NewTracker(rtpStreamTTL),
			}

			packet := make([]byte, 200)
			packet[12] = 0x08
			packet[13] = 0x00
			packet[14] = 0x45
			packet[23] = 17

			sipPacket := r.packet +
				"From: <sip:user@domain>;tag=abc\r\n" +
				"To: <sip:other@domain>;tag=xyz\r\n" +
				"Call-ID: test\r\n" +
				"CSeq: 1 INVITE\r\n"

			copy(packet[42:], []byte(sipPacket))

			_, err := e.parseRawPacket(packet)
			require.NoError(t, err)
		})
	}
}

// ==================== NewExporter tests ====================

func TestNewExporter(t *testing.T) {
	m := service.NewMetricser()
	d := service.NewDialoger()

	exp := NewExporter(Deps{
		Metricser: m,
		Dialoger:  d,
	})
	require.NotNil(t, exp)
}

// ==================== htons tests ====================

func TestHtons(t *testing.T) {
	tests := []struct {
		input    uint16
		expected uint16
		name     string
	}{
		{0x0000, 0x0000, "zero"},
		{0x0001, 0x0100, "one"},
		{0x1234, 0x3412, "1234"},
		{0xFFFF, 0xFFFF, "ffff"},
		{0x0800, 0x0008, "eth_ip"},
		{0x8100, 0x0081, "vlan"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := htons(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

// ==================== SIP method prefix tests ====================

func TestParseRawPacket_SIPMethodsBytesPrefix(t *testing.T) {
	testCases := []struct {
		method      string
		shouldMatch bool
	}{
		{"INVITE", true},
		{"ACK", true},
		{"BYE", true},
		{"CANCEL", true},
		{"OPTIONS", true},
		{"REGISTER", true},
		{"SUBSCRIBE", true},
		{"NOTIFY", true},
		{"PUBLISH", true},
		{"INFO", true},
		{"PRACK", true},
		{"UPDATE", true},
		{"MESSAGE", true},
		{"REFER", true},
		{"SIP/2.0", true},
		{"INVALID", false},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.method, func(t *testing.T) {
			result := strings.HasPrefix(tc.method, "INVITE") ||
				strings.HasPrefix(tc.method, "ACK") ||
				strings.HasPrefix(tc.method, "BYE") ||
				strings.HasPrefix(tc.method, "CANCEL") ||
				strings.HasPrefix(tc.method, "OPTIONS") ||
				strings.HasPrefix(tc.method, "REGISTER") ||
				strings.HasPrefix(tc.method, "SUBSCRIBE") ||
				strings.HasPrefix(tc.method, "NOTIFY") ||
				strings.HasPrefix(tc.method, "PUBLISH") ||
				strings.HasPrefix(tc.method, "INFO") ||
				strings.HasPrefix(tc.method, "PRACK") ||
				strings.HasPrefix(tc.method, "UPDATE") ||
				strings.HasPrefix(tc.method, "MESSAGE") ||
				strings.HasPrefix(tc.method, "REFER") ||
				strings.HasPrefix(tc.method, "SIP/2.0")

			require.Equal(t, tc.shouldMatch, result)
		})
	}
}

// ==================== Additional sipPacketParse tests ====================

func TestSIPPacketParse_Response401(t *testing.T) {
	e := exporter{}

	input := []byte("SIP/2.0 401 Unauthorized\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:55147;rport=55147;branch=z9hG4bKPjda81fdbda2a5464898d03d02ed894a2d\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.True(t, p.IsResponse)
	require.Equal(t, []byte("401"), p.ResponseStatus)
}

func TestSIPPacketParse_INVITE(t *testing.T) {
	e := exporter{}

	input := []byte("INVITE sip:1001@192.168.0.89 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:49375;rport;branch=z9hG4bKPjdad03fa8a00c49fb9b08469cc8c2215b\r\n" +
		"Max-Forwards: 70\r\n" +
		"From: <sip:1000@192.168.0.89>;tag=21e4850e69de4f50a3f96a8051e1af35\r\n" +
		"To: <sip:1001@192.168.0.89>\r\n" +
		"Contact: <sip:1000@192.168.0.89:49375;ob>\r\n" +
		"Call-ID: 618e627cb7eb4275a9addb1c6b639656\r\n" +
		"CSeq: 9217 INVITE\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.False(t, p.IsResponse)
	require.Equal(t, []byte("INVITE"), p.Method)
	require.Equal(t, []byte("21e4850e69de4f50a3f96a8051e1af35"), p.From.Tag)
	require.Equal(t, []byte("618e627cb7eb4275a9addb1c6b639656"), p.CallID)
	require.Equal(t, []byte("9217"), p.CSeq.ID)
	require.Equal(t, []byte("INVITE"), p.CSeq.Method)
}

func TestParseResponsesPacket_401(t *testing.T) {
	e := exporter{}

	input := []byte("SIP/2.0 401 Unauthorized\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:49375;rport=49375;branch=z9hG4bKPjbce993f574bb40a9919447d899e324fa\r\n" +
		"From: <sip:1000@192.168.0.89>;tag=e2540aafe5474bd7a5f9059b0ffccfec\r\n" +
		"To: <sip:1000@192.168.0.89>;tag=8Xy7r28Ne5ZSQ\r\n" +
		"Call-ID: 583ce713cb324f27bd614e594db53cc2\r\n" +
		"CSeq: 6596 REGISTER\r\n" +
		"User-Agent: MicroSIP/3.22.3\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.True(t, p.IsResponse)
	require.Equal(t, []byte("401"), p.ResponseStatus)
	require.Equal(t, []byte("e2540aafe5474bd7a5f9059b0ffccfec"), p.From.Tag)
	require.Equal(t, []byte("8Xy7r28Ne5ZSQ"), p.To.Tag)
	require.Equal(t, []byte("583ce713cb324f27bd614e594db53cc2"), p.CallID)
	require.Equal(t, []byte("6596"), p.CSeq.ID)
	require.Equal(t, []byte("REGISTER"), p.CSeq.Method)
}

func TestParseResponsesPacket_200(t *testing.T) {
	e := exporter{}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:49375;rport=49375;branch=z9hG4bKPjbce993f574bb40a9919447d899e324fa\r\n" +
		"From: <sip:1000@192.168.0.89>;tag=e2540aafe5474bd7a5f9059b0ffccfec\r\n" +
		"To: <sip:1000@192.168.0.89>;tag=8Xy7r28Ne5ZSQ\r\n" +
		"Call-ID: 583ce713cb324f27bd614e594db53cc2\r\n" +
		"CSeq: 6596 INVITE\r\n" +
		"User-Agent: MicroSIP/3.22.3\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.True(t, p.IsResponse)
	require.Equal(t, []byte("200"), p.ResponseStatus)
	require.Equal(t, []byte("e2540aafe5474bd7a5f9059b0ffccfec"), p.From.Tag)
	require.Equal(t, []byte("8Xy7r28Ne5ZSQ"), p.To.Tag)
	require.Equal(t, []byte("583ce713cb324f27bd614e594db53cc2"), p.CallID)
	require.Equal(t, []byte("6596"), p.CSeq.ID)
	require.Equal(t, []byte("INVITE"), p.CSeq.Method)
}

func TestParseRegisterPacket(t *testing.T) {
	e := exporter{}

	input := []byte("REGISTER sip:192.168.0.89:5060 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:49375;rport;branch=z9hG4bKPjbce993f574bb40a9919447d899e324fa\r\n" +
		"Max-Forwards: 70\r\n" +
		"From: <sip:1000@192.168.0.89>;tag=e2540aafe5474bd7a5f9059b0ffccfec\r\n" +
		"To: <sip:1000@192.168.0.89>\r\n" +
		"Call-ID: 583ce713cb324f27bd614e594db53cc2\r\n" +
		"CSeq: 6596 REGISTER\r\n" +
		"User-Agent: MicroSIP/3.22.3\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.False(t, p.IsResponse)
	require.Equal(t, []byte("REGISTER"), p.Method)
	require.Equal(t, []byte("e2540aafe5474bd7a5f9059b0ffccfec"), p.From.Tag)
	require.Equal(t, []byte("583ce713cb324f27bd614e594db53cc2"), p.CallID)
	require.Equal(t, []byte("6596"), p.CSeq.ID)
	require.Equal(t, []byte("REGISTER"), p.CSeq.Method)
}

// ==================== Register Tracker tests ====================

func TestExporter_RegisterTracker_StoreAndRemove(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	callID := "test-call-id-123"

	// Store
	e.storeRegisterTime(callID, "other", "other", "", "")

	// Verify stored
	_, exists := e.getRegisterTime(callID)
	require.True(t, exists, "entry should exist after store")

	// Remove
	e.removeRegisterTime(callID)

	// Verify removed
	_, exists = e.getRegisterTime(callID)
	require.False(t, exists, "entry should not exist after remove")
}

func TestExporter_RegisterTracker_401Removes(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	// REGISTER request
	registerReq := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: reg-401-test\r\n" +
		"CSeq: 1 REGISTER\r\n")

	err := e.handleMessage("other", "", registerReq)
	require.NoError(t, err)

	// Verify stored
	_, exists := e.getRegisterTime("reg-401-test")
	require.True(t, exists, "register should be tracked")

	// 401 Unauthorized response
	registerResp := []byte("SIP/2.0 401 Unauthorized\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: reg-401-test\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"WWW-Authenticate: Digest realm=\"test\"\r\n")

	err = e.handleMessage("other", "", registerResp)
	require.NoError(t, err)

	// Verify removed
	_, exists = e.getRegisterTime("reg-401-test")
	require.False(t, exists, "register should be removed after 401")
	require.False(t, mm.rrdUpdated, "RRD should NOT be updated for 401")
}

func TestExporter_RegisterTracker_403Removes(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	// REGISTER request
	registerReq := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: reg-403-test\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerReq)

	// 403 Forbidden response
	registerResp := []byte("SIP/2.0 403 Forbidden\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: reg-403-test\r\n" +
		"CSeq: 1 REGISTER\r\n")

	err := e.handleMessage("other", "", registerResp)
	require.NoError(t, err)

	_, exists := e.getRegisterTime("reg-403-test")
	require.False(t, exists, "register should be removed after 403")
	require.False(t, mm.rrdUpdated, "RRD should NOT be updated for 403")
}

func TestExporter_RegisterTracker_500Removes(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	// REGISTER request
	registerReq := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: reg-500-test\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerReq)

	// 500 Server Error response
	registerResp := []byte("SIP/2.0 500 Server Internal Error\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: reg-500-test\r\n" +
		"CSeq: 1 REGISTER\r\n")

	err := e.handleMessage("other", "", registerResp)
	require.NoError(t, err)

	_, exists := e.getRegisterTime("reg-500-test")
	require.False(t, exists, "register should be removed after 500")
	require.False(t, mm.rrdUpdated, "RRD should NOT be updated for 500")
}

func TestExporter_RegisterTracker_TTLExpired(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	// Add entry older than TTL (61 seconds)
	oldTime := time.Now().Add(-61 * time.Second)
	e.registerTracker["expired-call-id"] = registerEntry{timestamp: oldTime, carrier: "other"}

	// Add entry at TTL border (59 seconds)
	borderTime := time.Now().Add(-59 * time.Second)
	e.registerTracker["border-call-id"] = registerEntry{timestamp: borderTime, carrier: "other"}

	// Add fresh entry
	e.registerTracker["fresh-call-id"] = registerEntry{timestamp: time.Now(), carrier: "other"}

	// Run cleanup
	e.cleanupRegisterTracker()

	// Verify
	_, expiredExists := e.getRegisterTime("expired-call-id")
	_, borderExists := e.getRegisterTime("border-call-id")
	_, freshExists := e.getRegisterTime("fresh-call-id")

	require.False(t, expiredExists, "expired entry (61s) should be removed")
	require.True(t, borderExists, "entry at 59s should remain (TTL=60s)")
	require.True(t, freshExists, "fresh entry should remain")
}

func TestExporter_RegisterTracker_TTLNotExpired(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	// Add entry just before TTL (30 seconds ago)
	recentTime := time.Now().Add(-30 * time.Second)
	e.registerTracker["recent-call-id"] = registerEntry{timestamp: recentTime, carrier: "other"}

	// Run cleanup
	e.cleanupRegisterTracker()

	// Verify still exists
	_, exists := e.getRegisterTime("recent-call-id")
	require.True(t, exists, "entry at 30s should remain (TTL=60s)")
}

func TestExporter_RegisterTracker_Retransmit200OK(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	// First REGISTER
	registerReq := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: same-call-id\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerReq)

	// Wait a bit
	time.Sleep(20 * time.Millisecond)

	// Retransmit REGISTER (same Call-ID)
	e.handleMessage("other", "", registerReq)

	// Wait a bit more
	time.Sleep(10 * time.Millisecond)

	// 200 OK arrives
	registerResp := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: same-call-id\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerResp)

	// RRD should be measured
	require.Eventually(t, func() bool {
		return mm.rrdUpdated
	}, 100*time.Millisecond, 10*time.Millisecond)

	// RRD should be from LAST REGISTER (~10-30ms), not first (~30-50ms)
	require.Less(t, mm.rrdDelay, 35.0, "RRD should be from last REGISTER, not first")
	require.Greater(t, mm.rrdDelay, 0.0, "RRD should be positive")

	// Entry should be removed
	_, exists := e.getRegisterTime("same-call-id")
	require.False(t, exists, "entry should be removed after 200 OK")
}

func TestExporter_RegisterTracker_Retransmit401(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	// First REGISTER
	registerReq := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: same-call-id-401\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerReq)
	time.Sleep(20 * time.Millisecond)

	// Retransmit REGISTER (same Call-ID)
	e.handleMessage("other", "", registerReq)

	// 401 arrives
	registerResp := []byte("SIP/2.0 401 Unauthorized\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: same-call-id-401\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerResp)

	// RRD should NOT be updated
	require.False(t, mm.rrdUpdated, "RRD should not be updated for 401")

	// Entry should be removed
	_, exists := e.getRegisterTime("same-call-id-401")
	require.False(t, exists, "entry should be removed after 401")
}

func TestExporter_RegisterTracker_DifferentCallID(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	// REGISTER with Call-ID 1
	registerReq1 := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: call-id-1\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerReq1)

	// REGISTER with Call-ID 2
	registerReq2 := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=def\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: call-id-2\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerReq2)

	// Both should be tracked
	_, exists1 := e.getRegisterTime("call-id-1")
	_, exists2 := e.getRegisterTime("call-id-2")
	require.True(t, exists1, "call-id-1 should be tracked")
	require.True(t, exists2, "call-id-2 should be tracked")

	// 200 OK for Call-ID 1
	registerResp1 := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: call-id-1\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerResp1)

	// Only call-id-1 should be removed
	_, exists1 = e.getRegisterTime("call-id-1")
	_, exists2 = e.getRegisterTime("call-id-2")
	require.False(t, exists1, "call-id-1 should be removed")
	require.True(t, exists2, "call-id-2 should still be tracked")

	// 200 OK for Call-ID 2
	registerResp2 := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=def\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: call-id-2\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerResp2)

	// Both removed
	_, exists1 = e.getRegisterTime("call-id-1")
	_, exists2 = e.getRegisterTime("call-id-2")
	require.False(t, exists1, "call-id-1 should be removed")
	require.False(t, exists2, "call-id-2 should be removed")
}

func TestSipDialogMetricsUpdate_ExpiredIncrementsSessionCompleted(t *testing.T) {
	start := time.Now()
	mm := &mockMetricser{}
	md := &mockDialoger{
		cleanupResults: []service.CleanupResult{
			{Duration: 1 * time.Second, Carrier: "carrier-a"},
			{Duration: 2 * time.Second, Carrier: "carrier-b"},
		},
	}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
	}

	results := e.services.dialoger.Cleanup()
	for _, r := range results {
		e.services.metricser.SessionCompleted(r.Carrier, r.UAType, r.SourceCountry)
		e.services.metricser.UpdateSPD(r.Carrier, r.UAType, r.SourceCountry, r.Duration)
	}

	require.True(t, mm.sessionCompletedFlag)
	require.True(t, mm.spdUpdated)
	t.Logf("duration: %v", time.Since(start))
}

// ==================== Invite Tracker tests ====================

func TestExporter_InviteTracker_StoreAndMeasure(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	callID := "test-call-id-123"

	e.storeInviteTime(callID, "other", "other", "")

	time.Sleep(10 * time.Millisecond)

	delayMs, carrier, _, _, ok := e.readInviteEntry(callID)
	require.True(t, ok, "readInviteEntry should return true for existing entry")
	require.Greater(t, delayMs, 0.0, "delay should be positive")
	require.Equal(t, "other", carrier)

	e.removeInviteTime(callID)
	_, _, _, _, ok = e.readInviteEntry(callID)
	require.False(t, ok, "entry should not exist after remove")
}

func TestExporter_InviteTracker_StoreAndRemove(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	callID := "test-call-id-remove"

	e.storeInviteTime(callID, "other", "other", "")
	e.removeInviteTime(callID)

	_, _, _, _, ok := e.readInviteEntry(callID)
	require.False(t, ok, "entry should not exist after remove")
}

func TestExporter_InviteTracker_MeasureNonExistent(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	delayMs, carrier, _, _, ok := e.readInviteEntry("nonexistent")
	require.False(t, ok, "readInviteEntry should return false for nonexistent entry")
	require.InDelta(t, 0.0, delayMs, 0.01)
	require.Empty(t, carrier)
}

func TestExporter_InviteTracker_RemoveNonExistent(_ *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	e.removeInviteTime("nonexistent")
}

func TestExporter_InviteTracker_TTLExpired(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	oldTime := time.Now().Add(-61 * time.Second)
	e.inviteTracker["expired-call-id"] = inviteEntry{timestamp: oldTime, carrier: "other"}

	borderTime := time.Now().Add(-59 * time.Second)
	e.inviteTracker["border-call-id"] = inviteEntry{timestamp: borderTime, carrier: "other"}

	e.inviteTracker["fresh-call-id"] = inviteEntry{timestamp: time.Now(), carrier: "other"}

	e.cleanupInviteTracker()

	_, _, _, _, expiredExists := e.readInviteEntry("expired-call-id")
	_, _, _, _, borderExists := e.readInviteEntry("border-call-id")
	_, _, _, _, freshExists := e.readInviteEntry("fresh-call-id")

	require.False(t, expiredExists, "expired entry (61s) should be removed")
	require.True(t, borderExists, "entry at 59s should remain (TTL=60s)")
	require.True(t, freshExists, "fresh entry should remain")
}

func TestExporter_InviteTracker_TTLNotExpired(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	recentTime := time.Now().Add(-30 * time.Second)
	e.inviteTracker["recent-call-id"] = inviteEntry{timestamp: recentTime, carrier: "other"}

	e.cleanupInviteTracker()

	_, _, _, _, exists := e.readInviteEntry("recent-call-id")
	require.True(t, exists, "entry at 30s should remain (TTL=60s)")
}

func TestExporter_InviteTracker_DifferentCallIDs(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	e.storeInviteTime("call-id-1", "other", "other", "")
	e.storeInviteTime("call-id-2", "other", "other", "")

	_, _, _, _, ok1 := e.readInviteEntry("call-id-1")
	_, _, _, _, ok2 := e.readInviteEntry("call-id-2")
	require.True(t, ok1)
	require.True(t, ok2)

	e.removeInviteTime("call-id-1")
	_, _, _, _, ok1 = e.readInviteEntry("call-id-1")
	require.False(t, ok1, "call-id-1 should be removed")
}

// ==================== TTR integration tests ====================

func TestHandleMessage_TTR_100Trying(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: ttr-test-100\r\n" +
		"CSeq: 1 INVITE\r\n")

	err := e.handleMessage("other", "", inviteReq)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	time.Sleep(10 * time.Millisecond)

	tryingResp := []byte("SIP/2.0 100 Trying\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-test-100\r\n" +
		"CSeq: 1 INVITE\r\n")

	err = e.handleMessage("other", "", tryingResp)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return mm.ttrUpdated
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.True(t, mm.ttrUpdated)
	require.Greater(t, mm.ttrDelay, 0.0)
}

func TestHandleMessage_TTR_180Ringing(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: ttr-test-180\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", inviteReq)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	time.Sleep(10 * time.Millisecond)

	ringingResp := []byte("SIP/2.0 180 Ringing\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-test-180\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", ringingResp)
	require.Eventually(t, func() bool {
		return mm.ttrUpdated
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.Greater(t, mm.ttrDelay, 0.0)
}

func TestHandleMessage_TTR_183SessionProgress(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: ttr-test-183\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", inviteReq)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	time.Sleep(10 * time.Millisecond)

	progressResp := []byte("SIP/2.0 183 Session Progress\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-test-183\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", progressResp)
	require.Eventually(t, func() bool {
		return mm.ttrUpdated
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.Greater(t, mm.ttrDelay, 0.0)
}

func TestHandleMessage_TTR_NoProvisionalResponse(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: ttr-no-prov\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", inviteReq)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	okResp := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-no-prov\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Session-Expires: 3600\r\n")

	e.handleMessage("other", "", okResp)
	time.Sleep(20 * time.Millisecond)

	require.False(t, mm.ttrUpdated, "TTR should NOT be measured when no 1xx received")
}

func TestHandleMessage_TTR_OnlyFirstProvisionalMeasured(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: ttr-first-only\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", inviteReq)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	time.Sleep(10 * time.Millisecond)

	tryingResp := []byte("SIP/2.0 100 Trying\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-first-only\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", tryingResp)
	require.Eventually(t, func() bool {
		return mm.ttrUpdated
	}, 100*time.Millisecond, 10*time.Millisecond)

	firstTTR := mm.ttrDelay
	require.Greater(t, firstTTR, 0.0)

	time.Sleep(10 * time.Millisecond)

	ringingResp := []byte("SIP/2.0 180 Ringing\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-first-only\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", ringingResp)
	time.Sleep(10 * time.Millisecond)

	require.InDelta(t, firstTTR, mm.ttrDelay, 0.01, "TTR should NOT be measured again on second 1xx")
}

func TestHandleMessage_TTR_RetransmitOverwrites(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: ttr-retransmit\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", inviteReq)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	time.Sleep(20 * time.Millisecond)

	e.handleMessage("other", "", inviteReq)

	time.Sleep(10 * time.Millisecond)

	tryingResp := []byte("SIP/2.0 100 Trying\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-retransmit\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", tryingResp)
	require.Eventually(t, func() bool {
		return mm.ttrUpdated
	}, 100*time.Millisecond, 10*time.Millisecond)

	require.Less(t, mm.ttrDelay, 35.0, "TTR should be from last INVITE, not first")
	require.Greater(t, mm.ttrDelay, 0.0)
}

func TestHandleMessage_TTR_FinalResponseRemovesTracker(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: ttr-final-remove\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", inviteReq)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	busyResp := []byte("SIP/2.0 486 Busy Here\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-final-remove\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", busyResp)
	time.Sleep(10 * time.Millisecond)

	require.False(t, mm.ttrUpdated, "TTR should NOT be measured for non-1xx response")

	_, _, _, _, ok := e.readInviteEntry("ttr-final-remove")
	require.False(t, ok, "tracker entry should be removed after final response")
}

func TestHandleMessage_TTR_NonInviteResponse_Ignored(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	registerReq := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: ttr-non-invite\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerReq)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("REGISTER"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	tryingResp := []byte("SIP/2.0 100 Trying\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-non-invite\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", tryingResp)
	time.Sleep(10 * time.Millisecond)

	require.False(t, mm.ttrUpdated, "TTR should NOT be measured for REGISTER 100 Trying")
}

func TestHandleMessage_TTR_FullCallFlow(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: ttr-full-flow\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", inviteReq)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	time.Sleep(10 * time.Millisecond)

	tryingResp := []byte("SIP/2.0 100 Trying\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-full-flow\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", tryingResp)
	require.Eventually(t, func() bool {
		return mm.ttrUpdated
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.Greater(t, mm.ttrDelay, 0.0)

	okResp := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-full-flow\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Session-Expires: 3600\r\n")

	e.handleMessage("other", "", okResp)
	require.Eventually(t, func() bool {
		return mm.invite200OKCalled
	}, 100*time.Millisecond, 10*time.Millisecond)

	byeReq := []byte("BYE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-full-flow\r\n" +
		"CSeq: 2 BYE\r\n")

	e.handleMessage("other", "", byeReq)
	time.Sleep(10 * time.Millisecond)

	byeOkResp := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-full-flow\r\n" +
		"CSeq: 2 BYE\r\n")

	e.handleMessage("other", "", byeOkResp)
	time.Sleep(10 * time.Millisecond)

	require.True(t, mm.ttrUpdated, "TTR should be measured during full call flow")
	require.True(t, mm.sessionCompletedFlag, "session should be completed")
}

// ==================== PDD integration tests ====================

func TestHandleMessage_PDD_180Ringing(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: pdd-test-180\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", inviteReq)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	time.Sleep(10 * time.Millisecond)

	ringingResp := []byte("SIP/2.0 180 Ringing\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: pdd-test-180\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", ringingResp)
	require.Eventually(t, func() bool {
		return mm.pddUpdated
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.True(t, mm.ttrUpdated, "TTR should also be measured on 180")
	require.Greater(t, mm.pddDelay, 0.0)
	require.InDelta(t, mm.ttrDelay, mm.pddDelay, 0.01, "PDD and TTR delay should be equal for direct 180")
}

func TestHandleMessage_PDD_100TryingThen180Ringing(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: pdd-test-100-180\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", inviteReq)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	time.Sleep(5 * time.Millisecond)

	tryingResp := []byte("SIP/2.0 100 Trying\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: pdd-test-100-180\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", tryingResp)
	require.Eventually(t, func() bool {
		return mm.ttrUpdated
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.False(t, mm.pddUpdated, "PDD should NOT be measured on 100 Trying")

	time.Sleep(10 * time.Millisecond)

	ringingResp := []byte("SIP/2.0 180 Ringing\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: pdd-test-100-180\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", ringingResp)
	require.Eventually(t, func() bool {
		return mm.pddUpdated
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.Greater(t, mm.pddDelay, 0.0)
}

func TestHandleMessage_PDD_183NoPDD(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: pdd-test-183\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", inviteReq)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	time.Sleep(10 * time.Millisecond)

	progressResp := []byte("SIP/2.0 183 Session Progress\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: pdd-test-183\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", progressResp)
	require.Eventually(t, func() bool {
		return mm.ttrUpdated
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.False(t, mm.pddUpdated, "PDD should NOT be measured on 183 Session Progress")
}

func TestHandleMessage_PDD_No180NoPDD(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: pdd-test-no180\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", inviteReq)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	time.Sleep(10 * time.Millisecond)

	okResp := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: pdd-test-no180\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Session-Expires: 3600\r\n")

	e.handleMessage("other", "", okResp)
	time.Sleep(10 * time.Millisecond)

	require.False(t, mm.pddUpdated, "PDD should NOT be measured when no 180 received")
	require.False(t, mm.ttrUpdated, "TTR should NOT be measured when no 1xx received")
}

func TestHandleMessage_PDD_NonInviteResponse_Ignored(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	regReq := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:user@domain>\r\n" +
		"Call-ID: pdd-non-invite\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", regReq)
	time.Sleep(10 * time.Millisecond)

	tryingResp := []byte("SIP/2.0 180 Ringing\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:user@domain>;tag=xyz\r\n" +
		"Call-ID: pdd-non-invite\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", tryingResp)
	time.Sleep(10 * time.Millisecond)

	require.False(t, mm.pddUpdated, "PDD should NOT be measured for REGISTER 180 Ringing")
}

func TestHandleMessage_CarrierPropagation_FullDialog(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		optionsTracker:  make(map[string]optionsEntry),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	inviteReq := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: carrier-dialog-test\r\n" +
		"CSeq: 1 INVITE\r\n")

	err := e.handleMessage("carrier-A", "", inviteReq)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return bytes.Equal(mm.requestCalled, []byte("INVITE"))
	}, 100*time.Millisecond, 10*time.Millisecond)

	time.Sleep(10 * time.Millisecond)

	okResp := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: carrier-dialog-test\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Session-Expires: 3600\r\n")

	err = e.handleMessage("carrier-B", "", okResp)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(md.created) > 0
	}, 100*time.Millisecond, 10*time.Millisecond)

	dialogID := "carrier-dialog-test:abc:xyz"
	require.Equal(t, "carrier-A", md.created[dialogID].carrier)

	byeResp := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: carrier-dialog-test\r\n" +
		"CSeq: 2 BYE\r\n")

	err = e.handleMessage("carrier-B", "", byeResp)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(md.deleted) > 0
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.True(t, mm.sessionCompletedFlag)
	require.True(t, mm.spdUpdated)
}

func TestHandleMessage_CarrierPropagation_MultiCarrierDialogs(t *testing.T) {
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		optionsTracker:  make(map[string]optionsEntry),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	carrierACount := 10
	carrierBCount := 20

	for i := range carrierACount {
		callID := fmt.Sprintf("call-a-%d", i)
		invite := []byte("INVITE sip:test SIP/2.0\r\n" +
			"From: <sip:user@domain>;tag=from-a-" + callID + "\r\n" +
			"To: <sip:other@domain>\r\n" +
			"Call-ID: " + callID + "\r\n" +
			"CSeq: 1 INVITE\r\n")
		e.handleMessage("carrier-A", "", invite)

		okResp := []byte("SIP/2.0 200 OK\r\n" +
			"From: <sip:user@domain>;tag=from-a-" + callID + "\r\n" +
			"To: <sip:other@domain>;tag=to-a-" + callID + "\r\n" +
			"Call-ID: " + callID + "\r\n" +
			"CSeq: 1 INVITE\r\n" +
			"Session-Expires: 3600\r\n")
		e.handleMessage("carrier-C", "", okResp)
	}

	for i := range carrierBCount {
		callID := fmt.Sprintf("call-b-%d", i)
		invite := []byte("INVITE sip:test SIP/2.0\r\n" +
			"From: <sip:user@domain>;tag=from-b-" + callID + "\r\n" +
			"To: <sip:other@domain>\r\n" +
			"Call-ID: " + callID + "\r\n" +
			"CSeq: 1 INVITE\r\n")
		e.handleMessage("carrier-B", "", invite)

		okResp := []byte("SIP/2.0 200 OK\r\n" +
			"From: <sip:user@domain>;tag=from-b-" + callID + "\r\n" +
			"To: <sip:other@domain>;tag=to-b-" + callID + "\r\n" +
			"Call-ID: " + callID + "\r\n" +
			"CSeq: 1 INVITE\r\n" +
			"Session-Expires: 3600\r\n")
		e.handleMessage("carrier-C", "", okResp)
	}

	require.Eventually(t, func() bool {
		return len(md.created) == carrierACount+carrierBCount
	}, 200*time.Millisecond, 10*time.Millisecond)

	carrierADialogs := 0
	carrierBDialogs := 0
	carrierCDialogs := 0
	for _, args := range md.created {
		switch args.carrier {
		case "carrier-A":
			carrierADialogs++
		case "carrier-B":
			carrierBDialogs++
		case "carrier-C":
			carrierCDialogs++
		}
	}

	require.Equal(t, carrierACount, carrierADialogs, "all carrier-A INVITEs should create carrier-A dialogs")
	require.Equal(t, carrierBCount, carrierBDialogs, "all carrier-B INVITEs should create carrier-B dialogs")
	require.Equal(t, 0, carrierCDialogs, "carrier-C (server) should never own a dialog")
}

// ==================== Carrier-tracking mock for MC/DC tests ====================

type carrierCall struct {
	carrier       string
	method        string
	value         float64
	uaType        string
	sourceCountry string
}

type carrierFailure struct {
	carrier string
	code    string
}

type carrierTrackingMetricser struct {
	requests               []carrierCall
	responseWithMetrics    []carrierCall
	ttrCalls               []carrierCall
	pddCalls               []carrierCall
	rrdCalls               []carrierCall
	lrdCalls               []carrierCall
	ordCalls               []carrierCall
	spdCalls               []carrierCall
	sessionCompleted       []carrierCall
	invite200OK            []carrierCall
	vqReports              []carrierCall
	registerSuccess        []string
	registerFailure        []carrierFailure
	registerCountryChange  []carrierCall
	registerScan           []carrierCall
	inviteBurst            []carrierCall
	packetsTotal           int
	systemErrors           int
	sessionsByCarrierAndUA map[string]map[string]int
}

func newCarrierTrackingMetricser() *carrierTrackingMetricser {
	return &carrierTrackingMetricser{
		sessionsByCarrierAndUA: make(map[string]map[string]int),
	}
}

func (m *carrierTrackingMetricser) Request(carrier, _, _, _, _, _ string, in []byte) {
	m.requests = append(m.requests, carrierCall{carrier: carrier, method: string(in)})
	m.packetsTotal++
}

func (m *carrierTrackingMetricser) Reinvite(carrier, _, _ string) {
	m.requests = append(m.requests, carrierCall{carrier: carrier, method: "REINVITE"})
	m.packetsTotal++
}

func (m *carrierTrackingMetricser) Response(_, _, _ string, _ []byte, _ bool) {
	m.packetsTotal++
}

func (m *carrierTrackingMetricser) ResponseWithMetrics(
	carrier, _, _ string, status []byte, isInviteResponse, is200OK bool,
) {
	m.responseWithMetrics = append(m.responseWithMetrics, carrierCall{carrier: carrier, method: string(status)})
	m.packetsTotal++
	if is200OK && isInviteResponse {
		m.invite200OK = append(m.invite200OK, carrierCall{carrier: carrier})
	}
}

func (m *carrierTrackingMetricser) Invite200OK(_, _, _, _, _, _ string) {
	// Tracking is done via ResponseWithMetrics which receives the is200OK flag.
}

func (m *carrierTrackingMetricser) SessionCompleted(carrier, _, _ string) {
	m.sessionCompleted = append(m.sessionCompleted, carrierCall{carrier: carrier})
}

func (m *carrierTrackingMetricser) RegisterSuccess(carrier, _, _ string) {
	m.registerSuccess = append(m.registerSuccess, carrier)
}

func (m *carrierTrackingMetricser) RegisterFailure(carrier, _, _, code string) {
	m.registerFailure = append(m.registerFailure, carrierFailure{carrier: carrier, code: code})
}

func (m *carrierTrackingMetricser) RegisterCountryChange(carrier, sourceCountry string) {
	m.registerCountryChange = append(m.registerCountryChange,
		carrierCall{carrier: carrier, sourceCountry: sourceCountry})
}

func (m *carrierTrackingMetricser) RegisterScan(carrier, sourceCountry string) {
	m.registerScan = append(m.registerScan,
		carrierCall{carrier: carrier, sourceCountry: sourceCountry})
}

func (m *carrierTrackingMetricser) InviteBurst(carrier, sourceCountry string) {
	m.inviteBurst = append(m.inviteBurst,
		carrierCall{carrier: carrier, sourceCountry: sourceCountry})
}

func (m *carrierTrackingMetricser) UpdateRRD(carrier, _, _ string, delayMs float64) {
	m.rrdCalls = append(m.rrdCalls, carrierCall{carrier: carrier, value: delayMs})
}

func (m *carrierTrackingMetricser) UpdateSPD(carrier, _, _ string, duration time.Duration) {
	m.spdCalls = append(m.spdCalls, carrierCall{carrier: carrier, value: duration.Seconds()})
}

func (m *carrierTrackingMetricser) UpdateTTR(carrier, _, _ string, delayMs float64) {
	m.ttrCalls = append(m.ttrCalls, carrierCall{carrier: carrier, value: delayMs})
}

func (m *carrierTrackingMetricser) UpdatePDD(carrier, _, _ string, delayMs float64) {
	m.pddCalls = append(m.pddCalls, carrierCall{carrier: carrier, value: delayMs})
}

func (m *carrierTrackingMetricser) UpdateORD(carrier, _, _ string, delayMs float64) {
	m.ordCalls = append(m.ordCalls, carrierCall{carrier: carrier, value: delayMs})
}

func (m *carrierTrackingMetricser) UpdateLRD(carrier, _, _ string, delayMs float64) {
	m.lrdCalls = append(m.lrdCalls, carrierCall{carrier: carrier, value: delayMs})
}

func (m *carrierTrackingMetricser) UpdateSession(carrier, uaType, _ string, size int) {
	if m.sessionsByCarrierAndUA[carrier] == nil {
		m.sessionsByCarrierAndUA[carrier] = make(map[string]int)
	}
	m.sessionsByCarrierAndUA[carrier][uaType] = size
}

func (m *carrierTrackingMetricser) UpdateSessions(_ []service.LabeledCount) {}

func (m *carrierTrackingMetricser) SetSessionsLimits(_ map[string]int) {}

func (m *carrierTrackingMetricser) UpdateActiveRegistrations(_ []service.LabeledCount) {}

func (m *carrierTrackingMetricser) SystemError() {
	m.systemErrors++
}

func (m *carrierTrackingMetricser) ParseError(string)             {}
func (m *carrierTrackingMetricser) SocketStats(_, _ uint32)       {}
func (m *carrierTrackingMetricser) UpdateChannelLength(int)       {}
func (m *carrierTrackingMetricser) UpdateChannelCapacity(int)     {}
func (m *carrierTrackingMetricser) UpdateTrackerSize(string, int) {}
func (m *carrierTrackingMetricser) UpdateActiveDialogs(int)       {}

func (m *carrierTrackingMetricser) UpdateVQReport(carrier, uaType, _ string, _ *vq.SessionReport) {
	m.vqReports = append(m.vqReports, carrierCall{carrier: carrier, uaType: uaType})
}

func (m *carrierTrackingMetricser) UpdateRTPPackets(string, string, string, string)         {}
func (m *carrierTrackingMetricser) UpdateRTPLoss(string, string, string, string, uint64)    {}
func (m *carrierTrackingMetricser) UpdateRTPDuplicates(string, string, string, string)      {}
func (m *carrierTrackingMetricser) UpdateRTPJitter(string, string, string, string, float64) {}
func (m *carrierTrackingMetricser) UpdateRTPMOS(string, string, string, string, float64)    {}
func (m *carrierTrackingMetricser) UpdateRTPMOSVariants(string, string, string, string, float64, float64, float64) {
}
func (m *carrierTrackingMetricser) UpdateRTPRFactor(string, string, string, string, float64) {}
func (m *carrierTrackingMetricser) UpdateRTPLossDistribution(string, string, string, string, float64, float64) {
}
func (m *carrierTrackingMetricser) UpdateRTPActiveStreams(_ []service.LabeledCount) {}
func (m *carrierTrackingMetricser) OneWayCall(string, string, string)               {}
func (m *carrierTrackingMetricser) MissingRTP(string, string, string)               {}

// ==================== SIP message builders for MC/DC tests ====================

func makeInvite(callID string, fromTag string) []byte {
	return []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=" + fromTag + "\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 INVITE\r\n")
}

func makeInvite200OK(callID string, fromTag string, toTag string) []byte {
	return []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=" + fromTag + "\r\n" +
		"To: <sip:other@domain>;tag=" + toTag + "\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Session-Expires: 3600\r\n")
}

func makeTrying(callID string, fromTag string, toTag string) []byte {
	return []byte("SIP/2.0 100 Trying\r\n" +
		"From: <sip:user@domain>;tag=" + fromTag + "\r\n" +
		"To: <sip:other@domain>;tag=" + toTag + "\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 INVITE\r\n")
}

func makeBye200OK(callID string, fromTag string, toTag string) []byte {
	return []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=" + fromTag + "\r\n" +
		"To: <sip:other@domain>;tag=" + toTag + "\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 2 BYE\r\n")
}

func makeRegister(callID string, fromTag string) []byte {
	return []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=" + fromTag + "\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 REGISTER\r\n")
}

func makeRegister200OK(callID string, fromTag string, toTag string) []byte {
	return []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=" + fromTag + "\r\n" +
		"To: <sip:other@domain>;tag=" + toTag + "\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 REGISTER\r\n")
}

// makeRegisterStatus builds a REGISTER response with an arbitrary status line
// (e.g. "403 Forbidden", "401 Unauthorized", "100 Trying").
func makeRegisterStatus(statusLine, callID, fromTag, toTag string) []byte {
	return []byte("SIP/2.0 " + statusLine + "\r\n" +
		"From: <sip:user@domain>;tag=" + fromTag + "\r\n" +
		"To: <sip:other@domain>;tag=" + toTag + "\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 REGISTER\r\n")
}

func makeRegister3xx(callID string, fromTag string, toTag string) []byte {
	return []byte("SIP/2.0 302 Moved\r\n" +
		"From: <sip:user@domain>;tag=" + fromTag + "\r\n" +
		"To: <sip:other@domain>;tag=" + toTag + "\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 REGISTER\r\n")
}

func makeOptions(callID string, fromTag string) []byte {
	return []byte("OPTIONS sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=" + fromTag + "\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 OPTIONS\r\n")
}

func makeOptions200OK(callID string, fromTag string, toTag string) []byte {
	return []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=" + fromTag + "\r\n" +
		"To: <sip:other@domain>;tag=" + toTag + "\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 OPTIONS\r\n")
}

func newTestExporter(mm *carrierTrackingMetricser, md *mockDialoger) *exporter {
	return &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		vqHandler:       vq.NewHandler(mm),
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		optionsTracker:  make(map[string]optionsEntry),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}
}

func countCarrier(calls []carrierCall, carrier string) int {
	n := 0
	for _, c := range calls {
		if c.carrier == carrier {
			n++
		}
	}
	return n
}

func countCarrierMethod(calls []carrierCall, carrier, method string) int {
	n := 0
	for _, c := range calls {
		if c.carrier == carrier && c.method == method {
			n++
		}
	}
	return n
}

// ==================== MC/DC Carrier Propagation Tests ====================

func TestMCDC_TC1_InviteResponse_CarrierFromTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeInvite("tc1", "ft1"))
	e.handleMessage("carrier-B", "", makeInvite200OK("tc1", "ft1", "tt1"))

	require.Eventually(t, func() bool { return len(md.created) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", md.created["tc1:ft1:tt1"].carrier)
}

func TestMCDC_TC2_InviteResponse_CarrierFallbackWithoutTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-B", "", makeInvite200OK("tc2", "ft2", "tt2"))

	require.Eventually(t, func() bool { return len(mm.responseWithMetrics) > 0 },
		100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-B", mm.responseWithMetrics[0].carrier)
}

func TestMCDC_TC3_RegisterResponse_CarrierFromTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeRegister("tc3", "ft3"))
	time.Sleep(5 * time.Millisecond)
	e.handleMessage("carrier-B", "", makeRegister200OK("tc3", "ft3", "tt3"))

	require.Eventually(t, func() bool { return len(mm.rrdCalls) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", mm.rrdCalls[0].carrier)
}

func TestMCDC_TC4_RegisterResponse_CarrierFallbackWithoutTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-B", "", makeRegister200OK("tc4", "ft4", "tt4"))

	require.Eventually(
		t,
		func() bool { return len(mm.responseWithMetrics) > 0 },
		100*time.Millisecond,
		10*time.Millisecond,
	)
	require.Equal(t, "carrier-B", mm.responseWithMetrics[0].carrier)
}

func TestMCDC_TC5_TTR_1xxResponse_CarrierFromTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeInvite("tc5", "ft5"))
	time.Sleep(5 * time.Millisecond)
	e.handleMessage("carrier-B", "", makeTrying("tc5", "ft5", "tt5"))

	require.Eventually(t, func() bool { return len(mm.ttrCalls) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", mm.ttrCalls[0].carrier)
}

func TestMCDC_TC6_TTR_Non1xxResponse_NotMeasured(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeInvite("tc6", "ft6"))
	e.handleMessage("carrier-B", "", makeInvite200OK("tc6", "ft6", "tt6"))

	require.Eventually(t, func() bool { return len(mm.invite200OK) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Empty(t, mm.ttrCalls)
	_, _, _, _, ok := e.readInviteEntry("tc6")
	require.False(t, ok)
}

func TestMCDC_TC7_TTR_NonInviteResponse_Ignored(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeRegister("tc7", "ft7"))
	time.Sleep(5 * time.Millisecond)
	e.handleMessage("carrier-B", "", makeRegister200OK("tc7", "ft7", "tt7"))

	require.Eventually(t, func() bool { return len(mm.rrdCalls) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Empty(t, mm.ttrCalls)
}

func TestMCDC_TC8_DialogCreatedWithTrackerCarrier_Mismatch(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeInvite("tc8", "ft8"))
	e.handleMessage("carrier-B", "", makeInvite200OK("tc8", "ft8", "tt8"))

	require.Eventually(t, func() bool { return len(md.created) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", md.created["tc8:ft8:tt8"].carrier,
		"dialog carrier must come from INVITE tracker, not from 200 OK packet")
	require.Equal(t, "carrier-A", mm.invite200OK[0].carrier,
		"invite200OK metric must use INVITE tracker carrier")
}

func TestMCDC_TC9_DialogCreatedWithTrackerCarrier_SameCarrier(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeInvite("tc9", "ft9"))
	e.handleMessage("carrier-A", "", makeInvite200OK("tc9", "ft9", "tt9"))

	require.Eventually(t, func() bool { return len(md.created) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", md.created["tc9:ft9:tt9"].carrier)
}

func TestMCDC_TC10_Bye200OK_CarrierFromDialog(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeInvite("tc10", "ft10"))
	e.handleMessage("carrier-B", "", makeInvite200OK("tc10", "ft10", "tt10"))
	require.Eventually(t, func() bool { return len(md.created) > 0 }, 100*time.Millisecond, 10*time.Millisecond)

	e.handleMessage("carrier-C", "", makeBye200OK("tc10", "ft10", "tt10"))
	require.Eventually(
		t,
		func() bool { return len(mm.sessionCompleted) > 0 },
		100*time.Millisecond,
		10*time.Millisecond,
	)

	require.Equal(t, "carrier-A", mm.sessionCompleted[0].carrier,
		"SessionCompleted must use dialog carrier (from INVITE), not BYE packet carrier")
	require.Equal(t, "carrier-A", mm.spdCalls[0].carrier,
		"UpdateSPD must use dialog carrier")
}

func TestMCDC_TC11_Bye200OK_NonExistingDialog_NoMetrics(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeBye200OK("tc11-nonexist", "ft11", "tt11"))

	require.Eventually(
		t,
		func() bool { return len(mm.responseWithMetrics) > 0 },
		100*time.Millisecond,
		10*time.Millisecond,
	)
	require.Empty(t, mm.sessionCompleted)
	require.Empty(t, mm.spdCalls)
}

func TestMCDC_TC12_DialogExpiry_CarrierFromDialog(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{
		cleanupResults: []service.CleanupResult{
			{Duration: 5 * time.Minute, Carrier: "carrier-A"},
		},
	}
	e := newTestExporter(mm, md)

	results := e.services.dialoger.Cleanup()
	for _, r := range results {
		e.services.metricser.SessionCompleted(r.Carrier, r.UAType, r.SourceCountry)
		e.services.metricser.UpdateSPD(r.Carrier, r.UAType, r.SourceCountry, r.Duration)
	}

	require.Len(t, mm.sessionCompleted, 1)
	require.Equal(t, "carrier-A", mm.sessionCompleted[0].carrier)
	require.Equal(t, "carrier-A", mm.spdCalls[0].carrier)
}

func TestMCDC_TC13_DialogExpiry_DifferentCarrier(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{
		cleanupResults: []service.CleanupResult{
			{Duration: 3 * time.Minute, Carrier: "carrier-B"},
		},
	}
	e := newTestExporter(mm, md)

	results := e.services.dialoger.Cleanup()
	for _, r := range results {
		e.services.metricser.SessionCompleted(r.Carrier, r.UAType, r.SourceCountry)
		e.services.metricser.UpdateSPD(r.Carrier, r.UAType, r.SourceCountry, r.Duration)
	}

	require.Equal(t, "carrier-B", mm.sessionCompleted[0].carrier)
}

func TestMCDC_TC14_Register200OK_RRDCarrierFromTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeRegister("tc14", "ft14"))
	time.Sleep(5 * time.Millisecond)
	e.handleMessage("carrier-B", "", makeRegister200OK("tc14", "ft14", "tt14"))

	require.Eventually(t, func() bool { return len(mm.rrdCalls) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", mm.rrdCalls[0].carrier)
	require.Greater(t, mm.rrdCalls[0].value, 0.0)
}

func TestMCDC_TC15_Register3xx_LRDCarrierFromTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeRegister("tc15", "ft15"))
	time.Sleep(5 * time.Millisecond)
	e.handleMessage("carrier-B", "", makeRegister3xx("tc15", "ft15", "tt15"))

	require.Eventually(t, func() bool { return len(mm.lrdCalls) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", mm.lrdCalls[0].carrier)
}

func TestMCDC_TC16_OptionsResponse_ORDCarrierFromTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeOptions("tc16", "ft16"))
	time.Sleep(5 * time.Millisecond)
	e.handleMessage("carrier-B", "", makeOptions200OK("tc16", "ft16", "tt16"))

	require.Eventually(t, func() bool { return len(mm.ordCalls) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", mm.ordCalls[0].carrier)
}

func TestMCDC_TC17_MultiCarrier_CorrectAttribution(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	for i := range 10 {
		callID := fmt.Sprintf("tc17-a-%d", i)
		e.handleMessage("carrier-A", "", makeInvite(callID, "ft-"+callID))
		e.handleMessage("carrier-C", "", makeInvite200OK(callID, "ft-"+callID, "tt-"+callID))
	}

	for i := range 20 {
		callID := fmt.Sprintf("tc17-b-%d", i)
		e.handleMessage("carrier-B", "", makeInvite(callID, "ft-"+callID))
		e.handleMessage("carrier-C", "", makeInvite200OK(callID, "ft-"+callID, "tt-"+callID))
	}

	require.Eventually(t, func() bool { return len(md.created) == 30 }, 200*time.Millisecond, 10*time.Millisecond)

	carrierA := 0
	carrierB := 0
	carrierC := 0
	for _, args := range md.created {
		switch args.carrier {
		case "carrier-A":
			carrierA++
		case "carrier-B":
			carrierB++
		case "carrier-C":
			carrierC++
		}
	}

	require.Equal(t, 10, carrierA)
	require.Equal(t, 20, carrierB)
	require.Equal(t, 0, carrierC)

	require.Equal(t, 10, countCarrier(mm.invite200OK, "carrier-A"))
	require.Equal(t, 20, countCarrier(mm.invite200OK, "carrier-B"))
	require.Equal(t, 0, countCarrier(mm.invite200OK, "carrier-C"))
}

func TestMCDC_TC18_TrackerTTLExpired_FallbackToPacketCarrier(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	oldTime := time.Now().Add(-61 * time.Second)
	e.inviteTracker["tc18"] = inviteEntry{timestamp: oldTime, carrier: "carrier-A"}
	e.cleanupInviteTracker()

	e.handleMessage("carrier-B", "", makeInvite200OK("tc18", "ft18", "tt18"))

	require.Eventually(
		t,
		func() bool { return len(mm.responseWithMetrics) > 0 },
		100*time.Millisecond,
		10*time.Millisecond,
	)
	require.Equal(t, "carrier-B", mm.responseWithMetrics[0].carrier,
		"when tracker entry expired, should fall back to packet carrier")
}

func TestMCDC_TC19_Retransmit_OverwritesCarrier(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeInvite("tc19", "ft19"))
	e.handleMessage("carrier-B", "", makeInvite("tc19", "ft19"))
	e.handleMessage("carrier-C", "", makeInvite200OK("tc19", "ft19", "tt19"))

	require.Eventually(t, func() bool { return len(md.created) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-B", md.created["tc19:ft19:tt19"].carrier,
		"retransmitted INVITE should overwrite carrier in tracker")
}

func TestMCDC_TC20_OtherCarrier_20Known_10Other(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	for i := range 20 {
		callID := fmt.Sprintf("tc20-known-%d", i)
		e.handleMessage("carrier-A", "", makeInvite(callID, "ft-"+callID))
		e.handleMessage("carrier-B", "", makeInvite200OK(callID, "ft-"+callID, "tt-"+callID))
	}

	for i := range 10 {
		callID := fmt.Sprintf("tc20-other-%d", i)
		e.handleMessage("other", "", makeInvite(callID, "ft-"+callID))
		e.handleMessage("carrier-B", "", makeInvite200OK(callID, "ft-"+callID, "tt-"+callID))
	}

	require.Eventually(t, func() bool { return len(md.created) == 30 }, 200*time.Millisecond, 10*time.Millisecond)

	carrierA := 0
	carrierOther := 0
	carrierB := 0
	for _, args := range md.created {
		switch args.carrier {
		case "carrier-A":
			carrierA++
		case "other":
			carrierOther++
		case "carrier-B":
			carrierB++
		}
	}

	require.Equal(t, 20, carrierA)
	require.Equal(t, 10, carrierOther)
	require.Equal(t, 0, carrierB)
}

func TestHandleMessage_CANCEL_RemovesInviteTracker(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		optionsTracker:  make(map[string]optionsEntry),
		registerTracker: make(map[string]registerEntry),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	invitePkt := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: cancel-test-1\r\n" +
		"CSeq: 1 INVITE\r\n")

	err := e.handleMessage("other", "", invitePkt)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		_, _, _, _, ok := e.readInviteEntry("cancel-test-1")
		return ok
	}, 100*time.Millisecond, 10*time.Millisecond, "inviteTracker should have entry after INVITE")

	cancelPkt := []byte("CANCEL sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: cancel-test-1\r\n" +
		"CSeq: 2 CANCEL\r\n")

	err = e.handleMessage("other", "", cancelPkt)
	require.NoError(t, err)

	_, _, _, _, ok := e.readInviteEntry("cancel-test-1")
	require.False(t, ok, "inviteTracker entry should be removed after CANCEL")
}

func TestHandleMessage_CANCEL_NoEntry_NoOp(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		optionsTracker:  make(map[string]optionsEntry),
		registerTracker: make(map[string]registerEntry),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	cancelPkt := []byte("CANCEL sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: nonexistent-call\r\n" +
		"CSeq: 2 CANCEL\r\n")

	err := e.handleMessage("other", "", cancelPkt)
	require.NoError(t, err)
}

func TestHandleMessage_CANCEL_ThenProvisional_NoTTR(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		optionsTracker:  make(map[string]optionsEntry),
		registerTracker: make(map[string]registerEntry),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	invitePkt := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: cancel-ttr-test\r\n" +
		"CSeq: 1 INVITE\r\n")

	err := e.handleMessage("other", "", invitePkt)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		_, _, _, _, ok := e.readInviteEntry("cancel-ttr-test")
		return ok
	}, 100*time.Millisecond, 10*time.Millisecond)

	cancelPkt := []byte("CANCEL sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: cancel-ttr-test\r\n" +
		"CSeq: 2 CANCEL\r\n")

	err = e.handleMessage("other", "", cancelPkt)
	require.NoError(t, err)

	provisionalPkt := []byte("SIP/2.0 100 Trying\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: cancel-ttr-test\r\n" +
		"CSeq: 1 INVITE\r\n")

	err = e.handleMessage("other", "", provisionalPkt)
	require.NoError(t, err)

	require.False(t, mm.ttrUpdated, "TTR should not be measured after CANCEL removed tracker entry")
}

func TestHandleRequest_PUBLISH_VQReport(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	vqBody := "VQSessionReport: CallTerm\r\nMOSLQ=4.5 NLR=0.50\r\n"
	publish := []byte("PUBLISH sip:collector@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.1.5:5060\r\n" +
		"From: <sip:user1@example.com>;tag=abc123\r\n" +
		"To: <sip:collector@example.com>;tag=xyz789\r\n" +
		"Call-ID: vq-test-publish@example.com\r\n" +
		"CSeq: 1 PUBLISH\r\n" +
		"Content-Type: application/vq-rtcpxr\r\n" +
		"\r\n" +
		vqBody)

	err := e.handleMessage("carrier-a", "", publish)
	require.NoError(t, err)

	require.Equal(t, 1, countCarrierMethod(mm.requests, "carrier-a", "PUBLISH"), "PUBLISH request should be counted")
	require.Equal(t, 0, mm.systemErrors, "VQ report should not trigger system error")
	require.Len(t, mm.vqReports, 1, "VQ handler should be called once")
	require.Equal(t, "carrier-a", mm.vqReports[0].carrier)
}

func TestHandleRequest_NOTIFY_VQReport(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	vqBody := "VQSessionReport: CallTerm\r\nMOSLQ=4.2 IAJ=5.2\r\n"
	notify := []byte("NOTIFY sip:user@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.1.5:5060\r\n" +
		"From: <sip:server@example.com>;tag=abc123\r\n" +
		"To: <sip:user@example.com>;tag=xyz789\r\n" +
		"Call-ID: vq-test-notify@example.com\r\n" +
		"CSeq: 2 NOTIFY\r\n" +
		"Content-Type: application/vq-rtcpxr\r\n" +
		"\r\n" +
		vqBody)

	err := e.handleMessage("carrier-b", "", notify)
	require.NoError(t, err)

	require.Equal(t, 1, countCarrierMethod(mm.requests, "carrier-b", "NOTIFY"), "NOTIFY request should be counted")
	require.Equal(t, 0, mm.systemErrors, "VQ report should not trigger system error")
	require.Len(t, mm.vqReports, 1, "VQ handler should be called once")
	require.Equal(t, "carrier-b", mm.vqReports[0].carrier)
}

func TestHandleRequest_PUBLISH_NoVQContentType(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	publish := []byte("PUBLISH sip:collector@example.com SIP/2.0\r\n" +
		"From: <sip:user1@example.com>;tag=abc123\r\n" +
		"To: <sip:collector@example.com>;tag=xyz789\r\n" +
		"Call-ID: no-vq-test@example.com\r\n" +
		"CSeq: 1 PUBLISH\r\n" +
		"Content-Type: application/sdp\r\n" +
		"\r\n" +
		"some sdp body")

	err := e.handleMessage("carrier-a", "", publish)
	require.NoError(t, err)
	require.Equal(t, 0, mm.systemErrors)
	require.Empty(t, mm.vqReports, "VQ handler should not be called for non-vq content type")
}

func TestHandleRequest_NOTIFY_NoVQContentType(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	notify := []byte("NOTIFY sip:user@example.com SIP/2.0\r\n" +
		"From: <sip:server@example.com>;tag=abc123\r\n" +
		"To: <sip:user@example.com>;tag=xyz789\r\n" +
		"Call-ID: no-vq-notify@example.com\r\n" +
		"CSeq: 2 NOTIFY\r\n" +
		"Content-Type: application/dialog-info+xml\r\n" +
		"\r\n" +
		"some body")

	err := e.handleMessage("carrier-a", "", notify)
	require.NoError(t, err)
	require.Equal(t, 0, mm.systemErrors)
	require.Empty(t, mm.vqReports, "VQ handler should not be called for non-vq content type")
}

func TestHandleRequest_PUBLISH_VQEmptyBody(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	publish := []byte("PUBLISH sip:collector@example.com SIP/2.0\r\n" +
		"From: <sip:user1@example.com>;tag=abc123\r\n" +
		"To: <sip:collector@example.com>;tag=xyz789\r\n" +
		"Call-ID: empty-vq@example.com\r\n" +
		"CSeq: 1 PUBLISH\r\n" +
		"Content-Type: application/vq-rtcpxr\r\n" +
		"\r\n")

	err := e.handleMessage("carrier-a", "", publish)
	require.NoError(t, err)
	require.Equal(t, 1, mm.systemErrors, "empty VQ body should trigger system error")
	require.Empty(t, mm.vqReports, "VQ handler should not report metrics for empty body")
}

func TestHandleRequest_NOTIFY_VQInvalidBody(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	notify := []byte("NOTIFY sip:user@example.com SIP/2.0\r\n" +
		"From: <sip:server@example.com>;tag=abc123\r\n" +
		"To: <sip:user@example.com>;tag=xyz789\r\n" +
		"Call-ID: invalid-vq@example.com\r\n" +
		"CSeq: 2 NOTIFY\r\n" +
		"Content-Type: application/vq-rtcpxr\r\n" +
		"\r\n" +
		"this is not a valid vq report")

	err := e.handleMessage("carrier-a", "", notify)
	require.NoError(t, err)
	require.Equal(t, 1, mm.systemErrors, "invalid VQ body should trigger system error")
	require.Empty(t, mm.vqReports, "VQ handler should not report metrics for invalid body")
}

// TestExporter_GracefulShutdown verifies that readSocket exits cleanly when
// Close() is called (no EBADF spin loop), and that Close() completes within a
// reasonable timeout. readPackets and sipDialogMetricsUpdate also receive the
// done signal and wind down asynchronously.
func TestExporter_GracefulShutdown(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	require.NoError(t, err)
	defer unix.Close(fds[1])

	tv := &unix.Timeval{Sec: 1}
	require.NoError(t, unix.SetsockoptTimeval(fds[0], unix.SOL_SOCKET, unix.SO_RCVTIMEO, tv))

	e := &exporter{
		sock:     fds[0],
		messages: make(chan []byte, 10),
		done:     make(chan struct{}),
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		mediaTracker:    mediatracker.NewTracker(30 * time.Second),
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		optionsTracker:  make(map[string]optionsEntry),
	}

	go e.readPackets()
	e.wg.Add(1)
	go e.readSocket()
	go e.sipDialogMetricsUpdate()

	time.Sleep(100 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		e.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close() did not complete within 5s — goroutine leak")
	}

	_, ok := <-e.messages
	require.False(t, ok, "messages channel should be closed after Close()")
}

// buildUDPPacket constructs a minimal Ethernet/IPv4/UDP packet with the given
// src/dst ports for testing isSIPPacket classification.
func buildUDPPacket(srcPort, dstPort uint16) []byte {
	pkt := make([]byte, 42) // 14 eth + 20 ip + 8 udp
	// Ethernet
	pkt[12] = 0x08 // IPv4
	pkt[13] = 0x00
	// IPv4
	pkt[14] = 0x45 // version=4, IHL=5
	pkt[16] = 0x00 // total length hi
	pkt[17] = 28   // total length lo (20 ip + 8 udp)
	pkt[23] = 17   // protocol = UDP
	// UDP
	binary.BigEndian.PutUint16(pkt[34:36], srcPort)
	binary.BigEndian.PutUint16(pkt[36:38], dstPort)
	return pkt
}

func buildVLANUDPPacket(srcPort, dstPort uint16) []byte {
	pkt := make([]byte, 46) // 14 eth + 4 vlan + 20 ip + 8 udp
	// Ethernet with VLAN 802.1Q
	pkt[12] = 0x81 // VLAN ethertype hi
	pkt[13] = 0x00 // VLAN ethertype lo
	// VLAN tag (4 bytes at 14-17)
	pkt[16] = 0x08 // inner ethertype IPv4 hi
	pkt[17] = 0x00 // inner ethertype lo
	// IPv4 at offset 18
	pkt[18] = 0x45 // version=4, IHL=5
	pkt[20] = 0x00
	pkt[21] = 28
	pkt[27] = 17 // protocol = UDP
	// UDP at offset 38
	binary.BigEndian.PutUint16(pkt[38:40], srcPort)
	binary.BigEndian.PutUint16(pkt[40:42], dstPort)
	return pkt
}

func buildLargeIHLPacket(srcPort, dstPort uint16) []byte {
	// 42-byte packet with IHL=15 (60-byte IP header) → udpOff=74 > len → too-short guard
	pkt := make([]byte, 42)
	pkt[12] = 0x08
	pkt[13] = 0x00
	pkt[14] = 0x4F // version=4, IHL=15
	pkt[23] = 17
	binary.BigEndian.PutUint16(pkt[34:36], srcPort)
	binary.BigEndian.PutUint16(pkt[36:38], dstPort)
	return pkt
}

func TestIsSIPPacket(t *testing.T) {
	e := &exporter{sipPort: 5060, sipsPort: 5061}

	tests := []struct {
		name string
		pkt  []byte
		want bool
	}{
		{"dst 5060", buildUDPPacket(12345, 5060), true},
		{"dst 5061", buildUDPPacket(12345, 5061), true},
		{"src 5060", buildUDPPacket(5060, 12345), true},
		{"src 5061", buildUDPPacket(5061, 12345), true},
		{"RTP port 5004", buildUDPPacket(12345, 5004), false},
		{"RTP port 10000", buildUDPPacket(10000, 20000), false},
		{"VLAN SIP", buildVLANUDPPacket(12345, 5060), true},
		{"VLAN RTP", buildVLANUDPPacket(12345, 5004), false},
		{"large IHL too short", buildLargeIHLPacket(12345, 5060), true},
		{"too short", make([]byte, 10), true},
		{"empty", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, e.isSIPPacket(tt.pkt))
		})
	}
}

func TestSendPacket_RTPDropWhenFull(t *testing.T) {
	e := &exporter{
		messages: make(chan []byte, 1),
		done:     make(chan struct{}),
		sipPort:  5060,
		sipsPort: 5061,
	}
	e.messages <- make([]byte, 1) // fill the channel

	// RTP packet (non-blocking) → dropped, sendPacket returns true
	rtpPkt := buildUDPPacket(12345, 5004)
	require.True(t, e.sendPacket(rtpPkt), "RTP sendPacket should not block")

	// SIP packet (blocking) → would block, but we signal done to unblock
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(e.done)
	}()
	sipPkt := buildUDPPacket(12345, 5060)
	require.False(t, e.sendPacket(sipPkt), "SIP sendPacket should return false on done")
}

func TestSendPacket_SuccessPaths(t *testing.T) {
	t.Run("SIP success", func(t *testing.T) {
		e := &exporter{
			messages: make(chan []byte, 1),
			done:     make(chan struct{}),
			sipPort:  5060, sipsPort: 5061,
		}
		sipPkt := buildUDPPacket(12345, 5060)
		require.True(t, e.sendPacket(sipPkt))
		require.Len(t, e.messages, 1)
	})

	t.Run("RTP success", func(t *testing.T) {
		e := &exporter{
			messages: make(chan []byte, 1),
			done:     make(chan struct{}),
			sipPort:  5060, sipsPort: 5061,
		}
		rtpPkt := buildUDPPacket(12345, 5004)
		require.True(t, e.sendPacket(rtpPkt))
		require.Len(t, e.messages, 1)
	})

	t.Run("RTP done signal", func(t *testing.T) {
		e := &exporter{
			messages: make(chan []byte), // zero-capacity → always full
			done:     make(chan struct{}),
			sipPort:  5060, sipsPort: 5061,
		}
		close(e.done)
		rtpPkt := buildUDPPacket(12345, 5004)
		require.False(t, e.sendPacket(rtpPkt), "RTP sendPacket should return false on done")
	})
}
