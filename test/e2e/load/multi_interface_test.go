//go:build e2e

package load

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const multiNICTopologyImage = "alpine:3.22.4"

var multiNICFixtureID atomic.Uint64

type (
	multiNICLink struct {
		hostInterface string
		peerInterface string
		hostIP        string
		peerIP        string
	}

	multiNICFixture struct {
		peerContainerID string
		links           []multiNICLink
	}
)

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
	peerContainerID   string
	exporterContainer testcontainers.Container
	uacTargets        []uacTarget
	limits            WorkloadLimits
}

func routeDevice(route string) (string, bool) {
	fields := strings.Fields(route)
	for i, field := range fields {
		if field == "dev" && i+1 < len(fields) {
			return fields[i+1], true
		}
	}
	return "", false
}

func multiNICBusinessValues(
	samples []metricSample,
	links []multiNICLink,
	callsPerInterface float64,
) map[string]float64 {
	values := map[string]float64{
		"cross_interface_series": 0,
		"unexpected_series":      0,
	}
	interfaces := make(map[string]int, len(links))
	hosts := make(map[string]struct{}, len(links))
	seen := make(map[int]bool, len(links))
	for i, link := range links {
		values[fmt.Sprintf("invites_iface_%d", i+1)] = 0
		interfaces[link.hostInterface] = i
		hosts[link.hostIP] = struct{}{}
	}

	for _, sample := range samples {
		iface, ifaceKnown := interfaces[sample.labels["iface"]]
		calledHost := sample.labels["called_host"]
		_, hostKnown := hosts[calledHost]
		if !ifaceKnown || !hostKnown {
			values["unexpected_series"]++
			continue
		}
		if calledHost != links[iface].hostIP {
			values["cross_interface_series"]++
			continue
		}
		if seen[iface] {
			values["unexpected_series"]++
			continue
		}
		seen[iface] = true
		values[fmt.Sprintf("invites_iface_%d", iface+1)] = sample.value
	}

	return values
}

func newMultiNICFixture(t *testing.T, n int) multiNICFixture {
	t.Helper()
	if n < 1 {
		require.Failf(t, "invalid n", "newMultiNICFixture requires n >= 1, got %d", n)
		return multiNICFixture{}
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	id := multiNICFixtureID.Add(1)
	peerName := fmt.Sprintf("sip-exporter-load-peer-%d-%d", os.Getpid(), id)
	peerOut, err := exec.CommandContext(ctx, "docker", "run", "-d", "--rm",
		"--network", "none", "--cap-add", "NET_ADMIN", "--name", peerName,
		multiNICTopologyImage, "sleep", "infinity",
	).CombinedOutput()
	require.NoError(t, err, "create multi-NIC peer: %s", string(peerOut))

	fixture := multiNICFixture{peerContainerID: strings.TrimSpace(string(peerOut))}
	var cleanupOnce sync.Once
	t.Cleanup(func() {
		cleanupOnce.Do(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanupCancel()
			_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", fixture.peerContainerID).Run()
			for _, link := range fixture.links {
				_, _ = runMultiNICHostIP(cleanupCtx, "link", "delete", link.hostInterface)
			}
		})
	})

	pidOut, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Pid}}",
		fixture.peerContainerID).CombinedOutput()
	require.NoError(t, err, "inspect multi-NIC peer PID: %s", string(pidOut))
	peerPID, err := strconv.Atoi(strings.TrimSpace(string(pidOut)))
	require.NoError(t, err, "parse multi-NIC peer PID")

	for i := range n {
		link := multiNICLink{
			hostInterface: fmt.Sprintf("sml%04x%04x%d", os.Getpid()&0xffff, id&0xffff, i),
			peerInterface: fmt.Sprintf("eth%d", i),
			hostIP:        fmt.Sprintf("10.240.%d.1", i),
			peerIP:        fmt.Sprintf("10.240.%d.2", i),
		}
		fixture.links = append(fixture.links, link)

		out, err := runMultiNICHostIP(ctx, "link", "add", link.hostInterface, "type", "veth")
		require.NoError(t, err, "create host veth %d: %s", i, string(out))
		linkOut, err := runMultiNICHostIP(ctx, "link", "show", "dev", link.hostInterface)
		require.NoError(t, err, "inspect host veth %d: %s", i, string(linkOut))
		peerDevice, ok := vethPeerDevice(string(linkOut))
		require.True(t, ok, "host veth must report peer: %s", string(linkOut))

		for _, args := range [][]string{
			{"link", "set", peerDevice, "netns", strconv.Itoa(peerPID)},
			{"addr", "add", link.hostIP + "/30", "dev", link.hostInterface},
			{"link", "set", link.hostInterface, "up"},
		} {
			out, err = runMultiNICHostIP(ctx, args...)
			require.NoError(t, err, "configure host veth %d: %s", i, string(out))
		}
		for _, args := range [][]string{
			{"link", "set", peerDevice, "name", link.peerInterface},
			{"addr", "add", link.peerIP + "/30", "dev", link.peerInterface},
			{"link", "set", link.peerInterface, "up"},
		} {
			dockerArgs := append([]string{"exec", fixture.peerContainerID, "ip"}, args...)
			out, err = exec.CommandContext(ctx, "docker", dockerArgs...).CombinedOutput()
			require.NoError(t, err, "configure peer veth %d: %s", i, string(out))
		}

		route := fixture.peerRoute(t, link.hostIP)
		device, ok := routeDevice(route)
		require.True(t, ok, "peer route must contain device: %s", route)
		require.NotEqual(t, "lo", device)
		require.Equal(t, link.peerInterface, device)
	}
	return fixture
}

func runMultiNICHostIP(ctx context.Context, args ...string) ([]byte, error) {
	dockerArgs := []string{"run", "--rm", "--network", "host", "--pid", "host", "--privileged",
		multiNICTopologyImage, "ip"}
	dockerArgs = append(dockerArgs, args...)
	return exec.CommandContext(ctx, "docker", dockerArgs...).CombinedOutput()
}

func vethPeerDevice(link string) (string, bool) {
	for _, field := range strings.Fields(link) {
		_, peer, ok := strings.Cut(strings.TrimSuffix(field, ":"), "@")
		if ok && peer != "" {
			return peer, true
		}
	}
	return "", false
}

func (f multiNICFixture) peerRoute(t *testing.T, destination string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "docker", "exec", f.peerContainerID,
		"ip", "route", "get", destination).CombinedOutput()
	require.NoError(t, err, "resolve peer route to %s: %s", destination, string(out))
	return strings.TrimSpace(string(out))
}

func newMultiNICEnv(
	ctx context.Context,
	t *testing.T,
	peerContainerID string,
	links []multiNICLink,
) *multiNICEnv {
	t.Helper()
	require.NotEmpty(t, links, "need at least one interface")

	portMu.Lock()
	httpPort := strconv.Itoa(nextBasePort)
	sipPort := strconv.Itoa(nextBasePort + 1)
	uacPorts := make([]string, len(links))
	for i := range links {
		uacPorts[i] = strconv.Itoa(nextBasePort + 2 + i)
	}
	nextBasePort += 2 + len(links)
	portMu.Unlock()

	ifaces := make([]string, len(links))
	for i, link := range links {
		ifaces[i] = link.hostInterface
	}
	req := exporterContainerRequest(
		ctx, t, strings.Join(ifaces, ","), httpPort, sipPort, peakLimits,
	)
	req.Env["SIP_EXPORTER_HOST_LABELS"] = "true"

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
	require.NoError(t, verifyContainerLimits(ctx, c.GetContainerID(), peakLimits))
	recordScenarioLimits(t, peakLimits)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		recordContainerLogs(cleanupCtx, t, "exporter.log", c)
		if testing.Verbose() {
			logs, logErr := c.Logs(cleanupCtx)
			if logErr == nil {
				defer logs.Close()
				logBytes, _ := io.ReadAll(logs)
				t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
			}
		}
		_ = c.Stop(cleanupCtx, nil)
		_ = c.Terminate(cleanupCtx)
		for range 10 {
			conn, dialErr := net.DialTimeout("tcp", "localhost:"+httpPort, 500*time.Millisecond)
			if dialErr != nil {
				return
			}
			conn.Close()
			time.Sleep(500 * time.Millisecond)
		}
	})

	targets := make([]uacTarget, len(links))
	for i, link := range links {
		targets[i] = uacTarget{uacIP: link.peerIP, uacPort: uacPorts[i], uasIP: link.hostIP}
	}

	return &multiNICEnv{
		endpoint:          fmt.Sprintf("http://localhost:%s", httpPort),
		sipPort:           sipPort,
		peerContainerID:   peerContainerID,
		exporterContainer: c,
		uacTargets:        targets,
		limits:            peakLimits,
	}
}

// runMultiNICLoad runs N UAC instances in parallel (one per interface), each
// sending callCount INVITEs at the given rate. Returns the load and generator evidence.
// Uses one steady-state coordinator for the single exporter container.
func runMultiNICLoad(
	ctx context.Context,
	t *testing.T,
	uacScenario string,
	callCount, rate int,
	env *multiNICEnv,
) (loadResult, []GeneratorResult) {
	t.Helper()

	measurementEnv := &testEnv{
		endpoint: env.endpoint, exporterContainer: env.exporterContainer, limits: env.limits,
	}
	measurement, measurementErr := newSteadyMeasurement(ctx, measurementEnv)
	require.NoError(t, measurementErr)

	recordMetricsSnapshot(t, "metrics-before.prom", env.endpoint)
	protocolsBefore := readProtocolCounters(t, env.endpoint)
	packetsBefore := protocolsBefore.SIPPackets
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	const packetsPerCall = 1.0 // flood_uac.xml sends 1 INVITE per call
	expectedTotal := float64(callCount * len(env.uacTargets) * int(packetsPerCall))

	uacPath := absScenarioPath(t, uacScenario)
	sippVol := filepath.Dir(uacPath)
	uacFile := filepath.Base(uacScenario)

	uacs := make([]*startedSippContainer, len(env.uacTargets))
	for i, tgt := range env.uacTargets {
		uacs[i] = prepareSippContainerInPeerNetns(ctx, t,
			[]string{
				"-sf", "/scenarios/" + uacFile,
				"-i", tgt.uacIP,
				"-p", tgt.uacPort,
				"-m", strconv.Itoa(callCount),
				"-r", strconv.Itoa(rate),
				"-nr",
				tgt.uasIP + ":" + env.sipPort,
			},
			sippVol, fmt.Sprintf("generator-%d", i), env.peerContainerID,
		)
	}
	require.NoError(t, measurement.Begin(ctx, time.Now()))
	startPreparedSippContainers(ctx, t, uacs...)
	measureStart := time.Now()

	measureEnds := make([]time.Time, len(uacs))
	for i, uac := range uacs {
		waitForContainerExit(ctx, t, uac)
		measureEnds[i] = time.Now()
	}

	measureEnd := measureEnds[0]
	for _, ended := range measureEnds[1:] {
		measureEnd = laterTime(measureEnd, ended)
	}
	sippDuration := measureEnd.Sub(measureStart)
	resourceSummary := finishSteadyMeasurement(ctx, t, measurement, measureEnd)
	postPhase := make([]time.Time, 0, len(uacs)+1)
	generators := make([]GeneratorResult, len(uacs))
	for i, uac := range uacs {
		var generatorErr error
		generators[i], generatorErr = uac.readGeneratorEvidence(ctx, t, PhaseTimestamps{
			WarmupStart: uac.started, Ready: measureStart, MeasureStart: measureStart,
			MeasureEnd: measureEnds[i], DrainEnd: measureEnds[i],
		})
		require.NoError(t, generatorErr)
		postPhase = append(postPhase, time.Now())
	}

	waitForExactSIPCapture(ctx, t, env.endpoint, packetsBefore, expectedTotal)

	stableTime := time.Now()
	postPhase = append(postPhase, stableTime)
	require.NoError(t, validatePostPhaseOrdering(measureEnd, postPhase...))
	drainTime := stableTime.Sub(measureEnd)
	for i := range generators {
		generators[i].Phases.DrainEnd = stableTime
		if activeRunRecorder != nil {
			require.NoError(t, activeRunRecorder.AttachGenerator(t.Name(), generators[i]))
		}
	}

	recordMetricsSnapshot(t, "metrics-after.prom", env.endpoint)
	protocolsAfter := readProtocolCounters(t, env.endpoint)
	protocols := protocolsAfter.delta(protocolsBefore)
	packetsAfter := protocolsAfter.SIPPackets
	errorsAfter := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	capture := newCaptureResult(expectedTotal, protocols.SIPPackets)
	require.NoError(t, capture.ValidateExact())
	totalCaptured := capture.Captured
	actualPPS := 0.0
	if sippDuration.Seconds() > 0 {
		actualPPS = totalCaptured / sippDuration.Seconds()
	}

	expectedPPS := float64(rate * len(env.uacTargets) * int(packetsPerCall))

	result := loadResult{
		Duration:      sippDuration,
		Capture:       capture,
		Protocols:     protocols,
		PacketsBefore: packetsBefore,
		PacketsAfter:  packetsAfter,
		ActualPPS:     actualPPS,
		ExpectedPPS:   expectedPPS,
		LossRate:      capture.LossPct / 100,
		ErrorCount:    errorsAfter - errorsBefore,
		DrainTime:     drainTime,
		Resources:     resourceSummary,
	}
	recordLoadResultEvidence(t, result)

	t.Logf("MultiNIC N=%d: actual=%.0f PPS (exp=%.0f), captured=%.0f, loss=%.2f%%, "+
		"drain=%v, cpu=%.2f%%(peak=%.2f%%), mem=%.1fMB, errors=%.0f",
		len(env.uacTargets), result.ActualPPS, result.ExpectedPPS,
		totalCaptured, result.LossRate*100, result.DrainTime,
		result.Resources.CPUP95Percent, result.Resources.CPUP95Percent,
		result.Resources.WorkingSetP99MB, result.ErrorCount)

	return result, generators
}

func TestMultiNICUnconfiguredInterface(t *testing.T) {
	fixture := newMultiNICFixture(t, 2)
	env := newMultiNICEnv(t.Context(), t, fixture.peerContainerID, fixture.links[:1])
	link := fixture.links[1]
	uacPath := absScenarioPath(t, "flood_uac.xml")
	uac := prepareSippContainerInPeerNetns(t.Context(), t, []string{
		"-sf", "/scenarios/flood_uac.xml",
		"-i", link.peerIP,
		"-p", allocatePortsN(1)[0],
		"-m", "10",
		"-r", "10",
		"-nr",
		link.hostIP + ":" + env.sipPort,
	}, filepath.Dir(uacPath), "", fixture.peerContainerID)
	startPreparedSippContainers(t.Context(), t, uac)
	waitForContainerExit(t.Context(), t, uac)
	state, err := uac.State(t.Context())
	require.NoError(t, err)
	require.Zero(t, state.ExitCode)

	require.Never(t, func() bool {
		for _, sample := range readMetricSamples(t, env.endpoint, "sip_exporter_invite_total") {
			if sample.labels["iface"] == link.hostInterface {
				return true
			}
		}
		return false
	}, time.Second, 50*time.Millisecond,
		"unconfigured interface %s must not produce INVITE series", link.hostInterface)
}

func TestLoadMultiInterface(t *testing.T) {
	for _, tt := range []struct {
		name       string
		interfaces int
	}{
		{name: "interfaces_1", interfaces: 1},
		{name: "interfaces_2", interfaces: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			beginScenario(t)
			fixture := newMultiNICFixture(t, tt.interfaces)
			env := newMultiNICEnv(t.Context(), t, fixture.peerContainerID, fixture.links)
			result, generators := runMultiNICLoad(t.Context(), t, "flood_uac.xml",
				multiNICCallsPerInterface, multiNICRatePerInterface, env)
			business := multiNICBusinessValues(readMetricSamples(t, env.endpoint,
				"sip_exporter_invite_total"), fixture.links, float64(multiNICCallsPerInterface))

			require.Len(t, generators, tt.interfaces)
			require.NoError(t, validateMultiNICGeneratorOverlap(generators))
			generatorSpec := WorkloadSpec{
				Calls: multiNICCallsPerInterface,
				Rate:  multiNICRatePerInterface,
			}
			for _, generator := range generators {
				require.NoError(t, generator.Validate(generatorSpec))
			}
			result.Generator = aggregateMultiNICGenerators(generators)
			require.NoError(t, result.Generator.Validate(WorkloadSpec{
				Calls: multiNICCallsPerInterface * tt.interfaces,
				Rate:  float64(multiNICRatePerInterface * tt.interfaces),
			}))
			require.NoError(t, result.Capture.ValidateExact())
			require.Zero(t, result.ErrorCount)
			require.Zero(t, result.Protocols.SocketDropped)
			for i := range tt.interfaces {
				require.Equal(t, float64(multiNICCallsPerInterface),
					business[fmt.Sprintf("invites_iface_%d", i+1)])
			}
			require.Zero(t, business["cross_interface_series"])
			require.Zero(t, business["unexpected_series"])
			recordResult(t, targetedMultiNICMetrics(result, business))
		})
	}
}

func TestReleaseMultiInterface(t *testing.T) {
	profile := releaseMultiNICProfile()
	beginScenario(t)
	fixture := newMultiNICFixture(t, multiNICCount)
	env := newMultiNICEnv(t.Context(), t, fixture.peerContainerID, fixture.links)
	result, generators := runMultiNICLoad(t.Context(), t, "flood_uac.xml",
		multiNICCallsPerInterface, multiNICRatePerInterface, env)
	business := multiNICBusinessValues(readMetricSamples(t, env.endpoint,
		"sip_exporter_invite_total"), fixture.links, float64(multiNICCallsPerInterface))

	require.Len(t, generators, multiNICCount)
	require.NoError(t, validateMultiNICGeneratorOverlap(generators))
	result.Generator = aggregateMultiNICGenerators(generators)
	require.NoError(t, result.Generator.Validate(profile.Workload))
	require.NoError(t, validateReleaseRow(releaseRowSpec{},
		releaseMultiNICRowFromLoad(profile, result, generators, business)))
	recordReleaseResult(t, result, business, nil)
}

func targetedMultiNICMetrics(
	result loadResult,
	business map[string]float64,
) map[string]MetricEntry {
	metrics := resourceMetricEntries(result.Resources)
	metrics["generator_cps"] = MetricEntry{
		Value: result.Generator.ActualRate, Unit: "cps", Direction: dirHigherIsBetter,
	}
	metrics["captured_packets"] = MetricEntry{
		Value: result.Capture.Captured, Unit: "count", Direction: dirHigherIsBetter,
	}
	metrics["system_errors"] = MetricEntry{
		Value: result.ErrorCount, Unit: "count", Direction: dirLowerIsBetter,
	}
	for name, value := range business {
		metrics[name] = releaseBusinessMetricEntry(name, value)
	}
	return metrics
}
