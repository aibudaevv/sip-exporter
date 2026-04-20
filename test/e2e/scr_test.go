//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSCR_AllScenarios tests SCR metric with various scenarios.
// SCR = (Successfully Completed Sessions) / (Total INVITE) × 100
// 3xx NOT excluded from denominator (same as ISA).
// On loopback inviteTotal is doubled (each INVITE seen as sent+recv),
// while sessionCompletedTotal is not (dialog map deduplicates).
// So expected SCR values are half of theoretical: e.g. all_completed → 50% not 100%.
func TestSCR_AllScenarios(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantSCR     float64
	}{
		{
			name:        "all_completed",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   50,
			wantSCR:     50.0,
		},
		{
			name:        "none_completed_486",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   50,
			wantSCR:     0.0,
		},
		{
			name:        "none_completed_500",
			uasScenario: "uas_server_error.xml",
			uacScenario: "uac_server_error.xml",
			callCount:   50,
			wantSCR:     0.0,
		},
		{
			name:        "redirect_only",
			uasScenario: "uas_redirect.xml",
			uacScenario: "uac_redirect.xml",
			callCount:   50,
			wantSCR:     0.0,
		},
		{
			name:        "no_invite",
			uasScenario: "uas_no_invite.xml",
			uacScenario: "uac_no_invite.xml",
			callCount:   50,
			wantSCR:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			env := newTestEnv(ctx, t)

			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			scr := getSCR(t, env.endpoint)
			t.Logf("SCR = %.2f (want %.2f)", scr, tt.wantSCR)
			require.Equal(t, tt.wantSCR, scr)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestSCR_Mixed tests 35 completed + 15 rejected (486).
// On loopback: inviteTotal=2×50=100, sessionCompletedTotal=35 → SCR = 35/100 × 100 = 35%.
func TestSCR_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 35, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 15, env)

	scr := getSCR(t, env.endpoint)
	t.Logf("SCR = %.2f (want %.2f)", scr, 35.0)
	require.Equal(t, 35.0, scr)

	waitForSessionsZero(t, env.endpoint)
}

// TestSCR_MixedWith3xx tests that 3xx are NOT excluded from SCR denominator.
// 25 redirect (3xx) + 25 successful → SCR = 25/50 × 100 = 50%.
// On loopback inviteTotal is doubled (2×50=100) while sessionCompletedTotal is not (25),
// so expected SCR = 25/100 × 100 = 25%.
// (SER would be 100% because 3xx excluded, but SCR keeps them.)
func TestSCR_MixedWith3xx(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 25, env)
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 25, env)

	scr := getSCR(t, env.endpoint)
	t.Logf("SCR = %.2f (want %.2f)", scr, 25.0)
	require.Equal(t, 25.0, scr)

	waitForSessionsZero(t, env.endpoint)
}

// TestSCR_Complex tests mixed scenarios.
// 20×completed + 15×486 + 15×500 → SCR = 20/50 × 100 = 40%.
// On loopback inviteTotal is doubled (2×50=100) while sessionCompletedTotal is not (20),
// so expected SCR = 20/100 × 100 = 20%.
func TestSCR_Complex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 20, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 15, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 15, env)

	scr := getSCR(t, env.endpoint)
	t.Logf("SCR = %.2f (want %.2f)", scr, 20.0)
	require.Equal(t, 20.0, scr)

	waitForSessionsZero(t, env.endpoint)
}

// TestSCR_SessionExpires tests that expired dialogs (Session-Expires timeout)
// increment sessionCompletedTotal, increasing SCR.
func TestSCR_SessionExpires(t *testing.T) {
	t.Parallel()
	start := time.Now()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	scrBefore := getSCR(t, env.endpoint)
	sessionsBefore := getSessions(t, env.endpoint)
	t.Logf("Before: SCR = %.2f, sessions = %.0f", scrBefore, sessionsBefore)

	runSippScenario(ctx, t, "uas_short_expires.xml", "uac_short_expires.xml", 10, env)

	require.Eventually(t, func() bool {
		return getMetric(t, env.endpoint, "sip_exporter_sessions") == 0
	}, 15*time.Second, 500*time.Millisecond, "sessions did not expire within timeout")

	scrAfter := getSCR(t, env.endpoint)
	sessionsAfter := getSessions(t, env.endpoint)
	t.Logf("After: SCR = %.2f, sessions = %.0f", scrAfter, sessionsAfter)

	require.Equal(t, 0.0, sessionsAfter, "sessions should be 0 after Session-Expires timeout")
	require.Greater(t, scrAfter, scrBefore, "SCR should increase after Session-Expires timeout")
	t.Logf("duration: %v", time.Since(start))
}

// TestSCR_WithCarrierConfig verifies SCR per-carrier on loopback.
// On loopback with carrier: inviteTotal doubles, sessionCompletedTotal does not.
// SCR = sessionCompletedTotal / inviteTotal × 100 = 50/100 × 100 = 50%.
func TestSCR_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 50, env)

	scr := env.getSCRByCarrier(t)
	t.Logf("SCR{carrier=%q} = %.2f (want %.2f)", env.carrier, scr, 50.0)
	require.Equal(t, 50.0, scr)

	env.waitForSessionsZeroByCarrier(t)
}

// TestSCR_MixedWithCarrierConfig verifies SCR per-carrier with mixed results.
func TestSCR_MixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 35, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 15, env)

	scr := env.getSCRByCarrier(t)
	t.Logf("SCR{carrier=%q} = %.2f (want %.2f)", env.carrier, scr, 35.0)
	require.Equal(t, 35.0, scr)

	env.waitForSessionsZeroByCarrier(t)
}
