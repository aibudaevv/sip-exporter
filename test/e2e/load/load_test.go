//go:build e2e

package load

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	sippImage                = "pbertera/sipp@sha256:063e8e9c8ecf54552e8efc3c363007afbfd3cae5a0f3f037db1c2e7fa4cd0349"
	testInterface            = "lo"
	selfMetricSampleInterval = 2 * time.Second
)

var sippRunID atomic.Uint64

func nextSippCallIDFormat() string {
	return fmt.Sprintf("load-%d-%%u-%%p@%%s", sippRunID.Add(1))
}

func TestSippCallIDFormat(t *testing.T) {
	first := nextSippCallIDFormat()
	second := nextSippCallIDFormat()

	require.NotEqual(t, first, second)
	for _, format := range []string{first, second} {
		require.Contains(t, format, "%u")
		require.Contains(t, format, "%p")
		require.Contains(t, format, "%s")
	}
}

type (
	testEnv struct {
		endpoint          string
		sippPort          string
		sippClientPort    string
		sippPort2         string
		sippClientPort2   string
		uasMediaPort      string
		uacMediaPort      string
		exporterContainer testcontainers.Container
		limits            WorkloadLimits
	}

	loadResult struct {
		Duration        time.Duration
		Generator       GeneratorResult
		Capture         CaptureResult
		Protocols       ProtocolCounters
		PacketsBefore   float64
		PacketsAfter    float64
		ActualPPS       float64
		ExpectedPPS     float64
		LossRate        float64
		ErrorCount      float64
		DrainTime       time.Duration
		PeakSessions    float64
		Resources       ResourceSummaryV2
		ResourceSamples ResourceSamplesV2
	}

	steadyMeasurement struct {
		mu             sync.Mutex
		env            *testEnv
		dockerCli      *client.Client
		cancel         context.CancelFunc
		done           chan struct{}
		measuring      bool
		ending         bool
		start          time.Time
		containerStart time.Time
		samples        ResourceSamplesV2
		err            error
	}

	testWriter struct {
		t *testing.T
	}

	metricSample struct {
		name   string
		labels map[string]string
		value  float64
	}
)

var (
	portMu             sync.Mutex
	nextBasePort       = 30000
	metricValuePattern = regexp.MustCompile(`^(?:NaN|[+-]?Inf|[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?)$`)
)

func projectRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

func exporterImage() string {
	image := os.Getenv("SIP_EXPORTER_E2E_IMAGE")
	if image == "" {
		return "sip-exporter:latest"
	}
	return image
}

func allocatePorts() (exporter, sipp, sippClient string) {
	portMu.Lock()
	defer portMu.Unlock()
	base := nextBasePort
	nextBasePort += 3
	return strconv.Itoa(base), strconv.Itoa(base + 1), strconv.Itoa(base + 2)
}

func allocatePortsN(n int) []string {
	portMu.Lock()
	defer portMu.Unlock()
	base := nextBasePort
	nextBasePort += n
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = strconv.Itoa(base + i)
	}
	return result
}

func contextTimeout(t *testing.T, ctx context.Context) time.Duration {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if testDeadline, testHasDeadline := t.Deadline(); testHasDeadline &&
		(!ok || testDeadline.Before(deadline)) {
		deadline = testDeadline
		ok = true
	}
	require.True(t, ok, "load test or operation context must have a deadline")
	timeout := time.Until(deadline)
	require.Greater(t, timeout, time.Duration(0), "load test context deadline already expired")
	return timeout
}

func exporterContainerRequest(
	ctx context.Context,
	t *testing.T,
	iface, httpPort, sipPorts string,
	limits WorkloadLimits,
) testcontainers.ContainerRequest {
	t.Helper()
	require.NoError(t, limits.validate())
	logLevel := "error"
	if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
		logLevel = "debug"
	}
	envVars := map[string]string{
		"SIP_EXPORTER_INTERFACE":       iface,
		"SIP_EXPORTER_HTTP_PORT":       httpPort,
		"SIP_EXPORTER_SIP_PORTS":       sipPorts,
		"SIP_EXPORTER_LOGGER_LEVEL":    logLevel,
		"SIP_EXPORTER_IGNORE_OUTGOING": "true",
		"SIP_EXPORTER_TELEMETRY":       "false",
	}
	if maxProcs := os.Getenv("SIP_EXPORTER_E2E_GOMAXPROCS"); maxProcs != "" {
		envVars["GOMAXPROCS"] = maxProcs
	}
	envVars["GODEBUG"] = forceGCTrace(os.Getenv("SIP_EXPORTER_E2E_GODEBUG"))
	return testcontainers.ContainerRequest{
		Image:       exporterImage(),
		Privileged:  true,
		NetworkMode: "host",
		Env:         envVars,
		WaitingFor: wait.ForHTTP("/metrics").
			WithPort(httpPort + "/tcp").
			WithStartupTimeout(contextTimeout(t, ctx)),
		HostConfigModifier: func(hostConfig *container.HostConfig) {
			hostConfig.Privileged = true
			hostConfig.NetworkMode = container.NetworkMode("host")
			hostConfig.NanoCPUs = int64(limits.CPUCores * 1_000_000_000)
			hostConfig.Memory = limits.MemoryBytes
		},
	}
}

func forceGCTrace(value string) string {
	settings := make([]string, 0)
	for _, setting := range strings.Split(value, ",") {
		if setting == "" || strings.HasPrefix(setting, "gctrace=") {
			continue
		}
		settings = append(settings, setting)
	}
	return strings.Join(append(settings, "gctrace=1"), ",")
}

func validateContainerLimits(nanoCPUs, memory int64, limits WorkloadLimits) error {
	if err := limits.validate(); err != nil {
		return err
	}
	wantNanoCPUs := int64(limits.CPUCores * 1_000_000_000)
	if nanoCPUs != wantNanoCPUs {
		return fmt.Errorf("container CPU limit: got %d, want %d", nanoCPUs, wantNanoCPUs)
	}
	if memory != limits.MemoryBytes {
		return fmt.Errorf("container memory limit: got %d, want %d", memory, limits.MemoryBytes)
	}
	return nil
}

func verifyContainerLimits(ctx context.Context, containerID string, limits WorkloadLimits) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("docker client: %w", err)
	}
	defer cli.Close()
	inspection, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect exporter container: %w", err)
	}
	return validateContainerLimits(
		inspection.Container.HostConfig.NanoCPUs,
		inspection.Container.HostConfig.Memory,
		limits,
	)
}

func newTestEnv(ctx context.Context, t *testing.T) *testEnv {
	return newTestEnvWithLimits(ctx, t, peakLimits)
}

func newTestEnvWithLimits(ctx context.Context, t *testing.T, limits WorkloadLimits) *testEnv {
	t.Helper()
	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	req := exporterContainerRequest(ctx, t, testInterface, exporterHTTPPort, sippPort, limits)

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           log.New(io.Discard, "", 0),
	})
	if err != nil && c != nil {
		logs, logErr := c.Logs(ctx)
		if logErr == nil {
			defer logs.Close()
			logBytes, _ := io.ReadAll(logs)
			t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
		}
	}
	require.NoError(t, err)
	require.NoError(t, verifyContainerLimits(ctx, c.GetContainerID(), limits))
	recordScenarioLimits(t, limits)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		recordContainerLogs(cleanupCtx, t, "exporter.log", c)
		if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
			logs, logErr := c.Logs(cleanupCtx)
			if logErr == nil {
				defer logs.Close()
				logBytes, _ := io.ReadAll(logs)
				t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
			}
		}
		_ = c.Stop(cleanupCtx, nil)
		_ = c.Terminate(cleanupCtx)
		for i := 0; i < 10; i++ {
			conn, dialErr := net.DialTimeout("tcp", "localhost:"+exporterHTTPPort, 500*time.Millisecond)
			if dialErr != nil {
				return
			}
			conn.Close()
			time.Sleep(500 * time.Millisecond)
		}
	})

	return &testEnv{
		endpoint:          fmt.Sprintf("http://localhost:%s", exporterHTTPPort),
		sippPort:          sippPort,
		sippClientPort:    sippClientPort,
		exporterContainer: c,
		limits:            limits,
	}
}

func newTestEnvWithCarrierAndUA(ctx context.Context, t *testing.T, carriersYAML, userAgentsYAML string) *testEnv {
	t.Helper()
	ports := allocatePortsN(5)
	exporterHTTPPort := ports[0]
	sippPort := ports[1]
	sippClientPort := ports[2]
	sippPort2 := ports[3]
	sippClientPort2 := ports[4]

	req := exporterContainerRequest(
		ctx, t, testInterface, exporterHTTPPort, sippPort+","+sippPort2, peakLimits,
	)

	var mounts testcontainers.ContainerMounts

	if carriersYAML != "" {
		tmpFile, tmpErr := os.CreateTemp("", "carriers-*.yaml")
		require.NoError(t, tmpErr)
		_, writeErr := tmpFile.WriteString(carriersYAML)
		require.NoError(t, writeErr)
		require.NoError(t, tmpFile.Close())
		t.Cleanup(func() { os.Remove(tmpFile.Name()) })

		mounts = append(mounts, testcontainers.BindMount(tmpFile.Name(), "/etc/sip-exporter/carriers.yaml"))
		req.Env["SIP_EXPORTER_CARRIERS_CONFIG"] = "/etc/sip-exporter/carriers.yaml"
	}

	if userAgentsYAML != "" {
		tmpFile, tmpErr := os.CreateTemp("", "user-agents-*.yaml")
		require.NoError(t, tmpErr)
		_, writeErr := tmpFile.WriteString(userAgentsYAML)
		require.NoError(t, writeErr)
		require.NoError(t, tmpFile.Close())
		t.Cleanup(func() { os.Remove(tmpFile.Name()) })

		mounts = append(mounts, testcontainers.BindMount(tmpFile.Name(), "/etc/sip-exporter/user_agents.yaml"))
		req.Env["SIP_EXPORTER_USER_AGENTS_CONFIG"] = "/etc/sip-exporter/user_agents.yaml"
	}

	req.Mounts = mounts

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           log.New(io.Discard, "", 0),
	})
	if err != nil && c != nil {
		logs, logErr := c.Logs(ctx)
		if logErr == nil {
			defer logs.Close()
			logBytes, _ := io.ReadAll(logs)
			t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
		}
	}
	require.NoError(t, err)
	require.NoError(t, verifyContainerLimits(ctx, c.GetContainerID(), peakLimits))
	recordScenarioLimits(t, peakLimits)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		recordContainerLogs(cleanupCtx, t, "exporter.log", c)
		if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
			logs, logErr := c.Logs(cleanupCtx)
			if logErr == nil {
				defer logs.Close()
				logBytes, _ := io.ReadAll(logs)
				t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
			}
		}
		_ = c.Stop(cleanupCtx, nil)
		_ = c.Terminate(cleanupCtx)
		for i := 0; i < 10; i++ {
			conn, dialErr := net.DialTimeout("tcp", "localhost:"+exporterHTTPPort, 500*time.Millisecond)
			if dialErr != nil {
				return
			}
			conn.Close()
			time.Sleep(500 * time.Millisecond)
		}
	})

	return &testEnv{
		endpoint:          fmt.Sprintf("http://localhost:%s", exporterHTTPPort),
		sippPort:          sippPort,
		sippClientPort:    sippClientPort,
		sippPort2:         sippPort2,
		sippClientPort2:   sippClientPort2,
		exporterContainer: c,
		limits:            peakLimits,
	}
}

func fetchMetricsBody(t *testing.T, endpoint string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/metrics", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return body
}

func recordMetricsSnapshot(t *testing.T, filename, endpoint string) {
	t.Helper()
	if activeRunRecorder == nil {
		return
	}
	recordScenarioArtifact(t, filename, fetchMetricsBody(t, endpoint))
}

func metricExists(t *testing.T, endpoint, metricName string) bool {
	t.Helper()
	return len(readMetricSamples(t, endpoint, metricName)) > 0
}

func getMetric(t *testing.T, endpoint, metricName string) float64 {
	t.Helper()
	return getMetricWithLabel(t, endpoint, metricName, "")
}

func getMetricWithLabel(t *testing.T, endpoint, metricName, labelFilter string) float64 {
	t.Helper()
	filterLabels, ok := parseMetricLabels(labelFilter)
	require.True(t, ok, "invalid label filter %q", labelFilter)
	if !ok {
		return 0
	}
	matches := metricSamplesWithLabels(readMetricSamples(t, endpoint, metricName), filterLabels)
	value, err := singleMetricValue(matches)
	require.NoError(t, err, "read metric %s with labels %q", metricName, labelFilter)
	return value
}

func getMetricSum(t *testing.T, endpoint, metricName string) float64 {
	t.Helper()
	samples := readMetricSamples(t, endpoint, metricName)
	require.NotEmpty(t, samples, "metric %s must have at least one sample", metricName)
	var sum float64
	for _, sample := range samples {
		sum += sample.value
	}
	return sum
}

func readMetricSamples(t *testing.T, endpoint, metricName string) []metricSample {
	t.Helper()
	return parseMetricSamples(fetchMetricsBody(t, endpoint), metricName)
}

func parseMetricSamples(body []byte, metricName string) []metricSample {
	var samples []metricSample
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		separator := metricSeriesEnd(line)
		if separator < 0 {
			continue
		}
		series, fields := line[:separator], strings.Fields(line[separator:])
		if len(fields) == 0 || len(fields) > 2 {
			continue
		}
		if len(fields) == 2 {
			if _, err := strconv.ParseInt(fields[1], 10, 64); err != nil {
				continue
			}
		}
		if !metricValuePattern.MatchString(fields[0]) {
			continue
		}
		name := series
		labels := map[string]string{}
		if labelsStart := strings.IndexByte(series, '{'); labelsStart >= 0 {
			name = series[:labelsStart]
			if !strings.HasSuffix(series, "}") {
				continue
			}
			var ok bool
			labels, ok = parseMetricLabels(series[labelsStart+1 : len(series)-1])
			if !ok {
				continue
			}
		}
		if name != metricName {
			continue
		}
		value, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		samples = append(samples, metricSample{name: name, labels: labels, value: value})
	}
	return samples
}

func metricSamplePointFromBody(at time.Time, body []byte) (metricSamplePoint, error) {
	channel, err := singleMetricValue(parseMetricSamples(body, "sip_exporter_channel_length"))
	if err != nil {
		return metricSamplePoint{}, fmt.Errorf("channel length: %w", err)
	}
	socketDrops, err := requiredMetricSum(body, "sip_exporter_socket_packets_dropped_total")
	if err != nil {
		return metricSamplePoint{}, err
	}
	rtpDrops, err := requiredMetricSum(body, "sip_exporter_rtp_dropped_total")
	if err != nil {
		return metricSamplePoint{}, err
	}
	if !finiteFloats(channel, socketDrops, rtpDrops) {
		return metricSamplePoint{}, fmt.Errorf("self metric is non-finite")
	}
	return metricSamplePoint{
		At: at, ChannelLength: channel, SocketDrops: socketDrops, RTPDrops: rtpDrops,
	}, nil
}

func requiredMetricSum(body []byte, name string) (float64, error) {
	samples := parseMetricSamples(body, name)
	if len(samples) == 0 {
		return 0, fmt.Errorf("metric %s is missing", name)
	}
	var total float64
	identities := make([]map[string]string, 0, len(samples))
	for _, sample := range samples {
		for _, identity := range identities {
			if maps.Equal(identity, sample.labels) {
				return 0, fmt.Errorf("metric %s contains a duplicate series", name)
			}
		}
		identities = append(identities, sample.labels)
		total += sample.value
	}
	return total, nil
}

func metricSeriesEnd(line string) int {
	insideLabels := false
	insideValue := false
	escaped := false
	for i := 0; i < len(line); i++ {
		if insideValue {
			if escaped {
				escaped = false
				continue
			}
			switch line[i] {
			case '\\':
				escaped = true
			case '"':
				insideValue = false
			}
			continue
		}
		switch line[i] {
		case '{':
			insideLabels = true
		case '}':
			insideLabels = false
		case '"':
			if insideLabels {
				insideValue = true
			}
		case ' ', '\t':
			if !insideLabels {
				return i
			}
		}
	}
	return -1
}

func parseMetricLabels(input string) (map[string]string, bool) {
	labels := make(map[string]string)
	for input != "" {
		equals := strings.IndexByte(input, '=')
		if equals <= 0 || equals+1 >= len(input) || input[equals+1] != '"' {
			return nil, false
		}
		name := input[:equals]
		if !validMetricLabelName(name) {
			return nil, false
		}
		if _, duplicate := labels[name]; duplicate {
			return nil, false
		}
		valueEnd := equals + 2
		for valueEnd < len(input) {
			if input[valueEnd] == '\\' {
				if valueEnd+1 >= len(input) || !strings.ContainsRune(`\"n`, rune(input[valueEnd+1])) {
					return nil, false
				}
				valueEnd += 2
				continue
			}
			if input[valueEnd] == '"' {
				break
			}
			valueEnd++
		}
		if valueEnd >= len(input) {
			return nil, false
		}
		value, err := strconv.Unquote(input[equals+1 : valueEnd+1])
		if err != nil {
			return nil, false
		}
		labels[name] = value
		input = input[valueEnd+1:]
		if input == "" {
			break
		}
		if input[0] != ',' {
			return nil, false
		}
		input = input[1:]
		if input == "" {
			return nil, false
		}
	}
	return labels, true
}

func validMetricLabelName(name string) bool {
	for i := 0; i < len(name); i++ {
		valid := name[i] == '_' || name[i] >= 'a' && name[i] <= 'z' || name[i] >= 'A' && name[i] <= 'Z'
		if i > 0 {
			valid = valid || name[i] >= '0' && name[i] <= '9'
		}
		if !valid {
			return false
		}
	}
	return name != ""
}

func matchMetricLabels(sampleLabels, filterLabels map[string]string) bool {
	for name, value := range filterLabels {
		sampleValue, exists := sampleLabels[name]
		if !exists || sampleValue != value {
			return false
		}
	}
	return true
}

func metricSamplesWithLabels(samples []metricSample, filterLabels map[string]string) []metricSample {
	var matches []metricSample
	for _, sample := range samples {
		if matchMetricLabels(sample.labels, filterLabels) {
			matches = append(matches, sample)
		}
	}
	return matches
}

func singleMetricValue(samples []metricSample) (float64, error) {
	if len(samples) != 1 {
		return 0, fmt.Errorf("expected exactly one metric sample, got %d", len(samples))
	}
	return samples[0].value, nil
}

func metricSumOrZero(t *testing.T, endpoint, metricName string) float64 {
	t.Helper()
	var total float64
	for _, sample := range readMetricSamples(t, endpoint, metricName) {
		total += sample.value
	}
	return total
}

func readProtocolCounters(t *testing.T, endpoint string) ProtocolCounters {
	t.Helper()
	return ProtocolCounters{
		SIPPackets:     metricSumOrZero(t, endpoint, "sip_exporter_packets_total"),
		RTPPackets:     metricSumOrZero(t, endpoint, "sip_exporter_rtp_packets_total"),
		RTCPReports:    metricSumOrZero(t, endpoint, "sip_exporter_rtcp_reports_total"),
		VQReports:      metricSumOrZero(t, endpoint, "sip_exporter_vq_reports_total"),
		SocketReceived: metricSumOrZero(t, endpoint, "sip_exporter_socket_packets_received_total"),
		SocketDropped:  metricSumOrZero(t, endpoint, "sip_exporter_socket_packets_dropped_total"),
	}
}

func waitForExactSIPCapture(
	ctx context.Context,
	t *testing.T,
	endpoint string,
	before, expected float64,
) {
	t.Helper()
	require.Eventually(t, func() bool {
		captured := metricSumOrZero(t, endpoint, "sip_exporter_packets_total") - before
		channelLength := metricSumOrZero(t, endpoint, "sip_exporter_channel_length")
		return exactCaptureComplete(expected, captured, channelLength)
	}, contextTimeout(t, ctx), 25*time.Millisecond,
		"SIP capture did not reach exact drained total %.0f", expected)
}

func absScenarioPath(t *testing.T, filename string) string {
	t.Helper()
	localPath := filepath.Join(projectRoot(), "test", "e2e", "load", "sipp", filename)
	if _, err := os.Stat(localPath); err == nil {
		return localPath
	}
	return filepath.Join(projectRoot(), "test", "e2e", "sipp", filename)
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

type startedSippContainer struct {
	testcontainers.Container
	statsDir       string
	evidencePrefix string
	started        time.Time
	evidenceSaved  bool
}

type startBarrier struct {
	done       chan struct{}
	once       sync.Once
	releasedAt time.Time
}

func newStartBarrier() *startBarrier {
	return &startBarrier{done: make(chan struct{})}
}

func (b *startBarrier) wait() time.Time {
	<-b.done
	return b.releasedAt
}

func (b *startBarrier) release(at time.Time) {
	b.once.Do(func() {
		b.releasedAt = at
		close(b.done)
	})
}

func validatePostPhaseOrdering(measureEnd time.Time, postPhase ...time.Time) error {
	for i, at := range postPhase {
		if !at.After(measureEnd) {
			return fmt.Errorf("post-phase timestamp %d must be after measurement end", i)
		}
	}
	return nil
}

func startSippContainer(
	ctx context.Context,
	t *testing.T,
	args []string,
	sippVol string,
	evidencePrefix string,
	waitForExit bool,
) *startedSippContainer {
	t.Helper()
	return newSippContainer(ctx, t, args, sippVol, evidencePrefix, waitForExit, true, "host")
}

func prepareSippContainer(
	ctx context.Context,
	t *testing.T,
	args []string,
	sippVol string,
	evidencePrefix string,
) *startedSippContainer {
	t.Helper()
	return newSippContainer(ctx, t, args, sippVol, evidencePrefix, false, false, "host")
}

func prepareSippContainerInPeerNetns(
	ctx context.Context,
	t *testing.T,
	args []string,
	sippVol, evidencePrefix, peerContainerID string,
) *startedSippContainer {
	t.Helper()
	return newSippContainer(
		ctx, t, args, sippVol, evidencePrefix, false, false, "container:"+peerContainerID,
	)
}

func newSippContainer(
	ctx context.Context,
	t *testing.T,
	args []string,
	sippVol string,
	evidencePrefix string,
	waitForExit, start bool,
	networkMode string,
) *startedSippContainer {
	t.Helper()

	started := time.Time{}
	if start {
		started = time.Now()
	}
	statsDir := ""
	recordingEvidence := evidencePrefix != "" && activeRunRecorder != nil
	if evidencePrefix != "" {
		statsDir = t.TempDir()
	}
	req := sippContainerRequest(ctx, t, args, sippVol, statsDir, waitForExit, networkMode)

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          start,
		Logger:           log.New(io.Discard, "", 0),
	})
	require.NoError(t, err)
	c := &startedSippContainer{
		Container: container, statsDir: statsDir, evidencePrefix: evidencePrefix, started: started,
	}

	if start && os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
		logs, logErr := c.Logs(ctx)
		if logErr == nil {
			defer logs.Close()
			logBytes, _ := io.ReadAll(logs)
			t.Logf("SIPp logs:\n%s", strings.TrimSpace(string(logBytes)))
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if recordingEvidence && !c.evidenceSaved {
			state, stateErr := c.State(cleanupCtx)
			if stateErr != nil {
				t.Errorf("read %s SIPp state during cleanup: %v", evidencePrefix, stateErr)
			} else {
				if state.Running {
					if stopErr := c.Stop(cleanupCtx, nil); stopErr != nil {
						t.Errorf("stop %s SIPp during cleanup: %v", evidencePrefix, stopErr)
					} else {
						c.recordEvidence(cleanupCtx, t, time.Now())
					}
				} else {
					c.recordEvidence(cleanupCtx, t, time.Now())
				}
			}
		}
		_ = c.Terminate(cleanupCtx)
	})

	return c
}

func startPreparedSippContainers(
	ctx context.Context,
	t *testing.T,
	containers ...*startedSippContainer,
) time.Time {
	t.Helper()
	barrier := newStartBarrier()
	errs := make(chan error, len(containers))
	for _, c := range containers {
		go func() {
			barrier.wait()
			errs <- c.Start(ctx)
		}()
	}
	releasedAt := time.Now()
	barrier.release(releasedAt)
	for range containers {
		require.NoError(t, <-errs)
	}
	startedAt := time.Time{}
	for _, c := range containers {
		inspection, err := c.Inspect(ctx)
		require.NoError(t, err)
		c.started, err = time.Parse(time.RFC3339Nano, inspection.State.StartedAt)
		require.NoError(t, err)
		if startedAt.IsZero() || c.started.Before(startedAt) {
			startedAt = c.started
		}
	}
	return startedAt
}

func (c *startedSippContainer) recordEvidence(
	ctx context.Context,
	t *testing.T,
	finished time.Time,
) {
	t.Helper()
	if c.evidencePrefix == "" || activeRunRecorder == nil || c.evidenceSaved {
		return
	}
	phases := PhaseTimestamps{
		WarmupStart:  c.started,
		Ready:        c.started,
		MeasureStart: c.started,
		MeasureEnd:   finished,
		DrainEnd:     finished,
	}
	result, err := c.readGeneratorEvidence(ctx, t, phases)
	if err != nil {
		t.Errorf("read %s SIPp evidence: %v", c.evidencePrefix, err)
		return
	}
	if err := activeRunRecorder.AttachGenerator(t.Name(), result); err != nil {
		t.Errorf("attach %s generator evidence: %v", c.evidencePrefix, err)
	}
}

func (c *startedSippContainer) readGeneratorEvidence(
	ctx context.Context,
	t *testing.T,
	phases PhaseTimestamps,
) (GeneratorResult, error) {
	t.Helper()
	state, err := c.State(ctx)
	if err != nil {
		return GeneratorResult{}, fmt.Errorf("read SIPp state: %w", err)
	}
	if state.Running {
		return GeneratorResult{}, fmt.Errorf("record SIPp evidence before exit")
	}
	recordContainerLogs(ctx, t, c.evidencePrefix+".log", c)
	stats, err := os.ReadFile(filepath.Join(c.statsDir, "stats.csv"))
	if err != nil {
		return GeneratorResult{ExitCode: int(state.ExitCode), Phases: phases},
			fmt.Errorf("read SIPp statistics: %w", err)
	}
	recordScenarioArtifact(t, c.evidencePrefix+"-stats.csv", stats)
	result, err := parseSIPpStats(stats, int(state.ExitCode), phases)
	if err != nil {
		return GeneratorResult{}, fmt.Errorf("parse SIPp statistics: %w", err)
	}
	c.evidenceSaved = true
	return result, nil
}

func sippContainerRequest(
	ctx context.Context,
	t *testing.T,
	args []string,
	sippVol, statsDir string,
	waitForExit bool,
	networkMode string,
) testcontainers.ContainerRequest {
	t.Helper()
	cmd := append([]string(nil), args...)
	mounts := testcontainers.Mounts(testcontainers.BindMount(sippVol, "/scenarios"))
	if statsDir != "" {
		cmd = append(cmd, "-trace_stat", "-stat_delimiter", ";", "-stf", "/artifacts/stats.csv")
		mounts = append(mounts, testcontainers.BindMount(statsDir, "/artifacts"))
	}

	req := testcontainers.ContainerRequest{
		Image:       sippImage,
		NetworkMode: container.NetworkMode(networkMode),
		Cmd:         cmd,
		Mounts:      mounts,
	}

	if waitForExit {
		req.WaitingFor = wait.ForExit().WithExitTimeout(contextTimeout(t, ctx))
	}

	return req
}

func runSippGenerator(
	ctx context.Context,
	t *testing.T,
	args []string,
	sippVol string,
	phases PhaseTimestamps,
) (*startedSippContainer, PhaseTimestamps) {
	return runNamedSippGenerator(ctx, t, args, sippVol, "generator", phases)
}

func runNamedSippGenerator(
	ctx context.Context,
	t *testing.T,
	args []string,
	sippVol, evidencePrefix string,
	phases PhaseTimestamps,
) (*startedSippContainer, PhaseTimestamps) {
	t.Helper()
	c := startSippContainer(ctx, t, args, sippVol, evidencePrefix, true)
	phases.MeasureEnd = time.Now()
	return c, phases
}

func waitForSIPpUDPReady(
	ctx context.Context,
	t *testing.T,
	c testcontainers.Container,
	port string,
) {
	t.Helper()
	require.Eventually(t, func() bool {
		state, err := c.State(ctx)
		if err != nil || !state.Running {
			return false
		}
		conn, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", port))
		if err != nil {
			return errors.Is(err, syscall.EADDRINUSE)
		}
		_ = conn.Close()
		return false
	}, contextTimeout(t, ctx), 25*time.Millisecond, "SIPp UAS did not bind UDP port %s", port)
}

func waitForContainerExit(ctx context.Context, t *testing.T, c testcontainers.Container) {
	t.Helper()
	require.Eventually(t, func() bool {
		state, err := c.State(ctx)
		if err != nil {
			return false
		}
		return !state.Running
	}, contextTimeout(t, ctx), 500*time.Millisecond, "SIPp container did not exit in time")
}

func newSteadyMeasurement(ctx context.Context, env *testEnv) (*steadyMeasurement, error) {
	if err := waitForMeasurementMetrics(ctx, env.endpoint); err != nil {
		return nil, err
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	inspection, err := cli.ContainerInspect(ctx, env.exporterContainer.GetContainerID(),
		client.ContainerInspectOptions{})
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("inspect exporter container: %w", err)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, inspection.Container.State.StartedAt)
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("parse exporter start time: %w", err)
	}
	measurementCtx, cancel := context.WithCancel(ctx)
	measurement := &steadyMeasurement{
		env: env, dockerCli: cli, cancel: cancel, done: make(chan struct{}),
		containerStart: startedAt,
	}
	go measurement.collect(measurementCtx)
	return measurement, nil
}

func waitForMeasurementMetrics(ctx context.Context, endpoint string) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		body, err := fetchMetricsBodyContext(ctx, endpoint)
		if err == nil {
			_, err = metricSamplePointFromBody(time.Now(), body)
		}
		if err == nil {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for measurement metrics: %v: %w", lastErr, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (m *steadyMeasurement) Begin(ctx context.Context, at time.Time) error {
	if at.IsZero() {
		return fmt.Errorf("measurement start is zero")
	}
	body, err := fetchMetricsBodyContext(ctx, m.env.endpoint)
	if err != nil {
		return err
	}
	point, err := metricSamplePointFromBody(at, body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.measuring || !m.start.IsZero() {
		return fmt.Errorf("measurement already started")
	}
	m.start = at
	m.measuring = true
	m.samples.Metrics = append(m.samples.Metrics, point)
	return nil
}

func (m *steadyMeasurement) WaitForSamples(
	ctx context.Context,
	minimumResources, minimumMetrics int,
) error {
	if minimumResources <= 0 || minimumMetrics <= 0 {
		return fmt.Errorf("minimum sample counts must be positive")
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		resourceCount := len(m.samples.Resources)
		metricCount := len(m.samples.Metrics)
		measurementErr := m.err
		resourcesReady := false
		if resourceCount >= minimumResources {
			_, resourceErr := throttlingPercent(
				m.samples.Resources[0], m.samples.Resources[resourceCount-1],
			)
			resourcesReady = resourceErr == nil
		}
		m.mu.Unlock()
		if measurementErr != nil {
			return measurementErr
		}
		if resourcesReady && metricCount >= minimumMetrics {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *steadyMeasurement) End(ctx context.Context, at time.Time) (
	ResourceSummaryV2, ResourceSamplesV2, error,
) {
	m.mu.Lock()
	if !m.measuring || m.ending || !at.After(m.start) {
		m.mu.Unlock()
		return ResourceSummaryV2{}, ResourceSamplesV2{}, fmt.Errorf("invalid measurement end")
	}
	m.ending = true
	start := m.start
	m.mu.Unlock()
	boundaryPoint, boundaryErr := m.fetchMetricPoint(ctx, at.Add(-time.Nanosecond))
	m.mu.Lock()
	if boundaryErr == nil {
		m.samples.Metrics = append(m.samples.Metrics, boundaryPoint)
	}
	m.measuring = false
	m.mu.Unlock()
	m.cancel()
	<-m.done

	logs, err := m.env.exporterContainer.Logs(ctx)
	if err != nil {
		return ResourceSummaryV2{}, ResourceSamplesV2{}, fmt.Errorf("read exporter logs: %w", err)
	}
	logBytes, readErr := io.ReadAll(logs)
	closeErr := logs.Close()
	if readErr != nil {
		return ResourceSummaryV2{}, ResourceSamplesV2{}, fmt.Errorf("read exporter log body: %w", readErr)
	}
	if closeErr != nil {
		return ResourceSummaryV2{}, ResourceSamplesV2{}, fmt.Errorf("close exporter logs: %w", closeErr)
	}
	m.mu.Lock()
	measurementErr := m.err
	samples := m.samples
	m.mu.Unlock()
	if measurementErr != nil {
		return ResourceSummaryV2{}, ResourceSamplesV2{}, measurementErr
	}
	if boundaryErr != nil {
		return ResourceSummaryV2{}, ResourceSamplesV2{}, boundaryErr
	}
	samples.GCPauses, err = parseGCPauseSamples(string(logBytes), m.containerStart, start, at)
	if err != nil {
		return ResourceSummaryV2{}, ResourceSamplesV2{}, err
	}
	summary, err := summarizeResources(samples, start, at, m.env.limits)
	return summary, samples, err
}

func (m *steadyMeasurement) collect(ctx context.Context) {
	defer close(m.done)
	defer m.dockerCli.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		m.collectDockerStats(ctx)
	}()
	go func() {
		defer wg.Done()
		m.collectSelfMetrics(ctx)
	}()
	wg.Wait()
}

func (m *steadyMeasurement) collectDockerStats(ctx context.Context) {
	resp, err := m.dockerCli.ContainerStats(ctx, m.env.exporterContainer.GetContainerID(),
		client.ContainerStatsOptions{Stream: true})
	if err != nil {
		m.setMeasurementError(err)
		return
	}
	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	for {
		var stats container.StatsResponse
		if err := decoder.Decode(&stats); err != nil {
			if ctx.Err() == nil {
				m.setMeasurementError(fmt.Errorf("decode Docker stats: %w", err))
			}
			return
		}
		m.mu.Lock()
		measuring := m.measuring
		m.mu.Unlock()
		if !measuring {
			continue
		}
		sample, err := resourceSampleFromStats(stats.Read, stats, m.env.limits)
		if err != nil {
			m.setMeasurementError(err)
			continue
		}
		m.mu.Lock()
		m.samples.Resources = append(m.samples.Resources, sample)
		m.mu.Unlock()
	}
}

func (m *steadyMeasurement) collectSelfMetrics(ctx context.Context) {
	ticker := time.NewTicker(selfMetricSampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-ticker.C:
			m.mu.Lock()
			measuring := m.measuring
			m.mu.Unlock()
			if !measuring {
				continue
			}
			if err := m.sampleMetricPoint(ctx, at); err != nil {
				m.setMeasurementError(err)
			}
		}
	}
}

func (m *steadyMeasurement) sampleMetricPoint(ctx context.Context, at time.Time) error {
	point, err := m.fetchMetricPoint(ctx, at)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.measuring || m.ending {
		return nil
	}
	m.samples.Metrics = append(m.samples.Metrics, point)
	return nil
}

func (m *steadyMeasurement) fetchMetricPoint(ctx context.Context, at time.Time) (metricSamplePoint, error) {
	body, err := fetchMetricsBodyContext(ctx, m.env.endpoint)
	if err != nil {
		return metricSamplePoint{}, err
	}
	point, err := metricSamplePointFromBody(at, body)
	if err != nil {
		return metricSamplePoint{}, err
	}
	return point, nil
}

func (m *steadyMeasurement) setMeasurementError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if (m.measuring || m.start.IsZero()) && m.err == nil {
		m.err = err
	}
}

func fetchMetricsBodyContext(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/metrics", nil)
	if err != nil {
		return nil, fmt.Errorf("create metrics request: %w", err)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch metrics: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read metrics body: %w", err)
	}
	return body, nil
}

func recordRawResourceSamples(t *testing.T, samples ResourceSamplesV2) {
	t.Helper()
	if activeRunRecorder == nil {
		return
	}
	data, err := json.MarshalIndent(samples, "", "  ")
	if err != nil {
		t.Fatalf("marshal resource samples: %v", err)
	}
	recordScenarioArtifact(t, "resource-samples.json", data)
}

func finishSteadyMeasurement(
	ctx context.Context,
	t *testing.T,
	measurement *steadyMeasurement,
	end time.Time,
) ResourceSummaryV2 {
	summary, _ := finishSteadyMeasurementWithSamples(ctx, t, measurement, end)
	return summary
}

func finishSteadyMeasurementWithSamples(
	ctx context.Context,
	t *testing.T,
	measurement *steadyMeasurement,
	end time.Time,
) (ResourceSummaryV2, ResourceSamplesV2) {
	t.Helper()
	summary, samples, err := measurement.End(ctx, end)
	require.NoError(t, err)
	require.NoError(t, validateAbsoluteResourceGates(summary))
	recordRawResourceSamples(t, samples)
	return summary, samples
}

func runSippWarmup(
	ctx context.Context,
	t *testing.T,
	uasScenario, uacScenario string,
	profile releaseProfileSpec,
	env *testEnv,
) {
	t.Helper()
	recordMetricsSnapshot(t, "metrics-warmup-before.prom", env.endpoint)
	protocolsBefore := readProtocolCounters(t, env.endpoint)
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")
	expected := float64(profile.Workload.Calls) * profile.PacketsPerCall

	uasPath := absScenarioPath(t, uasScenario)
	sippVol := filepath.Dir(uasPath)
	uas := startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/" + filepath.Base(uasScenario), "-i", "127.0.0.1", "-p", env.sippPort,
			"-m", strconv.Itoa(profile.Workload.Calls), "-nr", "-nostdin"},
		sippVol, "", false,
	)
	waitForSIPpUDPReady(ctx, t, uas, env.sippPort)

	phases := PhaseTimestamps{WarmupStart: time.Now(), Ready: time.Now(), MeasureStart: time.Now()}
	uacPath := absScenarioPath(t, uacScenario)
	generator, phases := runNamedSippGenerator(ctx, t,
		[]string{"-sf", "/scenarios/" + filepath.Base(uacScenario), "-i", "127.0.0.1", "-p", env.sippClientPort,
			"-m", strconv.Itoa(profile.Workload.Calls), "-r", strconv.Itoa(int(profile.Workload.Rate)), "-nr",
			"127.0.0.1:" + env.sippPort},
		filepath.Dir(uacPath), "warmup-generator", phases,
	)
	waitForContainerExit(ctx, t, uas)
	waitForExactSIPCapture(ctx, t, env.endpoint, protocolsBefore.SIPPackets, expected)
	phases.DrainEnd = time.Now()
	postDrain, postDrainBody, err := waitForPostDrainSnapshot(ctx, env.endpoint)
	require.NoError(t, err)
	require.NoError(t, postDrain.Validate())
	recordScenarioArtifact(t, "metrics-warmup-post-drain.prom", postDrainBody)
	generatorResult, err := generator.readGeneratorEvidence(ctx, t, phases)
	require.NoError(t, err)
	protocolsAfter := readProtocolCounters(t, env.endpoint)
	evidence := soakWarmupEvidence{
		Generator:  generatorResult,
		Capture:    newCaptureResult(expected, protocolsAfter.SIPPackets-protocolsBefore.SIPPackets),
		Protocols:  protocolsAfter.delta(protocolsBefore),
		ErrorCount: getMetric(t, env.endpoint, "sip_exporter_system_error_total") - errorsBefore,
	}
	require.NoError(t, validateReleaseSoakWarmup(profile, evidence))
	recordMetricsSnapshot(t, "metrics-warmup-after.prom", env.endpoint)
}

func measureSteadySnapshot(
	ctx context.Context,
	t *testing.T,
	env *testEnv,
) ResourceSummaryV2 {
	t.Helper()
	measurement, err := newSteadyMeasurement(ctx, env)
	require.NoError(t, err)
	measureStart := time.Now()
	require.NoError(t, measurement.Begin(ctx, measureStart))
	require.NoError(t, measurement.WaitForSamples(ctx, 2, 2))
	return finishSteadyMeasurement(ctx, t, measurement, time.Now())
}

func runSippLoad(
	ctx context.Context,
	t *testing.T,
	uasScenario, uacScenario string,
	callCount, rate int,
	packetsPerCall float64,
	env *testEnv,
) loadResult {
	t.Helper()

	measurement, measurementErr := newSteadyMeasurement(ctx, env)
	require.NoError(t, measurementErr)

	recordMetricsSnapshot(t, "metrics-before.prom", env.endpoint)
	protocolsBefore := readProtocolCounters(t, env.endpoint)
	packetsBefore := protocolsBefore.SIPPackets
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")
	expectedTotal := float64(callCount) * packetsPerCall

	phases := PhaseTimestamps{WarmupStart: time.Now()}
	var generator GeneratorResult
	var generatorContainer *startedSippContainer
	var uasContainer *startedSippContainer
	spec := WorkloadSpec{Calls: callCount, Rate: float64(rate)}

	if uasScenario != "" {
		uasPath := absScenarioPath(t, uasScenario)
		sippVol := filepath.Dir(uasPath)
		uasFile := filepath.Base(uasScenario)

		uasContainer = startSippContainer(ctx, t,
			[]string{"-sf", "/scenarios/" + uasFile, "-i", "127.0.0.1", "-p", env.sippPort,
				"-m", strconv.Itoa(callCount), "-nr", "-nostdin"},
			sippVol, "", false,
		)

		waitForSIPpUDPReady(ctx, t, uasContainer, env.sippPort)
		phases.Ready = time.Now()

		uacPath := absScenarioPath(t, uacScenario)
		sippVol = filepath.Dir(uacPath)
		uacFile := filepath.Base(uacScenario)

		phases.MeasureStart = time.Now()
		require.NoError(t, measurement.Begin(ctx, phases.MeasureStart))
		generatorContainer, phases = runSippGenerator(ctx, t,
			[]string{"-sf", "/scenarios/" + uacFile, "-i", "127.0.0.1", "-p", env.sippClientPort,
				"-m", strconv.Itoa(callCount), "-r", strconv.Itoa(rate),
				"-nr",
				"127.0.0.1:" + env.sippPort},
			sippVol, phases,
		)
	} else {
		uacPath := absScenarioPath(t, uacScenario)
		sippVol := filepath.Dir(uacPath)
		uacFile := filepath.Base(uacScenario)

		phases.Ready = time.Now()
		phases.MeasureStart = time.Now()
		require.NoError(t, measurement.Begin(ctx, phases.MeasureStart))
		generatorContainer, phases = runSippGenerator(ctx, t,
			[]string{"-sf", "/scenarios/" + uacFile, "-i", "127.0.0.1", "-p", env.sippClientPort,
				"-m", strconv.Itoa(callCount), "-r", strconv.Itoa(rate),
				"-nr",
				"127.0.0.1:" + env.sippPort},
			sippVol, phases,
		)
	}

	measureEnd := phases.MeasureEnd
	resourceSummary, resourceSamples := finishSteadyMeasurementWithSamples(ctx, t, measurement, measureEnd)
	postPhase := make([]time.Time, 0, 3)
	if uasContainer != nil {
		waitForContainerExit(ctx, t, uasContainer)
		postPhase = append(postPhase, time.Now())
	}
	var generatorErr error
	generator, generatorErr = generatorContainer.readGeneratorEvidence(ctx, t, phases)
	require.NoError(t, generatorErr)
	postPhase = append(postPhase, time.Now())

	waitForExactSIPCapture(ctx, t, env.endpoint, packetsBefore, expectedTotal)
	generator.Phases.DrainEnd = time.Now()
	postPhase = append(postPhase, generator.Phases.DrainEnd)
	require.NoError(t, validatePostPhaseOrdering(measureEnd, postPhase...))
	require.NoError(t, generator.Validate(spec))
	sippDuration := generator.Phases.MeasureEnd.Sub(generator.Phases.MeasureStart)
	drainTime := generator.Phases.DrainEnd.Sub(generator.Phases.MeasureEnd)

	recordMetricsSnapshot(t, "metrics-after.prom", env.endpoint)
	protocolsAfter := readProtocolCounters(t, env.endpoint)
	protocols := protocolsAfter.delta(protocolsBefore)
	packetsAfter := protocolsAfter.SIPPackets
	errorsAfter := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	capture := newCaptureResult(expectedTotal, protocols.SIPPackets)
	require.NoError(t, capture.ValidateExact())
	totalCaptured := capture.Captured
	actualPPS := 0.0
	if sippDuration.Seconds() > 0 {
		actualPPS = totalCaptured / sippDuration.Seconds()
	}
	expectedPPS := float64(rate) * packetsPerCall

	result := loadResult{
		Duration:        sippDuration,
		Generator:       generator,
		Capture:         capture,
		Protocols:       protocols,
		PacketsBefore:   packetsBefore,
		PacketsAfter:    packetsAfter,
		ActualPPS:       actualPPS,
		ExpectedPPS:     expectedPPS,
		LossRate:        capture.LossPct / 100,
		ErrorCount:      errorsAfter - errorsBefore,
		DrainTime:       drainTime,
		Resources:       resourceSummary,
		ResourceSamples: resourceSamples,
	}
	recordLoadResultEvidence(t, result)

	t.Logf("Load result: actual=%.0f PPS, captured=%.0f, expected=%.0f, loss=%.2f%%, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB, errors=%.0f",
		result.ActualPPS, totalCaptured, expectedTotal, result.LossRate*100, result.DrainTime,
		result.Resources.CPUP95Percent, result.Resources.CPUP95Percent,
		result.Resources.WorkingSetP99MB, result.ErrorCount)

	return result
}

func runConcurrentLoad(
	ctx context.Context,
	t *testing.T,
	uasScenario, uacScenario string,
	callCount, rate, limit int,
	env *testEnv,
) loadResult {
	t.Helper()

	measurement, measurementErr := newSteadyMeasurement(ctx, env)
	require.NoError(t, measurementErr)

	recordMetricsSnapshot(t, "metrics-before.prom", env.endpoint)
	protocolsBefore := readProtocolCounters(t, env.endpoint)
	packetsBefore := protocolsBefore.SIPPackets
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")
	expectedTotal := float64(callCount) * fullCallPacketsPerCall

	uasPath := absScenarioPath(t, uasScenario)
	sippVol := filepath.Dir(uasPath)
	uasFile := filepath.Base(uasScenario)

	uasContainer := startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/" + uasFile, "-i", "127.0.0.1", "-p", env.sippPort,
			"-m", strconv.Itoa(callCount), "-nr", "-nostdin"},
		sippVol, "", false,
	)
	waitForSIPpUDPReady(ctx, t, uasContainer, env.sippPort)

	uacPath := absScenarioPath(t, uacScenario)
	sippVol = filepath.Dir(uacPath)
	uacFile := filepath.Base(uacScenario)

	phases := PhaseTimestamps{WarmupStart: time.Now(), Ready: time.Now()}
	uacContainer := prepareSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/" + uacFile, "-i", "127.0.0.1", "-p", env.sippClientPort,
			"-m", strconv.Itoa(callCount), "-r", strconv.Itoa(rate),
			"-fd", "100ms",
			"-nr",
			"-l", strconv.Itoa(limit),
			"127.0.0.1:" + env.sippPort},
		sippVol, "generator",
	)
	require.NoError(t, measurement.Begin(ctx, time.Now()))
	phases.MeasureStart = startPreparedSippContainers(ctx, t, uacContainer)

	var peakSessions float64
	require.Eventually(t, func() bool {
		if metricExists(t, env.endpoint, "sip_exporter_sessions") {
			sessions := getMetric(t, env.endpoint, "sip_exporter_sessions")
			if sessions > peakSessions {
				peakSessions = sessions
			}
			if sessions == float64(limit) {
				return true
			}
		}
		state, stateErr := uacContainer.State(ctx)
		if stateErr != nil {
			return false
		}
		return !state.Running
	}, contextTimeout(t, ctx), 25*time.Millisecond, "concurrent sessions did not reach exactly %d", limit)
	waitForContainerExit(ctx, t, uacContainer)
	measureEnd := time.Now()
	phases.MeasureEnd = measureEnd
	resourceSummary := finishSteadyMeasurement(ctx, t, measurement, measureEnd)
	generator, generatorErr := uacContainer.readGeneratorEvidence(ctx, t, phases)
	require.NoError(t, generatorErr)
	sippDuration := measureEnd.Sub(generator.Phases.MeasureStart)
	evidenceAt := time.Now()

	waitForContainerExit(ctx, t, uasContainer)
	uasExitAt := time.Now()

	waitForExactSIPCapture(ctx, t, env.endpoint, packetsBefore, expectedTotal)

	stableTime := time.Now()
	require.NoError(t, validatePostPhaseOrdering(measureEnd, evidenceAt, uasExitAt, stableTime))
	drainTime := stableTime.Sub(measureEnd)
	generator.Phases.DrainEnd = stableTime
	generator.ActualRate, generatorErr = sippRampRate(callCount, generator.startedAt, generator.rampEndAt)
	require.NoError(t, generatorErr)
	require.NoError(t, generator.Validate(WorkloadSpec{Calls: callCount, Rate: float64(rate)}))

	recordMetricsSnapshot(t, "metrics-after.prom", env.endpoint)
	protocolsAfter := readProtocolCounters(t, env.endpoint)
	protocols := protocolsAfter.delta(protocolsBefore)
	packetsAfter := protocolsAfter.SIPPackets
	errorsAfter := getMetric(t, env.endpoint, "sip_exporter_system_error_total")
	capture := newCaptureResult(expectedTotal, protocols.SIPPackets)
	require.NoError(t, capture.ValidateExact())

	actualPPS := 0.0
	if sippDuration.Seconds() > 0 {
		actualPPS = (packetsAfter - packetsBefore) / sippDuration.Seconds()
	}

	result := loadResult{
		Duration:      sippDuration,
		Generator:     generator,
		Capture:       capture,
		Protocols:     protocols,
		PacketsBefore: packetsBefore,
		PacketsAfter:  packetsAfter,
		ActualPPS:     actualPPS,
		ErrorCount:    errorsAfter - errorsBefore,
		DrainTime:     drainTime,
		PeakSessions:  peakSessions,
		Resources:     resourceSummary,
	}
	recordLoadResultEvidence(t, result)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")

	t.Logf("Concurrent result: actual=%.0f PPS, peak_sessions=%.0f, invites=%.0f, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB, duration=%v",
		result.ActualPPS, peakSessions, inviteTotal, result.DrainTime,
		result.Resources.CPUP95Percent, result.Resources.CPUP95Percent,
		result.Resources.WorkingSetP99MB, result.Duration)

	return result
}
