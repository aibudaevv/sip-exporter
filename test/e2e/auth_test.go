//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthCompletion_401ChallengeThenSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const callCount = 10

	runSippScenario(ctx, t, "uas_auth_401.xml", "uac_auth_401.xml", callCount, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	invite200Total := getMetric(t, env.endpoint, "sip_exporter_invite_200_total")
	status401 := getMetric(t, env.endpoint, "sip_exporter_401_total")
	ackTotal := getMetric(t, env.endpoint, "sip_exporter_ack_total")
	byeTotal := getMetric(t, env.endpoint, "sip_exporter_bye_total")

	t.Logf("invite=%.0f, invite_200=%.0f, 401=%.0f, ack=%.0f, bye=%.0f",
		inviteTotal, invite200Total, status401, ackTotal, byeTotal)

	require.Equal(t, float64(callCount*2), inviteTotal,
		"2 INVITEs per call: original + auth retry")
	require.Equal(t, float64(callCount), invite200Total,
		"only retry INVITEs get 200 OK")
	require.Equal(t, float64(callCount), status401,
		"one 401 per call")
	require.Equal(t, float64(callCount*2), ackTotal,
		"2 ACKs per call: failed INVITE ACK + success ACK")
	require.Equal(t, float64(callCount), byeTotal,
		"1 BYE per call")

	waitForSessionsZero(t, env.endpoint)
}
