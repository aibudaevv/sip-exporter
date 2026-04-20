package service

import (
	"sync"
	"time"
)

type (
	dialogEntry struct {
		expiresAt time.Time
		createdAt time.Time
		carrier   string
	}

	CleanupResult struct {
		Duration time.Duration
		Carrier  string
	}

	dialogs struct {
		m       sync.Mutex
		storage map[string]dialogEntry
	}
	Dialoger interface {
		Create(dialogID string, expiresAt time.Time, createdAt time.Time, carrier string)
		Delete(dialogID string) CleanupResult
		Size() int
		SizeByCarrier() map[string]int
		Cleanup() []CleanupResult
	}
)

func NewDialoger() Dialoger {
	return &dialogs{
		storage: make(map[string]dialogEntry, 10_000),
	}
}

func (c *dialogs) Delete(dialogID string) CleanupResult {
	c.m.Lock()
	defer c.m.Unlock()
	entry, ok := c.storage[dialogID]
	if !ok {
		return CleanupResult{}
	}
	delete(c.storage, dialogID)
	d := time.Since(entry.createdAt)
	if d < 0 {
		return CleanupResult{Carrier: entry.carrier}
	}
	return CleanupResult{Duration: d, Carrier: entry.carrier}
}

func (c *dialogs) Create(dialogID string, expiresAt time.Time, createdAt time.Time, carrier string) {
	c.m.Lock()
	defer c.m.Unlock()
	if _, exists := c.storage[dialogID]; !exists {
		c.storage[dialogID] = dialogEntry{
			expiresAt: expiresAt,
			createdAt: createdAt,
			carrier:   carrier,
		}
	}
}

func (c *dialogs) Size() int {
	c.m.Lock()
	defer c.m.Unlock()
	return len(c.storage)
}

func (c *dialogs) SizeByCarrier() map[string]int {
	c.m.Lock()
	defer c.m.Unlock()
	result := make(map[string]int)
	for _, entry := range c.storage {
		result[entry.carrier]++
	}
	return result
}

func (c *dialogs) Cleanup() []CleanupResult {
	c.m.Lock()
	defer c.m.Unlock()
	now := time.Now()
	var results []CleanupResult
	for id, entry := range c.storage {
		if now.After(entry.expiresAt) {
			d := now.Sub(entry.createdAt)
			if d > 0 {
				results = append(results, CleanupResult{Duration: d, Carrier: entry.carrier})
			}
			delete(c.storage, id)
		}
	}
	return results
}
