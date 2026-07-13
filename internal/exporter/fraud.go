package exporter

import (
	"sync"
	"time"

	"github.com/aibudaevv/sip-exporter/internal/service"
)

type registerScanTracker struct {
	mu        sync.Mutex
	threshold int
	window    time.Duration
	entries   map[string]map[string]time.Time
	signaled  map[string]bool
}

func newRegisterScanTracker(threshold int, window time.Duration) *registerScanTracker {
	if threshold <= 0 || window <= 0 {
		return nil
	}
	return &registerScanTracker{
		threshold: threshold,
		window:    window,
		entries:   make(map[string]map[string]time.Time),
		signaled:  make(map[string]bool),
	}
}

func (t *registerScanTracker) record(
	srcIP, aor, carrier, sourceCountry string,
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

	if len(t.entries[srcIP]) < t.threshold {
		t.signaled[srcIP] = false
	}

	if !t.signaled[srcIP] {
		t.entries[srcIP][aor] = now
	}

	count := len(t.entries[srcIP])
	if count >= t.threshold && !t.signaled[srcIP] {
		t.signaled[srcIP] = true
		metricser.RegisterScan(carrier, sourceCountry)
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
			delete(t.signaled, ip)
		}
	}
}

type inviteBurstTracker struct {
	mu        sync.Mutex
	threshold int
	window    time.Duration
	entries   map[string][]time.Time
	signaled  map[string]bool
}

func newInviteBurstTracker(threshold int, window time.Duration) *inviteBurstTracker {
	if threshold <= 0 || window <= 0 {
		return nil
	}
	return &inviteBurstTracker{
		threshold: threshold,
		window:    window,
		entries:   make(map[string][]time.Time),
		signaled:  make(map[string]bool),
	}
}

func (t *inviteBurstTracker) record(
	srcIP, carrier, sourceCountry string,
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
		if !t.signaled[srcIP] {
			t.signaled[srcIP] = true
			metricser.InviteBurst(carrier, sourceCountry)
		}
	} else {
		t.signaled[srcIP] = false
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
			delete(t.signaled, ip)
		} else {
			t.entries[ip] = entries
		}
	}
}
