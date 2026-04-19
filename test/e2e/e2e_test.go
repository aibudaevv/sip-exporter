//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	sippImage     = "pbertera/sipp:latest"
	testInterface = "lo"
)

// testEnv holds per-test network configuration.
// Each parallel test gets its own set of free ports to avoid conflicts.
type testEnv struct {
	endpoint       string // http://localhost:PORT — exporter /metrics endpoint
	sippPort       string // UAS listens here; UAC connects here; exporter SIP_PORT
	sippClientPort string // UAC listens here; exporter SIPS_PORT
}

var (
	portMu       sync.Mutex
	nextBasePort = 20000
)

// allocatePorts returns 3 guaranteed-unique port numbers within this process.
// Uses a monotonic counter under a mutex so parallel tests never collide.
// Ports start at 20000 to stay clear of the OS ephemeral range (32768–60999).
func allocatePorts() (exporter, sipp, sippClient string) {
	portMu.Lock()
	defer portMu.Unlock()
	base := nextBasePort
	nextBasePort += 3
	return strconv.Itoa(base), strconv.Itoa(base + 1), strconv.Itoa(base + 2)
}

// newTestEnv allocates free ports and starts an exporter container for the test.
func newTestEnv(ctx context.Context, t *testing.T) *testEnv {
	t.Helper()
	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	env := &testEnv{
		sippPort:       sippPort,
		sippClientPort: sippClientPort,
	}
	env.endpoint = startExporter(ctx, t, exporterHTTPPort, sippPort, sippClientPort)
	return env
}

var projectRoot string
var interfaceIP string
var exporterImage string

func init() {
	_, file, _, _ := runtime.Caller(0)
	projectRoot = filepath.Join(filepath.Dir(file), "..", "..")
	interfaceIP = getInterfaceIP(testInterface)

	exporterImage = os.Getenv("SIP_EXPORTER_E2E_IMAGE")
	if exporterImage == "" {
		exporterImage = "sip-exporter:latest"
	}
}

// getInterfaceIP returns IPv4 address of network interface.
func getInterfaceIP(name string) string {
	if name == "lo" {
		return "127.0.0.1"
	}
	cmd := exec.Command("ip", "-4", "addr", "show", name)
	out, err := cmd.Output()
	if err != nil {
		return "127.0.0.1"
	}
	re := regexp.MustCompile(`inet\s+(\d+\.\d+\.\d+\.\d+)`)
	matches := re.FindSubmatch(out)
	if len(matches) < 2 {
		return "127.0.0.1"
	}
	return string(matches[1])
}

// sippResult stores SIPp call statistics.
type sippResult struct {
	totalCalls   int
	successCalls int
	failedCalls  int
}

// startExporter starts exporter container and returns HTTP endpoint.
// Image is pre-built via Makefile (docker_build target) and passed through
// SIP_EXPORTER_E2E_IMAGE environment variable.
// Called only from newTestEnv; ports are allocated by freePort.
func startExporter(ctx context.Context, t *testing.T, exporterPort, sippPort, sippClientPort string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	exporterLogLevel := "error"
	if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
		exporterLogLevel = "debug"
	}

	req := testcontainers.ContainerRequest{
		Image:       exporterImage,
		Privileged:  true,
		NetworkMode: "host",
		Env: map[string]string{
			"SIP_EXPORTER_INTERFACE":    testInterface,
			"SIP_EXPORTER_HTTP_PORT":    exporterPort,
			"SIP_EXPORTER_SIP_PORT":     sippPort,
			"SIP_EXPORTER_SIPS_PORT":    sippClientPort,
			"SIP_EXPORTER_LOGGER_LEVEL": exporterLogLevel,
		},
		WaitingFor: wait.ForHTTP("/metrics").
			WithPort(nat.Port(exporterPort)).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           log.New(io.Discard, "", 0),
	})
	if err != nil && container != nil {
		logs, logErr := container.Logs(ctx)
		if logErr == nil {
			defer logs.Close()
			logBytes, _ := io.ReadAll(logs)
			t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
		}
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
			logs, logErr := container.Logs(context.Background())
			if logErr == nil {
				defer logs.Close()
				logBytes, _ := io.ReadAll(logs)
				t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
			}
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = container.Stop(stopCtx, nil)
		_ = container.Terminate(ctx)
		for i := 0; i < 10; i++ {
			conn, err := net.DialTimeout("tcp", "localhost:"+exporterPort, 500*time.Millisecond)
			if err != nil {
				return
			}
			conn.Close()
			time.Sleep(500 * time.Millisecond)
		}
	})

	return fmt.Sprintf("http://localhost:%s", exporterPort)
}

// waitForSessionsZero polls sip_exporter_sessions until it reaches 0.
// Needed because the sessions gauge is updated by a 1-second ticker in the exporter,
// not immediately when dialogs are deleted.
func waitForSessionsZero(t *testing.T, endpoint string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return getMetric(t, endpoint, "sip_exporter_sessions") == 0
	}, 5*time.Second, 300*time.Millisecond, "sessions should reach 0 after all calls terminated")
}

// waitForMetricStable polls sip_exporter_packets_total until the value stops
// changing for 2 consecutive intervals (2 × 300 ms), indicating all in-flight
// packets have been processed by the exporter.
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
	}, 10*time.Second, 300*time.Millisecond, "metrics did not stabilize after SIPp scenario")
}

// getMetric reads a single numeric metric value from the exporter /metrics endpoint.
func getMetric(t *testing.T, endpoint string, metricName string) float64 {
	t.Helper()

	resp, err := http.Get(endpoint + "/metrics") //nolint:noctx // test helper
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	re := regexp.MustCompile(`^` + metricName + `\s+([0-9.]+)`)
	for _, line := range strings.Split(string(body), "\n") {
		matches := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) == 2 {
			val, err := strconv.ParseFloat(matches[1], 64)
			require.NoError(t, err)
			return val
		}
	}

	return 0
}

// runSippScenario starts SIPp server and client via docker CLI (host network mode),
// waits for calls to complete and returns statistics.
func runSippScenario(ctx context.Context, t *testing.T, uasScenario, uacScenario string, callCount int, env *testEnv) sippResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var stdout, stderr io.Writer = &testWriter{t}, &testWriter{t}
	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") != "true" {
		stdout, stderr = io.Discard, io.Discard
	}

	uasPath := absScenarioPath(t, uasScenario)
	sippVol := filepath.Dir(uasPath)
	uasScenarioFile := filepath.Base(uasScenario)
	uacScenarioFile := filepath.Base(uacScenario)

	uasCmd := exec.Command("docker", "run", "--rm",
		"--network", "host",
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/"+uasScenarioFile,
		"-i", "127.0.0.1",
		"-p", env.sippPort,
		"-m", strconv.Itoa(callCount),
		"-nostdin",
	)
	uasCmd.Stdout = stdout
	uasCmd.Stderr = stderr
	require.NoError(t, uasCmd.Start())

	time.Sleep(1 * time.Second)

	uacCmd := exec.Command("docker", "run", "--rm",
		"--network", "host",
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/"+uacScenarioFile,
		"-i", "127.0.0.1",
		"-p", env.sippClientPort,
		"-m", strconv.Itoa(callCount),
		"127.0.0.1:"+env.sippPort,
	)
	uacCmd.Stdout = stdout
	uacCmd.Stderr = stderr
	require.NoError(t, uacCmd.Run())

	_ = uasCmd.Wait()

	waitForMetricStable(t, env.endpoint)

	return sippResult{totalCalls: callCount}
}

// runSippUACOnly starts SIPp client only (no server) for timeout tests.
func runSippUACOnly(ctx context.Context, t *testing.T, uacScenario string, callCount int, env *testEnv) {
	t.Helper()

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var stdout, stderr io.Writer = &testWriter{t}, &testWriter{t}
	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") != "true" {
		stdout, stderr = io.Discard, io.Discard
	}

	uacPath := absScenarioPath(t, uacScenario)
	sippVol := filepath.Dir(uacPath)
	uacScenarioFile := filepath.Base(uacScenario)

	uacCmd := exec.Command("docker", "run", "--rm",
		"--network", "host",
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/"+uacScenarioFile,
		"-i", "127.0.0.1",
		"-p", env.sippClientPort,
		"-m", strconv.Itoa(callCount),
		"-timeout", "5s",
		"127.0.0.1:"+env.sippPort,
	)
	uacCmd.Stdout = stdout
	uacCmd.Stderr = stderr
	_ = uacCmd.Run()

	waitForMetricStable(t, env.endpoint)
}

// getSER reads sip_exporter_ser metric from exporter endpoint.
func getSER(t *testing.T, endpoint string) float64 {
	t.Helper()

	resp, err := http.Get(endpoint + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	re := regexp.MustCompile(`^sip_exporter_ser\s+([0-9.]+)`)
	for _, line := range strings.Split(string(body), "\n") {
		matches := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) == 2 {
			val, err := strconv.ParseFloat(matches[1], 64)
			require.NoError(t, err)
			return val
		}
	}

	return 0
}

// ADDED: getSEER reads sip_exporter_seer metric from exporter endpoint.
func getSEER(t *testing.T, endpoint string) float64 {
	t.Helper()

	resp, err := http.Get(endpoint + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	re := regexp.MustCompile(`^sip_exporter_seer\s+([0-9.]+)`)
	for _, line := range strings.Split(string(body), "\n") {
		matches := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) == 2 {
			val, err := strconv.ParseFloat(matches[1], 64)
			require.NoError(t, err)
			return val
		}
	}

	return 0
}

// ADDED: getISA reads sip_exporter_isa metric from exporter endpoint.
func getISA(t *testing.T, endpoint string) float64 {
	t.Helper()

	resp, err := http.Get(endpoint + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	re := regexp.MustCompile(`^sip_exporter_isa\s+([0-9.]+)`)
	for _, line := range strings.Split(string(body), "\n") {
		matches := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) == 2 {
			val, err := strconv.ParseFloat(matches[1], 64)
			require.NoError(t, err)
			return val
		}
	}

	return 0
}

func getSessions(t *testing.T, endpoint string) float64 {
	t.Helper()

	resp, err := http.Get(endpoint + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	re := regexp.MustCompile(`^sip_exporter_sessions\s+([0-9.]+)`)
	for _, line := range strings.Split(string(body), "\n") {
		matches := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) == 2 {
			val, err := strconv.ParseFloat(matches[1], 64)
			require.NoError(t, err)
			return val
		}
	}

	return 0
}

func getSCR(t *testing.T, endpoint string) float64 {
	t.Helper()

	resp, err := http.Get(endpoint + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	re := regexp.MustCompile(`^sip_exporter_scr\s+([0-9.]+)`)
	for _, line := range strings.Split(string(body), "\n") {
		matches := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) == 2 {
			val, err := strconv.ParseFloat(matches[1], 64)
			require.NoError(t, err)
			return val
		}
	}

	return 0
}

func getASR(t *testing.T, endpoint string) float64 {
	t.Helper()

	resp, err := http.Get(endpoint + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	re := regexp.MustCompile(`^sip_exporter_asr\s+([0-9.]+)`)
	for _, line := range strings.Split(string(body), "\n") {
		matches := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) == 2 {
			val, err := strconv.ParseFloat(matches[1], 64)
			require.NoError(t, err)
			return val
		}
	}

	return 0
}

func getSDC(t *testing.T, endpoint string) float64 {
	t.Helper()
	return getMetric(t, endpoint, "sip_exporter_sdc_total")
}

func getSPD(t *testing.T, endpoint string) float64 {
	t.Helper()

	sum := getMetric(t, endpoint, "sip_exporter_spd_sum")
	count := getMetric(t, endpoint, "sip_exporter_spd_count")
	if count == 0 {
		return 0
	}

	return sum / count
}

func getNER(t *testing.T, endpoint string) float64 {
	t.Helper()
	return getMetric(t, endpoint, "sip_exporter_ner")
}

func getISS(t *testing.T, endpoint string) float64 {
	t.Helper()
	return getMetric(t, endpoint, "sip_exporter_iss_total")
}

func getORD(t *testing.T, endpoint string) float64 {
	t.Helper()
	return getMetric(t, endpoint, "sip_exporter_ord_count")
}

func getLRD(t *testing.T, endpoint string) float64 {
	t.Helper()
	return getMetric(t, endpoint, "sip_exporter_lrd_count")
}

// absScenarioPath returns absolute path to SIPp scenario.
func absScenarioPath(t *testing.T, filename string) string {
	t.Helper()
	return filepath.Join(projectRoot, "test", "e2e", "sipp", filename)
}

// testWriter writes test logs via t.Log.
type testWriter struct {
	t *testing.T
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}
