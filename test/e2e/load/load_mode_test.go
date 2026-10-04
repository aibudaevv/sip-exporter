//go:build e2e

package load

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const loadSummaryArtifactDirEnv = "SIP_EXPORTER_LOAD_SUMMARY_ARTIFACT_DIR"

var releaseScenarios = [...]string{
	"TestReleaseConcurrentDialogs",
	"TestReleaseINVITEFlood",
	"TestReleaseMixedNominal",
	"TestReleaseMixedPeak",
	"TestReleaseMixedSoak",
	"TestReleaseMultiInterface",
	"TestReleaseVQMixed",
}

func validateReleaseRun(run RunArtifactV2) error {
	if run.Mode != runModeRelease {
		return fmt.Errorf("release artifact mode: got %q, want %q", run.Mode, runModeRelease)
	}
	if !run.ReleaseEligible {
		return fmt.Errorf("release artifact eligibility is false")
	}

	expected := make(map[string]struct{}, len(releaseScenarios))
	for _, name := range releaseScenarios {
		expected[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(run.Results))
	for _, result := range run.Results {
		if _, ok := expected[result.Name]; !ok {
			return fmt.Errorf("release artifact has unexpected scenario %q", result.Name)
		}
		if _, ok := seen[result.Name]; ok {
			return fmt.Errorf("release artifact has duplicate scenario %q", result.Name)
		}
		seen[result.Name] = struct{}{}
		if result.Status != scenarioStatusComplete {
			return fmt.Errorf("release scenario %q is not complete", result.Name)
		}
	}
	for _, name := range releaseScenarios {
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("release artifact is missing scenario %q", name)
		}
	}
	if err := run.Validate(); err != nil {
		return fmt.Errorf("validate release artifact: %w", err)
	}
	return nil
}

func readReleaseRun(root string) (RunArtifactV2, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return RunArtifactV2{}, fmt.Errorf("read load artifact directory: %w", err)
	}
	for _, entry := range entries {
		if len(entry.Name()) >= len("run-") && entry.Name()[:len("run-")] == "run-" &&
			(entry.Name() != "run-1" || !entry.IsDir()) {
			return RunArtifactV2{}, fmt.Errorf("unexpected load run %q", entry.Name())
		}
	}
	filename := filepath.Join(root, "run-1", resultV2File)
	data, err := os.ReadFile(filename)
	if err != nil {
		return RunArtifactV2{}, fmt.Errorf("read release result: %w", err)
	}
	run, err := decodeRunArtifactV2(data)
	if err != nil {
		return RunArtifactV2{}, fmt.Errorf("decode release result: %w", err)
	}
	return run, nil
}

func TestSummarizeLoadMode(t *testing.T) {
	root := os.Getenv(loadSummaryArtifactDirEnv)
	if root == "" {
		t.Skip("load summary is disabled")
	}
	accepted, err := writeLoadModeSummary(root)
	require.NoError(t, err)
	require.True(t, accepted, "load acceptance failed; inspect %s", filepath.Join(root, loadSummaryFile))
}
