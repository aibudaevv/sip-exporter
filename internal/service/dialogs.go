package service

import (
	"sync"
	"time"
)

type (
	dialogEntry struct {
		expiresAt time.Time
		createdAt time.Time
	}

	dialogs struct {
		m       sync.Mutex
		storage map[string]dialogEntry
	}
	Dialoger interface {
		Create(dialogID string, expiresAt time.Time, createdAt time.Time)
		Delete(dialogID string) time.Duration
		Size() int
		Cleanup() []time.Duration
	}
)

func NewDialoger() Dialoger {
	return &dialogs{
		storage: make(map[string]dialogEntry, 10_000),
	}
}

func (c *dialogs) Delete(dialogID string) time.Duration {
	c.m.Lock()
	defer c.m.Unlock()
	entry, ok := c.storage[dialogID]
	if !ok {
		return 0
	}
	delete(c.storage, dialogID)
	d := time.Since(entry.createdAt)
	if d < 0 {
		return 0
	}
	return d
}

func (c *dialogs) Create(dialogID string, expiresAt time.Time, createdAt time.Time) {
	c.m.Lock()
	defer c.m.Unlock()
	if _, exists := c.storage[dialogID]; !exists {
		c.storage[dialogID] = dialogEntry{
			expiresAt: expiresAt,
			createdAt: createdAt,
		}
	}
}

func (c *dialogs) Size() int {
	c.m.Lock()
	defer c.m.Unlock()
	return len(c.storage)
}

func (c *dialogs) Cleanup() []time.Duration {
	c.m.Lock()
	defer c.m.Unlock()
	now := time.Now()
	var durations []time.Duration
	for id, entry := range c.storage {
		if now.After(entry.expiresAt) {
			d := now.Sub(entry.createdAt)
			if d > 0 {
				durations = append(durations, d)
			}
			delete(c.storage, id)
		}
	}
	return durations
}
