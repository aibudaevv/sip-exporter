//go:build e2e

package e2e

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCaptureStartupSummary(t *testing.T) {
	exporterPort, sippPort, sippClientPort := allocatePorts()
	_, container := startExporterWithConfigAndUA(t.Context(), t,
		exporterPort, sippPort, sippClientPort, "", "",
		map[string]string{"SIP_EXPORTER_LOGGER_LEVEL": "info"}, "", "")
	registerExporterCleanup(t, container, exporterPort)

	logs, err := container.Logs(t.Context())
	require.NoError(t, err)
	defer logs.Close()
	logBytes, readErr := io.ReadAll(logs)
	require.NoError(t, readErr)

	var summaries []string
	for _, line := range strings.Split(string(logBytes), "\n") {
		if strings.Contains(line, "capture configured") {
			summaries = append(summaries, line)
		}
	}
	require.Len(t, summaries, 1)

	summary := summaries[0]
	require.Contains(t, summary, `"interfaces": ["lo"]`)
	require.Contains(t, summary, `"sip_ports": [[`+sippPort+`]]`)
	require.Contains(t, summary, `"capture_mode": "host"`)
	require.Contains(t, summary, `"rtp_filter": "sdp-strict"`)
	require.Contains(t, summary, `"ignore_outgoing": true`)
	require.NotContains(t, summary, "call_id")
	require.NotContains(t, summary, "src_ip")
	require.NotContains(t, summary, "dst_ip")
}
