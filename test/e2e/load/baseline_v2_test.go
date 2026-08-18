//go:build e2e

package load

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type (
	baselineKind string

	BenchmarkFingerprint struct {
		OS            string `json:"os"`
		Arch          string `json:"arch"`
		GoVersion     string `json:"go_version"`
		KernelVersion string `json:"kernel_version"`
		DockerVersion string `json:"docker_version"`
	}

	BaselineMetricV2 struct {
		Name         string  `json:"name"`
		Median       float64 `json:"median"`
		Unit         string  `json:"unit"`
		Direction    string  `json:"direction"`
		TolerancePct float64 `json:"tolerance_pct"`
	}

	BaselineScenarioV2 struct {
		Name    string             `json:"name"`
		Limits  WorkloadLimits     `json:"limits"`
		Metrics []BaselineMetricV2 `json:"metrics"`
	}

	BaselineV2 struct {
		Version       int                  `json:"version"`
		Kind          baselineKind         `json:"kind"`
		CreatedAt     time.Time            `json:"created_at"`
		Fingerprint   BenchmarkFingerprint `json:"fingerprint"`
		RepeatCount   int                  `json:"repeat_count"`
		SourceCommits []string             `json:"source_commits"`
		Results       []BaselineScenarioV2 `json:"results"`
	}

	AggregatedMetricV2 struct {
		Name      string
		Median    float64
		Unit      string
		Direction string
	}

	AggregatedScenarioV2 struct {
		Name    string
		Limits  WorkloadLimits
		Metrics []AggregatedMetricV2
	}

	AggregatedRunV2 struct {
		Mode          runMode
		Fingerprint   BenchmarkFingerprint
		RepeatCount   int
		SourceCommits []string
		Results       []AggregatedScenarioV2
	}

	ComparisonEntryV2 struct {
		Scenario string
		Metric   string
		Baseline float64
		Current  float64
		DeltaPct float64
		Status   ComparisonStatus
		Unit     string
	}

	ComparisonReportV2 struct {
		Entries []ComparisonEntryV2
	}
)

const (
	baselineSchemaVersion = 2
	baselineRepeatCount   = 5
	candidateBaselineFile = "baseline-candidate.json"

	baselineKindCandidate baselineKind = "candidate"
	baselineKindAccepted  baselineKind = "accepted"
)

func (b BaselineV2) Validate() error {
	if b.Version != baselineSchemaVersion {
		return fmt.Errorf("baseline schema version: got %d, want %d", b.Version, baselineSchemaVersion)
	}
	if b.Kind != baselineKindCandidate && b.Kind != baselineKindAccepted {
		return fmt.Errorf("baseline kind: %q", b.Kind)
	}
	if b.CreatedAt.IsZero() {
		return fmt.Errorf("missing baseline creation time")
	}
	if err := b.Fingerprint.validate(); err != nil {
		return err
	}
	if b.RepeatCount != baselineRepeatCount {
		return fmt.Errorf("baseline repeat count: got %d, want %d", b.RepeatCount, baselineRepeatCount)
	}
	if len(b.SourceCommits) != b.RepeatCount {
		return fmt.Errorf("source commit count: got %d, want %d", len(b.SourceCommits), b.RepeatCount)
	}
	for i, commit := range b.SourceCommits {
		if commit == "" {
			return fmt.Errorf("source commit %d is empty", i)
		}
	}
	if len(b.Results) == 0 {
		return fmt.Errorf("baseline has no scenario results")
	}

	seenScenarios := make(map[string]struct{}, len(b.Results))
	for i := range b.Results {
		scenario := b.Results[i]
		if scenario.Name == "" {
			return fmt.Errorf("baseline scenario %d has no name", i)
		}
		if _, ok := seenScenarios[scenario.Name]; ok {
			return fmt.Errorf("duplicate baseline scenario %q", scenario.Name)
		}
		seenScenarios[scenario.Name] = struct{}{}
		if err := scenario.validate(); err != nil {
			return fmt.Errorf("baseline scenario %q: %w", scenario.Name, err)
		}
	}
	return nil
}

func (f BenchmarkFingerprint) validate() error {
	switch {
	case f.OS == "":
		return fmt.Errorf("missing baseline OS")
	case f.Arch == "":
		return fmt.Errorf("missing baseline architecture")
	case f.GoVersion == "":
		return fmt.Errorf("missing baseline Go version")
	case f.KernelVersion == "":
		return fmt.Errorf("missing baseline kernel version")
	case f.DockerVersion == "":
		return fmt.Errorf("missing baseline Docker version")
	default:
		return nil
	}
}

func (s BaselineScenarioV2) validate() error {
	if err := s.Limits.validate(); err != nil {
		return err
	}
	if len(s.Metrics) == 0 {
		return fmt.Errorf("has no metrics")
	}
	seenMetrics := make(map[string]struct{}, len(s.Metrics))
	for i := range s.Metrics {
		metric := s.Metrics[i]
		if metric.Name == "" {
			return fmt.Errorf("metric %d has no name", i)
		}
		if _, ok := seenMetrics[metric.Name]; ok {
			return fmt.Errorf("duplicate metric %q", metric.Name)
		}
		seenMetrics[metric.Name] = struct{}{}
		if err := metric.validate(); err != nil {
			return fmt.Errorf("metric %q: %w", metric.Name, err)
		}
	}
	return nil
}

func (m BaselineMetricV2) validate() error {
	switch {
	case m.Unit == "":
		return fmt.Errorf("missing unit")
	case m.Direction != dirHigherIsBetter && m.Direction != dirLowerIsBetter:
		return fmt.Errorf("invalid direction %q", m.Direction)
	case !finiteFloats(m.Median):
		return fmt.Errorf("median is non-finite")
	case !finiteFloats(m.TolerancePct):
		return fmt.Errorf("tolerance is non-finite")
	case m.TolerancePct < 0:
		return fmt.Errorf("negative tolerance %v", m.TolerancePct)
	default:
		return nil
	}
}

func decodeBaselineV2(data []byte) (BaselineV2, error) {
	var baseline BaselineV2
	if err := json.Unmarshal(data, &baseline); err != nil {
		return BaselineV2{}, fmt.Errorf("decode baseline: %w", err)
	}
	if err := baseline.Validate(); err != nil {
		return BaselineV2{}, fmt.Errorf("validate baseline: %w", err)
	}
	return baseline, nil
}

func aggregateRunArtifacts(mode runMode, runs []RunArtifactV2) (AggregatedRunV2, error) {
	repeatCount, err := requiredRepeatCount(mode)
	if err != nil {
		return AggregatedRunV2{}, err
	}
	if len(runs) != repeatCount {
		return AggregatedRunV2{}, fmt.Errorf("%s run count: got %d, want %d", mode, len(runs), repeatCount)
	}
	for i := range runs {
		if err := runs[i].Validate(); err != nil {
			return AggregatedRunV2{}, fmt.Errorf("run %d: %w", i, err)
		}
		if runs[i].Mode != mode {
			return AggregatedRunV2{}, fmt.Errorf("run %d mode: got %q, want %q", i, runs[i].Mode, mode)
		}
	}

	fingerprint := benchmarkFingerprint(runs[0].Environment)
	for i := 1; i < len(runs); i++ {
		if !fingerprint.compatible(benchmarkFingerprint(runs[i].Environment)) {
			return AggregatedRunV2{}, fmt.Errorf("run %d has incompatible fingerprint", i)
		}
	}
	for i := 1; i < len(runs); i++ {
		if err := compareRunInventory(runs[0], runs[i]); err != nil {
			return AggregatedRunV2{}, fmt.Errorf("run %d: %w", i, err)
		}
	}

	aggregated := AggregatedRunV2{
		Mode:          mode,
		Fingerprint:   fingerprint,
		RepeatCount:   repeatCount,
		SourceCommits: make([]string, len(runs)),
		Results:       make([]AggregatedScenarioV2, 0, len(runs[0].Results)),
	}
	for i := range runs {
		aggregated.SourceCommits[i] = runs[i].Commit
	}
	for _, referenceScenario := range runs[0].Results {
		scenario := AggregatedScenarioV2{Name: referenceScenario.Name, Limits: referenceScenario.Limits}
		for metricName, referenceMetric := range referenceScenario.Metrics {
			values := make([]float64, len(runs))
			for i := range runs {
				values[i] = scenarioByName(runs[i], referenceScenario.Name).Metrics[metricName].Value
			}
			slices.Sort(values)
			scenario.Metrics = append(scenario.Metrics, AggregatedMetricV2{
				Name:      metricName,
				Median:    values[len(values)/2],
				Unit:      referenceMetric.Unit,
				Direction: referenceMetric.Direction,
			})
		}
		slices.SortFunc(scenario.Metrics, func(a, b AggregatedMetricV2) int {
			return cmp.Compare(a.Name, b.Name)
		})
		aggregated.Results = append(aggregated.Results, scenario)
	}
	slices.SortFunc(aggregated.Results, func(a, b AggregatedScenarioV2) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return aggregated, nil
}

func requiredRepeatCount(mode runMode) (int, error) {
	switch mode {
	case runModeRelease:
		return 3, nil
	case runModeCandidate:
		return 5, nil
	default:
		return 0, fmt.Errorf("run mode %q cannot be aggregated", mode)
	}
}

func benchmarkFingerprint(environment EnvironmentFingerprint) BenchmarkFingerprint {
	return BenchmarkFingerprint{
		OS:            environment.OS,
		Arch:          environment.Arch,
		GoVersion:     environment.GoVersion,
		KernelVersion: environment.KernelVersion,
		DockerVersion: environment.DockerVersion,
	}
}

func (f BenchmarkFingerprint) compatible(other BenchmarkFingerprint) bool {
	return f == other
}

func compareRunInventory(reference, current RunArtifactV2) error {
	referenceScenarios := scenarioInventory(reference)
	currentScenarios := scenarioInventory(current)
	for name := range currentScenarios {
		if _, ok := referenceScenarios[name]; !ok {
			return fmt.Errorf("new scenario %q", name)
		}
	}
	for name, referenceScenario := range referenceScenarios {
		currentScenario, ok := currentScenarios[name]
		if !ok {
			return fmt.Errorf("missing scenario %q", name)
		}
		if currentScenario.Limits != referenceScenario.Limits {
			return fmt.Errorf("scenario %q limits mismatch", name)
		}
		if err := compareMetricInventory(name, referenceScenario.Metrics, currentScenario.Metrics); err != nil {
			return err
		}
	}
	return nil
}

func scenarioInventory(run RunArtifactV2) map[string]ScenarioResultV2 {
	result := make(map[string]ScenarioResultV2, len(run.Results))
	for _, scenario := range run.Results {
		result[scenario.Name] = scenario
	}
	return result
}

func compareMetricInventory(
	scenarioName string,
	reference, current map[string]MetricEntry,
) error {
	for name := range current {
		if _, ok := reference[name]; !ok {
			return fmt.Errorf("scenario %q has new metric %q", scenarioName, name)
		}
	}
	for name, referenceMetric := range reference {
		currentMetric, ok := current[name]
		if !ok {
			return fmt.Errorf("scenario %q missing metric %q", scenarioName, name)
		}
		if currentMetric.Unit != referenceMetric.Unit {
			return fmt.Errorf("scenario %q metric %q unit mismatch", scenarioName, name)
		}
		if currentMetric.Direction != referenceMetric.Direction {
			return fmt.Errorf("scenario %q metric %q direction mismatch", scenarioName, name)
		}
	}
	return nil
}

func scenarioByName(run RunArtifactV2, name string) ScenarioResultV2 {
	for _, scenario := range run.Results {
		if scenario.Name == name {
			return scenario
		}
	}
	return ScenarioResultV2{}
}

func thresholdPolicy(scenario string, metric AggregatedMetricV2) (float64, error) {
	if metric.Direction != dirHigherIsBetter && metric.Direction != dirLowerIsBetter {
		return 0, fmt.Errorf("metric %q has invalid direction %q", metric.Name, metric.Direction)
	}
	switch metric.Name {
	case "actual_pps", "actual_cps":
		if metric.Direction != dirHigherIsBetter {
			return 0, fmt.Errorf("metric %q must be higher-is-better", metric.Name)
		}
		return 3, nil
	case "cpu_avg", "cpu_peak", "mem_mb":
		if metric.Direction != dirLowerIsBetter {
			return 0, fmt.Errorf("metric %q must be lower-is-better", metric.Name)
		}
		return 10, nil
	case "cpu_p95_percent", "working_set_p99_mb", "working_set_first_minute_median_mb",
		"working_set_last_minute_median_mb", "working_set_growth_mb":
		if metric.Direction != dirLowerIsBetter {
			return 0, fmt.Errorf("metric %q must be lower-is-better", metric.Name)
		}
		return 10, nil
	case "throttling_percent", "channel_peak", "socket_drops", "rtp_drops":
		if metric.Direction != dirLowerIsBetter {
			return 0, fmt.Errorf("metric %q must be lower-is-better", metric.Name)
		}
		return 0, nil
	case "gc_max_stw_ms", "scrape_p50_ms", "scrape_p95_ms", "scrape_p99_ms":
		if metric.Direction != dirLowerIsBetter {
			return 0, fmt.Errorf("latency metric %q must be lower-is-better", metric.Name)
		}
		return 20, nil
	default:
		if strings.HasSuffix(metric.Name, "_ms") &&
			(strings.Contains(scenario, "GC") || strings.Contains(scenario, "Scrape")) {
			if metric.Direction != dirLowerIsBetter {
				return 0, fmt.Errorf("latency metric %q must be lower-is-better", metric.Name)
			}
			return 20, nil
		}
		return 0, nil
	}
}

func buildCandidateBaseline(aggregated AggregatedRunV2, createdAt time.Time) (BaselineV2, error) {
	if aggregated.Mode != runModeCandidate || aggregated.RepeatCount != baselineRepeatCount {
		return BaselineV2{}, fmt.Errorf("candidate aggregate requires %d candidate runs", baselineRepeatCount)
	}
	baseline := BaselineV2{
		Version:       baselineSchemaVersion,
		Kind:          baselineKindCandidate,
		CreatedAt:     createdAt,
		Fingerprint:   aggregated.Fingerprint,
		RepeatCount:   aggregated.RepeatCount,
		SourceCommits: append([]string(nil), aggregated.SourceCommits...),
		Results:       make([]BaselineScenarioV2, 0, len(aggregated.Results)),
	}
	for _, aggregatedScenario := range aggregated.Results {
		scenario := BaselineScenarioV2{Name: aggregatedScenario.Name, Limits: aggregatedScenario.Limits}
		for _, aggregatedMetric := range aggregatedScenario.Metrics {
			tolerance, err := thresholdPolicy(aggregatedScenario.Name, aggregatedMetric)
			if err != nil {
				return BaselineV2{}, fmt.Errorf("scenario %q: %w", aggregatedScenario.Name, err)
			}
			scenario.Metrics = append(scenario.Metrics, BaselineMetricV2{
				Name:         aggregatedMetric.Name,
				Median:       aggregatedMetric.Median,
				Unit:         aggregatedMetric.Unit,
				Direction:    aggregatedMetric.Direction,
				TolerancePct: tolerance,
			})
		}
		slices.SortFunc(scenario.Metrics, func(a, b BaselineMetricV2) int {
			return cmp.Compare(a.Name, b.Name)
		})
		baseline.Results = append(baseline.Results, scenario)
	}
	slices.SortFunc(baseline.Results, func(a, b BaselineScenarioV2) int {
		return cmp.Compare(a.Name, b.Name)
	})
	if err := baseline.Validate(); err != nil {
		return BaselineV2{}, fmt.Errorf("build candidate baseline: %w", err)
	}
	return baseline, nil
}

func writeCandidateBaseline(root string, baseline BaselineV2) error {
	if baseline.Kind != baselineKindCandidate {
		return fmt.Errorf("candidate output requires kind %q", baselineKindCandidate)
	}
	if err := baseline.Validate(); err != nil {
		return fmt.Errorf("validate candidate baseline: %w", err)
	}
	canonical := canonicalBaselineV2(baseline)
	data, err := json.MarshalIndent(canonical, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal candidate baseline: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create candidate baseline directory: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(root, candidateBaselineFile), data, 0o644); err != nil {
		return fmt.Errorf("write candidate baseline: %w", err)
	}
	return nil
}

func canonicalBaselineV2(baseline BaselineV2) BaselineV2 {
	canonical := baseline
	canonical.SourceCommits = append([]string(nil), baseline.SourceCommits...)
	canonical.Results = make([]BaselineScenarioV2, len(baseline.Results))
	for i, scenario := range baseline.Results {
		canonical.Results[i] = scenario
		canonical.Results[i].Metrics = append([]BaselineMetricV2(nil), scenario.Metrics...)
		slices.SortFunc(canonical.Results[i].Metrics, func(a, b BaselineMetricV2) int {
			return cmp.Compare(a.Name, b.Name)
		})
	}
	slices.SortFunc(canonical.Results, func(a, b BaselineScenarioV2) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return canonical
}

func compareRelease(baseline BaselineV2, current AggregatedRunV2) (ComparisonReportV2, error) {
	if err := baseline.Validate(); err != nil {
		return ComparisonReportV2{}, fmt.Errorf("validate accepted baseline: %w", err)
	}
	if baseline.Kind != baselineKindAccepted {
		return ComparisonReportV2{}, fmt.Errorf("baseline kind %q is not accepted", baseline.Kind)
	}
	if err := validateReleaseAggregate(current); err != nil {
		return ComparisonReportV2{}, err
	}
	if !baseline.Fingerprint.compatible(current.Fingerprint) {
		return ComparisonReportV2{}, fmt.Errorf("release fingerprint does not match accepted baseline")
	}
	if err := validateComparisonInventory(baseline, current); err != nil {
		return ComparisonReportV2{}, err
	}

	baselineScenarios := baselineScenarioInventory(baseline.Results)
	currentScenarios := aggregatedScenarioInventory(current.Results)
	report := ComparisonReportV2{}
	for scenarioName, baselineScenario := range baselineScenarios {
		currentScenario := currentScenarios[scenarioName]
		baselineMetrics := baselineMetricInventory(baselineScenario.Metrics)
		currentMetrics := aggregatedMetricInventory(currentScenario.Metrics)
		for name, baselineMetric := range baselineMetrics {
			currentMetric := currentMetrics[name]
			report.Entries = append(report.Entries, ComparisonEntryV2{
				Scenario: scenarioName,
				Metric:   name,
				Baseline: baselineMetric.Median,
				Current:  currentMetric.Median,
				DeltaPct: metricDeltaPct(baselineMetric.Median, currentMetric.Median),
				Status: classifyMetricV2(
					baselineMetric.Median, currentMetric.Median,
					baselineMetric.TolerancePct, baselineMetric.Direction,
				),
				Unit: baselineMetric.Unit,
			})
		}
	}
	slices.SortFunc(report.Entries, func(a, b ComparisonEntryV2) int {
		if byScenario := cmp.Compare(a.Scenario, b.Scenario); byScenario != 0 {
			return byScenario
		}
		return cmp.Compare(a.Metric, b.Metric)
	})
	for _, entry := range report.Entries {
		if entry.Status == StatusRegression {
			return report, fmt.Errorf("release regression in %s metric %s", entry.Scenario, entry.Metric)
		}
	}
	return report, nil
}

func validateComparisonInventory(baseline BaselineV2, current AggregatedRunV2) error {
	baselineScenarios := baselineScenarioInventory(baseline.Results)
	currentScenarios := aggregatedScenarioInventory(current.Results)
	for _, name := range sortedKeys(currentScenarios) {
		if _, ok := baselineScenarios[name]; !ok {
			return fmt.Errorf("release has new scenario %q", name)
		}
	}
	for _, scenarioName := range sortedKeys(baselineScenarios) {
		baselineScenario := baselineScenarios[scenarioName]
		currentScenario, ok := currentScenarios[scenarioName]
		if !ok {
			return fmt.Errorf("release is missing scenario %q", scenarioName)
		}
		if baselineScenario.Limits != currentScenario.Limits {
			return fmt.Errorf("scenario %q limits mismatch", scenarioName)
		}
		baselineMetrics := baselineMetricInventory(baselineScenario.Metrics)
		currentMetrics := aggregatedMetricInventory(currentScenario.Metrics)
		for _, name := range sortedKeys(currentMetrics) {
			if _, ok := baselineMetrics[name]; !ok {
				return fmt.Errorf("scenario %q has new metric %q", scenarioName, name)
			}
		}
		for _, name := range sortedKeys(baselineMetrics) {
			baselineMetric := baselineMetrics[name]
			currentMetric, ok := currentMetrics[name]
			if !ok {
				return fmt.Errorf("scenario %q is missing metric %q", scenarioName, name)
			}
			if baselineMetric.Unit != currentMetric.Unit {
				return fmt.Errorf("scenario %q metric %q unit mismatch", scenarioName, name)
			}
			if baselineMetric.Direction != currentMetric.Direction {
				return fmt.Errorf("scenario %q metric %q direction mismatch", scenarioName, name)
			}
		}
	}
	for _, scenarioName := range sortedKeys(baselineScenarios) {
		baselineMetrics := baselineMetricInventory(baselineScenarios[scenarioName].Metrics)
		currentMetrics := aggregatedMetricInventory(currentScenarios[scenarioName].Metrics)
		for _, name := range sortedKeys(baselineMetrics) {
			wantTolerance, err := thresholdPolicy(scenarioName, currentMetrics[name])
			if err != nil {
				return fmt.Errorf("scenario %q: %w", scenarioName, err)
			}
			if baselineMetrics[name].TolerancePct != wantTolerance {
				return fmt.Errorf(
					"scenario %q metric %q tolerance: got %v, want %v",
					scenarioName, name, baselineMetrics[name].TolerancePct, wantTolerance,
				)
			}
		}
	}
	return nil
}

func validateReleaseAggregate(current AggregatedRunV2) error {
	if current.Mode != runModeRelease || current.RepeatCount != 3 {
		return fmt.Errorf("comparison requires exactly three release runs")
	}
	if len(current.SourceCommits) != current.RepeatCount {
		return fmt.Errorf("release source commit count mismatch")
	}
	for i, commit := range current.SourceCommits {
		if commit == "" {
			return fmt.Errorf("release source commit %d is empty", i)
		}
	}
	if err := current.Fingerprint.validate(); err != nil {
		return fmt.Errorf("validate release fingerprint: %w", err)
	}
	if len(current.Results) == 0 {
		return fmt.Errorf("release aggregate has no scenarios")
	}
	seenScenarios := make(map[string]struct{}, len(current.Results))
	for i, scenario := range current.Results {
		if scenario.Name == "" {
			return fmt.Errorf("release scenario %d has no name", i)
		}
		if _, ok := seenScenarios[scenario.Name]; ok {
			return fmt.Errorf("duplicate release scenario %q", scenario.Name)
		}
		seenScenarios[scenario.Name] = struct{}{}
		if err := scenario.Limits.validate(); err != nil {
			return fmt.Errorf("release scenario %q: %w", scenario.Name, err)
		}
		if len(scenario.Metrics) == 0 {
			return fmt.Errorf("release scenario %q has no metrics", scenario.Name)
		}
		seenMetrics := make(map[string]struct{}, len(scenario.Metrics))
		for j, metric := range scenario.Metrics {
			if metric.Name == "" {
				return fmt.Errorf("release scenario %q metric %d has no name", scenario.Name, j)
			}
			if _, ok := seenMetrics[metric.Name]; ok {
				return fmt.Errorf("release scenario %q has duplicate metric %q", scenario.Name, metric.Name)
			}
			seenMetrics[metric.Name] = struct{}{}
			if metric.Unit == "" ||
				(metric.Direction != dirHigherIsBetter && metric.Direction != dirLowerIsBetter) ||
				!finiteFloats(metric.Median) {
				return fmt.Errorf("release scenario %q has invalid metric %q", scenario.Name, metric.Name)
			}
		}
	}
	return nil
}

func classifyMetricV2(
	baseline, current, tolerance float64,
	direction string,
) ComparisonStatus {
	if !finiteFloats(baseline, current, tolerance) || tolerance < 0 {
		return StatusRegression
	}
	if baseline == 0 {
		if current == 0 {
			return StatusOK
		}
		if direction == dirHigherIsBetter {
			if current > 0 {
				return StatusImprovement
			}
			return StatusRegression
		}
		if current > 0 {
			return StatusRegression
		}
		return StatusImprovement
	}
	margin := math.Abs(baseline) * tolerance / 100
	lower := baseline - margin
	upper := baseline + margin
	if direction == dirHigherIsBetter {
		switch {
		case current < lower:
			return StatusRegression
		case current > upper:
			return StatusImprovement
		default:
			return StatusOK
		}
	}
	switch {
	case current > upper:
		return StatusRegression
	case current < lower:
		return StatusImprovement
	default:
		return StatusOK
	}
}

func metricDeltaPct(baseline, current float64) float64 {
	if baseline == 0 {
		return 0
	}
	return (current - baseline) / baseline * 100
}

func baselineScenarioInventory(rows []BaselineScenarioV2) map[string]BaselineScenarioV2 {
	result := make(map[string]BaselineScenarioV2, len(rows))
	for _, row := range rows {
		result[row.Name] = row
	}
	return result
}

func aggregatedScenarioInventory(rows []AggregatedScenarioV2) map[string]AggregatedScenarioV2 {
	result := make(map[string]AggregatedScenarioV2, len(rows))
	for _, row := range rows {
		result[row.Name] = row
	}
	return result
}

func baselineMetricInventory(rows []BaselineMetricV2) map[string]BaselineMetricV2 {
	result := make(map[string]BaselineMetricV2, len(rows))
	for _, row := range rows {
		result[row.Name] = row
	}
	return result
}

func aggregatedMetricInventory(rows []AggregatedMetricV2) map[string]AggregatedMetricV2 {
	result := make(map[string]AggregatedMetricV2, len(rows))
	for _, row := range rows {
		result[row.Name] = row
	}
	return result
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
