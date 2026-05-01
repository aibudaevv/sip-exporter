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

func TestVQ_MultipleVendors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	yealinkCalls := 3
	ciscoCalls := 3
	grandstreamCalls := 3
	totalReports := float64((yealinkCalls + ciscoCalls + grandstreamCalls) * 2)

	runSippScenario(ctx, t, "uas_vq_publish.xml", "uac_vq_yealink_publish.xml", yealinkCalls, env)

	runSippScenario(ctx, t, "uas_vq_notify.xml", "uac_vq_cisco_notify.xml", ciscoCalls, env)

	runSippScenario(ctx, t, "uas_vq_publish.xml", "uac_vq_grandstream_publish.xml", grandstreamCalls, env)

	reportsTotal := getMetric(t, env.endpoint, "sip_exporter_vq_reports_total")
	t.Logf("vq_reports_total = %.0f (want %.0f, total across all vendors, loopback x2)", reportsTotal, totalReports)
	require.Equal(t, totalReports, reportsTotal)

	moslqCount := getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_count")
	require.Equal(t, totalReports, moslqCount)

	moslqSum := getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_sum")
	yealinkExpected := float64(yealinkCalls * 2)
	ciscoExpected := float64(ciscoCalls * 2)
	grandstreamExpected := float64(grandstreamCalls * 2)
	expectedMOSLQSum := yealinkExpected*3.8 + ciscoExpected*4.1 + grandstreamExpected*4.3
	t.Logf("vq_mos_lq_sum = %.4f (want ~%.4f)", moslqSum, expectedMOSLQSum)
	require.InDelta(t, expectedMOSLQSum, moslqSum, 0.05)

	nlrCount := getMetric(t, env.endpoint, "sip_exporter_vq_nlr_percent_count")
	expectedNLRCount := yealinkExpected + grandstreamExpected
	t.Logf("vq_nlr_percent_count = %.0f (want %.0f, Yealink+Grandstream only, Cisco partial has no NLR)", nlrCount, expectedNLRCount)
	require.Equal(t, expectedNLRCount, nlrCount)

	nlrSum := getMetric(t, env.endpoint, "sip_exporter_vq_nlr_percent_sum")
	expectedNLRSpecSum := yealinkExpected*1.20 + grandstreamExpected*0.30
	t.Logf("vq_nlr_percent_sum = %.4f (want ~%.4f)", nlrSum, expectedNLRSpecSum)
	require.InDelta(t, expectedNLRSpecSum, nlrSum, 0.05)
}

func TestVQ_PartialReport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	callCount := 5
	runSippScenario(ctx, t, "uas_vq_notify.xml", "uac_vq_cisco_notify.xml", callCount, env)

	expectedTotal := float64(callCount * 2)

	reportsTotal := getMetric(t, env.endpoint, "sip_exporter_vq_reports_total")
	t.Logf("vq_reports_total = %.0f (want %.0f)", reportsTotal, expectedTotal)
	require.Equal(t, expectedTotal, reportsTotal)

	moslqCount := getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_count")
	require.Equal(t, expectedTotal, moslqCount)

	rlqCount := getMetric(t, env.endpoint, "sip_exporter_vq_rlq_count")
	require.Equal(t, expectedTotal, rlqCount)

	moslqSum := getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_sum")
	t.Logf("vq_mos_lq_sum = %.4f (want ~%.4f)", moslqSum, expectedTotal*4.1)
	require.InDelta(t, expectedTotal*4.1, moslqSum, 0.01)

	rlqSum := getMetric(t, env.endpoint, "sip_exporter_vq_rlq_sum")
	t.Logf("vq_rlq_sum = %.4f (want ~%.4f)", rlqSum, expectedTotal*85.0)
	require.InDelta(t, expectedTotal*85.0, rlqSum, 0.01)

	for _, metric := range []string{
		"sip_exporter_vq_nlr_percent_count",
		"sip_exporter_vq_jdr_percent_count",
		"sip_exporter_vq_bld_percent_count",
		"sip_exporter_vq_gld_percent_count",
		"sip_exporter_vq_rtd_ms_count",
		"sip_exporter_vq_esd_ms_count",
		"sip_exporter_vq_iaj_ms_count",
		"sip_exporter_vq_maj_ms_count",
		"sip_exporter_vq_mos_cq_count",
		"sip_exporter_vq_rcq_count",
		"sip_exporter_vq_rerl_db_count",
	} {
		val := getMetric(t, env.endpoint, metric)
		require.Equal(t, float64(0), val, "partial report should not have %s", metric)
	}
}

func TestVQ_MalformedReport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	callCount := 3
	runSippScenario(ctx, t, "uas_vq_publish.xml", "uac_vq_malformed_publish.xml", callCount, env)

	reportsTotal := getMetric(t, env.endpoint, "sip_exporter_vq_reports_total")
	t.Logf("vq_reports_total = %.0f (want 0, malformed reports should not increment)", reportsTotal)
	require.Equal(t, float64(0), reportsTotal)

	errorsAfter := getMetric(t, env.endpoint, "sip_exporter_system_error_total")
	errorCount := errorsAfter - errorsBefore
	t.Logf("system_error_total delta = %.0f (want >= %.0f)", errorCount, float64(callCount*2))
	require.GreaterOrEqual(t, errorCount, float64(callCount*2))

	publishTotal := getMetric(t, env.endpoint, "sip_exporter_publish_total")
	expectedPublish := float64(callCount * 2)
	t.Logf("publish_total = %.0f (want %.0f, PUBLISH packets still counted)", publishTotal, expectedPublish)
	require.Equal(t, expectedPublish, publishTotal)
}
