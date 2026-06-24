//go:build e2e

package rtp

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// pcmaFilter matches the PCMA codec label (SIPp streams G.711a, PT=8).
const pcmaFilter = `codec="PCMA"`

// getRTPMetric scrapes an RTP metric value for the PCMA codec label.
// Returns 0 if not found.
func getRTPMetric(t *testing.T, endpoint, name string) float64 {
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

	pattern := `^` + name + `\{[^}]*` + regexp.QuoteMeta(pcmaFilter) + `[^}]*\}\s+([0-9.]+)`
	re := regexp.MustCompile(pattern)
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

// getMetricByLabel scrapes a metric value whose label set contains ALL the given
// label substrings (e.g. `carrier="loopback"`, `codec="PCMA"`), in any order
// (Prometheus emits labels alphabetically, not in registration order). Returns 0
// if no matching sample is found.
func getMetricByLabel(t *testing.T, endpoint, name string, labels ...string) float64 {
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

	prefix := name + "{"
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		matches := true
		for _, l := range labels {
			if !strings.Contains(trimmed, l) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		// Value is the token after the closing brace.
		gtIdx := strings.LastIndexByte(trimmed, '}')
		if gtIdx < 0 {
			continue
		}
		v, parseErr := strconv.ParseFloat(strings.TrimSpace(trimmed[gtIdx+1:]), 64)
		require.NoError(t, parseErr)
		return v
	}
	return 0
}

// runSippRTP runs a UAS+UAC SIPp scenario pair that establishes a SIP dialog with
// SDP and streams real G.711a RTP (built-in /build/pcap/g711a.pcap). The exporter
// captures the SIP signalling (correlating media endpoints from SDP) and the RTP,
// producing labelled RTP metrics.
func runSippRTP(ctx context.Context, t *testing.T, uasSIP, uacSIP, uasMedia, uacMedia string) {
	t.Helper()

	scenarioDir := filepath.Join(projectRoot, "test", "e2e", "sipp")

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// When verbose, stream SIPp output to the test log; otherwise capture stderr
	// into a buffer so it can be dumped on failure for diagnostics.
	var stdout io.Writer = &testWriter{t}
	var stderrBuf bytes.Buffer
	var stderr io.Writer = &stderrBuf
	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
		stderr = &testWriter{t}
	}
	dumpStderr := func(stage string) {
		if stderrBuf.Len() > 0 {
			t.Logf("SIPp %s stderr:\n%s", stage, strings.TrimSpace(stderrBuf.String()))
		}
	}

	// UAS (callee): answers 200 OK + SDP, streams RTP from its -mp media port.
	// -mi forces the media IP to 127.0.0.1 so RTP flows on lo and matches the SDP c= line.
	uasCmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--network", "host",
		"-v", scenarioDir+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/uas_rtp.xml",
		"-i", "127.0.0.1",
		"-mi", "127.0.0.1",
		"-p", uasSIP,
		"-mp", uasMedia,
		"-m", "1",
		"-nr",
		"-nostdin",
	)
	uasCmd.Stdout = stdout
	uasCmd.Stderr = stderr
	require.NoError(t, uasCmd.Start())

	// Wait for UAS to bind its SIP port.
	require.Eventually(t, func() bool {
		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:"+uasSIP)
		if err != nil {
			return false
		}
		l, err := net.ListenUDP("udp", addr)
		if err != nil {
			return true // port busy → UAS is listening
		}
		l.Close()
		return false
	}, 5*time.Second, 50*time.Millisecond, "UAS should listen on SIP port %s", uasSIP)

	// UAC (caller): INVITE+SDP, ACK, streams RTP, BYE.
	uacCmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--network", "host",
		"-v", scenarioDir+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/uac_rtp.xml",
		"-i", "127.0.0.1",
		"-mi", "127.0.0.1",
		"-p", uacSIP,
		"-mp", uacMedia,
		"-m", "1",
		"-nr",
		"127.0.0.1:"+uasSIP,
	)
	uacCmd.Stdout = stdout
	uacCmd.Stderr = stderr
	if err := uacCmd.Run(); err != nil {
		dumpStderr("UAC")
		require.NoErrorf(t, err, "UAC SIPp failed (enable SIP_EXPORTER_E2E_SIPP_VERBOSE=true for full output)")
	}
	_ = uasCmd.Wait()
}

// TestRTP_MetricsFromSIPpStream verifies the full pipeline end-to-end: a real SIP
// dialog (INVITE/200 OK with SDP) + real G.711a RTP streamed by SIPp produces the
// labelled RTP metrics on /metrics. Closes review item I3.
func TestRTP_MetricsFromSIPpStream(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, "0", true)

	runSippRTP(context.Background(), t, uasSIP, uacSIP, uasMedia, uacMedia)

	// RTP packets counter (cumulative) must be > 0: RTP was correlated and observed.
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
	}, 10*time.Second, 500*time.Millisecond, "rtp_packets_total{codec=PCMA} must be observed")

	// Jitter and MOS histograms must have samples (emitted by the 1s snapshot).
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_jitter_milliseconds_count") > 0
	}, 10*time.Second, 500*time.Millisecond, "rtp_jitter histogram must have samples")
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_mos_score_count") > 0
	}, 10*time.Second, 500*time.Millisecond, "rtp_mos histogram must have samples")

	// MOS must be in a sane range for clean G.711 (E-model ~3.9-4.4).
	mosSum := getRTPMetric(t, endpoint, "sip_exporter_rtp_mos_score_sum")
	mosCount := getRTPMetric(t, endpoint, "sip_exporter_rtp_mos_score_count")
	require.Greater(t, mosCount, 0.0)
	avgMOS := mosSum / mosCount
	t.Logf("RTP metrics present: avg MOS=%.2f (PCMA, clean G.711)", avgMOS)
	require.Greater(t, avgMOS, 3.5, "clean G.711 MOS should be > 3.5")
	require.Less(t, avgMOS, 4.6)
}

// testWriter routes SIPp container output to the test log when verbose.
type testWriter struct{ t *testing.T }

func (w *testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", strings.TrimSpace(string(p)))
	return len(p), nil
}
