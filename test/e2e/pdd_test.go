//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// MC/DC test cases for PDD (Post Dial Delay):
//
// Decision: measurePDD is called when INVITE response with 1xx status is received.
//
// Conditions:
//   C1: response status is "180" (vs other 1xx like 100, 181, 182, 183)
//   C2: response belongs to INVITE transaction (vs REGISTER, OPTIONS, etc.)
//   C3: inviteTracker entry exists for the Call-ID
//
// MC/DC matrix (each condition independently changes the outcome):
//
// | Test                                | C1   | C2   | C3   | PDD  |
// |-------------------------------------|------|------|------|------|
// | 180Ringing_Measured                 | T    | T    | T    | >0   |
// | 100TryingOnly_NoPDD                 | F    | T    | T    | =0   |
// | 181CallForwarded_NoPDD              | F    | T    | T    | =0   |
// | 182Queued_NoPDD                     | F    | T    | T    | =0   |
// | BusyNo180_NoPDD                     | F    | T    | T    | =0   |
// | RegisterOnly_NoPDD                  | n/a  | F    | n/a  | =0   |
// | TimeoutNoResponse_NoPDD             | n/a  | T    | F    | =0   |
// | NoInviteTraffic_NoPDD               | n/a  | F    | n/a  | =0   |
// | ConcurrentCalls                     | T    | T    | T    | >0   |
// | MixedScenarios                      | T/F  | T    | T    | >0   |
// | WithCarrierConfig                   | T    | T    | T    | >0   |

func TestPDD_180Ringing_Measured(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 50, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	t.Logf("sip_exporter_invite_total = %.0f", inviteTotal)
	require.Greater(t, inviteTotal, 0.0, "should have INVITE requests")

	status180Total := getMetric(t, env.endpoint, "sip_exporter_180_total")
	t.Logf("sip_exporter_180_total = %.0f", status180Total)
	require.Greater(t, status180Total, 0.0, "should have 180 Ringing responses")

	pdd := getPDD(t, env.endpoint)
	t.Logf("PDD = %.2f ms", pdd)
	require.Greater(t, pdd, 0.0, "PDD should be > 0 when 180 Ringing is received (C1=T, C2=T, C3=T)")
	require.Greater(t, getMetric(t, env.endpoint, "sip_exporter_pdd_count"), 0.0, "PDD histogram should have observations")

	ttr := getTTR(t, env.endpoint)
	t.Logf("TTR = %.2f ms", ttr)
	require.Greater(t, ttr, 0.0, "TTR should also be measured on 100 Trying")

	waitForSessionsZero(t, env.endpoint)
}

func TestPDD_100TryingOnly_NoPDD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	pddBefore := getPDD(t, env.endpoint)

	runSippScenario(ctx, t, "uas_100only.xml", "uac_100only.xml", 50, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	t.Logf("sip_exporter_invite_total = %.0f", inviteTotal)
	require.Greater(t, inviteTotal, 0.0, "should have INVITE requests")

	status100Total := getMetric(t, env.endpoint, "sip_exporter_100_total")
	t.Logf("sip_exporter_100_total = %.0f", status100Total)
	require.Greater(t, status100Total, 0.0, "should have 100 Trying responses")

	pddAfter := getPDD(t, env.endpoint)
	t.Logf("PDD before = %.2f ms, after = %.2f ms", pddBefore, pddAfter)
	require.Equal(t, pddBefore, pddAfter, "PDD should NOT be measured for 100 Trying without 180 (C1=F: status != 180)")

	ttr := getTTR(t, env.endpoint)
	t.Logf("TTR = %.2f ms", ttr)
	require.Greater(t, ttr, 0.0, "TTR should still be measured on 100 Trying")

	waitForSessionsZero(t, env.endpoint)
}

func TestPDD_181CallForwarded_NoPDD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	pddBefore := getPDD(t, env.endpoint)

	runSippScenario(ctx, t, "uas_181.xml", "uac_181.xml", 50, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	t.Logf("sip_exporter_invite_total = %.0f", inviteTotal)
	require.Greater(t, inviteTotal, 0.0, "should have INVITE requests")

	pddAfter := getPDD(t, env.endpoint)
	t.Logf("PDD before = %.2f ms, after = %.2f ms", pddBefore, pddAfter)
	require.Equal(t, pddBefore, pddAfter, "PDD should NOT be measured for 181 Call Is Being Forwarded (C1=F: status != 180)")

	waitForSessionsZero(t, env.endpoint)
}

func TestPDD_182Queued_NoPDD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	pddBefore := getPDD(t, env.endpoint)

	runSippScenario(ctx, t, "uas_182.xml", "uac_182.xml", 50, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	t.Logf("sip_exporter_invite_total = %.0f", inviteTotal)
	require.Greater(t, inviteTotal, 0.0, "should have INVITE requests")

	pddAfter := getPDD(t, env.endpoint)
	t.Logf("PDD before = %.2f ms, after = %.2f ms", pddBefore, pddAfter)
	require.Equal(t, pddBefore, pddAfter, "PDD should NOT be measured for 182 Queued (C1=F: status != 180)")

	waitForSessionsZero(t, env.endpoint)
}

func TestPDD_BusyNo180_NoPDD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	pddBefore := getPDD(t, env.endpoint)

	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 50, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	t.Logf("sip_exporter_invite_total = %.0f", inviteTotal)
	require.Greater(t, inviteTotal, 0.0, "should have INVITE requests")

	pddAfter := getPDD(t, env.endpoint)
	t.Logf("PDD before = %.2f ms, after = %.2f ms", pddBefore, pddAfter)
	require.Equal(t, pddBefore, pddAfter, "PDD should NOT be measured for 486 Busy (no 180 in flow)")

	waitForSessionsZero(t, env.endpoint)
}

func TestPDD_RegisterOnly_NoPDD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	pddBefore := getPDD(t, env.endpoint)

	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", 50, env)

	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")
	t.Logf("sip_exporter_register_total = %.0f", registerTotal)
	require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

	pddAfter := getPDD(t, env.endpoint)
	t.Logf("PDD before = %.2f ms, after = %.2f ms", pddBefore, pddAfter)
	require.Equal(t, pddBefore, pddAfter, "PDD should NOT change for REGISTER-only traffic (C2=F: not INVITE)")
}

func TestPDD_TimeoutNoResponse_NoPDD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	pddBefore := getPDD(t, env.endpoint)
	inviteBefore := getMetric(t, env.endpoint, "sip_exporter_invite_total")

	runSippUACOnly(ctx, t, "uac_100.xml", 5, env)

	inviteAfter := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	require.Greater(t, inviteAfter, inviteBefore, "should have INVITE requests")

	pddAfter := getPDD(t, env.endpoint)
	t.Logf("PDD before = %.2f ms, after = %.2f ms", pddBefore, pddAfter)
	require.Equal(t, pddBefore, pddAfter, "PDD should NOT change for timeout with no response (C3=F: no tracker entry)")
}

func TestPDD_NoInviteTraffic_NoPDD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	pddBefore := getPDD(t, env.endpoint)

	runSippScenario(ctx, t, "uas_no_invite.xml", "uac_no_invite.xml", 50, env)

	pddAfter := getPDD(t, env.endpoint)
	t.Logf("PDD before = %.2f ms, after = %.2f ms", pddBefore, pddAfter)
	require.Equal(t, pddBefore, pddAfter, "PDD should NOT change when no INVITEs are sent")
}

func TestPDD_ConcurrentCalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	t.Logf("sip_exporter_invite_total = %.0f", inviteTotal)
	require.Greater(t, inviteTotal, 0.0, "should have INVITE requests")

	pdd := getPDD(t, env.endpoint)
	t.Logf("PDD = %.2f ms (100 concurrent calls)", pdd)
	require.Greater(t, pdd, 0.0, "PDD should be measured for concurrent calls with 180 Ringing")
	require.Greater(t, getMetric(t, env.endpoint, "sip_exporter_pdd_count"), 0.0, "PDD histogram should have observations")

	waitForSessionsZero(t, env.endpoint)
}

func TestPDD_MixedScenarios(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 30, env)
	runSippScenario(ctx, t, "uas_100only.xml", "uac_100only.xml", 20, env)

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	t.Logf("sip_exporter_invite_total = %.0f", inviteTotal)
	require.Greater(t, inviteTotal, 0.0, "should have INVITE requests")

	pdd := getPDD(t, env.endpoint)
	t.Logf("PDD = %.2f ms (mixed: 30 with 180, 20 without)", pdd)
	require.Greater(t, pdd, 0.0, "PDD should be measured for calls with 180 Ringing in mixed scenario")
	require.Greater(t, getMetric(t, env.endpoint, "sip_exporter_pdd_count"), 0.0, "PDD histogram should have observations")

	waitForSessionsZero(t, env.endpoint)
}

func TestPDD_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 50, env)

	pdd := env.getPDDByCarrier(t)
	t.Logf("PDD{carrier=%q} = %.2f ms", env.carrier, pdd)
	require.Greater(t, pdd, 0.0, "PDD should be > 0 with carrier labels when 180 Ringing is received")

	env.waitForSessionsZeroByCarrier(t)
}
