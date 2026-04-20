//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSER_AllScenarios(t *testing.T) {
	t.Parallel()
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
			callCount:   50,
			wantSER:     100.0,
		},
		{
			name:        "0_percent",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   50,
			wantSER:     0.0,
		},
		{
			name:        "redirect",
			uasScenario: "uas_redirect.xml",
			uacScenario: "uac_redirect.xml",
			callCount:   50,
			wantSER:     0.0,
		},
		{
			name:        "no_invite",
			uasScenario: "uas_no_invite.xml",
			uacScenario: "uac_no_invite.xml",
			callCount:   50,
			wantSER:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			env := newTestEnv(ctx, t)

			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			ser := getSER(t, env.endpoint)
			t.Logf("SER = %.2f (want %.2f)", ser, tt.wantSER)
			require.Equal(t, tt.wantSER, ser)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestSER_Mixed tests mixed scenario: some calls successful, some rejected.
func TestSER_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 35, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 15, env)

	ser := getSER(t, env.endpoint)
	t.Logf("SER = %.2f (want %.2f)", ser, 70.0)
	require.Equal(t, 70.0, ser)

	waitForSessionsZero(t, env.endpoint)
}

// TestSER_Mixed3xx tests that 3xx responses are correctly excluded from denominator.
// 50 redirect (3xx) + 50 successful (200 OK) → SER = 100% (all non-3xx successful).
func TestSER_Mixed3xx(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 25, env)
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 25, env)

	ser := getSER(t, env.endpoint)
	t.Logf("SER = %.2f (want %.2f)", ser, 100.0)
	require.Equal(t, 100.0, ser)

	waitForSessionsZero(t, env.endpoint)
}

// TestSER_WithCarrierConfig verifies SER is computed per-carrier when carriers.yaml is configured.
// On loopback with carrier config, all traffic gets carrier="loopback-carrier".
func TestSER_WithCarrierConfig(t *testing.T) {
	t.Parallel()
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
			callCount:   50,
			wantSER:     100.0,
		},
		{
			name:        "0_percent",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   50,
			wantSER:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			env := newTestEnvWithCarriers(ctx, t)

			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			ser := env.getSERByCarrier(t)
			t.Logf("SER{carrier=%q} = %.2f (want %.2f)", env.carrier, ser, tt.wantSER)
			require.Equal(t, tt.wantSER, ser)

			env.waitForSessionsZeroByCarrier(t)
		})
	}
}

// TestSER_MixedWithCarrierConfig verifies SER per-carrier for mixed traffic.
func TestSER_MixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 35, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 15, env)

	ser := env.getSERByCarrier(t)
	t.Logf("SER{carrier=%q} = %.2f (want %.2f)", env.carrier, ser, 70.0)
	require.Equal(t, 70.0, ser)

	env.waitForSessionsZeroByCarrier(t)
}
