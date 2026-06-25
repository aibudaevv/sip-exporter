package mediatracker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aibudaevv/sip-exporter/internal/rtp"
)

const g711Clock = 8000

func newHeader(seq uint16, ts uint32) rtp.Header {
	return rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: ts, SSRC: 0x11223344}
}

func TestStreamState_FirstPacket(t *testing.T) {
	s := newStreamState(0x11223344, "PCMU", g711Clock, time.Unix(0, 0))
	s.Observe(newHeader(1, 160), time.Unix(0, 0))

	require.Equal(t, uint64(1), s.packetsTotal)
	require.Equal(t, uint64(0), s.packetsLost)
	require.InDelta(t, 0.0, s.JitterMs(), 0.0001)
}

func TestStreamState_InOrderNoGap(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newStreamState(0x11223344, "PCMU", g711Clock, t0)
	s.Observe(newHeader(1, 160), t0)
	s.Observe(newHeader(2, 320), t0.Add(20*time.Millisecond))

	require.Equal(t, uint64(2), s.packetsTotal)
	require.Equal(t, uint64(0), s.packetsLost)
	// no deviation → jitter ~ 0
	require.InDelta(t, 0.0, s.JitterMs(), 0.001)
}

func TestStreamState_SequenceGapCountsLoss(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newStreamState(0x11223344, "PCMU", g711Clock, t0)
	s.Observe(newHeader(1, 160), t0)
	s.Observe(newHeader(6, 320), t0.Add(20*time.Millisecond))

	require.Equal(t, uint64(2), s.packetsTotal)
	require.Equal(t, uint64(4), s.packetsLost, "seq 1->6 = 4 lost")
}

func TestStreamState_ReorderNotLoss(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newStreamState(0x11223344, "PCMU", g711Clock, t0)
	s.Observe(newHeader(1, 160), t0)
	s.Observe(newHeader(5, 320), t0.Add(20*time.Millisecond)) // gap of 3 counted
	s.Observe(newHeader(3, 640), t0.Add(40*time.Millisecond)) // old seq, reorder

	// total incremented only for forward packets (1, 5); reorder (3) ignored
	require.Equal(t, uint64(2), s.packetsTotal)
	require.Equal(t, uint64(3), s.packetsLost)
}

func TestStreamState_ReorderNoDoubleCountLoss(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newStreamState(0x11223344, "PCMU", g711Clock, t0)
	s.Observe(newHeader(1, 160), t0)                          // first
	s.Observe(newHeader(5, 320), t0.Add(20*time.Millisecond)) // gap: 3 lost (2,3,4)
	s.Observe(newHeader(3, 640), t0.Add(40*time.Millisecond)) // reorder
	s.Observe(newHeader(6, 800), t0.Add(60*time.Millisecond)) // forward from maxSeq=5

	// Without fix: lastSeq=3 after reorder → delta=3 → lost+=2 → lost=5 (double-count)
	// With fix: maxSeq=5 after reorder → delta=1 → lost stays 3
	require.Equal(t, uint64(3), s.packetsLost, "reorder must not cause loss double-count")
	require.Equal(t, uint64(3), s.packetsTotal)
}

func TestStreamState_DuplicateIgnored(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newStreamState(0x11223344, "PCMU", g711Clock, t0)
	s.Observe(newHeader(1, 160), t0)
	s.Observe(newHeader(1, 160), t0.Add(1*time.Millisecond)) // same seq

	require.Equal(t, uint64(1), s.packetsTotal)
	require.Equal(t, uint64(0), s.packetsLost)
}

func TestStreamState_StreamRestartNoHugeLoss(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newStreamState(0x11223344, "PCMU", g711Clock, t0)
	s.Observe(newHeader(1, 160), t0)
	// huge jump beyond maxGap → treated as restart: counters reset, no huge loss
	s.Observe(newHeader(5000, 320), t0.Add(20*time.Millisecond))

	require.Equal(t, uint64(1), s.packetsTotal, "restart resets the flow counter to this packet")
	require.Equal(t, uint64(0), s.packetsLost, "restart must not count 4998 lost")
}

func TestStreamState_RestartResetsJitter(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newStreamState(0x11223344, "PCMU", g711Clock, t0)
	s.Observe(newHeader(1, 160), t0)
	s.Observe(newHeader(2, 320), t0.Add(45*time.Millisecond)) // introduces jitter
	require.Greater(t, s.JitterMs(), 0.0, "jitter must accumulate before restart")

	// huge sequence jump → stream restart → jitter baseline resets
	s.Observe(newHeader(5000, 100000), t0.Add(60*time.Millisecond))
	require.InDelta(t, 0.0, s.JitterMs(), 0.0001, "jitter must reset on stream restart")
}

func TestStreamState_SeqWraparound(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newStreamState(0x11223344, "PCMU", g711Clock, t0)
	s.Observe(newHeader(0xFFFF, 160), t0)
	// wraps to 0x0003 → forward diff of 4 → 3 lost
	s.Observe(newHeader(0x0003, 320), t0.Add(20*time.Millisecond))

	require.Equal(t, uint64(2), s.packetsTotal)
	require.Equal(t, uint64(3), s.packetsLost)
}

func TestStreamState_JitterRFC3550(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newStreamState(0x11223344, "PCMU", g711Clock, t0)
	// pkt1: ts=160 @ t0
	s.Observe(newHeader(1, 160), t0)
	// pkt2: ts=320 @ t0+20ms → expected spacing → d=0
	s.Observe(newHeader(2, 320), t0.Add(20*time.Millisecond))
	// pkt3: ts=480 @ t0+45ms → inter-arrival 25ms vs ts delta 20ms (160 ticks)
	//   arrivalDeltaTicks = 25ms*8000/1000 = 200; tsDelta=160; d=40; jitter=(40-0)/16=2.5 ticks
	s.Observe(newHeader(3, 480), t0.Add(45*time.Millisecond))

	// JitterMs = 2.5 ticks / 8000 * 1000 = 0.3125 ms
	require.InDelta(t, 0.3125, s.JitterMs(), 0.0001)
}

func TestStreamState_ReorderJitterNoWraparound(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newStreamState(0x11223344, "PCMU", g711Clock, t0)
	// seq 1 → 3: forward gap (1 lost), perfect spacing (d=0, jitter stays 0)
	s.Observe(newHeader(1, 160), t0)
	s.Observe(newHeader(3, 480), t0.Add(40*time.Millisecond))
	// seq 2: reorder (ts=320 < lastTS=480). uint32 subtraction 320-480 wraps.
	s.Observe(newHeader(2, 320), t0.Add(50*time.Millisecond))

	// arrivalDeltaTicks = 10ms*8 = 80; tsDelta(int32) = -160; d = |80+160| = 240
	// jitterTicks = (240-0)/16 = 15.0; JitterMs = 15/8 = 1.875
	require.InDelta(t, 1.875, s.JitterMs(), 0.001,
		"reorder timestamp delta must be signed (int32), not wrapped uint32")
}

func TestStreamState_LossRate(t *testing.T) {
	// LossRate = lost / (received + lost) = 5 / (100 + 5)
	s := &StreamState{packetsTotal: 100, packetsLost: 5}
	require.InDelta(t, 5.0/105.0, s.LossRate(), 0.0001)

	// no packets → 0 (avoid div by zero)
	empty := &StreamState{}
	require.InDelta(t, 0.0, empty.LossRate(), 0.0001)
}
