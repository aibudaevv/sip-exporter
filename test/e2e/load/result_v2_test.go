//go:build e2e

package load

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

const (
	resultSchemaVersion = 2
	resultV2File        = "result.json"
	loadModeEnv         = "SIP_EXPORTER_LOAD_MODE"
	loadArtifactDirEnv  = "SIP_EXPORTER_LOAD_ARTIFACT_DIR"
)

type (
	runMode        string
	scenarioStatus string

	EnvironmentFingerprint struct {
		OS            string `json:"os"`
		Arch          string `json:"arch"`
		GoVersion     string `json:"go_version"`
		KernelVersion string `json:"kernel_version"`
		DockerVersion string `json:"docker_version"`
		ExporterImage string `json:"exporter_image"`
	}

	ScenarioResultV2 struct {
		Name       string                 `json:"name"`
		Status     scenarioStatus         `json:"status"`
		StartedAt  time.Time              `json:"started_at"`
		FinishedAt time.Time              `json:"finished_at,omitempty"`
		Failure    string                 `json:"failure,omitempty"`
		Limits     WorkloadLimits         `json:"limits"`
		Generator  *GeneratorResult       `json:"generator,omitempty"`
		Capture    *CaptureResult         `json:"capture,omitempty"`
		Protocols  *ProtocolCounters      `json:"protocols,omitempty"`
		Resources  *ResourceSummaryV2     `json:"resources,omitempty"`
		Metrics    map[string]MetricEntry `json:"metrics,omitempty"`
		Artifacts  []string               `json:"artifacts,omitempty"`
	}

	ResourceSummaryV2 struct {
		Limits            WorkloadLimits `json:"limits"`
		CPUP95Percent     float64        `json:"cpu_p95_percent"`
		WorkingSetP99MB   float64        `json:"working_set_p99_mb"`
		ThrottlingPercent float64        `json:"throttling_percent"`
		ChannelPeak       float64        `json:"channel_peak"`
		SocketDrops       float64        `json:"socket_drops"`
		RTPDrops          float64        `json:"rtp_drops"`
		GCMaxSTWMS        float64        `json:"gc_max_stw_ms"`
	}

	ResourceSamplesV2 struct {
		Resources []resourceSample    `json:"resources,omitempty"`
		Metrics   []metricSamplePoint `json:"metrics,omitempty"`
		GCPauses  []gcPauseSample     `json:"gc_pauses,omitempty"`
	}

	RunArtifactV2 struct {
		Version         int                    `json:"version"`
		Mode            runMode                `json:"mode"`
		ReleaseEligible bool                   `json:"release_eligible"`
		StartedAt       time.Time              `json:"started_at"`
		FinishedAt      time.Time              `json:"finished_at"`
		Commit          string                 `json:"commit"`
		Environment     EnvironmentFingerprint `json:"environment"`
		Results         []ScenarioResultV2     `json:"results"`
	}

	runRecorderV2 struct {
		mu   sync.Mutex
		root string
		run  RunArtifactV2
		rows map[string]int
	}
)

const (
	runModeTargeted  runMode = "targeted"
	runModeRelease   runMode = "release"
	runModeCandidate runMode = "candidate"

	scenarioStatusIncomplete scenarioStatus = "incomplete"
	scenarioStatusComplete   scenarioStatus = "complete"
	scenarioStatusFailed     scenarioStatus = "failed"
)

func (r RunArtifactV2) Validate() error {
	if r.Version != resultSchemaVersion {
		return fmt.Errorf("result schema version: got %d, want %d", r.Version, resultSchemaVersion)
	}
	if !r.Mode.valid() {
		return fmt.Errorf("result mode: %q", r.Mode)
	}
	if r.ReleaseEligible != (r.Mode == runModeRelease) {
		return fmt.Errorf("release eligibility does not match mode %q", r.Mode)
	}
	if r.StartedAt.IsZero() || r.FinishedAt.IsZero() || r.FinishedAt.Before(r.StartedAt) {
		return fmt.Errorf("invalid run timestamps")
	}
	if r.Commit == "" {
		return fmt.Errorf("missing commit")
	}
	if err := r.Environment.validate(); err != nil {
		return err
	}
	if len(r.Results) == 0 {
		return fmt.Errorf("run has no scenario results")
	}

	seen := make(map[string]struct{}, len(r.Results))
	for i := range r.Results {
		row := r.Results[i]
		if row.Name == "" {
			return fmt.Errorf("result %d has no scenario name", i)
		}
		if _, ok := seen[row.Name]; ok {
			return fmt.Errorf("duplicate scenario result %q", row.Name)
		}
		seen[row.Name] = struct{}{}
		if err := row.validate(); err != nil {
			return fmt.Errorf("scenario %q: %w", row.Name, err)
		}
	}
	return nil
}

func (m runMode) valid() bool {
	return m == runModeTargeted || m == runModeRelease || m == runModeCandidate
}

func (f EnvironmentFingerprint) validate() error {
	switch {
	case f.OS == "":
		return fmt.Errorf("missing environment OS")
	case f.Arch == "":
		return fmt.Errorf("missing environment architecture")
	case f.GoVersion == "":
		return fmt.Errorf("missing Go version")
	case f.KernelVersion == "":
		return fmt.Errorf("missing kernel version")
	case f.DockerVersion == "":
		return fmt.Errorf("missing Docker version")
	case f.ExporterImage == "":
		return fmt.Errorf("missing exporter image")
	default:
		return nil
	}
}

func (r ScenarioResultV2) validate() error {
	if r.Status != scenarioStatusComplete && r.Status != scenarioStatusFailed {
		return fmt.Errorf("status is %q", r.Status)
	}
	if r.Status == scenarioStatusFailed {
		switch {
		case r.Failure == "":
			return fmt.Errorf("failed scenario has no failure")
		case r.Generator == nil:
			return fmt.Errorf("failed scenario has no generator evidence")
		case r.Capture == nil:
			return fmt.Errorf("failed scenario has no capture evidence")
		case r.Protocols == nil:
			return fmt.Errorf("failed scenario has no protocol evidence")
		case len(r.Artifacts) == 0:
			return fmt.Errorf("failed scenario has no artifacts")
		}
	}
	if r.StartedAt.IsZero() || r.FinishedAt.IsZero() || r.FinishedAt.Before(r.StartedAt) {
		return fmt.Errorf("invalid timestamps")
	}
	if err := r.Limits.validate(); err != nil {
		return err
	}
	if len(r.Metrics) == 0 {
		return fmt.Errorf("missing metrics")
	}
	if r.Resources == nil {
		return fmt.Errorf("missing resource summary")
	}
	for name, metric := range r.Metrics {
		if name == "" || metric.Unit == "" ||
			(metric.Direction != dirHigherIsBetter && metric.Direction != dirLowerIsBetter) {
			return fmt.Errorf("invalid metric %q", name)
		}
		if math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) {
			return fmt.Errorf("metric %q is non-finite", name)
		}
	}
	if r.Generator != nil && !finiteFloats(r.Generator.ActualRate) {
		return fmt.Errorf("generator contains a non-finite value")
	}
	if r.Capture != nil && !finiteFloats(
		r.Capture.Expected, r.Capture.Captured, r.Capture.Missing,
		r.Capture.Excess, r.Capture.LossPct, r.Capture.ExcessPct,
	) {
		return fmt.Errorf("capture contains a non-finite value")
	}
	if r.Protocols != nil && !finiteFloats(
		r.Protocols.SIPPackets, r.Protocols.RTPPackets, r.Protocols.RTCPReports,
		r.Protocols.VQReports, r.Protocols.SocketReceived, r.Protocols.SocketDropped,
	) {
		return fmt.Errorf("protocol counters contain a non-finite value")
	}
	if err := r.Resources.Limits.validate(); err != nil {
		return fmt.Errorf("resource summary: %w", err)
	}
	if r.Resources.Limits != r.Limits {
		return fmt.Errorf("resource limits do not match scenario limits")
	}
	if !finiteFloats(
		r.Resources.CPUP95Percent, r.Resources.WorkingSetP99MB,
		r.Resources.ThrottlingPercent, r.Resources.ChannelPeak,
		r.Resources.SocketDrops, r.Resources.RTPDrops, r.Resources.GCMaxSTWMS,
	) {
		return fmt.Errorf("resource summary contains a non-finite value")
	}
	for name, expected := range resourceMetricEntries(*r.Resources) {
		actual, ok := r.Metrics[name]
		if !ok {
			return fmt.Errorf("missing canonical resource metric %q", name)
		}
		if actual != expected {
			return fmt.Errorf("canonical resource metric %q does not match resource summary", name)
		}
	}
	return nil
}

func finiteFloats(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func decodeRunArtifactV2(data []byte) (RunArtifactV2, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return RunArtifactV2{}, fmt.Errorf("decode result artifact fields: %w", err)
	}
	releaseEligibleJSON, ok := fields["release_eligible"]
	if !ok {
		return RunArtifactV2{}, fmt.Errorf("validate result artifact: missing release_eligible")
	}
	var releaseEligible *bool
	if err := json.Unmarshal(releaseEligibleJSON, &releaseEligible); err != nil || releaseEligible == nil {
		return RunArtifactV2{}, fmt.Errorf("validate result artifact: release_eligible must be boolean")
	}
	var result RunArtifactV2
	if err := json.Unmarshal(data, &result); err != nil {
		return RunArtifactV2{}, fmt.Errorf("decode result artifact: %w", err)
	}
	if err := result.Validate(); err != nil {
		return RunArtifactV2{}, fmt.Errorf("validate result artifact: %w", err)
	}
	return result, nil
}

func newRunRecorderV2(
	mode runMode,
	root string,
	fingerprint EnvironmentFingerprint,
	commit string,
	started time.Time,
) (*runRecorderV2, error) {
	if mode == "" {
		return nil, nil
	}
	if !mode.valid() {
		return nil, fmt.Errorf("result mode: %q", mode)
	}
	if root == "" {
		return nil, fmt.Errorf("result artifact directory is empty")
	}
	return &runRecorderV2{
		root: root,
		run: RunArtifactV2{
			Version:         resultSchemaVersion,
			Mode:            mode,
			ReleaseEligible: mode == runModeRelease,
			StartedAt:       started,
			Commit:          commit,
			Environment:     fingerprint,
		},
		rows: make(map[string]int),
	}, nil
}

func (r *runRecorderV2) Begin(name string, started time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "" {
		return fmt.Errorf("scenario name is empty")
	}
	if _, ok := r.rows[name]; ok {
		return fmt.Errorf("duplicate scenario result %q", name)
	}
	r.rows[name] = len(r.run.Results)
	r.run.Results = append(r.run.Results, ScenarioResultV2{
		Name:      name,
		Status:    scenarioStatusIncomplete,
		StartedAt: started,
	})
	return nil
}

func (r *runRecorderV2) Complete(
	name string,
	metrics map[string]MetricEntry,
	finished time.Time,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := validateMetricEntries(metrics); err != nil {
		return err
	}
	index, ok := r.rows[name]
	if !ok {
		return fmt.Errorf("scenario %q was not started", name)
	}
	row := &r.run.Results[index]
	if row.Status == scenarioStatusComplete {
		return fmt.Errorf("scenario %q was already completed", name)
	}
	row.Metrics = cloneMetricEntries(metrics)
	row.Status = scenarioStatusComplete
	row.FinishedAt = finished
	return nil
}

func (r *runRecorderV2) Fail(name, failure string) error {
	if failure == "" {
		return fmt.Errorf("scenario failure is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	index, ok := r.rows[name]
	if !ok {
		return fmt.Errorf("scenario %q was not started", name)
	}
	row := &r.run.Results[index]
	if row.Status != scenarioStatusComplete {
		return fmt.Errorf("scenario %q has status %q", name, row.Status)
	}
	row.Status = scenarioStatusFailed
	row.Failure = failure
	return nil
}

func validateMetricEntries(metrics map[string]MetricEntry) error {
	for name, metric := range metrics {
		if math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) {
			return fmt.Errorf("metric %q is non-finite", name)
		}
	}
	return nil
}

func (r *runRecorderV2) Finalize(name string, failed bool, finished time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index, ok := r.rows[name]
	if !ok {
		return
	}
	row := &r.run.Results[index]
	if row.Status == scenarioStatusFailed {
		return
	}
	if failed {
		row.FinishedAt = finished
		if row.Status == scenarioStatusComplete {
			row.Status = scenarioStatusFailed
			row.Failure = "test failed after result completion"
		} else {
			row.Failure = "test failed before result completion"
		}
		return
	}
	if row.Status == scenarioStatusComplete {
		return
	}
	row.FinishedAt = finished
	row.Failure = "scenario finished without a recorded result"
}

func (r *runRecorderV2) Snapshot() RunArtifactV2 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneRunArtifactV2(r.run)
}

func (r *runRecorderV2) Save(finished time.Time) error {
	r.mu.Lock()
	r.run.FinishedAt = finished
	snapshot := cloneRunArtifactV2(r.run)
	r.mu.Unlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal result artifact: %w", err)
	}
	if err := os.MkdirAll(r.root, 0o755); err != nil {
		return fmt.Errorf("create result artifact directory: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(r.root, resultV2File), data, 0o644); err != nil {
		return fmt.Errorf("write result artifact: %w", err)
	}
	return snapshot.Validate()
}

func (r *runRecorderV2) RecordArtifact(name, filename string, data []byte) error {
	if filename == "" || filename == "." || strings.ContainsAny(filename, `/\`) {
		return fmt.Errorf("invalid artifact filename %q", filename)
	}
	r.mu.Lock()
	index, ok := r.rows[name]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("scenario %q was not started", name)
	}
	relative := path.Join("scenarios", fmt.Sprintf("%03d", index), filename)
	target := filepath.Join(r.root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create scenario artifact directory: %w", err)
	}
	if err := writeFileAtomic(target, data, 0o644); err != nil {
		return fmt.Errorf("write scenario artifact: %w", err)
	}
	r.mu.Lock()
	r.run.Results[index].Artifacts = append(r.run.Results[index].Artifacts, relative)
	r.mu.Unlock()
	return nil
}

func (r *runRecorderV2) AttachLimits(name string, limits WorkloadLimits) error {
	if err := limits.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	index, ok := r.rows[name]
	if !ok {
		return fmt.Errorf("scenario %q was not started", name)
	}
	r.run.Results[index].Limits = limits
	return nil
}

func (r *runRecorderV2) AttachLoadResult(name string, result loadResult) error {
	if err := validateLoadResultEvidence(result); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	index, ok := r.rows[name]
	if !ok {
		return fmt.Errorf("scenario %q was not started", name)
	}
	capture := result.Capture
	protocols := result.Protocols
	if result.Generator != (GeneratorResult{}) {
		generator := result.Generator
		r.run.Results[index].Generator = &generator
	}
	r.run.Results[index].Capture = &capture
	r.run.Results[index].Protocols = &protocols
	resources := result.Resources
	if resources.Limits != r.run.Results[index].Limits {
		return fmt.Errorf("resource limits do not match scenario limits")
	}
	r.run.Results[index].Resources = &resources
	return nil
}

func validateLoadResultEvidence(result loadResult) error {
	if result.Generator != (GeneratorResult{}) && !finiteFloats(result.Generator.ActualRate) {
		return fmt.Errorf("generator contains a non-finite value")
	}
	if !finiteFloats(
		result.Capture.Expected, result.Capture.Captured, result.Capture.Missing,
		result.Capture.Excess, result.Capture.LossPct, result.Capture.ExcessPct,
	) {
		return fmt.Errorf("capture contains a non-finite value")
	}
	if !finiteFloats(
		result.Protocols.SIPPackets, result.Protocols.RTPPackets, result.Protocols.RTCPReports,
		result.Protocols.VQReports, result.Protocols.SocketReceived, result.Protocols.SocketDropped,
	) {
		return fmt.Errorf("protocol counters contain a non-finite value")
	}
	if !finiteFloats(
		result.Resources.CPUP95Percent, result.Resources.WorkingSetP99MB,
		result.Resources.ThrottlingPercent, result.Resources.ChannelPeak,
		result.Resources.SocketDrops, result.Resources.RTPDrops, result.Resources.GCMaxSTWMS,
	) {
		return fmt.Errorf("resource summary contains a non-finite value")
	}
	if err := result.Resources.Limits.validate(); err != nil {
		return fmt.Errorf("resource summary: %w", err)
	}
	return nil
}

func (r *runRecorderV2) AttachGenerator(name string, result GeneratorResult) error {
	if !finiteFloats(result.ActualRate) {
		return fmt.Errorf("generator contains a non-finite value")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	index, ok := r.rows[name]
	if !ok {
		return fmt.Errorf("scenario %q was not started", name)
	}
	row := &r.run.Results[index]
	if row.Generator == nil {
		generator := result
		row.Generator = &generator
		return nil
	}
	row.Generator.ExitCode = max(row.Generator.ExitCode, result.ExitCode)
	row.Generator.SuccessfulCalls += result.SuccessfulCalls
	row.Generator.FailedCalls += result.FailedCalls
	row.Generator.Retransmissions += result.Retransmissions
	row.Generator.Phases.WarmupStart = earlierTime(
		row.Generator.Phases.WarmupStart, result.Phases.WarmupStart,
	)
	row.Generator.Phases.Ready = earlierTime(row.Generator.Phases.Ready, result.Phases.Ready)
	row.Generator.Phases.MeasureStart = earlierTime(
		row.Generator.Phases.MeasureStart, result.Phases.MeasureStart,
	)
	row.Generator.Phases.MeasureEnd = laterTime(
		row.Generator.Phases.MeasureEnd, result.Phases.MeasureEnd,
	)
	row.Generator.Phases.DrainEnd = laterTime(row.Generator.Phases.DrainEnd, result.Phases.DrainEnd)
	measureDuration := row.Generator.Phases.MeasureEnd.Sub(row.Generator.Phases.MeasureStart)
	if measureDuration > 0 {
		row.Generator.ActualRate = float64(row.Generator.SuccessfulCalls) / measureDuration.Seconds()
	}
	return nil
}

func earlierTime(left, right time.Time) time.Time {
	if left.IsZero() || (!right.IsZero() && right.Before(left)) {
		return right
	}
	return left
}

func laterTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func writeFileAtomic(filename string, data []byte, mode fs.FileMode) (returnErr error) {
	directory := filepath.Dir(filename)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(filename)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if returnErr != nil {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("chmod temporary file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("rename temporary file: %w", err)
	}
	dir, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open parent directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync parent directory: %w", err)
	}
	return nil
}

func cloneRunArtifactV2(run RunArtifactV2) RunArtifactV2 {
	cloned := run
	cloned.Results = make([]ScenarioResultV2, len(run.Results))
	for i, row := range run.Results {
		cloned.Results[i] = row
		cloned.Results[i].Metrics = cloneMetricEntries(row.Metrics)
		cloned.Results[i].Artifacts = append([]string(nil), row.Artifacts...)
	}
	return cloned
}

func cloneMetricEntries(metrics map[string]MetricEntry) map[string]MetricEntry {
	if metrics == nil {
		return nil
	}
	cloned := make(map[string]MetricEntry, len(metrics))
	for name, metric := range metrics {
		cloned[name] = metric
	}
	return cloned
}

func loadRunModeFromEnvironment() (runMode, error) {
	mode := runMode(os.Getenv(loadModeEnv))
	if mode == "" {
		return "", nil
	}
	if !mode.valid() {
		return "", fmt.Errorf("%s: invalid mode %q", loadModeEnv, mode)
	}
	return mode, nil
}

func collectEnvironmentFingerprint(ctx context.Context) (EnvironmentFingerprint, error) {
	kernelVersion, err := commandOutput(ctx, "uname", "-r")
	if err != nil {
		return EnvironmentFingerprint{}, fmt.Errorf("read kernel version: %w", err)
	}
	dockerVersion, err := commandOutput(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return EnvironmentFingerprint{}, fmt.Errorf("read Docker version: %w", err)
	}
	image := os.Getenv("SIP_EXPORTER_E2E_IMAGE")
	if image == "" {
		return EnvironmentFingerprint{}, fmt.Errorf("SIP_EXPORTER_E2E_IMAGE is empty")
	}
	return EnvironmentFingerprint{
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		GoVersion:     runtime.Version(),
		KernelVersion: kernelVersion,
		DockerVersion: dockerVersion,
		ExporterImage: image,
	}, nil
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", fmt.Errorf("empty output from %s", name)
	}
	return value, nil
}

func recordLoadResultEvidence(t *testing.T, result loadResult) {
	t.Helper()
	if activeRunRecorder == nil {
		return
	}
	if err := activeRunRecorder.AttachLoadResult(t.Name(), result); err != nil {
		t.Fatalf("attach load result evidence: %v", err)
	}
}

func recordScenarioLimits(t *testing.T, limits WorkloadLimits) {
	t.Helper()
	if activeRunRecorder == nil {
		return
	}
	if err := activeRunRecorder.AttachLimits(t.Name(), limits); err != nil {
		t.Fatalf("attach workload limits: %v", err)
	}
}

func recordScenarioArtifact(t *testing.T, filename string, data []byte) {
	t.Helper()
	if activeRunRecorder == nil {
		return
	}
	if err := activeRunRecorder.RecordArtifact(t.Name(), filename, data); err != nil {
		t.Errorf("record scenario artifact %s: %v", filename, err)
	}
}

func recordContainerLogs(
	ctx context.Context,
	t *testing.T,
	filename string,
	container testcontainers.Container,
) {
	t.Helper()
	if activeRunRecorder == nil {
		return
	}
	logs, err := container.Logs(ctx)
	if err != nil {
		t.Errorf("read container logs for %s: %v", filename, err)
		return
	}
	defer logs.Close()
	data, err := io.ReadAll(logs)
	if err != nil {
		t.Errorf("read container log body for %s: %v", filename, err)
		return
	}
	recordScenarioArtifact(t, filename, data)
}
