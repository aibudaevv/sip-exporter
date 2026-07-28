// Package mediatracker correlates RTP streams with SIP dialogs, computing
// jitter, loss, and MOS (RFC 3550 / ITU-T G.107) from observed packets.
package mediatracker

import (
	"sync"
	"time"

	"github.com/aibudaevv/sip-exporter/internal/rtp"
)

const defaultClockRate = 8000

const (
	matchedByDst = "dst"
	matchedBySrc = "src"
)

type (
	// MediaLabels is the SIP-dialog context attached to a media endpoint via SDP.
	MediaLabels struct {
		Carrier       string
		UAType        string
		SourceCountry string
		Direction     string
		CallID        string
		SDPCodecs     map[uint8]string // payload type → codec name (from SDP a=rtpmap)
		ClockRates    map[uint8]uint32 // payload type → clock rate (Hz, from SDP)
	}

	// StreamStats is a point-in-time view of an RTP stream, used for metric export.
	StreamStats struct {
		SSRC             uint32
		Codec            string
		Carrier          string
		UAType           string
		SourceCountry    string
		Direction        string
		CallID           string
		PacketsTotal     uint64
		PacketsLost      uint64
		PacketsDuplicate uint64
		JitterMs         float64
		MOS              float64
		MOSF1            float64
		MOSF2            float64
		MOSAdaptive      float64
		RFactor          float64
		BurstLossDensity float64
		GapLossDensity   float64
		LastSeen         time.Time
	}

	// ObserveResult is the per-packet outcome of an RTP observation.
	ObserveResult struct {
		Counted            bool    // packet counted as received (not duplicate/reorder)
		Duplicate          bool    // packet is a duplicate (same sequence number)
		Reorder            bool    // packet is out-of-order (seq < maxSeq, not duplicate)
		Lost               uint64  // packets newly marked lost by this observation
		DelayVariationMs   float64 // raw per-packet PDV (|arrivalDelta − tsDelta|, ms) of the last forward packet; not updated on duplicate/reorder (only emitted when Counted)
		StreamPacketsTotal uint64  // stream's total forward-counted packets after this Observe
		MatchedIP          string  // IP of the correlated media endpoint the stream is keyed by (dst-first, then src)
		MatchedPort        uint16  // port of the correlated media endpoint (companion of MatchedIP)
		MatchedBy          string  // which candidate matched: "dst" (local receive endpoint) or "src" (sender, NAT fallback)
		Codec              string  // resolved codec name
		Carrier            string  // dialog carrier (for metric labels)
		UAType             string  // dialog UA type (for metric labels)
		SourceCountry      string  // dialog source country (for metric labels)
		Direction          string  // dialog direction (for metric labels)
		CallID             string  // dialog Call-ID (used to clear FAS pending once media is established)
	}

	// RTPDialogResult is the per-dialog RTP summary returned at teardown.
	RTPDialogResult struct {
		MediaExpected bool // at least 1 media endpoint was registered (SDP seen)
		RTPObserved   bool // at least 1 RTP stream was active
		OneWay        bool // 2+ endpoints registered but only 1 has RTP
	}

	// MediaEndpoint identifies a registered RTP media endpoint (IP:port from SDP).
	MediaEndpoint struct {
		IP   string
		Port uint16
	}

	endpointKey struct {
		ip   string
		port uint16
	}

	rtcpMediaEntry struct {
		callID      string
		rtpEndpoint endpointKey
	}

	// streamKey identifies one RTP flow: a media endpoint plus an SSRC. SSRCs are
	// only unique within a flow, so keying by SSRC alone would collide when two
	// dialogs reuse an SSRC within the TTL window.
	streamKey struct {
		endpoint endpointKey
		ssrc     uint32
	}

	// Tracker keeps per-flow RTP statistics and correlates RTP flows to SIP
	// dialogs via the media-endpoint map (IP:port → labels) populated from SDP.
	Tracker struct {
		mu          sync.Mutex
		streams     map[streamKey]*streamEntry
		media       map[endpointKey]MediaLabels
		mediaOwners map[endpointKey][]string
		callMedia   map[string]map[endpointKey]MediaLabels
		rtcpMedia   map[endpointKey]rtcpMediaEntry // RTCP endpoints for BPF cleanup and RTCP→RTP correlation
		rtcpOwners  map[endpointKey][]string
		callRTCP    map[string]map[endpointKey]rtcpMediaEntry
		callRTP     map[string]map[endpointKey]struct{} // per-CallID endpoints that ever had RTP (TTL-independent)
		ssrcIndex   map[uint32][]streamKey              // SSRC → stream keys (multi-valued: an SSRC may be reused across endpoints)
		ttl         time.Duration
		now         func() time.Time
	}

	// streamEntry bundles a stream state with its correlation labels.
	streamEntry struct {
		state        *StreamState
		labels       MediaLabels
		codec        string
		lastRTCP     time.Time // last RTCP arrival (TTL refresh only; does NOT affect jitter)
		rtcpPrevLoss int32     // last RTCP cumulative-lost seen for this SSRC (delta tracking)
		rtcpLossSeen bool      // whether an RTCP RR has established the loss baseline
	}

	// RTCPContext is the resolved context of an RTP stream needed to emit RTCP
	// metrics for a report block that names the stream's SSRC.
	RTCPContext struct {
		Labels    MediaLabels
		Codec     string
		ClockRate uint32
	}
)

// NewTracker creates a Tracker that expires idle streams after ttl.
func NewTracker(ttl time.Duration) *Tracker {
	return &Tracker{
		streams:     make(map[streamKey]*streamEntry),
		media:       make(map[endpointKey]MediaLabels),
		mediaOwners: make(map[endpointKey][]string),
		callMedia:   make(map[string]map[endpointKey]MediaLabels),
		rtcpMedia:   make(map[endpointKey]rtcpMediaEntry),
		rtcpOwners:  make(map[endpointKey][]string),
		callRTCP:    make(map[string]map[endpointKey]rtcpMediaEntry),
		callRTP:     make(map[string]map[endpointKey]struct{}),
		ssrcIndex:   make(map[uint32][]streamKey),
		ttl:         ttl,
		now:         time.Now,
	}
}

// SetNow overrides the clock used for expiry (for testing).
func (t *Tracker) SetNow(now func() time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.now = now
}

// SetTTL updates the idle-stream expiry threshold (RFC 3550 §6.3.5 timeout).
// Used to tune expiry from config (SIP_EXPORTER_RTP_STREAM_TTL) after construction.
func (t *Tracker) SetTTL(ttl time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ttl = ttl
}

// Register associates a media endpoint (IP:port) with SIP-dialog labels and
// reports whether the dialog newly owns the endpoint.
func (t *Tracker) Register(ip string, port uint16, labels MediaLabels) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := endpointKey{ip: ip, port: port}
	if t.callMedia[labels.CallID] == nil {
		t.callMedia[labels.CallID] = make(map[endpointKey]MediaLabels)
	}
	_, owned := t.callMedia[labels.CallID][key]
	t.callMedia[labels.CallID][key] = labels
	if !owned {
		t.mediaOwners[key] = append(t.mediaOwners[key], labels.CallID)
	}
	t.media[key] = labels
	return !owned
}

// RegisterRTCP records a separate RTCP endpoint (IP:port) for BPF-map cleanup
// and maps it to its RTP endpoint for SSRC-collision disambiguation.
func (t *Tracker) RegisterRTCP(
	ip string, port uint16,
	rtpIP string, rtpPort uint16,
	callID string,
) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := endpointKey{ip: ip, port: port}
	entry := rtcpMediaEntry{
		callID:      callID,
		rtpEndpoint: endpointKey{ip: rtpIP, port: rtpPort},
	}
	if t.callRTCP[callID] == nil {
		t.callRTCP[callID] = make(map[endpointKey]rtcpMediaEntry)
	}
	_, owned := t.callRTCP[callID][key]
	t.callRTCP[callID][key] = entry
	if !owned {
		t.rtcpOwners[key] = append(t.rtcpOwners[key], callID)
	}
	t.rtcpMedia[key] = entry
	return !owned
}

// Unregister removes all media endpoints and RTP streams belonging to a SIP
// dialog (called on BYE 200 OK or Session-Expires cleanup) and returns a
// summary of the RTP activity observed for that dialog, plus the list of
// deleted media endpoints (for BPF map cleanup).
func (t *Tracker) Unregister(callID string) (RTPDialogResult, []MediaEndpoint) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var deleted []MediaEndpoint
	mediaCount := len(t.callMedia[callID])
	for k := range t.callMedia[callID] {
		deleted = append(deleted, MediaEndpoint{IP: k.ip, Port: k.port})
		t.mediaOwners[k] = removeOwner(t.mediaOwners[k], callID)
		if len(t.mediaOwners[k]) == 0 {
			delete(t.mediaOwners, k)
			delete(t.media, k)
			continue
		}
		owner := t.mediaOwners[k][len(t.mediaOwners[k])-1]
		t.media[k] = t.callMedia[owner][k]
	}
	delete(t.callMedia, callID)

	for k := range t.callRTCP[callID] {
		deleted = append(deleted, MediaEndpoint{IP: k.ip, Port: k.port})
		t.rtcpOwners[k] = removeOwner(t.rtcpOwners[k], callID)
		if len(t.rtcpOwners[k]) == 0 {
			delete(t.rtcpOwners, k)
			delete(t.rtcpMedia, k)
			continue
		}
		owner := t.rtcpOwners[k][len(t.rtcpOwners[k])-1]
		t.rtcpMedia[k] = t.callRTCP[owner][k]
	}
	delete(t.callRTCP, callID)

	rtpEndpointCount := 0
	if eps, ok := t.callRTP[callID]; ok {
		rtpEndpointCount = len(eps)
		delete(t.callRTP, callID)
	}

	for k, e := range t.streams {
		if e.labels.CallID == callID {
			t.removeSSRCIndex(k)
			delete(t.streams, k)
		}
	}

	return RTPDialogResult{
		MediaExpected: mediaCount > 0,
		RTPObserved:   rtpEndpointCount > 0,
		OneWay:        mediaCount >= 2 && rtpEndpointCount == 1,
	}, deleted
}

func removeOwner(owners []string, callID string) []string {
	for i, owner := range owners {
		if owner == callID {
			return append(owners[:i], owners[i+1:]...)
		}
	}
	return owners
}

// Lookup resolves a media endpoint to its labels.
func (t *Tracker) Lookup(ip string, port uint16) (MediaLabels, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	l, ok := t.media[endpointKey{ip: ip, port: port}]
	return l, ok
}

// lookupLabels resolves a packet's media endpoint trying destination first
// (local receive endpoint, NAT-robust), then source. Returns the matched labels,
// the endpoint key used for flow identity, and which candidate matched ("dst" or
// "src") so FAS side-gating can reject src-fallback matches on offer endpoints.
func (t *Tracker) lookupLabels(
	srcIP string, srcPort uint16,
	dstIP string, dstPort uint16,
) (MediaLabels, endpointKey, string, bool) {
	dst := endpointKey{ip: dstIP, port: dstPort}
	if l, ok := t.media[dst]; ok {
		return l, dst, matchedByDst, true
	}
	src := endpointKey{ip: srcIP, port: srcPort}
	if l, ok := t.media[src]; ok {
		return l, src, matchedBySrc, true
	}
	return MediaLabels{}, endpointKey{}, "", false
}

// Observe ingests an RTP packet. Correlation tries the destination endpoint
// first (the local media endpoint that receives the stream — robust to NAT/asymmetric
// RTP where the source port is remapped), then falls back to the source endpoint.
// Returns (result, false) when neither is correlated to a SIP dialog (drop).
func (t *Tracker) Observe(
	srcIP string, srcPort uint16,
	dstIP string, dstPort uint16,
	h rtp.Header, arrival time.Time,
) (ObserveResult, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	labels, ep, matchedBy, ok := t.lookupLabels(srcIP, srcPort, dstIP, dstPort)
	if !ok {
		return ObserveResult{}, false
	}

	codec := rtp.CodecName(h.PayloadType, labels.SDPCodecs)
	clockRate := uint32(defaultClockRate)
	if cr, crOk := labels.ClockRates[h.PayloadType]; crOk && cr > 0 {
		clockRate = cr
	}

	key := streamKey{endpoint: ep, ssrc: h.SSRC}
	entry, exists := t.streams[key]
	if !exists {
		entry = &streamEntry{
			state:  newStreamState(h.SSRC, codec, clockRate, arrival),
			labels: labels,
			codec:  codec,
		}
		t.streams[key] = entry
		t.ssrcIndex[h.SSRC] = append(t.ssrcIndex[h.SSRC], key)
		if t.callRTP[labels.CallID] == nil {
			t.callRTP[labels.CallID] = make(map[endpointKey]struct{})
		}
		t.callRTP[labels.CallID][ep] = struct{}{}
	}

	prevLost := entry.state.packetsLost
	prevTotal := entry.state.packetsTotal
	prevDup := entry.state.packetsDuplicate
	prevReorder := entry.state.packetsReorder
	if rtp.IsAudioCodec(codec) {
		entry.state.Observe(h, arrival)
	} else {
		entry.state.ObserveNonAudio(h, arrival)
	}

	var lostDelta uint64
	if entry.state.packetsLost >= prevLost {
		lostDelta = entry.state.packetsLost - prevLost
	}

	return ObserveResult{
		Counted:            entry.state.packetsTotal > prevTotal,
		Duplicate:          entry.state.packetsDuplicate > prevDup,
		Reorder:            entry.state.packetsReorder > prevReorder,
		Lost:               lostDelta,
		DelayVariationMs:   entry.state.lastPacketDelayVariationMs,
		StreamPacketsTotal: entry.state.packetsTotal,
		MatchedIP:          ep.ip,
		MatchedPort:        ep.port,
		MatchedBy:          matchedBy,
		Codec:              codec,
		Carrier:            labels.Carrier,
		UAType:             labels.UAType,
		SourceCountry:      labels.SourceCountry,
		Direction:          labels.Direction,
		CallID:             labels.CallID,
	}, true
}

// Snapshot returns the current statistics of all active RTP streams.
// Raw counters and cheap divisions are copied under the lock; expensive
// MOS/R-factor computation is deferred outside to minimize contention
// with Observe.
func (t *Tracker) Snapshot() []StreamStats {
	t.mu.Lock()
	out := make([]StreamStats, 0, len(t.streams))
	for _, e := range t.streams {
		s := e.state
		s.classifyLossRun()
		out = append(out, StreamStats{
			SSRC:             s.SSRC,
			Codec:            e.codec,
			Carrier:          e.labels.Carrier,
			UAType:           e.labels.UAType,
			SourceCountry:    e.labels.SourceCountry,
			Direction:        e.labels.Direction,
			CallID:           e.labels.CallID,
			PacketsTotal:     s.packetsTotal,
			PacketsLost:      s.packetsLost,
			PacketsDuplicate: s.packetsDuplicate,
			JitterMs:         s.JitterMs(),
			BurstLossDensity: s.BurstLossDensity(),
			GapLossDensity:   s.GapLossDensity(),
			LastSeen:         s.lastArrival,
		})
	}
	t.mu.Unlock()

	for i := range out {
		st := &out[i]
		expected := st.PacketsTotal + st.PacketsLost
		var lossRate float64
		if expected > 0 {
			lossRate = float64(st.PacketsLost) / float64(expected)
		}
		r := ComputeRFactor(st.Codec, lossRate, st.JitterMs)
		st.RFactor = r
		st.MOS = mosFromR(r)
		st.MOSF1 = ComputeMOSF1(st.Codec, lossRate, st.JitterMs)
		st.MOSF2 = ComputeMOSF2(st.Codec, lossRate, st.JitterMs)
		st.MOSAdaptive = ComputeMOSAdaptive(st.Codec, lossRate, st.JitterMs)
	}
	return out
}

// Cleanup removes streams idle for longer than the configured TTL. A stream is
// considered active if either RTP or RTCP has been observed within the TTL
// window — RTCP reports (sent every 5s per RFC 3550) keep the stream alive
// during RTP pauses (hold, mute, one-way audio), preventing quality metrics
// from degrading to orphans.
func (t *Tracker) Cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for key, e := range t.streams {
		last := e.state.lastArrival
		if e.lastRTCP.After(last) {
			last = e.lastRTCP
		}
		if now.Sub(last) > t.ttl {
			t.removeSSRCIndex(key)
			delete(t.streams, key)
		}
	}
}

// removeSSRCIndex drops a stream key from the SSRC index. Caller holds t.mu.
func (t *Tracker) removeSSRCIndex(key streamKey) {
	keys := t.ssrcIndex[key.ssrc]
	for i, k := range keys {
		if k == key {
			if len(keys) == 1 {
				delete(t.ssrcIndex, key.ssrc)
				return
			}
			t.ssrcIndex[key.ssrc] = append(keys[:i], keys[i+1:]...)
			return
		}
	}
}

// LookupBySSRC resolves an SSRC (carried in an RTCP report block) to the context of
// the tracked RTP stream sending with that SSRC, enabling RTCP↔RTP correlation.
// When multiple streams share an SSRC (reuse across endpoints), the RTCP packet's
// endpoints disambiguate — destination first (NAT-robust, mirroring lookupLabels),
// then source. A unique SSRC resolves even without an endpoint match; an ambiguous
// SSRC (collision) without a matching endpoint returns false (cannot attribute
// safely). Returns false if no stream tracks the SSRC.
//
// Read-only: use this when you need just the context. The RTCP metric hot path
// uses RecordRTCP, which resolves the context and records the loss delta under a
// single lock (do NOT pair this lookup with a separate mutation — that reintroduces
// a TOCTOU window).
func (t *Tracker) LookupBySSRC(
	ssrc uint32,
	srcIP string, srcPort uint16,
	dstIP string, dstPort uint16,
) (RTCPContext, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.selectStream(ssrc, srcIP, srcPort, dstIP, dstPort)
	if e == nil {
		return RTCPContext{}, false
	}
	return rtcpContext(e), true
}

// RecordRTCP records an RTCP RR observation for an SSRC and atomically returns
// the resolved stream context together with the cumulative-loss delta since the
// previous RR — the amount to add to rtcp_cumulative_loss_total. Performing the
// lookup and the delta update under a single lock guarantees the labels and the
// delta attribute to the SAME stream (no TOCTOU window where Cleanup could
// remove or replace the stream between a separate lookup and update). The first
// observation establishes the baseline and returns a zero delta (avoiding a
// rate() spike from cumulative-0 at hot start). A negative cumulative-lost
// (duplicates exceeding losses, RFC 3550 §6.4.1) returns a zero delta and
// preserves the baseline. A 24-bit wrap or session reset (current less than
// previous) yields the current value as the delta. Returns ok=false when the
// SSRC is untracked or cannot be attributed unambiguously (caller counts it as
// an uncorrelated report).
func (t *Tracker) RecordRTCP(
	ssrc uint32, cumulative int32,
	srcIP string, srcPort uint16,
	dstIP string, dstPort uint16,
) (RTCPContext, uint64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.selectStream(ssrc, srcIP, srcPort, dstIP, dstPort)
	if e == nil {
		return RTCPContext{}, 0, false
	}
	e.lastRTCP = t.now()
	if cumulative < 0 {
		if !e.rtcpLossSeen {
			e.rtcpLossSeen = true
			e.rtcpPrevLoss = cumulative
		}
		return rtcpContext(e), 0, true
	}
	var delta uint64
	switch {
	case !e.rtcpLossSeen:
		e.rtcpLossSeen = true
	case cumulative >= e.rtcpPrevLoss:
		delta = uint64(cumulative - e.rtcpPrevLoss)
	default:
		delta = uint64(cumulative) // 24-bit wrap or session reset
	}
	e.rtcpPrevLoss = cumulative
	return rtcpContext(e), delta, true
}

// selectStream finds the tracked stream for an SSRC, disambiguating collisions by
// the RTCP packet's endpoints (destination first, then source). A unique SSRC
// (one stream) resolves even without an endpoint match — there is no collision
// to mis-attribute. An ambiguous SSRC (collision) without a matching endpoint
// returns nil so the caller counts the report as uncorrelated rather than
// attributing it to an arbitrary stream's labels. Caller holds t.mu.
func (t *Tracker) selectStream(
	ssrc uint32,
	srcIP string, srcPort uint16,
	dstIP string, dstPort uint16,
) *streamEntry {
	keys, ok := t.ssrcIndex[ssrc]
	if !ok || len(keys) == 0 {
		return nil
	}
	for _, ep := range []endpointKey{
		{ip: dstIP, port: dstPort},
		{ip: srcIP, port: srcPort},
	} {
		if rtcp, found := t.rtcpMedia[ep]; found {
			ep = rtcp.rtpEndpoint
		}
		for _, k := range keys {
			if k.endpoint != ep {
				continue
			}
			if e, found := t.streams[k]; found {
				return e
			}
		}
	}
	if len(keys) == 1 {
		// Unique SSRC: no collision possible, safe to attribute.
		if e, found := t.streams[keys[0]]; found {
			return e
		}
	}
	// Ambiguous SSRC without an endpoint match: cannot attribute safely.
	return nil
}

// rtcpContext builds the RTCP correlation context from a stream entry.
func rtcpContext(e *streamEntry) RTCPContext {
	return RTCPContext{Labels: e.labels, Codec: e.codec, ClockRate: e.state.clockRate}
}

// StreamCount returns the number of active RTP streams.
func (t *Tracker) StreamCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.streams)
}
