//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestISA_AllScenarios tests ISA metric with various ineffective response scenarios.
// ISA = (INVITE → 408, 500, 503, 504) / Total INVITE × 100
func TestISA_AllScenarios(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantISA     float64
	}{
		{
			name:        "all_500",
			uasScenario: "uas_server_error.xml",
			uacScenario: "uac_server_error.xml",
			callCount:   50,
			wantISA:     100.0,
		},
		{
			name:        "all_503",
			uasScenario: "uas_unavailable.xml",
			uacScenario: "uac_unavailable.xml",
			callCount:   50,
			wantISA:     100.0,
		},
		{
			name:        "all_200",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   50,
			wantISA:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			env := newTestEnv(ctx, t)

			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			isa := getISA(t, env.endpoint)
			t.Logf("ISA = %.2f (want %.2f)", isa, tt.wantISA)
			require.Equal(t, tt.wantISA, isa)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestISA_Mixed tests 50% 200 OK + 50% 503 → ISA = 50%.
// 503 is ineffective, 200 is effective.
func TestISA_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 25, env)
	runSippScenario(ctx, t, "uas_unavailable.xml", "uac_unavailable.xml", 25, env)

	isa := getISA(t, env.endpoint)
	t.Logf("ISA = %.2f (want %.2f)", isa, 50.0)
	require.Equal(t, 50.0, isa)

	waitForSessionsZero(t, env.endpoint)
}

// TestISA_MixedWith3xx tests 50% 302 Redirect + 50% 500 Server Error → ISA = 50%.
// Unlike SER/SEER, 3xx are NOT excluded from ISA denominator.
// ISA = 25 / 50 × 100 = 50%
func TestISA_MixedWith3xx(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 25, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 25, env)

	isa := getISA(t, env.endpoint)
	t.Logf("ISA = %.2f (want %.2f)", isa, 50.0)
	require.Equal(t, 50.0, isa)

	waitForSessionsZero(t, env.endpoint)
}

// TestISA_Complex tests mixed effective and ineffective codes.
// 20×200 OK + 15×500 Server Error + 15×503 Service Unavailable → ISA = (15+15)/50 × 100 = 60%.
func TestISA_Complex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 20, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 15, env)
	runSippScenario(ctx, t, "uas_unavailable.xml", "uac_unavailable.xml", 15, env)

	isa := getISA(t, env.endpoint)
	t.Logf("ISA = %.2f (want %.2f)", isa, 60.0)
	require.Equal(t, 60.0, isa)

	waitForSessionsZero(t, env.endpoint)
}

// TestISA_WithCarrierConfig verifies ISA per-carrier.
func TestISA_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 50, env)

	isa := env.getISAByCarrier(t)
	t.Logf("ISA{carrier=%q} = %.2f (want %.2f)", env.carrier, isa, 100.0)
	require.Equal(t, 100.0, isa)

	env.waitForSessionsZeroByCarrier(t)
}

// TestISA_MixedWithCarrierConfig verifies ISA per-carrier for mixed traffic.
func TestISA_MixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 25, env)
	runSippScenario(ctx, t, "uas_unavailable.xml", "uac_unavailable.xml", 25, env)

	isa := env.getISAByCarrier(t)
	t.Logf("ISA{carrier=%q} = %.2f (want %.2f)", env.carrier, isa, 50.0)
	require.Equal(t, 50.0, isa)

	env.waitForSessionsZeroByCarrier(t)
}
