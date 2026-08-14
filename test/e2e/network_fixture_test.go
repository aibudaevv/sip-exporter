//go:build e2e

package e2e

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	nsVethGuest = "eth0"
	nsGuestIP   = "10.210.0.2"
	nsHostIP    = "10.210.0.1"

	peerSippScenarioChildEnv = "SIP_EXPORTER_E2E_PEER_SIPP_SCENARIO_CHILD"
)

var networkFixtureMu sync.Mutex

type networkFixture struct {
	hostInterface   string
	peerInterface   string
	hostIP          string
	peerIP          string
	peerContainerID string
	cleanup         func()
}

// newNetworkFixture exclusively owns a veth pair between the host and an
// isolated container network namespace. The returned cleanup removes the
// topology and releases the next fixture user.
func newNetworkFixture(t *testing.T) networkFixture {
	t.Helper()

	// One fixture owns the fixed veth/container names for its full lifetime.
	// This also prevents concurrent AF_PACKET captures from sharing topology.
	networkFixtureMu.Lock()
	hostInterface := "sipe2e" + strconv.Itoa(os.Getpid())
	peerContainerID := ""
	peerContainerName := "sip-exporter-e2e-peer-" + strconv.Itoa(os.Getpid())
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if peerContainerID != "" {
				_ = exec.CommandContext(ctx, "docker", "rm", "-f", peerContainerID).Run()
			}
			_, _ = runHostIP(ctx, "link", "delete", hostInterface)
			networkFixtureMu.Unlock()
		})
	}
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pauseOut, err := exec.CommandContext(ctx, "docker", "run", "-d", "--rm",
		"--network", "none",
		"--cap-add", "NET_ADMIN",
		"--name", peerContainerName,
		"--entrypoint", "", "alpine", "sleep", "infinity",
	).Output()
	if err != nil {
		cleanup()
		require.NoError(t, err, "failed to create pause container")
	}
	peerContainerID = strings.TrimSpace(string(pauseOut))

	pidOut, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Pid}}", peerContainerID).Output()
	if err != nil {
		cleanup()
		require.NoError(t, err, "failed to get peer container PID")
	}
	peerPID, err := strconv.Atoi(strings.TrimSpace(string(pidOut)))
	if err != nil {
		cleanup()
		require.NoError(t, err, "invalid peer container PID %q", string(pidOut))
	}

	out, err := runHostIP(ctx, "link", "add", hostInterface, "type", "veth")
	if err != nil {
		cleanup()
		require.NoError(t, err, "failed to create host veth: %s", string(out))
	}
	linkOut, err := runHostIP(ctx, "link", "show", "dev", hostInterface)
	if err != nil {
		cleanup()
		require.NoError(t, err, "failed to inspect host veth: %s", string(linkOut))
	}
	peerHostInterface, ok := vethPeerName(string(linkOut))
	require.True(t, ok, "host veth must report its peer name: %s", string(linkOut))

	for _, args := range [][]string{
		{"link", "set", peerHostInterface, "netns", strconv.Itoa(peerPID)},
		{"addr", "add", nsHostIP + "/30", "dev", hostInterface},
		{"link", "set", hostInterface, "up"},
	} {
		out, err := runHostIP(ctx, args...)
		if err != nil {
			cleanup()
			require.NoError(t, err, "failed to configure host veth: %s", string(out))
		}
	}
	for _, args := range [][]string{
		{"link", "set", peerHostInterface, "name", nsVethGuest},
		{"addr", "add", nsGuestIP + "/30", "dev", nsVethGuest},
		{"link", "set", nsVethGuest, "up"},
	} {
		dockerArgs := append([]string{"exec", peerContainerID, "ip"}, args...)
		out, err := exec.CommandContext(ctx, "docker", dockerArgs...).CombinedOutput()
		if err != nil {
			cleanup()
			require.NoError(t, err, "failed to configure peer veth: %s", string(out))
		}
	}

	return networkFixture{
		hostInterface:   hostInterface,
		peerInterface:   nsVethGuest,
		hostIP:          nsHostIP,
		peerIP:          nsGuestIP,
		peerContainerID: peerContainerID,
		cleanup:         cleanup,
	}
}

func runHostIP(ctx context.Context, args ...string) ([]byte, error) {
	dockerArgs := []string{"run", "--rm", "--network", "host", "--pid", "host", "--privileged", "alpine", "ip"}
	dockerArgs = append(dockerArgs, args...)
	return exec.CommandContext(ctx, "docker", dockerArgs...).CombinedOutput()
}

func vethPeerName(link string) (string, bool) {
	for _, field := range strings.Fields(link) {
		_, peer, ok := strings.Cut(strings.TrimSuffix(field, ":"), "@")
		if ok && peer != "" {
			return peer, true
		}
	}
	return "", false
}

func (f networkFixture) peerRoute(t *testing.T, destination string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "docker", "exec", f.peerContainerID,
		"ip", "route", "get", destination).CombinedOutput()
	require.NoError(t, err, "failed to resolve peer route to %s: %s", destination, string(out))
	return strings.TrimSpace(string(out))
}

func routeInterface(route string) (string, bool) {
	fields := strings.Fields(route)
	for i, field := range fields {
		if field == "dev" && i+1 < len(fields) {
			return fields[i+1], true
		}
	}
	return "", false
}

func containerUDPPortInUse(ctx context.Context, container, port string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	data, err := exec.CommandContext(probeCtx,
		"docker", "exec", container, "cat", "/proc/net/udp").Output()
	return err == nil && udpPortInUse(data, port)
}

// runSippUACInNetns runs a SIPp UAC inside the fixture peer's network
// namespace. The UAC sends from peerIP to the host endpoint.
func runSippUACInNetns(
	ctx context.Context,
	t *testing.T,
	peerContainerID, uacScenario string,
	peerIP string,
	callCount int,
	env *testEnv,
	hostIP string,
) (string, string, error) {
	t.Helper()

	uacPath := absScenarioPath(t, uacScenario)
	sippVol := filepath.Dir(uacPath)

	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--network", "container:"+peerContainerID,
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/"+filepath.Base(uacScenario),
		"-i", peerIP,
		"-p", env.sippClientPort,
		"-m", strconv.Itoa(callCount),
		"-nr",
		hostIP+":"+env.sippPort,
	)
	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
		cmd.Stdout = &testWriter{t}
		cmd.Stderr = &testWriter{t}
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}
	return runSippCommand(cmd, cmd.Stdout, cmd.Stderr)
}

func (f networkFixture) runSippScenarioFromPeer(
	ctx context.Context,
	t *testing.T,
	uasScenario, uacScenario string,
	callCount int,
	env *testEnv,
) sippResult {
	t.Helper()

	uasPath := absScenarioPath(t, uasScenario)
	sippVol := filepath.Dir(uasPath)
	uasContainerName, err := nextSippContainerName()
	require.NoError(t, err)

	var stdout, stderr io.Writer = &testWriter{t}, &testWriter{t}
	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") != "true" {
		stdout, stderr = io.Discard, io.Discard
	}

	uasCmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--name", uasContainerName,
		"--network", "host",
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/"+filepath.Base(uasScenario),
		"-i", f.hostIP,
		"-p", env.sippPort,
		"-m", strconv.Itoa(callCount),
		"-nr", "-nostdin",
	)
	uasCmd.Stdout = stdout
	uasCmd.Stderr = stderr
	require.NoError(t, uasCmd.Start())
	t.Cleanup(func() {
		if err := removeSippContainer(uasContainerName); err != nil {
			t.Logf("peer UAS cleanup: %v", err)
		}
	})

	require.Eventually(t, func() bool {
		return containerUDPPortInUse(t.Context(), uasContainerName, env.sippPort)
	}, 10*time.Second, 50*time.Millisecond,
		"UAS should start listening on %s:%s", f.hostIP, env.sippPort)
	uacStdout, uacStderr, err := runSippUACInNetns(
		ctx, t, f.peerContainerID, uacScenario, f.peerIP, callCount, env, f.hostIP,
	)
	if err != nil {
		stopErr := removeSippContainer(uasContainerName)
		_ = uasCmd.Wait()
		t.Logf("peer UAC stdout:\n%s", uacStdout)
		t.Logf("peer UAC stderr:\n%s", uacStderr)
		require.NoError(t, stopErr, "stop peer UAS after failed UAC execution")
		require.NoError(t, err, "SIPp UAC in netns failed")
	}
	require.NoError(t, uasCmd.Wait(), "fixture SIPp UAS failed")
	waitForMetricStable(t, env.endpoint)

	return sippResult{totalCalls: callCount}
}

func TestNetworkFixturePeerRouteUsesVeth(t *testing.T) {
	fixture := newNetworkFixture(t)
	t.Cleanup(fixture.cleanup)

	route := fixture.peerRoute(t, fixture.hostIP)
	device, ok := routeInterface(route)
	require.True(t, ok)
	require.Equal(t, fixture.peerInterface, device)
}

func TestRouteInterface(t *testing.T) {
	tests := []struct {
		name       string
		route      string
		wantDevice string
		wantOK     bool
	}{
		{name: "exact", route: "10.210.0.1 dev sipns1 src 10.210.0.2", wantDevice: "sipns1", wantOK: true},
		{name: "prefix collision", route: "10.210.0.1 dev sipns10 src 10.210.0.2", wantDevice: "sipns10", wantOK: true},
		{name: "missing dev", route: "10.210.0.1 via 10.210.0.254", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device, ok := routeInterface(tt.route)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantDevice, device)
		})
	}
}

func TestVethPeerName(t *testing.T) {
	tests := []struct {
		name string
		link string
		want string
		ok   bool
	}{
		{name: "veth peer", link: "153: sipe2e123@veth0: <BROADCAST>", want: "veth0", ok: true},
		{name: "no peer", link: "153: sipe2e123: <BROADCAST>", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peer, ok := vethPeerName(tt.link)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, peer)
		})
	}
}

func TestRunSippScenarioFromPeerWaitsForUASOnUACFailure(t *testing.T) {
	testBinary, err := os.Executable()
	require.NoError(t, err)

	attemptsFile := filepath.Join(t.TempDir(), "attempts")
	stopFile := filepath.Join(t.TempDir(), "stopped")
	exitFile := filepath.Join(t.TempDir(), "uas-exited")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, testBinary,
		"-test.run=^TestRunSippScenarioFromPeerFailureChild$", "-test.v")
	cmd.Env = append(os.Environ(),
		peerSippScenarioChildEnv+"=true",
		sippAttemptsFileEnv+"="+attemptsFile,
		sippStopFileEnv+"="+stopFile,
		sippUASExitFileEnv+"="+exitFile,
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, ctx.Err(), "peer runner contract child must not hang")
	require.Error(t, err, "peer runner must fail when its UAC fails")
	require.Contains(t, string(output), "--- FAIL: TestRunSippScenarioFromPeerFailureChild")
	require.Contains(t, string(output), "stdout diagnostic")
	require.Contains(t, string(output), "stderr diagnostic")

	attempts, err := os.ReadFile(attemptsFile)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(attempts), "attempt\n"))
	stopMarker, err := os.ReadFile(stopFile)
	require.NoError(t, err, "peer runner must stop UAS before reporting failure")
	require.Equal(t, "stopped\n", string(stopMarker))
	exitMarker, err := os.ReadFile(exitFile)
	require.NoError(t, err, "peer runner must wait for the stopped UAS process")
	require.Equal(t, "exited\n", string(exitMarker))
}

func TestRunSippScenarioFromPeerFailureChild(t *testing.T) {
	if os.Getenv(peerSippScenarioChildEnv) != "true" {
		t.Skip("subprocess helper")
	}

	testBinary, err := os.Executable()
	require.NoError(t, err)
	binDir := t.TempDir()
	require.NoError(t, os.Symlink(testBinary, filepath.Join(binDir, "docker")))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(sippFailureHelperEnv, "true")
	t.Setenv(sippHelperExitCodeEnv, "1")

	_, sippPort, sippClientPort := allocatePorts()
	env := &testEnv{sippPort: sippPort, sippClientPort: sippClientPort}
	fixture := networkFixture{
		hostIP:          "127.0.0.1",
		peerIP:          "127.0.0.1",
		peerContainerID: "test-peer",
	}
	fixture.runSippScenarioFromPeer(
		t.Context(), t, "uas_100.xml", "uac_100.xml", 1, env,
	)
}
