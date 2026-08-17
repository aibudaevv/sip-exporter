//go:build e2e

package load

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/moby/moby/api/types/container"
)

type (
	WorkloadLimits struct {
		CPUCores    float64 `json:"cpu_cores"`
		MemoryBytes int64   `json:"memory_bytes"`
	}

	resourceSample struct {
		At                  time.Time
		CPUQuotaPercent     float64
		WorkingSetBytes     uint64
		CPUPeriods          uint64
		CPUThrottledPeriods uint64
	}

	metricSamplePoint struct {
		At            time.Time
		ChannelLength float64
		SocketDrops   float64
		RTPDrops      float64
	}

	gcPauseSample struct {
		At         time.Time
		DurationMS float64
	}
)

var (
	nominalLimits          = WorkloadLimits{CPUCores: 1, MemoryBytes: 128 << 20}
	peakLimits             = WorkloadLimits{CPUCores: 2, MemoryBytes: 256 << 20}
	diagnosticMemoryLimits = WorkloadLimits{CPUCores: 2, MemoryBytes: 1 << 30}
)

func (l WorkloadLimits) validate() error {
	if l.CPUCores <= 0 || math.IsNaN(l.CPUCores) || math.IsInf(l.CPUCores, 0) {
		return fmt.Errorf("invalid CPU limit %v", l.CPUCores)
	}
	if l.MemoryBytes <= 0 {
		return fmt.Errorf("invalid memory limit %d", l.MemoryBytes)
	}
	return nil
}

func phaseSamples[T any](
	samples []T,
	at func(T) time.Time,
	start, end time.Time,
) ([]T, error) {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return nil, fmt.Errorf("invalid measurement interval")
	}
	selected := make([]T, 0, len(samples))
	for _, sample := range samples {
		timestamp := at(sample)
		if !timestamp.Before(start) && timestamp.Before(end) {
			selected = append(selected, sample)
		}
	}
	return selected, nil
}

func workingSetBytes(usage uint64, stats map[string]uint64) (uint64, error) {
	cache, ok := stats["inactive_file"]
	if !ok {
		cache, ok = stats["total_inactive_file"]
	}
	if !ok {
		return 0, fmt.Errorf("missing inactive file cache")
	}
	if cache > usage {
		return 0, fmt.Errorf("inactive file cache %d exceeds memory usage %d", cache, usage)
	}
	return usage - cache, nil
}

func percentile(values []float64, p float64) (float64, error) {
	if len(values) == 0 {
		return 0, fmt.Errorf("percentile requires samples")
	}
	if p < 0 || p > 100 || math.IsNaN(p) || math.IsInf(p, 0) {
		return 0, fmt.Errorf("invalid percentile %v", p)
	}
	owned := slices.Clone(values)
	for _, value := range owned {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, fmt.Errorf("percentile sample is non-finite")
		}
	}
	slices.Sort(owned)
	idx := p / 100 * float64(len(owned)-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= len(owned) {
		return owned[len(owned)-1], nil
	}
	frac := idx - float64(lower)
	return owned[lower]*(1-frac) + owned[upper]*frac, nil
}

func resourceSampleFromStats(
	at time.Time,
	stats container.StatsResponse,
	limits WorkloadLimits,
) (resourceSample, error) {
	if limits.CPUCores <= 0 || limits.MemoryBytes <= 0 {
		return resourceSample{}, fmt.Errorf("invalid workload limits")
	}
	if stats.Read.IsZero() || stats.PreRead.IsZero() || !stats.Read.After(stats.PreRead) {
		return resourceSample{}, fmt.Errorf("invalid Docker stats interval")
	}
	cpuUsage := stats.CPUStats.CPUUsage.TotalUsage
	previousCPUUsage := stats.PreCPUStats.CPUUsage.TotalUsage
	if cpuUsage < previousCPUUsage {
		return resourceSample{}, fmt.Errorf("invalid CPU stats delta")
	}
	cpuDelta := float64(cpuUsage - previousCPUUsage)
	elapsed := float64(stats.Read.Sub(stats.PreRead))
	cpuQuotaPercent := cpuDelta / elapsed / limits.CPUCores * 100
	workingSet, err := workingSetBytes(stats.MemoryStats.Usage, stats.MemoryStats.Stats)
	if err != nil {
		return resourceSample{}, err
	}
	return resourceSample{
		At:                  at,
		CPUQuotaPercent:     cpuQuotaPercent,
		WorkingSetBytes:     workingSet,
		CPUPeriods:          stats.CPUStats.ThrottlingData.Periods,
		CPUThrottledPeriods: stats.CPUStats.ThrottlingData.ThrottledPeriods,
	}, nil
}

func throttlingPercent(first, last resourceSample) (float64, error) {
	if last.CPUPeriods <= first.CPUPeriods ||
		last.CPUThrottledPeriods < first.CPUThrottledPeriods {
		return 0, fmt.Errorf(
			"invalid throttling counter delta: periods %d->%d, throttled %d->%d",
			first.CPUPeriods, last.CPUPeriods,
			first.CPUThrottledPeriods, last.CPUThrottledPeriods,
		)
	}
	periods := last.CPUPeriods - first.CPUPeriods
	throttled := last.CPUThrottledPeriods - first.CPUThrottledPeriods
	if throttled > periods {
		return 0, fmt.Errorf("throttled period delta exceeds total period delta")
	}
	return float64(throttled) / float64(periods) * 100, nil
}

func summarizeResources(
	samples ResourceSamplesV2,
	start, end time.Time,
	limits WorkloadLimits,
) (ResourceSummaryV2, error) {
	if err := limits.validate(); err != nil {
		return ResourceSummaryV2{}, err
	}
	resources, err := phaseSamples(samples.Resources, func(sample resourceSample) time.Time {
		return sample.At
	}, start, end)
	if err != nil {
		return ResourceSummaryV2{}, err
	}
	metrics, err := phaseSamples(samples.Metrics, func(sample metricSamplePoint) time.Time {
		return sample.At
	}, start, end)
	if err != nil {
		return ResourceSummaryV2{}, err
	}
	pauses, err := phaseSamples(samples.GCPauses, func(sample gcPauseSample) time.Time {
		return sample.At
	}, start, end)
	if err != nil {
		return ResourceSummaryV2{}, err
	}
	if len(resources) < 2 {
		return ResourceSummaryV2{}, fmt.Errorf("measurement requires at least two resource samples")
	}
	if len(metrics) < 2 {
		return ResourceSummaryV2{}, fmt.Errorf("measurement requires at least two metric samples")
	}
	cpuValues := make([]float64, len(resources)-1)
	memoryValues := make([]float64, len(resources))
	for i, sample := range resources {
		memoryValues[i] = float64(sample.WorkingSetBytes) / (1024 * 1024)
		if i > 0 {
			cpuValues[i-1] = sample.CPUQuotaPercent
		}
	}
	cpuP95, err := percentile(cpuValues, 95)
	if err != nil {
		return ResourceSummaryV2{}, err
	}
	memoryP99, err := percentile(memoryValues, 99)
	if err != nil {
		return ResourceSummaryV2{}, err
	}
	throttling, err := throttlingPercent(resources[0], resources[len(resources)-1])
	if err != nil {
		return ResourceSummaryV2{}, err
	}
	firstMetric := metrics[0]
	lastMetric := metrics[len(metrics)-1]
	if lastMetric.SocketDrops < firstMetric.SocketDrops || lastMetric.RTPDrops < firstMetric.RTPDrops {
		return ResourceSummaryV2{}, fmt.Errorf("drop counter rollback")
	}
	channelPeak := 0.0
	for _, sample := range metrics {
		if !finiteFloats(sample.ChannelLength, sample.SocketDrops, sample.RTPDrops) {
			return ResourceSummaryV2{}, fmt.Errorf("metric sample contains a non-finite value")
		}
		channelPeak = max(channelPeak, sample.ChannelLength)
	}
	gcMax := 0.0
	for _, pause := range pauses {
		if !finiteFloats(pause.DurationMS) || pause.DurationMS < 0 {
			return ResourceSummaryV2{}, fmt.Errorf("invalid GC pause")
		}
		gcMax = max(gcMax, pause.DurationMS)
	}
	return ResourceSummaryV2{
		Limits:            limits,
		CPUP95Percent:     cpuP95,
		WorkingSetP99MB:   memoryP99,
		ThrottlingPercent: throttling,
		ChannelPeak:       channelPeak,
		SocketDrops:       lastMetric.SocketDrops - firstMetric.SocketDrops,
		RTPDrops:          lastMetric.RTPDrops - firstMetric.RTPDrops,
		GCMaxSTWMS:        gcMax,
	}, nil
}

func validateAbsoluteResourceGates(summary ResourceSummaryV2) error {
	if err := summary.Limits.validate(); err != nil {
		return err
	}
	values := []float64{
		summary.CPUP95Percent, summary.WorkingSetP99MB, summary.ThrottlingPercent,
		summary.ChannelPeak, summary.SocketDrops, summary.RTPDrops, summary.GCMaxSTWMS,
	}
	if !finiteFloats(values...) {
		return fmt.Errorf("resource summary contains a non-finite value")
	}
	for _, value := range values {
		if value < 0 {
			return fmt.Errorf("resource summary contains a negative value")
		}
	}
	if summary.CPUP95Percent > 80 {
		return fmt.Errorf("CPU p95 %.3f%% exceeds 80%% quota", summary.CPUP95Percent)
	}
	memoryLimitMB := float64(summary.Limits.MemoryBytes) / (1024 * 1024)
	if summary.WorkingSetP99MB > memoryLimitMB*0.8 {
		return fmt.Errorf("working-set p99 %.3f MiB exceeds 80%% limit", summary.WorkingSetP99MB)
	}
	if summary.ThrottlingPercent > 1 {
		return fmt.Errorf("CPU throttling %.3f%% exceeds 1%%", summary.ThrottlingPercent)
	}
	if summary.GCMaxSTWMS >= 50 {
		return fmt.Errorf("GC max STW %.3f ms is not below 50 ms", summary.GCMaxSTWMS)
	}
	if summary.SocketDrops != 0 || summary.RTPDrops != 0 {
		return fmt.Errorf("measurement contains drops")
	}
	return nil
}

func resourceMetricEntries(summary ResourceSummaryV2) map[string]MetricEntry {
	return map[string]MetricEntry{
		"cpu_p95_percent":    {Value: summary.CPUP95Percent, Unit: "%", Direction: dirLowerIsBetter},
		"working_set_p99_mb": {Value: summary.WorkingSetP99MB, Unit: "MiB", Direction: dirLowerIsBetter},
		"throttling_percent": {Value: summary.ThrottlingPercent, Unit: "%", Direction: dirLowerIsBetter},
		"channel_peak":       {Value: summary.ChannelPeak, Unit: "count", Direction: dirLowerIsBetter},
		"socket_drops":       {Value: summary.SocketDrops, Unit: "count", Direction: dirLowerIsBetter},
		"rtp_drops":          {Value: summary.RTPDrops, Unit: "count", Direction: dirLowerIsBetter},
		"gc_max_stw_ms":      {Value: summary.GCMaxSTWMS, Unit: "ms", Direction: dirLowerIsBetter},
	}
}
