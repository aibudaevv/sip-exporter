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

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"gopkg.in/yaml.v3"
)

const (
	sippImage     = "pbertera/sipp:latest"
	testInterface = "lo"
	ratioDelta    = 2.0
)

// testEnv holds per-test network configuration.
// Each parallel test gets its own set of free ports to avoid conflicts.
type testEnv struct {
	endpoint       string
	sippPort       string
	sippClientPort string
	carrier        string
}

var (
	portMu       sync.Mutex
	nextBasePort = 20000
)

// allocatePorts returns 3 guaranteed-unique port numbers within this process.
// Uses a monotonic counter under a mutex so parallel tests never collide.
// Ports start at 20000 to stay clear of the OS ephemeral range (32768–60999).
// Gap layout: exporter=base, sippPort=base+1, [UAS media=base+3],
// sippClientPort=base+5, [UAC media=base+7], next=base+10.
// The wide gaps prevent SIPp's default media port (local_port+2) from
// colliding with the other SIPp instance's signalling port.
func allocatePorts() (exporter, sipp, sippClient string) {
	portMu.Lock()
	defer portMu.Unlock()
	base := nextBasePort
	nextBasePort += 10
	return strconv.Itoa(base), strconv.Itoa(base + 1), strconv.Itoa(base + 5)
}

// newTestEnv allocates free ports and starts an exporter container for the test.
func newTestEnv(ctx context.Context, t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithConfig(ctx, t, "")
}

// newTestEnvWithCarriers starts an exporter with carrier config.
// Uses test/e2e/carriers.yaml which maps 127.0.0.0/8 to a carrier name.
// The carrier name is stored in env.carrier for use by env.*ByCarrier() methods.
func newTestEnvWithCarriers(ctx context.Context, t *testing.T) *testEnv {
	t.Helper()
	carriersYAML := loadCarriersYAML(t, "carriers.yaml")

	var cfg struct {
		Carriers []struct {
			Name string `yaml:"name"`
		} `yaml:"carriers"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(carriersYAML), &cfg))
	require.NotEmpty(t, cfg.Carriers, "carriers.yaml must define at least one carrier")

	env := newTestEnvWithConfig(ctx, t, carriersYAML)
	env.carrier = cfg.Carriers[0].Name
	return env
}

// newTestEnvWithCarriersYAML starts an exporter with arbitrary carriers YAML content
// and sets env.carrier to the given carrierName for use by env.*ByCarrier() methods.
func newTestEnvWithCarriersYAML(ctx context.Context, t *testing.T, carriersYAML string, carrierName string) *testEnv {
	t.Helper()
	env := newTestEnvWithConfig(ctx, t, carriersYAML)
	env.carrier = carrierName
	return env
}

func newSharedTestEnvWithCarriersYAML(ctx context.Context, t *testing.T, carriersYAML string, carrierName string) *sharedTestEnv {
	t.Helper()
	env := newSharedTestEnvWithConfig(ctx, t, carriersYAML)
	env.carrier = carrierName
	return env
}

type sharedTestEnv struct {
	testEnv
	container    testcontainers.Container
	exporterPort string
}

func newSharedTestEnv(ctx context.Context, t *testing.T) *sharedTestEnv {
	t.Helper()
	return newSharedTestEnvWithConfig(ctx, t, "")
}

func newSharedTestEnvWithCarriers(ctx context.Context, t *testing.T) *sharedTestEnv {
	t.Helper()
	carriersYAML := loadCarriersYAML(t, "carriers.yaml")

	var cfg struct {
		Carriers []struct {
			Name string `yaml:"name"`
		} `yaml:"carriers"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(carriersYAML), &cfg))
	require.NotEmpty(t, cfg.Carriers, "carriers.yaml must define at least one carrier")

	env := newSharedTestEnvWithConfig(ctx, t, carriersYAML)
	env.carrier = cfg.Carriers[0].Name
	return env
}

func newSharedTestEnvWithConfig(ctx context.Context, t *testing.T, carriersYAML string) *sharedTestEnv {
	t.Helper()
	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	env := &sharedTestEnv{
		testEnv: testEnv{
			sippPort:       sippPort,
			sippClientPort: sippClientPort,
		},
		exporterPort: exporterHTTPPort,
	}
	endpoint, container := startExporterWithConfig(ctx, t, exporterHTTPPort, sippPort, sippClientPort, carriersYAML)
	env.endpoint = endpoint
	env.container = container
	registerExporterCleanup(t, container, exporterHTTPPort)
	return env
}

func loadUserAgentsYAML(t *testing.T, filename string) string {
	t.Helper()
	path := filepath.Join(projectRoot, "test", "e2e", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func newSharedTestEnvWithUAConfig(ctx context.Context, t *testing.T, uaYAMLFile string) *sharedTestEnv {
	t.Helper()
	uaYAML := loadUserAgentsYAML(t, uaYAMLFile)
	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	env := &sharedTestEnv{
		testEnv: testEnv{
			sippPort:       sippPort,
			sippClientPort: sippClientPort,
		},
		exporterPort: exporterHTTPPort,
	}
	endpoint, container := startExporterWithConfigAndUA(ctx, t, exporterHTTPPort, sippPort, sippClientPort, "", uaYAML, nil, "")
	env.endpoint = endpoint
	env.container = container
	registerExporterCleanup(t, container, exporterHTTPPort)
	return env
}

func newSharedTestEnvWithCarrierAndUA(ctx context.Context, t *testing.T, carriersYAML string, carrierName string, uaYAMLFile string) *sharedTestEnv {
	t.Helper()
	uaYAML := loadUserAgentsYAML(t, uaYAMLFile)
	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	env := &sharedTestEnv{
		testEnv: testEnv{
			sippPort:       sippPort,
			sippClientPort: sippClientPort,
			carrier:        carrierName,
		},
		exporterPort: exporterHTTPPort,
	}
	endpoint, container := startExporterWithConfigAndUA(ctx, t, exporterHTTPPort, sippPort, sippClientPort, carriersYAML, uaYAML, nil, "")
	env.endpoint = endpoint
	env.container = container
	registerExporterCleanup(t, container, exporterHTTPPort)
	return env
}

func (s *sharedTestEnv) restart(t *testing.T) {
	t.Helper()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	require.NoError(t, s.container.Stop(stopCtx, nil))

	require.NoError(t, s.container.Start(context.Background()))

	require.Eventually(t, func() bool {
		resp, err := http.Get(s.endpoint + "/metrics") //nolint:noctx // test helper
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == 200
	}, 30*time.Second, 500*time.Millisecond, "exporter should be ready after restart")
}

// loadCarriersYAML reads a carriers YAML file from test/e2e/ directory.
func loadCarriersYAML(t *testing.T, filename string) string {
	t.Helper()
	path := filepath.Join(projectRoot, "test", "e2e", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// setupSecondaryIPs adds test IP addresses to loopback interface
// using a privileged Docker container (avoids requiring root on the host).
// Addresses are removed via t.Cleanup when the test finishes.
func setupSecondaryIPs(t *testing.T) {
	t.Helper()
	addrs := []string{
		"10.1.0.1/32",
		"10.2.0.1/32",
		"172.16.0.1/32",
		"172.16.0.2/32",
		"10.1.1.5/32",
	}

	script := "set -e"
	for _, addr := range addrs {
		script += " && ip addr add " + addr + " dev lo || true"
	}

	out, err := exec.Command("docker", "run", "--rm", "--privileged", "--network", "host",
		"--entrypoint", "", "alpine",
		"sh", "-c", script,
	).CombinedOutput()
	require.NoError(t, err, "failed to add secondary IPs: %s", string(out))

	t.Cleanup(func() {
		cleanScript := "set -e"
		for _, addr := range addrs {
			addrNoMask := strings.SplitN(addr, "/", 2)[0]
			cleanScript += " && ip addr del " + addrNoMask + " dev lo 2>/dev/null || true"
		}
		_ = exec.Command("docker", "run", "--rm", "--privileged", "--network", "host",
			"--entrypoint", "", "alpine",
			"sh", "-c", cleanScript,
		).Run()
	})
}

// addLoopbackIP adds a single IP address to the loopback interface.
func addLoopbackIP(t *testing.T, addr string) {
	t.Helper()
	out, err := exec.Command("docker", "run", "--rm", "--privileged", "--network", "host",
		"--entrypoint", "", "alpine",
		"sh", "-c", "ip addr add "+addr+" dev lo || true",
	).CombinedOutput()
	require.NoError(t, err, "failed to add %s to lo: %s", addr, string(out))

	t.Cleanup(func() {
		ip := strings.SplitN(addr, "/", 2)[0]
		_ = exec.Command("docker", "run", "--rm", "--privileged", "--network", "host",
			"--entrypoint", "", "alpine",
			"sh", "-c", "ip addr del "+ip+" dev lo 2>/dev/null || true",
		).Run()
	})
}

// newTestEnvWithConfig starts exporter, optionally with carriers config.
func newTestEnvWithConfig(ctx context.Context, t *testing.T, carriersYAML string) *testEnv {
	t.Helper()
	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	env := &testEnv{
		sippPort:       sippPort,
		sippClientPort: sippClientPort,
	}
	endpoint, container := startExporterWithConfig(ctx, t, exporterHTTPPort, sippPort, sippClientPort, carriersYAML)
	env.endpoint = endpoint
	registerExporterCleanup(t, container, exporterHTTPPort)
	return env
}

// newTestEnvWithExtraEnv starts an exporter with extra environment variables
// (e.g. SIP_EXPORTER_LOCAL_COUNTRY_CODE for destination_country fallback tests).
func newTestEnvWithExtraEnv(ctx context.Context, t *testing.T, carriersYAML string, extraEnv map[string]string) *testEnv {
	t.Helper()
	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	env := &testEnv{
		sippPort:       sippPort,
		sippClientPort: sippClientPort,
	}
	endpoint, container := startExporterWithConfigAndUA(ctx, t, exporterHTTPPort, sippPort, sippClientPort, carriersYAML, "", extraEnv, "")
	env.endpoint = endpoint
	registerExporterCleanup(t, container, exporterHTTPPort)
	return env
}

// newTestEnvWithGeoIP starts an exporter with a GeoIP country DB mounted.
// geoipDBPath is the host path to the .mmdb file.
func newTestEnvWithGeoIP(ctx context.Context, t *testing.T, geoipDBPath string) *testEnv {
	t.Helper()
	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	env := &testEnv{
		sippPort:       sippPort,
		sippClientPort: sippClientPort,
	}
	endpoint, container := startExporterWithConfigAndUA(ctx, t, exporterHTTPPort, sippPort, sippClientPort, "", "", nil, geoipDBPath)
	env.endpoint = endpoint
	registerExporterCleanup(t, container, exporterHTTPPort)
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

func startExporterWithConfig(ctx context.Context, t *testing.T, exporterPort, sippPort, sippClientPort string, carriersYAML string) (string, testcontainers.Container) {
	t.Helper()
	return startExporterWithConfigAndUA(ctx, t, exporterPort, sippPort, sippClientPort, carriersYAML, "", nil, "")
}

func startExporterWithConfigAndUA(ctx context.Context, t *testing.T, exporterPort, sippPort, sippClientPort string, carriersYAML string, userAgentsYAML string, extraEnv map[string]string, geoipDBPath string) (string, testcontainers.Container) {
	t.Helper()

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	exporterLogLevel := "error"
	if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
		exporterLogLevel = "debug"
	}

	envVars := map[string]string{
		"SIP_EXPORTER_INTERFACE":       testInterface,
		"SIP_EXPORTER_HTTP_PORT":       exporterPort,
		"SIP_EXPORTER_SIP_PORT":        sippPort,
		"SIP_EXPORTER_SIPS_PORT":       sippClientPort,
		"SIP_EXPORTER_LOGGER_LEVEL":    exporterLogLevel,
		"SIP_EXPORTER_IGNORE_OUTGOING": "true",
		"SIP_EXPORTER_TELEMETRY":       "false",
	}
	for k, v := range extraEnv {
		envVars[k] = v
	}

	var mounts testcontainers.ContainerMounts
	if carriersYAML != "" {
		tmpFile, err := os.CreateTemp("", "carriers-*.yaml")
		require.NoError(t, err)
		_, err = tmpFile.WriteString(carriersYAML)
		require.NoError(t, err)
		require.NoError(t, tmpFile.Close())
		t.Cleanup(func() { os.Remove(tmpFile.Name()) })

		mounts = append(mounts, testcontainers.BindMount(tmpFile.Name(), "/etc/sip-exporter/carriers.yaml"))
		envVars["SIP_EXPORTER_CARRIERS_CONFIG"] = "/etc/sip-exporter/carriers.yaml"
	}

	if userAgentsYAML != "" {
		tmpFile, err := os.CreateTemp("", "user-agents-*.yaml")
		require.NoError(t, err)
		_, err = tmpFile.WriteString(userAgentsYAML)
		require.NoError(t, err)
		require.NoError(t, tmpFile.Close())
		t.Cleanup(func() { os.Remove(tmpFile.Name()) })

		mounts = append(mounts, testcontainers.BindMount(tmpFile.Name(), "/etc/sip-exporter/user_agents.yaml"))
		envVars["SIP_EXPORTER_USER_AGENTS_CONFIG"] = "/etc/sip-exporter/user_agents.yaml"
	}

	if geoipDBPath != "" {
		mounts = append(mounts, testcontainers.BindMount(geoipDBPath, "/data/geoip.mmdb"))
		envVars["SIP_EXPORTER_GEOIP_COUNTRY_DB"] = "/data/geoip.mmdb"
	}

	req := testcontainers.ContainerRequest{
		Image:       exporterImage,
		Privileged:  true,
		NetworkMode: "host",
		Env:         envVars,
		Mounts:      mounts,
		WaitingFor: wait.ForHTTP("/metrics").
			WithPort(exporterPort + "/tcp").
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

	return fmt.Sprintf("http://localhost:%s", exporterPort), container
}

func registerExporterCleanup(t *testing.T, container testcontainers.Container, exporterPort string) {
	t.Helper()
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
		_ = container.Terminate(context.Background())
		for i := 0; i < 10; i++ {
			conn, err := net.DialTimeout("tcp", "localhost:"+exporterPort, 500*time.Millisecond)
			if err != nil {
				return
			}
			conn.Close()
			time.Sleep(500 * time.Millisecond)
		}
	})
}

// waitForSessionsZero polls sip_exporter_sessions until it reaches 0 (or ≤2 under
// parallel packet-capture contention on lo, where a missed BYE/200 OK can leave
// a dialog stuck for the 1800s default TTL).
func waitForSessionsZero(t *testing.T, endpoint string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return getMetric(t, endpoint, "sip_exporter_sessions") <= 2
	}, 15*time.Second, 300*time.Millisecond,
		"sessions should reach ≤2 after all calls terminated (packet-capture contention tolerance)")

	assertSelfMonitoringHealthy(t, endpoint)
}

func assertSelfMonitoringHealthy(t *testing.T, endpoint string) {
	t.Helper()

	received := getMetric(t, endpoint, "sip_exporter_socket_packets_received_total")
	require.Equal(t, true, received > 0, "socket_packets_received_total should be > 0 after traffic")

	dropped := getMetric(t, endpoint, "sip_exporter_socket_packets_dropped_total")
	require.Equal(t, 0.0, dropped, "socket_packets_dropped_total should be 0 (no drops)")

	require.Eventually(t, func() bool {
		return getMetric(t, endpoint, "sip_exporter_channel_length") == 0.0
	}, 3*time.Second, 100*time.Millisecond, "channel_length should be 0 after all packets processed")

	capacity := getMetric(t, endpoint, "sip_exporter_channel_capacity")
	require.Equal(t, 10000.0, capacity, "channel_capacity should be 10000")

	dialogs := getMetric(t, endpoint, "sip_exporter_active_dialogs")
	require.LessOrEqual(t, dialogs, 2.0, "active_dialogs should be ≤2 after all sessions completed (packet-capture contention tolerance)")
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
	return getMetricWithLabel(t, endpoint, metricName, "")
}

// getMetricWithLabel reads a metric with an optional label filter.
// If labelFilter is empty, matches any label set (first match).
// If labelFilter is set (e.g. `carrier="loopback-carrier"`), matches that exact label.
func getMetricWithLabel(t *testing.T, endpoint string, metricName string, labelFilter string) float64 {
	t.Helper()

	resp, err := http.Get(endpoint + "/metrics") //nolint:noctx // test helper
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var pattern string
	if labelFilter != "" {
		filters := strings.Split(labelFilter, ",")
		quotedParts := make([]string, len(filters))
		for i, f := range filters {
			quotedParts[i] = regexp.QuoteMeta(f)
		}
		pattern = `^` + metricName + `\{[^}]*` + strings.Join(quotedParts, `[^}]*`) + `[^}]*\}\s+([0-9.]+)`
	} else {
		pattern = `^` + metricName + `(?:\{[^}]*\})?\s+([0-9.]+)`
	}
	re := regexp.MustCompile(pattern)
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

// getMetricWithCarrier reads a metric filtered by carrier label.
func getMetricWithCarrier(t *testing.T, endpoint string, metricName string, carrier string) float64 {
	t.Helper()
	return getMetricWithLabel(t, endpoint, metricName, `carrier="`+carrier+`"`)
}

func getMetricWithUA(t *testing.T, endpoint string, metricName string, uaType string) float64 {
	t.Helper()
	return getMetricWithLabel(t, endpoint, metricName, `ua_type="`+uaType+`"`)
}

func getMetricWithCarrierAndUA(t *testing.T, endpoint string, metricName string, carrier string, uaType string) float64 {
	t.Helper()
	return getMetricWithLabel(t, endpoint, metricName, `carrier="`+carrier+`",ua_type="`+uaType+`"`)
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

	uasCmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--network", "host",
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/"+uasScenarioFile,
		"-i", "127.0.0.1",
		"-p", env.sippPort,
		"-m", strconv.Itoa(callCount),
		"-nr",
		"-nostdin",
	)
	uasCmd.Stdout = stdout
	uasCmd.Stderr = stderr
	require.NoError(t, uasCmd.Start())

	require.Eventually(t, func() bool {
		return isUDPPortInUse(env.sippPort)
	}, 10*time.Second, 50*time.Millisecond, "UAS should start listening on port %s", env.sippPort)

	uacMaxAttempts := 3
	for uacAttempt := 1; ; uacAttempt++ {
		var uacStderr strings.Builder
		uacCmd := exec.CommandContext(ctx, "docker", "run", "--rm",
			"--network", "host",
			"-v", sippVol+":/scenarios:ro",
			sippImage,
			"-sf", "/scenarios/"+uacScenarioFile,
			"-i", "127.0.0.1",
			"-p", env.sippClientPort,
			"-m", strconv.Itoa(callCount),
			"-nr",
			"127.0.0.1:"+env.sippPort,
		)
		uacCmd.Stdout = stdout
		if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
			uacCmd.Stderr = io.MultiWriter(stderr, &uacStderr)
		} else {
			uacCmd.Stderr = &uacStderr
		}
		if err := uacCmd.Run(); err != nil {
			if uacAttempt < uacMaxAttempts && ctx.Err() == nil && isUDPPortInUse(env.sippPort) {
				t.Logf("UAC failed (attempt %d/%d), retrying: %v", uacAttempt, uacMaxAttempts, err)
				if uacAttempt == 1 {
					dumpUDPPort(t, env.sippClientPort)
				}
				time.Sleep(2 * time.Second)
				continue
			}
			t.Logf("UAC stderr:\n%s", strings.TrimSpace(uacStderr.String()))
			require.NoError(t, err)
		}
		break
	}

	_ = uasCmd.Wait()

	waitForMetricStable(t, env.endpoint)

	require.Eventually(t, func() bool {
		return !isUDPPortInUse(env.sippPort)
	}, 5*time.Second, 100*time.Millisecond, "port %s should be free after SIPp exit", env.sippPort)

	require.Eventually(t, func() bool {
		return !isUDPPortInUse(env.sippClientPort)
	}, 5*time.Second, 100*time.Millisecond, "UAC port %s should be free after SIPp exit", env.sippClientPort)

	return sippResult{totalCalls: callCount}
}

// runSippScenarioWithIPs starts SIPp server and client with custom source IPs
// using testcontainers. uasIP and uacIP are bound via SIPp -i flag so packets
// have the specified source address. The target address for UAC is uasIP:env.sippPort.
func runSippScenarioWithIPs(ctx context.Context, t *testing.T, uasScenario, uacScenario string, callCount int, env *testEnv, uasIP, uacIP string) sippResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	uasPath := absScenarioPath(t, uasScenario)
	sippVol := filepath.Dir(uasPath)
	uasScenarioFile := filepath.Base(uasScenario)
	uacScenarioFile := filepath.Base(uacScenario)

	uasReq := testcontainers.ContainerRequest{
		Image:       sippImage,
		NetworkMode: "host",
		Cmd: []string{
			"-sf", "/scenarios/" + uasScenarioFile,
			"-i", uasIP,
			"-p", env.sippPort,
			"-m", strconv.Itoa(callCount),
			"-nr",
			"-nostdin",
		},
		Mounts: testcontainers.Mounts(
			testcontainers.BindMount(sippVol, "/scenarios"),
		),
	}

	uasC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: uasReq,
		Started:          true,
		Logger:           log.New(io.Discard, "", 0),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = uasC.Terminate(ctx) })

	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
		logs, logErr := uasC.Logs(ctx)
		if logErr == nil {
			defer logs.Close()
			logBytes, _ := io.ReadAll(logs)
			t.Logf("UAS logs:\n%s", strings.TrimSpace(string(logBytes)))
		}
	}

	require.Eventually(t, func() bool {
		return isUDPPortInUse(env.sippPort)
	}, 10*time.Second, 50*time.Millisecond, "UAS should start listening on %s:%s", uasIP, env.sippPort)

	uacReq := testcontainers.ContainerRequest{
		Image:       sippImage,
		NetworkMode: "host",
		Cmd: []string{
			"-sf", "/scenarios/" + uacScenarioFile,
			"-i", uacIP,
			"-p", env.sippClientPort,
			"-m", strconv.Itoa(callCount),
			"-nr",
			uasIP + ":" + env.sippPort,
		},
		Mounts: testcontainers.Mounts(
			testcontainers.BindMount(sippVol, "/scenarios"),
		),
		WaitingFor: wait.ForExit().WithExitTimeout(60 * time.Second),
	}

	uacC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: uacReq,
		Started:          true,
		Logger:           log.New(io.Discard, "", 0),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = uacC.Terminate(ctx) })

	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
		logs, logErr := uacC.Logs(ctx)
		if logErr == nil {
			defer logs.Close()
			logBytes, _ := io.ReadAll(logs)
			t.Logf("UAC logs:\n%s", strings.TrimSpace(string(logBytes)))
		}
	}

	waitForContainerExitLogless(t, uasC)
	waitForMetricStable(t, env.endpoint)

	return sippResult{totalCalls: callCount}
}

// waitForContainerExitLogless waits for a container to stop running.
func waitForContainerExitLogless(t *testing.T, c testcontainers.Container) {
	t.Helper()
	require.Eventually(t, func() bool {
		state, err := c.State(context.Background())
		if err != nil {
			return false
		}
		return !state.Running
	}, 60*time.Second, 500*time.Millisecond, "container did not exit in time")
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

	uacMaxAttempts := 3
	for uacAttempt := 1; ; uacAttempt++ {
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
		if err := uacCmd.Run(); err != nil {
			if uacAttempt < uacMaxAttempts {
				t.Logf("UAC-only failed (attempt %d/%d), retrying: %v", uacAttempt, uacMaxAttempts, err)
				time.Sleep(time.Second)
				continue
			}
			t.Logf("UAC-only failed after %d attempts: %v", uacMaxAttempts, err)
		}
		break
	}

	waitForMetricStable(t, env.endpoint)
}

// getSER reads sip_exporter_ser metric from exporter endpoint.
func getSER(t *testing.T, endpoint string) float64 {
	t.Helper()
	return getMetric(t, endpoint, "sip_exporter_ser")
}

// ADDED: getSEER reads sip_exporter_seer metric from exporter endpoint.
func getSEER(t *testing.T, endpoint string) float64 {
	t.Helper()
	return getMetric(t, endpoint, "sip_exporter_seer")
}

func getISA(t *testing.T, endpoint string) float64 {
	t.Helper()
	return getMetric(t, endpoint, "sip_exporter_isa")
}

func getSessions(t *testing.T, endpoint string) float64 {
	t.Helper()
	return getMetric(t, endpoint, "sip_exporter_sessions")
}

func getSCR(t *testing.T, endpoint string) float64 {
	t.Helper()
	return getMetric(t, endpoint, "sip_exporter_scr")
}

func getASR(t *testing.T, endpoint string) float64 {
	t.Helper()
	return getMetric(t, endpoint, "sip_exporter_asr")
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

func getRRD(t *testing.T, endpoint string) float64 {
	t.Helper()
	sum := getMetric(t, endpoint, "sip_exporter_rrd_sum")
	count := getMetric(t, endpoint, "sip_exporter_rrd_count")
	if count == 0 {
		return 0
	}
	return sum / count
}

func getTTR(t *testing.T, endpoint string) float64 {
	t.Helper()
	sum := getMetric(t, endpoint, "sip_exporter_ttr_sum")
	count := getMetric(t, endpoint, "sip_exporter_ttr_count")
	if count == 0 {
		return 0
	}
	return sum / count
}

func getPDD(t *testing.T, endpoint string) float64 {
	t.Helper()
	sum := getMetric(t, endpoint, "sip_exporter_pdd_sum")
	count := getMetric(t, endpoint, "sip_exporter_pdd_count")
	if count == 0 {
		return 0
	}
	return sum / count
}

func (e *testEnv) getSERByCarrier(t *testing.T) float64 {
	t.Helper()
	return getMetricWithCarrier(t, e.endpoint, "sip_exporter_ser", e.carrier)
}

func (e *testEnv) getSEERByCarrier(t *testing.T) float64 {
	t.Helper()
	return getMetricWithCarrier(t, e.endpoint, "sip_exporter_seer", e.carrier)
}

func (e *testEnv) getISAByCarrier(t *testing.T) float64 {
	t.Helper()
	return getMetricWithCarrier(t, e.endpoint, "sip_exporter_isa", e.carrier)
}

func (e *testEnv) getSCRByCarrier(t *testing.T) float64 {
	t.Helper()
	return getMetricWithCarrier(t, e.endpoint, "sip_exporter_scr", e.carrier)
}

func (e *testEnv) getASRByCarrier(t *testing.T) float64 {
	t.Helper()
	return getMetricWithCarrier(t, e.endpoint, "sip_exporter_asr", e.carrier)
}

func (e *testEnv) getNERByCarrier(t *testing.T) float64 {
	t.Helper()
	return getMetricWithCarrier(t, e.endpoint, "sip_exporter_ner", e.carrier)
}

func (e *testEnv) getSDCByCarrier(t *testing.T) float64 {
	t.Helper()
	return getMetricWithCarrier(t, e.endpoint, "sip_exporter_sdc_total", e.carrier)
}

func (e *testEnv) getISSByCarrier(t *testing.T) float64 {
	t.Helper()
	return getMetricWithCarrier(t, e.endpoint, "sip_exporter_iss_total", e.carrier)
}

func (e *testEnv) getSPDByCarrier(t *testing.T) float64 {
	t.Helper()
	sum := getMetricWithCarrier(t, e.endpoint, "sip_exporter_spd_sum", e.carrier)
	count := getMetricWithCarrier(t, e.endpoint, "sip_exporter_spd_count", e.carrier)
	if count == 0 {
		return 0
	}
	return sum / count
}

func (e *testEnv) getRRDByCarrier(t *testing.T) float64 {
	t.Helper()
	sum := getMetricWithCarrier(t, e.endpoint, "sip_exporter_rrd_sum", e.carrier)
	count := getMetricWithCarrier(t, e.endpoint, "sip_exporter_rrd_count", e.carrier)
	if count == 0 {
		return 0
	}
	return sum / count
}

func (e *testEnv) getTTRByCarrier(t *testing.T) float64 {
	t.Helper()
	sum := getMetricWithCarrier(t, e.endpoint, "sip_exporter_ttr_sum", e.carrier)
	count := getMetricWithCarrier(t, e.endpoint, "sip_exporter_ttr_count", e.carrier)
	if count == 0 {
		return 0
	}
	return sum / count
}

func (e *testEnv) getPDDByCarrier(t *testing.T) float64 {
	t.Helper()
	sum := getMetricWithCarrier(t, e.endpoint, "sip_exporter_pdd_sum", e.carrier)
	count := getMetricWithCarrier(t, e.endpoint, "sip_exporter_pdd_count", e.carrier)
	if count == 0 {
		return 0
	}
	return sum / count
}

func (e *testEnv) getORDByCarrier(t *testing.T) float64 {
	t.Helper()
	return getMetricWithCarrier(t, e.endpoint, "sip_exporter_ord_count", e.carrier)
}

func (e *testEnv) getLRDByCarrier(t *testing.T) float64 {
	t.Helper()
	return getMetricWithCarrier(t, e.endpoint, "sip_exporter_lrd_count", e.carrier)
}

func (e *testEnv) waitForSessionsZeroByCarrier(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		return getMetricWithCarrier(t, e.endpoint, "sip_exporter_sessions", e.carrier) <= 2
	}, 10*time.Second, 300*time.Millisecond,
		"sessions should reach ≤2 after all calls terminated (packet-capture contention tolerance)")

	assertSelfMonitoringHealthy(t, e.endpoint)
}

// isUDPPortInUse checks if a UDP port is in use by reading /proc/net/udp.
// Unlike net.ListenUDP, this does NOT bind to the port, avoiding a TOCTOU race
// where the probe itself prevents the target process (SIPp) from binding.
func isUDPPortInUse(port string) bool {
	data, err := os.ReadFile("/proc/net/udp")
	if err != nil {
		return false
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return false
	}
	portHex := fmt.Sprintf(":%04X", p)
	for _, line := range strings.Split(string(data), "\n")[1:] { // skip header
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// local_address field format: IP:PORT in hex (e.g. 0100007F:4E20)
		if strings.HasSuffix(fields[1], portHex) {
			return true
		}
	}
	return false
}

// dumpUDPPort logs /proc/net/udp lines matching the given port for diagnostics.
func dumpUDPPort(t *testing.T, port string) {
	t.Helper()
	data, err := os.ReadFile("/proc/net/udp")
	if err != nil {
		t.Logf("dumpUDPPort: cannot read /proc/net/udp: %v", err)
		return
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return
	}
	portHex := fmt.Sprintf(":%04X", p)
	var matches []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, portHex) {
			matches = append(matches, strings.TrimSpace(line))
		}
	}
	if len(matches) > 0 {
		t.Logf("dumpUDPPort(%s): port IN USE:\n%s", port, strings.Join(matches, "\n"))
	} else {
		t.Logf("dumpUDPPort(%s): port NOT found in /proc/net/udp", port)
	}
	if out, err := exec.Command("ss", "-lunp").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, ":"+port+" ") || strings.Contains(line, ":"+port+"\t") {
				t.Logf("ss: %s", strings.TrimSpace(line))
			}
		}
	}
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
