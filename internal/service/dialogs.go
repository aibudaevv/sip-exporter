package service

import (
	"sync"
)

type dialogState int

const (
	initiated dialogState = iota
	trying
	ringing
	confirmed
	terminated
	expired
)

type (
	dialogs struct {
		m       sync.Mutex
		storage map[string]dialogState
	}
	Dialoger interface {
		Create(callID, fromTag string)
		Del(callID string)
		Size() int
	}
)

func NewDialoger() Dialoger {
	return &dialogs{
		storage: make(map[string]dialogState, 1000),
	}
}

func (c *dialogs) Del(callID string) {
	c.m.Lock()
	defer c.m.Unlock()

	delete(c.storage, callID)
}

func (c *dialogs) Create(callID, fromTag string) {
	c.m.Lock()
	defer c.m.Unlock()

	//key := fmt.Sprintf("%s:%s", callID, fromTag)

	//c.storage[key] = invite
}

func (c *dialogs) Size() int {
	c.m.Lock()
	defer c.m.Unlock()

	return len(c.storage)
}
