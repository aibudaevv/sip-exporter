//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSDC_AllScenarios tests SDC metric with various scenarios.
// SDC counts completed sessions (BYE→200 OK + expired dialogs).
// On loopback SDC is NOT doubled (dialog map deduplicates).
func TestSDC_AllScenarios(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantSDC     float64
	}{
		{
			name:        "all_completed",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   50,
			wantSDC:     50.0,
		},
		{
			name:        "rejected_486",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   50,
			wantSDC:     0.0,
		},
		{
			name:        "server_error",
			uasScenario: "uas_server_error.xml",
			uacScenario: "uac_server_error.xml",
			callCount:   50,
			wantSDC:     0.0,
		},
		{
			name:        "redirect",
			uasScenario: "uas_redirect.xml",
			uacScenario: "uac_redirect.xml",
			callCount:   50,
			wantSDC:     0.0,
		},
		{
			name:        "no_invite",
			uasScenario: "uas_no_invite.xml",
			uacScenario: "uac_no_invite.xml",
			callCount:   50,
			wantSDC:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.restart(t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, &env.testEnv)

			sdc := getSDC(t, env.endpoint)
			t.Logf("SDC = %.0f (want %.0f)", sdc, tt.wantSDC)
			require.Equal(t, tt.wantSDC, sdc)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestSDC_Mixed tests 35 completed + 15 rejected (486).
// SDC = 35 (only completed sessions counted).
func TestSDC_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 35, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 15, env)

	sdc := getSDC(t, env.endpoint)
	t.Logf("SDC = %.0f (want %.0f)", sdc, 35.0)
	require.Equal(t, 35.0, sdc)

	waitForSessionsZero(t, env.endpoint)
}

// TestSDC_MixedWith3xx tests 25 redirect + 25 successful.
// SDC = 25 (only completed sessions from successful calls).
func TestSDC_MixedWith3xx(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 25, env)
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 25, env)

	sdc := getSDC(t, env.endpoint)
	t.Logf("SDC = %.0f (want %.0f)", sdc, 25.0)
	require.Equal(t, 25.0, sdc)

	waitForSessionsZero(t, env.endpoint)
}

// TestSDC_Complex tests 20×completed + 15×480 + 15×500.
// SDC = 20 (only completed sessions).
func TestSDC_Complex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 20, env)
	runSippScenario(ctx, t, "uas_busy.xml", "uac_busy.xml", 15, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 15, env)

	sdc := getSDC(t, env.endpoint)
	t.Logf("SDC = %.0f (want %.0f)", sdc, 20.0)
	require.Equal(t, 20.0, sdc)

	waitForSessionsZero(t, env.endpoint)
}

// TestSDC_SessionExpires tests that expired dialogs increment SDC.
func TestSDC_SessionExpires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	sdcBefore := getSDC(t, env.endpoint)
	t.Logf("Before: SDC = %.0f", sdcBefore)

	runSippScenario(ctx, t, "uas_short_expires.xml", "uac_short_expires.xml", 10, env)

	require.Eventually(t, func() bool {
		return getMetric(t, env.endpoint, "sip_exporter_sessions") == 0
	}, 15*time.Second, 500*time.Millisecond, "sessions did not expire within timeout")

	sdcAfter := getSDC(t, env.endpoint)
	t.Logf("After: SDC = %.0f", sdcAfter)

	require.Equal(t, sdcBefore+10.0, sdcAfter, "SDC should increase by 10 after Session-Expires timeout")
}

// TestSDC_WithCarrierConfig verifies SDC per-carrier.
func TestSDC_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 50, env)

	sdc := env.getSDCByCarrier(t)
	t.Logf("SDC{carrier=%q} = %.0f (want 50)", env.carrier, sdc)
	require.Equal(t, 50.0, sdc)

	env.waitForSessionsZeroByCarrier(t)
}

// TestSDC_MixedWithCarrierConfig verifies SDC per-carrier with mixed results.
func TestSDC_MixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 35, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 15, env)

	sdc := env.getSDCByCarrier(t)
	t.Logf("SDC{carrier=%q} = %.0f (want 35)", env.carrier, sdc)
	require.Equal(t, 35.0, sdc)

	env.waitForSessionsZeroByCarrier(t)
}
