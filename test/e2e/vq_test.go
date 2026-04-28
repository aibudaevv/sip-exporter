//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVQ_PUBLISH_SessionReport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	callCount := 5
	runSippScenario(ctx, t, "uas_vq_publish.xml", "uac_vq_publish.xml", callCount, env)

	expectedTotal := float64(callCount * 2)

	reportsTotal := getMetric(t, env.endpoint, "sip_exporter_vq_reports_total")
	t.Logf("vq_reports_total = %.0f (want %.0f, loopback x2)", reportsTotal, expectedTotal)
	require.Equal(t, expectedTotal, reportsTotal)

	nlrCount := getMetric(t, env.endpoint, "sip_exporter_vq_nlr_percent_count")
	t.Logf("vq_nlr_percent_count = %.0f", nlrCount)
	require.Equal(t, expectedTotal, nlrCount)

	jdrCount := getMetric(t, env.endpoint, "sip_exporter_vq_jdr_percent_count")
	require.Equal(t, expectedTotal, jdrCount)

	moslqCount := getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_count")
	require.Equal(t, expectedTotal, moslqCount)

	rerlCount := getMetric(t, env.endpoint, "sip_exporter_vq_rerl_db_count")
	require.Equal(t, expectedTotal, rerlCount)

	nlrSum := getMetric(t, env.endpoint, "sip_exporter_vq_nlr_percent_sum")
	expectedNLRSpec := expectedTotal * 0.50
	t.Logf("vq_nlr_percent_sum = %.4f (want ~%.4f)", nlrSum, expectedNLRSpec)
	require.InDelta(t, expectedNLRSpec, nlrSum, 0.01)

	moslqSum := getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_sum")
	expectedMOSLQ := expectedTotal * 4.5
	t.Logf("vq_mos_lq_sum = %.4f (want ~%.4f)", moslqSum, expectedMOSLQ)
	require.InDelta(t, expectedMOSLQ, moslqSum, 0.01)

	rerlSum := getMetric(t, env.endpoint, "sip_exporter_vq_rerl_db_sum")
	expectedRERL := expectedTotal * 55.0
	t.Logf("vq_rerl_db_sum = %.4f (want ~%.4f)", rerlSum, expectedRERL)
	require.InDelta(t, expectedRERL, rerlSum, 0.01)

	publishTotal := getMetric(t, env.endpoint, "sip_exporter_publish_total")
	t.Logf("publish_total = %.0f (want %.0f, loopback x2)", publishTotal, expectedTotal)
	require.Equal(t, expectedTotal, publishTotal)
}

func TestVQ_NOTIFY_SessionReport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	callCount := 5
	runSippScenario(ctx, t, "uas_vq_notify.xml", "uac_vq_notify.xml", callCount, env)

	expectedTotal := float64(callCount * 2)

	reportsTotal := getMetric(t, env.endpoint, "sip_exporter_vq_reports_total")
	t.Logf("vq_reports_total = %.0f (want %.0f, loopback x2)", reportsTotal, expectedTotal)
	require.Equal(t, expectedTotal, reportsTotal)

	nlrCount := getMetric(t, env.endpoint, "sip_exporter_vq_nlr_percent_count")
	require.Equal(t, expectedTotal, nlrCount)

	moslqCount := getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_count")
	require.Equal(t, expectedTotal, moslqCount)

	moslqSum := getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_sum")
	expectedMOSLQ := expectedTotal * 4.5
	t.Logf("vq_mos_lq_sum = %.4f (want ~%.4f)", moslqSum, expectedMOSLQ)
	require.InDelta(t, expectedMOSLQ, moslqSum, 0.01)

	notifyTotal := getMetric(t, env.endpoint, "sip_exporter_notify_total")
	t.Logf("notify_total = %.0f (want %.0f, loopback x2)", notifyTotal, expectedTotal)
	require.Equal(t, expectedTotal, notifyTotal)
}

func TestVQ_PUBLISH_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	callCount := 5
	runSippScenario(ctx, t, "uas_vq_publish.xml", "uac_vq_publish.xml", callCount, env)

	expectedTotal := float64(callCount * 2)

	reportsTotal := getMetricWithCarrier(t, env.endpoint, "sip_exporter_vq_reports_total", env.carrier)
	t.Logf("vq_reports_total{carrier=%q} = %.0f (want %.0f, loopback x2)", env.carrier, reportsTotal, expectedTotal)
	require.Equal(t, expectedTotal, reportsTotal)

	moslqCount := getMetricWithCarrier(t, env.endpoint, "sip_exporter_vq_mos_lq_count", env.carrier)
	require.Equal(t, expectedTotal, moslqCount)
}
