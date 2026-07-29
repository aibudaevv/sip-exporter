//go:build e2e

package rtp

import (
	"bytes"
	"context"
	"fmt"
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

type metricSample struct {
	name   string
	labels map[string]string
	value  float64
}

var metricValuePattern = regexp.MustCompile(`^(?:NaN|[+-]?Inf|[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?)$`)

// getRTPMetric reads exactly one RTP metric sample with the PCMA codec label.
func getRTPMetric(t *testing.T, endpoint, name string) float64 {
	t.Helper()
	return getMetricByLabel(t, endpoint, name, pcmaFilter)
}

func fetchMetricsBody(t *testing.T, endpoint string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/metrics", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return body
}

func readMetricSamples(t *testing.T, endpoint, metricName string) []metricSample {
	t.Helper()

	var samples []metricSample
	for _, line := range strings.Split(string(fetchMetricsBody(t, endpoint)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		separator := metricSeriesEnd(line)
		if separator < 0 {
			continue
		}
		series, fields := line[:separator], strings.Fields(line[separator:])
		if len(fields) == 0 || len(fields) > 2 {
			continue
		}
		if len(fields) == 2 {
			if _, err := strconv.ParseInt(fields[1], 10, 64); err != nil {
				continue
			}
		}
		if !metricValuePattern.MatchString(fields[0]) {
			continue
		}
		name := series
		labels := map[string]string{}
		if labelsStart := strings.IndexByte(series, '{'); labelsStart >= 0 {
			name = series[:labelsStart]
			if !strings.HasSuffix(series, "}") {
				continue
			}
			var ok bool
			labels, ok = parseMetricLabels(series[labelsStart+1 : len(series)-1])
			if !ok {
				continue
			}
		}
		if name != metricName {
			continue
		}
		value, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		samples = append(samples, metricSample{name: name, labels: labels, value: value})
	}
	return samples
}

func metricSeriesEnd(line string) int {
	insideLabels := false
	insideValue := false
	escaped := false
	for i := 0; i < len(line); i++ {
		if insideValue {
			if escaped {
				escaped = false
				continue
			}
			switch line[i] {
			case '\\':
				escaped = true
			case '"':
				insideValue = false
			}
			continue
		}
		switch line[i] {
		case '{':
			insideLabels = true
		case '}':
			insideLabels = false
		case '"':
			if insideLabels {
				insideValue = true
			}
		case ' ', '\t':
			if !insideLabels {
				return i
			}
		}
	}
	return -1
}

func parseMetricLabels(input string) (map[string]string, bool) {
	labels := make(map[string]string)
	for input != "" {
		equals := strings.IndexByte(input, '=')
		if equals <= 0 || equals+1 >= len(input) || input[equals+1] != '"' {
			return nil, false
		}
		name := input[:equals]
		if !validMetricLabelName(name) {
			return nil, false
		}
		if _, duplicate := labels[name]; duplicate {
			return nil, false
		}
		valueEnd := equals + 2
		for valueEnd < len(input) {
			if input[valueEnd] == '\\' {
				if valueEnd+1 >= len(input) || !strings.ContainsRune(`\"n`, rune(input[valueEnd+1])) {
					return nil, false
				}
				valueEnd += 2
				continue
			}
			if input[valueEnd] == '"' {
				break
			}
			valueEnd++
		}
		if valueEnd >= len(input) {
			return nil, false
		}
		value, err := strconv.Unquote(input[equals+1 : valueEnd+1])
		if err != nil {
			return nil, false
		}
		labels[name] = value
		input = input[valueEnd+1:]
		if input == "" {
			break
		}
		if input[0] != ',' {
			return nil, false
		}
		input = input[1:]
		if input == "" {
			return nil, false
		}
	}
	return labels, true
}

func validMetricLabelName(name string) bool {
	for i := 0; i < len(name); i++ {
		valid := name[i] == '_' || name[i] >= 'a' && name[i] <= 'z' || name[i] >= 'A' && name[i] <= 'Z'
		if i > 0 {
			valid = valid || name[i] >= '0' && name[i] <= '9'
		}
		if !valid {
			return false
		}
	}
	return name != ""
}

func matchMetricLabels(sampleLabels, filterLabels map[string]string) bool {
	for name, value := range filterLabels {
		sampleValue, exists := sampleLabels[name]
		if !exists || sampleValue != value {
			return false
		}
	}
	return true
}

func metricSamplesWithLabels(samples []metricSample, filterLabels map[string]string) []metricSample {
	var matches []metricSample
	for _, sample := range samples {
		if matchMetricLabels(sample.labels, filterLabels) {
			matches = append(matches, sample)
		}
	}
	return matches
}

func singleMetricValue(samples []metricSample) (float64, error) {
	if len(samples) != 1 {
		return 0, fmt.Errorf("expected exactly one metric sample, got %d", len(samples))
	}
	return samples[0].value, nil
}

func metricExists(t *testing.T, endpoint, name string) bool {
	t.Helper()
	return len(readMetricSamples(t, endpoint, name)) > 0
}

func rtpMetricExists(t *testing.T, endpoint, name string) bool {
	t.Helper()
	return metricWithLabelsExists(t, endpoint, name, pcmaFilter)
}

func metricWithLabelsExists(t *testing.T, endpoint, name string, labels ...string) bool {
	t.Helper()
	filterLabels, ok := parseMetricLabels(strings.Join(labels, ","))
	require.True(t, ok, "invalid label filters %q", labels)
	if !ok {
		return false
	}
	return len(metricSamplesWithLabels(readMetricSamples(t, endpoint, name), filterLabels)) > 0
}

func getMetricByLabel(t *testing.T, endpoint, name string, labels ...string) float64 {
	t.Helper()
	filterLabels, ok := parseMetricLabels(strings.Join(labels, ","))
	require.True(t, ok, "invalid label filters %q", labels)
	if !ok {
		return 0
	}
	matches := metricSamplesWithLabels(readMetricSamples(t, endpoint, name), filterLabels)
	value, err := singleMetricValue(matches)
	require.NoError(t, err, "read metric %s with labels %q", name, labels)
	return value
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

	scenarioDir := filepath.Join(projectRoot(), "test", "e2e", "sipp")

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

// TestRTPMetricsFromSIPpStream verifies the full pipeline end-to-end: a real SIP
// dialog (INVITE/200 OK with SDP) + real G.711a RTP streamed by SIPp produces the
// labelled RTP metrics on /metrics. Closes review item I3.
func TestRTPMetricsFromSIPpStream(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	endpoint := startExporter(t.Context(), t, httpPort, uasSIP, testInterface, "")

	runSippRTP(t.Context(), t, uasSIP, uacSIP, uasMedia, uacMedia)

	// RTP packets counter (cumulative) must be > 0: RTP was correlated and observed.
	require.Eventually(t, func() bool {
		return rtpMetricExists(t, endpoint, "sip_exporter_rtp_packets_total") &&
			getRTPMetric(t, endpoint, "sip_exporter_rtp_packets_total") > 0
	}, 10*time.Second, 500*time.Millisecond, "rtp_packets_total{codec=PCMA} must be observed")

	// Jitter and MOS histograms must have samples (emitted by the 1s snapshot).
	require.Eventually(t, func() bool {
		return rtpMetricExists(t, endpoint, "sip_exporter_rtp_jitter_milliseconds_count") &&
			getRTPMetric(t, endpoint, "sip_exporter_rtp_jitter_milliseconds_count") > 0
	}, 10*time.Second, 500*time.Millisecond, "rtp_jitter histogram must have samples")
	require.Eventually(t, func() bool {
		return rtpMetricExists(t, endpoint, "sip_exporter_rtp_mos_score_count") &&
			getRTPMetric(t, endpoint, "sip_exporter_rtp_mos_score_count") > 0
	}, 10*time.Second, 500*time.Millisecond, "rtp_mos histogram must have samples")

	// PDV histogram must have samples (S11-1): observed per RTP packet in
	// handleRTP (VoIPMonitor parity), so samples appear as soon as media flows.
	require.Eventually(t, func() bool {
		return rtpMetricExists(t, endpoint, "sip_exporter_rtp_pdv_milliseconds_count") &&
			getRTPMetric(t, endpoint, "sip_exporter_rtp_pdv_milliseconds_count") > 0
	}, 10*time.Second, 500*time.Millisecond, "rtp_pdv histogram must have samples")

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
