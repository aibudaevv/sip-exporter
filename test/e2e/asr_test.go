//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestASR_AllScenarios tests ASR metric with various scenarios.
// ASR = (INVITE → 200 OK) / Total INVITE × 100 (ITU-T E.411)
// 3xx NOT excluded from denominator (difference from SER).
// On loopback both numerator and denominator double → ASR unchanged (like SER).
func TestASR_AllScenarios(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantASR     float64
	}{
		{
			name:        "100_percent",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   50,
			wantASR:     100.0,
		},
		{
			name:        "0_percent",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   50,
			wantASR:     0.0,
		},
		{
			name:        "redirect",
			uasScenario: "uas_redirect.xml",
			uacScenario: "uac_redirect.xml",
			callCount:   50,
			wantASR:     0.0,
		},
		{
			name:        "no_invite",
			uasScenario: "uas_no_invite.xml",
			uacScenario: "uac_no_invite.xml",
			callCount:   50,
			wantASR:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			env := newTestEnv(ctx, t)

			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			asr := getASR(t, env.endpoint)
			t.Logf("ASR = %.2f (want %.2f)", asr, tt.wantASR)
			require.Equal(t, tt.wantASR, asr)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestASR_Mixed tests 35 successful + 15 rejected (486).
// On loopback: inviteTotal=2×50=100, invite200OKTotal=2×35=70 → ASR = 70/100 × 100 = 70%.
func TestASR_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 35, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 15, env)

	asr := getASR(t, env.endpoint)
	t.Logf("ASR = %.2f (want %.2f)", asr, 70.0)
	require.Equal(t, 70.0, asr)

	waitForSessionsZero(t, env.endpoint)
}

// TestASR_MixedWith3xx tests that 3xx are NOT excluded from ASR denominator.
// 25 redirect (3xx) + 25 successful → ASR = 25/50 × 100 = 50%.
// On loopback: inviteTotal=2×50=100, invite200OKTotal=2×25=50 → ASR = 50/100 × 100 = 50%.
// (SER would be 100% because 3xx excluded, but ASR keeps them.)
func TestASR_MixedWith3xx(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 25, env)
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 25, env)

	asr := getASR(t, env.endpoint)
	t.Logf("ASR = %.2f (want %.2f)", asr, 50.0)
	require.Equal(t, 50.0, asr)

	ser := getSER(t, env.endpoint)
	t.Logf("SER = %.2f (must be >= ASR)", ser)
	require.GreaterOrEqual(t, ser, asr, "SER must be >= ASR")

	waitForSessionsZero(t, env.endpoint)
}

// TestASR_Complex tests mixed scenarios.
// 20×200 OK + 15×480 + 15×500 → ASR = 20/50 × 100 = 40%.
// On loopback: inviteTotal=2×50=100, invite200OKTotal=2×20=40 → ASR = 40/100 × 100 = 40%.
func TestASR_Complex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 20, env)
	runSippScenario(ctx, t, "uas_busy.xml", "uac_busy.xml", 15, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 15, env)

	asr := getASR(t, env.endpoint)
	t.Logf("ASR = %.2f (want %.2f)", asr, 40.0)
	require.Equal(t, 40.0, asr)

	waitForSessionsZero(t, env.endpoint)
}

// TestASR_WithCarrierConfig verifies ASR per-carrier.
func TestASR_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 50, env)

	asr := env.getASRByCarrier(t)
	t.Logf("ASR{carrier=%q} = %.2f (want %.2f)", env.carrier, asr, 100.0)
	require.Equal(t, 100.0, asr)

	env.waitForSessionsZeroByCarrier(t)
}

// TestASR_MixedWithCarrierConfig verifies ASR per-carrier for mixed traffic.
func TestASR_MixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 35, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 15, env)

	asr := env.getASRByCarrier(t)
	t.Logf("ASR{carrier=%q} = %.2f (want %.2f)", env.carrier, asr, 70.0)
	require.Equal(t, 70.0, asr)

	env.waitForSessionsZeroByCarrier(t)
}
