//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	exporterImage  = "sip-exporter:0.4.0-dev2"
	sippImage      = "pbertera/sipp:latest"
	exporterPort   = "2113"
	sippPort       = "5060"
	sipsPort       = "5061"
	sippClientPort = "5061"
	testInterface  = "lo"
)

var projectRoot string
var interfaceIP string

func init() {
	_, file, _, _ := runtime.Caller(0)
	projectRoot = filepath.Join(filepath.Dir(file), "..", "..")
	interfaceIP = getInterfaceIP(testInterface)
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
// Image is built from Dockerfile in project root.
func startExporter(ctx context.Context, t *testing.T) string {
	t.Helper()

	// ADDED: configurable exporter log level
	exporterLogLevel := "error"
	if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
		exporterLogLevel = "debug"
	}

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    projectRoot,
			Dockerfile: "Dockerfile",
		},
		Privileged:  true,
		NetworkMode: "host",
		Env: map[string]string{
			"SIP_EXPORTER_INTERFACE":    testInterface,
			"SIP_EXPORTER_HTTP_PORT":    exporterPort,
			"SIP_EXPORTER_SIP_PORT":     sippPort,
			"SIP_EXPORTER_SIPS_PORT":    sipsPort,
			"SIP_EXPORTER_LOGGER_LEVEL": exporterLogLevel,
		},
		WaitingFor: wait.ForHTTP("/metrics").
			WithPort(exporterPort).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		logs, err := container.Logs(ctx)
		if err == nil {
			defer logs.Close()
			logBytes, _ := io.ReadAll(logs)
			t.Logf("Exporter logs:\n%s", string(logBytes))
		}
		_ = container.Terminate(ctx)
	})

	return fmt.Sprintf("http://localhost:%s", exporterPort)
}

// runSippScenario starts SIPp server and client via docker CLI (host network mode),
// waits for calls to complete and returns statistics.
func runSippScenario(ctx context.Context, t *testing.T, uasScenario, uacScenario string, callCount int) sippResult {
	t.Helper()

	// ADDED: configurable SIPp verbosity
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
		"-p", sippPort,
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
		"-p", sippClientPort,
		"-m", strconv.Itoa(callCount),
		"127.0.0.1:"+sippPort,
	)
	uacCmd.Stdout = stdout
	uacCmd.Stderr = stderr
	require.NoError(t, uacCmd.Run())

	_ = uasCmd.Wait()

	time.Sleep(3 * time.Second)

	return sippResult{totalCalls: callCount}
}

// getSER reads sip_exporter_ser metric from exporter endpoint.
func getSER(t *testing.T, endpoint string) float64 {
	t.Helper()

	resp, err := http.Get(endpoint + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	t.Logf("Metrics output:\n%s", string(body))

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
