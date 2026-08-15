//go:build e2e

package load

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

type logsOnlyContainer struct {
	testcontainers.Container
	logs string
}

func (c logsOnlyContainer) Logs(context.Context) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(c.logs)), nil
}

func TestSteadyMeasurementBeginHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	measurement := &steadyMeasurement{env: &testEnv{endpoint: "http://127.0.0.1:1"}}

	err := measurement.Begin(ctx, time.Now())

	require.ErrorIs(t, err, context.Canceled)
}

func TestSteadyMeasurementWaitForSamplesRequiresBothCollectors(t *testing.T) {
	tests := []struct {
		name    string
		samples ResourceSamplesV2
		wantErr bool
	}{
		{
			name: "both collectors ready",
			samples: ResourceSamplesV2{
				Resources: []resourceSample{{CPUPeriods: 1}, {CPUPeriods: 2}},
				Metrics:   []metricSamplePoint{{}, {}},
			},
		},
		{
			name: "resource counters have no usable delta",
			samples: ResourceSamplesV2{
				Resources: []resourceSample{{CPUPeriods: 1}, {CPUPeriods: 1}},
				Metrics:   []metricSamplePoint{{}, {}},
			},
			wantErr: true,
		},
		{
			name: "resource collector incomplete",
			samples: ResourceSamplesV2{
				Resources: []resourceSample{{}},
				Metrics:   []metricSamplePoint{{}, {}},
			},
			wantErr: true,
		},
		{
			name: "metric collector incomplete",
			samples: ResourceSamplesV2{
				Resources: []resourceSample{{}, {}},
				Metrics:   []metricSamplePoint{{}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			measurement := &steadyMeasurement{samples: tt.samples}
			ctx := t.Context()
			if tt.wantErr {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}

			err := measurement.WaitForSamples(ctx, 2, 2)
			if tt.wantErr {
				require.ErrorIs(t, err, context.Canceled)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestSteadyMeasurementIgnoresCancellationAfterPhaseClose(t *testing.T) {
	measurement := &steadyMeasurement{start: time.Now()}

	measurement.setMeasurementError(context.Canceled)

	require.NoError(t, measurement.err)
}

func TestSteadyMeasurementRetainsCollectorFailureBeforePhaseStart(t *testing.T) {
	measurement := &steadyMeasurement{}

	measurement.setMeasurementError(context.DeadlineExceeded)

	require.ErrorIs(t, measurement.err, context.DeadlineExceeded)
}

func TestSteadyMeasurementEndSamplesMetricBoundary(t *testing.T) {
	var measurement *steadyMeasurement
	boundaryWhileMeasuring := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		measurement.mu.Lock()
		boundaryWhileMeasuring = measurement.measuring
		measurement.mu.Unlock()
		_, err := io.WriteString(w, "sip_exporter_channel_length 7\n"+
			"sip_exporter_socket_packets_dropped_total{interface=\"lo\"} 1\n"+
			"sip_exporter_rtp_dropped_total 0\n")
		require.NoError(t, err)
	}))
	defer server.Close()

	start := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Second)
	done := make(chan struct{})
	close(done)
	measurement = &steadyMeasurement{
		env: &testEnv{
			endpoint: server.URL, exporterContainer: logsOnlyContainer{}, limits: nominalLimits,
		},
		cancel: func() {}, done: done, measuring: true, start: start,
		samples: ResourceSamplesV2{
			Resources: []resourceSample{
				{At: start, CPUPeriods: 1},
				{At: start.Add(time.Second), CPUPeriods: 2},
			},
			Metrics: []metricSamplePoint{
				{At: start},
				{At: start.Add(time.Second)},
			},
		},
	}

	summary, samples, err := measurement.End(t.Context(), end)

	require.NoError(t, err)
	require.True(t, boundaryWhileMeasuring)
	require.Equal(t, float64(1), summary.SocketDrops)
	require.Equal(t, float64(7), summary.ChannelPeak)
	require.Equal(t, metricSamplePoint{
		At: end.Add(-time.Nanosecond), ChannelLength: 7, SocketDrops: 1,
	}, samples.Metrics[len(samples.Metrics)-1])
}

func TestSteadyMeasurementEndRejectsInflightPeriodicSample(t *testing.T) {
	periodicStarted := make(chan struct{})
	releasePeriodic := make(chan struct{})
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requestCount.Add(1) == 1 {
			close(periodicStarted)
			<-releasePeriodic
			_, err := io.WriteString(w, "sip_exporter_channel_length 99\n"+
				"sip_exporter_socket_packets_dropped_total{interface=\"lo\"} 99\n"+
				"sip_exporter_rtp_dropped_total 0\n")
			require.NoError(t, err)
			return
		}
		_, err := io.WriteString(w, "sip_exporter_channel_length 7\n"+
			"sip_exporter_socket_packets_dropped_total{interface=\"lo\"} 1\n"+
			"sip_exporter_rtp_dropped_total 0\n")
		require.NoError(t, err)
	}))
	defer server.Close()

	start := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Second)
	done := make(chan struct{})
	close(done)
	measurement := &steadyMeasurement{
		env: &testEnv{
			endpoint: server.URL, exporterContainer: logsOnlyContainer{}, limits: nominalLimits,
		},
		cancel: func() {}, done: done, measuring: true, start: start,
		samples: ResourceSamplesV2{
			Resources: []resourceSample{
				{At: start, CPUPeriods: 1},
				{At: start.Add(time.Second), CPUPeriods: 2},
			},
			Metrics: []metricSamplePoint{{At: start}, {At: start.Add(time.Second)}},
		},
	}
	periodicErr := make(chan error, 1)
	go func() {
		periodicErr <- measurement.sampleMetricPoint(t.Context(), start.Add(2*time.Second))
	}()
	<-periodicStarted

	summary, _, endErr := measurement.End(t.Context(), end)
	close(releasePeriodic)
	fetchErr := <-periodicErr

	require.NoError(t, endErr)
	require.NoError(t, fetchErr)
	require.Equal(t, float64(1), summary.SocketDrops)
	measurement.mu.Lock()
	defer measurement.mu.Unlock()
	require.Len(t, measurement.samples.Metrics, 3)
	require.Equal(t, end.Add(-time.Nanosecond), measurement.samples.Metrics[2].At)
}

func TestPhaseSamplesSelectsHalfOpenMeasurementInterval(t *testing.T) {
	start := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Second)
	samples := []resourceSample{
		{At: start.Add(-time.Nanosecond), WorkingSetBytes: 1},
		{At: start, WorkingSetBytes: 2},
		{At: start.Add(time.Second), WorkingSetBytes: 3},
		{At: end, WorkingSetBytes: 4},
		{At: end.Add(time.Nanosecond), WorkingSetBytes: 5},
	}
	original := slices.Clone(samples)

	got, err := phaseSamples(samples, func(sample resourceSample) time.Time { return sample.At }, start, end)

	require.NoError(t, err)
	require.Equal(t, []resourceSample{samples[1], samples[2]}, got)
	require.Equal(t, original, samples, "phase selection must not mutate caller samples")
}

func TestPhaseSamplesRejectsInvalidInterval(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		start time.Time
		end   time.Time
	}{
		{name: "missing start", end: now},
		{name: "missing end", start: now},
		{name: "end before start", start: now, end: now.Add(-time.Nanosecond)},
		{name: "empty interval", start: now, end: now},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := phaseSamples([]resourceSample{{At: now}},
				func(sample resourceSample) time.Time { return sample.At }, tt.start, tt.end)
			require.Error(t, err)
		})
	}
}

func TestWorkingSetBytesUsesCgroupCache(t *testing.T) {
	tests := []struct {
		name  string
		usage uint64
		stats map[string]uint64
		want  uint64
	}{
		{name: "cgroup v2", usage: 100, stats: map[string]uint64{"inactive_file": 30}, want: 70},
		{name: "cgroup v1", usage: 100, stats: map[string]uint64{"total_inactive_file": 40}, want: 60},
		{name: "zero cache", usage: 100, stats: map[string]uint64{"inactive_file": 0}, want: 100},
		{name: "all cache", usage: 100, stats: map[string]uint64{"inactive_file": 100}, want: 0},
		{name: "v2 precedence", usage: 100, stats: map[string]uint64{
			"inactive_file": 20, "total_inactive_file": 90,
		}, want: 80},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := workingSetBytes(tt.usage, tt.stats)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestWorkingSetBytesRejectsUntrustworthyCache(t *testing.T) {
	tests := []struct {
		name  string
		stats map[string]uint64
	}{
		{name: "missing cache", stats: map[string]uint64{}},
		{name: "nil stats", stats: nil},
		{name: "cache exceeds usage", stats: map[string]uint64{"inactive_file": 101}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := workingSetBytes(100, tt.stats)
			require.Error(t, err)
		})
	}
}

func TestPercentileInterpolatesOwnedSortedCopy(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		p      float64
		want   float64
	}{
		{name: "singleton p99", values: []float64{7}, p: 99, want: 7},
		{name: "odd p50", values: []float64{5, 1, 3}, p: 50, want: 3},
		{name: "even p50", values: []float64{4, 1, 3, 2}, p: 50, want: 2.5},
		{name: "p95", values: []float64{1, 2, 3, 4, 5}, p: 95, want: 4.8},
		{name: "p99", values: []float64{1, 2, 3, 4, 5}, p: 99, want: 4.96},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := slices.Clone(tt.values)
			got, err := percentile(tt.values, tt.p)
			require.NoError(t, err)
			require.InDelta(t, tt.want, got, 0.000001)
			require.Equal(t, original, tt.values, "percentile must not reorder caller samples")
		})
	}
}

func TestPercentileRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		p      float64
	}{
		{name: "empty", values: nil, p: 50},
		{name: "negative percentile", values: []float64{1}, p: -1},
		{name: "percentile above one hundred", values: []float64{1}, p: 101},
		{name: "non finite sample", values: []float64{math.Inf(1)}, p: 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := percentile(tt.values, tt.p)
			require.Error(t, err)
		})
	}
}

func TestResourceSampleFromStatsNormalizesCPUToQuota(t *testing.T) {
	at := time.Date(2026, 8, 14, 12, 0, 1, 0, time.UTC)
	stats := validDockerStats(at)

	got, err := resourceSampleFromStats(at, stats, WorkloadLimits{CPUCores: 2, MemoryBytes: 256 << 20})

	require.NoError(t, err)
	require.InDelta(t, 50, got.CPUQuotaPercent, 0.000001)
	require.Equal(t, uint64(80<<20), got.WorkingSetBytes)
	require.Equal(t, uint64(100), got.CPUPeriods)
	require.Equal(t, uint64(2), got.CPUThrottledPeriods)
}

func TestResourceSampleFromStatsRejectsInvalidInputs(t *testing.T) {
	at := time.Date(2026, 8, 14, 12, 0, 1, 0, time.UTC)
	tests := []struct {
		name   string
		limits WorkloadLimits
		mutate func(*container.StatsResponse)
	}{
		{name: "zero CPU quota", limits: WorkloadLimits{MemoryBytes: 256 << 20}},
		{name: "zero memory limit", limits: WorkloadLimits{CPUCores: 2}},
		{name: "zero system delta", limits: WorkloadLimits{CPUCores: 2, MemoryBytes: 256 << 20}, mutate: func(s *container.StatsResponse) {
			s.PreCPUStats.SystemUsage = s.CPUStats.SystemUsage
		}},
		{name: "CPU counter rollback", limits: WorkloadLimits{CPUCores: 2, MemoryBytes: 256 << 20}, mutate: func(s *container.StatsResponse) {
			s.PreCPUStats.CPUUsage.TotalUsage = s.CPUStats.CPUUsage.TotalUsage + 1
		}},
		{name: "missing cache", limits: WorkloadLimits{CPUCores: 2, MemoryBytes: 256 << 20}, mutate: func(s *container.StatsResponse) {
			s.MemoryStats.Stats = nil
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := validDockerStats(at)
			if tt.mutate != nil {
				tt.mutate(&stats)
			}
			_, err := resourceSampleFromStats(at, stats, tt.limits)
			require.Error(t, err)
		})
	}
}

func TestThrottlingPercentUsesCounterDeltas(t *testing.T) {
	first := resourceSample{CPUPeriods: 100, CPUThrottledPeriods: 5}
	last := resourceSample{CPUPeriods: 300, CPUThrottledPeriods: 7}

	got, err := throttlingPercent(first, last)

	require.NoError(t, err)
	require.InDelta(t, 1, got, 0.000001)
}

func TestThrottlingPercentRejectsInvalidCounters(t *testing.T) {
	tests := []struct {
		name  string
		first resourceSample
		last  resourceSample
	}{
		{name: "zero period delta", first: resourceSample{CPUPeriods: 10}, last: resourceSample{CPUPeriods: 10}},
		{name: "period rollback", first: resourceSample{CPUPeriods: 10}, last: resourceSample{CPUPeriods: 9}},
		{name: "throttled rollback", first: resourceSample{CPUPeriods: 10, CPUThrottledPeriods: 2}, last: resourceSample{CPUPeriods: 20, CPUThrottledPeriods: 1}},
		{name: "throttled exceeds periods", first: resourceSample{}, last: resourceSample{CPUPeriods: 10, CPUThrottledPeriods: 11}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := throttlingPercent(tt.first, tt.last)
			require.Error(t, err)
		})
	}
}

func validDockerStats(at time.Time) container.StatsResponse {
	return container.StatsResponse{
		Read: at,
		CPUStats: container.CPUStats{
			CPUUsage:       container.CPUUsage{TotalUsage: 3_000},
			SystemUsage:    14_000,
			OnlineCPUs:     4,
			ThrottlingData: container.ThrottlingData{Periods: 100, ThrottledPeriods: 2},
		},
		PreCPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 2_000},
			SystemUsage: 10_000,
		},
		MemoryStats: container.MemoryStats{
			Usage: 100 << 20,
			Stats: map[string]uint64{"inactive_file": 20 << 20},
		},
	}
}

func TestSummarizeResourcesUsesOnlyMeasurementPhase(t *testing.T) {
	start := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Second)
	limits := WorkloadLimits{CPUCores: 2, MemoryBytes: 256 << 20}
	samples := ResourceSamplesV2{
		Resources: []resourceSample{
			{At: start.Add(-time.Nanosecond), CPUQuotaPercent: 99, WorkingSetBytes: 200 << 20},
			{At: start, CPUQuotaPercent: 20, WorkingSetBytes: 50 << 20, CPUPeriods: 100, CPUThrottledPeriods: 1},
			{At: start.Add(2 * time.Second), CPUQuotaPercent: 40, WorkingSetBytes: 100 << 20, CPUPeriods: 200, CPUThrottledPeriods: 2},
			{At: end, CPUQuotaPercent: 100, WorkingSetBytes: 250 << 20, CPUPeriods: 300, CPUThrottledPeriods: 50},
		},
		Metrics: []metricSamplePoint{
			{At: start.Add(-time.Nanosecond), ChannelLength: 90, SocketDrops: 8, RTPDrops: 9},
			{At: start, ChannelLength: 1, SocketDrops: 10, RTPDrops: 20},
			{At: start.Add(time.Second), ChannelLength: 3, SocketDrops: 10, RTPDrops: 20},
			{At: end, ChannelLength: 100, SocketDrops: 30, RTPDrops: 40},
		},
		GCPauses: []gcPauseSample{
			{At: start.Add(-time.Nanosecond), DurationMS: 99},
			{At: start, DurationMS: 1},
			{At: start.Add(time.Second), DurationMS: 5},
			{At: end, DurationMS: 100},
		},
	}

	got, err := summarizeResources(samples, start, end, limits)

	require.NoError(t, err)
	require.Equal(t, limits, got.Limits)
	require.InDelta(t, 40, got.CPUP95Percent, 0.000001)
	require.InDelta(t, 99.5, got.WorkingSetP99MB, 0.000001)
	require.InDelta(t, 1, got.ThrottlingPercent, 0.000001)
	require.Equal(t, float64(3), got.ChannelPeak)
	require.Zero(t, got.SocketDrops)
	require.Zero(t, got.RTPDrops)
	require.Equal(t, float64(5), got.GCMaxSTWMS)
}

func TestSummarizeResourcesExcludesSeedCPUInterval(t *testing.T) {
	start := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	samples := ResourceSamplesV2{
		Resources: []resourceSample{
			{At: start, CPUQuotaPercent: 95, WorkingSetBytes: 10 << 20, CPUPeriods: 10},
			{At: start.Add(time.Second), CPUQuotaPercent: 10, WorkingSetBytes: 20 << 20, CPUPeriods: 20},
			{At: start.Add(2 * time.Second), CPUQuotaPercent: 20, WorkingSetBytes: 30 << 20, CPUPeriods: 30},
		},
		Metrics: []metricSamplePoint{{At: start}, {At: start.Add(time.Second)}},
	}

	got, err := summarizeResources(samples, start, start.Add(3*time.Second), nominalLimits)

	require.NoError(t, err)
	require.InDelta(t, 19.5, got.CPUP95Percent, 0.000001)
	require.InDelta(t, 29.8, got.WorkingSetP99MB, 0.000001)
}

func TestSummarizeResourcesFailsClosedOnMissingEvidence(t *testing.T) {
	start := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Second)
	valid := ResourceSamplesV2{
		Resources: []resourceSample{
			{At: start, CPUPeriods: 1},
			{At: start.Add(time.Nanosecond), CPUPeriods: 2},
		},
		Metrics: []metricSamplePoint{{At: start}, {At: start.Add(time.Nanosecond)}},
	}
	tests := []struct {
		name   string
		mutate func(*ResourceSamplesV2)
	}{
		{name: "missing resource samples", mutate: func(samples *ResourceSamplesV2) { samples.Resources = nil }},
		{name: "single resource sample", mutate: func(samples *ResourceSamplesV2) { samples.Resources = samples.Resources[:1] }},
		{name: "missing metric samples", mutate: func(samples *ResourceSamplesV2) { samples.Metrics = nil }},
		{name: "single metric sample", mutate: func(samples *ResourceSamplesV2) { samples.Metrics = samples.Metrics[:1] }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			samples := valid
			tt.mutate(&samples)
			_, err := summarizeResources(samples, start, end, peakLimits)
			require.Error(t, err)
		})
	}
}

func TestSummarizeResourcesTreatsNoGCAsZeroPause(t *testing.T) {
	start := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	samples := ResourceSamplesV2{
		Resources: []resourceSample{{At: start, CPUPeriods: 1}, {At: start.Add(time.Nanosecond), CPUPeriods: 2}},
		Metrics:   []metricSamplePoint{{At: start}, {At: start.Add(time.Nanosecond)}},
	}

	got, err := summarizeResources(samples, start, start.Add(time.Second), peakLimits)

	require.NoError(t, err)
	require.Zero(t, got.GCMaxSTWMS)
}

func TestValidateAbsoluteResourceGatesUsesExactBoundaries(t *testing.T) {
	base := ResourceSummaryV2{Limits: peakLimits}
	tests := []struct {
		name    string
		mutate  func(*ResourceSummaryV2)
		wantErr bool
	}{
		{name: "all below"},
		{name: "CPU equal", mutate: func(summary *ResourceSummaryV2) { summary.CPUP95Percent = 80 }},
		{name: "CPU above", mutate: func(summary *ResourceSummaryV2) { summary.CPUP95Percent = 80.000001 }, wantErr: true},
		{name: "memory equal", mutate: func(summary *ResourceSummaryV2) { summary.WorkingSetP99MB = 204.8 }},
		{name: "memory above", mutate: func(summary *ResourceSummaryV2) { summary.WorkingSetP99MB = 204.800001 }, wantErr: true},
		{name: "throttling equal", mutate: func(summary *ResourceSummaryV2) { summary.ThrottlingPercent = 1 }},
		{name: "throttling above", mutate: func(summary *ResourceSummaryV2) { summary.ThrottlingPercent = 1.000001 }, wantErr: true},
		{name: "GC below", mutate: func(summary *ResourceSummaryV2) { summary.GCMaxSTWMS = 49.999999 }},
		{name: "GC equal", mutate: func(summary *ResourceSummaryV2) { summary.GCMaxSTWMS = 50 }, wantErr: true},
		{name: "socket drop", mutate: func(summary *ResourceSummaryV2) { summary.SocketDrops = 1 }, wantErr: true},
		{name: "RTP drop", mutate: func(summary *ResourceSummaryV2) { summary.RTPDrops = 1 }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := base
			if tt.mutate != nil {
				tt.mutate(&summary)
			}
			err := validateAbsoluteResourceGates(summary)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateAbsoluteResourceGatesRejectsInvalidNumericDomains(t *testing.T) {
	fields := []struct {
		name string
		set  func(*ResourceSummaryV2, float64)
	}{
		{name: "CPU", set: func(summary *ResourceSummaryV2, value float64) { summary.CPUP95Percent = value }},
		{name: "memory", set: func(summary *ResourceSummaryV2, value float64) { summary.WorkingSetP99MB = value }},
		{name: "throttling", set: func(summary *ResourceSummaryV2, value float64) { summary.ThrottlingPercent = value }},
		{name: "channel", set: func(summary *ResourceSummaryV2, value float64) { summary.ChannelPeak = value }},
		{name: "socket drops", set: func(summary *ResourceSummaryV2, value float64) { summary.SocketDrops = value }},
		{name: "RTP drops", set: func(summary *ResourceSummaryV2, value float64) { summary.RTPDrops = value }},
		{name: "GC", set: func(summary *ResourceSummaryV2, value float64) { summary.GCMaxSTWMS = value }},
	}
	invalid := []struct {
		name  string
		value float64
	}{
		{name: "NaN", value: math.NaN()},
		{name: "positive infinity", value: math.Inf(1)},
		{name: "negative infinity", value: math.Inf(-1)},
		{name: "negative", value: -0.000001},
	}

	require.NoError(t, validateAbsoluteResourceGates(ResourceSummaryV2{Limits: peakLimits}))
	for _, field := range fields {
		for _, value := range invalid {
			t.Run(field.name+"/"+value.name, func(t *testing.T) {
				summary := ResourceSummaryV2{Limits: peakLimits}
				field.set(&summary, value.value)
				require.Error(t, validateAbsoluteResourceGates(summary))
			})
		}
	}
}

func TestRequiredMetricSumRejectsDuplicateSeriesIdentity(t *testing.T) {
	duplicate := []byte("sip_exporter_rtp_dropped_total 1\n" +
		"sip_exporter_rtp_dropped_total 2\n")

	_, err := requiredMetricSum(duplicate, "sip_exporter_rtp_dropped_total")

	require.Error(t, err)
}

func TestRequiredMetricSumAddsDistinctSeries(t *testing.T) {
	distinct := []byte("sip_exporter_rtp_dropped_total{interface=\"lo\"} 1\n" +
		"sip_exporter_rtp_dropped_total{interface=\"eth0\"} 2\n")

	got, err := requiredMetricSum(distinct, "sip_exporter_rtp_dropped_total")

	require.NoError(t, err)
	require.Equal(t, 3.0, got)
}

func TestParseGCPauseSamplesMapsProcessUptimeToWallTime(t *testing.T) {
	containerStart := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	logs := "gc 1 @1.000s 1%: 0.10+1.0+0.20 ms clock\n" +
		"gc 2 @2.500s 1%: 0.30+1.0+0.40 ms clock\n"

	got, err := parseGCPauseSamples(
		logs, containerStart, containerStart, containerStart.Add(10*time.Second),
	)

	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, containerStart.Add(time.Second), got[0].At)
	require.InDelta(t, 0.3, got[0].DurationMS, 0.000001)
	require.Equal(t, containerStart.Add(2500*time.Millisecond), got[1].At)
	require.InDelta(t, 0.7, got[1].DurationMS, 0.000001)
}

func TestParseGCPauseSamplesFailsClosedWithinMeasurementPhase(t *testing.T) {
	containerStart := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	validGC := "gc 1 @1.000s 1%: 0.10+1.0+0.20 ms clock"
	tests := []struct {
		name    string
		logs    string
		wantLen int
		wantErr bool
	}{
		{name: "no GC lines", logs: "exporter ready\n"},
		{name: "malformed only", logs: "gc 3 @1.2s 0%: malformed\n", wantErr: true},
		{name: "valid plus malformed", logs: validGC + "\ngc 4 @1.4s 0%: malformed\n", wantErr: true},
		{name: "malformed outside phase", logs: "gc 3 @20s 0%: malformed\n"},
		{name: "unparseable timestamp", logs: "gc 3 @badsecs 0%: malformed\n", wantErr: true},
		{name: "valid inside phase", logs: validGC + "\n", wantLen: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGCPauseSamples(
				tt.logs, containerStart, containerStart, containerStart.Add(10*time.Second),
			)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, tt.wantLen)
		})
	}
}

func TestValidatePostPhaseOrdering(t *testing.T) {
	measureEnd := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		postPhase []time.Time
		wantErr   bool
	}{
		{
			name: "all strictly after",
			postPhase: []time.Time{
				measureEnd.Add(time.Nanosecond),
				measureEnd.Add(2 * time.Nanosecond),
				measureEnd.Add(3 * time.Nanosecond),
			},
		},
		{name: "evidence equal", postPhase: []time.Time{measureEnd}, wantErr: true},
		{name: "evidence before", postPhase: []time.Time{measureEnd.Add(-time.Nanosecond)}, wantErr: true},
		{
			name: "UAS exit before",
			postPhase: []time.Time{
				measureEnd.Add(time.Nanosecond),
				measureEnd.Add(-time.Nanosecond),
			},
			wantErr: true,
		},
		{
			name: "drain before",
			postPhase: []time.Time{
				measureEnd.Add(time.Nanosecond),
				measureEnd.Add(2 * time.Nanosecond),
				measureEnd.Add(-time.Nanosecond),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePostPhaseOrdering(measureEnd, tt.postPhase...)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestStartBarrierBlocksWorkersUntilSharedRelease(t *testing.T) {
	barrier := newStartBarrier()
	ready := make(chan struct{}, 2)
	observed := make(chan time.Time, 2)
	for range 2 {
		go func() {
			ready <- struct{}{}
			observed <- barrier.wait()
		}()
	}
	<-ready
	<-ready
	require.Never(t, func() bool { return len(observed) != 0 }, 25*time.Millisecond, time.Millisecond)

	release := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	barrier.release(release)
	require.Equal(t, release, <-observed)
	require.Equal(t, release, <-observed)
}

func TestMetricSamplePointFromBodyRequiresCompleteSelfMetrics(t *testing.T) {
	at := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	body := []byte("sip_exporter_channel_length 4\n" +
		"sip_exporter_socket_packets_dropped_total{interface=\"lo\"} 1\n" +
		"sip_exporter_socket_packets_dropped_total{interface=\"eth0\"} 2\n" +
		"sip_exporter_rtp_dropped_total 5\n")

	got, err := metricSamplePointFromBody(at, body)

	require.NoError(t, err)
	require.Equal(t, metricSamplePoint{
		At: at, ChannelLength: 4, SocketDrops: 3, RTPDrops: 5,
	}, got)
}

func TestMetricSamplePointFromBodyFailsClosed(t *testing.T) {
	validLines := []string{
		"sip_exporter_channel_length 4",
		"sip_exporter_socket_packets_dropped_total{interface=\"lo\"} 1",
		"sip_exporter_rtp_dropped_total 5",
	}

	for missing := range validLines {
		t.Run(validLines[missing], func(t *testing.T) {
			lines := slices.Clone(validLines)
			lines = slices.Delete(lines, missing, missing+1)
			_, err := metricSamplePointFromBody(time.Now(), []byte(strings.Join(lines, "\n")+"\n"))
			require.Error(t, err)
		})
	}

	t.Run("duplicate channel gauge", func(t *testing.T) {
		body := strings.Join(append(validLines, "sip_exporter_channel_length 5"), "\n") + "\n"
		_, err := metricSamplePointFromBody(time.Now(), []byte(body))
		require.Error(t, err)
	})
}

func TestResourceMetricEntriesReturnsCanonicalOwnedInventory(t *testing.T) {
	summary := ResourceSummaryV2{
		CPUP95Percent: 40, WorkingSetP99MB: 64, ThrottlingPercent: 0.5,
		ChannelPeak: 3, SocketDrops: 0, RTPDrops: 0, GCMaxSTWMS: 2,
	}

	got := resourceMetricEntries(summary)

	require.Equal(t, map[string]MetricEntry{
		"cpu_p95_percent":    {Value: 40, Unit: "%", Direction: dirLowerIsBetter},
		"working_set_p99_mb": {Value: 64, Unit: "MiB", Direction: dirLowerIsBetter},
		"throttling_percent": {Value: 0.5, Unit: "%", Direction: dirLowerIsBetter},
		"channel_peak":       {Value: 3, Unit: "count", Direction: dirLowerIsBetter},
		"socket_drops":       {Value: 0, Unit: "count", Direction: dirLowerIsBetter},
		"rtp_drops":          {Value: 0, Unit: "count", Direction: dirLowerIsBetter},
		"gc_max_stw_ms":      {Value: 2, Unit: "ms", Direction: dirLowerIsBetter},
	}, got)
	got["cpu_p95_percent"] = MetricEntry{Value: 99}
	require.Equal(t, float64(40), summary.CPUP95Percent)
}
