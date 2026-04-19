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
	tests := []struct {
		name    string
		uas     string
		uac     string
		count   int
		wantNER float64
	}{
		{
			name:    "100_percent",
			uas:     "uas_100.xml",
			uac:     "uac_100.xml",
			count:   50,
			wantNER: 100.0,
		},
		{
			name:    "0_percent_486",
			uas:     "uas_0.xml",
			uac:     "uac_0.xml",
			count:   50,
			wantNER: 100.0,
		},
		{
			name:    "server_error",
			uas:     "uas_server_error.xml",
			uac:     "uac_server_error.xml",
			count:   50,
			wantNER: 0.0,
		},
		{
			name:    "redirect",
			uas:     "uas_redirect.xml",
			uac:     "uac_redirect.xml",
			count:   50,
			wantNER: 100.0,
		},
		{
			name:    "no_invite",
			uas:     "uas_no_invite.xml",
			uac:     "uac_no_invite.xml",
			count:   50,
			wantNER: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			env := newTestEnv(ctx, t)

			runSippScenario(ctx, t, tt.uas, tt.uac, tt.count, env)
			ner := getNER(t, env.endpoint)
			t.Logf("NER = %.2f (want %.2f)", ner, tt.wantNER)
			require.Equal(t, tt.wantNER, ner)
		})
	}
}

// TestNER_Mixed tests 35 successful + 15 server error (500).
// ineffective = 15, total = 50 → NER = 35/50 × 100 = 70%.

func TestNER_Mixed(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 35, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 15, env)

	ner := getNER(t, env.endpoint)
	t.Logf("NER = %.2f (want %.2f)", ner, 70.0)
	require.Equal(t, 70.0, ner)
}

// TestNER_Equals100MinusISA verifies NER = 100 - ISA.

func TestNER_Equals100MinusISA(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 20, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 15, env)
	runSippScenario(ctx, t, "uas_busy.xml", "uac_busy.xml", 15, env)

	ner := getNER(t, env.endpoint)
	isa := getISA(t, env.endpoint)
	t.Logf("NER = %.2f, ISA = %.2f", ner, isa)
	require.InDelta(t, 100.0-isa, ner, 0.01, "NER must equal 100 - ISA")
}
