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

	pattern := `^` + name + `\{[^}]*` + regexp.QuoteMeta(pcmaFilter) + `[^}]*\}\s+(\S+)`
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

// startSippContainers starts UAS and UAC SIPp containers with the given scenario
// files. UAC is launched in a goroutine so the caller can inject traffic
// concurrently during the dialog's active phase (between ACK and BYE). The
// returned function blocks until both containers finish and must be called
// (typically at the end of the test, possibly via defer).
func startSippContainers(
	ctx context.Context, t *testing.T,
	uasXML, uacXML, uasSIP, uacSIP, uasMedia, uacMedia, uasIP, uacIP string,
) func() {
	t.Helper()

	scenarioDir := filepath.Join(projectRoot, "test", "e2e", "sipp")

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(cancel)

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

	uasCmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--network", "host",
		"-v", scenarioDir+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/"+uasXML,
		"-i", uasIP,
		"-mi", uasIP,
		"-p", uasSIP,
		"-mp", uasMedia,
		"-m", "1",
		"-nr",
		"-nostdin",
	)
	uasCmd.Stdout = stdout
	uasCmd.Stderr = stderr
	require.NoError(t, uasCmd.Start())

	require.Eventually(t, func() bool {
		addr, err := net.ResolveUDPAddr("udp", uasIP+":"+uasSIP)
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

	uacCmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--network", "host",
		"-v", scenarioDir+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/"+uacXML,
		"-i", uacIP,
		"-mi", uacIP,
		"-p", uacSIP,
		"-mp", uacMedia,
		"-m", "1",
		"-nr",
		uasIP+":"+uasSIP,
	)
	uacCmd.Stdout = stdout
	uacCmd.Stderr = stderr

	uacDone := make(chan error, 1)
	go func() {
		uacDone <- uacCmd.Run()
	}()

	return func() {
		if err := <-uacDone; err != nil {
			dumpStderr("UAC")
			require.NoErrorf(t, err, "UAC SIPp failed (enable SIP_EXPORTER_E2E_SIPP_VERBOSE=true for full output)")
		}
		_ = uasCmd.Wait()
	}
}

// runSippRTP runs a UAS+UAC SIPp scenario pair that establishes a SIP dialog with
// SDP and streams real G.711a RTP (built-in /build/pcap/g711a.pcap). The exporter
// captures the SIP signalling (correlating media endpoints from SDP) and the RTP,
// producing labelled RTP metrics.
func runSippRTP(ctx context.Context, t *testing.T, uasSIP, uacSIP, uasMedia, uacMedia string) {
	runSippRTPWithIPs(ctx, t, uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")
}

// runSippRTPWithIPs is like runSippRTP but binds UAS and UAC to the given IPs,
// enabling tests with non-loopback media endpoints.
func runSippRTPWithIPs(ctx context.Context, t *testing.T, uasSIP, uacSIP, uasMedia, uacMedia, uasIP, uacIP string) {
	startSippContainers(ctx, t, "uas_rtp.xml", "uac_rtp.xml", uasSIP, uacSIP, uasMedia, uacMedia, uasIP, uacIP)()
}

// TestRTP_MetricsFromSIPpStream verifies the full pipeline end-to-end: a real SIP
// dialog (INVITE/200 OK with SDP) + real G.711a RTP streamed by SIPp produces the
// labelled RTP metrics on /metrics. Closes review item I3.
func TestRTP_MetricsFromSIPpStream(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, "0", testInterface, true, "")

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
