package mediatracker

import (
	"fmt"
	"testing"
	"time"

	"github.com/aibudaevv/sip-exporter/internal/rtp"
)

const benchStreams = 1000

// BenchmarkTracker_Observe_1000Streams measures per-packet ingestion cost
// across 1000 concurrent RTP streams (worst case for monitoring).
func BenchmarkTracker_Observe_1000Streams(b *testing.B) {
	tr := NewTracker(30 * time.Second)
	labels := MediaLabels{
		Carrier: "carrier-a", UAType: "yealink", CallID: "call",
		SDPCodecs:  map[uint8]string{0: "PCMU"},
		ClockRates: map[uint8]uint32{0: 8000},
	}
	// Precompute endpoints to isolate Observe cost (avoid fmt.Sprintf in the loop).
	ips := make([]string, benchStreams)
	for i := range benchStreams {
		ips[i] = fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		tr.Register(ips[i], 5004, labels)
	}
	arrival := time.Unix(1000, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for n := range b.N {
		i := n % benchStreams
		h := rtp.Header{
			Version: 2, PayloadType: 0,
			SequenceNumber: uint16(n), Timestamp: uint32(n) * 160, SSRC: uint32(i),
		}
		_, _ = tr.Observe(ips[i], 5004, "0.0.0.0", 0, h, arrival.Add(time.Duration(n)*time.Millisecond))
	}
}

// BenchmarkTracker_Snapshot_1000Streams measures the periodic metrics-export cost.
func BenchmarkTracker_Snapshot_1000Streams(b *testing.B) {
	tr := NewTracker(30 * time.Second)
	labels := MediaLabels{
		Carrier: "carrier-a", UAType: "yealink", CallID: "call",
		SDPCodecs:  map[uint8]string{0: "PCMU"},
		ClockRates: map[uint8]uint32{0: 8000},
	}
	arrival := time.Unix(1000, 0)
	for i := range benchStreams {
		ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		tr.Register(ip, 5004, labels)
		h := rtp.Header{Version: 2, PayloadType: 0, SequenceNumber: 1, Timestamp: 160, SSRC: uint32(i)}
		_, _ = tr.Observe(ip, 5004, "0.0.0.0", 0, h, arrival)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = tr.Snapshot()
	}
}
