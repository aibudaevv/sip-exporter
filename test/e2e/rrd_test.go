//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRRD_RegistrationSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", 50, env)

	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")
	t.Logf("sip_exporter_register_total = %.0f", registerTotal)
	require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

	status200Total := getMetric(t, env.endpoint, "sip_exporter_200_total")
	t.Logf("sip_exporter_200_total = %.0f", status200Total)
	require.Greater(t, status200Total, 0.0, "should have 200 OK responses")

	rrd := getRRD(t, env.endpoint)
	t.Logf("RRD = %.2f ms", rrd)
	require.Greater(t, rrd, 0.0, "RRD should be greater than 0 after successful registrations")
}

func getRRD(t *testing.T, endpoint string) float64 {
	t.Helper()

	sum := getMetric(t, endpoint, "sip_exporter_rrd_sum")
	count := getMetric(t, endpoint, "sip_exporter_rrd_count")
	if count == 0 {
		return 0
	}

	return sum / count
}

func TestRRD_Register401(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	rrdBefore := getRRD(t, env.endpoint)

	runSippScenario(ctx, t, "reg_uas_401.xml", "reg_uac_401.xml", 20, env)

	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")
	t.Logf("sip_exporter_register_total = %.0f", registerTotal)
	require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

	status401Total := getMetric(t, env.endpoint, "sip_exporter_401_total")
	t.Logf("sip_exporter_401_total = %.0f", status401Total)
	require.Greater(t, status401Total, 0.0, "should have 401 responses")

	rrdAfter := getRRD(t, env.endpoint)
	t.Logf("RRD before = %.2f ms, after = %.2f ms", rrdBefore, rrdAfter)
	require.Equal(t, rrdBefore, rrdAfter, "RRD should not change for 401 responses")
}

func TestRRD_Register403(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	rrdBefore := getRRD(t, env.endpoint)

	runSippScenario(ctx, t, "reg_uas_403.xml", "reg_uac_403.xml", 20, env)

	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")
	require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

	status403Total := getMetric(t, env.endpoint, "sip_exporter_403_total")
	require.Greater(t, status403Total, 0.0, "should have 403 responses")

	rrdAfter := getRRD(t, env.endpoint)
	require.Equal(t, rrdBefore, rrdAfter, "RRD should not change for 403 responses")
}

func TestRRD_Register500(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	rrdBefore := getRRD(t, env.endpoint)

	runSippScenario(ctx, t, "reg_uas_500.xml", "reg_uac_500.xml", 20, env)

	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")
	require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

	status500Total := getMetric(t, env.endpoint, "sip_exporter_500_total")
	require.Greater(t, status500Total, 0.0, "should have 500 responses")

	rrdAfter := getRRD(t, env.endpoint)
	require.Equal(t, rrdBefore, rrdAfter, "RRD should not change for 500 responses")
}

func TestRRD_RegisterTimeout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	rrdBefore := getRRD(t, env.endpoint)
	registerBefore := getMetric(t, env.endpoint, "sip_exporter_register_total")

	runSippUACOnly(ctx, t, "reg_uac.xml", 5, env)

	registerAfter := getMetric(t, env.endpoint, "sip_exporter_register_total")
	require.Greater(t, registerAfter, registerBefore, "should have REGISTER requests")

	rrdAfter := getRRD(t, env.endpoint)
	require.Equal(t, rrdBefore, rrdAfter, "RRD should not change for timeout (no response)")
}

func TestRRD_ConcurrentRegistrations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", 100, env)

	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")
	t.Logf("sip_exporter_register_total = %.0f", registerTotal)
	require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

	rrd := getRRD(t, env.endpoint)
	t.Logf("RRD = %.2f ms", rrd)
	require.Greater(t, rrd, 0.0, "RRD should be measured for all concurrent registrations")
}

func TestSER_ConcurrentRequests(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 30, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 10, env)
	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 10, env)

	ser := getMetric(t, env.endpoint, "sip_exporter_ser")
	t.Logf("SER = %.2f%%", ser)
	require.Greater(t, ser, 0.0, "SER should be calculated")
	require.LessOrEqual(t, ser, 100.0, "SER should not exceed 100%")
}
