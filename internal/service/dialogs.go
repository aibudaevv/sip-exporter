package service

import (
	"sync"
	"time"
)

type (
	dialogEntry struct {
		expiresAt          time.Time
		createdAt          time.Time
		carrier            string
		uaType             string
		sourceCountry      string
		destinationCountry string
		direction          string
		callID             string
	}

	// DialogParams holds the parameters for creating a SIP dialog entry.
	DialogParams struct {
		DialogID           string
		ExpiresAt          time.Time
		CreatedAt          time.Time
		Carrier            string
		UAType             string
		SourceCountry      string
		DestinationCountry string
		Direction          string
		CallID             string
	}

	// CleanupResult carries the metadata of a dialog that has expired or was
	// deleted, used by the caller to emit teardown metrics (SPD, SDC, etc.).
	CleanupResult struct {
		Duration           time.Duration
		Carrier            string
		UAType             string
		SourceCountry      string
		DestinationCountry string
		Direction          string
		CallID             string
	}

	dialogs struct {
		m       sync.Mutex
		storage map[string]dialogEntry
	}
	// Dialoger tracks active SIP dialogs for session counting and cleanup.
	Dialoger interface {
		Create(p DialogParams)
		Delete(dialogID string) CleanupResult
		HasActiveDialog(dialogID string) bool
		Refresh(dialogID string, expiresAt time.Time) bool
		Size() int
		Counts() []LabeledCount
		Cleanup() []CleanupResult
	}
)

const dialogsInitialSize = 10_000

// NewDialoger creates a [Dialoger] with a pre-sized internal map.
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
		return CleanupResult{
			Carrier: entry.carrier, UAType: entry.uaType,
			SourceCountry: entry.sourceCountry, DestinationCountry: entry.destinationCountry,
			Direction: entry.direction, CallID: entry.callID,
		}
	}
	return CleanupResult{
		Duration: d, Carrier: entry.carrier, UAType: entry.uaType,
		SourceCountry: entry.sourceCountry, DestinationCountry: entry.destinationCountry,
		Direction: entry.direction, CallID: entry.callID,
	}
}

func (c *dialogs) Create(p DialogParams) {
	c.m.Lock()
	defer c.m.Unlock()
	if _, exists := c.storage[p.DialogID]; !exists {
		c.storage[p.DialogID] = dialogEntry{
			expiresAt:          p.ExpiresAt,
			createdAt:          p.CreatedAt,
			carrier:            p.Carrier,
			uaType:             p.UAType,
			sourceCountry:      p.SourceCountry,
			destinationCountry: p.DestinationCountry,
			direction:          p.Direction,
			callID:             p.CallID,
		}
	}
}

func (c *dialogs) HasActiveDialog(dialogID string) bool {
	c.m.Lock()
	defer c.m.Unlock()
	_, exists := c.storage[dialogID]
	return exists
}

func (c *dialogs) Refresh(dialogID string, expiresAt time.Time) bool {
	c.m.Lock()
	defer c.m.Unlock()
	entry, exists := c.storage[dialogID]
	if !exists {
		return false
	}
	entry.expiresAt = expiresAt
	c.storage[dialogID] = entry
	return true
}

func (c *dialogs) Size() int {
	c.m.Lock()
	defer c.m.Unlock()
	return len(c.storage)
}

func (c *dialogs) Counts() []LabeledCount {
	c.m.Lock()
	defer c.m.Unlock()
	type key struct{ carrier, uaType, sourceCountry, direction string }
	tmp := make(map[key]int)
	for _, entry := range c.storage {
		tmp[key{entry.carrier, entry.uaType, entry.sourceCountry, entry.direction}]++
	}
	result := make([]LabeledCount, 0, len(tmp))
	for k, n := range tmp {
		result = append(result, LabeledCount{
			Labels: map[string]string{
				"carrier": k.carrier, "ua_type": k.uaType,
				"source_country": k.sourceCountry, "direction": k.direction,
			},
			Count: n,
		})
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
				results = append(results, CleanupResult{
					Duration: d, Carrier: entry.carrier, UAType: entry.uaType,
					SourceCountry: entry.sourceCountry, DestinationCountry: entry.destinationCountry,
					Direction: entry.direction, CallID: entry.callID,
				})
			}
			delete(c.storage, id)
		}
	}
	return results
}
