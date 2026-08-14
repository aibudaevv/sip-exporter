//go:build e2e

package rtp

import (
	"context"
	"os/exec"
	"strconv"
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

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
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
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
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
	preCtx, preCancel := context.WithTimeout(t.Context(), 5*time.Second)
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

// TestRTPNetemDegradation verifies that RTP traffic degraded by tc netem
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
func TestRTPNetemDegradation(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]

	endpoint := startExporter(t.Context(), t, httpPort, uasSIP, testInterface, "")

	applyNetem(t,
		[]string{"delay", "30ms", "10ms", "loss", "50%"},
		uasMedia, uacMedia,
	)

	runSippRTP(t.Context(), t, uasSIP, uacSIP, uasMedia, uacMedia)

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

// TestRTPSequenceGapLoss verifies that a known RTP sequence gap is exported as
// an exact packet-loss counter value through the full capture pipeline.
func TestRTPSequenceGapLoss(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uasMediaNum, err := strconv.Atoi(uasMedia)
	require.NoError(t, err)

	endpoint := startExporterWithCarrierUA(t.Context(), t, httpPort, uasSIP,
		integrationCarriersYAML, integrationUserAgentsYAML, "")

	wait := startControlledSIPDialog(t, uasSIP, uacSIP, uasMedia, uacMedia)
	require.Eventually(t, func() bool {
		return metricWithLabelsExists(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) &&
			getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) >= 1
	}, 10*time.Second, 200*time.Millisecond, "dialog must be established")

	time.Sleep(1500 * time.Millisecond)
	sendControlledRTP(t, uasMediaNum, []uint16{1, 2, 3, 4, 5, 6, 7, 8, 9, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})

	require.Eventually(t, func() bool {
		return metricExists(t, endpoint, "sip_exporter_rtp_packets_lost_total") &&
			getMetricByLabel(t, endpoint, "sip_exporter_rtp_packets_lost_total", labelCarrier, labelUAType, labelCodec) == 1
	}, 10*time.Second, 200*time.Millisecond, "one RTP sequence gap must increment lost_total once")

	wait()
}
