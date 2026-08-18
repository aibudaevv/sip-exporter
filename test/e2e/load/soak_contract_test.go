//go:build e2e

package load

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
