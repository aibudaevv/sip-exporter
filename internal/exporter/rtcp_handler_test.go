package exporter

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aibudaevv/sip-exporter/internal/mediatracker"
	"github.com/aibudaevv/sip-exporter/internal/rtp"
)

// buildRTCPBlock builds a 24-byte RTCP reception report block (RFC 3550 §6.4.1).
func buildRTCPBlock(ssrc uint32, fracLost uint8, cumLost uint32, extSeq, jitter, lsr, dlsr uint32) []byte {
	b := make([]byte, 24)
	binary.BigEndian.PutUint32(b[0:4], ssrc)
	b[4] = fracLost
	b[5] = byte(cumLost >> 16)
	b[6] = byte(cumLost >> 8)
	b[7] = byte(cumLost)
	binary.BigEndian.PutUint32(b[8:12], extSeq)
	binary.BigEndian.PutUint32(b[12:16], jitter)
	binary.BigEndian.PutUint32(b[16:20], lsr)
	binary.BigEndian.PutUint32(b[20:24], dlsr)
	return b
}

// buildRR builds a Receiver Report (PT 201) for a fixed reporter SSRC with the
// given report blocks. The reporter SSRC is unused by the handler (correlation is
// by block SSRC), so it is hardcoded here.
func buildRR(blocks ...[]byte) []byte {
	const reporterSSRC = 0xDEAD
	pktLen := 4 + 4 + len(blocks)*24
	b := make([]byte, pktLen)
	b[0] = 0x80 | byte(len(blocks)) // V=2, P=0, RC
	b[1] = 201                      // PT=RR
	binary.BigEndian.PutUint16(b[2:4], uint16(pktLen/4-1))
	binary.BigEndian.PutUint32(b[4:8], reporterSSRC)
	off := 8
	for _, blk := range blocks {
		copy(b[off:], blk)
		off += 24
	}
	return b
}

// buildSR builds a Sender Report (PT 200) with sender info and report blocks.
func buildSR(ntpTS uint64, blocks ...[]byte) []byte {
	const reporterSSRC = 0xDEAD
	pktLen := 4 + 4 + 20 + len(blocks)*24 // header + sender SSRC + sender info + blocks
	b := make([]byte, pktLen)
	b[0] = 0x80 | byte(len(blocks)) // V=2, P=0, RC
	b[1] = 200                      // PT=SR
	binary.BigEndian.PutUint16(b[2:4], uint16(pktLen/4-1))
	binary.BigEndian.PutUint32(b[4:8], reporterSSRC)
	binary.BigEndian.PutUint64(b[8:16], ntpTS) // sender info (RTP ts / pkt / oct counts left zero)
	off := 28
	for _, blk := range blocks {
		copy(b[off:], blk)
		off += 24
	}
	return b
}

func TestNowNTP32_UnixEpoch(t *testing.T) {
	// Unix epoch (1970) = NTP seconds 2208988800 = 0x83AA7E80; low 16 bits 0x7E80,
	// fraction 0 → middle-32 NTP = 0x7E800000. Pins the formula independently of
	// the handler (which would self-cancel by computing LSR with the same function).
	require.Equal(t, uint32(0x7E800000), nowNTP32(time.Unix(0, 0)))
}

func newRTCPTestExporter(mm *mockMetricser) *exporter {
	return &exporter{
		services:     services{metricser: mm, dialoger: &mockDialoger{}},
		mediaTracker: mediatracker.NewTracker(rtpStreamTTL),
	}
}

// registerRTPStream registers an endpoint and observes one RTP packet so the SSRC
// enters the media tracker (and its SSRC index) with the given labels.
func registerRTPStream(t *testing.T, e *exporter, ssrc uint32) {
	t.Helper()
	e.mediaTracker.Register("10.0.0.1", 5004, mediatracker.MediaLabels{
		Carrier: "carrier-a", UAType: "yealink", SourceCountry: "RU", Direction: "inbound", CallID: "call-1",
		SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000},
	})
	hdr := rtp.Header{Version: 2, PayloadType: 0, SequenceNumber: 1, Timestamp: 160, SSRC: ssrc}
	_, ok := e.mediaTracker.Observe("10.0.0.1", 5004, "0.0.0.0", 0, hdr, time.Unix(1000, 0))
	require.True(t, ok)
}

func TestHandleRTCP_ReceiverReport(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const ssrc uint32 = 0xAAAA0001
	registerRTPStream(t, e, ssrc)

	// jitter=1600 ticks → 200 ms at clockRate 8000; fracLost=26 → 26/256*100 ≈ 10.16%;
	// cumLost=5 (first observation → baseline, emits no cumulative-loss delta);
	// LSR 5 s in the past, DLSR=0x10000 (1 s) → RTT = 5 − 1 = 4 s (exercises
	// DLSR subtraction, not just LSR).
	lsr := nowNTP32(time.Now().Add(-5 * time.Second))
	rr := buildRR(buildRTCPBlock(ssrc, 26, 5, 100, 1600, lsr, 0x00010000))

	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, rr)
	require.NoError(t, err)

	require.Equal(t, 1, mm.rtcpReportCalls)
	require.Equal(t, "rr", mm.rtcpReportType)
	require.Equal(t, "carrier-a", mm.rtcpReportCarrier)
	require.Equal(t, "inbound", mm.rtcpReportDirection)

	require.Equal(t, 1, mm.rtcpJitterCalls)
	require.InDelta(t, 200.0, mm.rtcpJitterVal, 0.01)

	require.Equal(t, 1, mm.rtcpLossFracCalls)
	require.InDelta(t, 26.0/256*100, mm.rtcpLossFracVal, 0.01)

	// First RR establishes the cumulative-loss baseline — no delta emitted (D3).
	require.Zero(t, mm.rtcpCumLossCalls, "first RR establishes baseline, emits no cumulative-loss delta")

	// RTT = (now - LSR - DLSR) ≈ 5 s − 1 s DLSR = 4 s; allow generous slack for timing.
	require.Equal(t, 1, mm.rtcpRTTCalls)
	require.Greater(t, mm.rtcpRTTVal, 3500.0)
	require.Less(t, mm.rtcpRTTVal, 4500.0)
}

// TestHandleRTCP_CumulativeLossDeltaOnSecondReport proves the D3 baseline + delta
// semantics through the full handler: the first RR establishes the baseline (no
// cumulative-loss emit), the second emits the delta between cumulative values.
func TestHandleRTCP_CumulativeLossDeltaOnSecondReport(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const ssrc uint32 = 0xAAAA0006
	registerRTPStream(t, e, ssrc)

	// First RR: cumulative=5 → baseline, no cumulative-loss delta emitted.
	rr1 := buildRR(buildRTCPBlock(ssrc, 0, 5, 0, 0, 0, 0))
	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, rr1)
	require.NoError(t, err)
	require.Zero(t, mm.rtcpCumLossCalls, "first RR establishes baseline")

	// Second RR: cumulative=8 → delta=3 emitted.
	rr2 := buildRR(buildRTCPBlock(ssrc, 0, 8, 0, 0, 0, 0))
	_, err = e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, rr2)
	require.NoError(t, err)
	require.Equal(t, 1, mm.rtcpCumLossCalls)
	require.Equal(t, uint64(3), mm.rtcpCumLossVal)
}

func TestHandleRTCP_RTTSkippedWhenLSRZero(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const ssrc uint32 = 0xAAAA0002
	registerRTPStream(t, e, ssrc)

	rr := buildRR(buildRTCPBlock(ssrc, 0, 0, 0, 0, 0, 0))
	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, rr)
	require.NoError(t, err)

	require.Equal(t, 1, mm.rtcpReportCalls, "report still counted")
	require.Zero(t, mm.rtcpRTTCalls, "RTT must be skipped when LSR==0")
}

func TestHandleRTCP_UncorrelatedSSRCDropped(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const knownSSRC uint32 = 0xAAAA0003
	registerRTPStream(t, e, knownSSRC)

	// Report block names an SSRC we do not track.
	rr := buildRR(buildRTCPBlock(0xDEADBEEF, 26, 5, 100, 1600, 0, 0))
	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, rr)
	require.NoError(t, err)

	require.Zero(t, mm.rtcpReportCalls, "uncorrelated SSRC must not emit metrics")
	require.Equal(t, 1, mm.rtcpOrphanCalls, "uncorrelated SSRC must increment the orphan counter")
}

func TestHandleRTCP_SenderReportType(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const ssrc uint32 = 0xAAAA0004
	registerRTPStream(t, e, ssrc)

	sr := buildSR(0, buildRTCPBlock(ssrc, 0, 0, 0, 0, 0, 0))
	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, sr)
	require.NoError(t, err)

	require.Equal(t, 1, mm.rtcpReportCalls)
	require.Equal(t, "sr", mm.rtcpReportType, "SR packet must be labelled type=sr")
}

func TestHandleRTCP_RTTSkippedOnClockSkew(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const ssrc uint32 = 0xAAAA0005
	registerRTPStream(t, e, ssrc)

	// LSR 5 s in the FUTURE → now-LSR-DLSR underflows → int32 < 0 → skip (clock skew).
	futureLSR := nowNTP32(time.Now().Add(5 * time.Second))
	rr := buildRR(buildRTCPBlock(ssrc, 0, 0, 0, 0, futureLSR, 0))
	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, rr)
	require.NoError(t, err)

	require.Zero(t, mm.rtcpRTTCalls, "negative RTT from clock skew must be skipped")
	require.Equal(t, 1, mm.rtcpReportCalls, "report is still counted")
}
