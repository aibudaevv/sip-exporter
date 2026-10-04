//go:build e2e

package load

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildLoadModeSummaryReleaseOnlyPass(t *testing.T) {
	root := t.TempDir()
	writeReleaseResult(t, root, validReleaseRunForReport())
	writeSummaryStage(t, filepath.Join(root, "run-1"), "go-test", 0)

	summary, accepted, err := buildLoadModeSummary(root)

	require.NoError(t, err)
	require.True(t, accepted)
	require.Contains(t, string(summary), "# Load acceptance: PASS")
	require.Contains(t, string(summary), "absolute scenario gates; no baseline comparison")
	require.Contains(t, string(summary), "## Scenarios")
	require.Contains(t, string(summary), "## Measurements")
	require.NotContains(t, string(summary), "Comparison")
	require.NotContains(t, string(summary), "Accepted baseline")
	require.NotContains(t, string(summary), "Candidate baseline")
}

func TestBuildLoadModeSummaryReleaseOnlyRejectsMalformedArtifact(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "run-1"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "run-1", resultV2File), []byte("{"), 0o644))
	writeSummaryStage(t, filepath.Join(root, "run-1"), "go-test", 0)

	summary, accepted, err := buildLoadModeSummary(root)

	require.NoError(t, err)
	require.False(t, accepted)
	require.Contains(t, string(summary), "# Load acceptance: FAIL")
	require.Contains(t, string(summary), "decode")
}

func TestBuildLoadModeSummaryReleaseOnlyRejectsFailedWorkload(t *testing.T) {
	root := t.TempDir()
	writeReleaseResult(t, root, validReleaseRunForReport())
	writeSummaryStage(t, filepath.Join(root, "run-1"), "go-test", 1)

	summary, accepted, err := buildLoadModeSummary(root)

	require.NoError(t, err)
	require.False(t, accepted)
	require.Contains(t, string(summary), "run-1 did not complete successfully")
}

func TestBuildLoadModeSummaryReleaseOnlyRejectsExtraRunDirectory(t *testing.T) {
	root := t.TempDir()
	writeReleaseResult(t, root, validReleaseRunForReport())
	writeSummaryStage(t, filepath.Join(root, "run-1"), "go-test", 0)
	require.NoError(t, os.Mkdir(filepath.Join(root, "run-2"), 0o755))

	summary, accepted, err := buildLoadModeSummary(root)

	require.NoError(t, err)
	require.False(t, accepted)
	require.Contains(t, string(summary), "unexpected load run")
}

func writeReleaseResult(t *testing.T, root string, run RunArtifactV2) {
	t.Helper()
	data, err := json.Marshal(run)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "run-1"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "run-1", resultV2File), data, 0o644))
}

func writeSummaryStage(t *testing.T, root, stage string, exitCode int) {
	t.Helper()
	filename := filepath.Join(root, stage+".exit-code")
	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0o755))
	require.NoError(t, os.WriteFile(filename, []byte(strconv.Itoa(exitCode)+"\n"), 0o644))
}
