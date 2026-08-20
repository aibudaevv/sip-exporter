//go:build e2e

package load

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const loadSummaryFile = "summary.md"

type loadSummaryStage struct {
	name string
	path string
}

type loadRunEvidence struct {
	Number          int
	ResultPath      string
	Artifact        *RunArtifactV2
	ReadFailure     string
	DecodeFailure   string
	ValidateFailure string
}

func buildLoadModeSummary(root string, mode runMode, baselinePath string) ([]byte, error) {
	repeatCount, err := requiredRepeatCount(mode)
	if err != nil {
		return nil, err
	}

	stages := loadSummaryStages(repeatCount)
	passed := true
	var failure string
	for _, stage := range stages {
		exitCode, found, err := readLoadStageExitCode(root, stage.path)
		if err != nil {
			passed = false
			failure = fmt.Sprintf("%s: %v", stage.name, err)
			continue
		}
		if !found || exitCode != 0 {
			passed = false
			failure = fmt.Sprintf("%s did not complete successfully", stage.name)
		}
	}

	runs, err := readLoadModeRuns(root, mode)
	var report *ComparisonReportV2
	var candidateBaseline *BaselineV2
	if err != nil {
		passed = false
		failure = err.Error()
	}
	if err == nil {
		if _, err := aggregateRunArtifacts(mode, runs); err != nil {
			passed = false
			failure = err.Error()
		}
	}
	evidence, evidenceErr := inspectLoadRunEvidence(root, mode)
	if evidenceErr != nil {
		return nil, evidenceErr
	}
	if mode == runModeCandidate {
		data, err := os.ReadFile(filepath.Join(root, candidateBaselineFile))
		if err != nil {
			passed = false
			failure = fmt.Sprintf("read candidate baseline: %v", err)
		} else {
			baseline, err := decodeBaselineV2(data)
			if err != nil || baseline.Kind != baselineKindCandidate {
				passed = false
				failure = "candidate baseline is invalid"
			} else {
				candidateBaseline = &baseline
			}
		}
	} else if err == nil {
		data, baselineErr := os.ReadFile(baselinePath)
		if baselineErr != nil {
			passed = false
			failure = fmt.Sprintf("read accepted baseline: %v", baselineErr)
		} else {
			baseline, baselineErr := decodeBaselineV2(data)
			if baselineErr != nil {
				passed = false
				failure = fmt.Sprintf("decode accepted baseline: %v", baselineErr)
			} else {
				aggregated, aggregateErr := aggregateRunArtifacts(mode, runs)
				if aggregateErr != nil {
					passed = false
					failure = aggregateErr.Error()
				} else {
					compared, compareErr := compareRelease(baseline, aggregated)
					report = &compared
					if compareErr != nil {
						passed = false
						failure = compareErr.Error()
					}
				}
			}
		}
	}

	var summary bytes.Buffer
	status := "FAIL"
	if passed {
		status = "PASS"
	}
	fmt.Fprintf(&summary, "# Load acceptance: %s\n\n", status)
	fmt.Fprintf(&summary, "- Mode: `%s`\n", mode)
	if err == nil {
		fmt.Fprintf(&summary, "- Runs: %d/%d complete\n", len(runs), repeatCount)
	} else {
		fmt.Fprintf(&summary, "- Runs: incomplete\n")
	}
	if mode == runModeCandidate {
		fmt.Fprintf(&summary, "- Candidate baseline: `%s`\n", candidateBaselineFile)
	}
	if baselinePath != "" {
		fmt.Fprintf(&summary, "- Accepted baseline: `%s`\n", baselinePath)
	}
	if failure != "" {
		fmt.Fprintf(&summary, "- Failure: %s\n", failure)
	}
	fmt.Fprintln(&summary)
	fmt.Fprintln(&summary, "## Stages")
	fmt.Fprintln(&summary, "| Stage | Exit code | Evidence |")
	fmt.Fprintln(&summary, "| --- | ---: | --- |")
	for _, stage := range stages {
		exitCode, found, stageErr := readLoadStageExitCode(root, stage.path)
		value := "not run"
		if stageErr != nil {
			value = markdownCell(stageErr.Error())
		} else if found {
			value = strconv.Itoa(exitCode)
		}
		fmt.Fprintf(&summary, "| %s | %s | `%s.log` |\n", stage.name, value, stage.path)
	}
	fmt.Fprintln(&summary)
	fmt.Fprintln(&summary, "## Runs")
	fmt.Fprintln(&summary, "| Run | Result | Evidence |")
	fmt.Fprintln(&summary, "| --- | --- | --- |")
	for _, run := range evidence {
		result := "complete"
		diagnostic := ""
		switch {
		case run.ReadFailure != "":
			result = "missing"
			diagnostic = run.ReadFailure
		case run.DecodeFailure != "":
			result = "decode failed"
			diagnostic = run.DecodeFailure
		case run.ValidateFailure != "":
			result = "invalid"
			diagnostic = run.ValidateFailure
		}
		fmt.Fprintf(&summary, "| run-%d | %s | `%s` %s |\n",
			run.Number, result, run.ResultPath, markdownCell(diagnostic))
	}
	if report != nil {
		fmt.Fprintln(&summary)
		fmt.Fprintln(&summary, "## Comparison")
		fmt.Fprintln(&summary, "| Scenario | Metric | Baseline | Current | Delta | Status |")
		fmt.Fprintln(&summary, "| --- | --- | ---: | ---: | ---: | --- |")
		for _, entry := range report.Entries {
			fmt.Fprintf(&summary, "| %s | %s | %.6g | %.6g | %.3f%% | %s |\n",
				markdownCell(entry.Scenario), markdownCell(entry.Metric), entry.Baseline,
				entry.Current, entry.DeltaPct, entry.Status)
		}
	}
	if candidateBaseline != nil {
		fmt.Fprintln(&summary)
		fmt.Fprintln(&summary, "## Candidate baseline")
		fmt.Fprintln(&summary, "| Scenario | Metric | Median | Unit | Direction |")
		fmt.Fprintln(&summary, "| --- | --- | ---: | --- | --- |")
		for _, scenario := range candidateBaseline.Results {
			for _, metric := range scenario.Metrics {
				fmt.Fprintf(&summary, "| %s | %s | %.6g | %s | %s |\n",
					markdownCell(scenario.Name), markdownCell(metric.Name), metric.Median,
					markdownCell(metric.Unit), markdownCell(metric.Direction))
			}
		}
	}

	return summary.Bytes(), nil
}

func writeLoadModeSummary(root string, mode runMode, baselinePath string) error {
	summary, err := buildLoadModeSummary(root, mode, baselinePath)
	if err != nil {
		return fmt.Errorf("build load summary: %w", err)
	}
	return writeFileAtomic(filepath.Join(root, loadSummaryFile), summary, 0o644)
}

func loadSummaryStages(repeatCount int) []loadSummaryStage {
	stages := []loadSummaryStage{{name: "preflight", path: "preflight"}}
	for run := 1; run <= repeatCount; run++ {
		stages = append(stages, loadSummaryStage{
			name: fmt.Sprintf("run-%d", run),
			path: filepath.Join(fmt.Sprintf("run-%d", run), "go-test"),
		})
	}
	return append(stages, loadSummaryStage{name: "finalize", path: "finalize"})
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

func inspectLoadRunEvidence(root string, mode runMode) ([]loadRunEvidence, error) {
	repeatCount, err := requiredRepeatCount(mode)
	if err != nil {
		return nil, err
	}
	evidence := make([]loadRunEvidence, repeatCount)
	for i := range evidence {
		number := i + 1
		resultPath := filepath.Join(fmt.Sprintf("run-%d", number), resultV2File)
		evidence[i] = loadRunEvidence{Number: number, ResultPath: filepath.ToSlash(resultPath)}
		data, err := os.ReadFile(filepath.Join(root, resultPath))
		if os.IsNotExist(err) {
			evidence[i].ReadFailure = "not run"
			continue
		}
		if err != nil {
			evidence[i].ReadFailure = err.Error()
			continue
		}

		var artifact RunArtifactV2
		if err := json.Unmarshal(data, &artifact); err != nil {
			evidence[i].DecodeFailure = err.Error()
			continue
		}
		evidence[i].Artifact = &artifact
		if err := artifact.Validate(); err != nil {
			evidence[i].ValidateFailure = err.Error()
		}
		if _, err := decodeRunArtifactV2(data); err != nil && evidence[i].ValidateFailure == "" {
			evidence[i].ValidateFailure = err.Error()
		}
	}
	return evidence, nil
}
