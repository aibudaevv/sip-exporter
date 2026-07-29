package mediatracker

import (
	"sync"
	"sync/atomic"
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

func TestCorrelatorRegisterAndLookup(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))

	got, ok := tr.Lookup("10.0.0.1", 5004)
	require.True(t, ok)
	require.Equal(t, "carrier-a", got.Carrier)
	require.Equal(t, "call-1", got.CallID)

	_, ok = tr.Lookup("10.0.0.1", 9999)
	require.False(t, ok)
}

func TestTrackerLearnSourceAlias(t *testing.T) {
	tests := []struct {
		name       string
		endpoints  []MediaEndpoint
		matched    MediaEndpoint
		sourceIP   string
		sourcePort uint16
		want       MediaEndpoint
		wantOK     bool
	}{
		{
			name: "two endpoints with same peer IP learns remapped port",
			endpoints: []MediaEndpoint{
				{IP: "10.0.0.1", Port: 4000},
				{IP: "10.0.0.2", Port: 5000},
			},
			matched:    MediaEndpoint{IP: "10.0.0.2", Port: 5000},
			sourceIP:   "10.0.0.1",
			sourcePort: 4100,
			want:       MediaEndpoint{IP: "10.0.0.1", Port: 4100},
			wantOK:     true,
		},
		{
			name:       "one endpoint is rejected",
			endpoints:  []MediaEndpoint{{IP: "10.0.0.2", Port: 5000}},
			matched:    MediaEndpoint{IP: "10.0.0.2", Port: 5000},
			sourceIP:   "10.0.0.1",
			sourcePort: 4100,
		},
		{
			name: "three endpoints are rejected",
			endpoints: []MediaEndpoint{
				{IP: "10.0.0.1", Port: 4000},
				{IP: "10.0.0.2", Port: 5000},
				{IP: "10.0.0.3", Port: 6000},
			},
			matched:    MediaEndpoint{IP: "10.0.0.2", Port: 5000},
			sourceIP:   "10.0.0.1",
			sourcePort: 4100,
		},
		{
			name: "changed source IP is rejected",
			endpoints: []MediaEndpoint{
				{IP: "10.0.0.1", Port: 4000},
				{IP: "10.0.0.2", Port: 5000},
			},
			matched:    MediaEndpoint{IP: "10.0.0.2", Port: 5000},
			sourceIP:   "10.0.0.99",
			sourcePort: 4100,
		},
		{
			name: "unchanged source port is rejected",
			endpoints: []MediaEndpoint{
				{IP: "10.0.0.1", Port: 4000},
				{IP: "10.0.0.2", Port: 5000},
			},
			matched:    MediaEndpoint{IP: "10.0.0.2", Port: 5000},
			sourceIP:   "10.0.0.1",
			sourcePort: 4000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTracker(30 * time.Second)
			for _, endpoint := range tt.endpoints {
				tr.Register(endpoint.IP, endpoint.Port, sampleLabels("call-1"))
			}

			got, ok := tr.LearnSourceAlias(
				"call-1", tt.matched.IP, tt.matched.Port, tt.sourceIP, tt.sourcePort,
			)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTrackerLearnSourceAliasRejectsRepeatedAlias(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 4000, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5000, sampleLabels("call-1"))

	_, ok := tr.LearnSourceAlias("call-1", "10.0.0.2", 5000, "10.0.0.1", 4100)
	require.True(t, ok)

	got, ok := tr.LearnSourceAlias("call-1", "10.0.0.2", 5000, "10.0.0.1", 4100)
	require.False(t, ok)
	require.Equal(t, MediaEndpoint{}, got)

	got, ok = tr.LearnSourceAlias("call-1", "10.0.0.2", 5000, "10.0.0.1", 4200)
	require.False(t, ok)
	require.Equal(t, MediaEndpoint{}, got)
}

func TestTrackerCanLearnSourceAliasOwner(t *testing.T) {
	matched := endpointKey{ip: "10.0.0.2", port: 5000}
	tests := []struct {
		name  string
		setup func(*Tracker)
		want  bool
	}{
		{
			name: "matching unique owner",
			setup: func(tr *Tracker) {
				tr.Register(matched.ip, matched.port, sampleLabels("call-1"))
			},
			want: true,
		},
		{
			name:  "zero owners",
			setup: func(*Tracker) {},
		},
		{
			name: "two owners",
			setup: func(tr *Tracker) {
				tr.Register(matched.ip, matched.port, sampleLabels("call-1"))
				tr.Register(matched.ip, matched.port, sampleLabels("call-2"))
			},
		},
		{
			name: "foreign unique owner",
			setup: func(tr *Tracker) {
				tr.Register(matched.ip, matched.port, sampleLabels("call-2"))
			},
		},
		{
			name: "shared owner removal restores eligibility",
			setup: func(tr *Tracker) {
				tr.Register(matched.ip, matched.port, sampleLabels("call-1"))
				tr.Register(matched.ip, matched.port, sampleLabels("call-2"))
				tr.Unregister("call-2")
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTracker(30 * time.Second)
			tt.setup(tr)

			tr.mu.Lock()
			got := tr.canLearnSourceAlias("call-1", matched)
			tr.mu.Unlock()
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTrackerUnregisterReturnsLearnedAlias(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 4000, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5000, sampleLabels("call-1"))

	_, ok := tr.LearnSourceAlias("call-1", "10.0.0.2", 5000, "10.0.0.1", 4100)
	require.True(t, ok)

	_, deleted := tr.Unregister("call-1")
	require.ElementsMatch(t, []MediaEndpoint{
		{IP: "10.0.0.1", Port: 4000},
		{IP: "10.0.0.2", Port: 5000},
		{IP: "10.0.0.1", Port: 4100},
	}, deleted)
}

func TestCorrelatorUnregisterByCallID(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5006, sampleLabels("call-1"))
	tr.Register("10.0.0.3", 5008, sampleLabels("call-2"))

	_, _ = tr.Unregister("call-1")

	_, ok1 := tr.Lookup("10.0.0.1", 5004)
	_, ok2 := tr.Lookup("10.0.0.2", 5006)
	_, ok3 := tr.Lookup("10.0.0.3", 5008)
	require.False(t, ok1, "endpoint of call-1 must be removed")
	require.False(t, ok2)
	require.True(t, ok3, "endpoint of call-2 must remain")
}

func TestTrackerUnregisterReturnsDeletedEndpoints(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5006, sampleLabels("call-1"))
	tr.Register("10.0.0.3", 5008, sampleLabels("call-2"))

	_, deleted := tr.Unregister("call-1")
	require.Len(t, deleted, 2)
	ips := map[string]bool{deleted[0].IP: true, deleted[1].IP: true}
	require.True(t, ips["10.0.0.1"] && ips["10.0.0.2"], "must return call-1 endpoints only")
}

func TestTrackerUnregisterReturnsSharedEndpointForEachOwner(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.1", 5004, sampleLabels("call-2"))

	_, deletedFirst := tr.Unregister("call-1")
	require.Equal(t, []MediaEndpoint{{IP: "10.0.0.1", Port: 5004}}, deletedFirst)

	labels, ok := tr.Lookup("10.0.0.1", 5004)
	require.True(t, ok)
	require.Equal(t, "call-2", labels.CallID)

	_, deletedSecond := tr.Unregister("call-2")
	require.Equal(t, []MediaEndpoint{{IP: "10.0.0.1", Port: 5004}}, deletedSecond)
	_, ok = tr.Lookup("10.0.0.1", 5004)
	require.False(t, ok)
}

func TestTrackerUnregisterRestoresPreviousSharedEndpointOwner(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.1", 5004, sampleLabels("call-2"))

	_, deleted := tr.Unregister("call-2")
	require.Equal(t, []MediaEndpoint{{IP: "10.0.0.1", Port: 5004}}, deleted)

	labels, ok := tr.Lookup("10.0.0.1", 5004)
	require.True(t, ok)
	require.Equal(t, "call-1", labels.CallID)
}

func TestTrackerDuplicateEndpointRegistrationHasOneOwner(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))

	_, deleted := tr.Unregister("call-1")
	require.Equal(t, []MediaEndpoint{{IP: "10.0.0.1", Port: 5004}}, deleted)
}

func TestTrackerUnregisterReturnsRTCPEndpoints(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.RegisterRTCP("10.0.0.1", 5005, "10.0.0.1", 5004, "call-1")
	tr.Register("10.0.0.3", 5008, sampleLabels("call-2"))
	tr.RegisterRTCP("10.0.0.3", 5009, "10.0.0.3", 5008, "call-2")

	_, deleted := tr.Unregister("call-1")
	require.Len(t, deleted, 2, "must return RTP and RTCP endpoints for call-1")
	ports := map[uint16]bool{}
	for _, ep := range deleted {
		ports[ep.Port] = true
	}
	require.True(t, ports[5004], "RTP port must be in deleted")
	require.True(t, ports[5005], "RTCP port must be in deleted")

	_, deleted2 := tr.Unregister("call-2")
	require.Len(t, deleted2, 2, "call-2 must also return both endpoints")
}

func TestTrackerUnregisterReturnsSharedRTCPEndpointForEachOwner(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5006, sampleLabels("call-2"))
	tr.RegisterRTCP("10.0.0.3", 5005, "10.0.0.1", 5004, "call-1")
	tr.RegisterRTCP("10.0.0.3", 5005, "10.0.0.2", 5006, "call-2")

	_, deletedFirst := tr.Unregister("call-1")
	require.Len(t, deletedFirst, 2)
	require.Contains(t, deletedFirst, MediaEndpoint{IP: "10.0.0.3", Port: 5005})

	_, deletedSecond := tr.Unregister("call-2")
	require.Len(t, deletedSecond, 2)
	require.Contains(t, deletedSecond, MediaEndpoint{IP: "10.0.0.3", Port: 5005})
}

func TestTrackerUnregisterNoRTCPOnlyRTPReturned(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))

	_, deleted := tr.Unregister("call-1")
	require.Len(t, deleted, 1, "rtcp-mux or no RTCP: only RTP returned")
	require.Equal(t, uint16(5004), deleted[0].Port)
}

func TestTrackerUnregisterRTCPDoesNotAffectOneWay(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5006, sampleLabels("call-1"))
	tr.RegisterRTCP("10.0.0.1", 5005, "10.0.0.1", 5004, "call-1")
	tr.RegisterRTCP("10.0.0.2", 5007, "10.0.0.2", 5006, "call-1")
	t0 := time.Unix(1000, 0)
	_, ok := tr.Observe("10.0.0.99", 9999, "10.0.0.1", 5004, newHeader(1, 160), t0)
	require.True(t, ok)

	r, _ := tr.Unregister("call-1")
	require.True(t, r.MediaExpected)
	require.True(t, r.RTPObserved)
	require.True(t, r.OneWay, "RTCP registration must not inflate mediaCount")
}

func TestTrackerObserveNoCorrelationDrop(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	t0 := time.Unix(1000, 0)
	_, ok := tr.Observe("10.0.0.99", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	require.False(t, ok, "RTP without registered endpoint must be dropped")
	require.Empty(t, tr.Snapshot())
}

func TestTrackerObserveWithCorrelation(t *testing.T) {
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

func TestTrackerObserveDuplicateFlag(t *testing.T) {
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

func TestTrackerObserveAliasSharedDestinationCounted(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 4000, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5000, sampleLabels("call-1"))
	tr.Register("10.0.0.1", 4001, sampleLabels("call-2"))
	tr.Register("10.0.0.2", 5000, sampleLabels("call-2"))

	result, ok := tr.Observe(
		"10.0.0.1", 4100, "10.0.0.2", 5000, newHeader(1, 160), time.Unix(1000, 0),
	)
	require.True(t, ok)
	require.True(t, result.Counted)
	require.Nil(t, result.LearnedEndpoint)
}

func TestTrackerObserveAliasDuplicateAndSourceFallbackCounted(t *testing.T) {
	tests := []struct {
		name      string
		endpoints []MediaEndpoint
		observe   func(*testing.T, *Tracker) (ObserveResult, bool)
		counted   bool
		duplicate bool
	}{
		{
			name: "duplicate",
			endpoints: []MediaEndpoint{
				{IP: "10.0.0.1", Port: 4000},
				{IP: "10.0.0.2", Port: 5000},
			},
			observe: func(t *testing.T, tr *Tracker) (ObserveResult, bool) {
				arrival := time.Unix(1000, 0)
				_, ok := tr.Observe("10.0.0.99", 4100, "10.0.0.2", 5000, newHeader(1, 160), arrival)
				require.True(t, ok)
				return tr.Observe(
					"10.0.0.1", 4100, "10.0.0.2", 5000, newHeader(1, 160), arrival.Add(time.Millisecond),
				)
			},
			duplicate: true,
		},
		{
			name: "source fallback",
			endpoints: []MediaEndpoint{
				{IP: "10.0.0.1", Port: 4000},
				{IP: "10.0.0.1", Port: 5000},
			},
			observe: func(_ *testing.T, tr *Tracker) (ObserveResult, bool) {
				return tr.Observe(
					"10.0.0.1", 4000, "10.0.0.99", 5000, newHeader(1, 160), time.Unix(1000, 0),
				)
			},
			counted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTracker(30 * time.Second)
			for _, endpoint := range tt.endpoints {
				tr.Register(endpoint.IP, endpoint.Port, sampleLabels("call-1"))
			}

			result, ok := tt.observe(t, tr)
			require.True(t, ok)
			require.Equal(t, tt.counted, result.Counted)
			require.Equal(t, tt.duplicate, result.Duplicate)
			require.Nil(t, result.LearnedEndpoint)
		})
	}
}

func TestTrackerSnapshotComputesMOSAndJitter(t *testing.T) {
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

func TestTrackerObservePDVPerPacket(t *testing.T) {
	// Each counted forward packet carries its raw deviation in ObserveResult.DelayVariationMs
	// (per-packet observation, VoIPMonitor-parity). Two streams → distinct per-packet PDV.
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-a"))
	tr.Register("10.0.0.2", 5004, sampleLabels("call-b"))
	t0 := time.Unix(1000, 0)

	// Stream A: baseline + late 2nd packet (arrivalDelta=45ms, tsDelta=20ms → d=25ms).
	resA1, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	require.True(t, ok && resA1.Counted)
	require.InDelta(t, 0.0, resA1.DelayVariationMs, 0.0001, "first packet has no reference → PDV 0")
	resA2, _ := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(2, 320), t0.Add(45*time.Millisecond))
	require.True(t, resA2.Counted)
	require.InDelta(t, 25.0, resA2.DelayVariationMs, 0.0001, "stream A late packet → 25ms PDV")

	// Stream B: perfectly spaced 2nd packet (arrivalDelta=tsDelta=20ms → d=0).
	_, _ = tr.Observe("10.0.0.2", 5004, "0.0.0.0", 0, newHeader(1, 160), t0)
	resB2, _ := tr.Observe("10.0.0.2", 5004, "0.0.0.0", 0, newHeader(2, 320), t0.Add(20*time.Millisecond))
	require.True(t, resB2.Counted)
	require.InDelta(t, 0.0, resB2.DelayVariationMs, 0.0001, "stream B perfect spacing → 0ms PDV")

	// Duplicate/reorder packets must not carry a fresh PDV: res.Counted is false AND
	// DelayVariationMs stays at the last forward packet's value (stale, never emitted).
	resDup, _ := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeader(2, 320), t0.Add(46*time.Millisecond))
	require.False(t, resDup.Counted, "duplicate seq must not be Counted")
	require.InDelta(t, 25.0, resDup.DelayVariationMs, 0.0001,
		"duplicate must not update PDV (stale prior forward value)")
}

func TestTrackerSnapshotMOSVariants(t *testing.T) {
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

func TestTrackerSnapshotFlushesPendingLossRun(t *testing.T) {
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

func TestTrackerCleanupExpiredStreams(t *testing.T) {
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

// TestTrackerSetTTLLowersExpiryThreshold verifies that SetTTL changes the
// idle-expiry threshold of an existing tracker: the same elapsed idle time
// must NOT expire a stream under a long TTL, but MUST expire it after SetTTL
// lowers the threshold. This is the seam exercised by SIP_EXPORTER_RTP_STREAM_TTL.
func TestTrackerSetTTLLowersExpiryThreshold(t *testing.T) {
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

func TestTrackerDynamicCodecFromSDP(t *testing.T) {
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

// TestTrackerSSRCReusedAcrossEndpoints verifies that the same SSRC from two
// different media endpoints (two SIP dialogs) is tracked as separate flows,
// not merged into one (regression for SSRC-only keying).
func TestTrackerSSRCReusedAcrossEndpoints(t *testing.T) {
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

// TestTrackerObserveCorrelatesByDst verifies that when the source endpoint is
// unregistered (e.g. NAT/asymmetric RTP remapped the source port) the packet is
// still correlated via its destination endpoint (the local receive port from SDP).
func TestTrackerObserveCorrelatesByDst(t *testing.T) {
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

func TestTrackerStreamRestartNoUnderflow(t *testing.T) {
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

func TestTrackerClockRateFallback(t *testing.T) {
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

func TestTrackerZeroClockRateFallback(t *testing.T) {
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

func TestTrackerUnregisterResultNoMediaNoRTP(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	r, _ := tr.Unregister("call-1")
	require.False(t, r.MediaExpected)
	require.False(t, r.RTPObserved)
	require.False(t, r.OneWay)
}

func TestTrackerUnregisterResultMediaExpectedNoRTP(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5006, sampleLabels("call-1"))
	r, _ := tr.Unregister("call-1")
	require.True(t, r.MediaExpected)
	require.False(t, r.RTPObserved)
	require.False(t, r.OneWay)
}

func TestTrackerUnregisterResultTwoWayRTP(t *testing.T) {
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
	r, _ := tr.Unregister("call-1")
	require.True(t, r.MediaExpected)
	require.True(t, r.RTPObserved)
	require.False(t, r.OneWay)
}

func TestTrackerUnregisterResultOneWayRTP(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	tr.Register("10.0.0.2", 5006, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)
	// RTP only to endpoint 1
	_, ok := tr.Observe("10.0.0.99", 9999, "10.0.0.1", 5004, newHeader(1, 160), t0)
	require.True(t, ok)
	r, _ := tr.Unregister("call-1")
	require.True(t, r.MediaExpected)
	require.True(t, r.RTPObserved)
	require.True(t, r.OneWay, "2 endpoints registered, only 1 with RTP = one-way")
}

func TestTrackerUnregisterResultSurvivesTTL(t *testing.T) {
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

	r, _ := tr.Unregister("call-1")
	require.True(t, r.MediaExpected, "media endpoints persist")
	require.True(t, r.RTPObserved, "RTP fact must survive stream TTL")
	require.False(t, r.OneWay, "two-way RTP was observed")
}

func TestMediaRevisionOwnedEndpoints(t *testing.T) {
	t.Run("snapshot includes RTP RTCP and learned aliases", func(t *testing.T) {
		tr := NewTracker(30 * time.Second)
		tr.Register("10.0.0.1", 4000, sampleLabels("call-1"))
		tr.Register("10.0.0.2", 5000, sampleLabels("call-1"))
		tr.RegisterRTCP("10.0.0.2", 5007, "10.0.0.2", 5000, "call-1")
		_, ok := tr.LearnSourceAlias("call-1", "10.0.0.2", 5000, "10.0.0.1", 4100)
		require.True(t, ok)

		require.ElementsMatch(t, []MediaEndpoint{
			{IP: "10.0.0.1", Port: 4000},
			{IP: "10.0.0.2", Port: 5000},
			{IP: "10.0.0.2", Port: 5007},
			{IP: "10.0.0.1", Port: 4100},
		}, tr.OwnedEndpoints("call-1"))
	})

	t.Run("snapshot preserves coincident RTP and RTCP ownership", func(t *testing.T) {
		tr := NewTracker(30 * time.Second)
		tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
		tr.RegisterRTCP("10.0.0.1", 5004, "10.0.0.1", 5004, "call-1")

		require.Equal(t, []MediaEndpoint{
			{IP: "10.0.0.1", Port: 5004},
			{IP: "10.0.0.1", Port: 5004},
		}, tr.OwnedEndpoints("call-1"))
	})

	t.Run("snapshot stays empty after replacement and unregister", func(t *testing.T) {
		tr := NewTracker(30 * time.Second)
		tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
		tr.Replace("call-1")
		require.Empty(t, tr.OwnedEndpoints("call-1"))
		tr.Replace("call-1")
		require.Empty(t, tr.OwnedEndpoints("call-1"))

		tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
		tr.Unregister("call-1")
		require.Empty(t, tr.OwnedEndpoints("call-1"))
		tr.Unregister("call-1")
		require.Empty(t, tr.OwnedEndpoints("call-1"))
	})
}

// TestTrackerLookupBySSRC verifies RTCP correlation: an SSRC from an RTCP report
// block resolves to the labels of the tracked RTP stream sending with that SSRC.
func TestTrackerLookupBySSRC(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)
	const ssrc uint32 = 0x11223344
	_, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)
	require.True(t, ok)

	ctx, ok := tr.LookupBySSRC(ssrc, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.True(t, ok, "tracked SSRC must resolve")
	require.Equal(t, "call-1", ctx.Labels.CallID)
	require.Equal(t, "carrier-a", ctx.Labels.Carrier)
	require.Equal(t, "PCMU", ctx.Codec, "codec must resolve for RTCP metric labels")
	require.Equal(t, uint32(8000), ctx.ClockRate, "clock rate must resolve for jitter conversion")

	_, ok = tr.LookupBySSRC(0x99999999, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.False(t, ok, "unknown SSRC must not resolve")
}

func TestTrackerLookupBySSRCCollisionDisambiguates(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, MediaLabels{Carrier: "carrier-a", CallID: "call-1",
		SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000}})
	tr.Register("10.0.0.2", 5006, MediaLabels{Carrier: "carrier-b", CallID: "call-2",
		SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000}})
	t0 := time.Unix(1000, 0)
	const ssrc uint32 = 0xABCDEFFF
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)
	_, _ = tr.Observe("10.0.0.2", 5006, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)

	// RTCP matching the second stream's endpoint must resolve to carrier-b.
	ctx, ok := tr.LookupBySSRC(ssrc, "9.9.9.9", 0, "10.0.0.2", 5006)
	require.True(t, ok)
	require.Equal(t, "carrier-b", ctx.Labels.Carrier, "must disambiguate to the dst-matched stream")

	// Matching the first stream's endpoint must resolve to carrier-a.
	ctx, ok = tr.LookupBySSRC(ssrc, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.True(t, ok)
	require.Equal(t, "carrier-a", ctx.Labels.Carrier)
}

// TestTrackerLookupBySSRCCollisionNoMatchReturnsFalse verifies the D1 fix on
// the read path: a colliding SSRC whose RTCP endpoints match none of the
// tracked streams must not resolve to an arbitrary stream.
func TestTrackerLookupBySSRCCollisionNoMatchReturnsFalse(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, MediaLabels{Carrier: "carrier-a", CallID: "call-1",
		SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000}})
	tr.Register("10.0.0.2", 5006, MediaLabels{Carrier: "carrier-b", CallID: "call-2",
		SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000}})
	t0 := time.Unix(1000, 0)
	const ssrc uint32 = 0xABCDEFFF
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)
	_, _ = tr.Observe("10.0.0.2", 5006, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)

	_, ok := tr.LookupBySSRC(ssrc, "5.5.5.5", 0, "6.6.6.6", 0)
	require.False(t, ok, "ambiguous SSRC without endpoint match must not mis-attribute")
}

func TestTrackerRecordRTCP(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)
	const ssrc uint32 = 0xCAFED00D
	_, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)
	require.True(t, ok)
	// Endpoint matching the stream (dst-first), consistent with LookupBySSRC.
	const epSrcIP, epSrcPort = "9.9.9.9", uint16(0)
	const epDstIP, epDstPort = "10.0.0.1", uint16(5004)

	// First RR: cumulative=10 → establishes baseline, delta=0 (no rate() spike at
	// hot start). RecordRTCP returns context and delta from one call; the atomic
	// one-lock guarantee (no TOCTOU between lookup and update) is structural in
	// RecordRTCP itself, not exercised by this synchronous test.
	ctx, delta, ok := tr.RecordRTCP(ssrc, 10, epSrcIP, epSrcPort, epDstIP, epDstPort)
	require.True(t, ok)
	require.Zero(t, delta, "first observation establishes baseline, emits no delta")
	require.Equal(t, "carrier-a", ctx.Labels.Carrier, "context resolves from the same stream")
	require.Equal(t, "PCMU", ctx.Codec)

	// Second RR: cumulative=15 → delta=5.
	_, delta, ok = tr.RecordRTCP(ssrc, 15, epSrcIP, epSrcPort, epDstIP, epDstPort)
	require.True(t, ok)
	require.Equal(t, uint64(5), delta)

	// 24-bit wrap / session reset: cumulative=3 (< 15) → delta=3 (treated as fresh).
	_, delta, ok = tr.RecordRTCP(ssrc, 3, epSrcIP, epSrcPort, epDstIP, epDstPort)
	require.True(t, ok)
	require.Equal(t, uint64(3), delta)

	// No change → delta 0.
	_, delta, ok = tr.RecordRTCP(ssrc, 3, epSrcIP, epSrcPort, epDstIP, epDstPort)
	require.True(t, ok)
	require.Zero(t, delta)

	// Unknown SSRC → ok=false (uncorrelated report).
	_, _, ok = tr.RecordRTCP(0xDEADBEEF, 99, epSrcIP, epSrcPort, epDstIP, epDstPort)
	require.False(t, ok)
}

func TestTrackerRecordRTCPCollisionResolvesSeparateRTCPEndpoints(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	labelsA := sampleLabels("call-a")
	labelsA.Carrier = "carrier-a"
	labelsB := sampleLabels("call-b")
	labelsB.Carrier = "carrier-b"
	tr.Register("10.0.0.1", 5004, labelsA)
	tr.Register("10.0.0.2", 5006, labelsB)
	tr.RegisterRTCP("10.0.0.1", 5005, "10.0.0.1", 5004, "call-a")
	tr.RegisterRTCP("10.0.0.2", 5007, "10.0.0.2", 5006, "call-b")

	const ssrc uint32 = 0xCAFED00D
	t0 := time.Unix(1000, 0)
	_, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)
	require.True(t, ok)
	_, ok = tr.Observe("10.0.0.2", 5006, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)
	require.True(t, ok)

	ctx, delta, ok := tr.RecordRTCP(ssrc, 0, "9.9.9.9", 0, "10.0.0.1", 5005)
	require.True(t, ok)
	require.Equal(t, "carrier-a", ctx.Labels.Carrier)
	require.Zero(t, delta)

	ctx, delta, ok = tr.RecordRTCP(ssrc, 0, "9.9.9.9", 0, "10.0.0.2", 5007)
	require.True(t, ok)
	require.Equal(t, "carrier-b", ctx.Labels.Carrier)
	require.Zero(t, delta)

	_, delta, ok = tr.RecordRTCP(ssrc, 5, "9.9.9.9", 0, "10.0.0.1", 5005)
	require.True(t, ok)
	require.Equal(t, uint64(5), delta)
	_, delta, ok = tr.RecordRTCP(ssrc, 7, "9.9.9.9", 0, "10.0.0.2", 5007)
	require.True(t, ok)
	require.Equal(t, uint64(7), delta)
}

// TestRecordRTCPNegativeCumulativePreservesBaseline proves that a negative
// cumulative-lost value (duplicates exceeding losses, RFC 3550 §6.4.1) does not
// corrupt the delta baseline: 10 → -3 → 13 yields delta=3 (13−10), not 9 (13−0).
func TestRecordRTCPNegativeCumulativePreservesBaseline(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	const ssrc uint32 = 0xCAFE0001
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), time.Unix(1000, 0))

	_, delta, ok := tr.RecordRTCP(ssrc, 10, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.True(t, ok)
	require.Zero(t, delta, "first observation establishes baseline")

	_, delta, ok = tr.RecordRTCP(ssrc, -3, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.True(t, ok)
	require.Zero(t, delta, "negative cumulative emits no delta")

	_, delta, ok = tr.RecordRTCP(ssrc, 13, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.True(t, ok)
	require.Equal(t, uint64(3), delta, "delta=13-10=3, baseline preserved despite negative")
}

// TestRecordRTCPFirstNegativeBaseline proves that when the FIRST RTCP report for
// an SSRC carries a negative cumulative-lost (duplicates exceeding losses,
// RFC 3550 §6.4.1), the baseline is set to the actual value, not zero. Without
// the fix [-5 → 3] yields delta=3 (3−0); with the fix delta=8 (3−(−5)).
func TestRecordRTCPFirstNegativeBaseline(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	const ssrc uint32 = 0xCAFE0002
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), time.Unix(1000, 0))

	_, delta, ok := tr.RecordRTCP(ssrc, -5, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.True(t, ok)
	require.Zero(t, delta, "first observation establishes baseline, emits no delta")

	_, delta, ok = tr.RecordRTCP(ssrc, 3, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.True(t, ok)
	require.Equal(t, uint64(8), delta, "delta=3-(-5)=8, negative baseline preserved")
}

// TestRecordRTCPRefreshesTTL proves that RTCP reports refresh the stream TTL:
// when RTP pauses (hold/mute/one-way) but RTCP keeps arriving, the stream must
// survive Cleanup beyond the RTP-idle window. Without the fix, RecordRTCP never
// updates any timestamp — Cleanup evicts based solely on lastArrival (set only by
// RTP), so the stream expires and subsequent RTCP reports become orphans,
// dropping quality metrics precisely during degradation.
func TestRecordRTCPRefreshesTTL(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)
	const ssrc uint32 = 0xCAFED00D

	// Two RTP packets establish a stream with a non-zero jitter baseline.
	_, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)
	require.True(t, ok)
	_, ok = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(2, ssrc), t0.Add(20*time.Millisecond))
	require.True(t, ok)
	jitterBefore := tr.Snapshot()[0].JitterMs

	// Advance to 20s — approaching TTL expiry since last RTP.
	tr.SetNow(func() time.Time { return t0.Add(20 * time.Second) })

	// RTCP arrives with no new RTP. Must refresh TTL without altering jitter.
	_, _, ok = tr.RecordRTCP(ssrc, 0, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.True(t, ok)
	require.InDelta(t, jitterBefore, tr.Snapshot()[0].JitterMs, 0,
		"RTCP must not alter jitter (lastArrival invariant preserved)")

	// 31s since last RTP (t0), but only 11s since last RTCP (t0+20s).
	tr.SetNow(func() time.Time { return t0.Add(31 * time.Second) })
	tr.Cleanup()

	require.Len(t, tr.Snapshot(), 1, "stream survives: RTCP refreshed TTL beyond RTP-idle window")
}

// TestTrackerRecordRTCPUniqueSSRCResolvesWithoutEndpointMatch verifies the D1
// middle-ground: when an SSRC is unique (one stream), RTCP resolves even if its
// endpoints match no registered endpoint (NAT/remapped port) — there is no
// collision to mis-attribute.
func TestTrackerRecordRTCPUniqueSSRCResolvesWithoutEndpointMatch(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)
	const ssrc uint32 = 0x11223344
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)

	_, _, ok := tr.RecordRTCP(ssrc, 5, "5.5.5.5", 0, "6.6.6.6", 0)
	require.True(t, ok, "unique SSRC resolves even without an endpoint match")
}

// TestTrackerRecordRTCPAmbiguousSSRCWithoutMatchIsOrphan verifies the D1 fix:
// when multiple streams share an SSRC and the RTCP endpoints match none of them,
// the report is uncorrelated (ok=false) rather than mis-attributed to an
// arbitrary stream's labels.
func TestTrackerRecordRTCPAmbiguousSSRCWithoutMatchIsOrphan(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, MediaLabels{Carrier: "carrier-a", CallID: "call-1",
		SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000}})
	tr.Register("10.0.0.2", 5006, MediaLabels{Carrier: "carrier-b", CallID: "call-2",
		SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000}})
	t0 := time.Unix(1000, 0)
	const ssrc uint32 = 0xABCDEFFF
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)
	_, _ = tr.Observe("10.0.0.2", 5006, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)

	_, _, ok := tr.RecordRTCP(ssrc, 5, "5.5.5.5", 0, "6.6.6.6", 0)
	require.False(t, ok, "ambiguous SSRC without endpoint match must not mis-attribute")
}

func TestTrackerLookupBySSRCRemovedOnUnregister(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)
	const ssrc uint32 = 0xDEADBEEF
	_, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)
	require.True(t, ok)

	tr.Unregister("call-1")

	_, ok = tr.LookupBySSRC(ssrc, "10.0.0.1", 5004, "0.0.0.0", 0)
	require.False(t, ok, "SSRC must leave the index when its stream is unregistered")
}

func TestTrackerLookupBySSRCRemovedOnCleanup(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)
	const ssrc uint32 = 0xCAFEBABE
	_, ok := tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)
	require.True(t, ok)

	tr.SetNow(func() time.Time { return t0.Add(31 * time.Second) })
	tr.Cleanup()

	_, ok = tr.LookupBySSRC(ssrc, "10.0.0.1", 5004, "0.0.0.0", 0)
	require.False(t, ok, "SSRC must leave the index when its stream TTL-expires")
}

func TestTrackerLookupBySSRCConcurrent(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	t0 := time.Unix(1000, 0)
	const ssrc uint32 = 0x55667788
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), t0)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			_, _ = tr.LookupBySSRC(ssrc, "9.9.9.9", 0, "10.0.0.1", 5004)
		}
	}()
	for i := range 200 {
		_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(uint16(i+10), ssrc), t0)
	}
	<-done

	// Post-concurrency state must remain consistent (no index corruption).
	_, ok := tr.LookupBySSRC(ssrc, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.True(t, ok, "SSRC must still resolve after concurrent Observe/Lookup")
}

// TestRecordRTCPConcurrent proves thread-safety under concurrent access.
// All goroutines use an IDENTICAL cumulative value (50) deliberately:
// distinct values would make the sum non-deterministic because the 24-bit
// wrap/reset branch in RecordRTCP treats out-of-order arrivals as resets.
// The test verifies: (1) -race detects no data race, (2) the baseline
// survives concurrent access (follow-up +10 yields delta=10, proving
// rtcpPrevLoss==50 was not corrupted). Delta-accounting correctness is
// covered by the sequential TestTrackerRecordRTCP.
func TestRecordRTCPConcurrent(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, sampleLabels("call-1"))
	const ssrc uint32 = 0xBEEF1234
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrc), time.Unix(1000, 0))

	const concurrency = 100
	const cumul = int32(50)
	var sum atomic.Uint64
	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, delta, ok := tr.RecordRTCP(ssrc, cumul, "9.9.9.9", 0, "10.0.0.1", 5004)
			if ok {
				sum.Add(delta)
			}
		}()
	}
	wg.Wait()

	require.Zero(t, sum.Load(), "identical cumulative must produce no delta")

	_, delta, ok := tr.RecordRTCP(ssrc, cumul+10, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.True(t, ok)
	require.Equal(t, uint64(10), delta, "baseline survived concurrent access")
}

// TestRecordRTCPPerLegIsolation proves that two streams with distinct SSRCs
// maintain independent loss baselines: rtcpLossSeen and rtcpPrevLoss are
// per-stream-entry fields, not shared. Interleaved RR observations for SSRC-A
// and SSRC-B must not cross-contaminate deltas.
func TestRecordRTCPPerLegIsolation(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("10.0.0.1", 5004, MediaLabels{Carrier: "carrier-a", CallID: "call-1",
		SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000}})
	tr.Register("10.0.0.2", 5006, MediaLabels{Carrier: "carrier-b", CallID: "call-2",
		SDPCodecs: map[uint8]string{0: "PCMU"}, ClockRates: map[uint8]uint32{0: 8000}})
	t0 := time.Unix(1000, 0)
	const ssrcA uint32 = 0x11110001
	const ssrcB uint32 = 0x22220002
	_, _ = tr.Observe("10.0.0.1", 5004, "0.0.0.0", 0, newHeaderSSRC(1, ssrcA), t0)
	_, _ = tr.Observe("10.0.0.2", 5006, "0.0.0.0", 0, newHeaderSSRC(1, ssrcB), t0)

	// Establish baselines — interleaved to stress per-stream isolation.
	_, dA, ok := tr.RecordRTCP(ssrcA, 10, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.True(t, ok)
	require.Zero(t, dA, "A baseline")
	_, dB, ok := tr.RecordRTCP(ssrcB, 20, "9.9.9.9", 0, "10.0.0.2", 5006)
	require.True(t, ok)
	require.Zero(t, dB, "B baseline")

	// A jumps by 5 (10→15), B by 5 (20→25) — interleaved.
	_, dA, ok = tr.RecordRTCP(ssrcA, 15, "9.9.9.9", 0, "10.0.0.1", 5004)
	require.True(t, ok)
	require.Equal(t, uint64(5), dA, "A delta must be 15-10, not influenced by B")
	_, dB, ok = tr.RecordRTCP(ssrcB, 25, "9.9.9.9", 0, "10.0.0.2", 5006)
	require.True(t, ok)
	require.Equal(t, uint64(5), dB, "B delta must be 25-20, not influenced by A")
}
