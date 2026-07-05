//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSER_AllScenarios(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantSER     float64
	}{
		{
			name:        "100_percent",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   100,
			wantSER:     100.0,
		},
		{
			name:        "0_percent",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   100,
			wantSER:     0.0,
		},
		{
			name:        "redirect",
			uasScenario: "uas_redirect.xml",
			uacScenario: "uac_redirect.xml",
			callCount:   100,
			wantSER:     0.0,
		},
		{
			name:        "no_invite",
			uasScenario: "uas_no_invite.xml",
			uacScenario: "uac_no_invite.xml",
			callCount:   100,
			wantSER:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.restart(t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, &env.testEnv)

			ser := getSER(t, env.endpoint)
			t.Logf("SER = %.2f (want %.2f)", ser, tt.wantSER)
			if tt.uacScenario != "uac_no_invite.xml" {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_ser"))
			}
			require.InDelta(t, tt.wantSER, ser, ratioDelta)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestSER_Mixed tests mixed scenario: some calls successful, some rejected.
func TestSER_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 140, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 60, env)

	ser := getSER(t, env.endpoint)
	t.Logf("SER = %.2f (want %.2f)", ser, 70.0)
	require.InDelta(t, 70.0, ser, ratioDelta)

	waitForSessionsZero(t, env.endpoint)
}

// TestSER_Mixed3xx tests that 3xx responses are correctly excluded from denominator.
// 100 redirect (3xx) + 100 successful (200 OK) → SER = 100% (all non-3xx successful).
func TestSER_Mixed3xx(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 100, env)
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)

	ser := getSER(t, env.endpoint)
	t.Logf("SER = %.2f (want %.2f)", ser, 100.0)
	require.InDelta(t, 100.0, ser, ratioDelta)

	waitForSessionsZero(t, env.endpoint)
}

// TestSER_WithCarrierConfig verifies SER is computed per-carrier when carriers.yaml is configured.
// On loopback with carrier config, all traffic gets carrier="loopback-carrier".
func TestSER_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnvWithCarriers(ctx, t)

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantSER     float64
	}{
		{
			name:        "100_percent",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   100,
			wantSER:     100.0,
		},
		{
			name:        "0_percent",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   100,
			wantSER:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.restart(t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, &env.testEnv)

			ser := env.getSERByCarrier(t)
			t.Logf("SER{carrier=%q} = %.2f (want %.2f)", env.carrier, ser, tt.wantSER)
			require.True(t, metricExists(t, env.endpoint, "sip_exporter_ser"))
			require.InDelta(t, tt.wantSER, ser, ratioDelta)

			env.waitForSessionsZeroByCarrier(t)
		})
	}
}

// TestSER_MixedWithCarrierConfig verifies SER per-carrier for mixed traffic.
func TestSER_MixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 140, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 60, env)

	ser := env.getSERByCarrier(t)
	t.Logf("SER{carrier=%q} = %.2f (want %.2f)", env.carrier, ser, 70.0)
	require.InDelta(t, 70.0, ser, ratioDelta)

	env.waitForSessionsZeroByCarrier(t)
}

// TestSER_ConcurrentRequests verifies SER with concurrent INVITE traffic.
func TestSER_ConcurrentRequests(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 120, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 40, env)
	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 40, env)

	ser := getSER(t, env.endpoint)
	t.Logf("SER = %.2f%%", ser)
	require.Greater(t, ser, 0.0, "SER should be calculated")
	require.LessOrEqual(t, ser, 100.0, "SER should not exceed 100%")

	waitForSessionsZero(t, env.endpoint)
}
