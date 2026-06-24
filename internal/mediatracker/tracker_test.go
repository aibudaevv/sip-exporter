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
