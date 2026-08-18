//go:build e2e

package load

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validBaselineV2() BaselineV2 {
	return BaselineV2{
		Version:   2,
		Kind:      baselineKindAccepted,
		CreatedAt: time.Date(2026, time.August, 14, 17, 0, 0, 0, time.UTC),
		Fingerprint: BenchmarkFingerprint{
			OS:            "linux",
			Arch:          "amd64",
			GoVersion:     "go1.26.6",
			KernelVersion: "6.12.94",
			DockerVersion: "29.5.3",
		},
		RepeatCount:   5,
		SourceCommits: []string{"a", "b", "c", "d", "e"},
		Results: []BaselineScenarioV2{{
			Name:   "TestLoad/row",
			Limits: WorkloadLimits{CPUCores: 2, MemoryBytes: 256 << 20},
			Metrics: []BaselineMetricV2{{
				Name:         "actual_cps",
				Median:       100,
				Unit:         "cps",
				Direction:    dirHigherIsBetter,
				TolerancePct: 3,
			}},
		}},
	}
}

func TestBaselineV2RejectsInvalidSchema(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BaselineV2)
	}{
		{name: "wrong version", mutate: func(baseline *BaselineV2) { baseline.Version = 1 }},
		{name: "empty kind", mutate: func(baseline *BaselineV2) { baseline.Kind = "" }},
		{name: "unknown kind", mutate: func(baseline *BaselineV2) { baseline.Kind = "draft" }},
		{name: "zero timestamp", mutate: func(baseline *BaselineV2) { baseline.CreatedAt = time.Time{} }},
		{name: "missing OS", mutate: func(baseline *BaselineV2) { baseline.Fingerprint.OS = "" }},
		{name: "missing arch", mutate: func(baseline *BaselineV2) { baseline.Fingerprint.Arch = "" }},
		{name: "missing Go version", mutate: func(baseline *BaselineV2) {
			baseline.Fingerprint.GoVersion = ""
		}},
		{name: "missing kernel version", mutate: func(baseline *BaselineV2) {
			baseline.Fingerprint.KernelVersion = ""
		}},
		{name: "missing Docker version", mutate: func(baseline *BaselineV2) {
			baseline.Fingerprint.DockerVersion = ""
		}},
		{name: "wrong repeat count", mutate: func(baseline *BaselineV2) { baseline.RepeatCount = 3 }},
		{name: "source commit count mismatch", mutate: func(baseline *BaselineV2) {
			baseline.SourceCommits = baseline.SourceCommits[:4]
		}},
		{name: "empty source commit", mutate: func(baseline *BaselineV2) {
			baseline.SourceCommits[2] = ""
		}},
		{name: "empty results", mutate: func(baseline *BaselineV2) { baseline.Results = nil }},
		{name: "empty scenario name", mutate: func(baseline *BaselineV2) {
			baseline.Results[0].Name = ""
		}},
		{name: "duplicate scenario", mutate: func(baseline *BaselineV2) {
			baseline.Results = append(baseline.Results, baseline.Results[0])
		}},
		{name: "empty metrics", mutate: func(baseline *BaselineV2) {
			baseline.Results[0].Metrics = nil
		}},
		{name: "missing CPU limit", mutate: func(baseline *BaselineV2) {
			baseline.Results[0].Limits.CPUCores = 0
		}},
		{name: "missing memory limit", mutate: func(baseline *BaselineV2) {
			baseline.Results[0].Limits.MemoryBytes = 0
		}},
		{name: "empty metric name", mutate: func(baseline *BaselineV2) {
			baseline.Results[0].Metrics[0].Name = ""
		}},
		{name: "duplicate metric", mutate: func(baseline *BaselineV2) {
			metric := baseline.Results[0].Metrics[0]
			baseline.Results[0].Metrics = append(baseline.Results[0].Metrics, metric)
		}},
		{name: "empty unit", mutate: func(baseline *BaselineV2) {
			baseline.Results[0].Metrics[0].Unit = ""
		}},
		{name: "invalid direction", mutate: func(baseline *BaselineV2) {
			baseline.Results[0].Metrics[0].Direction = "neutral"
		}},
		{name: "negative tolerance", mutate: func(baseline *BaselineV2) {
			baseline.Results[0].Metrics[0].TolerancePct = -1
		}},
		{name: "NaN tolerance", mutate: func(baseline *BaselineV2) {
			baseline.Results[0].Metrics[0].TolerancePct = math.NaN()
		}},
		{name: "infinite tolerance", mutate: func(baseline *BaselineV2) {
			baseline.Results[0].Metrics[0].TolerancePct = math.Inf(1)
		}},
		{name: "NaN median", mutate: func(baseline *BaselineV2) {
			baseline.Results[0].Metrics[0].Median = math.NaN()
		}},
		{name: "infinite median", mutate: func(baseline *BaselineV2) {
			baseline.Results[0].Metrics[0].Median = math.Inf(-1)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseline := validBaselineV2()
			tt.mutate(&baseline)

			require.Error(t, baseline.Validate())
		})
	}
}

func TestBaselineV2AcceptsValidSchema(t *testing.T) {
	require.NoError(t, validBaselineV2().Validate())
}

func TestBaselineV2DecodeValidatesSchema(t *testing.T) {
	data, err := json.Marshal(validBaselineV2())
	require.NoError(t, err)

	decoded, err := decodeBaselineV2(data)
	require.NoError(t, err)
	require.Equal(t, validBaselineV2(), decoded)

	_, err = decodeBaselineV2([]byte(`{"version":99}`))
	require.Error(t, err)
}

func runArtifactsForAggregation(mode runMode, count int) []RunArtifactV2 {
	runs := make([]RunArtifactV2, count)
	for i := range runs {
		runs[i] = validRunArtifactV2()
		runs[i].Mode = mode
		runs[i].ReleaseEligible = mode == runModeRelease
		runs[i].Commit = string(rune('a' + i))
	}
	return runs
}

func TestAggregateRunArtifactsRequiresExactModeAndCount(t *testing.T) {
	tests := []struct {
		name    string
		mode    runMode
		count   int
		wantErr bool
	}{
		{name: "release short", mode: runModeRelease, count: 2, wantErr: true},
		{name: "release exact", mode: runModeRelease, count: 3},
		{name: "release excess", mode: runModeRelease, count: 4, wantErr: true},
		{name: "candidate short", mode: runModeCandidate, count: 4, wantErr: true},
		{name: "candidate exact", mode: runModeCandidate, count: 5},
		{name: "candidate excess", mode: runModeCandidate, count: 6, wantErr: true},
		{name: "targeted rejected", mode: runModeTargeted, count: 1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := aggregateRunArtifacts(tt.mode, runArtifactsForAggregation(tt.mode, tt.count))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.mode, got.Mode)
			require.Equal(t, tt.count, got.RepeatCount)
		})
	}
}

func TestAggregateRunArtifactsRejectsMixedMode(t *testing.T) {
	runs := runArtifactsForAggregation(runModeRelease, 3)
	runs[1].Mode = runModeCandidate
	runs[1].ReleaseEligible = false

	_, err := aggregateRunArtifacts(runModeRelease, runs)
	require.ErrorContains(t, err, "run 1")
}

func TestAggregateRunArtifactsRequiresSymmetricInventory(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*RunArtifactV2)
		wantDetail string
	}{
		{name: "missing scenario", mutate: func(run *RunArtifactV2) {
			run.Results = run.Results[1:]
		}, wantDetail: "TestLoadINVITEFlood/rate_100"},
		{name: "new scenario", mutate: func(run *RunArtifactV2) {
			row := run.Results[0]
			row.Name = "TestLoad/new"
			run.Results = append(run.Results, row)
		}, wantDetail: "TestLoad/new"},
		{name: "renamed scenario", mutate: func(run *RunArtifactV2) {
			run.Results[0].Name = "TestLoad/renamed"
		}, wantDetail: "TestLoad/renamed"},
		{name: "duplicate scenario", mutate: func(run *RunArtifactV2) {
			run.Results = append(run.Results, run.Results[0])
		}, wantDetail: "TestLoadINVITEFlood/rate_100"},
		{name: "missing metric", mutate: func(run *RunArtifactV2) {
			delete(run.Results[0].Metrics, "actual_cps")
		}, wantDetail: "actual_cps"},
		{name: "new metric", mutate: func(run *RunArtifactV2) {
			run.Results[0].Metrics["errors"] = MetricEntry{
				Value: 0, Unit: "count", Direction: dirLowerIsBetter,
			}
		}, wantDetail: "errors"},
		{name: "changed unit", mutate: func(run *RunArtifactV2) {
			metric := run.Results[0].Metrics["actual_cps"]
			metric.Unit = "pps"
			run.Results[0].Metrics["actual_cps"] = metric
		}, wantDetail: "actual_cps"},
		{name: "changed direction", mutate: func(run *RunArtifactV2) {
			metric := run.Results[0].Metrics["actual_cps"]
			metric.Direction = dirLowerIsBetter
			run.Results[0].Metrics["actual_cps"] = metric
		}, wantDetail: "actual_cps"},
		{name: "invalid run", mutate: func(run *RunArtifactV2) {
			run.FinishedAt = time.Time{}
		}, wantDetail: "run 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runs := runArtifactsForAggregation(runModeRelease, 3)
			for i := range runs {
				row := runs[i].Results[0]
				row.Name = "TestLoad/stable"
				runs[i].Results = append(runs[i].Results, row)
				runs[i].Results[0].Metrics["stable"] = MetricEntry{
					Value: 1, Unit: "count", Direction: dirHigherIsBetter,
				}
			}
			tt.mutate(&runs[1])

			_, err := aggregateRunArtifacts(runModeRelease, runs)
			require.ErrorContains(t, err, "run 1")
			require.ErrorContains(t, err, tt.wantDetail)
		})
	}
}

func TestAggregateRunArtifactsRequiresCompatibleFingerprint(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RunArtifactV2)
	}{
		{name: "OS", mutate: func(run *RunArtifactV2) { run.Environment.OS = "freebsd" }},
		{name: "arch", mutate: func(run *RunArtifactV2) { run.Environment.Arch = "arm64" }},
		{name: "Go", mutate: func(run *RunArtifactV2) { run.Environment.GoVersion = "go1.27" }},
		{name: "kernel", mutate: func(run *RunArtifactV2) { run.Environment.KernelVersion = "7.0" }},
		{name: "Docker", mutate: func(run *RunArtifactV2) { run.Environment.DockerVersion = "30.0" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runs := runArtifactsForAggregation(runModeRelease, 3)
			tt.mutate(&runs[1])

			_, err := aggregateRunArtifacts(runModeRelease, runs)
			require.ErrorContains(t, err, "run 1")
			require.ErrorContains(t, err, "fingerprint")
		})
	}
}

func TestAggregateRunArtifactsRequiresMatchingScenarioLimits(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkloadLimits)
	}{
		{name: "CPU", mutate: func(limits *WorkloadLimits) { limits.CPUCores = 1 }},
		{name: "memory", mutate: func(limits *WorkloadLimits) { limits.MemoryBytes = 128 << 20 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runs := runArtifactsForAggregation(runModeRelease, 3)
			tt.mutate(&runs[1].Results[0].Limits)

			_, err := aggregateRunArtifacts(runModeRelease, runs)
			require.ErrorContains(t, err, "limits")
		})
	}
}

func TestCandidateBaselineOwnsScenarioLimits(t *testing.T) {
	runs := runArtifactsForAggregation(runModeCandidate, 5)
	aggregated, err := aggregateRunArtifacts(runModeCandidate, runs)
	require.NoError(t, err)

	baseline, err := buildCandidateBaseline(aggregated, time.Now())
	require.NoError(t, err)
	require.Equal(t, runs[0].Results[0].Limits, baseline.Results[0].Limits)

	aggregated.Results[0].Limits.CPUCores = 99
	require.Equal(t, float64(2), baseline.Results[0].Limits.CPUCores)
}

func TestAggregateRunArtifactsIgnoresCommitAndImageForFingerprint(t *testing.T) {
	runs := runArtifactsForAggregation(runModeRelease, 3)
	runs[1].Commit = "different"
	runs[1].Environment.ExporterImage = "sip-exporter:different"

	_, err := aggregateRunArtifacts(runModeRelease, runs)
	require.NoError(t, err)
}

func TestAggregateRunArtifactsCalculatesMedianWithoutMutatingInput(t *testing.T) {
	runs := runArtifactsForAggregation(runModeRelease, 3)
	values := []float64{300, 100, 200}
	for i := range runs {
		runs[i].Results[0].Metrics["actual_cps"] = MetricEntry{
			Value: values[i], Unit: "cps", Direction: dirHigherIsBetter,
		}
		runs[i].Results[0].Metrics["errors"] = MetricEntry{
			Value: float64(2 - i), Unit: "count", Direction: dirLowerIsBetter,
		}
	}
	wantRuns := make([]RunArtifactV2, len(runs))
	for i := range runs {
		wantRuns[i] = cloneRunArtifactV2(runs[i])
	}

	got, err := aggregateRunArtifacts(runModeRelease, runs)
	require.NoError(t, err)
	require.Equal(t, []AggregatedScenarioV2{{
		Name:   "TestLoadINVITEFlood/rate_100",
		Limits: WorkloadLimits{CPUCores: 2, MemoryBytes: 256 << 20},
		Metrics: []AggregatedMetricV2{
			{Name: "actual_cps", Median: 200, Unit: "cps", Direction: dirHigherIsBetter},
			{Name: "channel_peak", Median: 0, Unit: "count", Direction: dirLowerIsBetter},
			{Name: "cpu_p95_percent", Median: 0, Unit: "%", Direction: dirLowerIsBetter},
			{Name: "errors", Median: 1, Unit: "count", Direction: dirLowerIsBetter},
			{Name: "gc_max_stw_ms", Median: 0, Unit: "ms", Direction: dirLowerIsBetter},
			{Name: "rtp_drops", Median: 0, Unit: "count", Direction: dirLowerIsBetter},
			{Name: "socket_drops", Median: 0, Unit: "count", Direction: dirLowerIsBetter},
			{Name: "throttling_percent", Median: 0, Unit: "%", Direction: dirLowerIsBetter},
			{Name: "working_set_p99_mb", Median: 0, Unit: "MiB", Direction: dirLowerIsBetter},
		},
	}}, got.Results)
	require.Equal(t, wantRuns, runs)
}

func TestThresholdPolicyUsesFixedMetricClasses(t *testing.T) {
	tests := []struct {
		name      string
		scenario  string
		metric    AggregatedMetricV2
		want      float64
		wantError bool
	}{
		{name: "CPS throughput", scenario: "TestLoad/row", metric: AggregatedMetricV2{
			Name: "actual_cps", Direction: dirHigherIsBetter,
		}, want: 3},
		{name: "PPS throughput", scenario: "TestLoad/row", metric: AggregatedMetricV2{
			Name: "actual_pps", Direction: dirHigherIsBetter,
		}, want: 3},
		{name: "average CPU", scenario: "TestLoad/row", metric: AggregatedMetricV2{
			Name: "cpu_avg", Direction: dirLowerIsBetter,
		}, want: 10},
		{name: "peak CPU", scenario: "TestLoad/row", metric: AggregatedMetricV2{
			Name: "cpu_peak", Direction: dirLowerIsBetter,
		}, want: 10},
		{name: "memory", scenario: "TestLoad/row", metric: AggregatedMetricV2{
			Name: "mem_mb", Direction: dirLowerIsBetter,
		}, want: 10},
		{name: "GC latency", scenario: "TestBenchmarkGCPauseDuration", metric: AggregatedMetricV2{
			Name: "p95_ms", Direction: dirLowerIsBetter,
		}, want: 20},
		{name: "scrape latency", scenario: "TestLoadScrapeLatency", metric: AggregatedMetricV2{
			Name: "max_ms", Direction: dirLowerIsBetter,
		}, want: 20},
		{name: "millisecond metric outside latency scenario", scenario: "TestLoad/row", metric: AggregatedMetricV2{
			Name: "drain_time_ms", Direction: dirLowerIsBetter,
		}},
		{name: "non-millisecond metric in latency scenario", scenario: "TestLoadScrapeLatency", metric: AggregatedMetricV2{
			Name: "scrapes", Direction: dirHigherIsBetter,
		}},
		{name: "correctness", scenario: "TestLoad/row", metric: AggregatedMetricV2{
			Name: "errors", Direction: dirLowerIsBetter,
		}},
		{name: "wrong throughput direction", scenario: "TestLoad/row", metric: AggregatedMetricV2{
			Name: "actual_cps", Direction: dirLowerIsBetter,
		}, wantError: true},
		{name: "wrong resource direction", scenario: "TestLoad/row", metric: AggregatedMetricV2{
			Name: "cpu_peak", Direction: dirHigherIsBetter,
		}, wantError: true},
		{name: "wrong latency direction", scenario: "TestBenchmarkGCPauseDuration", metric: AggregatedMetricV2{
			Name: "p95_ms", Direction: dirHigherIsBetter,
		}, wantError: true},
		{name: "invalid fallback direction", scenario: "TestLoad/row", metric: AggregatedMetricV2{
			Name: "errors", Direction: "neutral",
		}, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := thresholdPolicy(tt.scenario, tt.metric)
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestThresholdPolicyClassifiesCanonicalResourceMetrics(t *testing.T) {
	tests := []struct {
		name     string
		scenario string
		metric   string
		want     float64
	}{
		{name: "CPU", scenario: "TestLoad/row", metric: "cpu_p95_percent", want: 10},
		{name: "working set", scenario: "TestLoad/row", metric: "working_set_p99_mb", want: 10},
		{name: "soak first minute working set", scenario: "TestReleaseSoak", metric: "working_set_first_minute_median_mb", want: 10},
		{name: "soak last minute working set", scenario: "TestReleaseSoak", metric: "working_set_last_minute_median_mb", want: 10},
		{name: "soak working set growth", scenario: "TestReleaseSoak", metric: "working_set_growth_mb", want: 10},
		{name: "throttling", scenario: "TestLoad/row", metric: "throttling_percent"},
		{name: "channel", scenario: "TestLoad/row", metric: "channel_peak"},
		{name: "socket drops", scenario: "TestLoad/row", metric: "socket_drops"},
		{name: "RTP drops", scenario: "TestLoad/row", metric: "rtp_drops"},
		{name: "GC", scenario: "TestBenchmarkGCPauseDuration", metric: "gc_max_stw_ms", want: 20},
		{name: "scrape p50", scenario: "TestBenchmarkScrapeLatencyUnderLoad", metric: "scrape_p50_ms", want: 20},
		{name: "scrape p95", scenario: "TestBenchmarkScrapeLatencyUnderLoad", metric: "scrape_p95_ms", want: 20},
		{name: "scrape p99", scenario: "TestBenchmarkScrapeLatencyUnderLoad", metric: "scrape_p99_ms", want: 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := thresholdPolicy(tt.scenario, AggregatedMetricV2{
				Name: tt.metric, Direction: dirLowerIsBetter,
			})
			require.NoError(t, err)
			require.Equal(t, tt.want, got)

			_, err = thresholdPolicy(tt.scenario, AggregatedMetricV2{
				Name: tt.metric, Direction: dirHigherIsBetter,
			})
			require.Error(t, err)
		})
	}
}

func TestCandidateBaselineBuildsOwnedArtifact(t *testing.T) {
	runs := runArtifactsForAggregation(runModeCandidate, 5)
	for i := range runs {
		runs[i].Results[0].Metrics["errors"] = MetricEntry{
			Value: 0, Unit: "count", Direction: dirLowerIsBetter,
		}
	}
	aggregated, err := aggregateRunArtifacts(runModeCandidate, runs)
	require.NoError(t, err)
	createdAt := time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC)

	got, err := buildCandidateBaseline(aggregated, createdAt)
	require.NoError(t, err)
	require.Equal(t, baselineKindCandidate, got.Kind)
	require.Equal(t, 5, got.RepeatCount)
	require.Equal(t, createdAt, got.CreatedAt)
	require.Equal(t, []BaselineMetricV2{
		{Name: "actual_cps", Median: 100, Unit: "cps", Direction: dirHigherIsBetter, TolerancePct: 3},
		{Name: "channel_peak", Median: 0, Unit: "count", Direction: dirLowerIsBetter, TolerancePct: 0},
		{Name: "cpu_p95_percent", Median: 0, Unit: "%", Direction: dirLowerIsBetter, TolerancePct: 10},
		{Name: "errors", Median: 0, Unit: "count", Direction: dirLowerIsBetter, TolerancePct: 0},
		{Name: "gc_max_stw_ms", Median: 0, Unit: "ms", Direction: dirLowerIsBetter, TolerancePct: 20},
		{Name: "rtp_drops", Median: 0, Unit: "count", Direction: dirLowerIsBetter, TolerancePct: 0},
		{Name: "socket_drops", Median: 0, Unit: "count", Direction: dirLowerIsBetter, TolerancePct: 0},
		{Name: "throttling_percent", Median: 0, Unit: "%", Direction: dirLowerIsBetter, TolerancePct: 0},
		{Name: "working_set_p99_mb", Median: 0, Unit: "MiB", Direction: dirLowerIsBetter, TolerancePct: 10},
	}, got.Results[0].Metrics)
	require.NoError(t, got.Validate())
}

func TestCandidateBaselineRequiresCandidateModeAndFiveRuns(t *testing.T) {
	aggregated, err := aggregateRunArtifacts(runModeCandidate, runArtifactsForAggregation(runModeCandidate, 5))
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(*AggregatedRunV2)
	}{
		{name: "wrong mode", mutate: func(aggregate *AggregatedRunV2) {
			aggregate.Mode = runModeRelease
		}},
		{name: "wrong count", mutate: func(aggregate *AggregatedRunV2) {
			aggregate.RepeatCount = 4
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invalid := aggregated
			tt.mutate(&invalid)
			_, err := buildCandidateBaseline(invalid, time.Now())
			require.Error(t, err)
		})
	}
}

func TestCandidateBaselineWriterCannotReplaceAcceptedBaseline(t *testing.T) {
	root := t.TempDir()
	acceptedPath := filepath.Join(root, "baseline-v2.json")
	require.NoError(t, os.WriteFile(acceptedPath, []byte("owner-approved"), 0o644))
	baseline := validBaselineV2()
	baseline.Kind = baselineKindCandidate
	baseline.Results[0].Metrics = append(baseline.Results[0].Metrics, BaselineMetricV2{
		Name: "aaa_metric", Median: 1, Unit: "count", Direction: dirHigherIsBetter,
	})
	baseline.Results = append(baseline.Results, BaselineScenarioV2{
		Name:   "AFirstScenario",
		Limits: WorkloadLimits{CPUCores: 2, MemoryBytes: 256 << 20},
		Metrics: []BaselineMetricV2{{
			Name: "metric", Median: 1, Unit: "count", Direction: dirHigherIsBetter,
		}},
	})
	inputBefore, err := json.Marshal(baseline)
	require.NoError(t, err)

	require.NoError(t, writeCandidateBaseline(root, baseline))
	data, err := os.ReadFile(filepath.Join(root, "baseline-candidate.json"))
	require.NoError(t, err)
	decoded, err := decodeBaselineV2(data)
	require.NoError(t, err)
	require.Equal(t, []string{"AFirstScenario", "TestLoad/row"}, []string{
		decoded.Results[0].Name, decoded.Results[1].Name,
	})
	require.Equal(t, []string{"aaa_metric", "actual_cps"}, []string{
		decoded.Results[1].Metrics[0].Name, decoded.Results[1].Metrics[1].Name,
	})
	inputAfter, err := json.Marshal(baseline)
	require.NoError(t, err)
	require.Equal(t, inputBefore, inputAfter)
	accepted, err := os.ReadFile(acceptedPath)
	require.NoError(t, err)
	require.Equal(t, []byte("owner-approved"), accepted)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 2)
}

func acceptedBaselineForRelease(aggregated AggregatedRunV2) BaselineV2 {
	baseline := BaselineV2{
		Version:     baselineSchemaVersion,
		Kind:        baselineKindAccepted,
		CreatedAt:   time.Date(2026, time.August, 14, 19, 0, 0, 0, time.UTC),
		Fingerprint: aggregated.Fingerprint,
		RepeatCount: baselineRepeatCount,
		SourceCommits: []string{
			"accepted-a", "accepted-b", "accepted-c", "accepted-d", "accepted-e",
		},
	}
	for _, aggregatedScenario := range aggregated.Results {
		scenario := BaselineScenarioV2{Name: aggregatedScenario.Name, Limits: aggregatedScenario.Limits}
		for _, aggregatedMetric := range aggregatedScenario.Metrics {
			tolerance, err := thresholdPolicy(aggregatedScenario.Name, aggregatedMetric)
			if err != nil {
				panic(err)
			}
			scenario.Metrics = append(scenario.Metrics, BaselineMetricV2{
				Name: aggregatedMetric.Name, Median: aggregatedMetric.Median,
				Unit: aggregatedMetric.Unit, Direction: aggregatedMetric.Direction,
				TolerancePct: tolerance,
			})
		}
		baseline.Results = append(baseline.Results, scenario)
	}
	return baseline
}

func validReleaseComparisonPair(t *testing.T) (BaselineV2, AggregatedRunV2) {
	t.Helper()
	aggregated, err := aggregateRunArtifacts(runModeRelease, runArtifactsForAggregation(runModeRelease, 3))
	require.NoError(t, err)
	return acceptedBaselineForRelease(aggregated), aggregated
}

func TestCompareReleaseRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BaselineV2, *AggregatedRunV2)
	}{
		{name: "invalid baseline", mutate: func(baseline *BaselineV2, _ *AggregatedRunV2) {
			baseline.Version = 1
		}},
		{name: "candidate baseline", mutate: func(baseline *BaselineV2, _ *AggregatedRunV2) {
			baseline.Kind = baselineKindCandidate
		}},
		{name: "candidate aggregate", mutate: func(_ *BaselineV2, current *AggregatedRunV2) {
			current.Mode = runModeCandidate
		}},
		{name: "wrong release count", mutate: func(_ *BaselineV2, current *AggregatedRunV2) {
			current.RepeatCount = 2
		}},
		{name: "incompatible fingerprint", mutate: func(_ *BaselineV2, current *AggregatedRunV2) {
			current.Fingerprint.OS = "freebsd"
		}},
		{name: "missing scenario", mutate: func(_ *BaselineV2, current *AggregatedRunV2) {
			current.Results = nil
		}},
		{name: "new scenario", mutate: func(_ *BaselineV2, current *AggregatedRunV2) {
			current.Results = append(current.Results, AggregatedScenarioV2{
				Name: "TestLoad/new", Metrics: current.Results[0].Metrics,
			})
		}},
		{name: "renamed scenario", mutate: func(_ *BaselineV2, current *AggregatedRunV2) {
			current.Results[0].Name = "TestLoad/renamed"
		}},
		{name: "duplicate scenario", mutate: func(_ *BaselineV2, current *AggregatedRunV2) {
			current.Results = append(current.Results, current.Results[0])
		}},
		{name: "missing metric", mutate: func(_ *BaselineV2, current *AggregatedRunV2) {
			current.Results[0].Metrics = nil
		}},
		{name: "new metric", mutate: func(_ *BaselineV2, current *AggregatedRunV2) {
			current.Results[0].Metrics = append(current.Results[0].Metrics, AggregatedMetricV2{
				Name: "errors", Median: 0, Unit: "count", Direction: dirLowerIsBetter,
			})
		}},
		{name: "duplicate metric", mutate: func(_ *BaselineV2, current *AggregatedRunV2) {
			current.Results[0].Metrics = append(current.Results[0].Metrics, current.Results[0].Metrics[0])
		}},
		{name: "changed unit", mutate: func(_ *BaselineV2, current *AggregatedRunV2) {
			current.Results[0].Metrics[0].Unit = "pps"
		}},
		{name: "changed direction", mutate: func(_ *BaselineV2, current *AggregatedRunV2) {
			current.Results[0].Metrics[0].Direction = dirLowerIsBetter
		}},
		{name: "changed tolerance", mutate: func(baseline *BaselineV2, _ *AggregatedRunV2) {
			baseline.Results[0].Metrics[0].TolerancePct = 4
		}},
		{name: "changed CPU limit", mutate: func(baseline *BaselineV2, _ *AggregatedRunV2) {
			baseline.Results[0].Limits.CPUCores = 1
		}},
		{name: "changed memory limit", mutate: func(baseline *BaselineV2, _ *AggregatedRunV2) {
			baseline.Results[0].Limits.MemoryBytes = 128 << 20
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseline, current := validReleaseComparisonPair(t)
			tt.mutate(&baseline, &current)

			_, err := compareRelease(baseline, current)
			require.Error(t, err)
		})
	}
}

func TestCompareReleaseReturnsFullReportAndRegressionError(t *testing.T) {
	baseline, current := validReleaseComparisonPair(t)
	baseline.Results[0].Metrics = append(baseline.Results[0].Metrics, BaselineMetricV2{
		Name: "errors", Median: 0, Unit: "count", Direction: dirLowerIsBetter,
	})
	current.Results[0].Metrics = append(current.Results[0].Metrics, AggregatedMetricV2{
		Name: "errors", Median: 0, Unit: "count", Direction: dirLowerIsBetter,
	})
	current.Results[0].Metrics[0].Median = 96

	report, err := compareRelease(baseline, current)
	require.ErrorContains(t, err, "actual_cps")
	require.Len(t, report.Entries, 9)
	require.Equal(t, StatusRegression, report.Entries[0].Status)
	require.Equal(t, StatusOK, report.Entries[1].Status)
}

func TestCompareReleaseValidatesCompleteInventoryBeforePolicy(t *testing.T) {
	runs := runArtifactsForAggregation(runModeRelease, 3)
	for i := range runs {
		runs[i].Results[0].Metrics["errors"] = MetricEntry{
			Value: 0, Unit: "count", Direction: dirLowerIsBetter,
		}
	}
	current, err := aggregateRunArtifacts(runModeRelease, runs)
	require.NoError(t, err)
	baseline := acceptedBaselineForRelease(current)
	baselineMetricForTest(&baseline, "actual_cps").TolerancePct = 99
	metrics := make([]AggregatedMetricV2, 0, len(current.Results[0].Metrics)-1)
	for _, metric := range current.Results[0].Metrics {
		if metric.Name != "errors" {
			metrics = append(metrics, metric)
		}
	}
	current.Results[0].Metrics = metrics

	for range 100 {
		_, err := compareRelease(baseline, current)
		require.ErrorContains(t, err, "missing metric \"errors\"")
	}
}

func TestCompareReleaseSortsMultiScenarioReport(t *testing.T) {
	runs := runArtifactsForAggregation(runModeRelease, 3)
	for i := range runs {
		limits := WorkloadLimits{CPUCores: 2, MemoryBytes: 256 << 20}
		resources := ResourceSummaryV2{Limits: limits}
		metrics := resourceMetricEntries(resources)
		metrics["z_metric"] = MetricEntry{Value: 1, Unit: "count", Direction: dirHigherIsBetter}
		runs[i].Results = append(runs[i].Results, ScenarioResultV2{
			Name:       "AFirstScenario",
			Status:     scenarioStatusComplete,
			StartedAt:  runs[i].StartedAt,
			FinishedAt: runs[i].FinishedAt,
			Limits:     limits,
			Resources:  &resources,
			Metrics:    metrics,
		})
	}
	current, err := aggregateRunArtifacts(runModeRelease, runs)
	require.NoError(t, err)

	report, err := compareRelease(acceptedBaselineForRelease(current), current)
	require.NoError(t, err)
	keys := make([]string, len(report.Entries))
	for i, entry := range report.Entries {
		keys[i] = entry.Scenario + "/" + entry.Metric
	}
	require.Equal(t, []string{
		"AFirstScenario/channel_peak",
		"AFirstScenario/cpu_p95_percent",
		"AFirstScenario/gc_max_stw_ms",
		"AFirstScenario/rtp_drops",
		"AFirstScenario/socket_drops",
		"AFirstScenario/throttling_percent",
		"AFirstScenario/working_set_p99_mb",
		"AFirstScenario/z_metric",
		"TestLoadINVITEFlood/rate_100/actual_cps",
		"TestLoadINVITEFlood/rate_100/channel_peak",
		"TestLoadINVITEFlood/rate_100/cpu_p95_percent",
		"TestLoadINVITEFlood/rate_100/gc_max_stw_ms",
		"TestLoadINVITEFlood/rate_100/rtp_drops",
		"TestLoadINVITEFlood/rate_100/socket_drops",
		"TestLoadINVITEFlood/rate_100/throttling_percent",
		"TestLoadINVITEFlood/rate_100/working_set_p99_mb",
	}, keys)
}

func baselineMetricForTest(baseline *BaselineV2, name string) *BaselineMetricV2 {
	for i := range baseline.Results[0].Metrics {
		if baseline.Results[0].Metrics[i].Name == name {
			return &baseline.Results[0].Metrics[i]
		}
	}
	return nil
}

func TestClassifyMetricV2ThresholdBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		current   float64
		tolerance float64
		direction string
		want      ComparisonStatus
	}{
		{name: "higher below", current: 96.99, tolerance: 3, direction: dirHigherIsBetter, want: StatusRegression},
		{name: "higher lower boundary", current: 97, tolerance: 3, direction: dirHigherIsBetter, want: StatusOK},
		{name: "higher upper boundary", current: 103, tolerance: 3, direction: dirHigherIsBetter, want: StatusOK},
		{name: "higher above", current: 103.01, tolerance: 3, direction: dirHigherIsBetter, want: StatusImprovement},
		{name: "lower below", current: 89.99, tolerance: 10, direction: dirLowerIsBetter, want: StatusImprovement},
		{name: "lower lower boundary", current: 90, tolerance: 10, direction: dirLowerIsBetter, want: StatusOK},
		{name: "lower upper boundary", current: 110, tolerance: 10, direction: dirLowerIsBetter, want: StatusOK},
		{name: "lower above", current: 110.01, tolerance: 10, direction: dirLowerIsBetter, want: StatusRegression},
		{name: "same worsening value higher", current: 90, tolerance: 3, direction: dirHigherIsBetter, want: StatusRegression},
		{name: "same worsening value lower", current: 90, tolerance: 3, direction: dirLowerIsBetter, want: StatusImprovement},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyMetricV2(100, tt.current, tt.tolerance, tt.direction))
		})
	}
}

func TestClassifyMetricV2NegativeLowerBaselineThresholdBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		current float64
		want    ComparisonStatus
	}{
		{name: "identical", current: -10, want: StatusOK},
		{name: "upper boundary", current: -9, want: StatusOK},
		{name: "above upper boundary", current: math.Nextafter(-9, math.Inf(1)), want: StatusRegression},
		{name: "lower boundary", current: -11, want: StatusOK},
		{name: "below lower boundary", current: math.Nextafter(-11, math.Inf(-1)), want: StatusImprovement},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyMetricV2(-10, tt.current, 10, dirLowerIsBetter))
		})
	}
}

func TestCompareReleaseAcceptsIdenticalNegativeWorkingSetGrowth(t *testing.T) {
	baseline, current := validReleaseComparisonPair(t)
	baseline.Results[0].Metrics = append(baseline.Results[0].Metrics, BaselineMetricV2{
		Name: "working_set_growth_mb", Median: -10, Unit: "MiB", Direction: dirLowerIsBetter, TolerancePct: 10,
	})
	current.Results[0].Metrics = append(current.Results[0].Metrics, AggregatedMetricV2{
		Name: "working_set_growth_mb", Median: -10, Unit: "MiB", Direction: dirLowerIsBetter,
	})

	report, err := compareRelease(baseline, current)

	require.NoError(t, err)
	for _, entry := range report.Entries {
		if entry.Metric == "working_set_growth_mb" {
			require.Equal(t, StatusOK, entry.Status)
			return
		}
	}
	t.Fatal("working_set_growth_mb is missing from comparison report")
}

func TestClassifyMetricV2ZeroBaselineFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		current   float64
		direction string
		want      ComparisonStatus
	}{
		{name: "lower zero", current: 0, direction: dirLowerIsBetter, want: StatusOK},
		{name: "lower positive", current: math.SmallestNonzeroFloat64, direction: dirLowerIsBetter, want: StatusRegression},
		{name: "higher zero", current: 0, direction: dirHigherIsBetter, want: StatusOK},
		{name: "higher positive", current: 1, direction: dirHigherIsBetter, want: StatusImprovement},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyMetricV2(0, tt.current, 10, tt.direction))
		})
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		require.NotEqual(t, StatusOK, classifyMetricV2(100, value, 10, dirHigherIsBetter))
	}
}
