//go:build e2e

package load

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const loadSummaryFile = "summary.md"

func buildLoadModeSummary(root string) ([]byte, bool, error) {
	passed := true
	failure := ""
	exitCode, found, err := readLoadStageExitCode(root, filepath.Join("run-1", "go-test"))
	if err != nil {
		passed = false
		failure = fmt.Sprintf("run-1: %v", err)
	} else if !found || exitCode != 0 {
		passed = false
		failure = "run-1 did not complete successfully"
	}

	run, err := readReleaseRun(root)
	haveRun := err == nil
	if err != nil {
		passed = false
		if failure == "" {
			failure = err.Error()
		}
	} else if err := validateReleaseRun(run); err != nil {
		passed = false
		if failure == "" {
			failure = err.Error()
		}
	}

	var summary bytes.Buffer
	status := "FAIL"
	if passed {
		status = "PASS"
	}
	fmt.Fprintf(&summary, "# Load acceptance: %s\n\n", status)
	fmt.Fprintln(&summary, "- Mode: `release`")
	fmt.Fprintln(&summary, "- Policy: absolute scenario gates; no baseline comparison")
	if err == nil {
		fmt.Fprintln(&summary, "- Runs: 1/1 complete")
	} else {
		fmt.Fprintln(&summary, "- Runs: incomplete")
	}
	if failure != "" {
		fmt.Fprintf(&summary, "- Failure: %s\n", markdownCell(failure))
	}

	fmt.Fprintln(&summary)
	fmt.Fprintln(&summary, "## Stages")
	fmt.Fprintln(&summary, "| Stage | Exit code | Evidence |")
	fmt.Fprintln(&summary, "| --- | ---: | --- |")
	stageValue := "not run"
	if err != nil {
		stageValue = markdownCell(err.Error())
	} else if found {
		stageValue = strconv.Itoa(exitCode)
	}
	fmt.Fprintf(&summary, "| run-1 | %s | `run-1/go-test.log` |\n", stageValue)

	if haveRun {
		rows := append([]ScenarioResultV2(nil), run.Results...)
		slices.SortFunc(rows, func(a, b ScenarioResultV2) int {
			return cmp.Compare(a.Name, b.Name)
		})
		fmt.Fprintln(&summary)
		fmt.Fprintln(&summary, "## Scenarios")
		fmt.Fprintln(&summary, "| Scenario | Status | Duration |")
		fmt.Fprintln(&summary, "| --- | --- | ---: |")
		for _, row := range rows {
			fmt.Fprintf(&summary, "| %s | %s | %s |\n", markdownCell(row.Name), row.Status,
				row.FinishedAt.Sub(row.StartedAt).Round(time.Millisecond))
		}

		fmt.Fprintln(&summary)
		fmt.Fprintln(&summary, "## Measurements")
		fmt.Fprintln(&summary, "| Scenario | Metric | Value | Unit | Direction |")
		fmt.Fprintln(&summary, "| --- | --- | ---: | --- | --- |")
		for _, row := range rows {
			names := make([]string, 0, len(row.Metrics))
			for name := range row.Metrics {
				names = append(names, name)
			}
			slices.Sort(names)
			for _, name := range names {
				metric := row.Metrics[name]
				fmt.Fprintf(&summary, "| %s | %s | %.6g | %s | %s |\n", markdownCell(row.Name),
					markdownCell(name), metric.Value, markdownCell(metric.Unit), markdownCell(metric.Direction))
			}
		}
	}

	return summary.Bytes(), passed, nil
}

func writeLoadModeSummary(root string) (bool, error) {
	summary, accepted, err := buildLoadModeSummary(root)
	if err != nil {
		return false, fmt.Errorf("build load summary: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(root, loadSummaryFile), summary, 0o644); err != nil {
		return false, err
	}
	return accepted, nil
}

func readLoadStageExitCode(root, stage string) (int, bool, error) {
	data, err := os.ReadFile(filepath.Join(root, stage+".exit-code"))
	if os.IsNotExist(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	exitCode, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || exitCode < 0 {
		return 0, false, fmt.Errorf("invalid exit code %q", strings.TrimSpace(string(data)))
	}
	return exitCode, true, nil
}

func markdownCell(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"|", "\\|",
		"\r", "",
		"\n", "<br>",
	)
	return replacer.Replace(value)
}
