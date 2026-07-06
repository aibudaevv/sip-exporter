//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReinvite_CountedSeparately(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const callCount = 10

	runSippScenario(ctx, t, "uas_reinvite.xml", "uac_reinvite.xml", callCount, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	reinviteTotal := getMetric(t, env.endpoint, "sip_exporter_reinvite_total")
	invite200Total := getMetric(t, env.endpoint, "sip_exporter_invite_200_total")
	ackTotal := getMetric(t, env.endpoint, "sip_exporter_ack_total")
	byeTotal := getMetric(t, env.endpoint, "sip_exporter_bye_total")

	t.Logf("invite_total=%.0f, reinvite_total=%.0f, invite_200_total=%.0f, ack_total=%.0f, bye_total=%.0f",
		inviteTotal, reinviteTotal, invite200Total, ackTotal, byeTotal)

	require.Equal(t, float64(callCount), inviteTotal, "initial INVITEs only")
	require.Equal(t, float64(callCount), reinviteTotal, "one re-INVITE per call")
	require.Equal(t, float64(callCount), invite200Total, "200 OK to initial INVITE only")
	require.Equal(t, float64(callCount*2), ackTotal, "2 ACKs per call")
	require.Equal(t, float64(callCount), byeTotal, "1 BYE per call")

	waitForSessionsZero(t, env.endpoint)
}

func TestReinvite_DoesNotContaminateSER(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const callCount = 10

	runSippScenario(ctx, t, "uas_reinvite.xml", "uac_reinvite.xml", callCount, env)

	ser := getMetric(t, env.endpoint, "sip_exporter_ser")
	require.InDelta(t, 100.0, ser, ratioDelta,
		"SER should be 100%% — re-INVITEs excluded from inviteTotal denominator")

	scr := getMetric(t, env.endpoint, "sip_exporter_scr")
	require.InDelta(t, 100.0, scr, ratioDelta,
		"SCR should be 100%% — re-INVITEs excluded from inviteTotal denominator")

	waitForSessionsZero(t, env.endpoint)
}
