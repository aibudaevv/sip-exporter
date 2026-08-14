//go:build e2e

package load

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	sippImage     = "pbertera/sipp@sha256:063e8e9c8ecf54552e8efc3c363007afbfd3cae5a0f3f037db1c2e7fa4cd0349"
	testInterface = "lo"
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
	}

	loadResult struct {
		Duration      time.Duration
		Generator     GeneratorResult
		Capture       CaptureResult
		Protocols     ProtocolCounters
		PacketsBefore float64
		PacketsAfter  float64
		ActualPPS     float64
		ExpectedPPS   float64
		LossRate      float64
		ErrorCount    float64
		DrainTime     time.Duration
		CPUAvg        float64
		CPUPeak       float64
		MemMaxMB      float64
		PeakSessions  float64
	}

	statsCollector struct {
		mu          sync.Mutex
		samples     []float64
		memSamples  []float64
		cancel      context.CancelFunc
		done        chan struct{}
		dockerCli   *client.Client
		containerID string
		firstUsage  uint64
		lastUsage   uint64
		firstSys    uint64
		lastSys     uint64
		numCPU      int
		firstTime   time.Time
		lastTime    time.Time
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
) testcontainers.ContainerRequest {
	t.Helper()
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
	if goDebug := os.Getenv("SIP_EXPORTER_E2E_GODEBUG"); goDebug != "" {
		envVars["GODEBUG"] = goDebug
	}
	return testcontainers.ContainerRequest{
		Image:       exporterImage(),
		Privileged:  true,
		NetworkMode: "host",
		Env:         envVars,
		WaitingFor: wait.ForHTTP("/metrics").
			WithPort(httpPort + "/tcp").
			WithStartupTimeout(contextTimeout(t, ctx)),
	}
}

func newTestEnv(ctx context.Context, t *testing.T) *testEnv {
	t.Helper()
	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	req := exporterContainerRequest(ctx, t, testInterface, exporterHTTPPort, sippPort)

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

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
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
		ctx, t, testInterface, exporterHTTPPort, sippPort+","+sippPort2,
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

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
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

	var samples []metricSample
	for _, line := range strings.Split(string(fetchMetricsBody(t, endpoint)), "\n") {
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

func startSippContainer(
	ctx context.Context,
	t *testing.T,
	args []string,
	sippVol string,
	waitForExit bool,
) testcontainers.Container {
	t.Helper()

	req := sippContainerRequest(ctx, t, args, sippVol, "", waitForExit)

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           log.New(io.Discard, "", 0),
	})
	require.NoError(t, err)

	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
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
		_ = c.Terminate(cleanupCtx)
	})

	return c
}

func sippContainerRequest(
	ctx context.Context,
	t *testing.T,
	args []string,
	sippVol, statsDir string,
	waitForExit bool,
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
		NetworkMode: "host",
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
) (GeneratorResult, error) {
	t.Helper()
	statsDir := t.TempDir()
	req := sippContainerRequest(ctx, t, args, sippVol, statsDir, true)
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           log.New(io.Discard, "", 0),
	})
	if err != nil {
		return GeneratorResult{}, fmt.Errorf("start SIPp generator: %w", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Terminate(cleanupCtx)
	})

	phases.MeasureEnd = time.Now()
	state, err := c.State(ctx)
	if err != nil {
		return GeneratorResult{}, fmt.Errorf("read SIPp generator state: %w", err)
	}
	if state.ExitCode != 0 || os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
		logs, logErr := c.Logs(ctx)
		if logErr == nil {
			defer logs.Close()
			logBytes, _ := io.ReadAll(logs)
			t.Logf("SIPp generator logs:\n%s", strings.TrimSpace(string(logBytes)))
		}
	}

	stats, err := os.ReadFile(filepath.Join(statsDir, "stats.csv"))
	if err != nil {
		return GeneratorResult{ExitCode: int(state.ExitCode), Phases: phases},
			fmt.Errorf("read SIPp statistics: %w", err)
	}
	return parseSIPpStats(stats, int(state.ExitCode), phases)
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

func newStatsCollector(containerID string) (*statsCollector, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &statsCollector{
		samples:    make([]float64, 0),
		memSamples: make([]float64, 0),
		done:       make(chan struct{}),
		dockerCli:  cli,
		numCPU:     1,
	}, nil
}

func (s *statsCollector) start(ctx context.Context, containerID string) {
	s.containerID = containerID
	ctx, s.cancel = context.WithCancel(ctx)
	go func() {
		defer close(s.done)
		resp, err := s.dockerCli.ContainerStats(ctx, containerID, client.ContainerStatsOptions{Stream: true})
		if err != nil {
			return
		}
		defer resp.Body.Close()

		decoder := json.NewDecoder(resp.Body)
		firstFrame := true

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			var v container.StatsResponse
			if err := decoder.Decode(&v); err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}

			if firstFrame {
				s.firstUsage = v.CPUStats.CPUUsage.TotalUsage
				s.firstSys = v.CPUStats.SystemUsage
				s.firstTime = time.Now()
				s.numCPU = len(v.CPUStats.CPUUsage.PercpuUsage)
				if s.numCPU == 0 {
					s.numCPU = 1
				}
				firstFrame = false
				continue
			}

			s.lastUsage = v.CPUStats.CPUUsage.TotalUsage
			s.lastSys = v.CPUStats.SystemUsage
			s.lastTime = time.Now()

			cpuDelta := float64(v.CPUStats.CPUUsage.TotalUsage - v.PreCPUStats.CPUUsage.TotalUsage)
			sysDelta := float64(v.CPUStats.SystemUsage - v.PreCPUStats.SystemUsage)
			cpuPct := 0.0
			if sysDelta > 0 && cpuDelta > 0 {
				cpuPct = (cpuDelta / sysDelta) * float64(s.numCPU) * 100.0
			}

			memMB := float64(v.MemoryStats.Usage) / (1024.0 * 1024.0)

			s.mu.Lock()
			s.samples = append(s.samples, cpuPct)
			s.memSamples = append(s.memSamples, memMB)
			s.mu.Unlock()
		}
	}()
}

func (s *statsCollector) stop() (cpuAvg, cpuPeak, memMaxMB float64) {
	s.cancel()
	<-s.done
	s.dockerCli.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	memMaxMB = 0
	for _, m := range s.memSamples {
		if m > memMaxMB {
			memMaxMB = m
		}
	}

	if len(s.samples) == 0 {
		return 0, 0, memMaxMB
	}

	var cpuSum float64
	cpuPeak = 0
	for _, pct := range s.samples {
		cpuSum += pct
		if pct > cpuPeak {
			cpuPeak = pct
		}
	}
	perSampleAvg := cpuSum / float64(len(s.samples))

	wallDelta := s.lastTime.Sub(s.firstTime).Seconds()
	if wallDelta > 0 && s.numCPU > 0 {
		usageDelta := float64(s.lastUsage - s.firstUsage)
		sysDelta := float64(s.lastSys - s.firstSys)
		if sysDelta > 0 {
			cumulativeAvg := (usageDelta / sysDelta) * float64(s.numCPU) * 100.0
			if cumulativeAvg > 0 {
				return cumulativeAvg, cpuPeak, memMaxMB
			}
		}
	}

	return perSampleAvg, cpuPeak, memMaxMB
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

	stats, statsErr := newStatsCollector(env.exporterContainer.GetContainerID())
	require.NoError(t, statsErr)

	statsCtx, statsCancel := context.WithCancel(ctx)
	stats.start(statsCtx, env.exporterContainer.GetContainerID())
	statsStopped := false
	defer func() {
		if !statsStopped {
			statsCancel()
			stats.stop()
		}
	}()

	protocolsBefore := readProtocolCounters(t, env.endpoint)
	packetsBefore := protocolsBefore.SIPPackets
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")
	expectedTotal := float64(callCount) * packetsPerCall

	phases := PhaseTimestamps{WarmupStart: time.Now()}
	var generator GeneratorResult
	spec := WorkloadSpec{Calls: callCount, Rate: float64(rate)}

	if uasScenario != "" {
		uasPath := absScenarioPath(t, uasScenario)
		sippVol := filepath.Dir(uasPath)
		uasFile := filepath.Base(uasScenario)

		uasContainer := startSippContainer(ctx, t,
			[]string{"-sf", "/scenarios/" + uasFile, "-i", "127.0.0.1", "-p", env.sippPort,
				"-m", strconv.Itoa(callCount), "-nr", "-nostdin"},
			sippVol, false,
		)

		waitForSIPpUDPReady(ctx, t, uasContainer, env.sippPort)
		phases.Ready = time.Now()

		uacPath := absScenarioPath(t, uacScenario)
		sippVol = filepath.Dir(uacPath)
		uacFile := filepath.Base(uacScenario)

		phases.MeasureStart = time.Now()
		var generatorErr error
		generator, generatorErr = runSippGenerator(ctx, t,
			[]string{"-sf", "/scenarios/" + uacFile, "-i", "127.0.0.1", "-p", env.sippClientPort,
				"-m", strconv.Itoa(callCount), "-r", strconv.Itoa(rate),
				"-nr",
				"127.0.0.1:" + env.sippPort},
			sippVol, phases,
		)
		require.NoError(t, generatorErr)

		waitForContainerExit(ctx, t, uasContainer)
	} else {
		uacPath := absScenarioPath(t, uacScenario)
		sippVol := filepath.Dir(uacPath)
		uacFile := filepath.Base(uacScenario)

		phases.Ready = time.Now()
		phases.MeasureStart = time.Now()
		var generatorErr error
		generator, generatorErr = runSippGenerator(ctx, t,
			[]string{"-sf", "/scenarios/" + uacFile, "-i", "127.0.0.1", "-p", env.sippClientPort,
				"-m", strconv.Itoa(callCount), "-r", strconv.Itoa(rate),
				"-nr",
				"127.0.0.1:" + env.sippPort},
			sippVol, phases,
		)
		require.NoError(t, generatorErr)
	}

	waitForExactSIPCapture(ctx, t, env.endpoint, packetsBefore, expectedTotal)

	generator.Phases.DrainEnd = time.Now()
	require.NoError(t, generator.Validate(spec))
	sippDuration := generator.Phases.MeasureEnd.Sub(generator.Phases.MeasureStart)
	drainTime := generator.Phases.DrainEnd.Sub(generator.Phases.MeasureEnd)

	statsCancel()
	cpuAvg, cpuPeak, memMaxMB := stats.stop()
	statsStopped = true

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
		Duration:      sippDuration,
		Generator:     generator,
		Capture:       capture,
		Protocols:     protocols,
		PacketsBefore: packetsBefore,
		PacketsAfter:  packetsAfter,
		ActualPPS:     actualPPS,
		ExpectedPPS:   expectedPPS,
		LossRate:      capture.LossPct / 100,
		ErrorCount:    errorsAfter - errorsBefore,
		DrainTime:     drainTime,
		CPUAvg:        cpuAvg,
		CPUPeak:       cpuPeak,
		MemMaxMB:      memMaxMB,
	}

	t.Logf("Load result: actual=%.0f PPS, captured=%.0f, expected=%.0f, loss=%.2f%%, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB, errors=%.0f",
		result.ActualPPS, totalCaptured, expectedTotal, result.LossRate*100, result.DrainTime,
		result.CPUAvg, result.CPUPeak, result.MemMaxMB, result.ErrorCount)

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

	stats, statsErr := newStatsCollector(env.exporterContainer.GetContainerID())
	require.NoError(t, statsErr)

	statsCtx, statsCancel := context.WithCancel(ctx)
	stats.start(statsCtx, env.exporterContainer.GetContainerID())
	statsStopped := false
	defer func() {
		if !statsStopped {
			statsCancel()
			stats.stop()
		}
	}()

	protocolsBefore := readProtocolCounters(t, env.endpoint)
	packetsBefore := protocolsBefore.SIPPackets
	expectedTotal := float64(callCount) * fullCallPacketsPerCall

	start := time.Now()

	uasPath := absScenarioPath(t, uasScenario)
	sippVol := filepath.Dir(uasPath)
	uasFile := filepath.Base(uasScenario)

	uasContainer := startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/" + uasFile, "-i", "127.0.0.1", "-p", env.sippPort,
			"-m", strconv.Itoa(callCount), "-nr", "-nostdin"},
		sippVol, false,
	)

	waitForSIPpUDPReady(ctx, t, uasContainer, env.sippPort)

	uacPath := absScenarioPath(t, uacScenario)
	sippVol = filepath.Dir(uacPath)
	uacFile := filepath.Base(uacScenario)

	uacContainer := startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/" + uacFile, "-i", "127.0.0.1", "-p", env.sippClientPort,
			"-m", strconv.Itoa(callCount), "-r", strconv.Itoa(rate),
			"-nr",
			"-l", strconv.Itoa(limit),
			"127.0.0.1:" + env.sippPort},
		sippVol, false,
	)

	var peakSessions float64
	require.Eventually(t, func() bool {
		if metricExists(t, env.endpoint, "sip_exporter_sessions") {
			sessions := getMetric(t, env.endpoint, "sip_exporter_sessions")
			if sessions > peakSessions {
				peakSessions = sessions
			}
		}
		state, stateErr := uacContainer.State(ctx)
		if stateErr != nil {
			return false
		}
		return !state.Running
	}, contextTimeout(t, ctx), 500*time.Millisecond, "UAC container did not exit in time")

	waitForContainerExit(ctx, t, uasContainer)

	sippEnd := time.Now()
	sippDuration := sippEnd.Sub(start)

	waitForExactSIPCapture(ctx, t, env.endpoint, packetsBefore, expectedTotal)

	stableTime := time.Now()
	drainTime := stableTime.Sub(sippEnd)

	statsCancel()
	cpuAvg, cpuPeak, memMaxMB := stats.stop()
	statsStopped = true

	protocolsAfter := readProtocolCounters(t, env.endpoint)
	protocols := protocolsAfter.delta(protocolsBefore)
	packetsAfter := protocolsAfter.SIPPackets
	capture := newCaptureResult(expectedTotal, protocols.SIPPackets)
	require.NoError(t, capture.ValidateExact())

	actualPPS := 0.0
	if sippDuration.Seconds() > 0 {
		actualPPS = (packetsAfter - packetsBefore) / sippDuration.Seconds()
	}

	result := loadResult{
		Duration:      sippDuration,
		Capture:       capture,
		Protocols:     protocols,
		PacketsBefore: packetsBefore,
		PacketsAfter:  packetsAfter,
		ActualPPS:     actualPPS,
		DrainTime:     drainTime,
		CPUAvg:        cpuAvg,
		CPUPeak:       cpuPeak,
		MemMaxMB:      memMaxMB,
		PeakSessions:  peakSessions,
	}

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")

	t.Logf("Concurrent result: actual=%.0f PPS, peak_sessions=%.0f, invites=%.0f, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB, duration=%v",
		result.ActualPPS, peakSessions, inviteTotal, result.DrainTime,
		result.CPUAvg, result.CPUPeak, result.MemMaxMB, result.Duration)

	return result
}
