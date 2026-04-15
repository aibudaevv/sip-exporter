//go:build e2e

package e2e

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRRD_RegistrationSuccess(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)
	time.Sleep(2 * time.Second)

	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", 50)

	time.Sleep(3 * time.Second)

	registerTotal := getMetric(t, endpoint, "sip_exporter_register_total")
	t.Logf("sip_exporter_register_total = %.0f", registerTotal)
	require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

	status200Total := getMetric(t, endpoint, "sip_exporter_200_total")
	t.Logf("sip_exporter_200_total = %.0f", status200Total)
	require.Greater(t, status200Total, 0.0, "should have 200 OK responses")

	rrd := getRRD(t, endpoint)
	t.Logf("RRD = %.2f ms", rrd)
	require.Greater(t, rrd, 0.0, "RRD should be greater than 0 after successful registrations")
}

func getRRD(t *testing.T, endpoint string) float64 {
	t.Helper()
	return getMetric(t, endpoint, "sip_exporter_rrd")
}

func getMetric(t *testing.T, endpoint string, metricName string) float64 {
	t.Helper()

	resp, err := http.Get(endpoint + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	re := regexp.MustCompile(`^` + metricName + `\s+([0-9.]+)`)
	for _, line := range strings.Split(string(body), "\n") {
		matches := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) == 2 {
			val, err := strconv.ParseFloat(matches[1], 64)
			require.NoError(t, err)
			return val
		}
	}

	return 0
}
