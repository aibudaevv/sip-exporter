//go:build e2e

package load

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	resultsFile  = "load_result.json"
	baselineFile = "baseline.json"

	dirHigherIsBetter = "higher_is_better"
	dirLowerIsBetter  = "lower_is_better"
)

type (
	MetricEntry struct {
		Value     float64 `json:"value"`
		Unit      string  `json:"unit"`
		Direction string  `json:"direction"`
	}

	ScenarioResult struct {
		Metrics map[string]MetricEntry `json:"metrics"`
	}

	RegressionLimit struct {
		TolerancePct float64 `json:"tolerance_pct"`
		Direction    string  `json:"direction"`
	}

	BaselineData struct {
		Version          int                       `json:"version"`
		Updated          string                    `json:"updated"`
		Results          map[string]ScenarioResult `json:"results"`
		RegressionLimits map[string]RegressionLimit `json:"regression_limits"`
	}

	ResultsData struct {
		Version int                       `json:"version"`
		Updated string                    `json:"updated"`
		Commit  string                    `json:"commit,omitempty"`
		Results map[string]ScenarioResult `json:"results"`
	}

	ComparisonStatus string

	ComparisonEntry struct {
		Scenario string
		Metric   string
		Baseline float64
		Current  float64
		DeltaPct float64
		Status   ComparisonStatus
		Unit     string
	}
)

const (
	StatusOK          ComparisonStatus = "OK"
	StatusRegression  ComparisonStatus = "REGRESSION"
	StatusImprovement ComparisonStatus = "IMPROVEMENT"
	StatusNew         ComparisonStatus = "NEW"
)

var (
	resultsMu   sync.Mutex
	loadResults = &ResultsData{
		Version: 1,
		Results: make(map[string]ScenarioResult),
	}
)

func recordResult(scenario string, metrics map[string]MetricEntry) {
	resultsMu.Lock()
	defer resultsMu.Unlock()
	loadResults.Results[scenario] = ScenarioResult{Metrics: metrics}
}

func resultsFilePath() string {
	return filepath.Join(projectRoot, "test", "e2e", "load", resultsFile)
}

func baselineFilePath() string {
	return filepath.Join(projectRoot, "test", "e2e", "load", baselineFile)
}

func saveResults() error {
	loadResults.Updated = time.Now().Format(time.RFC3339)
	loadResults.Commit = getGitCommit()

	data, err := json.MarshalIndent(loadResults, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	if err := os.WriteFile(resultsFilePath(), data, 0o644); err != nil {
		return fmt.Errorf("write results: %w", err)
	}
	return nil
}

func loadBaseline() (*BaselineData, error) {
	data, err := os.ReadFile(baselineFilePath())
	if err != nil {
		return nil, err
	}
	var baseline BaselineData
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, fmt.Errorf("unmarshal baseline: %w", err)
	}
	return &baseline, nil
}

func getGitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func compareWithBaseline() ([]ComparisonEntry, error) {
	baseline, err := loadBaseline()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	resultsMu.Lock()
	snapshot := make(map[string]ScenarioResult, len(loadResults.Results))
	for k, v := range loadResults.Results {
		snapshot[k] = v
	}
	resultsMu.Unlock()

	var comparisons []ComparisonEntry

	for scenarioName, currentResult := range snapshot {
		baselineResult, exists := baseline.Results[scenarioName]
		if !exists {
			for metricName, entry := range currentResult.Metrics {
				comparisons = append(comparisons, ComparisonEntry{
					Scenario: scenarioName,
					Metric:   metricName,
					Current:  entry.Value,
					Status:   StatusNew,
					Unit:     entry.Unit,
				})
			}
			continue
		}

		for metricName, currentEntry := range currentResult.Metrics {
			baselineEntry, exists := baselineResult.Metrics[metricName]
			if !exists {
				comparisons = append(comparisons, ComparisonEntry{
					Scenario: scenarioName,
					Metric:   metricName,
					Current:  currentEntry.Value,
					Status:   StatusNew,
					Unit:     currentEntry.Unit,
				})
				continue
			}

			deltaPct := calcDeltaPct(baselineEntry.Value, currentEntry.Value)
			status := StatusOK

			limit, hasLimit := baseline.RegressionLimits[metricName]
			if hasLimit {
				status = classifyChange(currentEntry.Value, baselineEntry.Value,
					limit.TolerancePct, limit.Direction)
			}

			comparisons = append(comparisons, ComparisonEntry{
				Scenario: scenarioName,
				Metric:   metricName,
				Baseline: baselineEntry.Value,
				Current:  currentEntry.Value,
				DeltaPct: deltaPct,
				Status:   status,
				Unit:     currentEntry.Unit,
			})
		}
	}

	sort.Slice(comparisons, func(i, j int) bool {
		if comparisons[i].Scenario != comparisons[j].Scenario {
			return comparisons[i].Scenario < comparisons[j].Scenario
		}
		return comparisons[i].Metric < comparisons[j].Metric
	})

	return comparisons, nil
}

func calcDeltaPct(baseline, current float64) float64 {
	if baseline == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return (current - baseline) / baseline * 100
}

func classifyChange(current, baseline, tolerancePct float64, direction string) ComparisonStatus {
	if baseline == 0 {
		return StatusOK
	}
	deltaPct := (current - baseline) / baseline * 100

	if direction == dirHigherIsBetter {
		if deltaPct > tolerancePct {
			return StatusImprovement
		}
		if deltaPct < -tolerancePct {
			return StatusRegression
		}
	} else {
		if deltaPct > tolerancePct {
			return StatusRegression
		}
		if deltaPct < -tolerancePct {
			return StatusImprovement
		}
	}
	return StatusOK
}

func printSummary(comparisons []ComparisonEntry) {
	fmt.Println()
	fmt.Println("=== Load Test Baseline Comparison ===")
	fmt.Println()
	fmt.Printf("%-45s %-15s %-12s %-12s %-10s %s\n",
		"Scenario", "Metric", "Baseline", "Current", "Delta", "Status")

	okCount, regCount, imprCount, newCount := 0, 0, 0, 0

	for _, c := range comparisons {
		switch c.Status {
		case StatusOK:
			okCount++
		case StatusRegression:
			regCount++
		case StatusImprovement:
			imprCount++
		case StatusNew:
			newCount++
		}

		baselineStr := "-"
		deltaStr := "-"
		if c.Status != StatusNew {
			baselineStr = formatMetricValue(c.Baseline, c.Unit)
			deltaStr = fmt.Sprintf("%+.1f%%", c.DeltaPct)
		}

		currentStr := formatMetricValue(c.Current, c.Unit)

		fmt.Printf("%-45s %-15s %-12s %-12s %-10s %s\n",
			shortenName(c.Scenario), c.Metric, baselineStr, currentStr, deltaStr, c.Status)
	}

	fmt.Println()
	fmt.Printf("Summary: %d REGRESSIONS, %d IMPROVEMENTS, %d OK, %d NEW\n",
		regCount, imprCount, okCount, newCount)

	if regCount > 0 {
		fmt.Println("Result: FAIL - performance regressions detected")
	} else {
		fmt.Println("Result: PASS - no regressions detected")
	}
}

func formatMetricValue(v float64, unit string) string {
	switch unit {
	case "%":
		return fmt.Sprintf("%.2f%%", v)
	case "pps":
		return fmt.Sprintf("%.0f", v)
	case "count":
		return fmt.Sprintf("%.0f", v)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}

func shortenName(name string) string {
	name = strings.TrimPrefix(name, "TestLoad_")
	name = strings.TrimPrefix(name, "TestBenchmark_")
	return name
}

func saveAndCompare() int {
	resultsPath := resultsFilePath()
	if err := saveResults(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to save results to %s: %v\n", resultsPath, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "Results saved to %s\n", resultsPath)

	comparisons, err := compareWithBaseline()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: baseline comparison failed: %v\n", err)
		return 0
	}

	if len(comparisons) > 0 {
		printSummary(comparisons)
		for _, c := range comparisons {
			if c.Status == StatusRegression {
				return 1
			}
		}
		return 0
	}

	fmt.Println("\nNo baseline found — skipping comparison.")
	fmt.Println("Run 'make test-load-update-baseline' to save current results as baseline.")
	return 0
}

func TestMain(m *testing.M) {
	code := m.Run()
	if extra := saveAndCompare(); extra != 0 && code == 0 {
		code = extra
	}
	os.Exit(code)
}