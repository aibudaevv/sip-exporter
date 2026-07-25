package exporter

import (
	"sync"
	"time"

	"github.com/aibudaevv/sip-exporter/internal/service"
)

const registerScanMaxEntriesPerIP = 10000

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

type fasEntry struct {
	createdAt     time.Time
	carrier       string
	uaType        string
	sourceCountry string
	direction     string
}

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

func (t *fasTracker) store(callID string, e fasEntry) {
	if t == nil || callID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e.createdAt = time.Now()
	t.entries[callID] = e
}

func (t *fasTracker) clear(callID string) {
	if t == nil || callID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, callID)
}

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
