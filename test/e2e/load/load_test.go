//go:build e2e

package load

import (
	"context"
	"encoding/json"
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
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	sippImage     = "pbertera/sipp:latest"
	testInterface = "lo"
)

type (
	testEnv struct {
		endpoint          string
		sippPort          string
		sippClientPort    string
		exporterContainer testcontainers.Container
	}

	loadResult struct {
		Duration      time.Duration
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
)

var (
	portMu       sync.Mutex
	nextBasePort = 30000

	projectRoot   string
	exporterImage string
)

func init() {
	_, file, _, _ := runtime.Caller(0)
	projectRoot = filepath.Join(filepath.Dir(file), "..", "..", "..")

	exporterImage = os.Getenv("SIP_EXPORTER_E2E_IMAGE")
	if exporterImage == "" {
		exporterImage = "sip-exporter:latest"
	}
}

func allocatePorts() (exporter, sipp, sippClient string) {
	portMu.Lock()
	defer portMu.Unlock()
	base := nextBasePort
	nextBasePort += 3
	return strconv.Itoa(base), strconv.Itoa(base + 1), strconv.Itoa(base + 2)
}

func newTestEnv(ctx context.Context, t *testing.T) *testEnv {
	t.Helper()
	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	exporterLogLevel := "error"
	if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
		exporterLogLevel = "debug"
	}

	envVars := map[string]string{
		"SIP_EXPORTER_INTERFACE":    testInterface,
		"SIP_EXPORTER_HTTP_PORT":    exporterHTTPPort,
		"SIP_EXPORTER_SIP_PORT":     sippPort,
		"SIP_EXPORTER_SIPS_PORT":    sippClientPort,
		"SIP_EXPORTER_LOGGER_LEVEL": exporterLogLevel,
	}

	if maxProcs := os.Getenv("SIP_EXPORTER_E2E_GOMAXPROCS"); maxProcs != "" {
		envVars["GOMAXPROCS"] = maxProcs
	}

	if goDebug := os.Getenv("SIP_EXPORTER_E2E_GODEBUG"); goDebug != "" {
		envVars["GODEBUG"] = goDebug
	}

	req := testcontainers.ContainerRequest{
		Image:       exporterImage,
		Privileged:  true,
		NetworkMode: "host",
		Env:         envVars,
		WaitingFor: wait.ForHTTP("/metrics").
			WithPort(nat.Port(exporterHTTPPort)).
			WithStartupTimeout(120 * time.Second),
	}

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
		if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
			logs, logErr := c.Logs(context.Background())
			if logErr == nil {
				defer logs.Close()
				logBytes, _ := io.ReadAll(logs)
				t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
			}
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = c.Stop(stopCtx, nil)
		_ = c.Terminate(ctx)
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

func getMetric(t *testing.T, endpoint, metricName string) float64 {
	t.Helper()

	resp, err := http.Get(endpoint + "/metrics") //nolint:noctx // test helper
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	re := regexp.MustCompile(`^` + metricName + `(?:\{[^}]*\})?\s+([0-9.]+)`)
	for _, line := range strings.Split(string(body), "\n") {
		matches := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) == 2 {
			val, parseErr := strconv.ParseFloat(matches[1], 64)
			require.NoError(t, parseErr)
			return val
		}
	}

	return 0
}

func waitForMetricStable(t *testing.T, endpoint string) {
	t.Helper()
	prev := -1.0
	stableCount := 0
	require.Eventually(t, func() bool {
		cur := getMetric(t, endpoint, "sip_exporter_packets_total")
		if cur == prev {
			stableCount++
			return stableCount >= 2
		}
		prev = cur
		stableCount = 0
		return false
	}, 60*time.Second, 300*time.Millisecond, "metrics did not stabilize after SIPp scenario")
}

func absScenarioPath(t *testing.T, filename string) string {
	t.Helper()
	localPath := filepath.Join(projectRoot, "test", "e2e", "load", "sipp", filename)
	if _, err := os.Stat(localPath); err == nil {
		return localPath
	}
	return filepath.Join(projectRoot, "test", "e2e", "sipp", filename)
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

	req := testcontainers.ContainerRequest{
		Image:       sippImage,
		NetworkMode: "host",
		Cmd:         args,
		Mounts: testcontainers.Mounts(
			testcontainers.BindMount(sippVol, "/scenarios"),
		),
	}

	if waitForExit {
		req.WaitingFor = wait.ForExit().WithExitTimeout(300 * time.Second)
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           log.New(io.Discard, "", 0),
	})

	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
		logs, logErr := c.Logs(ctx)
		if logErr == nil {
			defer logs.Close()
			logBytes, _ := io.ReadAll(logs)
			t.Logf("SIPp logs:\n%s", strings.TrimSpace(string(logBytes)))
		}
	}

	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Terminate(ctx) })

	return c
}

func waitForContainerExit(ctx context.Context, t *testing.T, c testcontainers.Container) {
	t.Helper()
	require.Eventually(t, func() bool {
		state, err := c.State(ctx)
		if err != nil {
			return false
		}
		return !state.Running
	}, 300*time.Second, 500*time.Millisecond, "SIPp container did not exit in time")
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
		resp, err := s.dockerCli.ContainerStats(ctx, containerID, true)
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

	ctx, cancel := context.WithTimeout(ctx, 600*time.Second)
	defer cancel()

	stats, statsErr := newStatsCollector(env.exporterContainer.GetContainerID())
	require.NoError(t, statsErr)

	statsCtx, statsCancel := context.WithCancel(ctx)
	stats.start(statsCtx, env.exporterContainer.GetContainerID())

	packetsBefore := getMetric(t, env.endpoint, "sip_exporter_packets_total")
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	start := time.Now()

	if uasScenario != "" {
		uasPath := absScenarioPath(t, uasScenario)
		sippVol := filepath.Dir(uasPath)
		uasFile := filepath.Base(uasScenario)

		uasContainer := startSippContainer(ctx, t,
			[]string{"-sf", "/scenarios/" + uasFile, "-i", "127.0.0.1", "-p", env.sippPort,
				"-m", strconv.Itoa(callCount), "-nostdin"},
			sippVol, false,
		)

		time.Sleep(500 * time.Millisecond)

		uacPath := absScenarioPath(t, uacScenario)
		sippVol = filepath.Dir(uacPath)
		uacFile := filepath.Base(uacScenario)

		startSippContainer(ctx, t,
			[]string{"-sf", "/scenarios/" + uacFile, "-i", "127.0.0.1", "-p", env.sippClientPort,
				"-m", strconv.Itoa(callCount), "-r", strconv.Itoa(rate),
				"127.0.0.1:" + env.sippPort},
			sippVol, true,
		)

		waitForContainerExit(ctx, t, uasContainer)
	} else {
		uacPath := absScenarioPath(t, uacScenario)
		sippVol := filepath.Dir(uacPath)
		uacFile := filepath.Base(uacScenario)

		startSippContainer(ctx, t,
			[]string{"-sf", "/scenarios/" + uacFile, "-i", "127.0.0.1", "-p", env.sippClientPort,
				"-m", strconv.Itoa(callCount), "-r", strconv.Itoa(rate),
				"127.0.0.1:" + env.sippPort},
			sippVol, true,
		)
	}

	sippEnd := time.Now()
	sippDuration := sippEnd.Sub(start)

	waitForMetricStable(t, env.endpoint)

	stableTime := time.Now()
	drainTime := stableTime.Sub(sippEnd)

	statsCancel()
	cpuAvg, cpuPeak, memMaxMB := stats.stop()

	packetsAfter := getMetric(t, env.endpoint, "sip_exporter_packets_total")
	errorsAfter := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	totalCaptured := packetsAfter - packetsBefore
	actualPPS := 0.0
	if sippDuration.Seconds() > 0 {
		actualPPS = totalCaptured / sippDuration.Seconds()
	}
	expectedTotal := float64(callCount) * packetsPerCall
	expectedPPS := float64(rate) * packetsPerCall
	lossRate := 0.0
	if expectedTotal > 0 {
		lossRate = 1 - totalCaptured/expectedTotal
		if lossRate < 0 {
			lossRate = 0
		}
	}

	result := loadResult{
		Duration:      sippDuration,
		PacketsBefore: packetsBefore,
		PacketsAfter:  packetsAfter,
		ActualPPS:     actualPPS,
		ExpectedPPS:   expectedPPS,
		LossRate:      lossRate,
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

	ctx, cancel := context.WithTimeout(ctx, 600*time.Second)
	defer cancel()

	stats, statsErr := newStatsCollector(env.exporterContainer.GetContainerID())
	require.NoError(t, statsErr)

	statsCtx, statsCancel := context.WithCancel(ctx)
	stats.start(statsCtx, env.exporterContainer.GetContainerID())

	packetsBefore := getMetric(t, env.endpoint, "sip_exporter_packets_total")

	start := time.Now()

	uasPath := absScenarioPath(t, uasScenario)
	sippVol := filepath.Dir(uasPath)
	uasFile := filepath.Base(uasScenario)

	uasContainer := startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/" + uasFile, "-i", "127.0.0.1", "-p", env.sippPort,
			"-m", strconv.Itoa(callCount), "-nostdin"},
		sippVol, false,
	)

	time.Sleep(1 * time.Second)

	uacPath := absScenarioPath(t, uacScenario)
	sippVol = filepath.Dir(uacPath)
	uacFile := filepath.Base(uacScenario)

	startSippContainer(ctx, t,
		[]string{"-sf", "/scenarios/" + uacFile, "-i", "127.0.0.1", "-p", env.sippClientPort,
			"-m", strconv.Itoa(callCount), "-r", strconv.Itoa(rate),
			"-l", strconv.Itoa(limit),
			"127.0.0.1:" + env.sippPort},
		sippVol, true,
	)

	waitForContainerExit(ctx, t, uasContainer)

	sippEnd := time.Now()
	sippDuration := sippEnd.Sub(start)

	waitForMetricStable(t, env.endpoint)

	stableTime := time.Now()
	drainTime := stableTime.Sub(sippEnd)

	statsCancel()
	cpuAvg, cpuPeak, memMaxMB := stats.stop()

	packetsAfter := getMetric(t, env.endpoint, "sip_exporter_packets_total")

	actualPPS := 0.0
	if sippDuration.Seconds() > 0 {
		actualPPS = (packetsAfter - packetsBefore) / sippDuration.Seconds()
	}

	result := loadResult{
		Duration:      sippDuration,
		PacketsBefore: packetsBefore,
		PacketsAfter:  packetsAfter,
		ActualPPS:     actualPPS,
		DrainTime:     drainTime,
		CPUAvg:        cpuAvg,
		CPUPeak:       cpuPeak,
		MemMaxMB:      memMaxMB,
	}

	sessions := getMetric(t, env.endpoint, "sip_exporter_sessions")
	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")

	t.Logf("Concurrent result: actual=%.0f PPS, sessions=%.0f, invites=%.0f, drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB, duration=%v",
		result.ActualPPS, sessions, inviteTotal, result.DrainTime,
		result.CPUAvg, result.CPUPeak, result.MemMaxMB, result.Duration)

	return result
}
