//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMultiCodecEarlyMediaSuccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestEnv(ctx, t)

	const callCount = 10

	runSippScenario(ctx, t, "uas_multicodec.xml", "uac_multicodec.xml", callCount, env)

	for _, metric := range []string{
		"sip_exporter_invite_total",
		"sip_exporter_invite_200_total",
		"sip_exporter_183_total",
		"sip_exporter_ack_total",
		"sip_exporter_bye_total",
		"sip_exporter_ser",
	} {
		require.True(t, metricExists(t, env.endpoint, metric), "%s should exist", metric)
	}
	require.False(t, metricExists(t, env.endpoint, "sip_exporter_parse_errors_total"),
		"parse errors metric should be absent after valid multi-codec SDP")

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	invite200Total := getMetric(t, env.endpoint, "sip_exporter_invite_200_total")
	status183 := getMetric(t, env.endpoint, "sip_exporter_183_total")
	ackTotal := getMetric(t, env.endpoint, "sip_exporter_ack_total")
	byeTotal := getMetric(t, env.endpoint, "sip_exporter_bye_total")

	t.Logf("invite=%.0f, invite_200=%.0f, 183=%.0f, ack=%.0f, bye=%.0f",
		inviteTotal, invite200Total, status183, ackTotal, byeTotal)

	require.Equal(t, float64(callCount), inviteTotal, "initial INVITEs")
	require.Equal(t, float64(callCount), invite200Total, "200 OK responses")
	require.Equal(t, float64(callCount), status183, "183 Session Progress responses")
	require.Equal(t, float64(callCount), ackTotal, "ACK requests")
	require.Equal(t, float64(callCount), byeTotal, "BYE requests")
	ser := getMetric(t, env.endpoint, "sip_exporter_ser")
	require.InDelta(t, 100.0, ser, ratioDelta, "SER should be 100%%")

	waitForSessionsZero(t, env.endpoint)
}
