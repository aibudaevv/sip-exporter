package service

import (
	"sync"
	"time"
)

type (
	dialogs struct {
		m       sync.Mutex
		storage map[string]time.Time // dialogID -> expiresAt
	}
	Dialoger interface {
		Create(dialogID string, expiresAt time.Time)
		Delete(dialogID string)
		Size() int
		Cleanup() int
	}
)

func NewDialoger() Dialoger {
	return &dialogs{
		storage: make(map[string]time.Time, 10_000),
	}
}

func (c *dialogs) Delete(dialogID string) {
	c.m.Lock()
	defer c.m.Unlock()
	delete(c.storage, dialogID)
}

func (c *dialogs) Create(dialogID string, expiresAt time.Time) {
	c.m.Lock()
	defer c.m.Unlock()
	if _, exists := c.storage[dialogID]; !exists {
		c.storage[dialogID] = expiresAt
	}
}

func (c *dialogs) Size() int {
	c.m.Lock()
	defer c.m.Unlock()
	return len(c.storage)
}

func (c *dialogs) Cleanup() int {
	c.m.Lock()
	defer c.m.Unlock()
	now := time.Now()
	count := 0
	for id, expiresAt := range c.storage {
		if now.After(expiresAt) {
			delete(c.storage, id)
			count++
		}
	}
	return count
}
