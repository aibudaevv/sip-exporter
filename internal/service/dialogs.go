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
		uaType    string
	}

	CleanupResult struct {
		Duration time.Duration
		Carrier  string
		UAType   string
	}

	dialogs struct {
		m       sync.Mutex
		storage map[string]dialogEntry
	}
	Dialoger interface {
		Create(dialogID string, expiresAt time.Time, createdAt time.Time, carrier string, uaType string)
		Delete(dialogID string) CleanupResult
		Size() int
		SizeByCarrierAndUA() map[string]map[string]int
		Cleanup() []CleanupResult
	}
)

const dialogsInitialSize = 10_000

func NewDialoger() Dialoger {
	return &dialogs{
		storage: make(map[string]dialogEntry, dialogsInitialSize),
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
		return CleanupResult{Carrier: entry.carrier, UAType: entry.uaType}
	}
	return CleanupResult{Duration: d, Carrier: entry.carrier, UAType: entry.uaType}
}

func (c *dialogs) Create(dialogID string, expiresAt time.Time, createdAt time.Time, carrier string, uaType string) {
	c.m.Lock()
	defer c.m.Unlock()
	if _, exists := c.storage[dialogID]; !exists {
		c.storage[dialogID] = dialogEntry{
			expiresAt: expiresAt,
			createdAt: createdAt,
			carrier:   carrier,
			uaType:    uaType,
		}
	}
}

func (c *dialogs) Size() int {
	c.m.Lock()
	defer c.m.Unlock()
	return len(c.storage)
}

func (c *dialogs) SizeByCarrierAndUA() map[string]map[string]int {
	c.m.Lock()
	defer c.m.Unlock()
	result := make(map[string]map[string]int)
	for _, entry := range c.storage {
		if result[entry.carrier] == nil {
			result[entry.carrier] = make(map[string]int)
		}
		result[entry.carrier][entry.uaType]++
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
				results = append(results, CleanupResult{Duration: d, Carrier: entry.carrier, UAType: entry.uaType})
			}
			delete(c.storage, id)
		}
	}
	return results
}
