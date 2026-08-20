//go:build e2e

package load

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	dirHigherIsBetter = "higher_is_better"
	dirLowerIsBetter  = "lower_is_better"
)

type (
	MetricEntry struct {
		Value     float64 `json:"value"`
		Unit      string  `json:"unit"`
		Direction string  `json:"direction"`
	}

	ComparisonStatus string
)

const (
	StatusOK          ComparisonStatus = "OK"
	StatusRegression  ComparisonStatus = "REGRESSION"
	StatusImprovement ComparisonStatus = "IMPROVEMENT"
	StatusNew         ComparisonStatus = "NEW"
)

var activeRunRecorder *runRecorderV2

func beginScenario(t *testing.T) {
	t.Helper()
	if activeRunRecorder == nil {
		return
	}
	if err := activeRunRecorder.Begin(t.Name(), time.Now()); err != nil {
		t.Fatalf("begin load result row: %v", err)
	}
	t.Cleanup(func() {
		activeRunRecorder.Finalize(t.Name(), t.Failed(), time.Now())
	})
}

func recordResult(t *testing.T, metrics map[string]MetricEntry) {
	t.Helper()
	if activeRunRecorder != nil {
		if err := activeRunRecorder.Complete(t.Name(), metrics, time.Now()); err != nil {
			t.Fatalf("complete load result row: %v", err)
		}
	}
}

func getGitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func TestMain(m *testing.M) {
	if err := configureActiveRunRecorder(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: configure result recorder: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	if activeRunRecorder != nil {
		if err := activeRunRecorder.Save(time.Now()); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: save result v2 artifact: %v\n", err)
			if code == 0 {
				code = 1
			}
		} else {
			fmt.Fprintf(os.Stderr, "Result v2 artifact saved to %s\n",
				filepath.Join(os.Getenv(loadArtifactDirEnv), resultV2File))
		}
	}
	os.Exit(code)
}

func configureActiveRunRecorder() error {
	mode, err := loadRunModeFromEnvironment()
	if err != nil || mode == "" {
		return err
	}
	root := os.Getenv(loadArtifactDirEnv)
	if root == "" {
		return fmt.Errorf("%s is required when %s is set", loadArtifactDirEnv, loadModeEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fingerprint, err := collectEnvironmentFingerprint(ctx)
	if err != nil {
		return err
	}
	commit := getGitCommit()
	if commit == "" {
		return fmt.Errorf("read git commit")
	}
	activeRunRecorder, err = newRunRecorderV2(mode, root, fingerprint, commit, time.Now())
	return err
}
