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
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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
	portMu sync.Mutex

	projectRoot   string
	exporterImage string
)

func init() {
	_, file, _, _ := runtime.Caller(0)
	projectRoot = filepath.Join(filepath.Dir(file), "..", "..", "..")

	exporterImage = os.Getenv("SIP_EXPORTER_E2E_IMAGE")
	if exporterImage == "" {
		exporterImage = "sip-exporter:latest"
	}
}

// allocatePortsN returns n unique port numbers (as strings) using the kernel's
// ephemeral port allocator. Each port is verified free at allocation time via
// net.Listen(":0"), eliminating collisions between parallel subtests.
func allocatePortsN(n int) []string {
	portMu.Lock()
	defer portMu.Unlock()
	out := make([]string, n)
	for i := range n {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			panic(fmt.Sprintf("allocatePortsN: failed to get free port: %v", err))
		}
		out[i] = strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
		l.Close()
	}
	return out
}

// startExporter brings up the exporter container on the given interface(s) with
// the given RTP capture setting and returns its /metrics endpoint.
func startExporter(
	ctx context.Context, t *testing.T,
	httpPort, sipPort, sipsPort, iface string,
	rtpCapture bool, ttl string,
) string {
	t.Helper()

	startCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	logLevel := "error"
	if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
		logLevel = "debug"
	}

	rtpFlag := "false"
	if rtpCapture {
		rtpFlag = "true"
	}

	env := map[string]string{
		"SIP_EXPORTER_INTERFACE":       iface,
		"SIP_EXPORTER_HTTP_PORT":       httpPort,
		"SIP_EXPORTER_SIP_PORT":        sipPort,
		"SIP_EXPORTER_SIPS_PORT":       sipsPort,
		"SIP_EXPORTER_LOGGER_LEVEL":    logLevel,
		"SIP_EXPORTER_IGNORE_OUTGOING": "true",
		"SIP_EXPORTER_RTP_CAPTURE":     rtpFlag,
		"SIP_EXPORTER_TELEMETRY":       "false",
	}
	if ttl != "" {
		env["SIP_EXPORTER_RTP_STREAM_TTL"] = ttl
	}

	req := testcontainers.ContainerRequest{
		Image:       exporterImage,
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
		if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
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
	})

	return fmt.Sprintf("http://localhost:%s", httpPort)
}

// startExporterWithCarrierUA is like startExporter but additionally bind-mounts
// optional carriers.yaml and user_agents.yaml configs so that RTP/SIP metrics
// carry concrete carrier and ua_type labels (mirrors the main e2e suite helper).
func startExporterWithCarrierUA(
	ctx context.Context, t *testing.T,
	httpPort, sipPort, sipsPort string,
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
		"SIP_EXPORTER_SIP_PORT":        sipPort,
		"SIP_EXPORTER_SIPS_PORT":       sipsPort,
		"SIP_EXPORTER_LOGGER_LEVEL":    logLevel,
		"SIP_EXPORTER_IGNORE_OUTGOING": "true",
		"SIP_EXPORTER_RTP_CAPTURE":     "true",
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
		Image:       exporterImage,
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
		if os.Getenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE") == "true" {
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
	})

	return fmt.Sprintf("http://localhost:%s", httpPort)
}

// socketPacketsMetric is the self-monitoring counter used to verify RTP delivery.
const socketPacketsMetric = "sip_exporter_socket_packets_received_total"

// getSocketPacketsReceived scrapes the socket_packets_received_total counter
// from /metrics. It is the signal that packets passed the eBPF filter and were
// delivered to the exporter's AF_PACKET socket.
func getSocketPacketsReceived(t *testing.T, endpoint string) float64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/metrics", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	re := regexp.MustCompile(`^` + socketPacketsMetric + `(?:\{[^}]*\})?\s+([0-9.]+)`)
	for _, line := range strings.Split(string(body), "\n") {
		m := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) == 2 {
			v, parseErr := strconv.ParseFloat(m[1], 64)
			require.NoError(t, parseErr)
			return v
		}
	}
	return 0
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

// TestRTP_ReachesApp_WithCapture verifies that when RTP capture is enabled and
// a media endpoint is registered via SDP, RTP packets pass the eBPF filter and
// reach the exporter's socket.
func TestRTP_ReachesApp_WithCapture(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, sipsPort, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4], ports[5]
	uasMediaNum, _ := strconv.Atoi(uasMedia)

	endpoint := startExporterWithCarrierUA(context.Background(), t, httpPort, uasSIP, sipsPort,
		integrationCarriersYAML, integrationUserAgentsYAML, "")

	wait := startSippContainers(context.Background(), t,
		"uas_nortp.xml", "uac_nortp.xml", uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")

	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_sessions", labelCarrier, labelUAType) >= 1
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

// TestRTP_Dropped_WhenCaptureOff verifies that when RTP capture is disabled, the
// eBPF filter drops RTP packets (none reach the exporter socket).
func TestRTP_Dropped_WhenCaptureOff(t *testing.T) {
	ports := allocatePortsN(4)
	httpPort, sipPort, sipsPort, rtpPort := ports[0], ports[1], ports[2], ports[3]
	rtpPortNum, err := strconv.Atoi(rtpPort)
	require.NoError(t, err)

	endpoint := startExporter(context.Background(), t, httpPort, sipPort, sipsPort, testInterface, false, "")

	time.Sleep(1500 * time.Millisecond)
	before := getSocketPacketsReceived(t, endpoint)

	sendRTP(t, rtpPortNum, rtpPackets)

	time.Sleep(2500 * time.Millisecond)
	after := getSocketPacketsReceived(t, endpoint)

	delta := after - before
	t.Logf("capture=OFF: socket_packets_received_total before=%v after=%v delta=%v (sent %d)",
		before, after, delta, rtpPackets)
	require.Less(t, delta, float64(rtpPackets)*0.1,
		"RTP packets must NOT reach the exporter when capture is disabled")
}

// TestRTP_UncorrelatedDropped verifies RTP isolation: with the strict SDP-driven
// BPF filter, RTP sent to a port with no established SIP dialog (no SDP-registered
// media endpoint) is dropped by BPF — it never reaches the exporter socket and is
// not counted as RTP metrics.
func TestRTP_UncorrelatedDropped(t *testing.T) {
	ports := allocatePortsN(4)
	httpPort, sipPort, sipsPort, rtpPort := ports[0], ports[1], ports[2], ports[3]
	rtpPortNum, err := strconv.Atoi(rtpPort)
	require.NoError(t, err)

	endpoint := startExporter(context.Background(), t, httpPort, sipPort, sipsPort, testInterface, true, "")

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
	require.Equal(t, 0.0, getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total"),
		"uncorrelated RTP must be dropped (no rtp_packets_total)")
}
