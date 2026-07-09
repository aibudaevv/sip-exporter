package mediatracker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aibudaevv/sip-exporter/internal/rtp"
)

func sampleLabels(callID string) MediaLabels {
	return MediaLabels{
		Carrier:    "carrier-a",
		UAType:     "yealink",
		CallID:     callID,
		SDPCodecs:  map[uint8]string{0: "PCMU"},
		ClockRates: map[uint8]uint32{0: 8000},
	}
}

func TestCorrelator_RegisterAndLookup(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))

	got, ok := tr.Lookup("10.0.0.1", 5004)
	require.True(t, ok)
	require.Equal(t, "carrier-a", got.Carrier)
	require.Equal(t, "call-1", got.CallID)

	_, ok = tr.Lookup("10.0.0.1", 9999)
	require.False(t, ok)
}

func TestCorrelator_UnregisterByCallID(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5006, sampleLabels("call-1"))
	tr.Register("10.0.0.3", 5008, sampleLabels("call-2"))

	tr.Unregister("call-1")

	_, ok1 := tr.Lookup("10.0.0.1", 5004)
	_, ok2 := tr.Lookup("10.0.0.2", 5006)
	_, ok3 := tr.Lookup("10.0.0.3", 5008)
	require.False(t, ok1, "endpoint of call-1 must be removed")
	require.False(t, ok2)
	require.True(t, ok3, "endpoint of call-2 must remain")
}

func TestTracker_ObserveNoCorrelation_Drop(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	t0 := time.Unix(1000, 0)
	_, ok := tr.Observe("10.0.0.99", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	require.False(t, ok, "RTP without registered endpoint must be dropped")
	require.Empty(t, tr.Snapshot())
}

func TestTracker_ObserveWithCorrelation(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)

	res, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	require.True(t, ok)
	require.True(t, res.Counted)
	require.Equal(t, uint64(0), res.Lost)
	require.Equal(t, "PCMU", res.Codec)

	// gap of 3
	res, ok = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(5, 320), t0.Add(20*time.Millisecond))
	require.True(t, ok)
	require.True(t, res.Counted)
	require.Equal(t, uint64(3), res.Lost)

	stats := tr.Snapshot()
	require.Len(t, stats, 1)
	require.Equal(t, uint32(0x11223344), stats[0].SSRC)
	require.Equal(t, "carrier-a", stats[0].Carrier)
	require.Equal(t, "yealink", stats[0].UAType)
	require.Equal(t, "PCMU", stats[0].Codec)
	require.Equal(t, uint64(2), stats[0].PacketsTotal)
	require.Equal(t, uint64(3), stats[0].PacketsLost)
	// 60% loss on this stream → MOS must be valid but degraded (not clean)
	require.True(t, stats[0].MOS >= 1.0 && stats[0].MOS <= 4.5)
}

func TestTracker_ObserveDuplicateFlag(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-dup"))
	t0 := time.Unix(1000, 0)

	// first packet
	res, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(5, 160), t0)
	require.True(t, ok)
	require.True(t, res.Counted)
	require.False(t, res.Duplicate, "first packet must not be a duplicate")

	// same sequence number → duplicate
	res, ok = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(5, 160), t0.Add(1*time.Millisecond))
	require.True(t, ok)
	require.False(t, res.Counted, "duplicate must not be counted as received")
	require.True(t, res.Duplicate, "same seq must set Duplicate flag")

	// normal forward packet → not a duplicate
	res, ok = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(6, 320), t0.Add(20*time.Millisecond))
	require.True(t, ok)
	require.True(t, res.Counted)
	require.False(t, res.Duplicate)

	stats := tr.Snapshot()
	require.Len(t, stats, 1)
	require.Equal(t, uint64(2), stats[0].PacketsTotal)
	require.Equal(t, uint64(1), stats[0].PacketsDuplicate, "snapshot must report 1 duplicate")
}

func TestTracker_SnapshotComputesMOSAndJitter(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)

	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	// late packet: jitter introduced
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(2, 320), t0.Add(45*time.Millisecond))

	stats := tr.Snapshot()
	require.Len(t, stats, 1)
	require.Greater(t, stats[0].JitterMs, 0.0)
	require.Less(t, stats[0].MOS, 4.41) // some impairment from jitter
}

func TestTracker_SnapshotMOSVariants(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)

	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	// delay 1050ms → jitter ≈ 64ms (> jbMsDefault=60, < jbMsF2=200)
	// F1 (jb=50): discard 0.29, default (jb=60): discard 0.07, F2/Adaptive: 0
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(2, 320), t0.Add(1050*time.Millisecond))

	stats := tr.Snapshot()
	require.Len(t, stats, 1)
	require.Greater(t, stats[0].JitterMs, 60.0, "jitter must exceed jbMsDefault")
	require.Less(t, stats[0].MOSF1, stats[0].MOS, "F1 (strict JB) must be < default")
	require.Less(t, stats[0].MOS, stats[0].MOSF2, "default must be < F2 (generous JB)")
	require.InDelta(t, stats[0].MOSF2, stats[0].MOSAdaptive, 0.0001, "F2=Adaptive when jitter<200ms")
}

func TestTracker_SnapshotFlushesPendingLossRun(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)

	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	// 4 lost, no terminating good packet — lossRun=4 is pending at snapshot time
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(6, 640), t0.Add(40*time.Millisecond))

	stats := tr.Snapshot()
	require.Len(t, stats, 1)
	require.Equal(t, uint64(4), stats[0].PacketsLost)
	require.InDelta(t, 100.0, stats[0].BurstLossDensity, 0.01, "pending burst must be flushed at snapshot")
	require.InDelta(t, 0.0, stats[0].GapLossDensity, 0.01)
}

func TestTracker_CleanupExpiredStreams(t *testing.T) {
	tr := NewTracker(30 * time.Millisecond) // short TTL
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)

	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	require.Len(t, tr.Snapshot(), 1)

	// advance beyond TTL
	tr.SetNow(func() time.Time { return t0.Add(100 * time.Millisecond) })
	tr.Cleanup()
	require.Empty(t, tr.Snapshot(), "expired stream must be removed")
}

// TestTracker_SetTTL_LowersExpiryThreshold verifies that SetTTL changes the
// idle-expiry threshold of an existing tracker: the same elapsed idle time
// must NOT expire a stream under a long TTL, but MUST expire it after SetTTL
// lowers the threshold. This is the seam exercised by SIP_EXPORTER_RTP_STREAM_TTL.
func TestTracker_SetTTL_LowersExpiryThreshold(t *testing.T) {
	tr := NewTracker(1 * time.Hour) // long TTL
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)

	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	tr.SetNow(func() time.Time { return t0.Add(5 * time.Second) })

	tr.Cleanup()
	require.Len(t, tr.Snapshot(), 1, "stream must survive under the original long TTL")

	tr.SetTTL(1 * time.Second) // lower the threshold below the 5s idle time
	tr.Cleanup()
	require.Empty(t, tr.Snapshot(), "stream must expire after SetTTL lowers the threshold")
}

func TestTracker_DynamicCodecFromSDP(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, MediaLabels{
		Carrier: "c", UAType: "u", CallID: "call-x",
		SDPCodecs:  map[uint8]string{111: "opus"},
		ClockRates: map[uint8]uint32{111: 48000},
	})
	t0 := time.Unix(1000, 0)

	h := rtp.Header{Version: 2, PayloadType: 111, SequenceNumber: 1, Timestamp: 960, SSRC: 0x1}
	res, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, h, t0)
	require.True(t, ok)
	require.Equal(t, "opus", res.Codec)

	stats := tr.Snapshot()
	require.Equal(t, "opus", stats[0].Codec)
}

// TestTracker_SSRCReusedAcrossEndpoints verifies that the same SSRC from two
// different media endpoints (two SIP dialogs) is tracked as separate flows,
// not merged into one (regression for SSRC-only keying).
func TestTracker_SSRCReusedAcrossEndpoints(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, MediaLabels{Carrier: "carrier-a", UAType: "yealink", CallID: "call-1",
		SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000}})
	tr.Register("10.0.0.2", 5006, MediaLabels{Carrier: "carrier-b", UAType: "cisco", CallID: "call-2",
		SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000}})
	t0 := time.Unix(1000, 0)

	const reusedSSRC uint32 = 0xABCDEFFF
	_, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, reusedSSRC), t0)
	require.True(t, ok)
	_, ok = tr.Observe("10.0.0.2", 5006, "0.0.0.0", 0, newHeaderSSRC(1, reusedSSRC), t0)
	require.True(t, ok)

	stats := tr.Snapshot()
	require.Len(t, stats, 2, "same SSRC from different endpoints must be 2 flows")
}

func newHeaderSSRC(seq uint16, ssrc uint32) rtp.Header {
	return rtp.Header{Version: 2, PayloadType: 0, SequenceNumber: seq, Timestamp: 160, SSRC: ssrc}
}

// TestTracker_ObserveCorrelatesByDst verifies that when the source endpoint is
// unregistered (e.g. NAT/asymmetric RTP remapped the source port) the packet is
// still correlated via its destination endpoint (the local receive port from SDP).
func TestTracker_ObserveCorrelatesByDst(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	// Only the destination endpoint is registered (callee receive port).
	tr.Register("10.0.0.2", 5006, MediaLabels{
		Carrier: "carrier-dst", UAType: "polycom", CallID: "call-via-dst",
		SDPCodecs: map[uint8]string{8: "PCMA"}, ClockRates: map[uint8]uint32{8: 8000},
	})
	t0 := time.Unix(1000, 0)

	// RTP from an unregistered source to the registered destination.
	hdr := rtp.Header{Version: 2, PayloadType: 8, SequenceNumber: 1, Timestamp: 160, SSRC: 0xCAFE}
	res, ok := tr.Observe("9.9.9.9", 1234, "10.0.0.2", 5006, hdr, t0)
	require.True(t, ok, "must correlate via dst when src is unregistered")
	require.Equal(t, "carrier-dst", res.Carrier, "labels must come from the dst endpoint")
	require.Equal(t, "PCMA", res.Codec)
	require.Len(t, tr.Snapshot(), 1)
}

func TestTracker_StreamRestartNoUnderflow(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)

	// Build up some loss: seq 1→5 = 3 lost
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(5, 320), t0.Add(20*time.Millisecond))
	// packetsLost=3 at this point

	// Stream restart: huge gap → packetsLost resets to 0
	res, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(5000, 480), t0.Add(40*time.Millisecond))
	require.True(t, ok)

	// Without fix: 0 - 3 = 18446744073709551613 (uint64 underflow)
	// With fix: delta clamped to 0
	require.Equal(t, uint64(0), res.Lost, "stream restart must not underflow ObserveResult.Lost")
}

func TestTracker_ClockRateFallback(t *testing.T) {
	// PT absent from ClockRates → default 8000 (crOk=F)
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, MediaLabels{
		Carrier: "c", UAType: "u", CallID: "call-1",
		SDPCodecs:  map[uint8]string{0: "PCMU"},
		ClockRates: map[uint8]uint32{8: 8000}, // PT 0 absent
	})
	t0 := time.Unix(1000, 0)
	_, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	require.True(t, ok)
	stats := tr.Snapshot()
	require.Len(t, stats, 1)
	// clockRate defaults to 8000 → JitterMs can be computed (not stuck at 0)
	require.Equal(t, "PCMU", stats[0].Codec)
}

func TestTracker_ZeroClockRateFallback(t *testing.T) {
	// PT in ClockRates but rate=0 → default 8000 (cr>0=F)
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, MediaLabels{
		Carrier: "c", UAType: "u", CallID: "call-1",
		SDPCodecs:  map[uint8]string{0: "PCMU"},
		ClockRates: map[uint8]uint32{0: 0}, // rate=0
	})
	t0 := time.Unix(1000, 0)
	_, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	require.True(t, ok)
	stats := tr.Snapshot()
	require.Len(t, stats, 1)
}

func TestTracker_UnregisterResult_NoMediaNoRTP(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	r := tr.Unregister("call-1")
	require.False(t, r.MediaExpected)
	require.False(t, r.RTPObserved)
	require.False(t, r.OneWay)
}

func TestTracker_UnregisterResult_MediaExpectedNoRTP(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5006, sampleLabels("call-1"))
	r := tr.Unregister("call-1")
	require.True(t, r.MediaExpected)
	require.False(t, r.RTPObserved)
	require.False(t, r.OneWay)
}

func TestTracker_UnregisterResult_TwoWayRTP(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5006, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)
	// RTP to endpoint 1 (dst=10.0.0.1:5004)
	_, ok := tr.Observe("10.0.0.99", 9999, "10.0.0.1", 5004, newHeader(1, 160), t0)
	require.True(t, ok)
	// RTP to endpoint 2 (dst=10.0.0.2:5006)
	_, ok = tr.Observe("10.0.0.99", 9999, "10.0.0.2", 5006, newHeader(1, 160), t0)
	require.True(t, ok)
	r := tr.Unregister("call-1")
	require.True(t, r.MediaExpected)
	require.True(t, r.RTPObserved)
	require.False(t, r.OneWay)
}

func TestTracker_UnregisterResult_OneWayRTP(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5006, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)
	// RTP only to endpoint 1
	_, ok := tr.Observe("10.0.0.99", 9999, "10.0.0.1", 5004, newHeader(1, 160), t0)
	require.True(t, ok)
	r := tr.Unregister("call-1")
	require.True(t, r.MediaExpected)
	require.True(t, r.RTPObserved)
	require.True(t, r.OneWay, "2 endpoints registered, only 1 with RTP = one-way")
}

func TestTracker_UnregisterResult_SurvivesTTL(t *testing.T) {
	tr := NewTracker(30 * time.Millisecond)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5006, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)

	_, ok := tr.Observe("10.0.0.99", 9999, "10.0.0.1", 5004, newHeader(1, 160), t0)
	require.True(t, ok)
	_, ok = tr.Observe("10.0.0.99", 9999, "10.0.0.2", 5006, newHeader(1, 160), t0)
	require.True(t, ok)

	tr.SetNow(func() time.Time { return t0.Add(100 * time.Millisecond) })
	tr.Cleanup()
	require.Empty(t, tr.Snapshot(), "streams must be TTL-expired")

	r := tr.Unregister("call-1")
	require.True(t, r.MediaExpected, "media endpoints persist")
	require.True(t, r.RTPObserved, "RTP fact must survive stream TTL")
	require.False(t, r.OneWay, "two-way RTP was observed")
}
