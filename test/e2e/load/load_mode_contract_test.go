//go:build e2e

package load

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadFinalizeModeFromEnvironment(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		root        string
		baseline    string
		wantEnabled bool
		wantMode    runMode
		wantErr     bool
	}{
		{name: "unset"},
		{name: "release", mode: "release", root: "/tmp/release", baseline: "/tmp/baseline.json", wantEnabled: true, wantMode: runModeRelease},
		{name: "candidate", mode: "candidate", root: "/tmp/candidate", wantEnabled: true, wantMode: runModeCandidate},
		{name: "missing root", mode: "release", baseline: "/tmp/baseline.json", wantErr: true},
		{name: "missing release baseline", mode: "release", root: "/tmp/release", wantErr: true},
		{name: "candidate baseline", mode: "candidate", root: "/tmp/candidate", baseline: "/tmp/baseline.json", wantErr: true},
		{name: "targeted mode", mode: "targeted", root: "/tmp/targeted", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(loadFinalizeModeEnv, tt.mode)
			t.Setenv(loadFinalizeArtifactDirEnv, tt.root)
			t.Setenv(loadFinalizeBaselinePathEnv, tt.baseline)

			mode, root, baseline, enabled, err := loadFinalizeModeFromEnvironment()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantEnabled, enabled)
			require.Equal(t, tt.wantMode, mode)
			require.Equal(t, tt.root, root)
			require.Equal(t, tt.baseline, baseline)
		})
	}
}

func writeLoadModeRuns(t *testing.T, root string, mode runMode, count int) []RunArtifactV2 {
	t.Helper()
	runs := runArtifactsForAggregation(mode, count)
	for i, run := range runs {
		data, err := json.Marshal(run)
		require.NoError(t, err)
		dir := filepath.Join(root, fmt.Sprintf("run-%d", i+1))
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, resultV2File), data, 0o644))
	}
	return runs
}

func writeLoadModeBaseline(t *testing.T, filename string, baseline BaselineV2) []byte {
	t.Helper()
	data, err := json.Marshal(baseline)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filename, data, 0o644))
	return data
}

func TestFinalizeLoadModeRequiresExactRunInventory(t *testing.T) {
	tests := []struct {
		name    string
		mode    runMode
		count   int
		wantErr bool
	}{
		{name: "release missing run", mode: runModeRelease, count: 2, wantErr: true},
		{name: "release exact runs", mode: runModeRelease, count: 3},
		{name: "release extra run", mode: runModeRelease, count: 4, wantErr: true},
		{name: "candidate missing run", mode: runModeCandidate, count: 4, wantErr: true},
		{name: "candidate exact runs", mode: runModeCandidate, count: 5},
		{name: "candidate extra run", mode: runModeCandidate, count: 6, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeLoadModeRuns(t, root, tt.mode, tt.count)
			baselinePath := ""
			if tt.mode == runModeRelease {
				aggregated, err := aggregateRunArtifacts(runModeRelease,
					runArtifactsForAggregation(runModeRelease, 3))
				require.NoError(t, err)
				baselinePath = filepath.Join(root, "baseline-v2.json")
				writeLoadModeBaseline(t, baselinePath, acceptedBaselineForRelease(aggregated))
			}

			err := finalizeLoadMode(root, tt.mode, baselinePath)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestFinalizeLoadModeRejectsAliasedRunDirectory(t *testing.T) {
	root := t.TempDir()
	runs := writeLoadModeRuns(t, root, runModeRelease, 3)
	data, err := json.Marshal(runs[0])
	require.NoError(t, err)
	aliasDir := filepath.Join(root, "run-01")
	require.NoError(t, os.Mkdir(aliasDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(aliasDir, resultV2File), data, 0o644))
	aggregated, err := aggregateRunArtifacts(runModeRelease, runs)
	require.NoError(t, err)
	baselinePath := filepath.Join(root, "baseline-v2.json")
	writeLoadModeBaseline(t, baselinePath, acceptedBaselineForRelease(aggregated))

	require.Error(t, finalizeLoadMode(root, runModeRelease, baselinePath))
}

func TestFinalizeLoadModeRejectsInvalidRunArtifacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{name: "missing result", mutate: func(t *testing.T, root string) {
			require.NoError(t, os.Remove(filepath.Join(root, "run-5", resultV2File)))
		}},
		{name: "corrupt result", mutate: func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, "run-3", resultV2File), []byte("{"), 0o644))
		}},
		{name: "wrong result mode", mutate: func(t *testing.T, root string) {
			path := filepath.Join(root, "run-2", resultV2File)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			run, err := decodeRunArtifactV2(data)
			require.NoError(t, err)
			run.Mode = runModeRelease
			run.ReleaseEligible = true
			data, err = json.Marshal(run)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, data, 0o644))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeLoadModeRuns(t, root, runModeCandidate, 5)
			tt.mutate(t, root)

			require.Error(t, finalizeLoadMode(root, runModeCandidate, ""))
		})
	}
}

func TestFinalizeLoadModeWritesOnlyCandidateBaseline(t *testing.T) {
	root := t.TempDir()
	writeLoadModeRuns(t, root, runModeCandidate, 5)
	acceptedPath := filepath.Join(root, "baseline-v2.json")
	accepted := []byte("owner-approved")
	require.NoError(t, os.WriteFile(acceptedPath, accepted, 0o644))

	require.NoError(t, finalizeLoadMode(root, runModeCandidate, ""))
	candidate, err := os.ReadFile(filepath.Join(root, candidateBaselineFile))
	require.NoError(t, err)
	decoded, err := decodeBaselineV2(candidate)
	require.NoError(t, err)
	require.Equal(t, baselineKindCandidate, decoded.Kind)
	actualAccepted, err := os.ReadFile(acceptedPath)
	require.NoError(t, err)
	require.Equal(t, accepted, actualAccepted)
}

func TestFinalizeLoadModeRejectsMissingOrInvalidReleaseBaseline(t *testing.T) {
	tests := []struct {
		name  string
		write func(t *testing.T, filename string)
	}{
		{name: "missing", write: func(_ *testing.T, _ string) {}},
		{name: "invalid", write: func(t *testing.T, filename string) {
			require.NoError(t, os.WriteFile(filename, []byte("{"), 0o644))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeLoadModeRuns(t, root, runModeRelease, 3)
			baselinePath := filepath.Join(root, "baseline-v2.json")
			tt.write(t, baselinePath)

			require.Error(t, finalizeLoadMode(root, runModeRelease, baselinePath))
		})
	}
}

func TestFinalizeLoadModeFailsReleaseRegressionWithoutChangingBaseline(t *testing.T) {
	root := t.TempDir()
	runs := writeLoadModeRuns(t, root, runModeRelease, 3)
	aggregated, err := aggregateRunArtifacts(runModeRelease, runs)
	require.NoError(t, err)
	baseline := acceptedBaselineForRelease(aggregated)
	mutated := false
	for i := range baseline.Results[0].Metrics {
		if baseline.Results[0].Metrics[i].Name == "actual_cps" {
			baseline.Results[0].Metrics[i].Median = 1_000
			mutated = true
		}
	}
	require.True(t, mutated)
	baselinePath := filepath.Join(root, "baseline-v2.json")
	before := writeLoadModeBaseline(t, baselinePath, baseline)

	require.Error(t, finalizeLoadMode(root, runModeRelease, baselinePath))
	after, err := os.ReadFile(baselinePath)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
