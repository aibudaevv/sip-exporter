//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func getTTR(t *testing.T, endpoint string) float64 {
	t.Helper()

	sum := getMetric(t, endpoint, "sip_exporter_ttr_sum")
	count := getMetric(t, endpoint, "sip_exporter_ttr_count")
	if count == 0 {
		return 0
	}

	return sum / count
}

func TestTTR_SuccessfulCalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 50, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	t.Logf("sip_exporter_invite_total = %.0f", inviteTotal)
	require.Greater(t, inviteTotal, 0.0, "should have INVITE requests")

	status100Total := getMetric(t, env.endpoint, "sip_exporter_100_total")
	t.Logf("sip_exporter_100_total = %.0f", status100Total)
	require.Greater(t, status100Total, 0.0, "should have 100 Trying responses")

	ttr := getTTR(t, env.endpoint)
	t.Logf("TTR = %.2f ms", ttr)
	require.Greater(t, ttr, 0.0, "TTR should be greater than 0 when 1xx responses are sent")

	waitForSessionsZero(t, env.endpoint)
}

func TestTTR_BusyCalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 50, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	t.Logf("sip_exporter_invite_total = %.0f", inviteTotal)
	require.Greater(t, inviteTotal, 0.0, "should have INVITE requests")

	status100Total := getMetric(t, env.endpoint, "sip_exporter_100_total")
	t.Logf("sip_exporter_100_total = %.0f", status100Total)
	require.Greater(t, status100Total, 0.0, "uas_0 sends 100 Trying before 486")

	ttr := getTTR(t, env.endpoint)
	t.Logf("TTR = %.2f ms", ttr)
	require.Greater(t, ttr, 0.0, "TTR should be measured even for rejected calls (100 Trying is sent)")

	waitForSessionsZero(t, env.endpoint)
}

func TestTTR_RegisterScenario_NoTTR(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	ttrBefore := getTTR(t, env.endpoint)

	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", 50, env)

	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")
	t.Logf("sip_exporter_register_total = %.0f", registerTotal)
	require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

	ttrAfter := getTTR(t, env.endpoint)
	t.Logf("TTR before = %.2f ms, after = %.2f ms", ttrBefore, ttrAfter)
	require.Equal(t, ttrBefore, ttrAfter, "TTR should not change for REGISTER-only scenarios")
}

func TestTTR_NoInviteScenario(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	ttrBefore := getTTR(t, env.endpoint)

	runSippScenario(ctx, t, "uas_no_invite.xml", "uac_no_invite.xml", 50, env)

	ttrAfter := getTTR(t, env.endpoint)
	t.Logf("TTR before = %.2f ms, after = %.2f ms", ttrBefore, ttrAfter)
	require.Equal(t, ttrBefore, ttrAfter, "TTR should not change when no INVITEs are sent")
}

func TestTTR_Timeout_NoResponse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	ttrBefore := getTTR(t, env.endpoint)
	inviteBefore := getMetric(t, env.endpoint, "sip_exporter_invite_total")

	runSippUACOnly(ctx, t, "uac_100.xml", 5, env)

	inviteAfter := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	require.Greater(t, inviteAfter, inviteBefore, "should have INVITE requests")

	ttrAfter := getTTR(t, env.endpoint)
	t.Logf("TTR before = %.2f ms, after = %.2f ms", ttrBefore, ttrAfter)
	require.Equal(t, ttrBefore, ttrAfter, "TTR should not change for timeout (no response)")
}

func TestTTR_ConcurrentCalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	t.Logf("sip_exporter_invite_total = %.0f", inviteTotal)
	require.Greater(t, inviteTotal, 0.0, "should have INVITE requests")

	ttr := getTTR(t, env.endpoint)
	t.Logf("TTR = %.2f ms (100 concurrent calls)", ttr)
	require.Greater(t, ttr, 0.0, "TTR should be measured for concurrent calls")

	waitForSessionsZero(t, env.endpoint)
}

func TestTTR_MixedScenarios(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 30, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 20, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	t.Logf("sip_exporter_invite_total = %.0f", inviteTotal)
	require.Greater(t, inviteTotal, 0.0, "should have INVITE requests")

	ttr := getTTR(t, env.endpoint)
	t.Logf("TTR = %.2f ms (mixed)", ttr)
	require.Greater(t, ttr, 0.0, "TTR should be measured for mixed scenarios")

	waitForSessionsZero(t, env.endpoint)
}
