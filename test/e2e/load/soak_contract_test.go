//go:build e2e

package load

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type postDrainHTTPResponse struct {
	status int
	body   string
}

func newPostDrainSequenceServer(
	t *testing.T,
	responses []postDrainHTTPResponse,
) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	require.NotEmpty(t, responses)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(requests.Add(1)) - 1
		if index >= len(responses) {
			index = len(responses) - 1
		}
		response := responses[index]
		if response.status != 0 {
			w.WriteHeader(response.status)
		}
		_, err := io.WriteString(w, response.body)
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func postDrainMetrics(channelLength, activeDialogs, activeTrackers float64, marker string) string {
	return fmt.Sprintf("# %s\nsip_exporter_channel_length %v\n"+
		"sip_exporter_active_dialogs %v\n"+
		"sip_exporter_active_trackers{type=\"invite\"} %v\n",
		marker, channelLength, activeDialogs, activeTrackers)
}

func TestSummarizeSoakWorkingSetUsesHalfOpenMinuteWindows(t *testing.T) {
	start := time.Date(2026, time.August, 18, 10, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)
	samples := []resourceSample{
		{At: start.Add(30 * time.Second), WorkingSetBytes: 60 << 20},
		{At: start, WorkingSetBytes: 40 << 20},
		{At: start.Add(time.Minute), WorkingSetBytes: 1 << 20},
		{At: end.Add(-time.Minute), WorkingSetBytes: 50 << 20},
		{At: end.Add(-30 * time.Second), WorkingSetBytes: 60 << 20},
		{At: end, WorkingSetBytes: 1 << 20},
	}
	wantSamples := slices.Clone(samples)

	got, err := summarizeSoakWorkingSet(samples, start, end)

	require.NoError(t, err)
	require.Equal(t, soakWorkingSetGrowth{
		FirstMinuteMedianMB: 50,
		LastMinuteMedianMB:  55,
		GrowthMB:            5,
		AllowedGrowthMB:     8,
	}, got)
	require.Equal(t, wantSamples, samples)
}

func TestSummarizeSoakWorkingSetRejectsInvalidWindows(t *testing.T) {
	start := time.Date(2026, time.August, 18, 10, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)
	valid := []resourceSample{
		{At: start, WorkingSetBytes: 64 << 20},
		{At: end.Add(-time.Minute), WorkingSetBytes: 64 << 20},
	}
	tests := []struct {
		name    string
		samples []resourceSample
		start   time.Time
		end     time.Time
		wantErr string
	}{
		{name: "missing first minute", samples: valid[1:], start: start, end: end, wantErr: "first minute"},
		{name: "missing last minute", samples: valid[:1], start: start, end: end, wantErr: "last minute"},
		{name: "short interval", samples: valid, start: start, end: start.Add(2*time.Minute - time.Nanosecond), wantErr: "at least 2m0s"},
		{name: "zero start", samples: valid, end: end, wantErr: "invalid soak interval"},
		{name: "zero end", samples: valid, start: start, wantErr: "invalid soak interval"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := summarizeSoakWorkingSet(tt.samples, tt.start, tt.end)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestNewSoakWorkingSetGrowthSelectsLimitBranch(t *testing.T) {
	tests := []struct {
		name    string
		first   float64
		last    float64
		want    soakWorkingSetGrowth
		wantErr bool
	}{
		{
			name:  "eight MiB branch exact boundary",
			first: 64,
			last:  72,
			want: soakWorkingSetGrowth{
				FirstMinuteMedianMB: 64, LastMinuteMedianMB: 72,
				GrowthMB: 8, AllowedGrowthMB: 8,
			},
		},
		{name: "eight MiB branch exceeded", first: 64, last: math.Nextafter(72, math.Inf(1)), wantErr: true},
		{
			name:  "ten percent branch exact boundary",
			first: 100,
			last:  110,
			want: soakWorkingSetGrowth{
				FirstMinuteMedianMB: 100, LastMinuteMedianMB: 110,
				GrowthMB: 10, AllowedGrowthMB: 10,
			},
		},
		{name: "ten percent branch exceeded", first: 100, last: 110.000001, wantErr: true},
		{
			name:  "negative growth",
			first: 100,
			last:  90,
			want: soakWorkingSetGrowth{
				FirstMinuteMedianMB: 100, LastMinuteMedianMB: 90,
				GrowthMB: -10, AllowedGrowthMB: 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newSoakWorkingSetGrowth(tt.first, tt.last)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNewSoakWorkingSetGrowthRejectsNonFiniteValues(t *testing.T) {
	tests := []struct {
		name  string
		first float64
		last  float64
	}{
		{name: "first NaN", first: math.NaN(), last: 1},
		{name: "first positive infinity", first: math.Inf(1), last: 1},
		{name: "first negative infinity", first: math.Inf(-1), last: 1},
		{name: "last NaN", first: 1, last: math.NaN()},
		{name: "last positive infinity", first: 1, last: math.Inf(1)},
		{name: "last negative infinity", first: 1, last: math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newSoakWorkingSetGrowth(tt.first, tt.last)
			require.ErrorContains(t, err, "non-finite")
		})
	}
}

func TestReleaseSoakWorkingSetUsesRawLoadSamples(t *testing.T) {
	start := time.Date(2026, time.August, 18, 10, 0, 0, 0, time.UTC)
	result := loadResult{ResourceSamples: ResourceSamplesV2{Resources: []resourceSample{
		{At: start, WorkingSetBytes: 64 << 20},
		{At: start.Add(9*time.Minute + 30*time.Second), WorkingSetBytes: 72 << 20},
	}}}

	growth, err := summarizeSoakWorkingSet(result.ResourceSamples.Resources, start, start.Add(10*time.Minute))

	require.NoError(t, err)
	require.Equal(t, soakWorkingSetGrowth{
		FirstMinuteMedianMB: 64, LastMinuteMedianMB: 72, GrowthMB: 8, AllowedGrowthMB: 8,
	}, growth)
}

func TestParsePostDrainSnapshot(t *testing.T) {
	body := []byte("sip_exporter_channel_length 0\n" +
		"sip_exporter_active_dialogs 0\n" +
		"sip_exporter_active_trackers{type=\"invite\"} 0\n" +
		"sip_exporter_active_trackers{type=\"register\"} 0\n")

	got, err := parsePostDrainSnapshot(body)

	require.NoError(t, err)
	require.Equal(t, postDrainSnapshot{}, got)
}

func TestPostDrainSnapshotRejectsEachNonZeroLifecycleGauge(t *testing.T) {
	tests := []struct {
		name     string
		snapshot postDrainSnapshot
		wantErr  string
	}{
		{name: "channel length", snapshot: postDrainSnapshot{ChannelLength: 1}, wantErr: "channel_length"},
		{name: "active dialogs", snapshot: postDrainSnapshot{ActiveDialogs: 1}, wantErr: "active_dialogs"},
		{name: "active trackers", snapshot: postDrainSnapshot{ActiveTrackers: 1}, wantErr: "active_trackers"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorContains(t, tt.snapshot.Validate(), tt.wantErr)
		})
	}
}

func TestParsePostDrainSnapshotRejectsMissingAndNonFiniteGauges(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "missing channel", body: "sip_exporter_active_dialogs 0\n" +
			"sip_exporter_active_trackers{type=\"invite\"} 0\n", wantErr: "channel_length"},
		{name: "missing dialogs", body: "sip_exporter_channel_length 0\n" +
			"sip_exporter_active_trackers{type=\"invite\"} 0\n", wantErr: "active_dialogs"},
		{name: "missing trackers", body: "sip_exporter_channel_length 0\n" +
			"sip_exporter_active_dialogs 0\n", wantErr: "active_trackers"},
		{name: "NaN channel", body: "sip_exporter_channel_length NaN\n" +
			"sip_exporter_active_dialogs 0\n" +
			"sip_exporter_active_trackers{type=\"invite\"} 0\n", wantErr: "non-finite"},
		{name: "infinite dialogs", body: "sip_exporter_channel_length 0\n" +
			"sip_exporter_active_dialogs +Inf\n" +
			"sip_exporter_active_trackers{type=\"invite\"} 0\n", wantErr: "non-finite"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePostDrainSnapshot([]byte(tt.body))
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestWaitForPostDrainSnapshotRequiresConsecutiveZeroScrapes(t *testing.T) {
	zero := postDrainMetrics(0, 0, 0, "zero")
	stable := postDrainMetrics(0, 0, 0, "stable")
	tests := []struct {
		name      string
		responses []postDrainHTTPResponse
		requests  int32
	}{
		{
			name: "nonzero then two zeros",
			responses: []postDrainHTTPResponse{
				{body: postDrainMetrics(1, 0, 0, "busy")}, {body: zero}, {body: stable},
			},
			requests: 3,
		},
		{
			name: "nonzero resets zero streak",
			responses: []postDrainHTTPResponse{
				{body: zero}, {body: postDrainMetrics(0, 1, 0, "busy")}, {body: zero}, {body: stable},
			},
			requests: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, requests := newPostDrainSequenceServer(t, tt.responses)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()

			snapshot, body, err := waitForPostDrainSnapshot(ctx, server.URL)

			require.NoError(t, err)
			require.Equal(t, postDrainSnapshot{}, snapshot)
			require.Contains(t, string(body), "# stable")
			require.Equal(t, tt.requests, requests.Load())
		})
	}
}

func TestWaitForPostDrainSnapshotRetriesTransportErrors(t *testing.T) {
	server, requests := newPostDrainSequenceServer(t, []postDrainHTTPResponse{
		{body: postDrainMetrics(0, 0, 0, "zero-before-error")},
		{status: http.StatusServiceUnavailable, body: "unavailable"},
		{body: postDrainMetrics(0, 0, 0, "zero")},
		{body: postDrainMetrics(0, 0, 0, "stable")},
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	_, body, err := waitForPostDrainSnapshot(ctx, server.URL)

	require.NoError(t, err)
	require.Contains(t, string(body), "# stable")
	require.Equal(t, int32(4), requests.Load())
}

func TestWaitForPostDrainSnapshotFailsClosedOnMalformedSnapshot(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "missing gauge",
			body:    "sip_exporter_channel_length 0\nsip_exporter_active_dialogs 0\n",
			wantErr: "active_trackers",
		},
		{
			name:    "non-finite gauge",
			body:    postDrainMetrics(math.NaN(), 0, 0, "invalid"),
			wantErr: "finite",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, requests := newPostDrainSequenceServer(t, []postDrainHTTPResponse{{body: tt.body}})

			_, _, err := waitForPostDrainSnapshot(t.Context(), server.URL)

			require.ErrorContains(t, err, tt.wantErr)
			require.Equal(t, int32(1), requests.Load())
		})
	}
}

func TestWaitForPostDrainSnapshotTimesOutOnEachNonZeroGauge(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "channel length", body: postDrainMetrics(1, 0, 0, "busy"), wantErr: "channel_length"},
		{name: "active dialogs", body: postDrainMetrics(0, 1, 0, "busy"), wantErr: "active_dialogs"},
		{name: "active trackers", body: postDrainMetrics(0, 0, 1, "busy"), wantErr: "active_trackers"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _ := newPostDrainSequenceServer(t, []postDrainHTTPResponse{{body: tt.body}})
			ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
			defer cancel()

			_, _, err := waitForPostDrainSnapshot(ctx, server.URL)

			require.ErrorContains(t, err, tt.wantErr)
			require.ErrorIs(t, err, context.DeadlineExceeded)
		})
	}
}

func TestPostDrainWaitContract(t *testing.T) {
	require.Equal(t, 2, postDrainStableScrapes)
	require.Equal(t, 100*time.Millisecond, postDrainPollInterval)
	require.LessOrEqual(t, postDrainWaitLimit, 10*time.Second)
}
