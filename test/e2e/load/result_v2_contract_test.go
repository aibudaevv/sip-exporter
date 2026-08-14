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

func validRunArtifactV2() RunArtifactV2 {
	started := time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC)
	return RunArtifactV2{
		Version:         2,
		Mode:            runModeTargeted,
		ReleaseEligible: false,
		StartedAt:       started,
		FinishedAt:      started.Add(time.Minute),
		Commit:          "a104fd2",
		Environment: EnvironmentFingerprint{
			OS: "linux", Arch: "amd64", GoVersion: "go1.26.6",
			KernelVersion: "6.1.0", DockerVersion: "29.5.3",
			ExporterImage: "sip-exporter:s26-2-1-contract-20260814-1",
		},
		Results: []ScenarioResultV2{{
			Name:       "TestLoadINVITEFlood/rate_100",
			Status:     scenarioStatusComplete,
			StartedAt:  started,
			FinishedAt: started.Add(30 * time.Second),
			Metrics: map[string]MetricEntry{
				"actual_cps": {Value: 100, Unit: "cps", Direction: dirHigherIsBetter},
			},
		}},
	}
}

func TestRunArtifactV2RejectsInvalidSchema(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RunArtifactV2)
	}{
		{name: "unknown version", mutate: func(run *RunArtifactV2) { run.Version = 3 }},
		{name: "missing mode", mutate: func(run *RunArtifactV2) { run.Mode = "" }},
		{name: "missing run start", mutate: func(run *RunArtifactV2) { run.StartedAt = time.Time{} }},
		{name: "missing run finish", mutate: func(run *RunArtifactV2) { run.FinishedAt = time.Time{} }},
		{name: "missing commit", mutate: func(run *RunArtifactV2) { run.Commit = "" }},
		{name: "missing OS", mutate: func(run *RunArtifactV2) { run.Environment.OS = "" }},
		{name: "missing arch", mutate: func(run *RunArtifactV2) { run.Environment.Arch = "" }},
		{name: "missing Go version", mutate: func(run *RunArtifactV2) { run.Environment.GoVersion = "" }},
		{name: "missing kernel version", mutate: func(run *RunArtifactV2) {
			run.Environment.KernelVersion = ""
		}},
		{name: "missing Docker version", mutate: func(run *RunArtifactV2) {
			run.Environment.DockerVersion = ""
		}},
		{name: "missing exporter image", mutate: func(run *RunArtifactV2) { run.Environment.ExporterImage = "" }},
		{name: "empty run", mutate: func(run *RunArtifactV2) { run.Results = nil }},
		{name: "missing scenario name", mutate: func(run *RunArtifactV2) { run.Results[0].Name = "" }},
		{name: "incomplete row", mutate: func(run *RunArtifactV2) {
			run.Results[0].Status = scenarioStatusIncomplete
		}},
		{name: "missing scenario finish", mutate: func(run *RunArtifactV2) {
			run.Results[0].FinishedAt = time.Time{}
		}},
		{name: "missing metrics", mutate: func(run *RunArtifactV2) { run.Results[0].Metrics = nil }},
		{name: "missing metric unit", mutate: func(run *RunArtifactV2) {
			run.Results[0].Metrics["actual_cps"] = MetricEntry{Value: 100, Direction: dirHigherIsBetter}
		}},
		{name: "missing metric direction", mutate: func(run *RunArtifactV2) {
			run.Results[0].Metrics["actual_cps"] = MetricEntry{Value: 100, Unit: "cps"}
		}},
		{name: "duplicate row", mutate: func(run *RunArtifactV2) {
			run.Results = append(run.Results, run.Results[0])
		}},
		{name: "NaN metric", mutate: func(run *RunArtifactV2) {
			run.Results[0].Metrics["actual_cps"] = MetricEntry{Value: math.NaN(), Unit: "cps"}
		}},
		{name: "infinite metric", mutate: func(run *RunArtifactV2) {
			run.Results[0].Metrics["actual_cps"] = MetricEntry{Value: math.Inf(1), Unit: "cps"}
		}},
		{name: "NaN generator rate", mutate: func(run *RunArtifactV2) {
			run.Results[0].Generator = &GeneratorResult{ActualRate: math.NaN()}
		}},
		{name: "infinite capture", mutate: func(run *RunArtifactV2) {
			run.Results[0].Capture = &CaptureResult{Expected: math.Inf(1)}
		}},
		{name: "NaN protocol counter", mutate: func(run *RunArtifactV2) {
			run.Results[0].Protocols = &ProtocolCounters{SIPPackets: math.NaN()}
		}},
		{name: "infinite resource summary", mutate: func(run *RunArtifactV2) {
			run.Results[0].Resources = &ResourceSummaryV2{CPUAvg: math.Inf(1)}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := validRunArtifactV2()
			tt.mutate(&run)

			require.Error(t, run.Validate())
		})
	}
}

func TestRunArtifactV2AcceptsCompleteTargetedRun(t *testing.T) {
	require.NoError(t, validRunArtifactV2().Validate())
}

func TestDecodeRunArtifactV2RejectsUnknownVersion(t *testing.T) {
	_, err := decodeRunArtifactV2([]byte(`{"version":99}`))
	require.Error(t, err)
}

func TestDecodeRunArtifactV2RejectsMissingReleaseEligible(t *testing.T) {
	run := validRunArtifactV2()
	data, err := json.Marshal(run)
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))
	delete(fields, "release_eligible")
	data, err = json.Marshal(fields)
	require.NoError(t, err)

	_, err = decodeRunArtifactV2(data)
	require.Error(t, err)
}

func TestDecodeRunArtifactV2RejectsNullReleaseEligible(t *testing.T) {
	run := validRunArtifactV2()
	data, err := json.Marshal(run)
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))
	fields["release_eligible"] = json.RawMessage("null")
	data, err = json.Marshal(fields)
	require.NoError(t, err)

	_, err = decodeRunArtifactV2(data)
	require.Error(t, err)
}

func TestRunRecorderV2DisabledWithoutMode(t *testing.T) {
	recorder, err := newRunRecorderV2("", t.TempDir(), validRunArtifactV2().Environment,
		"a104fd2", time.Now())

	require.NoError(t, err)
	require.Nil(t, recorder)
}

func TestRunRecorderV2TargetedIsNotReleaseEligible(t *testing.T) {
	started := time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC)
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "a104fd2", started)
	require.NoError(t, err)

	run := recorder.Snapshot()
	require.Equal(t, runModeTargeted, run.Mode)
	require.False(t, run.ReleaseEligible)
}

func TestRunRecorderV2RejectsDuplicateScenarioBegin(t *testing.T) {
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "a104fd2", time.Now())
	require.NoError(t, err)
	require.NoError(t, recorder.Begin("same", time.Now()))

	require.Error(t, recorder.Begin("same", time.Now()))
}

func TestRunRecorderV2PreservesIncompleteScenario(t *testing.T) {
	started := time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC)
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "a104fd2", started)
	require.NoError(t, err)
	require.NoError(t, recorder.Begin("failed-scenario", started.Add(time.Second)))

	recorder.Finalize("failed-scenario", true, started.Add(2*time.Second))

	run := recorder.Snapshot()
	require.Len(t, run.Results, 1)
	require.Equal(t, "failed-scenario", run.Results[0].Name)
	require.Equal(t, scenarioStatusIncomplete, run.Results[0].Status)
	require.NotEmpty(t, run.Results[0].Failure)
}

func TestRunRecorderV2CompletesScenarioWithOwnedMetrics(t *testing.T) {
	started := time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC)
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "a104fd2", started)
	require.NoError(t, err)
	require.NoError(t, recorder.Begin("complete-scenario", started.Add(time.Second)))
	metrics := map[string]MetricEntry{
		"actual_cps": {Value: 100, Unit: "cps", Direction: dirHigherIsBetter},
	}

	require.NoError(t, recorder.Complete("complete-scenario", metrics, started.Add(2*time.Second)))
	metrics["actual_cps"] = MetricEntry{Value: 1, Unit: "cps", Direction: dirHigherIsBetter}

	run := recorder.Snapshot()
	require.Equal(t, scenarioStatusComplete, run.Results[0].Status)
	require.Equal(t, 100.0, run.Results[0].Metrics["actual_cps"].Value)
}

func TestRunRecorderV2PreservesNonFiniteResultForDiagnosis(t *testing.T) {
	root := t.TempDir()
	started := time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC)
	recorder, err := newRunRecorderV2(runModeTargeted, root,
		validRunArtifactV2().Environment, "a104fd2", started)
	require.NoError(t, err)
	require.NoError(t, recorder.Begin("non-finite", started))

	err = recorder.Complete("non-finite", map[string]MetricEntry{
		"actual_cps": {Value: math.NaN(), Unit: "cps", Direction: dirHigherIsBetter},
	}, started.Add(time.Second))
	require.Error(t, err)
	recorder.Finalize("non-finite", true, started.Add(2*time.Second))
	require.Error(t, recorder.Save(started.Add(3*time.Second)))

	data, readErr := os.ReadFile(filepath.Join(root, resultV2File))
	require.NoError(t, readErr)
	var saved RunArtifactV2
	require.NoError(t, json.Unmarshal(data, &saved))
	require.Equal(t, scenarioStatusIncomplete, saved.Results[0].Status)
	require.NotEmpty(t, saved.Results[0].Failure)
}

func TestRunRecorderV2PreservesNonFiniteEvidenceForDiagnosis(t *testing.T) {
	root := t.TempDir()
	started := time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC)
	recorder, err := newRunRecorderV2(runModeTargeted, root,
		validRunArtifactV2().Environment, "a104fd2", started)
	require.NoError(t, err)
	require.NoError(t, recorder.Begin("non-finite-evidence", started))

	err = recorder.AttachLoadResult("non-finite-evidence", loadResult{
		Capture: CaptureResult{Captured: math.Inf(1)},
	})
	require.Error(t, err)
	recorder.Finalize("non-finite-evidence", true, started.Add(time.Second))
	require.Error(t, recorder.Save(started.Add(2*time.Second)))

	data, readErr := os.ReadFile(filepath.Join(root, resultV2File))
	require.NoError(t, readErr)
	var saved RunArtifactV2
	require.NoError(t, json.Unmarshal(data, &saved))
	require.Equal(t, scenarioStatusIncomplete, saved.Results[0].Status)
}

func TestRunRecorderV2AggregatesGeneratorRateOverMeasureInterval(t *testing.T) {
	started := time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		secondFrom time.Duration
		wantRate   float64
	}{
		{name: "parallel", secondFrom: 0, wantRate: 20},
		{name: "sequential", secondFrom: time.Second, wantRate: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
				validRunArtifactV2().Environment, "a104fd2", started)
			require.NoError(t, err)
			require.NoError(t, recorder.Begin("scenario", started))
			firstPhases := phaseInterval(started, started.Add(time.Second))
			secondStart := started.Add(tt.secondFrom)
			secondPhases := phaseInterval(secondStart, secondStart.Add(time.Second))

			require.NoError(t, recorder.AttachGenerator("scenario", GeneratorResult{
				ExitCode: 0, SuccessfulCalls: 10, ActualRate: 10, Phases: firstPhases,
			}))
			require.NoError(t, recorder.AttachGenerator("scenario", GeneratorResult{
				ExitCode: 0, SuccessfulCalls: 10, ActualRate: 10, Phases: secondPhases,
			}))

			generator := recorder.Snapshot().Results[0].Generator
			require.NotNil(t, generator)
			require.Equal(t, 20, generator.SuccessfulCalls)
			require.Equal(t, tt.wantRate, generator.ActualRate)
		})
	}
}

func phaseInterval(started, finished time.Time) PhaseTimestamps {
	return PhaseTimestamps{
		WarmupStart: started, Ready: started, MeasureStart: started,
		MeasureEnd: finished, DrainEnd: finished,
	}
}

func TestRunRecorderV2FailedCleanupOverridesCompletedStatus(t *testing.T) {
	started := time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC)
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "a104fd2", started)
	require.NoError(t, err)
	require.NoError(t, recorder.Begin("failed-after-result", started))
	require.NoError(t, recorder.Complete("failed-after-result", map[string]MetricEntry{
		"actual_cps": {Value: 100, Unit: "cps", Direction: dirHigherIsBetter},
	}, started.Add(time.Second)))

	recorder.Finalize("failed-after-result", true, started.Add(2*time.Second))

	run := recorder.Snapshot()
	require.Equal(t, scenarioStatusFailed, run.Results[0].Status)
	require.NotEmpty(t, run.Results[0].Failure)
	require.Error(t, run.Validate())
}

func TestRunRecorderV2AttachesTypedLoadEvidence(t *testing.T) {
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "a104fd2", time.Now())
	require.NoError(t, err)
	require.NoError(t, recorder.Begin("scenario", time.Now()))
	evidence := loadResult{
		Generator: GeneratorResult{SuccessfulCalls: 10, ActualRate: 100},
		Capture:   CaptureResult{Expected: 70, Captured: 70},
		Protocols: ProtocolCounters{SIPPackets: 70, RTPPackets: 400},
		CPUAvg:    25,
		CPUPeak:   40,
		MemMaxMB:  64,
	}

	require.NoError(t, recorder.AttachLoadResult("scenario", evidence))

	row := recorder.Snapshot().Results[0]
	require.Equal(t, evidence.Generator, *row.Generator)
	require.Equal(t, evidence.Capture, *row.Capture)
	require.Equal(t, evidence.Protocols, *row.Protocols)
	require.Equal(t, ResourceSummaryV2{CPUAvg: 25, CPUPeak: 40, MemMaxMB: 64}, *row.Resources)
}

func TestRunRecorderV2EmptyRunFailsWithoutFabricatedRows(t *testing.T) {
	root := t.TempDir()
	started := time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC)
	recorder, err := newRunRecorderV2(runModeTargeted, root,
		validRunArtifactV2().Environment, "a104fd2", started)
	require.NoError(t, err)

	err = recorder.Save(started.Add(time.Second))

	require.Error(t, err)
	data, readErr := os.ReadFile(filepath.Join(root, resultV2File))
	require.NoError(t, readErr)
	var saved RunArtifactV2
	require.NoError(t, json.Unmarshal(data, &saved))
	require.Empty(t, saved.Results)
}

func TestWriteFileAtomicReplacesTargetWithoutTemporaryFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "result.json")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o644))

	require.NoError(t, writeFileAtomic(target, []byte("new"), 0o640))

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, []byte("new"), data)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "result.json", entries[0].Name())
	info, err := entries[0].Info()
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func TestRunRecorderV2RecordsRelativeArtifactPath(t *testing.T) {
	root := t.TempDir()
	started := time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC)
	recorder, err := newRunRecorderV2(runModeTargeted, root,
		validRunArtifactV2().Environment, "a104fd2", started)
	require.NoError(t, err)
	require.NoError(t, recorder.Begin("TestLoad/row", started))

	require.NoError(t, recorder.RecordArtifact("TestLoad/row", "metrics-before.prom", []byte("metric 1\n")))

	run := recorder.Snapshot()
	require.Equal(t, []string{"scenarios/000/metrics-before.prom"}, run.Results[0].Artifacts)
	require.False(t, filepath.IsAbs(run.Results[0].Artifacts[0]))
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(run.Results[0].Artifacts[0])))
	require.NoError(t, err)
	require.Equal(t, []byte("metric 1\n"), data)
}

func TestRunRecorderV2RejectsArtifactPathTraversal(t *testing.T) {
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "a104fd2", time.Now())
	require.NoError(t, err)
	require.NoError(t, recorder.Begin("scenario", time.Now()))

	require.Error(t, recorder.RecordArtifact("scenario", "../escape", []byte("bad")))
}

func TestBeginScenarioV2CleanupPreservesIncompleteRow(t *testing.T) {
	started := time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC)
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "a104fd2", started)
	require.NoError(t, err)
	previous := activeRunRecorder
	activeRunRecorder = recorder
	t.Cleanup(func() { activeRunRecorder = previous })

	t.Run("row", func(t *testing.T) {
		beginScenario(t)
	})

	run := recorder.Snapshot()
	require.Len(t, run.Results, 1)
	require.Equal(t, "TestBeginScenarioV2CleanupPreservesIncompleteRow/row", run.Results[0].Name)
	require.Equal(t, scenarioStatusIncomplete, run.Results[0].Status)
	require.NotEmpty(t, run.Results[0].Failure)
}

func TestRecordResultV2CompletesStartedRow(t *testing.T) {
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "a104fd2", time.Now())
	require.NoError(t, err)
	previous := activeRunRecorder
	activeRunRecorder = recorder
	t.Cleanup(func() { activeRunRecorder = previous })

	t.Run("row", func(t *testing.T) {
		beginScenario(t)
		recordResult(t, map[string]MetricEntry{
			"actual_cps": {Value: 100, Unit: "cps", Direction: dirHigherIsBetter},
		})
	})

	run := recorder.Snapshot()
	require.Len(t, run.Results, 1)
	require.Equal(t, scenarioStatusComplete, run.Results[0].Status)
}

func TestLoadRunModeFromEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    runMode
		wantErr bool
	}{
		{name: "unset"},
		{name: "targeted", value: "targeted", want: runModeTargeted},
		{name: "release", value: "release", want: runModeRelease},
		{name: "candidate", value: "candidate", want: runModeCandidate},
		{name: "unknown", value: "smoke", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(loadModeEnv, tt.value)
			mode, err := loadRunModeFromEnvironment()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, mode)
		})
	}
}

func TestCollectEnvironmentFingerprintUsesRealEnvironment(t *testing.T) {
	t.Setenv("SIP_EXPORTER_E2E_IMAGE", "sip-exporter:s26-2-1-contract-20260814-1")

	fingerprint, err := collectEnvironmentFingerprint(t.Context())

	require.NoError(t, err)
	require.NotEmpty(t, fingerprint.OS)
	require.NotEmpty(t, fingerprint.Arch)
	require.NotEmpty(t, fingerprint.GoVersion)
	require.NotEmpty(t, fingerprint.KernelVersion)
	require.NotEmpty(t, fingerprint.DockerVersion)
	require.Equal(t, "sip-exporter:s26-2-1-contract-20260814-1", fingerprint.ExporterImage)
}
