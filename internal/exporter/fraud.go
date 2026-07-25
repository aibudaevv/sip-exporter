package exporter

import (
	"sync"
	"time"

	"github.com/aibudaevv/sip-exporter/internal/service"
)

const registerScanMaxEntriesPerIP = 10000

// fasMediaPacketsThreshold is the minimum forward-RTP-packet count required to
// consider media established and cancel a pending FAS check. A single packet is
// not enough evidence (could be stray/spoofed); real media reaches this in ~20ms.
const fasMediaPacketsThreshold = 2

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

// fasEntry records a call awaiting media: a non-re-INVITE 200 OK was received
// for a dialog that registered media endpoints, and RTP has not yet been seen.
type fasEntry struct {
	createdAt     time.Time
	carrier       string
	uaType        string
	sourceCountry string
	direction     string
}

// fasTracker detects suspected False Answer Supervision: a 200 OK starts a
// pending entry, the first observed RTP packet for the call clears it, and a
// background sweep emits sip_exporter_fas_calls_total for any entry that outlives
// the threshold. Distinct from sessions_missing_rtp_total (a teardown metric):
// FAS is a real-time signal N seconds after answer.
type fasTracker struct {
	mu        sync.Mutex
	threshold time.Duration
	entries   map[string]fasEntry
}

func newFasTracker(threshold time.Duration) *fasTracker {
	if threshold <= 0 {
		return nil
	}
	return &fasTracker{
		threshold: threshold,
		entries:   make(map[string]fasEntry),
	}
}

// store marks a call as pending media. Called at 200 OK (non-re-INVITE) only when
// at least one media endpoint was registered (SDP not held).
func (t *fasTracker) store(callID, carrier, uaType, sourceCountry, direction string) {
	if t == nil || callID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries[callID] = fasEntry{
		createdAt:     time.Now(),
		carrier:       carrier,
		uaType:        uaType,
		sourceCountry: sourceCountry,
		direction:     direction,
	}
}

// clear drops a pending entry. Called when RTP is observed for the call, or at
// dialog teardown (BYE/expiry) so a fast call without RTP is not misreported.
func (t *fasTracker) clear(callID string) {
	if t == nil || callID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, callID)
}

// sweep emits fas_calls_total for every entry older than the threshold and
// removes it. Called from the 1-second background tick.
func (t *fasTracker) sweep(metricser service.Metricser) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := time.Now().Add(-t.threshold)
	for callID, e := range t.entries {
		if e.createdAt.Before(cutoff) {
			metricser.FasCall(e.carrier, e.uaType, e.sourceCountry, e.direction)
			delete(t.entries, callID)
		}
	}
}
