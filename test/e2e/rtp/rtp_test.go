//go:build e2e

// Package rtp contains e2e tests verifying that RTP traffic is captured by the
// eBPF filter and reaches the Go exporter. It is a self-contained package
// mirroring test/e2e/load (own port allocator and helpers) so it can run
// independently and avoid AF_PACKET contention with the main SIP e2e suite.
package rtp

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	testInterface = "lo"
	rtpPackets    = 120 // RTP packets sent per test
	sippImage     = "pbertera/sipp:latest"
)

var (
	portMu    sync.Mutex
	usedPorts = make(map[int]struct{})
)

func projectRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

func exporterImage() string {
	image := os.Getenv("SIP_EXPORTER_E2E_IMAGE")
	if image == "" {
		return "sip-exporter:latest"
	}
	return image
}

// allocatePortsN returns n unique port numbers (as strings) available for both
// UDP SIP/RTP sockets and the TCP HTTP listener used by the first port.
func allocatePortsN(n int) []string {
	portMu.Lock()
	defer portMu.Unlock()

	out := make([]string, 0, n)
	for len(out) < n {
		l, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		if err != nil {
			panic(fmt.Sprintf("allocatePortsN: failed to get free port: %v", err))
		}
		port := l.LocalAddr().(*net.UDPAddr).Port
		tcpListener, tcpErr := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		_ = l.Close()
		if tcpErr != nil {
			continue
		}
		_ = tcpListener.Close()
		if _, ok := usedPorts[port]; ok {
			continue
		}
		usedPorts[port] = struct{}{}
		out = append(out, strconv.Itoa(port))
	}
	return out
}

// startExporter brings up the exporter container on the given interface(s)
// and returns its /metrics endpoint.
func startExporter(
	ctx context.Context, t *testing.T,
	httpPort, sipPort, iface string,
	ttl string,
) string {
	t.Helper()
	return startExporterWithExtraEnv(ctx, t, httpPort, sipPort, iface, ttl, nil)
}

// startExporterWithExtraEnv is like startExporter but merges extraEnv into the
// exporter container environment (used to tune fraud/threshold config per test).
func startExporterWithExtraEnv(
	ctx context.Context, t *testing.T,
	httpPort, sipPort, iface, ttl string,
	extraEnv map[string]string,
) string {
	t.Helper()

	startCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	logLevel := "error"
	if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
		logLevel = "debug"
	}

	env := map[string]string{
		"SIP_EXPORTER_INTERFACE":       iface,
		"SIP_EXPORTER_HTTP_PORT":       httpPort,
		"SIP_EXPORTER_SIP_PORTS":       sipPort,
		"SIP_EXPORTER_LOGGER_LEVEL":    logLevel,
		"SIP_EXPORTER_IGNORE_OUTGOING": "true",
		"SIP_EXPORTER_TELEMETRY":       "false",
	}
	if ttl != "" {
		env["SIP_EXPORTER_RTP_STREAM_TTL"] = ttl
	}
	for k, v := range extraEnv {
		env[k] = v
	}

	req := testcontainers.ContainerRequest{
		Image:       exporterImage(),
		Privileged:  true,
		NetworkMode: "host",
		Env:         env,
		WaitingFor: wait.ForHTTP("/metrics").
			WithPort(httpPort + "/tcp").
			WithStartupTimeout(60 * time.Second),
	}

	c, err := testcontainers.GenericContainer(startCtx, testcontainers.GenericContainerRequest{
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
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
			logs, logErr := c.Logs(cleanupCtx)
			if logErr == nil {
				defer logs.Close()
				logBytes, _ := io.ReadAll(logs)
				t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
			}
		}
		_ = c.Stop(cleanupCtx, nil)
		_ = c.Terminate(cleanupCtx)
	})

	endpoint := fmt.Sprintf("http://localhost:%s", httpPort)
	waitForCaptureReady(t, endpoint, sipPort)
	return endpoint
}

func waitForCaptureReady(t *testing.T, endpoint, sipPorts string) {
	t.Helper()
	sipPort := strings.Split(sipPorts, ",")[0]
	port, err := strconv.Atoi(sipPort)
	require.NoError(t, err)

	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	require.NoError(t, err)
	defer listener.Close()

	sender, err := net.DialUDP("udp4", nil, listener.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	defer sender.Close()

	message := fmt.Sprintf("OPTIONS sip:ready@127.0.0.1:%s SIP/2.0\r\nFrom: readiness <sip:readiness@127.0.0.1>;tag=ready\r\nTo: service <sip:service@127.0.0.1:%s>\r\nCall-ID: capture-ready-%s\r\nCSeq: 1 OPTIONS\r\nUser-Agent: readiness\r\nContent-Length: 0\r\n\r\n", sipPort, sipPort, sipPort)
	require.Eventually(t, func() bool {
		_, err = sender.Write([]byte(message))
		require.NoError(t, err)
		return metricExists(t, endpoint, "sip_exporter_options_total")
	}, 5*time.Second, 100*time.Millisecond, "AF_PACKET capture must be ready before sending test traffic")
}

// startExporterWithCarrierUA is like startExporter but additionally bind-mounts
// optional carriers.yaml and user_agents.yaml configs so that RTP/SIP metrics
// carry concrete carrier and ua_type labels (mirrors the main e2e suite helper).
func startExporterWithCarrierUA(
	ctx context.Context, t *testing.T,
	httpPort, sipPort string,
	carriersYAML, userAgentsYAML, ttl string,
) string {
	t.Helper()

	startCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	logLevel := "error"
	if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
		logLevel = "debug"
	}

	envVars := map[string]string{
		"SIP_EXPORTER_INTERFACE":       testInterface,
		"SIP_EXPORTER_HTTP_PORT":       httpPort,
		"SIP_EXPORTER_SIP_PORTS":       sipPort,
		"SIP_EXPORTER_LOGGER_LEVEL":    logLevel,
		"SIP_EXPORTER_IGNORE_OUTGOING": "true",
		"SIP_EXPORTER_TELEMETRY":       "false",
	}
	if ttl != "" {
		envVars["SIP_EXPORTER_RTP_STREAM_TTL"] = ttl
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

	req := testcontainers.ContainerRequest{
		Image:       exporterImage(),
		Privileged:  true,
		NetworkMode: "host",
		Env:         envVars,
		Mounts:      mounts,
		WaitingFor: wait.ForHTTP("/metrics").
			WithPort(httpPort + "/tcp").
			WithStartupTimeout(60 * time.Second),
	}

	c, err := testcontainers.GenericContainer(startCtx, testcontainers.GenericContainerRequest{
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
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
			logs, logErr := c.Logs(cleanupCtx)
			if logErr == nil {
				defer logs.Close()
				logBytes, _ := io.ReadAll(logs)
				t.Logf("Exporter logs:\n%s", strings.TrimSpace(string(logBytes)))
			}
		}
		_ = c.Stop(cleanupCtx, nil)
		_ = c.Terminate(cleanupCtx)
	})

	endpoint := fmt.Sprintf("http://localhost:%s", httpPort)
	waitForCaptureReady(t, endpoint, sipPort)
	return endpoint
}

// socketPacketsMetric is the self-monitoring counter used to verify RTP delivery.
const socketPacketsMetric = "sip_exporter_socket_packets_received_total"

// getSocketPacketsReceived scrapes the socket_packets_received_total counter
// from /metrics. It is the signal that packets passed the eBPF filter and were
// delivered to the exporter's AF_PACKET socket.
func getSocketPacketsReceived(t *testing.T, endpoint string) float64 {
	t.Helper()
	const ifaceFilter = `iface="lo"`
	require.True(t, metricWithLabelsExists(t, endpoint, socketPacketsMetric, ifaceFilter),
		"%s{%s} must exist", socketPacketsMetric, ifaceFilter)
	return getMetricByLabel(t, endpoint, socketPacketsMetric, ifaceFilter)
}

// sendRTP sends count RTP-version-2 UDP packets to 127.0.0.1:port. The packets
// are NOT addressed to the SIP port, so they can only be passed by the eBPF
// filter via RTP pattern detection (first payload byte 0x80).
//
// A local UDP listener is bound to the target port (mirroring how SIPp delivers
// traffic that the exporter captures): this forces the packet to complete the
// loopback receive cycle (PACKET_HOST) which the exporter's AF_PACKET socket
// with PACKET_IGNORE_OUTGOING actually sees, and avoids ICMP port-unreachable.
func sendRTP(t *testing.T, port int, count int) {
	t.Helper()

	listenAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	listener, err := net.ListenUDP("udp4", listenAddr)
	require.NoError(t, err)
	defer listener.Close()

	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		buf := make([]byte, 1500)
		for {
			select {
			case <-done:
				return
			default:
			}
			_ = listener.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			if _, _, e := listener.ReadFromUDP(buf); e != nil {
				continue
			}
		}
	}()

	sender, err := net.DialUDP("udp4", nil, listenAddr)
	require.NoError(t, err)
	defer sender.Close()

	// 12-byte RTP header (RFC 3550) + 16 bytes payload
	pkt := make([]byte, 28)
	pkt[0] = 0x80                             // V=2, P=0, X=0, CC=0
	pkt[1] = 0x08                             // M=0, PT=8 (PCMA)
	binary.BigEndian.PutUint32(pkt[4:8], 160) // timestamp

	for i := range count {
		binary.BigEndian.PutUint16(pkt[2:4], uint16(i+1)) // sequence number
		_, _ = sender.Write(pkt)
		if i%10 == 0 {
			time.Sleep(5 * time.Millisecond) // spread across the 1s stats window
		}
	}
}

// sendRTPOutOfOrder sends 3 RTP packets with sequence numbers 1, 5, 3 to the
// given media port. This triggers reorder detection (seq=3 < maxSeq=5) in the
// media tracker. Uses PT=8 (PCMA) to match SIPp's G.711a stream codec.
func sendRTPOutOfOrder(t *testing.T, portStr string) {
	t.Helper()

	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	conn, err := net.DialUDP("udp4", nil, addr)
	require.NoError(t, err)
	defer conn.Close()

	pkt := make([]byte, 28)
	pkt[0] = 0x80                             // V=2, P=0, X=0, CC=0
	pkt[1] = 0x08                             // M=0, PT=8 (PCMA)
	binary.BigEndian.PutUint32(pkt[4:8], 160) // timestamp

	for _, seq := range []uint16{1, 5, 3} {
		binary.BigEndian.PutUint16(pkt[2:4], seq)
		_, _ = conn.Write(pkt)
		time.Sleep(5 * time.Millisecond)
	}
}

// TestRTPReachesAppWithCapture verifies that when RTP capture is enabled and
// a media endpoint is registered via SDP, RTP packets pass the eBPF filter and
// reach the exporter's socket.
func TestRTPReachesAppWithCapture(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uasMediaNum, _ := strconv.Atoi(uasMedia)

	endpoint := startExporterWithCarrierUA(t.Context(), t, httpPort, uasSIP,
		integrationCarriersYAML, integrationUserAgentsYAML, "")

	wait := startSippContainers(t.Context(), t,
		"uas_nortp.xml", "uac_nortp.xml", uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")

	require.Eventually(t, func() bool {
		return metricWithLabelsExists(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) &&
			getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) >= 1
	}, 10*time.Second, 200*time.Millisecond, "dialog must be established")

	time.Sleep(1500 * time.Millisecond)
	before := getSocketPacketsReceived(t, endpoint)

	sendControlledRTP(t, uasMediaNum, []uint16{1, 2, 3, 4, 5})

	// Allow the exporter's 1s getsockopt loop to accumulate the received count.
	time.Sleep(2500 * time.Millisecond)
	after := getSocketPacketsReceived(t, endpoint)

	delta := after - before
	t.Logf("capture=ON: socket_packets_received_total before=%v after=%v delta=%v (sent 5)",
		before, after, delta)
	require.GreaterOrEqual(t, delta, 3.0,
		"RTP packets must reach the exporter socket when capture is enabled on a registered endpoint")

	wait()
}

// TestRTPUncorrelatedDropped verifies RTP isolation: with the strict SDP-driven
// BPF filter, RTP sent to a port with no established SIP dialog (no SDP-registered
// media endpoint) is dropped by BPF — it never reaches the exporter socket and is
// not counted as RTP metrics.
func TestRTPUncorrelatedDropped(t *testing.T) {
	ports := allocatePortsN(4)
	httpPort, sipPort, rtpPort := ports[0], ports[1], ports[2]
	rtpPortNum, err := strconv.Atoi(rtpPort)
	require.NoError(t, err)

	endpoint := startExporter(t.Context(), t, httpPort, sipPort, testInterface, "")

	time.Sleep(1500 * time.Millisecond)
	beforeSocket := getSocketPacketsReceived(t, endpoint)

	// RTP to a media port with NO established SIP dialog (no SDP exchange).
	sendRTP(t, rtpPortNum, rtpPackets)

	time.Sleep(2500 * time.Millisecond)
	afterSocket := getSocketPacketsReceived(t, endpoint)

	// Strict BPF: unregistered RTP port → dropped by BPF, socket counter stays flat.
	require.Less(t, afterSocket-beforeSocket, float64(rtpPackets)*0.1,
		"uncorrelated RTP must NOT reach the socket (strict SDP-driven BPF drops it)")

	// No RTP metrics counted.
	require.False(t, rtpMetricExists(t, endpoint, "sip_exporter_rtp_packets_total"),
		"uncorrelated RTP must be dropped (no rtp_packets_total)")
}
