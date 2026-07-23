//go:build e2e

package load

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// vethPair describes one pair of the vethXa (exporter side) / vethXb (UAC side)
// link used to generate traffic on a non-loopback interface.
type vethPair struct {
	index int
	aName string // vethXa — exporter listens here
	bName string // vethXb — UAC sends from here
	aIP   string // vethXa IP, e.g. 10.0.0.1
	bIP   string // vethXb IP, e.g. 10.0.0.2
}

// uacTarget describes one parallel UAC's bind/target addresses for runMultiNICLoad.
type uacTarget struct {
	uacIP   string // -i bind IP
	uacPort string // -p bind source port
	uasIP   string // remote target IP
}

// multiNICEnv is the load-test environment for N interfaces: one exporter
// container + a list of UAC targets (one per interface).
type multiNICEnv struct {
	endpoint          string
	sipPort           string
	exporterContainer testcontainers.Container
	uacTargets        []uacTarget
}

// setupVethPairs creates n-1 veth pairs for multi-interface load testing
// (lo is always present as the first interface, so n=1 creates zero pairs).
// Pairs are named veth0a/veth0b, veth1a/veth1b, … with IPs 10.0.X.1/30 and
// 10.0.X.2/30. Uses a privileged Alpine container with iproute2 (busybox ip
// lacks `link add type veth peer name`). Precedent: test/e2e/rtp/degradation_test.go.
//
// Returns the slice of created pairs (length n-1).
func setupVethPairs(t *testing.T, n int) []vethPair {
	t.Helper()
	if n < 1 {
		require.Failf(t, "invalid n", "setupVethPairs requires n >= 1, got %d", n)
		return nil
	}

	pairs := make([]vethPair, 0, n-1)
	for i := range n - 1 {
		p := vethPair{
			index: i,
			aName: fmt.Sprintf("veth%da", i),
			bName: fmt.Sprintf("veth%db", i),
			aIP:   fmt.Sprintf("10.0.%d.1", i),
			bIP:   fmt.Sprintf("10.0.%d.2", i),
		}

		// `set -e` ensures apk/link-set failures abort. `|| true` guards only
		// the idempotent add commands so re-runs work when a previous test
		// left the pair half-created.
		script := strings.Join([]string{
			"set -e",
			"apk add --no-cache iproute2 > /dev/null",
			fmt.Sprintf("ip link add %s type veth peer name %s || true", p.aName, p.bName),
			fmt.Sprintf("ip addr add %s/30 dev %s || true", p.aIP, p.aName),
			fmt.Sprintf("ip addr add %s/30 dev %s || true", p.bIP, p.bName),
			fmt.Sprintf("ip link set %s up", p.aName),
			fmt.Sprintf("ip link set %s up", p.bName),
		}, "\n")

		out, err := exec.Command("docker", "run", "--rm", "--privileged", "--network", "host",
			"--entrypoint", "", "alpine",
			"sh", "-c", script,
		).CombinedOutput()
		require.NoError(t, err, "failed to create veth pair %d: %s", i, string(out))

		t.Cleanup(func() {
			// busybox `ip link del` is sufficient — kernel removes the peer.
			_ = exec.Command("docker", "run", "--rm", "--privileged", "--network", "host",
				"--entrypoint", "", "alpine",
				"sh", "-c", "ip link del "+p.aName+" 2>/dev/null || true",
			).Run()
		})

		pairs = append(pairs, p)
	}
	return pairs
}

// newMultiNICEnv starts an exporter listening on the given interfaces plus the
// SIP port, returning a multiNICEnv with N UAC targets (one per interface).
// ifaces[0] is always "lo"; for veth entries the UAC binds to the veth*bside.
func newMultiNICEnv(ctx context.Context, t *testing.T, ifaces []string, pairs []vethPair) *multiNICEnv {
	t.Helper()
	require.NotEmpty(t, ifaces, "need at least one interface")
	require.Len(t, pairs, len(ifaces)-1,
		"pairs count must equal len(ifaces)-1 (lo is the first interface)")

	portMu.Lock()
	httpPort := strconv.Itoa(nextBasePort)
	sipPort := strconv.Itoa(nextBasePort + 1)
	sipsPort := strconv.Itoa(nextBasePort + 2)
	uacPorts := make([]string, len(ifaces))
	for i := range ifaces {
		uacPorts[i] = strconv.Itoa(nextBasePort + 3 + i)
	}
	nextBasePort += 3 + len(ifaces)
	portMu.Unlock()

	exporterLogLevel := "error"
	if testing.Verbose() {
		exporterLogLevel = "info"
	}

	envVars := map[string]string{
		"SIP_EXPORTER_INTERFACE":       strings.Join(ifaces, ","),
		"SIP_EXPORTER_HTTP_PORT":       httpPort,
		"SIP_EXPORTER_SIP_PORT":        sipPort,
		"SIP_EXPORTER_SIPS_PORT":       sipsPort,
		"SIP_EXPORTER_LOGGER_LEVEL":    exporterLogLevel,
		"SIP_EXPORTER_IGNORE_OUTGOING": "true",
	}

	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	req := testcontainers.ContainerRequest{
		Image:       exporterImage,
		Privileged:  true,
		NetworkMode: "host",
		Env:         envVars,
		WaitingFor: wait.ForHTTP("/metrics").
			WithPort(httpPort + "/tcp").
			WithStartupTimeout(120 * time.Second),
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           log.New(io.Discard, "", 0),
	})
	if err != nil && c != nil {
		logs, logErr := c.Logs(ctx)
		if logErr == nil {
			defer logs.Close()
			logBytes, _ := io.ReadAll(logs)
			t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
		}
	}
	require.NoError(t, err)

	t.Cleanup(func() {
		if testing.Verbose() {
			logs, logErr := c.Logs(context.Background())
			if logErr == nil {
				defer logs.Close()
				logBytes, _ := io.ReadAll(logs)
				t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
			}
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = c.Stop(stopCtx, nil)
		_ = c.Terminate(context.Background())
		for range 10 {
			conn, dialErr := net.DialTimeout("tcp", "localhost:"+httpPort, 500*time.Millisecond)
			if dialErr != nil {
				return
			}
			conn.Close()
			time.Sleep(500 * time.Millisecond)
		}
	})

	targets := make([]uacTarget, len(ifaces))
	// Interface 0 is always lo.
	targets[0] = uacTarget{uacIP: "127.0.0.1", uacPort: uacPorts[0], uasIP: "127.0.0.1"}
	for i, p := range pairs {
		targets[i+1] = uacTarget{uacIP: p.bIP, uacPort: uacPorts[i+1], uasIP: p.aIP}
	}

	return &multiNICEnv{
		endpoint:          fmt.Sprintf("http://localhost:%s", httpPort),
		sipPort:           sipPort,
		exporterContainer: c,
		uacTargets:        targets,
	}
}

// runMultiNICLoad runs N UAC instances in parallel (one per interface), each
// sending callCount INVITEs at the given rate. Returns aggregated loadResult.
// Uses one statsCollector for the single exporter container.
func runMultiNICLoad(
	ctx context.Context,
	t *testing.T,
	uacScenario string,
	callCount, rate int,
	env *multiNICEnv,
) loadResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(ctx, 600*time.Second)
	defer cancel()

	stats, statsErr := newStatsCollector(env.exporterContainer.GetContainerID())
	require.NoError(t, statsErr)

	statsCtx, statsCancel := context.WithCancel(ctx)
	stats.start(statsCtx, env.exporterContainer.GetContainerID())

	packetsBefore := getMetric(t, env.endpoint, "sip_exporter_packets_total")
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	start := time.Now()

	uacPath := absScenarioPath(t, uacScenario)
	sippVol := filepath.Dir(uacPath)
	uacFile := filepath.Base(uacScenario)

	uacs := make([]testcontainers.Container, len(env.uacTargets))
	for i, tgt := range env.uacTargets {
		uacs[i] = startSippContainer(ctx, t,
			[]string{
				"-sf", "/scenarios/" + uacFile,
				"-i", tgt.uacIP,
				"-p", tgt.uacPort,
				"-m", strconv.Itoa(callCount),
				"-r", strconv.Itoa(rate),
				"-nr",
				tgt.uasIP + ":" + env.sipPort,
			},
			sippVol, false,
		)
	}

	// Brief settle to let all UAC containers bind their sockets.
	time.Sleep(500 * time.Millisecond)

	for _, uac := range uacs {
		waitForContainerExit(ctx, t, uac)
	}

	sippEnd := time.Now()
	sippDuration := sippEnd.Sub(start)

	waitForMetricStable(t, env.endpoint)

	stableTime := time.Now()
	drainTime := stableTime.Sub(sippEnd)

	statsCancel()
	cpuAvg, cpuPeak, memMaxMB := stats.stop()

	packetsAfter := getMetric(t, env.endpoint, "sip_exporter_packets_total")
	errorsAfter := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	totalCaptured := packetsAfter - packetsBefore
	actualPPS := 0.0
	if sippDuration.Seconds() > 0 {
		actualPPS = totalCaptured / sippDuration.Seconds()
	}

	const packetsPerCall = 1.0 // flood_uac.xml sends 1 INVITE per call
	expectedTotal := float64(callCount * len(env.uacTargets) * int(packetsPerCall))
	expectedPPS := float64(rate * len(env.uacTargets) * int(packetsPerCall))
	lossRate := 0.0
	if expectedTotal > 0 {
		lossRate = 1 - totalCaptured/expectedTotal
		if lossRate < 0 {
			t.Logf("WARNING: captured %.0f > expected %.0f (%.2f%% extra)",
				totalCaptured, expectedTotal, -lossRate*100)
			lossRate = 0
		}
	}

	result := loadResult{
		Duration:      sippDuration,
		PacketsBefore: packetsBefore,
		PacketsAfter:  packetsAfter,
		ActualPPS:     actualPPS,
		ExpectedPPS:   expectedPPS,
		LossRate:      lossRate,
		ErrorCount:    errorsAfter - errorsBefore,
		DrainTime:     drainTime,
		CPUAvg:        cpuAvg,
		CPUPeak:       cpuPeak,
		MemMaxMB:      memMaxMB,
	}

	t.Logf("MultiNIC N=%d: actual=%.0f PPS (exp=%.0f), captured=%.0f, loss=%.2f%%, "+
		"drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB, errors=%.0f",
		len(env.uacTargets), result.ActualPPS, result.ExpectedPPS,
		totalCaptured, result.LossRate*100, result.DrainTime,
		result.CPUAvg, result.CPUPeak, result.MemMaxMB, result.ErrorCount)

	return result
}

// TestLoad_MultiInterface verifies linear scaling from N=1 to N=3 interfaces.
// Each subtest records results into load_result.json via recordResult, keyed
// by subtest name, for later baseline comparison.
func TestLoad_MultiInterface(t *testing.T) {
	const callCount = 1000
	const rate = 500

	for _, n := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("interfaces_%d", n), func(t *testing.T) {
			ctx := context.Background()

			pairs := setupVethPairs(t, n)

			ifaces := []string{"lo"}
			for _, p := range pairs {
				ifaces = append(ifaces, p.aName)
			}

			env := newMultiNICEnv(ctx, t, ifaces, pairs)
			result := runMultiNICLoad(ctx, t, "flood_uac.xml", callCount, rate, env)

			// SLO: at most 1% packet loss regardless of N.
			require.LessOrEqual(t, result.LossRate, 0.01,
				"packet loss SLO: < 1%% at N=%d (got %.2f%%)", n, result.LossRate*100)

			// SLO: at most 0.1% system errors relative to processed packets.
			totalPackets := result.PacketsAfter - result.PacketsBefore
			maxErrors := totalPackets * 0.001
			require.LessOrEqual(t, result.ErrorCount, maxErrors,
				"error rate SLO: < 0.1%% of processed packets at N=%d", n)

			require.Greater(t, result.PacketsAfter, result.PacketsBefore,
				"exporter should have processed packets at N=%d", n)

			recordResult(t.Name(), map[string]MetricEntry{
				"actual_pps": {Value: result.ActualPPS, Unit: "pps", Direction: dirHigherIsBetter},
				"loss_rate":  {Value: result.LossRate * 100, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_avg":    {Value: result.CPUAvg, Unit: "%", Direction: dirLowerIsBetter},
				"cpu_peak":   {Value: result.CPUPeak, Unit: "%", Direction: dirLowerIsBetter},
				"mem_mb":     {Value: result.MemMaxMB, Unit: "MB", Direction: dirLowerIsBetter},
				"drain_time_ms": {
					Value:     float64(result.DrainTime.Milliseconds()),
					Unit:      "ms",
					Direction: dirLowerIsBetter,
				},
				"socket_packets_received": {
					Value:     totalPackets,
					Unit:      "count",
					Direction: dirHigherIsBetter,
				},
				"errors": {Value: result.ErrorCount, Unit: "count", Direction: dirLowerIsBetter},
			})
		})
	}
}
