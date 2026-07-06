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
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

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
			callCount:   100,
			wantISA:     100.0,
		},
		{
			name:        "all_503",
			uasScenario: "uas_unavailable.xml",
			uacScenario: "uac_unavailable.xml",
			callCount:   100,
			wantISA:     100.0,
		},
		{
			name:        "all_408",
			uasScenario: "uas_timeout_408.xml",
			uacScenario: "uac_timeout_408.xml",
			callCount:   100,
			wantISA:     100.0,
		},
		{
			name:        "all_504",
			uasScenario: "uas_server_error_504.xml",
			uacScenario: "uac_server_error_504.xml",
			callCount:   100,
			wantISA:     100.0,
		},
		{
			name:        "all_200",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   100,
			wantISA:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.restart(t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, &env.testEnv)

			isa := getISA(t, env.endpoint)
			t.Logf("ISA = %.2f (want %.2f)", isa, tt.wantISA)
			require.True(t, metricExists(t, env.endpoint, "sip_exporter_isa"))
			require.InDelta(t, tt.wantISA, isa, ratioDelta)

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

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)
	runSippScenario(ctx, t, "uas_unavailable.xml", "uac_unavailable.xml", 100, env)

	isa := getISA(t, env.endpoint)
	t.Logf("ISA = %.2f (want %.2f)", isa, 50.0)
	require.InDelta(t, 50.0, isa, ratioDelta)

	waitForSessionsZero(t, env.endpoint)
}

// TestISA_MixedWith3xx tests 50% 302 Redirect + 50% 500 Server Error → ISA = 50%.
// Unlike SER/SEER, 3xx are NOT excluded from ISA denominator.
// ISA = 100 / 200 × 100 = 50%
func TestISA_MixedWith3xx(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 100, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 100, env)

	isa := getISA(t, env.endpoint)
	t.Logf("ISA = %.2f (want %.2f)", isa, 50.0)
	require.InDelta(t, 50.0, isa, ratioDelta)

	waitForSessionsZero(t, env.endpoint)
}

// TestISA_Complex tests mixed effective and ineffective codes.
// 80×200 OK + 60×500 Server Error + 60×503 Service Unavailable → ISA = (60+60)/200 × 100 = 60%.
func TestISA_Complex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 80, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 60, env)
	runSippScenario(ctx, t, "uas_unavailable.xml", "uac_unavailable.xml", 60, env)

	isa := getISA(t, env.endpoint)
	t.Logf("ISA = %.2f (want %.2f)", isa, 60.0)
	require.InDelta(t, 60.0, isa, ratioDelta)

	waitForSessionsZero(t, env.endpoint)
}

// TestISA_WithCarrierConfig verifies ISA per-carrier.
func TestISA_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 200, env)

	isa := env.getISAByCarrier(t)
	t.Logf("ISA{carrier=%q} = %.2f (want %.2f)", env.carrier, isa, 100.0)
	require.InDelta(t, 100.0, isa, ratioDelta)

	env.waitForSessionsZeroByCarrier(t)
}

// TestISA_MixedWithCarrierConfig verifies ISA per-carrier for mixed traffic.
func TestISA_MixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)
	runSippScenario(ctx, t, "uas_unavailable.xml", "uac_unavailable.xml", 100, env)

	isa := env.getISAByCarrier(t)
	t.Logf("ISA{carrier=%q} = %.2f (want %.2f)", env.carrier, isa, 50.0)
	require.InDelta(t, 50.0, isa, ratioDelta)

	env.waitForSessionsZeroByCarrier(t)
}
