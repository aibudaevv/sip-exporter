package mediatracker

import (
	"sync"
	"time"

	"github.com/aibudaevv/sip-exporter/internal/rtp"
)

const defaultClockRate = 8000

type (
	// MediaLabels is the SIP-dialog context attached to a media endpoint via SDP.
	MediaLabels struct {
		Carrier       string
		UAType        string
		SourceCountry string
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
		Counted       bool   // packet counted as received (not duplicate/reorder)
		Duplicate     bool   // packet is a duplicate (same sequence number)
		Lost          uint64 // packets newly marked lost by this observation
		Codec         string // resolved codec name
		Carrier       string // dialog carrier (for metric labels)
		UAType        string // dialog UA type (for metric labels)
		SourceCountry string // dialog source country (for metric labels)
	}

	// RTPDialogResult is the per-dialog RTP summary returned at teardown.
	RTPDialogResult struct {
		MediaExpected bool // at least 1 media endpoint was registered (SDP seen)
		RTPObserved   bool // at least 1 RTP stream was active
		OneWay        bool // 2+ endpoints registered but only 1 has RTP
	}

	endpointKey struct {
		ip   string
		port uint16
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
		mu      sync.Mutex
		streams map[streamKey]*streamEntry
		media   map[endpointKey]MediaLabels
		callRTP map[string]map[endpointKey]struct{} // per-CallID endpoints that ever had RTP (TTL-independent)
		ttl     time.Duration
		now     func() time.Time
	}

	// streamEntry bundles a stream state with its correlation labels.
	streamEntry struct {
		state  *StreamState
		labels MediaLabels
		codec  string
	}
)

// NewTracker creates a Tracker that expires idle streams after ttl.
func NewTracker(ttl time.Duration) *Tracker {
	return &Tracker{
		streams: make(map[streamKey]*streamEntry),
		media:   make(map[endpointKey]MediaLabels),
		callRTP: make(map[string]map[endpointKey]struct{}),
		ttl:     ttl,
		now:     time.Now,
	}
}

// SetNow overrides the clock used for expiry (for testing).
func (t *Tracker) SetNow(now func() time.Time) {
	t.now = now
}

// SetTTL updates the idle-stream expiry threshold (RFC 3550 §6.3.5 timeout).
// Used to tune expiry from config (SIP_EXPORTER_RTP_STREAM_TTL) after construction.
func (t *Tracker) SetTTL(ttl time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ttl = ttl
}

// Register associates a media endpoint (IP:port) with SIP-dialog labels.
func (t *Tracker) Register(ip string, port uint16, labels MediaLabels) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.media[endpointKey{ip: ip, port: port}] = labels
}

// Unregister removes all media endpoints and RTP streams belonging to a SIP
// dialog (called on BYE 200 OK or Session-Expires cleanup) and returns a
// summary of the RTP activity observed for that dialog.
func (t *Tracker) Unregister(callID string) RTPDialogResult {
	t.mu.Lock()
	defer t.mu.Unlock()

	mediaCount := 0
	for k, v := range t.media {
		if v.CallID == callID {
			mediaCount++
			delete(t.media, k)
		}
	}

	rtpEndpointCount := 0
	if eps, ok := t.callRTP[callID]; ok {
		rtpEndpointCount = len(eps)
		delete(t.callRTP, callID)
	}

	for k, e := range t.streams {
		if e.labels.CallID == callID {
			delete(t.streams, k)
		}
	}

	return RTPDialogResult{
		MediaExpected: mediaCount > 0,
		RTPObserved:   rtpEndpointCount > 0,
		OneWay:        mediaCount >= 2 && rtpEndpointCount == 1,
	}
}

// Lookup resolves a media endpoint to its labels.
func (t *Tracker) Lookup(ip string, port uint16) (MediaLabels, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	l, ok := t.media[endpointKey{ip: ip, port: port}]
	return l, ok
}

// lookupLabels resolves a packet's media endpoint trying destination first
// (local receive endpoint, NAT-robust), then source. Returns the matched labels
// and the endpoint key used for flow identity.
func (t *Tracker) lookupLabels(
	srcIP string, srcPort uint16,
	dstIP string, dstPort uint16,
) (MediaLabels, endpointKey, bool) {
	for _, ep := range []endpointKey{
		{ip: dstIP, port: dstPort},
		{ip: srcIP, port: srcPort},
	} {
		if l, ok := t.media[ep]; ok {
			return l, ep, true
		}
	}
	return MediaLabels{}, endpointKey{}, false
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

	labels, ep, ok := t.lookupLabels(srcIP, srcPort, dstIP, dstPort)
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
		if t.callRTP[labels.CallID] == nil {
			t.callRTP[labels.CallID] = make(map[endpointKey]struct{})
		}
		t.callRTP[labels.CallID][ep] = struct{}{}
	}

	prevLost := entry.state.packetsLost
	prevTotal := entry.state.packetsTotal
	prevDup := entry.state.packetsDuplicate
	entry.state.Observe(h, arrival)

	var lostDelta uint64
	if entry.state.packetsLost >= prevLost {
		lostDelta = entry.state.packetsLost - prevLost
	}

	return ObserveResult{
		Counted:       entry.state.packetsTotal > prevTotal,
		Duplicate:     entry.state.packetsDuplicate > prevDup,
		Lost:          lostDelta,
		Codec:         codec,
		Carrier:       labels.Carrier,
		UAType:        labels.UAType,
		SourceCountry: labels.SourceCountry,
	}, true
}

// Snapshot returns the current statistics of all active RTP streams.
func (t *Tracker) Snapshot() []StreamStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]StreamStats, 0, len(t.streams))
	for _, e := range t.streams {
		s := e.state
		jitter := s.JitterMs()
		lossRate := s.LossRate()
		r := ComputeRFactor(e.codec, lossRate, jitter)
		s.classifyLossRun()
		out = append(out, StreamStats{
			SSRC:             s.SSRC,
			Codec:            e.codec,
			Carrier:          e.labels.Carrier,
			UAType:           e.labels.UAType,
			SourceCountry:    e.labels.SourceCountry,
			CallID:           e.labels.CallID,
			PacketsTotal:     s.packetsTotal,
			PacketsLost:      s.packetsLost,
			PacketsDuplicate: s.packetsDuplicate,
			JitterMs:         jitter,
			MOS:              mosFromR(r),
			MOSF1:            ComputeMOSF1(e.codec, lossRate, jitter),
			MOSF2:            ComputeMOSF2(e.codec, lossRate, jitter),
			MOSAdaptive:      ComputeMOSAdaptive(e.codec, lossRate, jitter),
			RFactor:          r,
			BurstLossDensity: s.BurstLossDensity(),
			GapLossDensity:   s.GapLossDensity(),
			LastSeen:         s.lastArrival,
		})
	}
	return out
}

// Cleanup removes streams idle for longer than the configured TTL.
func (t *Tracker) Cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for key, e := range t.streams {
		if now.Sub(e.state.lastArrival) > t.ttl {
			delete(t.streams, key)
		}
	}
}

// StreamCount returns the number of active RTP streams.
func (t *Tracker) StreamCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.streams)
}
