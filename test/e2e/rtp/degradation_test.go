//go:build e2e

package rtp

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// tcImage is the Alpine image used for running tc commands inside a privileged
// container with CAP_NET_ADMIN on the host network.
const tcImage = "alpine:3.22.4"

// startTCContainer launches a privileged host-network Alpine container with
// iproute2 installed for running tc commands that modify the host's lo qdisc.
// Returns the container ID. Removed in t.Cleanup.
func startTCContainer(t *testing.T) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "run", "-d", "--rm",
		"--privileged", "--network", "host",
		"--entrypoint", "sh", tcImage,
		"-c", "apk add --no-cache iproute2 > /dev/null 2>&1 && sleep 300",
	).Output()
	require.NoError(t, err, "failed to start tc container")
	id := strings.TrimSpace(string(out))

	require.Eventually(t, func() bool {
		return exec.Command("docker", "exec", id, "tc", "qdisc", "show", "dev", "lo").Run() == nil
	}, 30*time.Second, 500*time.Millisecond, "tc container not ready")

	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = exec.CommandContext(stopCtx, "docker", "rm", "-f", id).Run()
	})

	return id
}

// runTC executes a tc command inside the privileged container, failing the
// test on error.
func runTC(t *testing.T, id string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	execArgs := append([]string{"exec", id, "tc"}, args...)
	out, err := exec.CommandContext(ctx, "docker", execArgs...).CombinedOutput()
	require.NoErrorf(t, err, "tc %v failed: %s", args, string(out))
}

// applyNetem sets up a prio qdisc on lo with a netem child on band 3, routing
// only the given UDP ports through the degraded band. SIP signalling and
// testcontainers traffic (different ports) pass through normal bands untouched.
//
// Qdisc layout:
//
//	lo root: prio (handle 1:) — replaces default noqueue
//	  class 1:3 (band 2): netem child (handle 30:) with the supplied args
//	  tc filter u32: ip protocol 17 + dport <port> → flowid 1:3
//
// t.Cleanup restores lo's default qdisc (noqueue).
func applyNetem(t *testing.T, netemArgs []string, ports ...string) {
	t.Helper()
	id := startTCContainer(t)

	// Pre-clean any leftover qdisc from a failed prior run (ignore errors).
	preCtx, preCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer preCancel()
	_ = exec.CommandContext(preCtx, "docker", "exec", id,
		"tc", "qdisc", "del", "dev", "lo", "root").Run()

	runTC(t, id, "qdisc", "add", "dev", "lo", "root", "handle", "1:", "prio")

	// Register cleanup immediately after qdisc add so the qdisc is removed
	// even if subsequent netem/filter setup fails. LIFO: runs BEFORE the
	// container rm from startTCContainer.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "docker", "exec", id,
			"tc", "qdisc", "del", "dev", "lo", "root").Run()
	})

	netemCmd := append(
		[]string{"qdisc", "add", "dev", "lo", "parent", "1:3", "handle", "30:", "netem"},
		netemArgs...,
	)
	runTC(t, id, netemCmd...)

	for _, port := range ports {
		runTC(t, id, "filter", "add", "dev", "lo", "protocol", "ip",
			"parent", "1:0", "prio", "3", "u32",
			"match", "ip", "protocol", "17", "0xff",
			"match", "ip", "dport", port, "0xffff",
			"flowid", "1:3")
	}
}

// avgHistogramValue scrapes a Prometheus histogram's _sum and _count for the
// PCMA codec label and returns sum/count, or 0 when count is zero.
func avgHistogramValue(t *testing.T, endpoint, name string) float64 {
	t.Helper()
	sum := getRTPMetric(t, endpoint, name+"_sum")
	cnt := getRTPMetric(t, endpoint, name+"_count")
	if cnt == 0 {
		return 0
	}
	return sum / cnt
}

// TestRTP_NetemDegradation verifies that RTP traffic degraded by tc netem
// (jitter + loss) produces elevated jitter metrics, detected packet loss and
// degraded MOS on /metrics. Netem is applied only to the RTP media ports via
// u32 port filters — SIP signalling and testcontainers traffic on lo are
// unaffected.
//
// Parameters: delay 30ms ±10ms (range [20,40], max ΔD=20ms = inter-packet
// interval → P(reorder)=0) + loss 50%.
//
// Expected:
//   - jitter ≈ 7ms (E[|ΔD|]=2·10/3≈6.7ms; clean G.711 ≈ 0ms)
//   - loss ≈ 50% (sequence gaps detected)
//   - MOS ≈ 2.7 (effLoss=0.50, well below the 3.0 threshold; clean G.711 ≈ 4.4)
//
// The delay variation is kept ≤20ms (G.711 inter-packet interval) to avoid
// packet reordering, which would conflate jitter and loss measurements in
// this combined-degradation test.
func TestRTP_NetemDegradation(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]

	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, "0", testInterface, true, "")

	applyNetem(t,
		[]string{"delay", "30ms", "10ms", "loss", "50%"},
		uasMedia, uacMedia,
	)

	runSippRTP(context.Background(), t, uasSIP, uacSIP, uasMedia, uacMedia)

	// RTP packets must be observed (pipeline functional despite degradation).
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
	}, 10*time.Second, 500*time.Millisecond,
		"rtp_packets_total must be observed under netem")

	// Jitter must be elevated (clean G.711 ≈ 0ms; degraded > 3ms).
	require.Eventually(t, func() bool {
		return avgHistogramValue(t, endpoint, "sip_exporter_rtp_jitter_milliseconds") > 3
	}, 15*time.Second, 500*time.Millisecond,
		"avg jitter must be >3ms under netem degradation")

	// MOS must be degraded (clean G.711 ≈ 4.4; degraded < 3.0).
	var avgMOS float64
	require.Eventually(t, func() bool {
		avgMOS = avgHistogramValue(t, endpoint, "sip_exporter_rtp_mos_score")
		return avgMOS > 0 && avgMOS < 3
	}, 15*time.Second, 500*time.Millisecond,
		"avg MOS must be <3.0 under netem degradation")

	// Loss must be detected.
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_lost_total") > 0
	}, 10*time.Second, 500*time.Millisecond,
		"rtp_packets_lost_total must be >0 under netem loss")

	t.Logf("netem degradation: avg MOS=%.2f (target <3.0, clean≈4.4)", avgMOS)
}

// TestRTP_NetemPacketLoss verifies that sequence-gap loss detection produces a
// loss rate approximately matching the injected netem rate. Unlike
// TestRTP_NetemDegradation (combined jitter+loss, asserts only lost>0), this
// test applies loss-only netem and asserts the detected rate is quantitatively
// correct.
//
// Parameters: loss 30% (no delay variation — isolates loss detection, no
// reordering risk).
//
// Expected: ~200 RTP packets per leg over 4s (G.711a, 20ms pacing). Netem
// drops 30% → exporter receives ~140/leg, detects ~60 seq gaps/leg → lossRate
// = lost/(lost+received) ≈ 30%. Margin [20%, 40%] covers netem randomness and
// small-sample variance (at 400 packets, σ≈9.2).
func TestRTP_NetemPacketLoss(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]

	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, "0", testInterface, true, "")

	applyNetem(t,
		[]string{"loss", "30%"},
		uasMedia, uacMedia,
	)

	runSippRTP(context.Background(), t, uasSIP, uacSIP, uasMedia, uacMedia)

	// RTP packets must be observed.
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
	}, 10*time.Second, 500*time.Millisecond,
		"rtp_packets_total must be observed under netem loss")

	// Loss must be detected.
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_lost_total") > 0
	}, 10*time.Second, 500*time.Millisecond,
		"rtp_packets_lost_total must be >0 under netem loss")

	// Loss rate must be approximately 30% (margin [20%, 40%]).
	require.Eventually(t, func() bool {
		lost := getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_lost_total")
		total := getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total")
		if total == 0 {
			return false
		}
		rate := lost / (lost + total)
		t.Logf("netem loss: rate=%.1f%% (lost=%.0f total=%.0f)", rate*100, lost, total)
		return rate >= 0.20 && rate <= 0.40
	}, 15*time.Second, 500*time.Millisecond,
		"packet loss rate must be approximately 30%% (20%%-40%%)")
}
