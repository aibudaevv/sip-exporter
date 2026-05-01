//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNER_AllScenarios tests NER metric with various scenarios.
// NER = (Total INVITE - ineffective) / Total INVITE × 100 (GSMA IR.42)
// On loopback both numerator and denominator double → NER unchanged (like SER).

func TestNER_AllScenarios(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantNER     float64
	}{
		{
			name:        "100_percent",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   100,
			wantNER:     100.0,
		},
		{
			name:        "0_percent_486",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   100,
			wantNER:     100.0,
		},
		{
			name:        "server_error",
			uasScenario: "uas_server_error.xml",
			uacScenario: "uac_server_error.xml",
			callCount:   100,
			wantNER:     0.0,
		},
		{
			name:        "redirect",
			uasScenario: "uas_redirect.xml",
			uacScenario: "uac_redirect.xml",
			callCount:   100,
			wantNER:     100.0,
		},
		{
			name:        "no_invite",
			uasScenario: "uas_no_invite.xml",
			uacScenario: "uac_no_invite.xml",
			callCount:   100,
			wantNER:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.restart(t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, &env.testEnv)
			ner := getNER(t, env.endpoint)
			t.Logf("NER = %.2f (want %.2f)", ner, tt.wantNER)
			require.Equal(t, tt.wantNER, ner)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// TestNER_Mixed tests 140 successful + 60 server error (500).
// ineffective = 60, total = 200 → NER = 140/200 × 100 = 70%.

func TestNER_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 140, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 60, env)

	ner := getNER(t, env.endpoint)
	t.Logf("NER = %.2f (want %.2f)", ner, 70.0)
	require.Equal(t, 70.0, ner)

	waitForSessionsZero(t, env.endpoint)
}

// TestNER_Equals100MinusISA verifies NER = 100 - ISA.

func TestNER_Equals100MinusISA(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 80, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 60, env)
	runSippScenario(ctx, t, "uas_busy.xml", "uac_busy.xml", 60, env)

	ner := getNER(t, env.endpoint)
	isa := getISA(t, env.endpoint)
	t.Logf("NER = %.2f, ISA = %.2f", ner, isa)
	require.InDelta(t, 100.0-isa, ner, 0.01, "NER must equal 100 - ISA")

	waitForSessionsZero(t, env.endpoint)
}

// TestNER_WithCarrierConfig verifies NER per-carrier.
func TestNER_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 140, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 60, env)

	ner := env.getNERByCarrier(t)
	t.Logf("NER{carrier=%q} = %.2f (want %.2f)", env.carrier, ner, 70.0)
	require.Equal(t, 70.0, ner)
}
