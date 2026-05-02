//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUA_YealinkClassified(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnvWithUAConfig(ctx, t, "user_agents.yaml")

	env.restart(t)
	runSippScenario(ctx, t, "uas_yealink.xml", "uac_yealink.xml", 50, &env.testEnv)

	inviteTotal := getMetricWithUA(t, env.endpoint, "sip_exporter_invite_total", "yealink")
	t.Logf("invite_total{ua_type=yealink} = %.0f", inviteTotal)
	require.Equal(t, float64(50), inviteTotal)

	ser := getMetricWithUA(t, env.endpoint, "sip_exporter_ser", "yealink")
	t.Logf("SER{ua_type=yealink} = %.2f", ser)
	require.Equal(t, 100.0, ser)

	scr := getMetricWithUA(t, env.endpoint, "sip_exporter_scr", "yealink")
	t.Logf("SCR{ua_type=yealink} = %.2f", scr)
	require.Equal(t, 100.0, scr)

	waitForSessionsZero(t, env.endpoint)
}

func TestUA_GrandstreamClassified(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnvWithUAConfig(ctx, t, "user_agents.yaml")

	env.restart(t)
	runSippScenario(ctx, t, "uas_grandstream.xml", "uac_grandstream.xml", 50, &env.testEnv)

	inviteTotal := getMetricWithUA(t, env.endpoint, "sip_exporter_invite_total", "grandstream")
	t.Logf("invite_total{ua_type=grandstream} = %.0f", inviteTotal)
	require.Equal(t, float64(50), inviteTotal)

	ser := getMetricWithUA(t, env.endpoint, "sip_exporter_ser", "grandstream")
	t.Logf("SER{ua_type=grandstream} = %.2f", ser)
	require.Equal(t, 100.0, ser)

	waitForSessionsZero(t, env.endpoint)
}

func TestUA_MultipleTypesIsolated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnvWithUAConfig(ctx, t, "user_agents.yaml")

	env.restart(t)
	runSippScenario(ctx, t, "uas_yealink.xml", "uac_yealink.xml", 50, &env.testEnv)
	runSippScenario(ctx, t, "uas_grandstream.xml", "uac_grandstream.xml", 50, &env.testEnv)

	yealinkInvite := getMetricWithUA(t, env.endpoint, "sip_exporter_invite_total", "yealink")
	grandstreamInvite := getMetricWithUA(t, env.endpoint, "sip_exporter_invite_total", "grandstream")

	t.Logf("invite_total{yealink} = %.0f, {grandstream} = %.0f", yealinkInvite, grandstreamInvite)
	require.Equal(t, float64(50), yealinkInvite)
	require.Equal(t, float64(50), grandstreamInvite)

	yealinkSER := getMetricWithUA(t, env.endpoint, "sip_exporter_ser", "yealink")
	grandstreamSER := getMetricWithUA(t, env.endpoint, "sip_exporter_ser", "grandstream")

	t.Logf("SER{yealink} = %.2f, {grandstream} = %.2f", yealinkSER, grandstreamSER)
	require.Equal(t, 100.0, yealinkSER)
	require.Equal(t, 100.0, grandstreamSER)

	waitForSessionsZero(t, env.endpoint)
}

func TestUA_OtherWhenNoUAHeader(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnvWithUAConfig(ctx, t, "user_agents.yaml")

	env.restart(t)
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 50, &env.testEnv)

	inviteTotal := getMetricWithUA(t, env.endpoint, "sip_exporter_invite_total", "other")
	t.Logf("invite_total{ua_type=other} = %.0f", inviteTotal)
	require.Equal(t, float64(50), inviteTotal)

	yealinkInvite := getMetricWithUA(t, env.endpoint, "sip_exporter_invite_total", "yealink")
	require.Equal(t, float64(0), yealinkInvite, "no Yealink traffic")

	waitForSessionsZero(t, env.endpoint)
}

func TestUA_NoConfigAllOther(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

	env.restart(t)
	runSippScenario(ctx, t, "uas_yealink.xml", "uac_yealink.xml", 50, &env.testEnv)

	inviteTotal := getMetricWithUA(t, env.endpoint, "sip_exporter_invite_total", "other")
	t.Logf("invite_total{ua_type=other} = %.0f (no UA config)", inviteTotal)
	require.Equal(t, float64(50), inviteTotal)

	yealinkInvite := getMetricWithUA(t, env.endpoint, "sip_exporter_invite_total", "yealink")
	require.Equal(t, float64(0), yealinkInvite, "no config → no yealink labels")

	waitForSessionsZero(t, env.endpoint)
}

func TestUA_SDC_ByUAType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnvWithUAConfig(ctx, t, "user_agents.yaml")

	env.restart(t)
	runSippScenario(ctx, t, "uas_yealink.xml", "uac_yealink.xml", 50, &env.testEnv)

	sdc := getMetricWithUA(t, env.endpoint, "sip_exporter_sdc_total", "yealink")
	t.Logf("sdc_total{ua_type=yealink} = %.0f", sdc)
	require.Equal(t, float64(50), sdc, "SDC = completed sessions")

	sdcOther := getMetricWithUA(t, env.endpoint, "sip_exporter_sdc_total", "other")
	require.Equal(t, float64(0), sdcOther, "no other traffic")

	waitForSessionsZero(t, env.endpoint)
}

func TestUA_RatedMetricsByUAType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnvWithUAConfig(ctx, t, "user_agents.yaml")

	env.restart(t)
	runSippScenario(ctx, t, "uas_yealink.xml", "uac_yealink.xml", 50, &env.testEnv)

	seer := getMetricWithUA(t, env.endpoint, "sip_exporter_seer", "yealink")
	asr := getMetricWithUA(t, env.endpoint, "sip_exporter_asr", "yealink")
	ner := getMetricWithUA(t, env.endpoint, "sip_exporter_ner", "yealink")

	t.Logf("SEER{yealink}=%.2f ASR{yealink}=%.2f NER{yealink}=%.2f", seer, asr, ner)
	require.Equal(t, 100.0, seer)
	require.Equal(t, 100.0, asr)
	require.Equal(t, 100.0, ner)

	waitForSessionsZero(t, env.endpoint)
}

func TestUA_CarrierAndUALabelsCombined(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	carriersYAML := loadCarriersYAML(t, "carriers.yaml")
	env := newSharedTestEnvWithCarrierAndUA(ctx, t, carriersYAML, "loopback-carrier", "user_agents.yaml")

	env.restart(t)
	runSippScenario(ctx, t, "uas_yealink.xml", "uac_yealink.xml", 50, &env.testEnv)

	inviteCarrierUA := getMetricWithCarrierAndUA(t, env.endpoint, "sip_exporter_invite_total", "loopback-carrier", "yealink")
	t.Logf("invite_total{carrier=loopback-carrier,ua_type=yealink} = %.0f", inviteCarrierUA)
	require.Equal(t, float64(50), inviteCarrierUA)

	serCarrierUA := getMetricWithCarrierAndUA(t, env.endpoint, "sip_exporter_ser", "loopback-carrier", "yealink")
	t.Logf("SER{carrier=loopback-carrier,ua_type=yealink} = %.2f", serCarrierUA)
	require.Equal(t, 100.0, serCarrierUA)

	sdcCarrierUA := getMetricWithCarrierAndUA(t, env.endpoint, "sip_exporter_sdc_total", "loopback-carrier", "yealink")
	t.Logf("sdc_total{carrier=loopback-carrier,ua_type=yealink} = %.0f", sdcCarrierUA)
	require.Equal(t, float64(50), sdcCarrierUA)

	inviteNoCarrier := getMetricWithUA(t, env.endpoint, "sip_exporter_invite_total", "yealink")
	t.Logf("invite_total{ua_type=yealink} (any carrier) = %.0f", inviteNoCarrier)
	require.Equal(t, inviteCarrierUA, inviteNoCarrier, "all traffic from loopback-carrier, totals must match")

	waitForSessionsZero(t, env.endpoint)
}
