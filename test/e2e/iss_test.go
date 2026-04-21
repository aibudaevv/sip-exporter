//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestISS_AllScenarios tests ISS counter with various scenarios.
// ISS counts INVITE responses with 408, 500, 503, 504 status codes.
// On loopback each response is seen twice → ISS doubles.

func TestISS_AllScenarios(t *testing.T) {
	t.Parallel()
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
			callCount:   50,
			wantISS:     100.0,
		},
		{
			name:        "unavailable_503",
			uasScenario: "uas_unavailable.xml",
			uacScenario: "uac_unavailable.xml",
			callCount:   50,
			wantISS:     100.0,
		},
		{
			name:        "all_200_ok",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   50,
			wantISS:     0.0,
		},
		{
			name:        "rejected_486",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   50,
			wantISS:     0.0,
		},
		{
			name:        "no_invite",
			uasScenario: "uas_no_invite.xml",
			uacScenario: "uac_no_invite.xml",
			callCount:   50,
			wantISS:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			env := newTestEnv(ctx, t)

			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)
			iss := getISS(t, env.endpoint)
			t.Logf("ISS = %.0f (want %.0f)", iss, tt.wantISS)
			require.Equal(t, tt.wantISS, iss)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestISS_Mixed tests 20×200 OK + 15×busy (480) + 15×server error (500).
// ISS = 15×2 = 30 (loopback doubles 500 responses).

func TestISS_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 20, env)
	runSippScenario(ctx, t, "uas_busy.xml", "uac_busy.xml", 15, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 15, env)

	iss := getISS(t, env.endpoint)
	t.Logf("ISS = %.0f (want %.0f)", iss, 30.0)
	require.Equal(t, 30.0, iss)

	waitForSessionsZero(t, env.endpoint)
}

// TestISS_WithCarrierConfig verifies ISS per-carrier.
func TestISS_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 50, env)

	iss := env.getISSByCarrier(t)
	t.Logf("ISS{carrier=%q} = %.0f (want 100)", env.carrier, iss)
	require.Equal(t, 100.0, iss)
}
