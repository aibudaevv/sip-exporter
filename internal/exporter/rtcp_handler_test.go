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

// TestHandleRTCP_SenderReportValues proves that SR report-block values
// (jitter/loss/RTT) are extracted correctly despite the 20-byte sender-info
// preceding the blocks — the same values an equivalent RR would produce.
func TestHandleRTCP_SenderReportValues(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const ssrc uint32 = 0xAAAA0009
	registerRTPStream(t, e, ssrc)

	lsr := nowNTP32(time.Now().Add(-5 * time.Second))
	sr := buildSR(0, buildRTCPBlock(ssrc, 26, 5, 100, 1600, lsr, 0x00010000))

	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, sr)
	require.NoError(t, err)

	require.Equal(t, 1, mm.rtcpJitterCalls)
	require.InDelta(t, 200.0, mm.rtcpJitterVal, 0.01)

	require.Equal(t, 1, mm.rtcpLossFracCalls)
	require.InDelta(t, 26.0/256*100, mm.rtcpLossFracVal, 0.01)

	require.Zero(t, mm.rtcpCumLossCalls, "first SR establishes baseline")

	require.Equal(t, 1, mm.rtcpRTTCalls)
	require.Greater(t, mm.rtcpRTTVal, 3500.0)
	require.Less(t, mm.rtcpRTTVal, 4500.0)
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

// TestHandleRTCP_RTTSkippedWhenDLSRExceedsElapsed exercises the negative-RTT
// clamp via a path distinct from clock skew: LSR is valid (in the past) but DLSR
// exceeds the elapsed time since LSR, so now-LSR-DLSR < 0. This is the same
// int32(rttUnits) <= 0 guard, reached from an implausibly-large DLSR rather than
// a future LSR.
func TestHandleRTCP_RTTSkippedWhenDLSRExceedsElapsed(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const ssrc uint32 = 0xAAAA0008
	registerRTPStream(t, e, ssrc)

	lsr := nowNTP32(time.Now().Add(-5 * time.Second)) // LSR 5 s ago (valid)
	dlsr := uint32(10 * 65536)                        // 10 s in 1/65536 units — exceeds elapsed
	rr := buildRR(buildRTCPBlock(ssrc, 0, 0, 0, 0, lsr, dlsr))
	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, rr)
	require.NoError(t, err)

	require.Zero(t, mm.rtcpRTTCalls, "negative RTT from DLSR exceeding elapsed must be skipped")
	require.Equal(t, 1, mm.rtcpReportCalls, "report is still counted")
}

// TestHandleRTCP_RTTSkippedWhenDLSRZero proves that RTT is not computed when
// DLSR=0 despite a valid non-zero LSR. A zero DLSR with non-zero LSR is a
// malformed report (the receiver remembered the timestamp but not its own
// processing delay), and the formula now−LSR−0 would yield ~reporting interval
// (5 s) instead of a real sub-second RTT.
func TestHandleRTCP_RTTSkippedWhenDLSRZero(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const ssrc uint32 = 0xAAAA0010
	registerRTPStream(t, e, ssrc)

	lsr := nowNTP32(time.Now().Add(-5 * time.Second))
	rr := buildRR(buildRTCPBlock(ssrc, 0, 0, 0, 0, lsr, 0)) // DLSR=0
	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, rr)
	require.NoError(t, err)

	require.Zero(t, mm.rtcpRTTCalls, "RTT must be skipped when DLSR=0 (malformed)")
	require.Equal(t, 1, mm.rtcpReportCalls, "report is still counted")
}

// TestHandleRTCP_PartialCompoundProcessesValidPrefix proves that a single
// malformed trailing sub-packet does not blind the whole compound: rtcp.Parse
// returns the valid SR/RR prefix together with the error, and the handler
// salvages that prefix (emits its metrics) while counting the parse error.
func TestHandleRTCP_PartialCompoundProcessesValidPrefix(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const ssrc uint32 = 0xAAAA0007
	registerRTPStream(t, e, ssrc)

	// Valid RR (correlated SSRC) followed by a trailing sub-packet whose declared
	// length overruns the buffer → Parse returns the RR prefix + ErrTruncated.
	rr := buildRR(buildRTCPBlock(ssrc, 0, 0, 0, 0, 0, 0))
	badTail := []byte{0x80, 202, 0xFF, 0xFF} // SDES, length=65535 → overruns payload
	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, append(rr, badTail...))
	require.NoError(t, err, "a partial parse is not a handler error")

	require.Equal(t, 1, mm.rtcpReportCalls, "valid RR prefix must be salvaged and counted")
	require.Equal(t, 1, mm.parseErrorCalls, "the trailing parse error must be counted")
	require.Equal(t, "rtcp", mm.parseErrorType)
}

// TestHandleRTCP_MultiBlockRR proves the inner loop (for _, blk := range rep.Blocks)
// iterates all report blocks. With 2 blocks — one correlated (SSRC tracked) and one
// orphan (untracked) — the correlated block emits jitter/loss/report while the orphan
// increments the orphan counter and is skipped via continue.
func TestHandleRTCP_MultiBlockRR(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const (
		knownSSRC  uint32 = 0xAAAA0012
		orphanSSRC uint32 = 0xAAAA0013
	)
	registerRTPStream(t, e, knownSSRC)

	rr := buildRR(
		buildRTCPBlock(knownSSRC, 0, 0, 0, 1600, 0, 0),
		buildRTCPBlock(orphanSSRC, 0, 0, 0, 3200, 0, 0),
	)
	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, rr)
	require.NoError(t, err)

	require.Equal(t, 1, mm.rtcpJitterCalls, "only correlated block emits jitter")
	require.InDelta(t, 200.0, mm.rtcpJitterVal, 0.01)
	require.Equal(t, 1, mm.rtcpReportCalls, "only correlated block emits report")
	require.Equal(t, 1, mm.rtcpLossFracCalls)
	require.Equal(t, 1, mm.rtcpOrphanCalls, "orphan block must increment orphan counter")
	require.Zero(t, mm.rtcpCumLossCalls, "first RR establishes baseline")
}

// TestHandleRTCP_MixedCompound proves the outer loop (for _, rep := range reports)
// iterates multiple sub-packets in a compound RTCP packet. An SR followed by an RR
// (typical RFC 3550 ordering) — both carrying a block for the same correlated SSRC —
// must produce 2 report calls and 2 jitter calls.
func TestHandleRTCP_MixedCompound(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const ssrc uint32 = 0xAAAA0014
	registerRTPStream(t, e, ssrc)

	sr := buildSR(0, buildRTCPBlock(ssrc, 0, 0, 0, 1600, 0, 0))
	rr := buildRR(buildRTCPBlock(ssrc, 0, 0, 0, 1600, 0, 0))
	compound := append(sr, rr...)

	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, compound)
	require.NoError(t, err)

	require.Equal(t, 2, mm.rtcpReportCalls, "both SR and RR must be processed")
	require.Equal(t, 2, mm.rtcpJitterCalls, "both blocks must emit jitter")
	require.Equal(t, "rr", mm.rtcpReportType, "last report (RR) wins in mock")
}

// TestHandleRTCP_RTTUsesCaptureTimestamp proves that RTT is computed from the
// kernel capture timestamp (e.pktTimestamp, SO_TIMESTAMPNS), not wall-clock
// time.Now(). The capture time is set 60 s in the past; LSR is 5 s before the
// capture time and DLSR is 1 s, so the correct RTT ≈ 4 s. Without the fix the
// handler uses time.Now(), yielding RTT ≈ 64 s (60 s of accumulated drift).
func TestHandleRTCP_RTTUsesCaptureTimestamp(t *testing.T) {
	mm := &mockMetricser{}
	e := newRTCPTestExporter(mm)
	const ssrc uint32 = 0xAAAA0011
	registerRTPStream(t, e, ssrc)

	captureTime := time.Now().Add(-60 * time.Second)
	e.pktTimestamp = captureTime
	lsr := nowNTP32(captureTime.Add(-5 * time.Second))
	dlsr := uint32(1 * 65536) // 1 s in NTP32 units
	rr := buildRR(buildRTCPBlock(ssrc, 0, 0, 0, 0, lsr, dlsr))

	_, err := e.handleRTCP(net.IPv4(10, 0, 0, 1), 5004, net.IPv4(10, 0, 0, 2), 5006, rr)
	require.NoError(t, err)

	require.Equal(t, 1, mm.rtcpRTTCalls, "RTT must be computed (LSR/DLSR valid)")
	require.Greater(t, mm.rtcpRTTVal, 3500.0, "RTT should be ~4 s (capture-based)")
	require.Less(t, mm.rtcpRTTVal, 4500.0, "RTT should be ~4 s, not ~64 s (time.Now-based)")
}
