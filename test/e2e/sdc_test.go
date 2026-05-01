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
			callCount:   100,
			wantSDC:     200.0,
		},
		{
			name:        "rejected_486",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   100,
			wantSDC:     0.0,
		},
		{
			name:        "server_error",
			uasScenario: "uas_server_error.xml",
			uacScenario: "uac_server_error.xml",
			callCount:   100,
			wantSDC:     0.0,
		},
		{
			name:        "redirect",
			uasScenario: "uas_redirect.xml",
			uacScenario: "uac_redirect.xml",
			callCount:   100,
			wantSDC:     0.0,
		},
		{
			name:        "no_invite",
			uasScenario: "uas_no_invite.xml",
			uacScenario: "uac_no_invite.xml",
			callCount:   100,
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

// TestSDC_Mixed tests 140 completed + 60 rejected (486).
// SDC = 140 (only completed sessions counted).
func TestSDC_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 140, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 60, env)

	sdc := getSDC(t, env.endpoint)
	t.Logf("SDC = %.0f (want %.0f)", sdc, 140.0)
	require.Equal(t, 140.0, sdc)

	waitForSessionsZero(t, env.endpoint)
}

// TestSDC_MixedWith3xx tests 100 redirect + 100 successful.
// SDC = 100 (only completed sessions from successful calls).
func TestSDC_MixedWith3xx(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 100, env)
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)

	sdc := getSDC(t, env.endpoint)
	t.Logf("SDC = %.0f (want %.0f)", sdc, 100.0)
	require.Equal(t, 100.0, sdc)

	waitForSessionsZero(t, env.endpoint)
}

// TestSDC_Complex tests 80×completed + 60×480 + 60×500.
// SDC = 80 (only completed sessions).
func TestSDC_Complex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 80, env)
	runSippScenario(ctx, t, "uas_busy.xml", "uac_busy.xml", 60, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 60, env)

	sdc := getSDC(t, env.endpoint)
	t.Logf("SDC = %.0f (want %.0f)", sdc, 80.0)
	require.Equal(t, 80.0, sdc)

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

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 200, env)

	sdc := env.getSDCByCarrier(t)
	t.Logf("SDC{carrier=%q} = %.0f (want 200)", env.carrier, sdc)
	require.Equal(t, 200.0, sdc)

	env.waitForSessionsZeroByCarrier(t)
}

// TestSDC_MixedWithCarrierConfig verifies SDC per-carrier with mixed results.
func TestSDC_MixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 140, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 60, env)

	sdc := env.getSDCByCarrier(t)
	t.Logf("SDC{carrier=%q} = %.0f (want 140)", env.carrier, sdc)
	require.Equal(t, 140.0, sdc)

	env.waitForSessionsZeroByCarrier(t)
}
