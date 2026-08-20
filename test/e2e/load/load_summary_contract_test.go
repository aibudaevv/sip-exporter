//go:build e2e

package load

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildLoadModeSummaryCandidatePass(t *testing.T) {
	root := t.TempDir()
	runs := writeLoadModeRuns(t, root, runModeCandidate, 5)
	aggregated, err := aggregateRunArtifacts(runModeCandidate, runs)
	require.NoError(t, err)
	baseline, err := buildCandidateBaseline(aggregated, time.Now())
	require.NoError(t, err)
	require.NoError(t, writeCandidateBaseline(root, baseline))
	writeSummaryStage(t, root, "preflight", 0)
	for run := 1; run <= 5; run++ {
		writeSummaryStage(t, filepath.Join(root, "run-"+strconv.Itoa(run)), "go-test", 0)
	}
	writeSummaryStage(t, root, "finalize", 0)

	summary, err := buildLoadModeSummary(root, runModeCandidate, "")
	require.NoError(t, err)
	require.Contains(t, string(summary), "# Load acceptance: PASS")
	require.Contains(t, string(summary), "Runs: 5/5 complete")
	require.Contains(t, string(summary), "baseline-candidate.json")
	require.Contains(t, string(summary), "## Candidate baseline")
	require.Contains(t, string(summary), "actual_cps")
}

func TestBuildLoadModeSummaryReportsMalformedResult(t *testing.T) {
	root := t.TempDir()
	writeSummaryStage(t, root, "preflight", 0)
	writeSummaryStage(t, filepath.Join(root, "run-1"), "go-test", 1)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "run-1"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "run-1", resultV2File), []byte("{"), 0o644))

	summary, err := buildLoadModeSummary(root, runModeRelease, "")
	require.NoError(t, err)
	require.Contains(t, string(summary), "# Load acceptance: FAIL")
	require.Contains(t, string(summary), "run-1/result.json")
	require.Contains(t, string(summary), "decode")
	require.NotContains(t, string(summary), "run-2")
}

func TestBuildLoadModeSummaryReleasePassIncludesComparison(t *testing.T) {
	root := t.TempDir()
	runs := writeLoadModeRuns(t, root, runModeRelease, 1)
	aggregated, err := aggregateRunArtifacts(runModeRelease, runs)
	require.NoError(t, err)
	baselinePath := filepath.Join(root, "accepted-baseline.json")
	writeLoadModeBaseline(t, baselinePath, acceptedBaselineForRelease(aggregated))
	writeSummaryStage(t, root, "preflight", 0)
	writeSummaryStage(t, filepath.Join(root, "run-1"), "go-test", 0)
	writeSummaryStage(t, root, "finalize", 0)

	summary, err := buildLoadModeSummary(root, runModeRelease, baselinePath)
	require.NoError(t, err)
	require.Contains(t, string(summary), "# Load acceptance: PASS")
	require.Contains(t, string(summary), "## Comparison")
	require.Contains(t, string(summary), "| OK |")
}

func TestBuildLoadModeSummaryReleaseRegressionIncludesComparison(t *testing.T) {
	root := t.TempDir()
	runs := writeLoadModeRuns(t, root, runModeRelease, 1)
	baselineAggregate, err := aggregateRunArtifacts(runModeRelease, runs)
	require.NoError(t, err)
	baselinePath := filepath.Join(root, "accepted-baseline.json")
	writeLoadModeBaseline(t, baselinePath, acceptedBaselineForRelease(baselineAggregate))
	for i := range runs {
		runs[i].Results[0].Metrics["actual_cps"] = MetricEntry{Value: 1, Unit: "cps", Direction: dirHigherIsBetter}
		data, err := json.Marshal(runs[i])
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, "run-"+strconv.Itoa(i+1), resultV2File), data, 0o644))
	}
	writeSummaryStage(t, root, "preflight", 0)
	writeSummaryStage(t, filepath.Join(root, "run-1"), "go-test", 0)
	writeSummaryStage(t, root, "finalize", 1)

	summary, err := buildLoadModeSummary(root, runModeRelease, baselinePath)
	require.NoError(t, err)
	require.Contains(t, string(summary), "# Load acceptance: FAIL")
	require.Contains(t, string(summary), "## Comparison")
	require.Contains(t, string(summary), "REGRESSION")
}

func TestBuildLoadModeSummaryEscapesFailureCells(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "preflight.exit-code"), []byte("<broken>\n"), 0o644))

	summary, err := buildLoadModeSummary(root, runModeRelease, "")
	require.NoError(t, err)
	require.NotContains(t, string(summary), "<broken>")
	require.Contains(t, string(summary), "&lt;broken&gt;")
}

func writeSummaryStage(t *testing.T, root, stage string, exitCode int) {
	t.Helper()
	filename := filepath.Join(root, stage+".exit-code")
	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0o755))
	require.NoError(t, os.WriteFile(filename, []byte(strconv.Itoa(exitCode)+"\n"), 0o644))
}
