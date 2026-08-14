package exporter

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/sys/unix"

	"github.com/aibudaevv/sip-exporter/internal/carriers"
	"github.com/aibudaevv/sip-exporter/internal/dto"
	"github.com/aibudaevv/sip-exporter/internal/geoip"
	"github.com/aibudaevv/sip-exporter/internal/mediatracker"
	"github.com/aibudaevv/sip-exporter/internal/sdp"
	"github.com/aibudaevv/sip-exporter/internal/service"
	"github.com/aibudaevv/sip-exporter/internal/vq"
)

type fakeRTPEndpointMap struct {
	updates     []rtpEndpointMapUpdate
	deletes     []rtpEndpointKey
	present     map[rtpEndpointKey]bool
	updateError error
	deleteError error
}

type rtpEndpointMapUpdate struct {
	key   rtpEndpointKey
	value uint8
	flags ebpf.MapUpdateFlags
}

var _ rtpEndpointMap = (*fakeRTPEndpointMap)(nil)

func (m *fakeRTPEndpointMap) Update(key, value any, flags ebpf.MapUpdateFlags) error {
	if m.updateError != nil {
		return m.updateError
	}
	m.updates = append(m.updates, rtpEndpointMapUpdate{
		key:   key.(rtpEndpointKey),
		value: value.(uint8),
		flags: flags,
	})
	if m.present == nil {
		m.present = make(map[rtpEndpointKey]bool)
	}
	m.present[key.(rtpEndpointKey)] = true
	return nil
}

func (m *fakeRTPEndpointMap) Delete(key any) error {
	if m.deleteError != nil {
		return m.deleteError
	}
	m.deletes = append(m.deletes, key.(rtpEndpointKey))
	if !m.present[key.(rtpEndpointKey)] {
		return ebpf.ErrKeyNotExist
	}
	delete(m.present, key.(rtpEndpointKey))
	return nil
}

// Mock services for testing.
type mockMetricser struct {
	requestCalled                  []byte
	requestCount                   int
	reinviteCalled                 bool
	sipRetransmissionCalls         int
	sipRetransmissionMethod        string
	responseCalled                 []byte
	responseIsInvite               bool
	sessionUpdated                 int
	systemErrorCalled              bool
	packetsIncremented             int
	invite200OKCalled              bool
	sessionCompletedFlag           bool
	rrdUpdated                     bool
	rrdDelay                       float64
	responseWithMetricsCalled      bool
	spdUpdated                     bool
	spdDuration                    time.Duration
	ttrUpdated                     bool
	ttrDelay                       float64
	pddUpdated                     bool
	pddDelay                       float64
	ordUpdated                     bool
	ordDelay                       float64
	lrdUpdated                     bool
	lrdDelay                       float64
	pbdUpdated                     bool
	pbdDelay                       float64
	shortCallsUpdated              bool
	shortCallsDuration             time.Duration
	billableCalled                 bool
	billableCarrier                string
	billableDestCountry            string
	billableDuration               time.Duration
	registerSuccessCalls           int
	registerFailureCodes           []string
	registerCountryChange          []string
	registerScanCalls              int
	inviteBurstCalls               int
	fasCalls                       int
	fasCallsLabels                 []carrierCall
	vqReportCalled                 bool
	vqCarrier                      string
	vqUAType                       string
	vqReport                       *vq.SessionReport
	rtpPacketsCalls                int
	rtpLossCalls                   int
	rtpLossValue                   uint64
	rtpDuplicateCalls              int
	rtpOutOfOrderCalls             int
	rtpDroppedCount                int
	oneWayCalls                    int
	missingRTPCalls                int
	parseErrorCalls                int
	parseErrorType                 string
	rtcpJitterCalls                int
	rtcpJitterVal                  float64
	rtcpLossFracCalls              int
	rtcpLossFracVal                float64
	rtcpCumLossCalls               int
	rtcpCumLossVal                 uint64
	rtcpRTTCalls                   int
	rtcpRTTVal                     float64
	rtcpReportCalls                int
	rtcpReportType                 string
	rtcpReportCarrier              string
	rtcpReportDirection            string
	rtcpOrphanCalls                int
	rtpKernelTimestampMissingCalls int
	rtpAliasLearnedCalls           int
	rtpAliasReleasedCalls          int
	rtpAliasCarrier                string
	rtpAliasDirection              string
	rtpAliasMismatchType           string
}

func (m *mockMetricser) UpdateSessions(_ []service.LabeledCount) {}

func (m *mockMetricser) SetSessionsLimits(_ map[string]int) {}

func (m *mockMetricser) UpdateActiveRegistrations(_ []service.LabeledCount) {}

func (m *mockMetricser) Request(_, _, _, _, _, _, _, _ string, in []byte) {
	m.requestCalled = in
	m.requestCount++
	m.packetsIncremented++
}

func (m *mockMetricser) Reinvite(_, _, _, _ string) {
	m.reinviteCalled = true
	m.packetsIncremented++
}

func (m *mockMetricser) SIPRetransmission(_, _, _, _, method string) {
	m.sipRetransmissionCalls++
	m.sipRetransmissionMethod = method
}

func (m *mockMetricser) UpdateShortCalls(_, _, _, _ string, duration time.Duration) {
	m.shortCallsUpdated = true
	m.shortCallsDuration = duration
}

func (m *mockMetricser) UpdateBillableSeconds(carrier, destCountry, _ string, duration time.Duration) {
	m.billableCalled = true
	m.billableCarrier = carrier
	m.billableDestCountry = destCountry
	m.billableDuration = duration
}

func (m *mockMetricser) Response(_, _, _, _ string, in []byte, isInviteResponse bool) {
	m.responseCalled = in
	m.responseIsInvite = isInviteResponse
	m.packetsIncremented++
}

func (m *mockMetricser) ResponseWithMetrics(_, _, _, _ string, status []byte, isInviteResponse, is200OK bool) {
	m.responseWithMetricsCalled = true
	m.responseCalled = status
	m.responseIsInvite = isInviteResponse
	m.packetsIncremented++
	if is200OK && isInviteResponse {
		m.invite200OKCalled = true
	}
}

func (m *mockMetricser) Invite200OK(_, _, _, _, _, _, _, _ string) {
	m.invite200OKCalled = true
}

func (m *mockMetricser) SessionCompleted(_, _, _, _ string) {
	m.sessionCompletedFlag = true
}

func (m *mockMetricser) RegisterSuccess(_, _, _, _ string) {
	m.registerSuccessCalls++
}

func (m *mockMetricser) RegisterFailure(_, _, _, _ string, code string) {
	m.registerFailureCodes = append(m.registerFailureCodes, code)
}

func (m *mockMetricser) RegisterCountryChange(_, sourceCountry, _ string) {
	m.registerCountryChange = append(m.registerCountryChange, sourceCountry)
}

func (m *mockMetricser) RegisterScan(_, _, _ string) {
	m.registerScanCalls++
}

func (m *mockMetricser) InviteBurst(_, _, _ string) {
	m.inviteBurstCalls++
}

func (m *mockMetricser) FasCall(carrier, uaType, sourceCountry, direction string) {
	m.fasCalls++
	m.fasCallsLabels = append(m.fasCallsLabels, carrierCall{
		carrier: carrier, uaType: uaType, sourceCountry: sourceCountry, direction: direction,
	})
}

func (m *mockMetricser) UpdateRRD(_, _, _, _ string, delayMs float64) {
	m.rrdUpdated = true
	m.rrdDelay = delayMs
}

func (m *mockMetricser) UpdateSPD(_, _, _, _ string, duration time.Duration) {
	m.spdUpdated = true
	m.spdDuration = duration
}

func (m *mockMetricser) UpdateSession(_, _, _, _ string, size int) {
	m.sessionUpdated = size
}

func (m *mockMetricser) UpdateTTR(_, _, _, _ string, delayMs float64) {
	m.ttrUpdated = true
	m.ttrDelay = delayMs
}

func (m *mockMetricser) UpdatePDD(_, _, _, _ string, delayMs float64) {
	m.pddUpdated = true
	m.pddDelay = delayMs
}

func (m *mockMetricser) UpdateORD(_, _, _, _ string, delayMs float64) {
	m.ordUpdated = true
	m.ordDelay = delayMs
}

func (m *mockMetricser) UpdateLRD(_, _, _, _ string, delayMs float64) {
	m.lrdUpdated = true
	m.lrdDelay = delayMs
}

func (m *mockMetricser) UpdatePBD(_, _, _, _ string, delayMs float64) {
	m.pbdUpdated = true
	m.pbdDelay = delayMs
}

func (m *mockMetricser) SystemError() {
	m.systemErrorCalled = true
}

func (m *mockMetricser) ParseError(errorType string) {
	m.parseErrorCalls++
	m.parseErrorType = errorType
}
func (m *mockMetricser) SocketStats(_ []service.SocketStat) {}
func (m *mockMetricser) RTPDropped()                        { m.rtpDroppedCount++ }
func (m *mockMetricser) UpdateChannelLength(int)            {}
func (m *mockMetricser) UpdateChannelCapacity(int)          {}
func (m *mockMetricser) UpdateTrackerSize(string, int)      {}
func (m *mockMetricser) UpdateActiveDialogs(int)            {}

func (m *mockMetricser) UpdateVQReport(carrier string, uaType string, _, _ string, report *vq.SessionReport) {
	m.vqReportCalled = true
	m.vqCarrier = carrier
	m.vqUAType = uaType
	m.vqReport = report
}

func (m *mockMetricser) UpdateRTPPackets(_, _, _, _, _ string) {
	m.rtpPacketsCalls++
}
func (m *mockMetricser) UpdateRTPLoss(_, _, _, _, _ string, lost uint64) {
	m.rtpLossCalls++
	m.rtpLossValue = lost
}
func (m *mockMetricser) UpdateRTPDuplicates(_, _, _, _, _ string) {
	m.rtpDuplicateCalls++
}
func (m *mockMetricser) UpdateRTPOutOfOrder(_, _, _, _, _ string) {
	m.rtpOutOfOrderCalls++
}
func (m *mockMetricser) UpdateRTPJitter(string, string, string, string, string, float64) {}
func (m *mockMetricser) UpdateRTPPDV(string, string, string, string, string, float64)    {}
func (m *mockMetricser) UpdateRTPMOS(string, string, string, string, string, float64)    {}
func (m *mockMetricser) UpdateRTPMOSVariants(string, string, string, string, string, float64, float64, float64) {
}
func (m *mockMetricser) UpdateRTPRFactor(string, string, string, string, string, float64) {}
func (m *mockMetricser) UpdateRTPLossDistribution(string, string, string, string, string, float64, float64) {
}
func (m *mockMetricser) UpdateRTPActiveStreams(_ []service.LabeledCount) {}
func (m *mockMetricser) RTPAliasLearned(carrier, direction, mismatchType string) {
	m.rtpAliasLearnedCalls++
	m.rtpAliasCarrier = carrier
	m.rtpAliasDirection = direction
	m.rtpAliasMismatchType = mismatchType
}
func (m *mockMetricser) RTPAliasReleased(carrier, direction string) {
	m.rtpAliasReleasedCalls++
	m.rtpAliasCarrier = carrier
	m.rtpAliasDirection = direction
}
func (m *mockMetricser) OneWayCall(string, string, string, string) { m.oneWayCalls++ }
func (m *mockMetricser) MissingRTP(string, string, string, string) { m.missingRTPCalls++ }

func (m *mockMetricser) UpdateRTCPJitter(_, _, _, _, _ string, jitterMs float64) {
	m.rtcpJitterCalls++
	m.rtcpJitterVal = jitterMs
}
func (m *mockMetricser) UpdateRTCPLossFraction(_, _, _, _, _ string, fractionPercent float64) {
	m.rtcpLossFracCalls++
	m.rtcpLossFracVal = fractionPercent
}
func (m *mockMetricser) UpdateRTCPCumulativeLoss(_, _, _, _, _ string, lostDelta uint64) {
	m.rtcpCumLossCalls++
	m.rtcpCumLossVal = lostDelta
}
func (m *mockMetricser) UpdateRTCPRTT(_, _, _, _, _ string, rttMs float64) {
	m.rtcpRTTCalls++
	m.rtcpRTTVal = rttMs
}
func (m *mockMetricser) UpdateRTCPReport(carrier, _, _, direction, reportType string) {
	m.rtcpReportCalls++
	m.rtcpReportType = reportType
	m.rtcpReportCarrier = carrier
	m.rtcpReportDirection = direction
}
func (m *mockMetricser) UpdateRTCPOrphan() {
	m.rtcpOrphanCalls++
}
func (m *mockMetricser) RTPKernelTimestampMissing() {
	m.rtpKernelTimestampMissingCalls++
}

type dialogCreateArgs struct {
	expiresAt          time.Time
	createdAt          time.Time
	carrier            string
	uaType             string
	destinationCountry string
}

type mockDialoger struct {
	created        map[string]dialogCreateArgs
	deleted        []string
	cleanupResults []service.CleanupResult
}

func (m *mockDialoger) Create(p service.DialogParams) {
	if m.created == nil {
		m.created = make(map[string]dialogCreateArgs)
	}
	m.created[p.DialogID] = dialogCreateArgs{
		expiresAt:          p.ExpiresAt,
		createdAt:          p.CreatedAt,
		carrier:            p.Carrier,
		uaType:             p.UAType,
		destinationCountry: p.DestinationCountry,
	}
}

func (m *mockDialoger) Delete(dialogID string) service.CleanupResult {
	m.deleted = append(m.deleted, dialogID)
	if m.created != nil {
		if args, ok := m.created[dialogID]; ok {
			delete(m.created, dialogID)
			return service.CleanupResult{
				Duration: 100 * time.Millisecond,
				Carrier:  args.carrier, DestinationCountry: args.destinationCountry,
			}
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

func createActiveTestDialog(t *testing.T, dialoger service.Dialoger) {
	t.Helper()
	const callID = "call-1"
	dialogID, err := normalizeDialogID([]byte(callID), []byte("from-tag"), []byte("to-tag"))
	require.NoError(t, err)
	dialoger.Create(service.DialogParams{DialogID: dialogID})
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

func TestNormalizeDialogIDEmptyFromTag(t *testing.T) {
	_, err := normalizeDialogID([]byte("call-id"), []byte(""), []byte("to-tag"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "from tag or to tag is empty")
}

func TestNormalizeDialogIDEmptyToTag(t *testing.T) {
	_, err := normalizeDialogID([]byte("call-id"), []byte("from-tag"), []byte(""))
	require.Error(t, err)
	require.Contains(t, err.Error(), "from tag or to tag is empty")
}

func TestNormalizeDialogIDBothEmptyTags(t *testing.T) {
	_, err := normalizeDialogID([]byte("call-id"), []byte(""), []byte(""))
	require.Error(t, err)
	require.Contains(t, err.Error(), "from tag or to tag is empty")
}

func TestNormalizeDialogIDFromTagLessThanToTag(t *testing.T) {
	result, err := normalizeDialogID([]byte("test-call"), []byte("aaa"), []byte("zzz"))
	require.NoError(t, err)
	require.Equal(t, "test-call:aaa:zzz", result)
}

func TestNormalizeDialogIDFromTagGreaterThanToTag(t *testing.T) {
	result, err := normalizeDialogID([]byte("test-call"), []byte("zzz"), []byte("aaa"))
	require.NoError(t, err)
	require.Equal(t, "test-call:aaa:zzz", result)
}

func TestNormalizeDialogIDEqualTags(t *testing.T) {
	result, err := normalizeDialogID([]byte("test-call"), []byte("same"), []byte("same"))
	require.NoError(t, err)
	require.Equal(t, "test-call:same:same", result)
}

// ==================== splitHeader tests ====================

func TestSplitHeaderNormal(t *testing.T) {
	header, value := splitHeader([]byte("Content-Type: application/sdp"))
	require.Equal(t, []byte("Content-Type"), header)
	require.Equal(t, []byte("application/sdp"), value)
}

func TestSplitHeaderNoColon(t *testing.T) {
	header, value := splitHeader([]byte("NoColonHere"))
	require.Nil(t, header)
	require.Nil(t, value)
}

func TestSplitHeaderEmptyValue(t *testing.T) {
	header, value := splitHeader([]byte("Header:"))
	require.Equal(t, []byte("Header"), header)
	require.Empty(t, value)
}

func TestSplitHeaderEmptyLine(t *testing.T) {
	header, value := splitHeader([]byte(""))
	require.Nil(t, header)
	require.Nil(t, value)
}

func TestSplitHeaderOnlyColon(t *testing.T) {
	header, value := splitHeader([]byte(":"))
	require.Empty(t, header)
	require.Empty(t, value)
}

func TestSplitHeaderMultipleColons(t *testing.T) {
	header, value := splitHeader([]byte("Header: value: with: colons"))
	require.Equal(t, []byte("Header"), header)
	require.Equal(t, []byte("value: with: colons"), value)
}

func TestSplitHeaderWithSpaces(t *testing.T) {
	header, value := splitHeader([]byte("  Header  :  Value  "))
	require.Equal(t, []byte("Header"), header)
	require.Equal(t, []byte("Value"), value)
}

// ==================== extractTag tests ====================

func TestExtractTagNormal(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>;tag=abc123"))
	require.Equal(t, []byte("abc123"), tag)
}

func TestExtractTagNoTag(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>"))
	require.Nil(t, tag)
}

func TestExtractTagTagWithSemicolon(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>;tag=abc123;other=param"))
	require.Equal(t, []byte("abc123"), tag)
}

func TestExtractTagTagWithSpace(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>;tag=abc123 other"))
	require.Equal(t, []byte("abc123"), tag)
}

func TestExtractTagTagWithGreaterThan(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>;tag=abc123>"))
	require.Equal(t, []byte("abc123"), tag)
}

func TestExtractTagTagWithNewline(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>;tag=abc123\r\n"))
	require.Equal(t, []byte("abc123"), tag)
}

func TestExtractTagEmptyTag(t *testing.T) {
	tag := extractTag([]byte("<sip:user@domain>;tag="))
	require.Equal(t, []byte(""), tag)
}

func TestExtractTagOnlyTagMarker(t *testing.T) {
	tag := extractTag([]byte(";tag="))
	require.Equal(t, []byte(""), tag)
}

// ==================== extractCSeq tests ====================

func TestExtractCSeqNormal(t *testing.T) {
	id, method := extractCSeq([]byte("12345 INVITE"))
	require.Equal(t, []byte("12345"), id)
	require.Equal(t, []byte("INVITE"), method)
}

func TestExtractCSeqNoSpace(t *testing.T) {
	id, method := extractCSeq([]byte("12345"))
	require.Nil(t, id)
	require.Nil(t, method)
}

func TestExtractCSeqEmpty(t *testing.T) {
	id, method := extractCSeq([]byte(""))
	require.Nil(t, id)
	require.Nil(t, method)
}

func TestExtractCSeqMultipleSpaces(t *testing.T) {
	id, method := extractCSeq([]byte("12345 INVITE extra"))
	require.Equal(t, []byte("12345"), id)
	require.Equal(t, []byte("INVITE"), method)
}

func TestInviteSDPCSeqRetransmitReplacesSameTransaction(t *testing.T) {
	e := &exporter{inviteSDP: make(map[inviteSDPKey]inviteSDPEntity)}

	e.storeInviteSDP("call-1", "2", []byte("first offer"))
	e.storeInviteSDP("call-1", "2", []byte("replacement offer"))

	require.Len(t, e.inviteSDP, 1)
	body, ok := e.takeInviteSDP("call-1", "2")
	require.True(t, ok)
	require.Equal(t, []byte("replacement offer"), body)
}

func TestStoreInviteSDPOwnsPacketBuffer(t *testing.T) {
	e := &exporter{inviteSDP: make(map[inviteSDPKey]inviteSDPEntity)}
	raw := rawPacket{data: []byte("SIP body: v=0\r\nm=audio 4000 RTP/AVP 8\r\n")}
	body := raw.data[len("SIP body: "):]

	e.storeInviteSDP("call-1", "2", body)
	copy(raw.data[len("SIP body: "):], []byte("x=0\r\nm=audio 9999 RTP/AVP 8\r\n"))

	got, ok := e.takeInviteSDP("call-1", "2")
	require.True(t, ok)
	require.Equal(t, []byte("v=0\r\nm=audio 4000 RTP/AVP 8\r\n"), got)
}

func TestCleanupInviteTransactionsRemovesExpiredFinal(t *testing.T) {
	key := inviteSDPKey{callID: "call-1", cseqID: "2"}
	e := &exporter{inviteTransactions: map[inviteSDPKey]inviteTransaction{
		key: {final: true, timestamp: time.Now().Add(-defaultInviteTTL - time.Second)},
	}}

	e.cleanupInviteTransactions()

	_, ok := e.inviteTransactions[key]
	require.False(t, ok)
}

func TestClosedInviteTransactionRejectsRetransmittedRequest(t *testing.T) {
	e := &exporter{services: services{dialoger: &mockDialoger{}}}
	response := dto.Packet{
		CallID:         []byte("call-1"),
		From:           dto.From{Tag: []byte("from-tag")},
		CSeq:           dto.CSeq{ID: []byte("1"), Method: []byte("INVITE")},
		ResponseStatus: []byte("200"),
	}

	e.observeInviteTransaction("call-1", "1", false, "from-tag")
	_, _, process := e.classifyInviteResponse(response)
	require.True(t, process)
	e.closeInviteTransactions("call-1")

	e.observeInviteTransaction("call-1", "1", false, "from-tag")
	isInvite, isReinvite, process := e.classifyInviteResponse(response)
	require.False(t, isInvite)
	require.False(t, isReinvite)
	require.False(t, process)
}

func TestClosedInviteReplayDoesNotCreateSecondDialog(t *testing.T) {
	metricser := &mockMetricser{}
	dialoger := &mockDialoger{}
	e := &exporter{
		services:           services{metricser: metricser, dialoger: dialoger},
		inviteTracker:      make(map[string]inviteEntry),
		inviteSDP:          make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:       mediatracker.NewTracker(rtpStreamTTL),
		inviteBurstTracker: newInviteBurstTracker(3, time.Minute),
	}

	require.NoError(t, e.handleMessage("carrier", "", makeInvite("call-1", "from-tag")))
	require.NoError(t, e.handleMessage("carrier", "", makeInvite200OK("call-1", "from-tag", "to-tag")))
	require.Len(t, dialoger.created, 1)
	require.NoError(t, e.handleMessage("carrier", "", makeBye200OK("call-1", "from-tag", "to-tag")))
	require.Empty(t, dialoger.created)

	require.NoError(t, e.handleMessage("carrier", "", makeInvite("call-1", "from-tag")))
	require.NoError(t, e.handleMessage("carrier", "", makeInvite200OK("call-1", "from-tag", "to-tag")))
	require.Empty(t, dialoger.created)
}

func TestClosedInviteTransactionGenerations(t *testing.T) {
	newResponse := func(cseqID, fromTag string) dto.Packet {
		return dto.Packet{CallID: []byte("call-1"), From: dto.From{Tag: []byte(fromTag)},
			CSeq: dto.CSeq{ID: []byte(cseqID), Method: []byte("INVITE")}, ResponseStatus: []byte("200")}
	}
	tests := []struct {
		name   string
		setup  func(*exporter)
		packet dto.Packet
		want   bool
	}{
		{name: "new from tag", setup: func(e *exporter) {
			e.closeInviteTransactions("call-1")
			e.observeInviteTransaction("call-1", "1", false, "new")
		}, packet: newResponse("1", "new"), want: true},
		{name: "new cseq", setup: func(e *exporter) {
			e.closeInviteTransactions("call-1")
			e.observeInviteTransaction("call-1", "2", false, "old")
		}, packet: newResponse("2", "old"), want: true},
		{
			name:   "unknown closed transaction",
			setup:  func(e *exporter) { e.closeInviteTransactions("call-1") },
			packet: newResponse("1", "old"),
		},
		{name: "startup mid call", setup: func(*exporter) {}, packet: newResponse("1", "old"), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &exporter{services: services{dialoger: &mockDialoger{}}}
			tt.setup(e)
			_, _, process := e.classifyInviteResponse(tt.packet)
			require.Equal(t, tt.want, process)
		})
	}
}

func TestInviteSDPCSeqSeparatesFromTags(t *testing.T) {
	e := &exporter{inviteSDP: make(map[inviteSDPKey]inviteSDPEntity)}

	e.storeInviteSDP("call-1", "2", []byte("first offer"), "from-a")
	e.storeInviteSDP("call-1", "2", []byte("second offer"), "from-b")

	first, ok := e.takeInviteSDP("call-1", "2", "from-a")
	require.True(t, ok)
	require.Equal(t, []byte("first offer"), first)
	second, ok := e.takeInviteSDP("call-1", "2", "from-b")
	require.True(t, ok)
	require.Equal(t, []byte("second offer"), second)
}

func TestInviteSDPCSeqResponseRemovesOnlyMatchingTransaction(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		wantCSeq2 bool
		wantCSeq3 bool
	}{
		{
			name:      "provisional response preserves both offers",
			status:    "180 Ringing",
			wantCSeq2: true,
			wantCSeq3: true,
		},
		{
			name:      "final rejection removes only matching offer",
			status:    "488 Not Acceptable Here",
			wantCSeq2: false,
			wantCSeq3: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSDPCSeqTestExporter()
			e.storeInviteSDP("call-1", "2", []byte("offer 2"), "from-tag")
			e.storeInviteSDP("call-1", "3", []byte("offer 3"), "from-tag")

			require.NoError(t, e.handleMessage("carrier-x", "", inviteResponse(tt.status, "call-1", "2")))

			_, gotCSeq2 := e.takeInviteSDP("call-1", "2", "from-tag")
			_, gotCSeq3 := e.takeInviteSDP("call-1", "3", "from-tag")
			require.Equal(t, tt.wantCSeq2, gotCSeq2)
			require.Equal(t, tt.wantCSeq3, gotCSeq3)
		})
	}
}

// ==================== extractSessionExpires tests ====================

func TestExtractSessionExpiresOnlyNumber(t *testing.T) {
	expires := extractSessionExpires([]byte("1800"))
	require.Equal(t, 1800, expires)
}

func TestExtractSessionExpiresWithRefresher(t *testing.T) {
	expires := extractSessionExpires([]byte("1800;refresher=uac"))
	require.Equal(t, 1800, expires)
}

func TestExtractSessionExpiresEmpty(t *testing.T) {
	expires := extractSessionExpires([]byte(""))
	require.Equal(t, 0, expires)
}

func TestExtractSessionExpiresInvalidNumber(t *testing.T) {
	expires := extractSessionExpires([]byte("invalid"))
	require.Equal(t, 0, expires)
}

func TestExtractSessionExpiresZero(t *testing.T) {
	expires := extractSessionExpires([]byte("0"))
	require.Equal(t, 0, expires)
}

// ==================== extractExpires tests ====================

func TestExtractExpiresNormalValue(t *testing.T) {
	expires := extractExpires([]byte("3600"))
	require.Equal(t, 3600, expires)
}

func TestExtractExpiresZero(t *testing.T) {
	expires := extractExpires([]byte("0"))
	require.Equal(t, 0, expires)
}

func TestExtractExpiresEmpty(t *testing.T) {
	expires := extractExpires([]byte(""))
	require.Equal(t, 0, expires)
}

func TestExtractExpiresInvalidNumber(t *testing.T) {
	expires := extractExpires([]byte("invalid"))
	require.Equal(t, 0, expires)
}

// ==================== parseRawPacket tests ====================

func TestParseRawPacketTooShort(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	_, err := e.parseRawPacket([]byte("short"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "wrong len packet")
}

func TestParseRawPacketNotIPv4(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestParseRawPacketNotUDP(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestParseRawPacketNoSIPPayload(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestParseRawPacketNotSIPMethod(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestParseRawPacketVLANTagged(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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
	copy(packet[46:], []byte("INVITE sip:test SIP/2.0\r\nCall-ID: test\r\n"))

	_, err := e.parseRawPacket(packet)
	require.NoError(t, err)
}

func TestParseRawPacketIPHeaderTooShort(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestParseRawPacketUDPHeaderTooShort(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestParseRawPacketSIPPayloadTooSmall(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestParseRawPacketErrorTypes(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestParseRawPacketSuccessReturnsEmptyType(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	packet := make([]byte, 100)
	packet[12] = 0x08
	packet[13] = 0x00
	packet[14] = 0x45
	packet[23] = 17
	copy(packet[42:], []byte("INVITE sip:test SIP/2.0\r\nCall-ID: test\r\n"))

	errType, err := e.parseRawPacket(packet)
	require.NoError(t, err)
	require.Empty(t, errType, "successful parse should return empty error type")
}

// ==================== sipPacketParse tests ====================

func TestSIPPacketParseEmptyInput(t *testing.T) {
	e := exporter{}

	_, err := e.sipPacketParse([]byte(""))
	require.Error(t, err)
	require.Contains(t, err.Error(), "malformed request line")
}

func TestSIPPacketParseGarbageInput(t *testing.T) {
	e := exporter{}

	_, err := e.sipPacketParse([]byte("GARBAGE\r\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "malformed request line")
}

func TestSIPPacketParseNoFromTag(t *testing.T) {
	e := exporter{}

	input := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 INVITE\r\n")

	_, err := e.sipPacketParse(input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to extract tag from")
	require.Contains(t, err.Error(), "<sip:user@domain>")
}

func TestSIPPacketParseMissingCallID(t *testing.T) {
	e := exporter{}

	input := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"CSeq: 1 INVITE\r\n")

	_, err := e.sipPacketParse(input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing Call-ID")
}

func TestSIPPacketParseWithSessionExpires(t *testing.T) {
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

func TestSIPPacketParseInvalidSessionExpires(t *testing.T) {
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

func TestSIPPacketParseWithExpires(t *testing.T) {
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

func TestSIPPacketParseExpiresAbsenceIsZero(t *testing.T) {
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

func TestSIPPacketParseExpiresZero(t *testing.T) {
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

func TestSIPPacketParseNoCSeqMethod(t *testing.T) {
	e := exporter{}

	input := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1\r\n")

	_, err := e.sipPacketParse(input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to extract CSeq from")
}

func TestSIPPacketParseCSeqMultipleSpaces(t *testing.T) {
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

func TestSIPPacketParseCaseInsensitiveHeaders(t *testing.T) {
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

func TestSIPPacketParseCompactHeaders(t *testing.T) {
	e := exporter{}

	tests := []struct {
		name string
		from string
		to   string
		cid  string
	}{
		{name: "compact f/t/i", from: "f", to: "t", cid: "i"},
		{name: "uppercase F/T/I", from: "F", to: "T", cid: "I"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte("SIP/2.0 200 OK\r\n" +
				tt.from + ": <sip:user@domain>;tag=abc\r\n" +
				tt.to + ": <sip:other@domain>;tag=xyz\r\n" +
				tt.cid + ": test\r\n" +
				"cSeq: 1 INVITE\r\n")

			p, err := e.sipPacketParse(input)
			require.NoError(t, err)
			require.Equal(t, []byte("abc"), p.From.Tag)
			require.Equal(t, []byte("xyz"), p.To.Tag)
			require.Equal(t, []byte("test"), p.CallID)
		})
	}
}

func TestSIPPacketParseFoldedHeader(t *testing.T) {
	e := exporter{}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;\r\n" +
		" tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 INVITE\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.Equal(t, []byte("abc"), p.From.Tag)
	require.Equal(t, []byte("xyz"), p.To.Tag)
}

func TestSIPPacketParseCaseInsensitiveTag(t *testing.T) {
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

func TestExtractTagQuotedDisplayName(t *testing.T) {
	tests := []struct {
		name  string
		value []byte
		want  []byte
	}{
		{name: "tag inside quoted name", value: []byte(`"Joe;tag=evil" <sip:joe@h>;tag=real`), want: []byte("real")},
		{name: "tag after angle brackets", value: []byte("<sip:joe@h>;tag=real"), want: []byte("real")},
		{name: "no tag", value: []byte("<sip:joe@h>"), want: nil},
		{name: "bare URI with tag", value: []byte("sip:joe@h;tag=real"), want: []byte("real")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, extractTag(tt.value))
		})
	}
}

func TestSIPPacketParseTruncatedStatusLine(t *testing.T) {
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

func TestHandleMessageRequest(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessageINVITERetransmissionDedup(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	invite := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: retrans-call\r\n" +
		"CSeq: 1 INVITE\r\n")

	// First INVITE — counted
	err := e.handleMessage("carrier", "", invite)
	require.NoError(t, err)
	require.Equal(t, 1, mm.requestCount)

	// Tracker entry exists
	e.inviteMutex.RLock()
	_, exists := e.inviteTracker["retrans-call"]
	e.inviteMutex.RUnlock()
	require.True(t, exists)

	// Retransmission #1 — must NOT increment
	err = e.handleMessage("carrier", "", invite)
	require.NoError(t, err)
	require.Equal(t, 1, mm.requestCount, "first retransmission must not call Request()")

	// Retransmission #2 — must NOT increment
	err = e.handleMessage("carrier", "", invite)
	require.NoError(t, err)
	require.Equal(t, 1, mm.requestCount, "second retransmission must not call Request()")

	// Different Call-ID — new call, must be counted
	invite2 := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=def\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: new-call\r\n" +
		"CSeq: 1 INVITE\r\n")
	err = e.handleMessage("carrier", "", invite2)
	require.NoError(t, err)
	require.Equal(t, 2, mm.requestCount, "different Call-ID must be counted as new INVITE")
}

func TestHandleMessageResponse200INVITE(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessageReINVITECountedAsReinvite(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	md.Create(service.DialogParams{
		DialogID:  "test-call:abc:xyz",
		ExpiresAt: time.Now().Add(1 * time.Hour),
		CreatedAt: time.Now(),
		CallID:    "test-call",
	})

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

func TestHandleMessageInitialINVITECountedAsInvite(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessageReINVITE200OKDoesNotInflateMetrics(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	md.Create(service.DialogParams{
		DialogID:      "test-call:abc:xyz",
		ExpiresAt:     time.Now().Add(1 * time.Hour),
		CreatedAt:     time.Now(),
		Carrier:       "carrier-a",
		UAType:        "yealink",
		SourceCountry: "RU",
		CallID:        "test-call",
	})

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

func TestHandleMessageResponse200BYE(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessageResponse200REGISTER(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessageRRDFullCycle(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessageRegister200OKCallsRegisterSuccess(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	require.NoError(t, e.handleMessage("c", "US", makeRegister("ok1", "ft")))
	require.NoError(t, e.handleMessage("c", "US", makeRegister200OK("ok1", "ft", "tt")))

	require.Eventually(t, func() bool { return mm.registerSuccessCalls == 1 },
		100*time.Millisecond, 10*time.Millisecond)
	require.Empty(t, mm.registerFailureCodes)
}

func TestHandleMessageRegister403CallsRegisterFailure(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	require.NoError(t, e.handleMessage("c", "US", makeRegister("f403", "ft")))
	require.NoError(t, e.handleMessage("c", "US", makeRegisterStatus("403 Forbidden", "f403", "ft", "tt")))

	require.Eventually(t, func() bool { return len(mm.registerFailureCodes) == 1 },
		100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, []string{"403"}, mm.registerFailureCodes)
	require.Zero(t, mm.registerSuccessCalls)
}

// Challenge 401 is recorded in failure_total{code} (for brute-force detection)
// even though it does not affect register_success_ratio.
func TestHandleMessageRegister401ChallengeCallsRegisterFailure(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	require.NoError(t, e.handleMessage("c", "US", makeRegister("ch1", "ft")))
	require.NoError(t, e.handleMessage("c", "US", makeRegisterStatus("401 Unauthorized", "ch1", "ft", "tt")))

	require.Eventually(t, func() bool { return len(mm.registerFailureCodes) == 1 },
		100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, []string{"401"}, mm.registerFailureCodes)
}

// 1xx provisional response must NOT be counted as a registration failure.
func TestHandleMessageRegister100TryingNotAFailure(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	require.NoError(t, e.handleMessage("c", "US", makeRegister("t100", "ft")))
	require.NoError(t, e.handleMessage("c", "US", makeRegisterStatus("100 Trying", "t100", "ft", "tt")))

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
		inviteSDP:             make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:          mediatracker.NewTracker(rtpStreamTTL),
	}
}

func TestRegisterExpiryTrackerNewRegistration(t *testing.T) {
	e := newExporterWithRegTracker()

	e.storeRegistration("sip:user1@example.com", "c", "sip", "US", "", "", 3600)

	counts := e.registrationCounts()
	require.Len(t, counts, 1)
	require.Equal(t, 1, counts[0].Count)
}

func TestRegisterExpiryTrackerRefreshNoDoubleCount(t *testing.T) {
	e := newExporterWithRegTracker()
	aor := "sip:user1@example.com"

	e.storeRegistration(aor, "c", "sip", "US", "", "", 3600)
	e.storeRegistration(aor, "c", "sip", "US", "", "", 3600)
	e.storeRegistration(aor, "c", "sip", "US", "", "", 3600)

	counts := e.registrationCounts()
	require.Len(t, counts, 1)
	require.Equal(t, 1, counts[0].Count, "refresh of same AOR must not double-count")
}

func TestRegisterExpiryTrackerDifferentAORs(t *testing.T) {
	e := newExporterWithRegTracker()

	e.storeRegistration("sip:user1@example.com", "c", "sip", "US", "", "", 3600)
	e.storeRegistration("sip:user2@example.com", "c", "sip", "US", "", "", 3600)

	counts := e.registrationCounts()
	require.Len(t, counts, 1)
	require.Equal(t, 2, counts[0].Count)
}

func TestRegisterExpiryTrackerGroupsByLabels(t *testing.T) {
	e := newExporterWithRegTracker()

	e.storeRegistration("sip:u1@a", "carrier-A", "sip", "US", "", "", 3600)
	e.storeRegistration("sip:u2@a", "carrier-A", "sip", "US", "", "", 3600)
	e.storeRegistration("sip:u3@b", "carrier-B", "yealink", "DE", "", "", 3600)

	counts := e.registrationCounts()
	require.Len(t, counts, 2)
	byCarrier := map[string]int{}
	for _, c := range counts {
		byCarrier[c.Labels["carrier"]] = c.Count
	}
	require.Equal(t, 2, byCarrier["carrier-A"])
	require.Equal(t, 1, byCarrier["carrier-B"])
}

func TestRegisterExpiryTrackerCleanupExpired(t *testing.T) {
	e := newExporterWithRegTracker()

	e.storeRegistration("sip:user1@example.com", "c", "sip", "US", "", "", 1)
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

func TestRegisterExpiryTrackerRefreshKeepsActive(t *testing.T) {
	e := newExporterWithRegTracker()
	aor := "sip:user1@example.com"

	e.storeRegistration(aor, "c", "sip", "US", "", "", 1)
	// Backdate close to expiry.
	e.registerExpiryMutex.Lock()
	ent := e.registerExpiryTracker[aor]
	ent.expiry = time.Now().Add(100 * time.Millisecond)
	e.registerExpiryTracker[aor] = ent
	e.registerExpiryMutex.Unlock()

	// Refresh before expiry.
	e.storeRegistration(aor, "c", "sip", "US", "", "", 3600)

	// Even though the old expiry has passed, the refresh extended it.
	time.Sleep(150 * time.Millisecond)
	e.cleanupExpiredRegistrations()

	counts := e.registrationCounts()
	require.Len(t, counts, 1, "refreshed registration must survive old-expiry cleanup")
}

func TestRegisterExpiryTrackerConcurrentAccess(t *testing.T) {
	e := newExporterWithRegTracker()
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 100 {
			aor := "sip:user" + strconv.Itoa(i%10) + "@example.com"
			e.storeRegistration(aor, "c", "sip", "US", "", "", 3600)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 100 {
			e.cleanupExpiredRegistrations()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 100 {
			_ = e.registrationCounts()
		}
	}()

	wg.Wait()

	counts := e.registrationCounts()
	require.NotEmpty(t, counts, "should have active registrations after concurrent stores")
}

// ==================== S6-9.2: Registration Country Change ====================

func TestRegisterCountryChangeDifferentCountry(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services:              services{metricser: mm, dialoger: &mockDialoger{}},
		registerExpiryTracker: make(map[string]registerExpiryEntry),
	}
	aor := "sip:alice@example.com"

	e.storeRegistration(aor, "beeline", "sip", "RU", "", "", 3600)
	require.Empty(t, mm.registerCountryChange, "first registration must not signal")

	e.storeRegistration(aor, "beeline", "sip", "GE", "", "", 3600)
	require.Len(t, mm.registerCountryChange, 1, "country change must signal")
	require.Equal(t, "GE", mm.registerCountryChange[0])
}

func TestRegisterCountryChangeSameCountry(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services:              services{metricser: mm, dialoger: &mockDialoger{}},
		registerExpiryTracker: make(map[string]registerExpiryEntry),
	}
	aor := "sip:alice@example.com"

	e.storeRegistration(aor, "beeline", "sip", "RU", "", "", 3600)
	e.storeRegistration(aor, "beeline", "sip", "RU", "", "", 3600)

	require.Empty(t, mm.registerCountryChange, "same country must not signal")
}

func TestRegisterCountryChangeFirstRegistration(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services:              services{metricser: mm, dialoger: &mockDialoger{}},
		registerExpiryTracker: make(map[string]registerExpiryEntry),
	}

	e.storeRegistration("sip:bob@example.com", "mts", "sip", "DE", "", "", 3600)

	require.Empty(t, mm.registerCountryChange, "first registration has no baseline")
}

func TestRegisterCountryChangeEmptyPreviousCountry(t *testing.T) {
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

	e.storeRegistration(aor, "beeline", "sip", "RU", "", "", 3600)

	require.Empty(t, mm.registerCountryChange, "empty previous country must not signal")
}

// ==================== S6-9.1: Registration Scan Detection ====================

func TestRegisterScanTrackerSignalsAtThreshold(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, time.Minute)

	for i := range 2 {
		tracker.record("1.2.3.4", fmt.Sprintf("user%d@evil.com", i), "carrier", "RU", "", mm)
	}
	require.Zero(t, mm.registerScanCalls, "below threshold must not signal")

	tracker.record("1.2.3.4", "user2@evil.com", "carrier", "RU", "", mm)
	require.Equal(t, 1, mm.registerScanCalls, "at threshold must signal")
}

func TestRegisterScanTrackerIncrementsPerAORAboveThreshold(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, time.Minute)

	for i := range 5 {
		tracker.record("1.2.3.4", fmt.Sprintf("user%d@evil.com", i), "carrier", "RU", "", mm)
	}
	require.Equal(t, 3, mm.registerScanCalls, "must increment for each AOR at or above threshold (5-3+1=3)")
}

func TestRegisterScanTrackerUniqueAORsOnly(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, time.Minute)

	tracker.record("1.2.3.4", "user@evil.com", "carrier", "RU", "", mm)
	tracker.record("1.2.3.4", "user@evil.com", "carrier", "RU", "", mm)
	tracker.record("1.2.3.4", "user@evil.com", "carrier", "RU", "", mm)

	require.Zero(t, mm.registerScanCalls, "same AOR must not count as scan")
}

func TestRegisterScanTrackerNilTrackerSafe(t *testing.T) {
	mm := &mockMetricser{}
	var tracker *registerScanTracker

	tracker.record("1.2.3.4", "user@evil.com", "carrier", "RU", "", mm)
	require.Zero(t, mm.registerScanCalls, "nil tracker must be no-op")
}

func TestRegisterScanTrackerEmptySrcIPSkipped(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(1, time.Minute)

	tracker.record("", "user@evil.com", "carrier", "RU", "", mm)
	require.Zero(t, mm.registerScanCalls, "empty srcIP must be skipped")
}

// ==================== S6-A.1: Memory Cap ====================

func TestRegisterScanTrackerMemoryBoundedAtMaxEntries(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, time.Minute)

	for i := range registerScanMaxEntriesPerIP + 10 {
		tracker.record("1.2.3.4", fmt.Sprintf("user%d@evil.com", i), "carrier", "RU", "", mm)
	}

	require.LessOrEqual(t, len(tracker.entries["1.2.3.4"]), registerScanMaxEntriesPerIP,
		"inner map must not exceed registerScanMaxEntriesPerIP")
	require.Positive(t, mm.registerScanCalls, "must have signalled above threshold")
}

func TestRegisterScanTrackerEvictionWorksAfterCap(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, 50*time.Millisecond)

	for i := range 3 {
		tracker.record("1.2.3.4", fmt.Sprintf("user%d@evil.com", i), "carrier", "RU", "", mm)
	}
	require.Equal(t, 1, mm.registerScanCalls)

	time.Sleep(80 * time.Millisecond)

	tracker.cleanup()
	require.Empty(t, tracker.entries["1.2.3.4"], "entries must expire after window")

	tracker.record("1.2.3.4", "newuser@evil.com", "carrier", "RU", "", mm)
	require.Equal(t, 1, mm.registerScanCalls, "first AOR after reset must not re-signal")

	tracker.record("1.2.3.4", "newuser2@evil.com", "carrier", "RU", "", mm)
	tracker.record("1.2.3.4", "newuser3@evil.com", "carrier", "RU", "", mm)
	require.Equal(t, 2, mm.registerScanCalls, "new burst after eviction must signal again")
}

func TestRegisterScanTrackerMultipleIPsIndependent(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, time.Minute)

	for i := range 3 {
		tracker.record("10.0.0.1", fmt.Sprintf("user%d@a.com", i), "carrier", "RU", "", mm)
	}
	require.Equal(t, 1, mm.registerScanCalls, "IP1 at threshold must signal")

	tracker.record("10.0.0.2", "user0@b.com", "carrier", "RU", "", mm)
	tracker.record("10.0.0.2", "user1@b.com", "carrier", "RU", "", mm)
	require.Equal(t, 1, mm.registerScanCalls, "IP2 below threshold must not signal")
}

// ==================== S6-A.13: Wasted Event Fix ====================

// TestRegisterScanTrackerNoWastedEventAfterExpiry verifies that the first
// event after window expiry IS recorded (not silently dropped).
func TestRegisterScanTrackerNoWastedEventAfterExpiry(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, 50*time.Millisecond)

	for i := range 3 {
		tracker.record("1.2.3.4", fmt.Sprintf("user%d@evil.com", i), "carrier", "RU", "", mm)
	}
	require.Equal(t, 1, mm.registerScanCalls, "at threshold must signal")

	time.Sleep(80 * time.Millisecond)

	// Eviction happens inside record(), NOT via cleanup().
	tracker.record("1.2.3.4", "newuser@evil.com", "carrier", "RU", "", mm)

	require.Len(t, tracker.entries["1.2.3.4"], 1,
		"first event after window expiry must be recorded, not wasted")
}

// TestRegisterScanTrackerRetriggerAtExactThreshold verifies that after
// window expiry, exactly `threshold` new events re-trigger the signal.
func TestRegisterScanTrackerRetriggerAtExactThreshold(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newRegisterScanTracker(3, 50*time.Millisecond)

	for i := range 3 {
		tracker.record("1.2.3.4", fmt.Sprintf("user%d@evil.com", i), "carrier", "RU", "", mm)
	}
	require.Equal(t, 1, mm.registerScanCalls, "first burst must signal")

	time.Sleep(80 * time.Millisecond)

	// Without cleanup(), eviction happens lazily inside record().
	// Exactly threshold events must re-trigger — not threshold+1.
	tracker.record("1.2.3.4", "new1@evil.com", "carrier", "RU", "", mm)
	tracker.record("1.2.3.4", "new2@evil.com", "carrier", "RU", "", mm)
	tracker.record("1.2.3.4", "new3@evil.com", "carrier", "RU", "", mm)

	require.Equal(t, 2, mm.registerScanCalls,
		"re-trigger after window expiry must fire at exactly threshold events")
}

// ==================== S6-9.3: INVITE Burst Detection ====================

func TestInviteBurstTrackerSignalsAtThreshold(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newInviteBurstTracker(5, time.Minute)

	for range 4 {
		tracker.record("1.2.3.4", "carrier", "RU", "", mm)
	}
	require.Zero(t, mm.inviteBurstCalls, "below threshold must not signal")

	tracker.record("1.2.3.4", "carrier", "RU", "", mm)
	require.Equal(t, 1, mm.inviteBurstCalls, "at threshold must signal")
}

func TestInviteBurstTrackerIncrementsPerInviteAboveThreshold(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newInviteBurstTracker(3, time.Minute)

	for range 10 {
		tracker.record("1.2.3.4", "carrier", "RU", "", mm)
	}
	require.Equal(t, 8, mm.inviteBurstCalls, "must increment for each INVITE at or above threshold (10-3+1=8)")
}

func TestInviteBurstTrackerNilTrackerSafe(t *testing.T) {
	mm := &mockMetricser{}
	var tracker *inviteBurstTracker

	tracker.record("1.2.3.4", "carrier", "RU", "", mm)
	require.Zero(t, mm.inviteBurstCalls, "nil tracker must be no-op")
}

func TestInviteBurstTrackerEmptySrcIPSkipped(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newInviteBurstTracker(1, time.Minute)

	tracker.record("", "carrier", "RU", "", mm)
	require.Zero(t, mm.inviteBurstCalls, "empty srcIP must be skipped")
}

// ==================== S11-2: FAS (False Answer Supervision) Detection ====================

func TestFasTrackerSignalsAfterThreshold(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newFasTracker(60 * time.Millisecond)

	tracker.store(
		"call-1",
		fasEntry{carrier: "carrier-a", uaType: "yealink", sourceCountry: "US", direction: "inbound"},
		nil,
		false,
	)
	require.Zero(t, mm.fasCalls, "immediately after 200 OK, FAS must not fire")

	tracker.sweep(mm)
	require.Zero(t, mm.fasCalls, "sweep before threshold must not fire")

	time.Sleep(90 * time.Millisecond)
	tracker.sweep(mm)
	require.Equal(t, 1, mm.fasCalls, "after threshold with no RTP, FAS must fire once")
	require.Empty(t, tracker.entries, "fired entry must be removed")
}

func TestFasTrackerNoSignalBeforeThreshold(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newFasTracker(time.Second)

	tracker.store(
		"call-1",
		fasEntry{carrier: "carrier-a", uaType: "yealink", sourceCountry: "US", direction: "inbound"},
		nil,
		false,
	)
	tracker.sweep(mm)
	require.Zero(t, mm.fasCalls, "entry younger than threshold must not fire")
	require.Len(t, tracker.entries, 1, "entry must remain pending")
}

func TestFasTrackerClearPreventsSignal(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newFasTracker(60 * time.Millisecond)

	tracker.store(
		"call-1",
		fasEntry{carrier: "carrier-a", uaType: "yealink", sourceCountry: "US", direction: "inbound"},
		nil,
		false,
	)
	tracker.clear("call-1")
	time.Sleep(90 * time.Millisecond)
	tracker.sweep(mm)

	require.Zero(t, mm.fasCalls, "RTP observed (clear) before threshold must prevent FAS")
}

func TestFasTrackerPreservesLabelsOnFire(t *testing.T) {
	mm := &mockMetricser{}
	tracker := newFasTracker(50 * time.Millisecond)

	tracker.store(
		"call-1",
		fasEntry{carrier: "carrier-b", uaType: "grandstream", sourceCountry: "DE", direction: "outbound"},
		nil,
		false,
	)
	time.Sleep(70 * time.Millisecond)
	tracker.sweep(mm)

	require.Equal(t, 1, mm.fasCalls, "FAS must fire with the stored labels")
	require.Len(t, mm.fasCallsLabels, 1)
	got := mm.fasCallsLabels[0]
	require.Equal(t, "carrier-b", got.carrier)
	require.Equal(t, "grandstream", got.uaType)
	require.Equal(t, "DE", got.sourceCountry)
	require.Equal(t, "outbound", got.direction)
}

func TestFasTrackerNilTrackerSafe(t *testing.T) {
	mm := &mockMetricser{}
	var tracker *fasTracker

	tracker.store(
		"call-1",
		fasEntry{carrier: "carrier", uaType: "ua", sourceCountry: "US", direction: "inbound"},
		nil,
		false,
	)
	tracker.clear("call-1")
	tracker.sweep(mm)
	require.Zero(t, mm.fasCalls, "nil tracker must be no-op")
}

// ==================== S11-2: FAS exporter wiring ====================

const (
	fasSdpNormal = "v=0\r\no=- 1 1 IN IP4 10.0.0.1\r\ns=-\r\n" +
		"c=IN IP4 10.0.0.1\r\nt=0 0\r\nm=audio 5004 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n"
	fasSdpHeld = "v=0\r\no=- 1 1 IN IP4 0.0.0.0\r\ns=-\r\n" +
		"c=IN IP4 0.0.0.0\r\nt=0 0\r\nm=audio 5004 RTP/AVP 0\r\n"
	// fasSdpOffer is the INVITE offer (caller side) advertising 10.0.0.2:5004,
	// distinct from the answer (fasSdpNormal, 10.0.0.1:5004) so FAS answer-side
	// gating can distinguish which side sent media.
	fasSdpOffer = "v=0\r\no=- 1 1 IN IP4 10.0.0.2\r\ns=-\r\n" +
		"c=IN IP4 10.0.0.2\r\nt=0 0\r\nm=audio 5004 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n"
	// fasSdpOffer2 is a re-INVITE offer with a changed caller endpoint (NAT
	// remap / codec renegotiation), used to verify FAS offer-update on re-INVITE.
	fasSdpOffer2 = "v=0\r\no=- 1 1 IN IP4 10.0.0.3\r\ns=-\r\n" +
		"c=IN IP4 10.0.0.3\r\nt=0 0\r\nm=audio 5004 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n"
	fasSdpSRTP = "v=0\r\no=- 1 1 IN IP4 10.0.0.1\r\ns=-\r\n" +
		"c=IN IP4 10.0.0.1\r\nt=0 0\r\nm=audio 5004 RTP/SAVPF 111\r\na=rtpmap:111 opus/48000/2\r\n" +
		"a=fingerprint:sha-256 AB:CD:01:02\r\na=setup:actpass\r\n"
)

func newFasTestExporter(mm *mockMetricser, fasThreshold time.Duration) *exporter {
	return &exporter{
		services: services{
			metricser: mm,
			dialoger:  service.NewDialoger(),
		},
		inviteTracker: make(map[string]inviteEntry),
		inviteSDP:     make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:  mediatracker.NewTracker(rtpStreamTTL),
		fasTracker:    newFasTracker(fasThreshold),
	}
}

func fasInvite200OK(callID, sdp string) dto.Packet {
	return dto.Packet{
		CallID:         []byte(callID),
		From:           dto.From{Tag: []byte("from-tag")},
		To:             dto.To{Tag: []byte("to-tag")},
		ContentType:    []byte("application/sdp"),
		SessionExpires: 3600,
		Body:           []byte(sdp),
	}
}

// rtpPacket builds an RTP header: V=2, PT=0 (PCMU), ts=160, SSRC=0x11223344.
// Sequence number is parameterised so a test can send a forward-advancing burst.
func fasRTPPacket(seq uint16) []byte {
	return []byte{0x80, 0x00, byte(seq >> 8), byte(seq), 0x00, 0x00, 0x00, 0xA0, 0x11, 0x22, 0x33, 0x44}
}

func TestFASHeldSDPNotTracked(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, time.Second)

	require.NoError(
		t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-held", fasSdpHeld), false),
	)
	require.Empty(t, e.fasTracker.entries, "held SDP (c=0.0.0.0) registers no media → not a FAS candidate")
}

func TestFAS200OKThenNoRTPFiresAfterThreshold(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, 60*time.Millisecond)

	require.NoError(
		t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)
	require.Len(t, e.fasTracker.entries, 1, "200 OK with media endpoints must open a FAS pending entry")

	e.fasTracker.sweep(mm)
	require.Zero(t, mm.fasCalls, "before threshold, FAS must not fire")

	time.Sleep(90 * time.Millisecond)
	e.fasTracker.sweep(mm)
	require.Equal(t, 1, mm.fasCalls, "no RTP within threshold → FAS fires")
}

func TestFASRTPBeforeThresholdPreventsFire(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, 60*time.Millisecond)

	require.NoError(
		t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)

	// No INVITE offer SDP was cached (inviteSDP empty) → FAS cannot tell which
	// side sent media → legacy fallback: any media clears. Two forward packets
	// for the registered endpoint (10.0.0.1:5004) → media established (≥2) → cleared.
	_, err := e.handleRTP(net.ParseIP("10.0.0.2"), 5004, net.ParseIP("10.0.0.1"), 5004, fasRTPPacket(1))
	require.NoError(t, err)
	_, err = e.handleRTP(net.ParseIP("10.0.0.2"), 5004, net.ParseIP("10.0.0.1"), 5004, fasRTPPacket(2))
	require.NoError(t, err)
	require.Empty(t, e.fasTracker.entries, "≥2 RTP packets → FAS pending cleared")

	time.Sleep(90 * time.Millisecond)
	e.fasTracker.sweep(mm)
	require.Zero(t, mm.fasCalls, "established media before threshold must prevent FAS")
}

// TestFASCallerRTPDoesNotClear is the core S11-4 regression: when both offer
// (caller) and answer (callee) media endpoints are registered, RTP from the
// calling side (arriving at the answer endpoint) must NOT defeat FAS — only
// answer-side media (arriving at the offer endpoint) clears it. This prevents a
// fraudster's false 200 OK from being masked by the victim's own upstream RTP.
func TestFASCallerRTPDoesNotClear(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, 60*time.Millisecond)

	// Cache the INVITE offer SDP (caller endpoint 10.0.0.2:5004) so the 200 OK
	// registers BOTH sides and FAS can gate by originating side.
	e.storeInviteSDP("call-1", "", []byte(fasSdpOffer), "from-tag")

	require.NoError(
		t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)
	require.Len(t, e.fasTracker.entries, 1, "200 OK with media endpoints must open a FAS pending entry")
	require.Len(t, e.fasTracker.offer["call-1"], 1, "offer endpoint must be tracked for side gating")

	// Caller (offer side) sends media TO the answer endpoint 10.0.0.1:5004 → the
	// stream is keyed by the answer endpoint (dst-first correlation) → NOT an
	// offer endpoint → FAS must survive even after ≥2 forward packets.
	for _, seq := range []uint16{1, 2, 3} {
		_, err := e.handleRTP(net.ParseIP("10.0.0.2"), 5004, net.ParseIP("10.0.0.1"), 5004, fasRTPPacket(seq))
		require.NoError(t, err)
	}
	require.Len(t, e.fasTracker.entries, 1,
		"caller-side RTP (arriving at answer endpoint) must not clear FAS")

	// Answer side (callee) sends media TO the offer endpoint 10.0.0.2:5004 → the
	// stream is keyed by the offer endpoint → answer-side media confirmed → clears.
	for _, seq := range []uint16{1, 2} {
		_, err := e.handleRTP(net.ParseIP("10.0.0.1"), 5004, net.ParseIP("10.0.0.2"), 5004, fasRTPPacket(seq))
		require.NoError(t, err)
	}
	require.Empty(t, e.fasTracker.entries,
		"answer-side RTP (arriving at offer endpoint) must clear FAS")
}

// TestFASRetransmit200OKPreservesOfferSet verifies that a retransmitted 200 OK
// (Timer G, UDP) does not destroy the offer-side gating established by the first
// 200 OK. On retransmission the cached INVITE SDP is already consumed, so
// updateOffer receives empty endpoints — it must NOT delete the existing offer
// set. Otherwise the caller's own RTP clears FAS via the legacy fallback.
func TestFASRetransmit200OKPreservesOfferSet(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, time.Hour)

	e.storeInviteSDP("call-1", "", []byte(fasSdpOffer), "from-tag")
	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)
	require.Len(t, e.fasTracker.offer["call-1"], 1, "first 200 OK must populate offer endpoints")

	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), true),
	)
	require.Contains(t, e.fasTracker.offer["call-1"], fasEndpoint{ip: "10.0.0.2", port: 5004},
		"retransmitted 200 OK must not delete the offer set")

	for _, seq := range []uint16{1, 2, 3} {
		_, err := e.handleRTP(net.ParseIP("10.0.0.2"), 5004, net.ParseIP("10.0.0.1"), 5004, fasRTPPacket(seq))
		require.NoError(t, err)
	}
	require.Len(t, e.fasTracker.entries, 1,
		"caller RTP must not clear FAS after retransmitted 200 OK — side-gating preserved")
}

// TestFASLateOfferCallerRTPDoesNotClear closes the src-fallback hole: when
// the INVITE offer was cached (offer map populated) but the 200 OK carried no
// SDP (late offer — ISUP gateways), the answer endpoint is never registered.
// Caller RTP then matches by src-fallback on the offer endpoint. Without the
// fix this falsely clears FAS (caller's own media masks fraud). The match-by
// must be "src" and, with a non-empty offer map, must not clear.
func TestFASLateOfferCallerRTPDoesNotClear(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, 60*time.Millisecond)

	e.storeInviteSDP("call-late", "", []byte(fasSdpOffer), "from-tag")

	pkt := fasInvite200OK("call-late", "")
	pkt.ContentType = nil
	require.NoError(t, e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", pkt, false))
	require.Len(t, e.fasTracker.entries, 1, "offer endpoints from cached INVITE must open FAS pending")
	require.Len(t, e.fasTracker.offer["call-late"], 1, "offer endpoint tracked for side gating")

	for _, seq := range []uint16{1, 2, 3} {
		_, err := e.handleRTP(net.ParseIP("10.0.0.2"), 5004, net.ParseIP("10.0.0.1"), 5004, fasRTPPacket(seq))
		require.NoError(t, err)
	}
	require.Len(t, e.fasTracker.entries, 1,
		"caller RTP matched by src-fallback on offer endpoint must not clear FAS")
}

// TestFASLateOfferAnswerRTPClears is the MC/DC complement to the negative
// test above: in the same late-offer scenario (answer endpoint not registered),
// genuine answer-side media (callee→caller, dst-match on the offer endpoint)
// must still clear FAS. This proves the guard is matchedBy-specific, not a
// blanket block on late-offer calls.
func TestFASLateOfferAnswerRTPClears(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, 60*time.Millisecond)

	e.storeInviteSDP("call-late", "", []byte(fasSdpOffer), "from-tag")

	pkt := fasInvite200OK("call-late", "")
	pkt.ContentType = nil
	require.NoError(t, e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", pkt, false))
	require.Len(t, e.fasTracker.entries, 1)

	for _, seq := range []uint16{1, 2} {
		_, err := e.handleRTP(net.ParseIP("10.0.0.1"), 5004, net.ParseIP("10.0.0.2"), 5004, fasRTPPacket(seq))
		require.NoError(t, err)
	}
	require.Empty(t, e.fasTracker.entries,
		"answer RTP (dst-match on offer endpoint) must clear FAS even in late-offer scenario")
}

func TestFASSingleRTPDoesNotClear(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, 60*time.Millisecond)

	require.NoError(
		t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)

	// A single RTP packet must NOT clear FAS (could be a stray/spoofed packet).
	_, err := e.handleRTP(net.ParseIP("10.0.0.2"), 5004, net.ParseIP("10.0.0.1"), 5004, fasRTPPacket(1))
	require.NoError(t, err)
	require.Len(t, e.fasTracker.entries, 1, "single RTP packet must not clear FAS pending")

	time.Sleep(90 * time.Millisecond)
	e.fasTracker.sweep(mm)
	require.Equal(t, 1, mm.fasCalls, "one stray packet does not establish media → FAS fires after threshold")
}

func TestFASByeBeforeThresholdPreventsFire(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, 60*time.Millisecond)

	require.NoError(
		t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)

	// BYE tears down the dialog before the threshold → FAS pending cleared.
	require.NoError(t, e.handleBye200OK(fasInvite200OK("call-1", ""), ""))
	require.Empty(t, e.fasTracker.entries, "BYE teardown → FAS pending cleared")

	time.Sleep(90 * time.Millisecond)
	e.fasTracker.sweep(mm)
	require.Zero(t, mm.fasCalls, "short call without RTP (BYE before threshold) must not be misreported as FAS")
}

// TestFASByePathFiresAfterFloor verifies that a call answered with no
// answer-side RTP, terminated by BYE after the floor duration, is reported as
// FAS at teardown — covering short dead-air calls that end before the sweep
// threshold (S11-7 / F5).
func TestFASByePathFiresAfterFloor(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, time.Hour) // long threshold: only the BYE path can fire

	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)
	require.Len(t, e.fasTracker.entries, 1)

	// Simulate answer→BYE duration above the floor by backdating the entry.
	shift := fasByeFloor + time.Second
	e.fasTracker.mu.Lock()
	ent := e.fasTracker.entries["call-1"]
	ent.createdAt = time.Now().Add(-shift)
	ent.byeFloor = ent.byeFloor.Add(-shift)
	e.fasTracker.entries["call-1"] = ent
	e.fasTracker.mu.Unlock()

	require.NoError(t, e.handleBye200OK(fasInvite200OK("call-1", ""), ""))
	require.Equal(t, 1, mm.fasCalls, "BYE after floor with no answer-side RTP must fire FAS")
	require.Empty(t, e.fasTracker.entries, "entry cleared after BYE finalize")
}

// TestFASByePathBelowFloorNoFire verifies that a very short call (caller
// abandoned immediately) is not reported as FAS — not fraud, just a quick hangup.
func TestFASByePathBelowFloorNoFire(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, time.Hour)

	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)
	// No backdating: createdAt is "now", far below the floor.
	require.NoError(t, e.handleBye200OK(fasInvite200OK("call-1", ""), ""))
	require.Zero(t, mm.fasCalls, "BYE before floor must not fire FAS")
}

// TestFASByePathNoFireWhenMediaCleared verifies the BYE path is a no-op when
// answer-side media already cleared the pending entry (the normal good case).
func TestFASByePathNoFireWhenMediaCleared(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, time.Hour)

	e.storeInviteSDP("call-1", "", []byte(fasSdpOffer), "from-tag")
	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)
	// Answer-side media (2 packets to the offer endpoint) clears the entry.
	for _, seq := range []uint16{1, 2} {
		_, err := e.handleRTP(net.ParseIP("10.0.0.1"), 5004, net.ParseIP("10.0.0.2"), 5004, fasRTPPacket(seq))
		require.NoError(t, err)
	}
	require.Empty(t, e.fasTracker.entries, "answer-side media clears FAS pending")

	// Backdate would be moot — the entry is already gone; BYE must not fire.
	require.NoError(t, e.handleBye200OK(fasInvite200OK("call-1", ""), ""))
	require.Zero(t, mm.fasCalls, "no FAS when media already cleared the entry")
}

// TestFASByePathSRTPBelowFloorNoFire verifies that an SRTP call ending via
// BYE below fasByeFloor does not fire — the floor protects short calls from
// false positives (caller abandoned before media could start). The floor is
// the same for plain RTP and SRTP (S11-16: grace extends only the sweep path).
func TestFASByePathSRTPBelowFloorNoFire(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, 10*time.Second)

	t0 := time.Unix(1_700_000_000, 0)
	e.fasTracker.SetNow(func() time.Time { return t0 })

	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpSRTP), false),
	)
	require.Len(t, e.fasTracker.entries, 1)

	e.fasTracker.SetNow(func() time.Time { return t0.Add(1 * time.Second) })
	require.NoError(t, e.handleBye200OK(fasInvite200OK("call-1", ""), ""))
	require.Zero(t, mm.fasCalls, "SRTP call BYE below fasByeFloor must not fire")
}

// TestFASByePathSRTPFakeFingerprintStillFires closes the DTLS-grace evasion
// on the BYE path (S11-16): a fraudster can add a fake a=fingerprint to the 200
// OK and get byeFloor = full deadline (25s), hanging up before any media without
// detection. byeFloor must always be fasByeFloor (3s) regardless of SRTP — the
// grace protects only the sweep path, where the fraudster does not control timing.
func TestFASByePathSRTPFakeFingerprintStillFires(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, 10*time.Second)

	t0 := time.Unix(1_700_000_000, 0)
	e.fasTracker.SetNow(func() time.Time { return t0 })

	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpSRTP), false),
	)
	require.Len(t, e.fasTracker.entries, 1)

	// 5s elapsed: past fasByeFloor (3s), well within the SRTP sweep deadline
	// (10s + 15s grace = 25s). A fraudster hangs up here — FAS must fire.
	e.fasTracker.SetNow(func() time.Time { return t0.Add(5 * time.Second) })
	require.NoError(t, e.handleBye200OK(fasInvite200OK("call-1", ""), ""))
	require.Equal(t, 1, mm.fasCalls, "SRTP BYE after fasByeFloor must fire despite fake fingerprint")
}

// TestFASSRTPExtendsThreshold verifies the DTLS-SRTP grace: a call whose
// answer SDP carries a=fingerprint does NOT fire FAS at the base threshold, but
// does fire after base+grace (S11-6 / F2).
func TestFASSRTPExtendsThreshold(t *testing.T) {
	mm := &mockMetricser{}
	base := 60 * time.Millisecond
	e := newFasTestExporter(mm, base)

	t0 := time.Unix(1_700_000_000, 0)
	e.fasTracker.SetNow(func() time.Time { return t0 })

	const srtpSDP = "v=0\r\no=- 1 1 IN IP4 10.0.0.1\r\ns=-\r\nc=IN IP4 10.0.0.1\r\n" +
		"t=0 0\r\nm=audio 5004 RTP/SAVPF 111\r\na=rtpmap:111 opus/48000/2\r\n" +
		"a=fingerprint:sha-256 AB:CD:01:02\r\na=setup:actpass\r\n"

	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", srtpSDP), false),
	)
	require.Len(t, e.fasTracker.entries, 1)

	// At base threshold, an SRTP call must NOT fire yet (grace still running).
	e.fasTracker.SetNow(func() time.Time { return t0.Add(base + 10*time.Millisecond) })
	e.fasTracker.sweep(mm)
	require.Zero(t, mm.fasCalls, "SRTP call must not fire within base threshold (grace active)")

	// After base + grace, with no RTP, FAS fires.
	e.fasTracker.SetNow(func() time.Time { return t0.Add(base + fasSRTPGrace + 10*time.Millisecond) })
	e.fasTracker.sweep(mm)
	require.Equal(t, 1, mm.fasCalls, "SRTP call must fire after base+grace with no RTP")
}

func TestFASReinviteDoesNotOpenPending(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, time.Second)
	createActiveTestDialog(t, e.services.dialoger)

	require.NoError(
		t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), true),
	)
	require.Empty(t, e.fasTracker.entries, "re-INVITE refreshes a dialog, not a new answer → no FAS pending")
}

// TestFASReinviteUpdatesOfferEndpoints verifies that a re-INVITE changing the
// caller's media endpoint updates the FAS offer map — otherwise answer-side
// media arriving at the new endpoint would NOT clear FAS (stale offer).
func TestFASReinviteUpdatesOfferEndpoints(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, time.Hour)

	// Initial call: INVITE offer 10.0.0.2:5004 → 200 OK answer 10.0.0.1:5004.
	e.storeInviteSDP("call-1", "", []byte(fasSdpOffer), "from-tag")
	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)
	require.Contains(t, e.fasTracker.offer["call-1"], fasEndpoint{ip: "10.0.0.2", port: 5004},
		"initial offer endpoint must be tracked")

	// Re-INVITE: caller changes endpoint to 10.0.0.3:5004.
	e.storeInviteSDP("call-1", "", []byte(fasSdpOffer2), "from-tag")
	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), true),
	)

	require.Contains(t, e.fasTracker.offer["call-1"], fasEndpoint{ip: "10.0.0.3", port: 5004},
		"re-INVITE must update offer to the new endpoint")
	require.NotContains(t, e.fasTracker.offer["call-1"], fasEndpoint{ip: "10.0.0.2", port: 5004},
		"stale offer endpoint must be replaced")

	// Answer-side media at the NEW offer endpoint must clear FAS.
	for _, seq := range []uint16{1, 2} {
		_, err := e.handleRTP(net.ParseIP("10.0.0.1"), 5004, net.ParseIP("10.0.0.3"), 5004, fasRTPPacket(seq))
		require.NoError(t, err)
	}
	require.Empty(t, e.fasTracker.entries, "answer-side RTP at re-INVITE offer endpoint must clear FAS")
}

// TestFASReinviteSRTPExtendsDeadline verifies that a re-INVITE upgrading
// plain-RTP to SRTP extends the FAS deadline by the grace window — otherwise
// the sweep fires on the original (too-short) plain-RTP deadline.
func TestFASReinviteSRTPExtendsDeadline(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, 100*time.Millisecond)

	// Initial call with plain RTP: deadline = now + 100ms (no grace).
	e.storeInviteSDP("call-1", "", []byte(fasSdpOffer), "from-tag")
	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)
	origDeadline := e.fasTracker.entries["call-1"].deadline

	// Re-INVITE upgrades to SRTP: deadline must extend by fasSRTPGrace.
	e.storeInviteSDP("call-1", "", []byte(fasSdpOffer), "from-tag")
	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpSRTP), true),
	)
	newDeadline := e.fasTracker.entries["call-1"].deadline
	require.True(t, newDeadline.After(origDeadline.Add(fasSRTPGrace-time.Second)),
		"re-INVITE to SRTP must extend deadline by ~fasSRTPGrace")
	require.True(t, e.fasTracker.entries["call-1"].byeFloor.Before(newDeadline),
		"byeFloor must remain at fasByeFloor, not track the SRTP grace deadline")
}

// TestFASConcurrentSweepClearBye verifies thread safety of fasTracker under
// concurrent access from the three goroutines that touch it in production:
// sipDialogMetricsUpdate (sweep), readPackets/handleRTP (clearIfAnswerMedia),
// and readPackets/handleBye200OK (finalizeOnBye). Run with -race.
func TestFASConcurrentSweepClearBye(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, time.Hour)

	e.storeInviteSDP("call-1", "", []byte(fasSdpOffer), "from-tag")
	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				e.fasTracker.sweep(mm)
				e.fasTracker.clearIfAnswerMedia("call-1", fasEndpoint{ip: "10.0.0.2", port: 5004}, 5, "dst")
				e.fasTracker.finalizeOnBye("call-1", mm)
			}
		}()
	}
	wg.Wait()
	// No data-race panic under -race = PASS. fasCalls may be 0 or 1 depending
	// on which goroutine wins the entry — the test verifies safety, not value.
}

// TestFASSweepThenClearNoDoubleFire verifies that dialog-expiry cleanup
// calling fasTracker.clear after sweep already fired does NOT double-increment
// fasCalls (sweep deleted the entry; clear is a no-op).
func TestFASSweepThenClearNoDoubleFire(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, 100*time.Millisecond)

	t0 := time.Unix(1_700_000_000, 0)
	e.fasTracker.SetNow(func() time.Time { return t0 })

	require.NoError(t,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("call-1", fasSdpNormal), false),
	)

	// Advance past deadline → sweep fires.
	e.fasTracker.SetNow(func() time.Time { return t0.Add(200 * time.Millisecond) })
	e.fasTracker.sweep(mm)
	require.Equal(t, 1, mm.fasCalls)

	// Dialog expiry cleanup → clear must NOT double-fire.
	e.fasTracker.clear("call-1")
	require.Equal(t, 1, mm.fasCalls, "clear after sweep must not double-fire")
	require.Empty(t, e.fasTracker.entries)
}

// blockingFasMetricser wraps mockMetricser so FasCall signals called and then
// blocks until block is closed. Used to prove that sweep/finalizeOnBye release
// the fasTracker lock before invoking reportFAS (metricser + zap must run
// outside the critical section so packet processing is not stalled).
type blockingFasMetricser struct {
	mockMetricser

	called chan struct{}
	block  chan struct{}
}

func (m *blockingFasMetricser) FasCall(_, _, _, _ string) {
	m.called <- struct{}{}
	<-m.block
}

func TestFASSweepDoesNotHoldLockDuringReport(t *testing.T) {
	ft := newFasTracker(time.Hour)
	t0 := time.Unix(1_700_000_000, 0)
	ft.SetNow(func() time.Time { return t0 })

	ft.store("expired-1", fasEntry{}, nil, false)
	ft.store("expired-2", fasEntry{}, nil, false)

	ft.SetNow(func() time.Time { return t0.Add(2 * time.Hour) })

	blockMM := &blockingFasMetricser{
		called: make(chan struct{}, 2),
		block:  make(chan struct{}),
	}

	sweepDone := make(chan struct{})
	go func() {
		ft.sweep(blockMM)
		close(sweepDone)
	}()

	<-blockMM.called // FasCall invoked — sweep is inside reportFAS.

	// If sweep holds t.mu during reportFAS, Size blocks until FasCall returns.
	sizeDone := make(chan struct{})
	go func() {
		_ = ft.Size()
		close(sizeDone)
	}()

	select {
	case <-sizeDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("sweep held fasTracker.mu during reportFAS; Size blocked >200ms")
	}

	close(blockMM.block)
	<-sweepDone
}

func TestFASFinalizeOnByeDoesNotHoldLockDuringReport(t *testing.T) {
	ft := newFasTracker(time.Hour)
	t0 := time.Unix(1_700_000_000, 0)
	ft.SetNow(func() time.Time { return t0 })

	ft.store("call-bye", fasEntry{}, nil, false)

	// Advance past byeFloor so finalizeOnBye will report.
	ft.SetNow(func() time.Time { return t0.Add(2 * time.Hour) })

	blockMM := &blockingFasMetricser{
		called: make(chan struct{}, 1),
		block:  make(chan struct{}),
	}

	byeDone := make(chan struct{})
	go func() {
		ft.finalizeOnBye("call-bye", blockMM)
		close(byeDone)
	}()

	<-blockMM.called // FasCall invoked — finalizeOnBye is inside reportFAS.

	sizeDone := make(chan struct{})
	go func() {
		_ = ft.Size()
		close(sizeDone)
	}()

	select {
	case <-sizeDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("finalizeOnBye held fasTracker.mu during reportFAS; Size blocked >200ms")
	}

	close(blockMM.block)
	<-byeDone
}

// TestHandleRTPKernelTimestampMissingCounter verifies the wiring (MC/DC):
// handleRTP calls RTPKernelTimestampMissing when pktTimestamp is zero and does
// NOT call it when a kernel timestamp is present.
func TestHandleRTPKernelTimestampMissingCounter(t *testing.T) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, time.Hour)

	_, err := e.handleRTP(net.ParseIP("10.0.0.1"), 5004, net.ParseIP("10.0.0.2"), 5004, fasRTPPacket(1))
	require.NoError(t, err)
	require.Equal(t, 1, mm.rtpKernelTimestampMissingCalls,
		"zero pktTimestamp (no SO_TIMESTAMPNS) must increment the counter")

	e.pktTimestamp = time.Unix(1_700_000_000, 0)
	_, err = e.handleRTP(net.ParseIP("10.0.0.1"), 5004, net.ParseIP("10.0.0.2"), 5004, fasRTPPacket(2))
	require.NoError(t, err)
	require.Equal(t, 1, mm.rtpKernelTimestampMissingCalls,
		"non-zero pktTimestamp must not increment the counter")
}

// TestFASSweepLatency10kEntries is a performance regression guard: sweeping
// 10k expired entries must stay well under the threshold. A spike indicates
// accidental O(n²) behavior or a regression of the S11-14 lock-release fix.
func TestFASSweepLatency10kEntries(t *testing.T) {
	mm := &mockMetricser{}
	ft := newFasTracker(time.Hour)
	t0 := time.Unix(1_700_000_000, 0)
	ft.SetNow(func() time.Time { return t0 })

	for i := range 10_000 {
		ft.store(fmt.Sprintf("call-%d", i), fasEntry{}, nil, false)
	}
	ft.SetNow(func() time.Time { return t0.Add(2 * time.Hour) })

	start := time.Now()
	ft.sweep(mm)
	elapsed := time.Since(start)

	require.Equal(t, 10_000, mm.fasCalls, "all 10k entries must fire")
	require.Less(t, elapsed, 200*time.Millisecond,
		"sweep of 10k entries must complete <200ms (got %v)", elapsed)
}

func BenchmarkHandleRTP_FASHotPath(b *testing.B) {
	mm := &mockMetricser{}
	e := newFasTestExporter(mm, time.Hour)
	e.pktTimestamp = time.Unix(1_700_000_000, 0)

	for i := range 1000 {
		e.fasTracker.store(fmt.Sprintf("filler-%d", i), fasEntry{}, nil, false)
	}
	e.storeInviteSDP("bench", "", []byte(fasSdpOffer), "from-tag")
	require.NoError(b,
		e.handleInvite200OK("carrier-a", "yealink", "US", "inbound", fasInvite200OK("bench", fasSdpNormal), false),
	)

	dst := net.ParseIP("10.0.0.2")
	src := net.ParseIP("10.0.0.1")
	pkt := fasRTPPacket(0)

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		pkt[2] = byte(uint16(i) >> 8)
		pkt[3] = byte(uint16(i))
		_, _ = e.handleRTP(src, 5004, dst, 5004, pkt)
	}
}

func TestHandleMessageReINVITEExcludedFromBurst(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:      make(map[string]inviteEntry),
		inviteSDP:          make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker:     make(map[string]optionsEntry),
		mediaTracker:       mediatracker.NewTracker(rtpStreamTTL),
		inviteBurstTracker: newInviteBurstTracker(3, time.Minute),
	}

	md.Create(service.DialogParams{
		DialogID:  "call-id:abc:xyz",
		ExpiresAt: time.Now().Add(1 * time.Hour),
		CreatedAt: time.Now(),
		CallID:    "call-id",
	})

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
func TestHandleMessageRegister200OKPopulatesExpiryTracker(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker:       make(map[string]registerEntry),
		registerExpiryTracker: make(map[string]registerExpiryEntry),
		inviteTracker:         make(map[string]inviteEntry),
		inviteSDP:             make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:          mediatracker.NewTracker(rtpStreamTTL),
	}

	require.NoError(t, e.handleMessage("carrier-A", "US", makeRegister("e2e1", "ft")))
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

func TestHandleMessageResponse401(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

	require.Equal(t, []byte("401"), mm.responseCalled)
	require.False(t, mm.responseIsInvite)
}

func TestHandleMessageResponse302INVITE(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

	require.Equal(t, []byte("302"), mm.responseCalled)
	require.True(t, mm.responseIsInvite)
}

// Integration test for SER change via handleMessage.
func TestHandleMessageSERIntegration(t *testing.T) {
	m := &mockMetricser{}
	d := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: m,
			dialoger:  d,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	// 10 INVITE requests
	for i := range 10 {
		input := []byte("INVITE sip:test SIP/2.0\r\n" +
			"From: <sip:user@domain>;tag=abc\r\n" +
			"To: <sip:other@domain>\r\n" +
			"Call-ID: test-" + strconv.Itoa(i) + "\r\n" +
			"CSeq: 1 INVITE\r\n")
		err := e.handleMessage("other", "", input)
		require.NoError(t, err)
	}

	// 5 200 OK responses to INVITE (test-0..test-4)
	for i := range 5 {
		input := []byte("SIP/2.0 200 OK\r\n" +
			"From: <sip:user@domain>;tag=abc\r\n" +
			"To: <sip:other@domain>;tag=xyz" + strconv.Itoa(i) + "\r\n" +
			"Call-ID: test-" + strconv.Itoa(i) + "\r\n" +
			"CSeq: 1 INVITE\r\n")
		err := e.handleMessage("other", "", input)
		require.NoError(t, err)
	}

	// 2 302 responses to INVITE (test-5, test-6 — matching Call-IDs)
	for i := range 2 {
		input := []byte("SIP/2.0 302 Moved Temporarily\r\n" +
			"From: <sip:user@domain>;tag=abc\r\n" +
			"To: <sip:other@domain>;tag=xyz" + strconv.Itoa(5+i) + "\r\n" +
			"Call-ID: test-" + strconv.Itoa(5+i) + "\r\n" +
			"CSeq: 1 INVITE\r\n")
		err := e.handleMessage("other", "", input)
		require.NoError(t, err)
	}

	// 10 INVITE requests → requestCount must be 10
	require.Equal(t, 10, m.requestCount, "all 10 INVITEs must be counted")

	// 200 OK to INVITE → invite200OKCalled
	require.True(t, m.invite200OKCalled, "Invite200OK must be called for 200 OK responses")

	// INVITE responses → responseIsInvite
	require.True(t, m.responseIsInvite, "Response must be flagged as INVITE response")
}

func TestHandleMessageParseError(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}

	// Invalid SIP packet - "invalid" is not a valid SIP request line.
	// handleMessage returns parse error for malformed packets.
	err := e.handleMessage("other", "", []byte("invalid"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "malformed request line")
}

func TestHandleMessageResponse200InvalidDialogID(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
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

func TestParseRawPacketAllSIPMethods(t *testing.T) {
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
				inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
				optionsTracker:  make(map[string]optionsEntry),
				byeTracker:      make(map[string]byeEntry),
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

func TestParseRawPacketSIPResponse(t *testing.T) {
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
				inviteSDP:    make(map[inviteSDPKey]inviteSDPEntity),
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

func TestParseRawPacketSIPMethodsBytesPrefix(t *testing.T) {
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

func TestSIPPacketParseResponse401(t *testing.T) {
	e := exporter{}

	input := []byte("SIP/2.0 401 Unauthorized\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:55147;rport=55147;branch=z9hG4bKPjda81fdbda2a5464898d03d02ed894a2d\r\n" +
		"Call-ID: test\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.True(t, p.IsResponse)
	require.Equal(t, []byte("401"), p.ResponseStatus)
}

func TestSIPPacketParseINVITE(t *testing.T) {
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

func TestParseResponsesPacket401(t *testing.T) {
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

func TestParseResponsesPacket200(t *testing.T) {
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

func TestExporterRegisterTrackerStoreAndRemove(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestExporterRegisterTracker401Removes(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestExporterRegisterTracker403Removes(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestExporterRegisterTracker500Removes(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestExporterRegisterTrackerTTLExpired(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestExporterRegisterTrackerTTLNotExpired(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestExporterRegisterTrackerRetransmit200OK(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	// First REGISTER
	registerReq := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: same-call-id\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerReq)

	// Retransmit REGISTER (same Call-ID)
	e.handleMessage("other", "", registerReq)

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

func TestExporterRegisterTrackerRetransmit401(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	// First REGISTER
	registerReq := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: same-call-id-401\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", registerReq)

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

func TestExporterRegisterTrackerDifferentCallID(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestSIPDialogMetricsUpdateExpiredIncrementsSessionCompleted(t *testing.T) {
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
		e.services.metricser.SessionCompleted(r.Carrier, r.UAType, r.SourceCountry, "")
		e.services.metricser.UpdateSPD(r.Carrier, r.UAType, r.SourceCountry, "", r.Duration)
	}

	require.True(t, mm.sessionCompletedFlag)
	require.True(t, mm.spdUpdated)
	t.Logf("duration: %v", time.Since(start))
}

// ==================== Invite Tracker tests ====================

func TestExporterInviteTrackerStoreAndMeasure(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	callID := "test-call-id-123"

	e.storeInviteTime(callID, "other", "other", "", "")

	r := e.readInviteEntry(callID)
	require.True(t, r.Ok, "readInviteEntry should return true for existing entry")
	require.Greater(t, r.DelayMs, 0.0, "delay should be positive")
	require.Equal(t, "other", r.Carrier)

	e.removeInviteTime(callID)
	require.False(t, e.readInviteEntry(callID).Ok, "entry should not exist after remove")
}

func TestExporterInviteTrackerStoreAndRemove(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	callID := "test-call-id-remove"

	e.storeInviteTime(callID, "other", "other", "", "")
	e.removeInviteTime(callID)

	ok := e.readInviteEntry(callID).Ok
	require.False(t, ok, "entry should not exist after remove")
}

func TestExporterInviteTrackerMeasureNonExistent(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	r := e.readInviteEntry("nonexistent")
	require.False(t, r.Ok, "readInviteEntry should return false for nonexistent entry")
	require.InDelta(t, 0.0, r.DelayMs, 0.01)
	require.Empty(t, r.Carrier)
}

func TestExporterInviteTrackerRemoveNonExistent(_ *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	e.removeInviteTime("nonexistent")
}

func TestExporterInviteTrackerTTLExpired(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	oldTime := time.Now().Add(-61 * time.Second)
	e.inviteTracker["expired-call-id"] = inviteEntry{timestamp: oldTime, carrier: "other"}

	borderTime := time.Now().Add(-59 * time.Second)
	e.inviteTracker["border-call-id"] = inviteEntry{timestamp: borderTime, carrier: "other"}

	e.inviteTracker["fresh-call-id"] = inviteEntry{timestamp: time.Now(), carrier: "other"}

	e.cleanupInviteTracker()

	expiredExists := e.readInviteEntry("expired-call-id").Ok
	borderExists := e.readInviteEntry("border-call-id").Ok
	freshExists := e.readInviteEntry("fresh-call-id").Ok

	require.False(t, expiredExists, "expired entry (61s) should be removed")
	require.True(t, borderExists, "entry at 59s should remain (TTL=60s)")
	require.True(t, freshExists, "fresh entry should remain")
}

func TestExporterInviteTrackerTTLNotExpired(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	recentTime := time.Now().Add(-30 * time.Second)
	e.inviteTracker["recent-call-id"] = inviteEntry{timestamp: recentTime, carrier: "other"}

	e.cleanupInviteTracker()

	exists := e.readInviteEntry("recent-call-id").Ok
	require.True(t, exists, "entry at 30s should remain (TTL=60s)")
}

func TestExporterInviteTrackerDifferentCallIDs(t *testing.T) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	e.storeInviteTime("call-id-1", "other", "other", "", "")
	e.storeInviteTime("call-id-2", "other", "other", "", "")

	ok1 := e.readInviteEntry("call-id-1").Ok
	ok2 := e.readInviteEntry("call-id-2").Ok
	require.True(t, ok1)
	require.True(t, ok2)

	e.removeInviteTime("call-id-1")
	ok1 = e.readInviteEntry("call-id-1").Ok
	require.False(t, ok1, "call-id-1 should be removed")
}

// ==================== TTR integration tests ====================

func TestHandleMessageTTR100Trying(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessageTTR180Ringing(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessageTTR183SessionProgress(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessageTTRNoProvisionalResponse(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

	require.False(t, mm.ttrUpdated, "TTR should NOT be measured when no 1xx received")
}

func TestHandleMessageTTROnlyFirstProvisionalMeasured(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

	ringingResp := []byte("SIP/2.0 180 Ringing\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-first-only\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", ringingResp)

	require.InDelta(t, firstTTR, mm.ttrDelay, 0.01, "TTR should NOT be measured again on second 1xx")
}

func TestHandleMessageTTRRetransmitOverwrites(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

	e.handleMessage("other", "", inviteReq)

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

func TestHandleMessageTTRFinalResponseRemovesTracker(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

	require.False(t, mm.ttrUpdated, "TTR should NOT be measured for non-1xx response")

	ok := e.readInviteEntry("ttr-final-remove").Ok
	require.False(t, ok, "tracker entry should be removed after final response")
}

func TestHandleMessageTTRNonInviteResponseIgnored(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

	require.False(t, mm.ttrUpdated, "TTR should NOT be measured for REGISTER 100 Trying")
}

func TestHandleMessageTTRFullCallFlow(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		byeTracker:      make(map[string]byeEntry),
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

	byeOkResp := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: ttr-full-flow\r\n" +
		"CSeq: 2 BYE\r\n")

	e.handleMessage("other", "", byeOkResp)

	require.True(t, mm.ttrUpdated, "TTR should be measured during full call flow")
	require.True(t, mm.sessionCompletedFlag, "session should be completed")
}

// ==================== PDD integration tests ====================

func TestHandleMessagePDD180Ringing(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessagePDD100TryingThen180Ringing(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessagePDD183NoPDD(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessagePDDNo180NoPDD(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

	okResp := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: pdd-test-no180\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Session-Expires: 3600\r\n")

	e.handleMessage("other", "", okResp)

	require.False(t, mm.pddUpdated, "PDD should NOT be measured when no 180 received")
	require.False(t, mm.ttrUpdated, "TTR should NOT be measured when no 1xx received")
}

func TestHandleMessagePDDNonInviteResponseIgnored(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	regReq := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:user@domain>\r\n" +
		"Call-ID: pdd-non-invite\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", regReq)

	tryingResp := []byte("SIP/2.0 180 Ringing\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:user@domain>;tag=xyz\r\n" +
		"Call-ID: pdd-non-invite\r\n" +
		"CSeq: 1 REGISTER\r\n")

	e.handleMessage("other", "", tryingResp)

	require.False(t, mm.pddUpdated, "PDD should NOT be measured for REGISTER 180 Ringing")
}

func TestHandleMessageCarrierPropagationFullDialog(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessageCarrierPropagationMultiCarrierDialogs(t *testing.T) {
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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
	direction     string
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
	pbdCalls               []carrierCall
	spdCalls               []carrierCall
	sessionCompleted       []carrierCall
	invite200OK            []carrierCall
	vqReports              []carrierCall
	registerSuccess        []string
	registerFailure        []carrierFailure
	registerCountryChange  []carrierCall
	registerScan           []carrierCall
	inviteBurst            []carrierCall
	retransmissionCalls    []carrierCall
	billableCalls          []carrierCall
	packetsTotal           int
	systemErrors           int
	sessionsByCarrierAndUA map[string]map[string]int
}

func newCarrierTrackingMetricser() *carrierTrackingMetricser {
	return &carrierTrackingMetricser{
		sessionsByCarrierAndUA: make(map[string]map[string]int),
	}
}

func (m *carrierTrackingMetricser) Request(carrier, _, _, _, _, _, _, _ string, in []byte) {
	m.requests = append(m.requests, carrierCall{carrier: carrier, method: string(in)})
	m.packetsTotal++
}

func (m *carrierTrackingMetricser) Reinvite(carrier, _, _, _ string) {
	m.requests = append(m.requests, carrierCall{carrier: carrier, method: "REINVITE"})
	m.packetsTotal++
}

func (m *carrierTrackingMetricser) Response(_, _, _, _ string, _ []byte, _ bool) {
	m.packetsTotal++
}

func (m *carrierTrackingMetricser) ResponseWithMetrics(
	carrier, _, _, _ string, status []byte, isInviteResponse, is200OK bool,
) {
	m.responseWithMetrics = append(m.responseWithMetrics, carrierCall{carrier: carrier, method: string(status)})
	m.packetsTotal++
	if is200OK && isInviteResponse {
		m.invite200OK = append(m.invite200OK, carrierCall{carrier: carrier})
	}
}

func (m *carrierTrackingMetricser) Invite200OK(_, _, _, _, _, _, _, _ string) {
	// Tracking is done via ResponseWithMetrics which receives the is200OK flag.
}

func (m *carrierTrackingMetricser) SessionCompleted(carrier, _, _, _ string) {
	m.sessionCompleted = append(m.sessionCompleted, carrierCall{carrier: carrier})
}

func (m *carrierTrackingMetricser) RegisterSuccess(carrier, _, _, _ string) {
	m.registerSuccess = append(m.registerSuccess, carrier)
}

func (m *carrierTrackingMetricser) RegisterFailure(carrier, _, _, _ string, code string) {
	m.registerFailure = append(m.registerFailure, carrierFailure{carrier: carrier, code: code})
}

func (m *carrierTrackingMetricser) RegisterCountryChange(carrier, sourceCountry, _ string) {
	m.registerCountryChange = append(m.registerCountryChange,
		carrierCall{carrier: carrier, sourceCountry: sourceCountry})
}

func (m *carrierTrackingMetricser) RegisterScan(carrier, sourceCountry, _ string) {
	m.registerScan = append(m.registerScan,
		carrierCall{carrier: carrier, sourceCountry: sourceCountry})
}

func (m *carrierTrackingMetricser) InviteBurst(carrier, sourceCountry, _ string) {
	m.inviteBurst = append(m.inviteBurst,
		carrierCall{carrier: carrier, sourceCountry: sourceCountry})
}

func (m *carrierTrackingMetricser) FasCall(_, _, _, _ string) {}

func (m *carrierTrackingMetricser) SIPRetransmission(carrier, _, _, _, method string) {
	m.retransmissionCalls = append(m.retransmissionCalls,
		carrierCall{carrier: carrier, method: method})
}

func (m *carrierTrackingMetricser) UpdateShortCalls(string, string, string, string, time.Duration) {}

func (m *carrierTrackingMetricser) UpdateBillableSeconds(carrier, _, _ string, duration time.Duration) {
	m.billableCalls = append(m.billableCalls,
		carrierCall{carrier: carrier, value: duration.Seconds()})
}

func (m *carrierTrackingMetricser) UpdateRRD(carrier, _, _, _ string, delayMs float64) {
	m.rrdCalls = append(m.rrdCalls, carrierCall{carrier: carrier, value: delayMs})
}

func (m *carrierTrackingMetricser) UpdateSPD(carrier, _, _, _ string, duration time.Duration) {
	m.spdCalls = append(m.spdCalls, carrierCall{carrier: carrier, value: duration.Seconds()})
}

func (m *carrierTrackingMetricser) UpdateTTR(carrier, _, _, _ string, delayMs float64) {
	m.ttrCalls = append(m.ttrCalls, carrierCall{carrier: carrier, value: delayMs})
}

func (m *carrierTrackingMetricser) UpdatePDD(carrier, _, _, _ string, delayMs float64) {
	m.pddCalls = append(m.pddCalls, carrierCall{carrier: carrier, value: delayMs})
}

func (m *carrierTrackingMetricser) UpdateORD(carrier, _, _, _ string, delayMs float64) {
	m.ordCalls = append(m.ordCalls, carrierCall{carrier: carrier, value: delayMs})
}

func (m *carrierTrackingMetricser) UpdateLRD(carrier, _, _, _ string, delayMs float64) {
	m.lrdCalls = append(m.lrdCalls, carrierCall{carrier: carrier, value: delayMs})
}

func (m *carrierTrackingMetricser) UpdatePBD(carrier, _, _, _ string, delayMs float64) {
	m.pbdCalls = append(m.pbdCalls, carrierCall{carrier: carrier, value: delayMs})
}

func (m *carrierTrackingMetricser) UpdateSession(carrier, uaType, _, _ string, size int) {
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

func (m *carrierTrackingMetricser) ParseError(string)                  {}
func (m *carrierTrackingMetricser) SocketStats(_ []service.SocketStat) {}
func (m *carrierTrackingMetricser) RTPDropped()                        {}
func (m *carrierTrackingMetricser) UpdateChannelLength(int)            {}
func (m *carrierTrackingMetricser) UpdateChannelCapacity(int)          {}
func (m *carrierTrackingMetricser) UpdateTrackerSize(string, int)      {}
func (m *carrierTrackingMetricser) UpdateActiveDialogs(int)            {}

func (m *carrierTrackingMetricser) UpdateVQReport(carrier, uaType, _, _ string, _ *vq.SessionReport) {
	m.vqReports = append(m.vqReports, carrierCall{carrier: carrier, uaType: uaType})
}

func (m *carrierTrackingMetricser) UpdateRTPPackets(string, string, string, string, string)         {}
func (m *carrierTrackingMetricser) UpdateRTPLoss(string, string, string, string, string, uint64)    {}
func (m *carrierTrackingMetricser) UpdateRTPDuplicates(string, string, string, string, string)      {}
func (m *carrierTrackingMetricser) UpdateRTPOutOfOrder(string, string, string, string, string)      {}
func (m *carrierTrackingMetricser) UpdateRTPJitter(string, string, string, string, string, float64) {}
func (m *carrierTrackingMetricser) UpdateRTPPDV(string, string, string, string, string, float64)    {}
func (m *carrierTrackingMetricser) UpdateRTPMOS(string, string, string, string, string, float64)    {}
func (m *carrierTrackingMetricser) UpdateRTPMOSVariants(
	string, string, string, string, string, float64, float64, float64,
) {
}
func (m *carrierTrackingMetricser) UpdateRTPRFactor(string, string, string, string, string, float64) {
}
func (m *carrierTrackingMetricser) UpdateRTPLossDistribution(string, string, string, string, string, float64, float64) {
}
func (m *carrierTrackingMetricser) UpdateRTPActiveStreams(_ []service.LabeledCount) {}
func (m *carrierTrackingMetricser) RTPAliasLearned(string, string, string)          {}
func (m *carrierTrackingMetricser) RTPAliasReleased(string, string)                 {}
func (m *carrierTrackingMetricser) OneWayCall(string, string, string, string)       {}
func (m *carrierTrackingMetricser) MissingRTP(string, string, string, string)       {}

func (m *carrierTrackingMetricser) UpdateRTCPJitter(string, string, string, string, string, float64) {
}
func (m *carrierTrackingMetricser) UpdateRTCPLossFraction(string, string, string, string, string, float64) {
}
func (m *carrierTrackingMetricser) UpdateRTCPCumulativeLoss(string, string, string, string, string, uint64) {
}
func (m *carrierTrackingMetricser) UpdateRTCPRTT(string, string, string, string, string, float64) {}
func (m *carrierTrackingMetricser) UpdateRTCPReport(string, string, string, string, string)       {}
func (m *carrierTrackingMetricser) UpdateRTCPOrphan()                                             {}
func (m *carrierTrackingMetricser) RTPKernelTimestampMissing()                                    {}

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
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestMCDCTC1InviteResponseCarrierFromTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeInvite("tc1", "ft1"))
	e.handleMessage("carrier-B", "", makeInvite200OK("tc1", "ft1", "tt1"))

	require.Eventually(t, func() bool { return len(md.created) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", md.created["tc1:ft1:tt1"].carrier)
}

func TestMCDCTC2InviteResponseCarrierFallbackWithoutTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-B", "", makeInvite200OK("tc2", "ft2", "tt2"))

	require.Eventually(t, func() bool { return len(mm.responseWithMetrics) > 0 },
		100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-B", mm.responseWithMetrics[0].carrier)
}

func TestMCDCTC3RegisterResponseCarrierFromTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeRegister("tc3", "ft3"))
	e.handleMessage("carrier-B", "", makeRegister200OK("tc3", "ft3", "tt3"))

	require.Eventually(t, func() bool { return len(mm.rrdCalls) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", mm.rrdCalls[0].carrier)
}

func TestMCDCTC4RegisterResponseCarrierFallbackWithoutTracker(t *testing.T) {
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

func TestMCDCTC5TTR1xxResponseCarrierFromTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeInvite("tc5", "ft5"))
	e.handleMessage("carrier-B", "", makeTrying("tc5", "ft5", "tt5"))

	require.Eventually(t, func() bool { return len(mm.ttrCalls) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", mm.ttrCalls[0].carrier)
}

func TestMCDCTC6TTRNon1xxResponseNotMeasured(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeInvite("tc6", "ft6"))
	e.handleMessage("carrier-B", "", makeInvite200OK("tc6", "ft6", "tt6"))

	require.Eventually(t, func() bool { return len(mm.invite200OK) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Empty(t, mm.ttrCalls)
	require.False(t, e.readInviteEntry("tc6").Ok)
}

func TestMCDCTC7TTRNonInviteResponseIgnored(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeRegister("tc7", "ft7"))
	e.handleMessage("carrier-B", "", makeRegister200OK("tc7", "ft7", "tt7"))

	require.Eventually(t, func() bool { return len(mm.rrdCalls) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Empty(t, mm.ttrCalls)
}

func TestMCDCTC8DialogCreatedWithTrackerCarrierMismatch(t *testing.T) {
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

func TestMCDCTC9DialogCreatedWithTrackerCarrierSameCarrier(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeInvite("tc9", "ft9"))
	e.handleMessage("carrier-A", "", makeInvite200OK("tc9", "ft9", "tt9"))

	require.Eventually(t, func() bool { return len(md.created) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", md.created["tc9:ft9:tt9"].carrier)
}

func TestMCDCTC10Bye200OKCarrierFromDialog(t *testing.T) {
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

func TestMCDCTC11Bye200OKNonExistingDialogNoMetrics(t *testing.T) {
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

func TestMCDCTC12DialogExpiryCarrierFromDialog(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{
		cleanupResults: []service.CleanupResult{
			{Duration: 5 * time.Minute, Carrier: "carrier-A"},
		},
	}
	e := newTestExporter(mm, md)

	results := e.services.dialoger.Cleanup()
	for _, r := range results {
		e.services.metricser.SessionCompleted(r.Carrier, r.UAType, r.SourceCountry, "")
		e.services.metricser.UpdateSPD(r.Carrier, r.UAType, r.SourceCountry, "", r.Duration)
	}

	require.Len(t, mm.sessionCompleted, 1)
	require.Equal(t, "carrier-A", mm.sessionCompleted[0].carrier)
	require.Equal(t, "carrier-A", mm.spdCalls[0].carrier)
}

func TestBillableBye200OK(t *testing.T) {
	tests := []struct {
		name         string
		preCreate    bool
		wantBillable bool
	}{
		{"existing_dialog_emits_billable", true, true},
		{"non_existing_dialog_no_billable", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := newCarrierTrackingMetricser()
			md := &mockDialoger{}
			e := newTestExporter(mm, md)

			if tt.preCreate {
				e.handleMessage("carrier-A", "", makeInvite("bill-bye", "ft1"))
				e.handleMessage("carrier-A", "", makeInvite200OK("bill-bye", "ft1", "tt1"))
				require.Eventually(t, func() bool { return len(md.created) > 0 },
					100*time.Millisecond, 10*time.Millisecond)
			}

			e.handleMessage("carrier-A", "", makeBye200OK("bill-bye", "ft1", "tt1"))
			require.Eventually(t,
				func() bool { return len(mm.sessionCompleted) > 0 || !tt.preCreate },
				100*time.Millisecond, 10*time.Millisecond)

			if tt.wantBillable {
				require.Len(t, mm.billableCalls, 1, "UpdateBillableSeconds must be called on teardown")
				require.Equal(t, "carrier-A", mm.billableCalls[0].carrier)
			} else {
				require.Empty(t, mm.billableCalls, "UpdateBillableSeconds must NOT be called when Duration == 0")
			}
		})
	}
}

func TestBillableCleanupExpiry(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{
		cleanupResults: []service.CleanupResult{
			{Duration: 5 * time.Minute, Carrier: "carrier-A", DestinationCountry: "RU"},
		},
	}
	e := newTestExporter(mm, md)

	results := e.services.dialoger.Cleanup()
	for _, r := range results {
		e.services.metricser.SessionCompleted(r.Carrier, r.UAType, r.SourceCountry, "")
		e.services.metricser.UpdateSPD(r.Carrier, r.UAType, r.SourceCountry, "", r.Duration)
		e.services.metricser.UpdateShortCalls(r.Carrier, r.UAType, r.SourceCountry, "", r.Duration)
		e.services.metricser.UpdateBillableSeconds(r.Carrier, r.DestinationCountry, "", r.Duration)
	}

	require.Len(t, mm.billableCalls, 1)
	require.Equal(t, "carrier-A", mm.billableCalls[0].carrier)
	require.InDelta(t, 300.0, mm.billableCalls[0].value, 0.01)
}

func TestMCDCTC13DialogExpiryDifferentCarrier(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{
		cleanupResults: []service.CleanupResult{
			{Duration: 3 * time.Minute, Carrier: "carrier-B"},
		},
	}
	e := newTestExporter(mm, md)

	results := e.services.dialoger.Cleanup()
	for _, r := range results {
		e.services.metricser.SessionCompleted(r.Carrier, r.UAType, r.SourceCountry, "")
		e.services.metricser.UpdateSPD(r.Carrier, r.UAType, r.SourceCountry, "", r.Duration)
	}

	require.Equal(t, "carrier-B", mm.sessionCompleted[0].carrier)
}

func TestMCDCTC14Register200OKRRDCarrierFromTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeRegister("tc14", "ft14"))
	e.handleMessage("carrier-B", "", makeRegister200OK("tc14", "ft14", "tt14"))

	require.Eventually(t, func() bool { return len(mm.rrdCalls) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", mm.rrdCalls[0].carrier)
	require.Greater(t, mm.rrdCalls[0].value, 0.0)
}

func TestMCDCTC15Register3xxLRDCarrierFromTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeRegister("tc15", "ft15"))
	e.handleMessage("carrier-B", "", makeRegister3xx("tc15", "ft15", "tt15"))

	require.Eventually(t, func() bool { return len(mm.lrdCalls) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", mm.lrdCalls[0].carrier)
}

func TestMCDCTC16OptionsResponseORDCarrierFromTracker(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeOptions("tc16", "ft16"))
	e.handleMessage("carrier-B", "", makeOptions200OK("tc16", "ft16", "tt16"))

	require.Eventually(t, func() bool { return len(mm.ordCalls) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", mm.ordCalls[0].carrier)
}

func TestMCDCTC17MultiCarrierCorrectAttribution(t *testing.T) {
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

func TestMCDCTC18TrackerTTLExpiredFallbackToPacketCarrier(t *testing.T) {
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

func TestMCDCTC19RetransmitPreservesOriginalCarrier(t *testing.T) {
	mm := newCarrierTrackingMetricser()
	md := &mockDialoger{}
	e := newTestExporter(mm, md)

	e.handleMessage("carrier-A", "", makeInvite("tc19", "ft19"))
	e.handleMessage("carrier-B", "", makeInvite("tc19", "ft19"))
	e.handleMessage("carrier-C", "", makeInvite200OK("tc19", "ft19", "tt19"))

	require.Eventually(t, func() bool { return len(md.created) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "carrier-A", md.created["tc19:ft19:tt19"].carrier,
		"retransmitted INVITE must not overwrite original carrier in tracker")
}

func TestMCDCTC20OtherCarrier20Known10Other(t *testing.T) {
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

func TestHandleMessageCANCELRemovesInviteTracker(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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
		return e.readInviteEntry("cancel-test-1").Ok
	}, 100*time.Millisecond, 10*time.Millisecond, "inviteTracker should have entry after INVITE")

	cancelPkt := []byte("CANCEL sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: cancel-test-1\r\n" +
		"CSeq: 2 CANCEL\r\n")

	err = e.handleMessage("other", "", cancelPkt)
	require.NoError(t, err)

	ok := e.readInviteEntry("cancel-test-1").Ok
	require.False(t, ok, "inviteTracker entry should be removed after CANCEL")
}

func TestHandleMessageCANCELNoEntryNoOp(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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

func TestHandleMessageCANCELThenProvisionalNoTTR(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
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
		return e.readInviteEntry("cancel-ttr-test").Ok
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

func TestHandleRequestPUBLISHVQReport(t *testing.T) {
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

func TestHandleRequestNOTIFYVQReport(t *testing.T) {
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

func TestHandleRequestPUBLISHNoVQContentType(t *testing.T) {
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

func TestHandleRequestNOTIFYNoVQContentType(t *testing.T) {
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

func TestHandleRequestPUBLISHVQEmptyBody(t *testing.T) {
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

func TestHandleRequestNOTIFYVQInvalidBody(t *testing.T) {
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

// TestExporterGracefulShutdown verifies that readSocket exits cleanly when
// Close() is called (no EBADF spin loop), and that Close() completes within a
// reasonable timeout. readPackets and sipDialogMetricsUpdate also receive the
// done signal and wind down asynchronously.
func TestExporterGracefulShutdown(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	require.NoError(t, err)
	defer unix.Close(fds[1])

	tv := &unix.Timeval{Sec: 1}
	require.NoError(t, unix.SetsockoptTimeval(fds[0], unix.SOL_SOCKET, unix.SO_RCVTIMEO, tv))

	e := &exporter{
		socks:    []sockEntry{{fd: fds[0], iface: "test"}},
		messages: make(chan *rawPacket, 10),
		done:     make(chan struct{}),
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
		mediaTracker:    mediatracker.NewTracker(30 * time.Second),
		sipPortSets:     [][]uint16{{5060, 5061}},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker:  make(map[string]optionsEntry),
	}

	e.wg.Add(1)
	go e.readPackets()
	e.wg.Add(1)
	go e.readSocket(0)
	e.wg.Add(1)
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

// bpfObjectPath resolves the path to bin/sip.o relative to the package
// directory. go test runs from the package dir, so ../../bin/sip.o reaches
// the project root.
const bpfObjectPath = "../../bin/sip.o"

// countOpenFDs returns the number of currently open file descriptors of the
// process by listing /proc/self/fd. Used to detect FD leaks across an
// operation.
func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	return len(entries)
}

// newRollbackExporter builds a minimally-initialised *exporter suitable for
// exercising Initialize() — all maps and services required by the production
// code path are allocated.
func newRollbackExporter() *exporter {
	return &exporter{
		messages:        make(chan *rawPacket, messagesChanSize),
		done:            make(chan struct{}),
		services:        services{metricser: &mockMetricser{}, dialoger: &mockDialoger{}},
		mediaTracker:    mediatracker.NewTracker(30 * time.Second),
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker:  make(map[string]optionsEntry),
	}
}

func TestCaptureStartupSummary(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	t.Cleanup(zap.ReplaceGlobals(zap.New(core)))

	logCaptureConfigured(InitConfig{
		Interfaces:     []string{"eth0", "eth1"},
		SIPPorts:       [][]uint16{{5060, 5061}, {5060}},
		IgnoreOutgoing: true,
	})

	captureLogs := logs.FilterMessage("capture configured").All()
	require.Len(t, captureLogs, 1)
	fields := captureLogs[0].ContextMap()
	require.Equal(t, []any{"eth0", "eth1"}, fields["interfaces"])
	require.Equal(t, [][]uint16{{5060, 5061}, {5060}}, fields["sip_ports"])
	require.Equal(t, "host", fields["capture_mode"])
	require.Equal(t, "sdp-strict", fields["rtp_filter"])
	require.Equal(t, true, fields["ignore_outgoing"])
	require.NotContains(t, fields, "call_id")
	require.NotContains(t, fields, "src_ip")
	require.NotContains(t, fields, "dst_ip")
}

// TestInitializeRollbackOnInvalidInterface verifies that when Initialize
// fails partway through interface processing (some sockets already created),
// all created sockets are closed, the BPF collection is released, e.socks and
// e.collection are left in a clean state, and no file descriptors leak.
//
// Covers risk R2 (Initialize partial failure → FD/collection leak) from
// SPRINT_014.
func TestInitializeRollbackOnInvalidInterface(t *testing.T) {
	if syscall.Geteuid() != 0 {
		t.Skip("requires root privileges for AF_PACKET")
	}

	before := countOpenFDs(t)

	e := newRollbackExporter()
	err := e.Initialize(InitConfig{
		Interfaces:     []string{"lo", "nonexistent0"},
		BPFPath:        bpfObjectPath,
		SIPPorts:       [][]uint16{{5060, 5061}, {5060, 5061}},
		IgnoreOutgoing: true,
	})

	// "lo" succeeds, "nonexistent0" fails during net.InterfaceByName lookup.
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonexistent0")

	// Rollback must clear partial state.
	require.Empty(t, e.socks, "e.socks must be empty after rollback")
	require.Empty(t, e.collections, "e.collections must be empty after rollback")

	// All sockets created for "lo" must have been closed → no FD leak.
	after := countOpenFDs(t)
	require.Equal(t, before, after, "FD leak detected after failed Initialize")
}

// TestInitializeFirstInterfaceInvalid covers the case where the very first
// interface fails: createdSocks is empty, rollback loop closes nothing, but
// the collection must still be released and e.collection set to nil.
func TestInitializeFirstInterfaceInvalid(t *testing.T) {
	if syscall.Geteuid() != 0 {
		t.Skip("requires root privileges for AF_PACKET")
	}

	before := countOpenFDs(t)

	e := newRollbackExporter()
	err := e.Initialize(InitConfig{
		Interfaces:     []string{"definitely_missing0"},
		BPFPath:        bpfObjectPath,
		SIPPorts:       [][]uint16{{5060, 5061}},
		IgnoreOutgoing: true,
	})

	require.Error(t, err)
	require.Empty(t, e.socks)
	require.Empty(t, e.collections)

	after := countOpenFDs(t)
	require.Equal(t, before, after, "FD leak detected")
}

// TestReadSocketStatsAggregation verifies that readSocketStats sums
// PACKET_STATISTICS across multiple sockets rather than reading only one.
// Two AF_PACKET sockets are bound to lo; traffic is generated by sending UDP
// packets to 127.0.0.1; both sockets should observe packets, and the
// aggregated count must exceed what either single socket reports.
func TestReadSocketStatsAggregation(t *testing.T) {
	if syscall.Geteuid() != 0 {
		t.Skip("requires root privileges for AF_PACKET")
	}

	collection, err := ebpf.LoadCollection(bpfObjectPath)
	require.NoError(t, err)
	defer collection.Close()

	prog := collection.Programs["bpf_socket_filter"]
	require.NotNil(t, prog, "bpf_socket_filter program not found")
	progFD := prog.FD()

	// ignoreOutgoing=false so each send on lo produces both TX and RX
	// captures, doubling the chance of non-zero stats.
	sock1, err := createSocketForInterface("lo", progFD, false)
	require.NoError(t, err)
	defer unix.Close(sock1)

	sock2, err := createSocketForInterface("lo", progFD, false)
	require.NoError(t, err)
	defer unix.Close(sock2)

	e := &exporter{socks: []sockEntry{{fd: sock1, iface: "lo"}, {fd: sock2, iface: "lo"}}}

	// Drain any packets already queued on the fresh sockets so the baseline
	// for PACKET_STATISTICS is clean.
	drain := func(sock int) {
		buf := make([]byte, readBufSize)
		for {
			n, readErr := unix.Read(sock, buf)
			if readErr != nil || n == 0 {
				return
			}
		}
	}
	drain(sock1)
	drain(sock2)

	// Reset stats counters by reading them once.
	_ = e.readSocketStats()

	// Generate traffic: 5 SIP/UDP packets to 127.0.0.1:5060. No listener
	// is needed — the packets still traverse lo and are captured.
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5060})
	require.NoError(t, err)
	defer conn.Close()

	payload := []byte("INVITE sip:t@127.0.0.1 SIP/2.0\r\nContent-Length: 0\r\n\r\n")
	for range 5 {
		_, werr := conn.Write(payload)
		require.NoError(t, werr)
	}

	// Give the kernel a moment to enqueue packets onto the AF_PACKET sockets.
	time.Sleep(200 * time.Millisecond)

	drain(sock1)
	drain(sock2)

	stats := e.readSocketStats()
	var totalPackets uint32
	for _, s := range stats {
		totalPackets += s.Received
	}
	require.Positive(t, totalPackets,
		"aggregated packet count must be > 0 across two sockets on lo")
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

func buildZeroIHLPacket() []byte {
	pkt := make([]byte, 42)
	pkt[12] = 0x08
	pkt[13] = 0x00
	pkt[14] = 0x40 // version=4, IHL=0
	pkt[23] = 17
	return pkt
}

func TestIsSIPMethod(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"INVITE", []byte("INVITE sip:test SIP/2.0"), true},
		{"ACK", []byte("ACK sip:test SIP/2.0"), true},
		{"SIP response", []byte("SIP/2.0 200 OK"), true},
		{"INFORMATIONAL", []byte("INFORMATIONAL sip:test SIP/2.0"), false},
		{"OPTIONS-long", []byte("OPTIONScustom sip:test"), false},
		{"garbage", []byte("GARBAGE data"), false},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isSIPMethod(tt.data))
		})
	}
}

func TestIsSDPContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType []byte
		want        bool
	}{
		{"lowercase", []byte("application/sdp"), true},
		{"uppercase", []byte("APPLICATION/SDP"), true},
		{"mixed-case", []byte("Application/SDP"), true},
		{"with charset", []byte("application/sdp; charset=utf-8"), true},
		{"not sdp", []byte("text/plain"), false},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isSDPContentType(tt.contentType))
		})
	}
}

func TestIsVQContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType []byte
		want        bool
	}{
		{"lowercase", []byte("application/vq-rtcpxr"), true},
		{"uppercase", []byte("APPLICATION/VQ-RTCPXR"), true},
		{"mixed-case", []byte("Application/VQ-RTCPXR"), true},
		{"not vq", []byte("application/sdp"), false},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isVQContentType(tt.contentType))
		})
	}
}

func TestIsSIPPacket(t *testing.T) {
	ports := []uint16{5060, 5061}

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
		{"zero IHL", buildZeroIHLPacket(), true},
		{"too short", make([]byte, 10), true},
		{"empty", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isSIPPacket(tt.pkt, ports))
		})
	}
}

func TestSendPacketRTPDropWhenFull(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		messages: make(chan *rawPacket, 1),
		done:     make(chan struct{}),
		services: services{metricser: mm},
	}
	fillPkt := rawPacket{}
	e.messages <- &fillPkt // fill the channel

	// RTP packet (non-blocking) → dropped, sendPacket returns true
	rtpPkt := buildUDPPacket(12345, 5004)
	require.True(t, e.sendPacket(&rawPacket{data: rtpPkt}, []uint16{5060, 5061}), "RTP sendPacket should not block")
	require.Equal(t, 1, mm.rtpDroppedCount, "RTPDropped should be called when channel is full")

	// SIP packet (blocking) → would block, but we signal done to unblock
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(e.done)
	}()
	sipPkt := buildUDPPacket(12345, 5060)
	require.False(
		t,
		e.sendPacket(&rawPacket{data: sipPkt}, []uint16{5060, 5061}),
		"SIP sendPacket should return false on done",
	)
}

func TestSendPacketSuccessPaths(t *testing.T) {
	t.Run("SIP success", func(t *testing.T) {
		e := &exporter{
			messages: make(chan *rawPacket, 1),
			done:     make(chan struct{}),
		}
		sipPkt := buildUDPPacket(12345, 5060)
		require.True(t, e.sendPacket(&rawPacket{data: sipPkt}, []uint16{5060, 5061}))
		require.Len(t, e.messages, 1)
	})

	t.Run("RTP success", func(t *testing.T) {
		e := &exporter{
			messages: make(chan *rawPacket, 1),
			done:     make(chan struct{}),
		}
		rtpPkt := buildUDPPacket(12345, 5004)
		require.True(t, e.sendPacket(&rawPacket{data: rtpPkt}, []uint16{5060, 5061}))
		require.Len(t, e.messages, 1)
	})

	t.Run("RTP done signal", func(t *testing.T) {
		e := &exporter{
			messages: make(chan *rawPacket), // zero-capacity → always full
			done:     make(chan struct{}),
		}
		close(e.done)
		rtpPkt := buildUDPPacket(12345, 5004)
		require.False(
			t,
			e.sendPacket(&rawPacket{data: rtpPkt}, []uint16{5060, 5061}),
			"RTP sendPacket should return false on done",
		)
	})
}

func buildIPHeader(srcIP, dstIP [4]byte) []byte {
	hdr := make([]byte, 20)
	hdr[12] = srcIP[0]
	hdr[13] = srcIP[1]
	hdr[14] = srcIP[2]
	hdr[15] = srcIP[3]
	hdr[16] = dstIP[0]
	hdr[17] = dstIP[1]
	hdr[18] = dstIP[2]
	hdr[19] = dstIP[3]
	return hdr
}

func TestResolveCarrier(t *testing.T) {
	resolver, err := carriers.NewResolver([]carriers.Carrier{
		{Name: "provider-a", Country: "US", CIDRs: []string{"10.1.0.0/16"}},
		{Name: "provider-b", Country: "DE", CIDRs: []string{"10.2.0.0/16"}},
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		resolver    *carriers.Resolver
		srcIP       [4]byte
		dstIP       [4]byte
		wantCarrier string
		wantCountry string
	}{
		{
			name:        "nil_resolver_returns_default",
			resolver:    nil,
			wantCarrier: "other",
			wantCountry: "",
		},
		{
			name:        "srcIP_matches",
			resolver:    resolver,
			srcIP:       [4]byte{10, 1, 0, 1},
			dstIP:       [4]byte{10, 3, 0, 1},
			wantCarrier: "provider-a",
			wantCountry: "US",
		},
		{
			name:        "srcIP_no_match_dstIP_matches",
			resolver:    resolver,
			srcIP:       [4]byte{10, 9, 0, 1},
			dstIP:       [4]byte{10, 2, 0, 1},
			wantCarrier: "provider-b",
			wantCountry: "DE",
		},
		{
			name:        "neither_matches",
			resolver:    resolver,
			srcIP:       [4]byte{10, 9, 0, 1},
			dstIP:       [4]byte{10, 9, 0, 2},
			wantCarrier: "other",
			wantCountry: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &exporter{carrierResolver: tc.resolver}
			ipHdr := buildIPHeader(tc.srcIP, tc.dstIP)
			carrier, country := e.resolveCarrier(ipHdr)
			require.Equal(t, tc.wantCarrier, carrier)
			require.Equal(t, tc.wantCountry, country)
		})
	}
}

func TestResolveSourceCountry(t *testing.T) {
	geoipReader := &geoip.Reader{}

	tests := []struct {
		name           string
		carrierCountry string
		geoip          *geoip.Reader
		want           string
	}{
		{
			name:           "carrierCountry_non_empty",
			carrierCountry: "FR",
			geoip:          geoipReader,
			want:           "FR",
		},
		{
			name:           "carrierCountry_empty_geoip_nil",
			carrierCountry: "",
			geoip:          nil,
			want:           "unknown",
		},
		{
			name:           "carrierCountry_empty_geoip_no_db",
			carrierCountry: "",
			geoip:          geoipReader,
			want:           "unknown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &exporter{geoip: tc.geoip}
			ipHdr := buildIPHeader([4]byte{10, 1, 0, 1}, [4]byte{10, 2, 0, 1})
			got := e.resolveSourceCountry(tc.carrierCountry, ipHdr)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestSIPDialogMetricsUpdateTrackerLenNoRace verifies that len() calls on
// registerTracker, inviteTracker, and optionsTracker in sipDialogMetricsUpdate
// do not race with concurrent writes from readPackets.
//
// Run with: go test -race -run TestSIPDialogMetricsUpdateTrackerLenNoRace
//
// Before S14-7.2 fix, the three len() calls at exporter.go:516-518 read map
// headers without holding the matching mutex, while readPackets (simulated
// here by a writer goroutine) mutates those maps under lock. Under -race this
// produces "concurrent map read and map write" — a fatal runtime error.
func TestSIPDialogMetricsUpdateTrackerLenNoRace(t *testing.T) {
	e := newRollbackExporter()

	// Writer goroutine: intensively writes/deletes tracker entries under locks,
	// simulating the readPackets consumer mutating maps while the metrics
	// goroutine tries to read len().
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		deadline := time.Now().Add(2 * time.Second)
		i := 0
		for time.Now().Before(deadline) {
			key := fmt.Sprintf("k%d", i)
			e.registerMutex.Lock()
			e.registerTracker[key] = registerEntry{timestamp: time.Now()}
			e.registerMutex.Unlock()

			e.inviteMutex.Lock()
			e.inviteTracker[key] = inviteEntry{timestamp: time.Now()}
			e.inviteMutex.Unlock()

			e.optionsMutex.Lock()
			e.optionsTracker[key] = optionsEntry{timestamp: time.Now()}
			e.optionsMutex.Unlock()

			if i > 100 {
				oldKey := fmt.Sprintf("k%d", i-100)
				e.registerMutex.Lock()
				delete(e.registerTracker, oldKey)
				e.registerMutex.Unlock()
			}
			i++
		}
	}()

	// Start metrics update goroutine (reads len() of trackers every 1s).
	e.wg.Add(1)
	go e.sipDialogMetricsUpdate()

	select {
	case <-writerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("writer did not finish within 3s")
	}

	require.NotPanics(t, func() { e.Close() })
}

// TestReadSocketFailStopNoSystemError verifies that readSocket returns cleanly
// without incrementing SystemError when the socket becomes invalid (EBADF).
// This is the same return-path used for ENETDOWN/ENODEV hot-unplug (S14-7.1):
// the goroutine stops silently rather than spamming Error+SystemError every
// second on a dead NIC.
//
// A dedicated ENETDOWN test requires veth-pair infrastructure (root-gated)
// and is deferred to S15 (S14-7.5 readSocket error-branch MC/DC tests).
func TestReadSocketFailStopNoSystemError(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	require.NoError(t, err)
	defer unix.Close(fds[1])

	tv := &unix.Timeval{Sec: 1}
	require.NoError(t, unix.SetsockoptTimeval(fds[0], unix.SOL_SOCKET, unix.SO_RCVTIMEO, tv))

	mm := &mockMetricser{}
	e := &exporter{
		socks:       []sockEntry{{fd: fds[0]}},
		sipPortSets: [][]uint16{{5060, 5061}},
		messages:    make(chan *rawPacket, 10),
		done:        make(chan struct{}),
		services:    services{metricser: mm, dialoger: &mockDialoger{}},
	}

	e.wg.Add(1)
	go e.readSocket(0)

	// Close the FD → EBADF in readSocket → clean return, no SystemError.
	unix.Close(fds[0])

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("readSocket did not exit within 3s after FD closed")
	}

	require.False(t, mm.systemErrorCalled,
		"SystemError should not be called on fail-stop (EBADF/ENETDOWN/ENODEV)")
}

func TestDirectionFromPkttype(t *testing.T) {
	tests := []struct {
		name       string
		pkttype    uint8
		isResponse bool
		want       string
	}{
		{"request_host_inbound", unix.PACKET_HOST, false, directionInbound},
		{"request_outgoing_outbound", unix.PACKET_OUTGOING, false, directionOutbound},
		{"response_host_outbound", unix.PACKET_HOST, true, directionOutbound},
		{"response_outgoing_inbound", unix.PACKET_OUTGOING, true, directionInbound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, directionFromPkttype(tt.pkttype, tt.isResponse))
		})
	}
}

type directionTrackingMetricser struct {
	mockMetricser

	requestDirections  []string
	responseDirections []string
}

func (m *directionTrackingMetricser) Request(_, _, _, _, _, _, _, direction string, _ []byte) {
	m.requestDirections = append(m.requestDirections, direction)
}

func (m *directionTrackingMetricser) ResponseWithMetrics(_, _, _, direction string, _ []byte, _, _ bool) {
	m.responseDirections = append(m.responseDirections, direction)
}

func TestHandleMessageDirectionFromPkttype(t *testing.T) {
	tests := []struct {
		name      string
		pktType   uint8
		isRequest bool
		wantDir   string
	}{
		{"request_host_inbound", unix.PACKET_HOST, true, directionInbound},
		{"request_outgoing_outbound", unix.PACKET_OUTGOING, true, directionOutbound},
		{"response_host_outbound", unix.PACKET_HOST, false, directionOutbound},
		{"response_outgoing_inbound", unix.PACKET_OUTGOING, false, directionInbound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := &directionTrackingMetricser{}
			md := &mockDialoger{}

			e := &exporter{
				services: services{
					metricser: mm,
					dialoger:  md,
				},
				inviteTracker:  make(map[string]inviteEntry),
				inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
				optionsTracker: make(map[string]optionsEntry),
				mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
			}
			e.pktType = tt.pktType

			callID := tt.name
			if tt.isRequest {
				err := e.handleMessage("carrier", "", makeInvite(callID, "ft"))
				require.NoError(t, err)
				require.Len(t, mm.requestDirections, 1)
				require.Equal(t, tt.wantDir, mm.requestDirections[0])
			} else {
				err := e.handleMessage("carrier", "", makeTrying(callID, "ft", "tt"))
				require.NoError(t, err)
				require.Len(t, mm.responseDirections, 1)
				require.Equal(t, tt.wantDir, mm.responseDirections[0])
			}
		})
	}
}

func TestIPPortToKey(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		port     uint16
		wantOK   bool
		wantIP   uint32
		wantPort uint16
	}{
		{
			name:     "valid IPv4",
			ip:       "192.168.1.1",
			port:     5004,
			wantOK:   true,
			wantIP:   0xC0A80101,
			wantPort: 5004,
		},
		{
			name:     "localhost",
			ip:       "127.0.0.1",
			port:     5060,
			wantOK:   true,
			wantIP:   0x7F000001,
			wantPort: 5060,
		},
		{
			name:   "invalid IP",
			ip:     "not-an-ip",
			port:   5004,
			wantOK: false,
		},
		{
			name:   "empty IP",
			ip:     "",
			port:   5004,
			wantOK: false,
		},
		{
			name:   "IPv6 not supported",
			ip:     "::1",
			port:   5004,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := ipPortToKey(tt.ip, tt.port)
			require.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				require.Equal(t, tt.wantIP, key.IP)
				require.Equal(t, tt.wantPort, key.Port)
			}
		})
	}
}

func TestRTPEndpointRetainRelease(t *testing.T) {
	endpoint := rtpEndpointKey{IP: 0xC000020A, Port: 5004}
	firstMap := &fakeRTPEndpointMap{}
	secondMap := &fakeRTPEndpointMap{}
	e := &exporter{
		rtpEndpointsMaps: []rtpEndpointMap{firstMap, secondMap},
		rtpEndpointRefs:  make(map[rtpEndpointKey]uint),
	}

	e.retainRTPEndpoint("192.0.2.10", 5004)
	e.retainRTPEndpoint("192.0.2.10", 5004)
	require.Equal(t, uint(2), e.rtpEndpointRefs[endpoint])
	wantUpdates := []rtpEndpointMapUpdate{{key: endpoint, value: 1, flags: ebpf.UpdateAny}}
	require.Equal(t, wantUpdates, firstMap.updates)
	require.Equal(t, wantUpdates, secondMap.updates)

	e.releaseRTPEndpoint("192.0.2.10", 5004)
	require.Equal(t, uint(1), e.rtpEndpointRefs[endpoint])
	require.Empty(t, firstMap.deletes)
	require.Empty(t, secondMap.deletes)

	e.releaseRTPEndpoint("192.0.2.10", 5004)
	_, ok := e.rtpEndpointRefs[endpoint]
	require.False(t, ok)
	require.Equal(t, []rtpEndpointKey{endpoint}, firstMap.deletes)
	require.Equal(t, []rtpEndpointKey{endpoint}, secondMap.deletes)
}

func TestRTPEndpointRefcountIgnoresUnsupportedEndpoints(t *testing.T) {
	e := &exporter{
		rtpEndpointRefs: make(map[rtpEndpointKey]uint),
	}

	e.retainRTPEndpoint("2001:db8::1", 5004)
	e.releaseRTPEndpoint("2001:db8::1", 5004)
	e.releaseRTPEndpoint("192.0.2.10", 5004)

	require.Empty(t, e.rtpEndpointRefs)
}

func TestRTPEndpointUpdateFailureReconcilesDesiredPresence(t *testing.T) {
	endpoint := rtpEndpointKey{IP: 0xC000020A, Port: 5004}
	firstMap := &fakeRTPEndpointMap{updateError: errors.New("update failed")}
	secondMap := &fakeRTPEndpointMap{}
	e := &exporter{
		rtpEndpointsMaps: []rtpEndpointMap{firstMap, secondMap},
		rtpEndpointRefs:  make(map[rtpEndpointKey]uint),
	}

	e.retainRTPEndpoint("192.0.2.10", 5004)

	require.Equal(t, uint(1), e.rtpEndpointRefs[endpoint])
	require.Equal(t, map[rtpEndpointKey]bool{endpoint: true}, e.dirtyRTPEndpoints)
	require.False(t, firstMap.present[endpoint])
	require.True(t, secondMap.present[endpoint])

	firstMap.updateError = nil
	e.reconcileRTPEndpoints()

	require.True(t, firstMap.present[endpoint])
	require.True(t, secondMap.present[endpoint])
	require.Empty(t, e.dirtyRTPEndpoints)
	firstUpdates, secondUpdates := len(firstMap.updates), len(secondMap.updates)
	e.reconcileRTPEndpoints()
	require.Len(t, firstMap.updates, firstUpdates)
	require.Len(t, secondMap.updates, secondUpdates)
}

func TestRTPEndpointDeleteFailureReconcilesDesiredAbsence(t *testing.T) {
	endpoint := rtpEndpointKey{IP: 0xC000020A, Port: 5004}
	firstMap := &fakeRTPEndpointMap{}
	secondMap := &fakeRTPEndpointMap{}
	e := &exporter{
		rtpEndpointsMaps: []rtpEndpointMap{firstMap, secondMap},
		rtpEndpointRefs:  make(map[rtpEndpointKey]uint),
	}
	e.retainRTPEndpoint("192.0.2.10", 5004)
	firstMap.deleteError = errors.New("delete failed")
	delete(secondMap.present, endpoint)

	e.releaseRTPEndpoint("192.0.2.10", 5004)

	require.NotContains(t, e.rtpEndpointRefs, endpoint)
	require.Equal(t, map[rtpEndpointKey]bool{endpoint: false}, e.dirtyRTPEndpoints)
	require.True(t, firstMap.present[endpoint])

	firstMap.deleteError = nil
	e.reconcileRTPEndpoints()

	require.False(t, firstMap.present[endpoint])
	require.Empty(t, e.dirtyRTPEndpoints)
}

func TestRegisterMediaEndpointsRetainsDuplicateSDPEndpointOnce(t *testing.T) {
	e := &exporter{
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
		rtpEndpointRefs: make(map[rtpEndpointKey]uint),
	}
	body := []byte("v=0\r\nc=IN IP4 192.0.2.10\r\nm=audio 5004 RTP/AVP 8\r\na=rtpmap:8 PCMA/8000\r\n")
	labels := mediatracker.MediaLabels{CallID: "call-1"}

	e.registerMediaEndpoints(body, labels)
	e.registerMediaEndpoints(body, labels)

	require.Equal(t, uint(1), e.rtpEndpointRefs[rtpEndpointKey{IP: 0xC000020A, Port: 5004}])
	require.Equal(t, uint(1), e.rtpEndpointRefs[rtpEndpointKey{IP: 0xC000020A, Port: 5005}])
}

// ==================== S10-R1: Exporter-layer wiring tests ====================

func TestHandleRequestRetransmissionMetric(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  &mockDialoger{},
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker:  make(map[string]optionsEntry),
		byeTracker:      make(map[string]byeEntry),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	invite := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: retrans-1\r\n" +
		"CSeq: 1 INVITE\r\n")

	e.handleMessage("other", "", invite)
	require.Equal(t, 1, mm.requestCount, "first INVITE should call Request")
	require.Equal(t, 0, mm.sipRetransmissionCalls, "first INVITE should not trigger retransmission")

	e.handleMessage("other", "", invite)
	require.Equal(t, 1, mm.requestCount, "retransmitted INVITE should NOT call Request again")
	require.Equal(t, 1, mm.sipRetransmissionCalls, "retransmitted INVITE should trigger SipRetransmission")
}

func TestRTPHandleRTPOutOfOrderMetric(t *testing.T) {
	mm := &mockMetricser{}
	e := &exporter{
		services:       services{metricser: mm, dialoger: &mockDialoger{}},
		inviteTracker:  make(map[string]inviteEntry),
		inviteSDP:      make(map[inviteSDPKey]inviteSDPEntity),
		optionsTracker: make(map[string]optionsEntry),
		byeTracker:     make(map[string]byeEntry),
		mediaTracker:   mediatracker.NewTracker(rtpStreamTTL),
	}
	e.mediaTracker.Register("10.0.0.1", 5004, mediatracker.MediaLabels{
		Carrier:    "c",
		UAType:     "u",
		CallID:     "call-reorder",
		SDPCodecs:  map[uint8]string{0: "PCMU"},
		ClockRates: map[uint8]uint32{0: 8000},
	})

	e.handleRTP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(0, 0, 0, 0), 0, makeRTPPayloadSeq(0xAABB, 1))
	e.handleRTP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(0, 0, 0, 0), 0, makeRTPPayloadSeq(0xAABB, 5))
	e.handleRTP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(0, 0, 0, 0), 0, makeRTPPayloadSeq(0xAABB, 3))

	require.Equal(t, 1, mm.rtpOutOfOrderCalls, "seq=3 after maxSeq=5 should trigger UpdateRTPOutOfOrder")
}

func TestHandleBye200OKPBDMetric(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		byeTracker:      make(map[string]byeEntry),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	invite := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: pbd-test\r\n" +
		"CSeq: 1 INVITE\r\n")
	e.handleMessage("other", "", invite)

	ok200 := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: pbd-test\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Session-Expires: 3600\r\n")
	e.handleMessage("other", "", ok200)

	byeReq := []byte("BYE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: pbd-test\r\n" +
		"CSeq: 2 BYE\r\n")
	e.handleMessage("other", "", byeReq)

	time.Sleep(10 * time.Millisecond)

	byeOk := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: pbd-test\r\n" +
		"CSeq: 2 BYE\r\n")
	e.handleMessage("other", "", byeOk)

	require.True(t, mm.pbdUpdated, "BYE → 200 OK BYE must trigger UpdatePBD")
	require.Greater(t, mm.pbdDelay, 0.0, "PBD delay must be positive")
}

func TestHandleBye200OKShortCallsMetric(t *testing.T) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[inviteSDPKey]inviteSDPEntity),
		byeTracker:      make(map[string]byeEntry),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
	}

	invite := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: shortcalls-test\r\n" +
		"CSeq: 1 INVITE\r\n")
	e.handleMessage("other", "", invite)

	ok200 := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: shortcalls-test\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Session-Expires: 3600\r\n")
	e.handleMessage("other", "", ok200)

	byeReq := []byte("BYE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: shortcalls-test\r\n" +
		"CSeq: 2 BYE\r\n")
	e.handleMessage("other", "", byeReq)

	byeOk := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: shortcalls-test\r\n" +
		"CSeq: 2 BYE\r\n")
	e.handleMessage("other", "", byeOk)

	require.True(t, mm.shortCallsUpdated, "BYE 200 OK with Duration > 0 must trigger UpdateShortCalls")
	require.Greater(t, mm.shortCallsDuration, time.Duration(0), "duration must be positive")
}

func TestCleanupByeTrackerTTLExpiry(t *testing.T) {
	e := &exporter{
		byeTracker: make(map[string]byeEntry),
	}

	e.byeMutex.Lock()
	e.byeTracker["expired"] = byeEntry{timestamp: time.Now().Add(-2 * time.Minute)}
	e.byeTracker["fresh"] = byeEntry{timestamp: time.Now()}
	e.byeMutex.Unlock()

	e.cleanupByeTracker()

	e.byeMutex.RLock()
	defer e.byeMutex.RUnlock()
	_, expiredGone := e.byeTracker["expired"]
	_, freshKept := e.byeTracker["fresh"]
	require.False(t, expiredGone, "expired entry must be cleaned up")
	require.True(t, freshKept, "fresh entry must survive cleanup")
}

func TestResolveRTCEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		m        sdp.Media
		wantIP   string
		wantPort uint16
		wantOK   bool
	}{
		{
			name:   "a=rtcp with unicast address",
			m:      sdp.Media{IP: "10.0.0.1", Port: 5004, RTCPPort: 5005, RTCPAddr: "10.0.0.99"},
			wantIP: "10.0.0.99", wantPort: 5005, wantOK: true,
		},
		{
			name:   "a=rtcp port only (no address)",
			m:      sdp.Media{IP: "10.0.0.1", Port: 5004, RTCPPort: 5005},
			wantIP: "10.0.0.1", wantPort: 5005, wantOK: true,
		},
		{
			name:   "rtcp-mux shares RTP port",
			m:      sdp.Media{IP: "10.0.0.1", Port: 5004, RTCPMux: true},
			wantIP: "", wantPort: 0, wantOK: false,
		},
		{
			name:   "legacy port+1",
			m:      sdp.Media{IP: "10.0.0.1", Port: 5004},
			wantIP: "10.0.0.1", wantPort: 5005, wantOK: true,
		},
		{
			name:   "port at max skips legacy",
			m:      sdp.Media{IP: "10.0.0.1", Port: maxUDPPort},
			wantIP: "", wantPort: 0, wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, port, ok := resolveRTCEndpoint(tt.m)
			require.Equal(t, tt.wantIP, ip)
			require.Equal(t, tt.wantPort, port)
			require.Equal(t, tt.wantOK, ok)
		})
	}
}
