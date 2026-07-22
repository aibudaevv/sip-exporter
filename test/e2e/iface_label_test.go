//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	nsVethHost  = "sipns0"
	nsVethGuest = "sipns1"
	nsHostIP    = "10.210.0.1"
	nsGuestIP   = "10.210.0.2"
)

// setupVethNetns creates a veth pair bridging the host and an isolated network
// namespace (pause container). Traffic from a container sharing the pause
// container's netns physically traverses the host veth (sipns0), allowing
// per-interface AF_PACKET capture to distinguish it from loopback traffic.
//
// Returns the pause container ID (for use with --network container:<id>).
//
// Unlike setupVethPair (which puts both ends in the host netns), this function
// puts the guest end in a separate netns, so the kernel local routing table
// does NOT redirect traffic through lo.
func setupVethNetns(t *testing.T) string {
	t.Helper()

	// 1. Create pause container with isolated network namespace.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pauseOut, err := exec.CommandContext(ctx, "docker", "run", "-d", "--rm",
		"--network", "none", "--cap-add", "NET_ADMIN",
		"--name", "sip-pause-"+strconv.Itoa(os.Getpid()),
		"--entrypoint", "", "alpine", "sleep", "infinity",
	).Output()
	require.NoError(t, err, "failed to create pause container")
	pauseID := strings.TrimSpace(string(pauseOut))
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", pauseID).Run()
	})

	// 2. Get the pause container's PID (host-visible).
	pidOut, err := exec.Command("docker", "inspect", "-f", "{{.State.Pid}}", pauseID).Output()
	require.NoError(t, err, "failed to get pause container PID")
	pid := strings.TrimSpace(string(pidOut))

	// 3. Create veth pair, move guest end into pause netns, configure IPs.
	//    Runs in a privileged host-network host-pid Alpine container so that
	//    nsenter can access the pause container's network namespace.
	script := strings.Join([]string{
		"set -e",
		"apk add --no-cache iproute2 > /dev/null",
		"ip link add " + nsVethHost + " type veth peer name " + nsVethGuest + " || true",
		"ip addr add " + nsHostIP + "/24 dev " + nsVethHost + " || true",
		"ip link set " + nsVethHost + " up",
		"ip link set " + nsVethGuest + " netns " + pid,
		"nsenter -t " + pid + " -n ip addr add " + nsGuestIP + "/24 dev " + nsVethGuest,
		"nsenter -t " + pid + " -n ip link set " + nsVethGuest + " up",
		"nsenter -t " + pid + " -n ip link set lo up",
	}, "\n")

	setupCtx, setupCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer setupCancel()
	out, err := exec.CommandContext(setupCtx, "docker", "run", "--rm",
		"--privileged", "--network", "host", "--pid", "host",
		"--entrypoint", "", "alpine",
		"sh", "-c", script,
	).CombinedOutput()
	require.NoError(t, err, "failed to create veth netns: %s", string(out))

	// 4. Cleanup: delete host veth (auto-removes the guest end).
	t.Cleanup(func() {
		_ = exec.Command("docker", "run", "--rm", "--privileged", "--network", "host",
			"--entrypoint", "", "alpine",
			"sh", "-c", "ip link del "+nsVethHost+" 2>/dev/null || true",
		).Run()
	})

	return pauseID
}

// runSippUACInNetns runs a SIPp UAC inside the pause container's network
// namespace (--network container:<pauseID>). The UAC sends from nsGuestIP to
// the UAS at uasIP:env.sippPort. Waits for completion.
func runSippUACInNetns(ctx context.Context, t *testing.T, pauseID, uacScenario string, callCount int, env *testEnv, uasIP string) {
	t.Helper()

	uacPath := absScenarioPath(t, uacScenario)
	sippVol := filepath.Dir(uacPath)
	uacFile := filepath.Base(uacScenario)

	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--network", "container:"+pauseID,
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/"+uacFile,
		"-i", nsGuestIP,
		"-p", env.sippClientPort,
		"-m", strconv.Itoa(callCount),
		"-nr",
		uasIP+":"+env.sippPort,
	)
	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
		cmd.Stdout = &testWriter{t}
		cmd.Stderr = &testWriter{t}
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}
	require.NoError(t, cmd.Run(), "SIPp UAC in netns failed")
}

// TestIfaceLabel_MultiInterface verifies that the iface label correctly
// identifies which NIC captured each packet, using a real second interface
// (separate network namespace via pause container + veth pair).
//
// Setup:
//   - lo:      standard loopback
//   - sipns0:  host end of veth pair bridging to an isolated netns
//
// Flow 1 (lo):      100 calls on 127.0.0.1 → iface="lo"
// Flow 2 (sipns0):   50 calls from UAC in netns (10.210.0.2) → UAS on host (10.210.0.1)
//
// On sipns0, IGNORE_OUTGOING=true captures only RX (UAC→UAS direction). The
// 200 OK (UAS→UAC, TX on sipns0) is not captured, so invite_200_total is only
// asserted on lo.
func TestIfaceLabel_MultiInterface(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pauseID := setupVethNetns(t)

	const loCalls = 100
	const nsCalls = 50

	env := newTestEnvWithExtraEnv(ctx, t, "", map[string]string{
		"SIP_EXPORTER_INTERFACE": fmt.Sprintf("%s,%s", testInterface, nsVethHost),
	})

	// Flow 1: lo (standard SIPp on 127.0.0.1).
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", loCalls, env)

	// Flow 2: netns (UAC in isolated namespace → UAS on host veth IP).
	// UAS runs on host network, listening on the veth host IP.
	uasCtx, uasCancel := context.WithTimeout(ctx, 60*time.Second)
	defer uasCancel()
	uasPath := absScenarioPath(t, "uas_100.xml")
	sippVol := filepath.Dir(uasPath)
	uasCmd := exec.CommandContext(uasCtx, "docker", "run", "--rm",
		"--network", "host",
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/uas_100.xml",
		"-i", nsHostIP,
		"-p", env.sippPort,
		"-m", strconv.Itoa(nsCalls),
		"-nr", "-nostdin",
	)
	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
		uasCmd.Stdout = &testWriter{t}
		uasCmd.Stderr = &testWriter{t}
	} else {
		uasCmd.Stdout = io.Discard
		uasCmd.Stderr = io.Discard
	}
	require.NoError(t, uasCmd.Start())
	require.Eventually(t, func() bool {
		return isUDPPortInUse(env.sippPort)
	}, 10*time.Second, 50*time.Millisecond, "UAS should start listening on %s:%s", nsHostIP, env.sippPort)

	runSippUACInNetns(ctx, t, pauseID, "uac_100.xml", nsCalls, env, nsHostIP)
	_ = uasCmd.Wait()
	waitForMetricStable(t, env.endpoint)

	tests := []struct {
		name        string
		metricName  string
		labelFilter string
		want        float64
		atLeast     bool
	}{
		{"invite_total on lo", "sip_exporter_invite_total", `iface="lo"`, float64(loCalls), false},
		{"invite_total on sipns0", "sip_exporter_invite_total", fmt.Sprintf(`iface="%s"`, nsVethHost), float64(nsCalls), false},
		{"invite_200_total on lo", "sip_exporter_invite_200_total", `iface="lo"`, float64(loCalls), false},
		{"pkts_recv on lo", "sip_exporter_socket_packets_received_total", `iface="lo"`, 0, true},
		{"pkts_recv on sipns0", "sip_exporter_socket_packets_received_total", fmt.Sprintf(`iface="%s"`, nsVethHost), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.True(t, metricWithLabelExists(t, env.endpoint, tt.metricName, tt.labelFilter),
				"%s{%s} should exist", tt.metricName, tt.labelFilter)
			val := getMetricWithLabel(t, env.endpoint, tt.metricName, tt.labelFilter)
			t.Logf("%s{%s} = %.0f", tt.metricName, tt.labelFilter, val)
			if tt.atLeast {
				require.Greater(t, val, tt.want)
			} else {
				require.Equal(t, tt.want, val)
			}
		})
	}

}
