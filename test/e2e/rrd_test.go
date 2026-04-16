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

func TestRRD_Register401(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)
	time.Sleep(2 * time.Second)

	rrdBefore := getRRD(t, endpoint)

	runSippScenario(ctx, t, "reg_uas_401.xml", "reg_uac_401.xml", 20)

	time.Sleep(3 * time.Second)

	registerTotal := getMetric(t, endpoint, "sip_exporter_register_total")
	t.Logf("sip_exporter_register_total = %.0f", registerTotal)
	require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

	status401Total := getMetric(t, endpoint, "sip_exporter_401_total")
	t.Logf("sip_exporter_401_total = %.0f", status401Total)
	require.Greater(t, status401Total, 0.0, "should have 401 responses")

	rrdAfter := getRRD(t, endpoint)
	t.Logf("RRD before = %.2f ms, after = %.2f ms", rrdBefore, rrdAfter)
	require.Equal(t, rrdBefore, rrdAfter, "RRD should not change for 401 responses")
}

func TestRRD_Register403(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)
	time.Sleep(2 * time.Second)

	rrdBefore := getRRD(t, endpoint)

	runSippScenario(ctx, t, "reg_uas_403.xml", "reg_uac_403.xml", 20)

	time.Sleep(3 * time.Second)

	registerTotal := getMetric(t, endpoint, "sip_exporter_register_total")
	require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

	status403Total := getMetric(t, endpoint, "sip_exporter_403_total")
	require.Greater(t, status403Total, 0.0, "should have 403 responses")

	rrdAfter := getRRD(t, endpoint)
	require.Equal(t, rrdBefore, rrdAfter, "RRD should not change for 403 responses")
}

func TestRRD_Register500(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)
	time.Sleep(2 * time.Second)

	rrdBefore := getRRD(t, endpoint)

	runSippScenario(ctx, t, "reg_uas_500.xml", "reg_uac_500.xml", 20)

	time.Sleep(3 * time.Second)

	registerTotal := getMetric(t, endpoint, "sip_exporter_register_total")
	require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

	status500Total := getMetric(t, endpoint, "sip_exporter_500_total")
	require.Greater(t, status500Total, 0.0, "should have 500 responses")

	rrdAfter := getRRD(t, endpoint)
	require.Equal(t, rrdBefore, rrdAfter, "RRD should not change for 500 responses")
}

func TestRRD_RegisterTimeout(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)
	time.Sleep(2 * time.Second)

	rrdBefore := getRRD(t, endpoint)
	registerBefore := getMetric(t, endpoint, "sip_exporter_register_total")

	runSippUACOnly(ctx, t, "reg_uac.xml", 5)

	time.Sleep(3 * time.Second)

	registerAfter := getMetric(t, endpoint, "sip_exporter_register_total")
	require.Greater(t, registerAfter, registerBefore, "should have REGISTER requests")

	rrdAfter := getRRD(t, endpoint)
	require.Equal(t, rrdBefore, rrdAfter, "RRD should not change for timeout (no response)")
}

func TestRRD_ConcurrentRegistrations(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)
	time.Sleep(2 * time.Second)

	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", 100)

	time.Sleep(5 * time.Second)

	registerTotal := getMetric(t, endpoint, "sip_exporter_register_total")
	t.Logf("sip_exporter_register_total = %.0f", registerTotal)
	require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

	rrd := getRRD(t, endpoint)
	t.Logf("RRD = %.2f ms", rrd)
	require.Greater(t, rrd, 0.0, "RRD should be measured for all concurrent registrations")
}

func TestSER_ConcurrentRequests(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)
	time.Sleep(2 * time.Second)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 30)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 10)
	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 10)

	time.Sleep(5 * time.Second)

	ser := getMetric(t, endpoint, "sip_exporter_ser")
	t.Logf("SER = %.2f%%", ser)
	require.Greater(t, ser, 0.0, "SER should be calculated")
	require.LessOrEqual(t, ser, 100.0, "SER should not exceed 100%")
}
