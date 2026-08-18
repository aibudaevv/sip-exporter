//go:build e2e

package load

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	loadFinalizeModeEnv         = "SIP_EXPORTER_LOAD_FINALIZE_MODE"
	loadFinalizeArtifactDirEnv  = "SIP_EXPORTER_LOAD_FINALIZE_ARTIFACT_DIR"
	loadFinalizeBaselinePathEnv = "SIP_EXPORTER_LOAD_FINALIZE_BASELINE"
)

func finalizeLoadMode(root string, mode runMode, baselinePath string) error {
	runs, err := readLoadModeRuns(root, mode)
	if err != nil {
		return err
	}
	aggregated, err := aggregateRunArtifacts(mode, runs)
	if err != nil {
		return fmt.Errorf("aggregate load runs: %w", err)
	}

	switch mode {
	case runModeCandidate:
		baseline, err := buildCandidateBaseline(aggregated, time.Now())
		if err != nil {
			return fmt.Errorf("build candidate baseline: %w", err)
		}
		if err := writeCandidateBaseline(root, baseline); err != nil {
			return fmt.Errorf("write candidate baseline: %w", err)
		}
		return nil
	case runModeRelease:
		if baselinePath == "" {
			return fmt.Errorf("release baseline path is empty")
		}
		data, err := os.ReadFile(baselinePath)
		if err != nil {
			return fmt.Errorf("read release baseline: %w", err)
		}
		baseline, err := decodeBaselineV2(data)
		if err != nil {
			return fmt.Errorf("decode release baseline: %w", err)
		}
		if _, err := compareRelease(baseline, aggregated); err != nil {
			return fmt.Errorf("compare release: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("run mode %q cannot be finalized", mode)
	}
}

func readLoadModeRuns(root string, mode runMode) ([]RunArtifactV2, error) {
	count, err := requiredRepeatCount(mode)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read load artifact directory: %w", err)
	}
	seen := make(map[int]struct{}, count)
	for _, entry := range entries {
		if len(entry.Name()) < len("run-") || entry.Name()[:len("run-")] != "run-" {
			continue
		}
		number, err := strconv.Atoi(entry.Name()[len("run-"):])
		if err != nil || entry.Name() != fmt.Sprintf("run-%d", number) ||
			number < 1 || number > count || !entry.IsDir() {
			return nil, fmt.Errorf("unexpected load run %q", entry.Name())
		}
		seen[number] = struct{}{}
	}
	runs := make([]RunArtifactV2, count)
	for i := range runs {
		number := i + 1
		if _, ok := seen[number]; !ok {
			return nil, fmt.Errorf("missing load run-%d", number)
		}
		filename := filepath.Join(root, fmt.Sprintf("run-%d", number), resultV2File)
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("read load run-%d result: %w", number, err)
		}
		run, err := decodeRunArtifactV2(data)
		if err != nil {
			return nil, fmt.Errorf("decode load run-%d result: %w", number, err)
		}
		runs[i] = run
	}
	return runs, nil
}

func TestFinalizeLoadMode(t *testing.T) {
	mode, root, baselinePath, enabled, err := loadFinalizeModeFromEnvironment()
	require.NoError(t, err)
	if !enabled {
		t.Skip("load mode finalizer is disabled")
	}
	require.Nil(t, activeRunRecorder)
	require.NoError(t, finalizeLoadMode(root, mode, baselinePath))
	require.Nil(t, activeRunRecorder)
}

func loadFinalizeModeFromEnvironment() (runMode, string, string, bool, error) {
	mode := runMode(os.Getenv(loadFinalizeModeEnv))
	root := os.Getenv(loadFinalizeArtifactDirEnv)
	baselinePath := os.Getenv(loadFinalizeBaselinePathEnv)
	if mode == "" && root == "" && baselinePath == "" {
		return "", "", "", false, nil
	}
	if mode != runModeRelease && mode != runModeCandidate {
		return "", "", "", false, fmt.Errorf("invalid load finalize mode %q", mode)
	}
	if root == "" {
		return "", "", "", false, fmt.Errorf("load finalize artifact directory is empty")
	}
	if mode == runModeRelease && baselinePath == "" {
		return "", "", "", false, fmt.Errorf("release finalize baseline path is empty")
	}
	if mode == runModeCandidate && baselinePath != "" {
		return "", "", "", false, fmt.Errorf("candidate finalize baseline path is not allowed")
	}
	return mode, root, baselinePath, true, nil
}
