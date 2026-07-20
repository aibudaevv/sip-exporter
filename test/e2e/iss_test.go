//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestISS_AllScenarios tests ISS counter with various scenarios.
// ISS counts INVITE responses with 408, 500, 503, 504 status codes.
// IGNORE_OUTGOING=true on lo → each packet seen once → ISS matches call count exactly.

func TestISS_AllScenarios(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantISS     float64
	}{
		{
			name:        "server_error_500",
			uasScenario: "uas_server_error.xml",
			uacScenario: "uac_server_error.xml",
			callCount:   100,
			wantISS:     100.0,
		},
		{
			name:        "unavailable_503",
			uasScenario: "uas_unavailable.xml",
			uacScenario: "uac_unavailable.xml",
			callCount:   100,
			wantISS:     100.0,
		},
		{
			name:        "all_200_ok",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   100,
			wantISS:     0.0,
		},
		{
			name:        "rejected_486",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   100,
			wantISS:     0.0,
		},
		{
			name:        "no_invite",
			uasScenario: "uas_no_invite.xml",
			uacScenario: "uac_no_invite.xml",
			callCount:   100,
			wantISS:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.restart(t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, &env.testEnv)
			iss := getISS(t, env.endpoint)
			t.Logf("ISS = %.0f (want %.0f)", iss, tt.wantISS)
			require.Equal(t, tt.wantISS, iss)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestISS_Mixed tests 80×200 OK + 60×busy (480) + 60×server error (500).
// ISS = 60 / 200 × 100 = 30.0.
func TestISS_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 80, env)
	runSippScenario(ctx, t, "uas_busy.xml", "uac_busy.xml", 60, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 60, env)

	iss := getISS(t, env.endpoint)
	t.Logf("ISS = %.0f (want %.0f)", iss, 60.0)
	require.Equal(t, 60.0, iss)

	waitForSessionsZero(t, env.endpoint)
}

// TestISS_WithCarrierConfig verifies ISS per-carrier.
func TestISS_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 200, env)

	iss := env.getISSByCarrier(t)
	t.Logf("ISS{carrier=%q} = %.0f (want 200)", env.carrier, iss)
	require.Equal(t, 200.0, iss)
}
