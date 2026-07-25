package exporter

import (
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/aibudaevv/sip-exporter/internal/service"
)

const registerScanMaxEntriesPerIP = 10000

const fasMediaPacketsThreshold = 2

// fasSRTPGrace extends the FAS threshold for calls whose answer SDP carries a
// DTLS-SRTP fingerprint (a=fingerprint): ICE/DTLS setup delays first media by
// up to ~15 s after the 200 OK, which would otherwise false-positive.
const fasSRTPGrace = 15 * time.Second

// fasByeFloor is the minimum answer→BYE duration for a BYE-path FAS report.
// Below this the caller likely abandoned before the callee could send media
// (not fraud); above it with zero answer-side RTP, the dead air is suspect.
const fasByeFloor = 3 * time.Second

type registerScanTracker struct {
	mu        sync.Mutex
	threshold int
	window    time.Duration
	entries   map[string]map[string]time.Time
}

func newRegisterScanTracker(threshold int, window time.Duration) *registerScanTracker {
	if threshold <= 0 || window <= 0 {
		return nil
	}
	return &registerScanTracker{
		threshold: threshold,
		window:    window,
		entries:   make(map[string]map[string]time.Time),
	}
}

func (t *registerScanTracker) record(
	srcIP, aor, carrier, sourceCountry, direction string,
	metricser service.Metricser,
) {
	if t == nil || srcIP == "" || aor == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-t.window)

	if t.entries[srcIP] == nil {
		t.entries[srcIP] = make(map[string]time.Time)
	}

	for a, ts := range t.entries[srcIP] {
		if ts.Before(cutoff) {
			delete(t.entries[srcIP], a)
		}
	}

	if len(t.entries[srcIP]) < registerScanMaxEntriesPerIP {
		t.entries[srcIP][aor] = now
	}

	if len(t.entries[srcIP]) >= t.threshold {
		metricser.RegisterScan(carrier, sourceCountry, direction)
	}
}

func (t *registerScanTracker) cleanup() {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := time.Now().Add(-t.window)
	for ip, aors := range t.entries {
		for a, ts := range aors {
			if ts.Before(cutoff) {
				delete(aors, a)
			}
		}
		if len(aors) == 0 {
			delete(t.entries, ip)
		}
	}
}

type inviteBurstTracker struct {
	mu        sync.Mutex
	threshold int
	window    time.Duration
	entries   map[string][]time.Time
}

func newInviteBurstTracker(threshold int, window time.Duration) *inviteBurstTracker {
	if threshold <= 0 || window <= 0 {
		return nil
	}
	return &inviteBurstTracker{
		threshold: threshold,
		window:    window,
		entries:   make(map[string][]time.Time),
	}
}

func (t *inviteBurstTracker) record(
	srcIP, carrier, sourceCountry, direction string,
	metricser service.Metricser,
) {
	if t == nil || srcIP == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-t.window)

	entries := t.entries[srcIP]

	i := 0
	for i < len(entries) && entries[i].Before(cutoff) {
		i++
	}
	entries = append(entries[i:], now)

	if len(entries) > t.threshold+1 {
		entries = entries[len(entries)-t.threshold-1:]
	}

	t.entries[srcIP] = entries

	if len(entries) >= t.threshold {
		metricser.InviteBurst(carrier, sourceCountry, direction)
	}
}

func (t *inviteBurstTracker) cleanup() {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := time.Now().Add(-t.window)
	for ip, entries := range t.entries {
		i := 0
		for i < len(entries) && entries[i].Before(cutoff) {
			i++
		}
		entries = entries[i:]
		if len(entries) == 0 {
			delete(t.entries, ip)
		} else {
			t.entries[ip] = entries
		}
	}
}

type fasEntry struct {
	createdAt     time.Time
	deadline      time.Time // createdAt + threshold (+ fasSRTPGrace when SRTP); sweep fires after this
	byeFloor      time.Time // earliest teardown time at which finalizeOnBye may report FAS (= deadline for SRTP, else createdAt + fasByeFloor)
	carrier       string
	uaType        string
	sourceCountry string
	direction     string
}

// fasEndpoint identifies a media endpoint (IP:port) registered from SDP, used to
// gate FAS clearing by the originating SDP side (offer vs answer).
type fasEndpoint struct {
	ip   string
	port uint16
}

type fasTracker struct {
	mu        sync.Mutex
	threshold time.Duration
	entries   map[string]fasEntry
	// offer holds the offer-side media endpoints (from the INVITE SDP) per
	// Call-ID. Answer-side media is detected by its arrival at an offer endpoint
	// (callee→caller); only such media defeats FAS. When empty for a call (INVITE
	// SDP was not cached), the side is unknown and any media clears (legacy).
	offer map[string]map[fasEndpoint]struct{}
	now   func() time.Time
}

func newFasTracker(threshold time.Duration) *fasTracker {
	if threshold <= 0 {
		return nil
	}
	return &fasTracker{
		threshold: threshold,
		entries:   make(map[string]fasEntry),
		offer:     make(map[string]map[fasEndpoint]struct{}),
		now:       time.Now,
	}
}

// SetNow overrides the clock used for FAS timing (for testing).
func (t *fasTracker) SetNow(now func() time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.now = now
	t.mu.Unlock()
}

func (t *fasTracker) store(callID string, e fasEntry, offerEndpoints []fasEndpoint, srtp bool) {
	if t == nil || callID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	e.createdAt = now
	deadline := now.Add(t.threshold)
	// The BYE path may report FAS once the call outlived its setup tolerance. For
	// plain RTP that's fasByeFloor; for DTLS-SRTP it's the full grace window
	// (deadline) — a WebRTC call that fails ICE and hangs up is not FAS.
	byeFloor := now.Add(fasByeFloor)
	if srtp {
		deadline = deadline.Add(fasSRTPGrace)
		byeFloor = deadline
	}
	e.deadline = deadline
	e.byeFloor = byeFloor
	t.entries[callID] = e
	if len(offerEndpoints) > 0 {
		set := make(map[fasEndpoint]struct{}, len(offerEndpoints))
		for _, ep := range offerEndpoints {
			set[ep] = struct{}{}
		}
		t.offer[callID] = set
	}
}

// updateOffer replaces the offer endpoints for an existing FAS entry and extends
// the deadline when the re-INVITE answer indicates SRTP. No-op when the entry was
// already cleared (media established or sweep fired). The timer is NOT reset —
// the callee had time to send media before the re-INVITE.
func (t *fasTracker) updateOffer(callID string, offerEndpoints []fasEndpoint, srtp bool) {
	if t == nil || callID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[callID]
	if !ok {
		return
	}
	if len(offerEndpoints) > 0 {
		set := make(map[fasEndpoint]struct{}, len(offerEndpoints))
		for _, ep := range offerEndpoints {
			set[ep] = struct{}{}
		}
		t.offer[callID] = set
	} else {
		delete(t.offer, callID)
	}
	if srtp {
		graceDeadline := e.createdAt.Add(t.threshold + fasSRTPGrace)
		if e.deadline.Before(graceDeadline) {
			e.deadline = graceDeadline
			e.byeFloor = graceDeadline
		}
	}
	t.entries[callID] = e
}

func (t *fasTracker) clear(callID string) {
	if t == nil || callID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, callID)
	delete(t.offer, callID)
}

func (t *fasTracker) Size() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
}

// clearIfAnswerMedia clears the FAS pending entry for callID once the answering
// side has sent ≥ fasMediaPacketsThreshold forward RTP packets. Answer-side media
// arrives at a registered offer-side endpoint (callee→caller), so a stream keyed
// by an offer endpoint proves the answer side sent media. When no offer endpoints
// are tracked (INVITE offer SDP not cached), the side is unknown and any media
// clears the entry (legacy fallback, avoids new false FAS alerts).
func (t *fasTracker) clearIfAnswerMedia(callID string, ep fasEndpoint, packetsTotal uint64) {
	if t == nil || callID == "" || packetsTotal < fasMediaPacketsThreshold {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.entries[callID]; !ok {
		return
	}
	offer, hasOffer := t.offer[callID]
	if !hasOffer || len(offer) == 0 {
		delete(t.entries, callID)
		delete(t.offer, callID)
		return
	}
	if _, isOffer := offer[ep]; isOffer {
		delete(t.entries, callID)
		delete(t.offer, callID)
	}
}

// finalizeOnBye reports FAS at teardown when the entry is still pending (the
// answer side never sent media) and the call lasted beyond its byeFloor, then
// clears. A no-op when media already cleared the entry earlier (normal case).
// byeFloor equals fasByeFloor for plain RTP but the full deadline (threshold +
// fasSRTPGrace) for DTLS-SRTP, so a WebRTC call failing ICE and hanging up does
// not false-positive during the setup window.
// reportFAS increments the FAS counter and logs a structured warning. path is
// "sweep" (periodic threshold timeout) or "bye" (teardown with no media). Must
// be called under t.mu.
func (t *fasTracker) reportFAS(callID string, e fasEntry, path string, metricser service.Metricser) {
	metricser.FasCall(e.carrier, e.uaType, e.sourceCountry, e.direction)
	zap.L().Warn("FAS suspected: answered call with no answer-side RTP",
		zap.String("path", path),
		zap.String("call_id", callID),
		zap.String("carrier", e.carrier),
		zap.String("ua_type", e.uaType),
		zap.String("source_country", e.sourceCountry),
		zap.String("direction", e.direction),
		zap.Duration("duration", t.now().Sub(e.createdAt)),
	)
}

func (t *fasTracker) finalizeOnBye(callID string, metricser service.Metricser) {
	if t == nil || callID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[callID]
	if !ok {
		return
	}
	if t.now().After(e.byeFloor) {
		t.reportFAS(callID, e, "bye", metricser)
	}
	delete(t.entries, callID)
	delete(t.offer, callID)
}

func (t *fasTracker) sweep(metricser service.Metricser) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for callID, e := range t.entries {
		if now.After(e.deadline) {
			t.reportFAS(callID, e, "sweep", metricser)
			delete(t.entries, callID)
			delete(t.offer, callID)
		}
	}
}
