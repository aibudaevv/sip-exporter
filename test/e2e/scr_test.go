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
// PACKET_IGNORE_OUTGOING suppresses TX on lo → each packet seen once → SCR matches theoretical.
func TestSCR_AllScenarios(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

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
			callCount:   100,
			wantSCR:     100.0,
		},
		{
			name:        "none_completed_486",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   100,
			wantSCR:     0.0,
		},
		{
			name:        "none_completed_500",
			uasScenario: "uas_server_error.xml",
			uacScenario: "uac_server_error.xml",
			callCount:   100,
			wantSCR:     0.0,
		},
		{
			name:        "redirect_only",
			uasScenario: "uas_redirect.xml",
			uacScenario: "uac_redirect.xml",
			callCount:   100,
			wantSCR:     0.0,
		},
		{
			name:        "no_invite",
			uasScenario: "uas_no_invite.xml",
			uacScenario: "uac_no_invite.xml",
			callCount:   100,
			wantSCR:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.restart(t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, &env.testEnv)

			scr := getSCR(t, env.endpoint)
			t.Logf("SCR = %.2f (want %.2f)", scr, tt.wantSCR)
			if tt.uacScenario != "uac_no_invite.xml" {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_scr"))
			}
			require.InDelta(t, tt.wantSCR, scr, ratioDelta)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestSCR_Mixed tests 140 completed + 60 rejected (486).
// SCR = 140/200 × 100 = 70%.
func TestSCR_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 140, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 60, env)

	scr := getSCR(t, env.endpoint)
	t.Logf("SCR = %.2f (want %.2f)", scr, 70.0)
	require.InDelta(t, 70.0, scr, ratioDelta)

	waitForSessionsZero(t, env.endpoint)
}

// TestSCR_MixedWith3xx tests that 3xx are NOT excluded from SCR denominator.
// 100 redirect (3xx) + 100 successful → SCR = 100/200 × 100 = 50%.
func TestSCR_MixedWith3xx(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 100, env)
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)

	scr := getSCR(t, env.endpoint)
	t.Logf("SCR = %.2f (want %.2f)", scr, 50.0)
	require.InDelta(t, 50.0, scr, ratioDelta)

	waitForSessionsZero(t, env.endpoint)
}

// TestSCR_Complex tests mixed scenarios.
// 80×completed + 60×486 + 60×500 → SCR = 80/200 × 100 = 40%.
func TestSCR_Complex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 80, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 60, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 60, env)

	scr := getSCR(t, env.endpoint)
	t.Logf("SCR = %.2f (want %.2f)", scr, 40.0)
	require.InDelta(t, 40.0, scr, ratioDelta)

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

// TestSCR_WithCarrierConfig verifies SCR per-carrier.
// SCR = sessionCompletedTotal / inviteTotal × 100 = 200/200 × 100 = 100%.
func TestSCR_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 200, env)

	scr := env.getSCRByCarrier(t)
	t.Logf("SCR{carrier=%q} = %.2f (want %.2f)", env.carrier, scr, 100.0)
	require.InDelta(t, 100.0, scr, ratioDelta)

	env.waitForSessionsZeroByCarrier(t)
}

// TestSCR_MixedWithCarrierConfig verifies SCR per-carrier with mixed results.
func TestSCR_MixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 140, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 60, env)

	scr := env.getSCRByCarrier(t)
	t.Logf("SCR{carrier=%q} = %.2f (want %.2f)", env.carrier, scr, 70.0)
	require.InDelta(t, 70.0, scr, ratioDelta)

	env.waitForSessionsZeroByCarrier(t)
}
