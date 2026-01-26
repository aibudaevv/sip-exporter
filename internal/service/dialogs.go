package service

import (
	"sync"
)

type (
	dialogs struct {
		m       sync.Mutex
		storage map[string]struct{}
	}
	Dialoger interface {
		Create(dialogID string)
		Delete(dialogID string)
		Size() int
	}
)

func NewDialoger() Dialoger {
	return &dialogs{
		storage: make(map[string]struct{}, 10_000),
	}
}

func (c *dialogs) Delete(dialogID string) {
	c.m.Lock()
	defer c.m.Unlock()

	delete(c.storage, dialogID)
}

func (c *dialogs) Create(dialogID string) {
	c.m.Lock()
	defer c.m.Unlock()

	c.storage[dialogID] = struct{}{}
}

func (c *dialogs) Size() int {
	c.m.Lock()
	defer c.m.Unlock()

	return len(c.storage)
}
