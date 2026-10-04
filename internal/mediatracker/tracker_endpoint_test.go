package mediatracker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTrackerBinaryEndpointKeyParity(t *testing.T) {
	tests := []struct {
		name      string
		peerIP    string
		matchedIP string
		rtcpIP    string
		missingIP string
		outsideIP string
	}{
		{
			name:      "IPv4",
			peerIP:    "192.0.2.10",
			matchedIP: "192.0.2.20",
			rtcpIP:    "192.0.2.21",
			missingIP: "192.0.2.99",
			outsideIP: "198.51.100.1",
		},
		{
			name:      "fallback strings",
			peerIP:    "peer.invalid",
			matchedIP: "matched.invalid",
			rtcpIP:    "rtcp.invalid",
			missingIP: "missing.invalid",
			outsideIP: "outside.invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTracker(30 * time.Second)
			peerLabels := sampleLabels("call-1")
			peerLabels.Carrier = "peer-carrier"
			matchedLabels := sampleLabels("call-1")
			matchedLabels.Carrier = "matched-carrier"

			tr.Register(tt.peerIP, 4000, peerLabels)
			tr.Register(tt.matchedIP, 5000, matchedLabels)
			require.Contains(t, tr.media, newBinaryEndpointKey(tt.matchedIP, 5000))

			otherLabels := sampleLabels("call-2")
			otherLabels.Carrier = "temporary-owner"
			tr.Register(tt.matchedIP, 5000, otherLabels)
			owned, ok := tr.Lookup(tt.matchedIP, 5000)
			require.True(t, ok)
			require.Equal(t, "temporary-owner", owned.Carrier)
			_, deleted := tr.Unregister("call-2")
			require.Equal(t, []MediaEndpoint{{IP: tt.matchedIP, Port: 5000}}, deleted)
			owned, ok = tr.Lookup(tt.matchedIP, 5000)
			require.True(t, ok)
			require.Equal(t, "matched-carrier", owned.Carrier)

			t0 := time.Unix(1000, 0)
			dstResult, ok := tr.Observe(
				tt.peerIP, 4000, tt.matchedIP, 5000, newHeaderSSRC(1, 1), t0,
			)
			require.True(t, ok)
			require.Equal(t, matchedByDst, dstResult.MatchedBy)
			require.Equal(t, tt.matchedIP, dstResult.MatchedIP)
			require.Equal(t, "matched-carrier", dstResult.Carrier)

			srcResult, ok := tr.Observe(
				tt.peerIP, 4000, tt.missingIP, 9000, newHeaderSSRC(1, 2), t0,
			)
			require.True(t, ok)
			require.Equal(t, matchedBySrc, srcResult.MatchedBy)
			require.Equal(t, tt.peerIP, srcResult.MatchedIP)
			require.Equal(t, "peer-carrier", srcResult.Carrier)

			aliasResult, ok := tr.Observe(
				tt.peerIP, 4100, tt.matchedIP, 5000, newHeaderSSRC(1, 3), t0,
			)
			require.True(t, ok)
			require.Equal(t, &MediaEndpoint{IP: tt.peerIP, Port: 4100}, aliasResult.LearnedEndpoint)

			const sharedSSRC uint32 = 4
			_, ok = tr.Observe(
				tt.outsideIP, 9001, tt.peerIP, 4000, newHeaderSSRC(1, sharedSSRC), t0,
			)
			require.True(t, ok)
			_, ok = tr.Observe(
				tt.outsideIP, 9002, tt.matchedIP, 5000, newHeaderSSRC(1, sharedSSRC), t0,
			)
			require.True(t, ok)
			require.True(t, tr.RegisterRTCP(tt.rtcpIP, 5001, tt.matchedIP, 5000, "call-1"))
			tr.SetNow(func() time.Time { return t0.Add(10 * time.Second) })
			ctx, delta, ok := tr.RecordRTCP(sharedSSRC, 0, tt.outsideIP, 9003, tt.rtcpIP, 5001)
			require.True(t, ok)
			require.Zero(t, delta)
			require.Equal(t, "matched-carrier", ctx.Labels.Carrier)

			require.ElementsMatch(t, []MediaEndpoint{
				{IP: tt.peerIP, Port: 4000},
				{IP: tt.matchedIP, Port: 5000},
				{IP: tt.peerIP, Port: 4100},
				{IP: tt.rtcpIP, Port: 5001},
			}, tr.OwnedEndpoints("call-1"))
			require.Len(t, tr.Snapshot(), 5)

			tr.SetNow(func() time.Time { return t0.Add(time.Minute) })
			tr.Cleanup()
			require.Empty(t, tr.Snapshot())
			_, ok = tr.LookupBySSRC(sharedSSRC, tt.outsideIP, 9003, tt.rtcpIP, 5001)
			require.False(t, ok)
		})
	}
}

func TestTrackerObserveReusesMatchedIPString(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	tr.Register("192.0.2.20", 5000, sampleLabels("call-1"))
	h := newHeaderSSRC(0, 1)
	allMatched := true

	allocs := testing.AllocsPerRun(100, func() {
		h.SequenceNumber++
		result, ok := tr.Observe(
			"198.51.100.1", 4000, "192.0.2.20", 5000, h, time.Unix(1000, 0),
		)
		allMatched = allMatched && ok && result.MatchedIP == "192.0.2.20"
	})

	require.True(t, allMatched)
	require.LessOrEqual(t, allocs, float64(1))
}

func TestTrackerObserveIPv4CorrelatesWithoutAllocations(t *testing.T) {
	tests := []struct {
		name        string
		src         [4]byte
		dst         [4]byte
		wantMatched [4]byte
		wantBy      string
	}{
		{
			name:        "destination match",
			src:         [4]byte{198, 51, 100, 1},
			dst:         [4]byte{192, 0, 2, 20},
			wantMatched: [4]byte{192, 0, 2, 20},
			wantBy:      matchedByDst,
		},
		{
			name:        "source fallback",
			src:         [4]byte{192, 0, 2, 20},
			dst:         [4]byte{198, 51, 100, 1},
			wantMatched: [4]byte{192, 0, 2, 20},
			wantBy:      matchedBySrc,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTracker(30 * time.Second)
			tr.Register("192.0.2.20", 5000, sampleLabels("call-1"))
			h := newHeaderSSRC(0, 1)
			allMatched := true

			allocs := testing.AllocsPerRun(100, func() {
				h.SequenceNumber++
				result, ok := tr.ObserveIPv4(tt.src, 5000, tt.dst, 5000, h, time.Unix(1000, 0))
				allMatched = allMatched && ok && result.MatchedIPv4 == tt.wantMatched && result.MatchedBy == tt.wantBy
			})

			require.True(t, allMatched)
			require.Zero(t, allocs)
		})
	}
}

func TestTrackerRecordRTCPIPv4DisambiguatesSharedSSRC(t *testing.T) {
	tr := NewTracker(30 * time.Second)
	firstLabels := sampleLabels("call-1")
	firstLabels.Carrier = "first"
	secondLabels := sampleLabels("call-2")
	secondLabels.Carrier = "second"
	tr.Register("192.0.2.10", 5000, firstLabels)
	tr.Register("192.0.2.20", 6000, secondLabels)
	const ssrc uint32 = 42
	_, ok := tr.ObserveIPv4(
		[4]byte{198, 51, 100, 1}, 4000, [4]byte{192, 0, 2, 10}, 5000,
		newHeaderSSRC(1, ssrc), time.Unix(1000, 0),
	)
	require.True(t, ok)
	_, ok = tr.ObserveIPv4(
		[4]byte{198, 51, 100, 2}, 4000, [4]byte{192, 0, 2, 20}, 6000,
		newHeaderSSRC(1, ssrc), time.Unix(1000, 0),
	)
	require.True(t, ok)

	ctx, delta, ok := tr.RecordRTCPIPv4(
		ssrc, 0, [4]byte{198, 51, 100, 2}, 4001, [4]byte{192, 0, 2, 20}, 6000,
	)

	require.True(t, ok)
	require.Zero(t, delta)
	require.Equal(t, "second", ctx.Labels.Carrier)
}
