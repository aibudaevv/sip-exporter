//go:build e2e

package e2e

import (
	"context"
	"io"
	"net"
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
		// Wait for the creating caller to finish — parallel top-level tests
		// may reach setupVethPair before the first caller's docker run completes.
		require.Eventually(t, func() bool {
			_, err := os.Stat("/sys/class/net/" + veth0aName)
			return err == nil
		}, 15*time.Second, 200*time.Millisecond, "veth pair not created in time")
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

// TestMultiInterface_InviteFlowOnBothNICs verifies INVITE dialog matching
// across multiple interfaces with traffic on BOTH lo and veth pair.
// Each subtest starts a fresh exporter and runs flows on both interfaces.
//
// The veth flow exercises cross-NIC dialog correlation: INVITE captured on
// veth0a RX (UAC→UAS), 200 OK on veth0b RX (UAS→UAC). The dialog tracker must
// correlate both halves by Call-ID regardless of which NIC delivered them.
func TestMultiInterface_InviteFlowOnBothNICs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupVethPair(t)

	extraEnv := map[string]string{
		"SIP_EXPORTER_INTERFACE":   "lo," + veth0aName + "," + veth0bName,
		"SIP_EXPORTER_HOST_LABELS": "true",
	}
	callCount := 10

	tests := []struct {
		description    string
		loUAS          string
		loUAC          string
		vethUAS        string
		vethUAC        string
		wantLoInvite   float64
		wantLo200OK    float64
		wantVethInvite float64
		wantVeth200OK  float64
		wantSER        float64
	}{
		{
			description:    "lo fail (uas_0) + veth success (uas_100) → SER = 50%",
			loUAS:          "uas_0.xml",
			loUAC:          "uac_0.xml",
			vethUAS:        "uas_100.xml",
			vethUAC:        "uac_100.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    0,
			wantVethInvite: float64(callCount),
			wantVeth200OK:  float64(callCount),
			wantSER:        50.0,
		},
		{
			description:    "lo success (uas_100) + veth success (uas_100) → SER = 100%",
			loUAS:          "uas_100.xml",
			loUAC:          "uac_100.xml",
			vethUAS:        "uas_100.xml",
			vethUAC:        "uac_100.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    float64(callCount),
			wantVethInvite: float64(callCount),
			wantVeth200OK:  float64(callCount),
			wantSER:        100.0,
		},
		{
			description:    "lo fail (uas_0) + veth fail (uas_0) → SER = 0%",
			loUAS:          "uas_0.xml",
			loUAC:          "uac_0.xml",
			vethUAS:        "uas_0.xml",
			vethUAC:        "uac_0.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    0,
			wantVethInvite: float64(callCount),
			wantVeth200OK:  0,
			wantSER:        0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()
			env := newTestEnvWithExtraEnv(ctx, t, "", extraEnv)

			runSippScenario(ctx, t, tt.loUAS, tt.loUAC, callCount, env)
			runSippScenarioWithIPs(ctx, t, tt.vethUAS, tt.vethUAC, callCount, env, veth0aIP, veth0bIP)

			loLabel := `called_host="127.0.0.1"`
			gotLoInvite := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", loLabel)
			gotLo200OK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", loLabel)

			vethLabel := `called_host="10.10.0.1"`
			gotVethInvite := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", vethLabel)
			gotVeth200OK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", vethLabel)

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_ser"), "SER metric must exist")
			gotSER := getSER(t, env.endpoint)

			t.Logf(
				"lo: invite %.0f/%.0f, 200 OK %.0f/%.0f | veth: invite %.0f/%.0f, 200 OK %.0f/%.0f | SER %.2f%%/%.1f%%",
				gotLoInvite,
				tt.wantLoInvite,
				gotLo200OK,
				tt.wantLo200OK,
				gotVethInvite,
				tt.wantVethInvite,
				gotVeth200OK,
				tt.wantVeth200OK,
				gotSER,
				tt.wantSER,
			)

			require.InDelta(t, tt.wantLoInvite, gotLoInvite, ratioDelta, "lo INVITE: %s", tt.description)
			require.InDelta(t, tt.wantLo200OK, gotLo200OK, ratioDelta, "lo 200 OK: %s", tt.description)
			require.InDelta(t, tt.wantVethInvite, gotVethInvite, ratioDelta, "veth INVITE: %s", tt.description)
			require.InDelta(t, tt.wantVeth200OK, gotVeth200OK, ratioDelta, "veth 200 OK: %s", tt.description)
			require.InDelta(t, tt.wantSER, gotSER, ratioDelta, "SER: %s", tt.description)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestMultiInterface_SER verifies SER computation with traffic on BOTH lo and
// veth pair simultaneously. Each subtest starts a fresh exporter and runs flows
// on both interfaces, then verifies per-host metrics to prove multi-NIC capture.
//
// SER = invite_200_total / (invite_total - invite_3xx_total) × 100.
// The critical assertion is invite_200_total{called_host="10.10.0.1"} >= 10:
// the 200 OK travels UAS(veth0a)→UAC(veth0b) and can only be counted if veth0b
// is captured.
func TestMultiInterface_SER(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupVethPair(t)

	extraEnv := map[string]string{
		"SIP_EXPORTER_INTERFACE":   "lo," + veth0aName + "," + veth0bName,
		"SIP_EXPORTER_HOST_LABELS": "true",
	}
	callCount := 10

	tests := []struct {
		description    string
		loUAS          string
		loUAC          string
		vethUAS        string
		vethUAC        string
		wantLoInvite   float64
		wantLo200OK    float64
		wantVethInvite float64
		wantVeth200OK  float64
		wantSER        float64
	}{
		{
			description:    "lo fail (uas_0) + veth success (uas_100) → SER = 50%",
			loUAS:          "uas_0.xml",
			loUAC:          "uac_0.xml",
			vethUAS:        "uas_100.xml",
			vethUAC:        "uac_100.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    0,
			wantVethInvite: float64(callCount),
			wantVeth200OK:  float64(callCount),
			wantSER:        50.0,
		},
		{
			description:    "lo success (uas_100) + veth success (uas_100) → SER = 100%",
			loUAS:          "uas_100.xml",
			loUAC:          "uac_100.xml",
			vethUAS:        "uas_100.xml",
			vethUAC:        "uac_100.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    float64(callCount),
			wantVethInvite: float64(callCount),
			wantVeth200OK:  float64(callCount),
			wantSER:        100.0,
		},
		{
			description:    "lo fail (uas_0) + veth fail (uas_0) → SER = 0%",
			loUAS:          "uas_0.xml",
			loUAC:          "uac_0.xml",
			vethUAS:        "uas_0.xml",
			vethUAC:        "uac_0.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    0,
			wantVethInvite: float64(callCount),
			wantVeth200OK:  0,
			wantSER:        0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()
			env := newTestEnvWithExtraEnv(ctx, t, "", extraEnv)

			runSippScenario(ctx, t, tt.loUAS, tt.loUAC, callCount, env)
			runSippScenarioWithIPs(ctx, t, tt.vethUAS, tt.vethUAC, callCount, env, veth0aIP, veth0bIP)

			loLabel := `called_host="127.0.0.1"`
			gotLoInvite := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", loLabel)
			gotLo200OK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", loLabel)

			vethLabel := `called_host="10.10.0.1"`
			gotVethInvite := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", vethLabel)
			gotVeth200OK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", vethLabel)

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_ser"), "SER metric must exist")
			gotSER := getSER(t, env.endpoint)

			t.Logf(
				"lo: invite %.0f/%.0f, 200 OK %.0f/%.0f | veth: invite %.0f/%.0f, 200 OK %.0f/%.0f | SER %.2f%%/%.1f%%",
				gotLoInvite,
				tt.wantLoInvite,
				gotLo200OK,
				tt.wantLo200OK,
				gotVethInvite,
				tt.wantVethInvite,
				gotVeth200OK,
				tt.wantVeth200OK,
				gotSER,
				tt.wantSER,
			)

			require.InDelta(t, tt.wantLoInvite, gotLoInvite, ratioDelta, "lo INVITE: %s", tt.description)
			require.InDelta(t, tt.wantLo200OK, gotLo200OK, ratioDelta, "lo 200 OK: %s", tt.description)
			require.InDelta(t, tt.wantVethInvite, gotVethInvite, ratioDelta, "veth INVITE: %s", tt.description)
			require.InDelta(t, tt.wantVeth200OK, gotVeth200OK, ratioDelta, "veth 200 OK: %s", tt.description)
			require.InDelta(t, tt.wantSER, gotSER, ratioDelta, "SER: %s", tt.description)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestMultiInterface_ASR verifies ASR computation with traffic on BOTH lo and
// veth pair simultaneously. Each subtest starts a fresh exporter and runs flows
// on both interfaces, then verifies per-host metrics to prove multi-NIC capture.
//
// The critical assertion is invite_200_total{called_host="10.10.0.1"} >= 10:
// the 200 OK travels UAS(veth0a)→UAC(veth0b) and can only be counted if veth0b
// is captured. invite_total{called_host="127.0.0.1"} >= 10 proves lo captured
// its traffic in the same test.
func TestMultiInterface_ASR(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupVethPair(t)

	extraEnv := map[string]string{
		"SIP_EXPORTER_INTERFACE":   "lo," + veth0aName + "," + veth0bName,
		"SIP_EXPORTER_HOST_LABELS": "true",
	}
	callCount := 10

	tests := []struct {
		description    string
		loUAS          string
		loUAC          string
		vethUAS        string
		vethUAC        string
		wantLoInvite   float64
		wantLo200OK    float64
		wantVethInvite float64
		wantVeth200OK  float64
		wantASR        float64
	}{
		{
			description:    "lo fail (uas_0) + veth success (uas_100) → ASR = 50%",
			loUAS:          "uas_0.xml",
			loUAC:          "uac_0.xml",
			vethUAS:        "uas_100.xml",
			vethUAC:        "uac_100.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    0,
			wantVethInvite: float64(callCount),
			wantVeth200OK:  float64(callCount),
			wantASR:        50.0,
		},
		{
			description:    "lo success (uas_100) + veth success (uas_100) → ASR = 100%",
			loUAS:          "uas_100.xml",
			loUAC:          "uac_100.xml",
			vethUAS:        "uas_100.xml",
			vethUAC:        "uac_100.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    float64(callCount),
			wantVethInvite: float64(callCount),
			wantVeth200OK:  float64(callCount),
			wantASR:        100.0,
		},
		{
			description:    "lo fail (uas_0) + veth fail (uas_0) → ASR = 0%",
			loUAS:          "uas_0.xml",
			loUAC:          "uac_0.xml",
			vethUAS:        "uas_0.xml",
			vethUAC:        "uac_0.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    0,
			wantVethInvite: float64(callCount),
			wantVeth200OK:  0,
			wantASR:        0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()
			env := newTestEnvWithExtraEnv(ctx, t, "", extraEnv)

			runSippScenario(ctx, t, tt.loUAS, tt.loUAC, callCount, env)
			runSippScenarioWithIPs(ctx, t, tt.vethUAS, tt.vethUAC, callCount, env, veth0aIP, veth0bIP)

			loLabel := `called_host="127.0.0.1"`
			gotLoInvite := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", loLabel)
			gotLo200OK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", loLabel)

			vethLabel := `called_host="10.10.0.1"`
			gotVethInvite := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", vethLabel)
			gotVeth200OK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", vethLabel)

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_asr"), "ASR metric must exist")
			gotASR := getASR(t, env.endpoint)

			t.Logf(
				"lo: invite %.0f/%.0f, 200 OK %.0f/%.0f | veth: invite %.0f/%.0f, 200 OK %.0f/%.0f | ASR %.2f%%/%.1f%%",
				gotLoInvite,
				tt.wantLoInvite,
				gotLo200OK,
				tt.wantLo200OK,
				gotVethInvite,
				tt.wantVethInvite,
				gotVeth200OK,
				tt.wantVeth200OK,
				gotASR,
				tt.wantASR,
			)

			require.InDelta(t, tt.wantLoInvite, gotLoInvite, ratioDelta, "lo INVITE: %s", tt.description)
			require.InDelta(t, tt.wantLo200OK, gotLo200OK, ratioDelta, "lo 200 OK: %s", tt.description)
			require.InDelta(t, tt.wantVethInvite, gotVethInvite, ratioDelta, "veth INVITE: %s", tt.description)
			require.InDelta(t, tt.wantVeth200OK, gotVeth200OK, ratioDelta, "veth 200 OK: %s", tt.description)
			require.InDelta(t, tt.wantASR, gotASR, ratioDelta, "ASR: %s", tt.description)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestMultiInterface_SDC verifies SDC (Session Disconnect Count) with traffic on
// BOTH lo and veth pair simultaneously. Each subtest starts a fresh exporter and
// runs flows on both interfaces, then verifies per-host metrics to prove multi-NIC
// capture.
//
// SDC counts completed sessions (BYE→200 OK). waitForSessionsZero is called
// before the SDC assertion because SDC only increments when a session completes
// (BYE processed). The critical multi-NIC assertion: invite_200_total{called_host=
// "10.10.0.1"} >= 10 proves veth0b captured the 200 OK, and SDC reflects sessions
// that completed via cross-NIC BYE→200 OK correlation.
func TestMultiInterface_SDC(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupVethPair(t)

	extraEnv := map[string]string{
		"SIP_EXPORTER_INTERFACE":   "lo," + veth0aName + "," + veth0bName,
		"SIP_EXPORTER_HOST_LABELS": "true",
	}
	callCount := 10

	tests := []struct {
		description    string
		loUAS          string
		loUAC          string
		vethUAS        string
		vethUAC        string
		wantLoInvite   float64
		wantLo200OK    float64
		wantVethInvite float64
		wantVeth200OK  float64
		wantSDC        float64
	}{
		{
			description:    "lo success + veth success → SDC = 20",
			loUAS:          "uas_100.xml",
			loUAC:          "uac_100.xml",
			vethUAS:        "uas_100.xml",
			vethUAC:        "uac_100.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    float64(callCount),
			wantVethInvite: float64(callCount),
			wantVeth200OK:  float64(callCount),
			wantSDC:        2.0 * float64(callCount),
		},
		{
			description:    "lo success + veth fail → SDC = 10",
			loUAS:          "uas_100.xml",
			loUAC:          "uac_100.xml",
			vethUAS:        "uas_0.xml",
			vethUAC:        "uac_0.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    float64(callCount),
			wantVethInvite: float64(callCount),
			wantVeth200OK:  0,
			wantSDC:        float64(callCount),
		},
		{
			description:    "lo fail + veth fail → SDC = 0",
			loUAS:          "uas_0.xml",
			loUAC:          "uac_0.xml",
			vethUAS:        "uas_0.xml",
			vethUAC:        "uac_0.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    0,
			wantVethInvite: float64(callCount),
			wantVeth200OK:  0,
			wantSDC:        0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()
			env := newTestEnvWithExtraEnv(ctx, t, "", extraEnv)

			runSippScenario(ctx, t, tt.loUAS, tt.loUAC, callCount, env)
			runSippScenarioWithIPs(ctx, t, tt.vethUAS, tt.vethUAC, callCount, env, veth0aIP, veth0bIP)

			loLabel := `called_host="127.0.0.1"`
			gotLoInvite := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", loLabel)
			gotLo200OK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", loLabel)

			vethLabel := `called_host="10.10.0.1"`
			gotVethInvite := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", vethLabel)
			gotVeth200OK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", vethLabel)

			require.InDelta(t, tt.wantLoInvite, gotLoInvite, ratioDelta, "lo INVITE: %s", tt.description)
			require.InDelta(t, tt.wantLo200OK, gotLo200OK, ratioDelta, "lo 200 OK: %s", tt.description)
			require.InDelta(t, tt.wantVethInvite, gotVethInvite, ratioDelta, "veth INVITE: %s", tt.description)
			require.InDelta(t, tt.wantVeth200OK, gotVeth200OK, ratioDelta, "veth 200 OK: %s", tt.description)

			// SDC only increments when sessions complete (BYE→200 OK processed).
			// Wait for all sessions to finish before checking SDC.
			waitForSessionsZero(t, env.endpoint)

			// When wantSDC == 0, the sdc_total CounterVec may be absent (no
			// SessionCompleted calls → lazy-initialized metric never created).
			// getSDC returns 0.0 for absent metrics, matching wantSDC.
			if tt.wantSDC > 0 {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_sdc_total"), "SDC metric must exist")
			}
			gotSDC := getSDC(t, env.endpoint)

			t.Logf("lo: invite %.0f/%.0f, 200 OK %.0f/%.0f | veth: invite %.0f/%.0f, 200 OK %.0f/%.0f | SDC %.0f/%.0f",
				gotLoInvite, tt.wantLoInvite, gotLo200OK, tt.wantLo200OK,
				gotVethInvite, tt.wantVethInvite, gotVeth200OK, tt.wantVeth200OK,
				gotSDC, tt.wantSDC)

			require.InDelta(t, tt.wantSDC, gotSDC, ratioDelta, "SDC: %s", tt.description)
		})
	}
}

// TestMultiInterface_PDD verifies PDD (Post Dial Delay) measurement with traffic
// on BOTH lo and veth pair simultaneously. Each subtest starts a fresh exporter
// and runs flows on both interfaces, then verifies per-host metrics to prove
// multi-NIC capture.
//
// PDD is measured when 180 Ringing is received for an INVITE. The INVITE arrives
// on veth0a RX (UAC→UAS), the 180 Ringing arrives on veth0b RX (UAS→UAC). The
// dialog tracker must correlate both halves by Call-ID to measure the delay.
// uas_100.xml sends 180 Ringing; uas_0.xml (486 Busy) does not.
//
// The critical multi-NIC assertion in scenario 1: pdd_count >= 2*callCount proves
// 180 Ringing was captured on BOTH lo and veth0b, and the dialog tracker measured
// PDD from cross-NIC INVITE↔180 pairs.
func TestMultiInterface_PDD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupVethPair(t)

	extraEnv := map[string]string{
		"SIP_EXPORTER_INTERFACE":   "lo," + veth0aName + "," + veth0bName,
		"SIP_EXPORTER_HOST_LABELS": "true",
	}
	callCount := 10

	tests := []struct {
		description    string
		loUAS          string
		loUAC          string
		vethUAS        string
		vethUAC        string
		wantLoInvite   float64
		wantLo200OK    float64
		wantVethInvite float64
		wantVeth200OK  float64
		wantPDDCount   float64
	}{
		{
			description:    "lo 180 + veth 180 → PDD from both NICs",
			loUAS:          "uas_100.xml",
			loUAC:          "uac_100.xml",
			vethUAS:        "uas_100.xml",
			vethUAC:        "uac_100.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    float64(callCount),
			wantVethInvite: float64(callCount),
			wantVeth200OK:  float64(callCount),
			wantPDDCount:   2.0 * float64(callCount),
		},
		{
			description:    "lo 180 + veth no 180 → PDD from lo only",
			loUAS:          "uas_100.xml",
			loUAC:          "uac_100.xml",
			vethUAS:        "uas_0.xml",
			vethUAC:        "uac_0.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    float64(callCount),
			wantVethInvite: float64(callCount),
			wantVeth200OK:  0,
			wantPDDCount:   float64(callCount),
		},
		{
			description:    "lo no 180 + veth no 180 → no PDD measured",
			loUAS:          "uas_0.xml",
			loUAC:          "uac_0.xml",
			vethUAS:        "uas_0.xml",
			vethUAC:        "uac_0.xml",
			wantLoInvite:   float64(callCount),
			wantLo200OK:    0,
			wantVethInvite: float64(callCount),
			wantVeth200OK:  0,
			wantPDDCount:   0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()
			env := newTestEnvWithExtraEnv(ctx, t, "", extraEnv)

			runSippScenario(ctx, t, tt.loUAS, tt.loUAC, callCount, env)
			runSippScenarioWithIPs(ctx, t, tt.vethUAS, tt.vethUAC, callCount, env, veth0aIP, veth0bIP)

			loLabel := `called_host="127.0.0.1"`
			gotLoInvite := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", loLabel)
			gotLo200OK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", loLabel)

			vethLabel := `called_host="10.10.0.1"`
			gotVethInvite := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", vethLabel)
			gotVeth200OK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", vethLabel)

			require.InDelta(t, tt.wantLoInvite, gotLoInvite, ratioDelta, "lo INVITE: %s", tt.description)
			require.InDelta(t, tt.wantLo200OK, gotLo200OK, ratioDelta, "lo 200 OK: %s", tt.description)
			require.InDelta(t, tt.wantVethInvite, gotVethInvite, ratioDelta, "veth INVITE: %s", tt.description)
			require.InDelta(t, tt.wantVeth200OK, gotVeth200OK, ratioDelta, "veth 200 OK: %s", tt.description)

			// PDD histogram: no output lines if zero observations were made.
			// For wantPDDCount > 0 the metric must exist; for 0 it may be absent.
			if tt.wantPDDCount > 0 {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_pdd_count"),
					"PDD count metric must exist: %s", tt.description)
				pddCount := getMetric(t, env.endpoint, "sip_exporter_pdd_count")
				pdd := getPDD(t, env.endpoint)

				t.Logf(
					"lo: invite %.0f/%.0f, 200 OK %.0f/%.0f | veth: invite %.0f/%.0f, 200 OK %.0f/%.0f | PDD count %.0f (want >= %.0f) | PDD %.2f ms",
					gotLoInvite,
					tt.wantLoInvite,
					gotLo200OK,
					tt.wantLo200OK,
					gotVethInvite,
					tt.wantVethInvite,
					gotVeth200OK,
					tt.wantVeth200OK,
					pddCount,
					tt.wantPDDCount,
					pdd,
				)

				require.GreaterOrEqual(t, pddCount, tt.wantPDDCount,
					"PDD observations: %s", tt.description)
				require.Greater(t, pdd, 0.0, "PDD should be > 0: %s", tt.description)
			} else {
				t.Logf("lo: invite %.0f/%.0f, 200 OK %.0f/%.0f | veth: invite %.0f/%.0f, 200 OK %.0f/%.0f | no PDD expected",
					gotLoInvite, tt.wantLoInvite, gotLo200OK, tt.wantLo200OK,
					gotVethInvite, tt.wantVethInvite, gotVeth200OK, tt.wantVeth200OK)

				// No 180 Ringing → histogram may have zero observations.
				if metricExists(t, env.endpoint, "sip_exporter_pdd_count") {
					pddCount := getMetric(t, env.endpoint, "sip_exporter_pdd_count")
					require.InDelta(t, tt.wantPDDCount, pddCount, ratioDelta,
						"PDD count should be 0 when no 180: %s", tt.description)
				}
			}

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// freePort returns a single free TCP port number (for SIPp -p allocation).
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// TestMultiPortPerInterface verifies that SIP_EXPORTER_SIP_PORTS configures
// multiple SIP ports on a single interface and the exporter captures REGISTER
// traffic on ALL of them — proving the 3-entry eBPF map + userspace
// isSIPPacket port scan work end-to-end.
//
// Single interface (lo) with 3 distinct SIP ports. lo + IGNORE_OUTGOING=true →
// each packet seen once (AGENTS.md Critical Rule for loopback).
//
// Per-interface port sets (different ports per NIC) are covered by the
// ParsedSIPPorts MC/DC unit tests and the multi-collection Initialize path
// exercised by the existing multi-interface e2e suite.
func TestMultiPortPerInterface(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sipPorts := []string{freePort(t), freePort(t), freePort(t)}

	extraEnv := map[string]string{
		"SIP_EXPORTER_SIP_PORTS": strings.Join(sipPorts, ","),
	}
	env := newTestEnvWithExtraEnv(ctx, t, "", extraEnv)

	const callCount = 5

	for i := range sipPorts {
		flowEnv := &testEnv{
			endpoint:       env.endpoint,
			sippPort:       sipPorts[i],
			sippClientPort: freePort(t),
		}
		runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", callCount, flowEnv)
	}

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_total"),
		"register_total must exist after SIP traffic")
	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")
	t.Logf("register_total=%.0f (want >= %d)", registerTotal, 3*callCount)
	require.GreaterOrEqual(t, registerTotal, 3.0*callCount,
		"REGISTER captured on all 3 SIP ports")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestMultiPort_PerInterfaceDifferentPorts verifies that each interface gets
// its OWN port set — different SIP ports on different NICs — and traffic on
// each interface's configured ports is captured.
//
// Interfaces:
//
//	lo     → [loPort1, loPort2]
//	sipns0 → [vethPort]   (host end of veth bridging to an isolated netns)
//
// SIP_EXPORTER_SIP_PORTS = "loPort1,loPort2;vethPort"
//
// The veth flow uses a pause container (isolated netns) so traffic physically
// traverses sipns0 — kernel local delivery via lo does NOT apply (unlike
// setupVethPair where both ends share the host netns). See setupVethNetns.
//
// The veth flow on vethPort is captured ONLY because sipns0's BPF collection
// has vethPort in its sip_ports map. If a bug applied lo's port set
// [loPort1,loPort2] to all collections, vethPort traffic would be dropped by
// the eBPF filter → register_total = 2*callCount, failing the assertion.
func TestMultiPort_PerInterfaceDifferentPorts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pauseID := setupVethNetns(t)

	loPort1 := freePort(t)
	loPort2 := freePort(t)
	vethPort := freePort(t)

	extraEnv := map[string]string{
		"SIP_EXPORTER_INTERFACE": testInterface + "," + nsVethHost,
		"SIP_EXPORTER_SIP_PORTS": loPort1 + "," + loPort2 + ";" + vethPort,
	}
	env := newTestEnvWithExtraEnv(ctx, t, "", extraEnv)

	const callCount = 5

	// Flows 1+2: loopback on loPort1 and loPort2 (lo's port set).
	for _, port := range []string{loPort1, loPort2} {
		flowEnv := &testEnv{
			endpoint:       env.endpoint,
			sippPort:       port,
			sippClientPort: freePort(t),
		}
		runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", callCount, flowEnv)
	}

	// Flow 3: veth (sipns0) on vethPort. UAS on host (nsHostIP:vethPort),
	// UAC in isolated netns (nsGuestIP) → traffic physically traverses sipns0.
	vethEnv := &testEnv{
		endpoint:       env.endpoint,
		sippPort:       vethPort,
		sippClientPort: freePort(t),
	}
	uasCtx, uasCancel := context.WithTimeout(ctx, 60*time.Second)
	defer uasCancel()
	uasPath := absScenarioPath(t, "reg_uas.xml")
	sippVol := filepath.Dir(uasPath)
	uasCmd := exec.CommandContext(uasCtx, "docker", "run", "--rm",
		"--network", "host",
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/reg_uas.xml",
		"-i", nsHostIP,
		"-p", vethPort,
		"-m", strconv.Itoa(callCount),
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
		return isUDPPortInUse(vethPort)
	}, 10*time.Second, 50*time.Millisecond, "UAS should start listening on %s:%s", nsHostIP, vethPort)

	runSippUACInNetns(ctx, t, pauseID, "reg_uac.xml", callCount, vethEnv, nsHostIP)
	_ = uasCmd.Wait()
	waitForMetricStable(t, env.endpoint)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_total"),
		"register_total must exist after SIP traffic")
	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")
	t.Logf("register_total=%.0f (want >= %d: lo 2×%d + veth 1×%d)",
		registerTotal, 3*callCount, callCount, callCount)
	require.GreaterOrEqual(t, registerTotal, 3.0*callCount,
		"REGISTER captured on lo ports AND veth port (per-interface port sets)")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestMultiPort_PerInterfacePortIsolation verifies that port sets are truly
// per-interface: traffic on loPort sent via sipns0 (where loPort is NOT in
// sipns0's port set) is silently dropped by the eBPF filter.
//
// Interfaces:
//
//	lo     → [loPort]
//	sipns0 → [vethPort]
//
// Phase 1 (positive): REGISTER on loPort via lo → captured.
// Phase 2 (negative): REGISTER on loPort via sipns0 → eBPF drops it.
//
// The SIPp exchange in phase 2 succeeds because the eBPF socket filter only
// affects the exporter's AF_PACKET copies, not normal kernel packet delivery.
func TestMultiPort_PerInterfacePortIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pauseID := setupVethNetns(t)

	loPort := freePort(t)
	vethPort := freePort(t)

	extraEnv := map[string]string{
		"SIP_EXPORTER_INTERFACE": testInterface + "," + nsVethHost,
		"SIP_EXPORTER_SIP_PORTS": loPort + ";" + vethPort,
	}
	env := newTestEnvWithExtraEnv(ctx, t, "", extraEnv)

	const callCount = 5

	// Phase 1: REGISTER on loPort via lo (positive control).
	loEnv := &testEnv{
		endpoint:       env.endpoint,
		sippPort:       loPort,
		sippClientPort: freePort(t),
	}
	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", callCount, loEnv)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_total"),
		"register_total must exist after phase 1")
	registerBefore := getMetric(t, env.endpoint, "sip_exporter_register_total")
	t.Logf("phase 1 (lo, port=%s): register_total=%.0f (want >= %d)",
		loPort, registerBefore, callCount)
	require.GreaterOrEqual(t, registerBefore, float64(callCount),
		"phase 1: REGISTER captured on lo:%s", loPort)

	// Phase 2: REGISTER on loPort via sipns0 (negative — loPort ∉ sipns0's ports).
	// UAS on host (nsHostIP:loPort), UAC in netns (nsGuestIP → nsHostIP:loPort).
	uasCtx, uasCancel := context.WithTimeout(ctx, 60*time.Second)
	defer uasCancel()
	uasPath := absScenarioPath(t, "reg_uas.xml")
	sippVol := filepath.Dir(uasPath)
	uasCmd := exec.CommandContext(uasCtx, "docker", "run", "--rm",
		"--network", "host",
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/reg_uas.xml",
		"-i", nsHostIP,
		"-p", loPort,
		"-m", strconv.Itoa(callCount),
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
		return isUDPPortInUse(loPort)
	}, 10*time.Second, 50*time.Millisecond, "UAS should start listening on %s:%s", nsHostIP, loPort)

	vethEnv := &testEnv{
		endpoint:       env.endpoint,
		sippPort:       loPort,
		sippClientPort: freePort(t),
	}
	runSippUACInNetns(ctx, t, pauseID, "reg_uac.xml", callCount, vethEnv, nsHostIP)
	_ = uasCmd.Wait()
	waitForMetricStable(t, env.endpoint)

	registerAfter := getMetric(t, env.endpoint, "sip_exporter_register_total")
	t.Logf("phase 2 (sipns0, port=%s): register_total=%.0f (want == %.0f, unchanged)",
		loPort, registerAfter, registerBefore)
	require.InDelta(t, registerBefore, registerAfter, 0.0,
		"phase 2: REGISTER on loPort via sipns0 must NOT be captured (loPort ∉ sipns0's ports)")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestMultiPort_UnconfiguredPortDropped verifies that SIP traffic on a port NOT
// listed in SIP_EXPORTER_SIP_PORTS is silently dropped by the eBPF filter.
//
// Single interface (lo) with SIP_EXPORTER_SIP_PORTS=port1,port2.
//
// Phase 1 (positive): REGISTER on port1 → captured.
// Phase 2 (negative): REGISTER on port3 (unconfigured) → eBPF drops it.
func TestMultiPort_UnconfiguredPortDropped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	port1 := freePort(t)
	port2 := freePort(t)
	port3 := freePort(t)

	extraEnv := map[string]string{
		"SIP_EXPORTER_SIP_PORTS": port1 + "," + port2,
	}
	env := newTestEnvWithExtraEnv(ctx, t, "", extraEnv)

	const callCount = 5

	// Phase 1: REGISTER on port1 (configured).
	flowEnv := &testEnv{
		endpoint:       env.endpoint,
		sippPort:       port1,
		sippClientPort: freePort(t),
	}
	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", callCount, flowEnv)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_total"),
		"register_total must exist after phase 1")
	registerBefore := getMetric(t, env.endpoint, "sip_exporter_register_total")
	t.Logf("phase 1 (port=%s): register_total=%.0f (want >= %d)",
		port1, registerBefore, callCount)
	require.GreaterOrEqual(t, registerBefore, float64(callCount),
		"phase 1: REGISTER captured on configured port %s", port1)

	// Phase 2: REGISTER on port3 (NOT configured) — eBPF filter should drop it.
	dropEnv := &testEnv{
		endpoint:       env.endpoint,
		sippPort:       port3,
		sippClientPort: freePort(t),
	}
	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", callCount, dropEnv)

	registerAfter := getMetric(t, env.endpoint, "sip_exporter_register_total")
	t.Logf("phase 2 (port=%s, unconfigured): register_total=%.0f (want == %.0f, unchanged)",
		port3, registerAfter, registerBefore)
	require.InDelta(t, registerBefore, registerAfter, 0.0,
		"phase 2: REGISTER on unconfigured port %s must NOT be captured", port3)

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestMultiPort_INVITE_Flow verifies that INVITE→200 OK→BYE dialogs are captured
// across multiple configured SIP ports on a single interface.
//
// Single interface (lo) with 3 SIP ports. Each port runs a full INVITE flow
// with callCount calls. The eBPF filter matches both dst_port (INVITE/BYE/ACK)
// and src_port (200 OK responses) against the configured SIP ports.
func TestMultiPort_INVITE_Flow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sipPorts := []string{freePort(t), freePort(t), freePort(t)}

	extraEnv := map[string]string{
		"SIP_EXPORTER_SIP_PORTS": strings.Join(sipPorts, ","),
	}
	env := newTestEnvWithExtraEnv(ctx, t, "", extraEnv)

	const callCount = 5

	for i := range sipPorts {
		flowEnv := &testEnv{
			endpoint:       env.endpoint,
			sippPort:       sipPorts[i],
			sippClientPort: freePort(t),
		}
		runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", callCount, flowEnv)
	}

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_invite_total"),
		"invite_total must exist after INVITE traffic")
	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	invite200 := getMetric(t, env.endpoint, "sip_exporter_invite_200_total")
	t.Logf("invite_total=%.0f, invite_200_total=%.0f (want >= %d each)",
		inviteTotal, invite200, 3*callCount)
	require.GreaterOrEqual(t, inviteTotal, 3.0*callCount,
		"INVITE captured on all 3 SIP ports")
	require.GreaterOrEqual(t, invite200, 3.0*callCount,
		"200 OK for INVITE captured on all 3 SIP ports")

	waitForSessionsZero(t, env.endpoint)
}
