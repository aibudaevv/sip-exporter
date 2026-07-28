//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVQSessionReports(t *testing.T) {
	t.Parallel()

	const callCount = 5
	expectedTotal := float64(callCount)

	tests := []struct {
		name    string
		carrier bool
		uas     string
		uac     string
		check   func(t *testing.T, endpoint, carrier string)
	}{
		{
			"PUBLISH", false, "uas_vq_publish.xml", "uac_vq_publish.xml",
			func(t *testing.T, endpoint, _ string) {
				for _, m := range []string{
					"sip_exporter_vq_nlr_percent_count",
					"sip_exporter_vq_jdr_percent_count",
					"sip_exporter_vq_mos_lq_count",
					"sip_exporter_vq_rerl_db_count",
				} {
					require.True(t, metricExists(t, endpoint, m), "%s should exist", m)
				}

				require.Equal(t, expectedTotal, getMetric(t, endpoint, "sip_exporter_vq_nlr_percent_count"))
				require.Equal(t, expectedTotal, getMetric(t, endpoint, "sip_exporter_vq_jdr_percent_count"))
				require.Equal(t, expectedTotal, getMetric(t, endpoint, "sip_exporter_vq_mos_lq_count"))
				require.Equal(t, expectedTotal, getMetric(t, endpoint, "sip_exporter_vq_rerl_db_count"))

				nlrSum := getMetric(t, endpoint, "sip_exporter_vq_nlr_percent_sum")
				require.InDelta(t, expectedTotal*0.50, nlrSum, 0.01)

				moslqSum := getMetric(t, endpoint, "sip_exporter_vq_mos_lq_sum")
				require.InDelta(t, expectedTotal*4.5, moslqSum, 0.01)

				rerlSum := getMetric(t, endpoint, "sip_exporter_vq_rerl_db_sum")
				require.InDelta(t, expectedTotal*55.0, rerlSum, 0.01)

				require.Equal(t, expectedTotal, getMetric(t, endpoint, "sip_exporter_publish_total"))
			},
		},
		{
			"NOTIFY", false, "uas_vq_notify.xml", "uac_vq_notify.xml",
			func(t *testing.T, endpoint, _ string) {
				require.True(t, metricExists(t, endpoint, "sip_exporter_vq_nlr_percent_count"))
				require.Equal(t, expectedTotal, getMetric(t, endpoint, "sip_exporter_vq_nlr_percent_count"))
				require.Equal(t, expectedTotal, getMetric(t, endpoint, "sip_exporter_vq_mos_lq_count"))

				moslqSum := getMetric(t, endpoint, "sip_exporter_vq_mos_lq_sum")
				require.InDelta(t, expectedTotal*4.5, moslqSum, 0.01)

				require.Equal(t, expectedTotal, getMetric(t, endpoint, "sip_exporter_notify_total"))
			},
		},
		{
			"PUBLISH_WithCarrierConfig", true, "uas_vq_publish.xml", "uac_vq_publish.xml",
			func(t *testing.T, endpoint, carrier string) {
				carrierLabel := `carrier="` + carrier + `"`
				require.True(t, metricWithLabelExists(t, endpoint, "sip_exporter_vq_reports_total", carrierLabel),
					"vq_reports_total should exist for carrier %q", carrier)
				require.True(t, metricWithLabelExists(t, endpoint, "sip_exporter_vq_mos_lq_count", carrierLabel),
					"vq_mos_lq_count should exist for carrier %q", carrier)

				reportsTotal := getMetricWithCarrier(t, endpoint, "sip_exporter_vq_reports_total", carrier)
				require.Equal(t, expectedTotal, reportsTotal)

				moslqCount := getMetricWithCarrier(t, endpoint, "sip_exporter_vq_mos_lq_count", carrier)
				require.Equal(t, expectedTotal, moslqCount)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			var env *testEnv
			if tt.carrier {
				env = newTestEnvWithCarriers(ctx, t)
			} else {
				env = newTestEnv(ctx, t)
			}

			runSippScenario(ctx, t, tt.uas, tt.uac, callCount, env)

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_vq_reports_total"),
				"vq_reports_total should exist")
			reportsTotal := getMetric(t, env.endpoint, "sip_exporter_vq_reports_total")
			t.Logf("vq_reports_total = %.0f (want %.0f)", reportsTotal, expectedTotal)
			require.Equal(t, expectedTotal, reportsTotal)

			tt.check(t, env.endpoint, env.carrier)

			assertSelfMonitoringHealthy(t, env.endpoint)
		})
	}
}

func TestVQMultipleVendors(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestEnv(ctx, t)

	yealinkCalls := 3
	ciscoCalls := 3
	grandstreamCalls := 3
	totalReports := float64(yealinkCalls + ciscoCalls + grandstreamCalls)

	runSippScenario(ctx, t, "uas_vq_publish.xml", "uac_vq_yealink_publish.xml", yealinkCalls, env)
	runSippScenario(ctx, t, "uas_vq_notify.xml", "uac_vq_cisco_notify.xml", ciscoCalls, env)
	runSippScenario(ctx, t, "uas_vq_publish.xml", "uac_vq_grandstream_publish.xml", grandstreamCalls, env)

	reportsTotal := getMetric(t, env.endpoint, "sip_exporter_vq_reports_total")
	t.Logf("vq_reports_total = %.0f (want %.0f)", reportsTotal, totalReports)
	require.Equal(t, totalReports, reportsTotal)

	moslqCount := getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_count")
	require.Equal(t, totalReports, moslqCount)

	moslqSum := getMetric(t, env.endpoint, "sip_exporter_vq_mos_lq_sum")
	yealinkExpected := float64(yealinkCalls)
	ciscoExpected := float64(ciscoCalls)
	grandstreamExpected := float64(grandstreamCalls)
	expectedMOSLQSum := yealinkExpected*3.8 + ciscoExpected*4.1 + grandstreamExpected*4.3
	t.Logf("vq_mos_lq_sum = %.4f (want ~%.4f)", moslqSum, expectedMOSLQSum)
	require.InDelta(t, expectedMOSLQSum, moslqSum, 0.05)

	nlrCount := getMetric(t, env.endpoint, "sip_exporter_vq_nlr_percent_count")
	expectedNLRCount := yealinkExpected + grandstreamExpected
	t.Logf("vq_nlr_percent_count = %.0f (want %.0f)", nlrCount, expectedNLRCount)
	require.Equal(t, expectedNLRCount, nlrCount)

	nlrSum := getMetric(t, env.endpoint, "sip_exporter_vq_nlr_percent_sum")
	expectedNLRSpecSum := yealinkExpected*1.20 + grandstreamExpected*0.30
	t.Logf("vq_nlr_percent_sum = %.4f (want ~%.4f)", nlrSum, expectedNLRSpecSum)
	require.InDelta(t, expectedNLRSpecSum, nlrSum, 0.05)

	assertSelfMonitoringHealthy(t, env.endpoint)
}

func TestVQPartialReport(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestEnv(ctx, t)

	const callCount = 5
	runSippScenario(ctx, t, "uas_vq_notify.xml", "uac_vq_cisco_notify.xml", callCount, env)

	expectedTotal := float64(callCount)

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
		require.False(t, metricExists(t, env.endpoint, metric),
			"partial report should not have %s", metric)
	}

	assertSelfMonitoringHealthy(t, env.endpoint)
}

func TestVQMalformedReport(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestEnv(ctx, t)

	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	const callCount = 3
	runSippScenario(ctx, t, "uas_vq_publish.xml", "uac_vq_malformed_publish.xml", callCount, env)

	require.False(t, metricExists(t, env.endpoint, "sip_exporter_vq_reports_total"),
		"vq_reports_total should be absent (malformed reports not counted)")

	errorsAfter := getMetric(t, env.endpoint, "sip_exporter_system_error_total")
	errorCount := errorsAfter - errorsBefore
	t.Logf("system_error_total delta = %.0f (want >= %d)", errorCount, callCount)
	require.GreaterOrEqual(t, errorCount, float64(callCount))

	publishTotal := getMetric(t, env.endpoint, "sip_exporter_publish_total")
	expectedPublish := float64(callCount)
	t.Logf("publish_total = %.0f (want %.0f)", publishTotal, expectedPublish)
	require.Equal(t, expectedPublish, publishTotal)

	assertSelfMonitoringHealthy(t, env.endpoint)
}
