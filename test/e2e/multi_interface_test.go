//go:build e2e

package e2e

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	veth0aName = "veth0a"
	veth0bName = "veth0b"
	veth0aIP   = "10.10.0.1"
	veth0bIP   = "10.10.0.2"
)

var (
	vethMu  sync.Mutex
	vethRef int
)

// setupVethPair creates a veth pair (veth0a/veth0b) for multi-interface capture
// tests. Uses a privileged Docker container with iproute2 installed (Alpine's
// busybox ip does not support `link add type veth peer name`). Precedent:
// test/e2e/rtp/degradation_test.go startTCContainer.
// Reference-counted: the pair persists until the last parallel test finishes,
// matching the setupSecondaryIPs pattern.
func setupVethPair(t *testing.T) {
	t.Helper()

	vethMu.Lock()
	vethRef++
	needCreate := vethRef == 1
	vethMu.Unlock()

	t.Cleanup(func() {
		vethMu.Lock()
		vethRef--
		needDelete := vethRef == 0
		vethMu.Unlock()

		if !needDelete {
			return
		}
		// busybox `ip link del` is sufficient (no `type veth` syntax needed).
		_ = exec.Command("docker", "run", "--rm", "--privileged", "--network", "host",
			"--entrypoint", "", "alpine",
			"sh", "-c", "ip link del "+veth0aName+" 2>/dev/null || true",
		).Run()
	})

	if !needCreate {
		return
	}

	// All commands run in a single fresh Alpine container (busybox ip lacks
	// `link add type veth peer name`, so iproute2 is installed first).
	// `set -e` ensures apk/link-set failures abort. `|| true` guards only the
	// idempotent add commands so re-runs work when a previous test left the
	// pair half-created.
	script := strings.Join([]string{
		"set -e",
		"apk add --no-cache iproute2 > /dev/null",
		"ip link add " + veth0aName + " type veth peer name " + veth0bName + " || true",
		"ip addr add " + veth0aIP + "/24 dev " + veth0aName + " || true",
		"ip addr add " + veth0bIP + "/24 dev " + veth0bName + " || true",
		"ip link set " + veth0aName + " up",
		"ip link set " + veth0bName + " up",
	}, "\n")

	out, err := exec.Command("docker", "run", "--rm", "--privileged", "--network", "host",
		"--entrypoint", "", "alpine",
		"sh", "-c", script,
	).CombinedOutput()
	require.NoError(t, err, "failed to create veth pair: %s", string(out))
}

// TestMultiInterface_RegisterOnBothNICs verifies that the exporter captures SIP
// traffic from multiple interfaces simultaneously and aggregates metrics across
// all AF_PACKET sockets.
//
// Interfaces configured: lo, veth0a, veth0b (3 NICs).
// The third NIC (veth0b) is required because veth is not loopback: with
// IGNORE_OUTGOING=true, only RX packets are captured. REGISTER flows UAC→UAS
// (captured on veth0a RX), 200 OK flows UAS→UAC (captured on veth0b RX).
// Without veth0b the 200 OK would be missed and register_success_total /
// active_registrations could not be asserted.
//
// Two REGISTER flows:
//  1. lo:        UAC 127.0.0.1 → UAS 127.0.0.1 (both directions via lo RX)
//  2. veth pair: UAC on veth0b (10.10.0.2) → UAS on veth0a (10.10.0.1)
//
// Verifies aggregated metrics:
//   - register_total ≥ 2*callCount (REGISTER seen on both flows)
//   - register_success_total ≥ 2*callCount (200 OK seen for both flows)
//   - active_registrations ≥ 2 (two distinct AORs stored: 127.0.0.1 and 10.10.0.2)
//   - socket_packets_received_total > 0 (stats aggregation across sockets works)
func TestMultiInterface_RegisterOnBothNICs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupVethPair(t)

	extraEnv := map[string]string{
		"SIP_EXPORTER_INTERFACE": "lo," + veth0aName + "," + veth0bName,
	}
	env := newTestEnvWithExtraEnv(ctx, t, "", extraEnv)

	const callCount = 10

	// Flow 1: loopback. UAC and UAS both on 127.0.0.1.
	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", callCount, env)

	// Flow 2: veth pair. UAS binds to veth0a IP, UAC binds to veth0b IP.
	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac.xml", callCount, env, veth0aIP, veth0bIP)

	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")
	t.Logf("register_total=%.0f (want >= %d)", registerTotal, 2*callCount)
	require.GreaterOrEqual(t, registerTotal, 2.0*callCount,
		"REGISTER seen on both interfaces")

	success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")
	t.Logf("register_success_total=%.0f (want >= %d)", success, 2*callCount)
	require.GreaterOrEqual(t, success, 2.0*callCount,
		"200 OK seen for both flows")

	// active_registrations counts distinct AORs in registerExpiryTracker.
	// Flow 1 AOR: sipp@127.0.0.1, Flow 2 AOR: sipp@10.10.0.2 — two distinct entries.
	require.Eventually(t, func() bool {
		return getMetric(t, env.endpoint, "sip_exporter_active_registrations") >= 2.0
	}, 5*time.Second, 500*time.Millisecond,
		"two distinct AORs stored from both interfaces")

	assertSelfMonitoringHealthy(t, env.endpoint)
}
