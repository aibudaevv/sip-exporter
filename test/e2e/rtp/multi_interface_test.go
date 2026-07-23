//go:build e2e

package rtp

import (
	"context"
	"os"
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
// busybox ip does not support `link add type veth peer name`). Reference-counted:
// the pair persists until the last parallel test finishes.
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
		_ = exec.Command("docker", "run", "--rm", "--privileged", "--network", "host",
			"--entrypoint", "", "alpine",
			"sh", "-c", "ip link del "+veth0aName+" 2>/dev/null || true",
		).Run()
	})

	if !needCreate {
		require.Eventually(t, func() bool {
			_, err := os.Stat("/sys/class/net/" + veth0aName)
			return err == nil
		}, 15*time.Second, 200*time.Millisecond, "veth pair not created in time")
		return
	}

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

// TestRTP_MultiInterface verifies that RTP capture and correlation work when
// the SIP dialog uses non-loopback IPs. A single SIP+RTP flow is established
// between UAS (10.10.0.1) and UAC (10.10.0.2), both bound to veth endpoints.
// The exporter listens on lo+veth0a+veth0b.
//
// Note: when both veth endpoints reside in the same network namespace (as is the
// case here — both are on the host), the kernel delivers traffic between them
// via the local routing table (lo), not via the veth pair. This means the
// exporter captures the packets on lo RX, not on veth RX. The test therefore
// proves SDP correlation and RTP tracking with non-loopback media endpoints
// (more realistic than 127.0.0.1), but does NOT prove cross-NIC capture per se.
// True cross-NIC testing would require separate network namespaces.
func TestRTP_MultiInterface(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupVethPair(t)

	ports := allocatePortsN(6)
	httpPort, uasSIP, sipsPort, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4], ports[5]

	iface := "lo," + veth0aName + "," + veth0bName
	endpoint := startExporter(ctx, t, httpPort, uasSIP, sipsPort, iface, true, "")

	// SIP+RTP flow with non-loopback IPs: UAS on 10.10.0.1, UAC on 10.10.0.2.
	runSippRTPWithIPs(ctx, t, uasSIP, uacSIP, uasMedia, uacMedia, veth0aIP, veth0bIP)

	// RTP packets counter must be > 0: SDP correlation registered the media
	// endpoints (10.10.0.x) and RTP was observed.
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
	}, 10*time.Second, 500*time.Millisecond,
		"rtp_packets_total must be observed from veth IP flow")

	// Jitter and MOS histograms must have samples (1s snapshot cycle).
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_jitter_milliseconds_count") > 0
	}, 10*time.Second, 500*time.Millisecond, "jitter histogram must have samples")
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_mos_score_count") > 0
	}, 10*time.Second, 500*time.Millisecond, "MOS histogram must have samples")

	// MOS in sane range for clean G.711 (E-model ~3.9-4.4).
	mosSum := getRTPMetric(t, endpoint, "sip_exporter_rtp_mos_score_sum")
	mosCount := getRTPMetric(t, endpoint, "sip_exporter_rtp_mos_score_count")
	require.Greater(t, mosCount, 0.0)
	avgMOS := mosSum / mosCount
	t.Logf("Multi-NIC RTP: avg MOS=%.2f (PCMA, veth IPs)", avgMOS)
	require.Greater(t, avgMOS, 3.5, "clean G.711 MOS should be > 3.5")
	require.Less(t, avgMOS, 4.6)
}
