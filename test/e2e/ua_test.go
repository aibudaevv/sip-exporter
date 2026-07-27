//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUA(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const callCount = 50

	type sippRun struct {
		uas, uac string
	}

	tests := []struct {
		name   string
		uaYAML string // "" = no UA config
		runs   []sippRun
		check  func(t *testing.T, endpoint string)
	}{
		{
			"YealinkClassified", "user_agents.yaml",
			[]sippRun{{"uas_yealink.xml", "uac_yealink.xml"}},
			func(t *testing.T, ep string) {
				inviteTotal := getMetricWithUA(t, ep, "sip_exporter_invite_total", "yealink")
				require.Equal(t, float64(callCount), inviteTotal)
				ser := getMetricWithUA(t, ep, "sip_exporter_ser", "yealink")
				require.InDelta(t, 100.0, ser, ratioDelta)
				scr := getMetricWithUA(t, ep, "sip_exporter_scr", "yealink")
				require.InDelta(t, 100.0, scr, ratioDelta)
			},
		},
		{
			"GrandstreamClassified", "user_agents.yaml",
			[]sippRun{{"uas_grandstream.xml", "uac_grandstream.xml"}},
			func(t *testing.T, ep string) {
				inviteTotal := getMetricWithUA(t, ep, "sip_exporter_invite_total", "grandstream")
				require.Equal(t, float64(callCount), inviteTotal)
				ser := getMetricWithUA(t, ep, "sip_exporter_ser", "grandstream")
				require.InDelta(t, 100.0, ser, ratioDelta)
			},
		},
		{
			"MultipleTypesIsolated", "user_agents.yaml",
			[]sippRun{
				{"uas_yealink.xml", "uac_yealink.xml"},
				{"uas_grandstream.xml", "uac_grandstream.xml"},
			},
			func(t *testing.T, ep string) {
				yealinkInvite := getMetricWithUA(t, ep, "sip_exporter_invite_total", "yealink")
				grandstreamInvite := getMetricWithUA(t, ep, "sip_exporter_invite_total", "grandstream")
				require.Equal(t, float64(callCount), yealinkInvite)
				require.Equal(t, float64(callCount), grandstreamInvite)

				yealinkSER := getMetricWithUA(t, ep, "sip_exporter_ser", "yealink")
				grandstreamSER := getMetricWithUA(t, ep, "sip_exporter_ser", "grandstream")
				require.InDelta(t, 100.0, yealinkSER, ratioDelta)
				require.InDelta(t, 100.0, grandstreamSER, ratioDelta)
			},
		},
		{
			"OtherWhenNoUAHeader", "user_agents.yaml",
			[]sippRun{{"uas_100.xml", "uac_100.xml"}},
			func(t *testing.T, ep string) {
				inviteTotal := getMetricWithUA(t, ep, "sip_exporter_invite_total", "other")
				require.Equal(t, float64(callCount), inviteTotal)

				require.False(t,
					metricWithLabelExists(t, ep, "sip_exporter_invite_total", `ua_type="yealink"`),
					"no Yealink traffic")
			},
		},
		{
			"NoConfigAllOther", "",
			[]sippRun{{"uas_yealink.xml", "uac_yealink.xml"}},
			func(t *testing.T, ep string) {
				inviteTotal := getMetricWithUA(t, ep, "sip_exporter_invite_total", "other")
				require.Equal(t, float64(callCount), inviteTotal)

				require.False(t,
					metricWithLabelExists(t, ep, "sip_exporter_invite_total", `ua_type="yealink"`),
					"no config → no yealink labels")
			},
		},
		{
			"SDC_ByUAType", "user_agents.yaml",
			[]sippRun{{"uas_yealink.xml", "uac_yealink.xml"}},
			func(t *testing.T, ep string) {
				sdc := getMetricWithUA(t, ep, "sip_exporter_sdc_total", "yealink")
				require.Equal(t, float64(callCount), sdc)

				require.False(t,
					metricWithLabelExists(t, ep, "sip_exporter_sdc_total", `ua_type="other"`),
					"no other traffic")
			},
		},
		{
			"RatedMetricsByUAType", "user_agents.yaml",
			[]sippRun{{"uas_yealink.xml", "uac_yealink.xml"}},
			func(t *testing.T, ep string) {
				seer := getMetricWithUA(t, ep, "sip_exporter_seer", "yealink")
				asr := getMetricWithUA(t, ep, "sip_exporter_asr", "yealink")
				ner := getMetricWithUA(t, ep, "sip_exporter_ner", "yealink")
				require.InDelta(t, 100.0, seer, ratioDelta)
				require.InDelta(t, 100.0, asr, ratioDelta)
				require.InDelta(t, 100.0, ner, ratioDelta)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var env *testEnv
			if tt.uaYAML != "" {
				env = newTestEnvWithUAConfig(ctx, t, tt.uaYAML)
			} else {
				env = newTestEnv(ctx, t)
			}

			for _, r := range tt.runs {
				runSippScenario(ctx, t, r.uas, r.uac, callCount, env)
			}

			tt.check(t, env.endpoint)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

func TestUACarrierAndUALabelsCombined(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const callCount = 50
	carriersYAML := loadCarriersYAML(t, "carriers.yaml")
	env := newTestEnvWithCarrierAndUA(ctx, t, carriersYAML, "loopback-carrier", "user_agents.yaml")

	runSippScenario(ctx, t, "uas_yealink.xml", "uac_yealink.xml", callCount, env)

	inviteCarrierUA := getMetricWithCarrierAndUA(t, env.endpoint, "sip_exporter_invite_total", "loopback-carrier", "yealink")
	t.Logf("invite_total{carrier=loopback-carrier,ua_type=yealink} = %.0f", inviteCarrierUA)
	require.Equal(t, float64(callCount), inviteCarrierUA)

	serCarrierUA := getMetricWithCarrierAndUA(t, env.endpoint, "sip_exporter_ser", "loopback-carrier", "yealink")
	require.InDelta(t, 100.0, serCarrierUA, ratioDelta)

	sdcCarrierUA := getMetricWithCarrierAndUA(t, env.endpoint, "sip_exporter_sdc_total", "loopback-carrier", "yealink")
	require.Equal(t, float64(callCount), sdcCarrierUA)

	inviteNoCarrier := getMetricWithUA(t, env.endpoint, "sip_exporter_invite_total", "yealink")
	require.Equal(t, inviteCarrierUA, inviteNoCarrier, "all traffic from loopback-carrier, totals must match")

	waitForSessionsZero(t, env.endpoint)
}
