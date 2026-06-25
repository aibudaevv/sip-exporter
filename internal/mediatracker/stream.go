package mediatracker

import (
	"time"

	"github.com/aibudaevv/sip-exporter/internal/rtp"
)

const (
	jitterGain = 16   // RFC 3550 A.8 smoothing factor denominator
	maxGap     = 1000 // sequence gap threshold above which a stream restart is assumed
	seqHalf    = 1 << 15
	msPerSec   = 1000
)

// StreamState tracks per-SSRC RTP statistics: jitter (RFC 3550 A.8),
// packet loss via sequence-number gaps, and total packet count.
type StreamState struct {
	SSRC         uint32
	Codec        string
	clockRate    uint32
	lastSeq      uint16
	lastTS       uint32
	lastArrival  time.Time
	jitterTicks  float64
	packetsTotal uint64
	packetsLost  uint64
	hasPrev      bool
}

func newStreamState(ssrc uint32, codec string, clockRate uint32, now time.Time) *StreamState {
	return &StreamState{
		SSRC:        ssrc,
		Codec:       codec,
		clockRate:   clockRate,
		lastArrival: now,
	}
}

// Observe ingests an RTP packet and updates jitter/loss counters.
// Sequence arithmetic is performed in uint16 space (natural wraparound), then
// classified by the magnitude of the wrapped delta to avoid signed casts.
func (s *StreamState) Observe(h rtp.Header, arrival time.Time) {
	if !s.hasPrev {
		s.packetsTotal++
		s.saveBaseline(h, arrival)
		return
	}

	delta := h.SequenceNumber - s.lastSeq // uint16, wraps around 0xFFFF→0x0000

	switch {
	case delta >= seqHalf:
		// out-of-order (reorder): update timing, ignore for loss
		s.updateJitter(h, arrival)
	case delta > maxGap:
		// forward but huge gap → stream restart (e.g. SSRC reuse): reset all
		// counters — this is a new flow instance, the previous totals are stale.
		s.jitterTicks = 0
		s.packetsLost = 0
		s.packetsTotal = 1
	case delta > 0:
		// normal forward
		s.updateJitter(h, arrival)
		s.packetsTotal++
		if delta > 1 {
			s.packetsLost += uint64(delta - 1)
		}
	default:
		// delta == 0: duplicate/retransmit — update timing, ignore for loss
		s.updateJitter(h, arrival)
	}

	s.saveBaseline(h, arrival)
}

// saveBaseline records the current packet as the reference for the next one.
func (s *StreamState) saveBaseline(h rtp.Header, arrival time.Time) {
	s.lastArrival = arrival
	s.lastSeq = h.SequenceNumber
	s.lastTS = h.Timestamp
	s.hasPrev = true
}

func (s *StreamState) updateJitter(h rtp.Header, arrival time.Time) {
	if s.clockRate == 0 {
		return
	}
	// Inter-arrival delta in RTP timestamp units (avoid overflow of absolute time).
	// The uint32 subtraction wraps correctly for forward deltas; int32 reinterprets
	// backward deltas (out-of-order arrivals) as small negatives instead of ~4 billion.
	arrivalDeltaTicks := arrival.Sub(s.lastArrival).Nanoseconds() * int64(s.clockRate) / int64(time.Second)
	tsDelta := int64(int32(h.Timestamp - s.lastTS))
	d := arrivalDeltaTicks - tsDelta
	if d < 0 {
		d = -d
	}
	s.jitterTicks += (float64(d) - s.jitterTicks) / jitterGain
}

// JitterMs returns the smoothed interarrival jitter in milliseconds (RFC 3550).
func (s *StreamState) JitterMs() float64 {
	if s.clockRate == 0 {
		return 0
	}
	return s.jitterTicks / float64(s.clockRate) * msPerSec
}

// LossRate returns the fraction of lost packets (0..1): lost / (received + lost).
func (s *StreamState) LossRate() float64 {
	expected := s.packetsTotal + s.packetsLost
	if expected == 0 {
		return 0
	}
	return float64(s.packetsLost) / float64(expected)
}
