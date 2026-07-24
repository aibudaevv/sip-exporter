//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNER_AllScenarios(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantNER     float64
	}{
		{"100_percent", "uas_100.xml", "uac_100.xml", 100, 100.0},
		{"0_percent_486", "uas_0.xml", "uac_0.xml", 100, 100.0},
		{"server_error", "uas_server_error.xml", "uac_server_error.xml", 100, 0.0},
		{"redirect", "uas_redirect.xml", "uac_redirect.xml", 100, 100.0},
		{"no_invite", "uas_no_invite.xml", "uac_no_invite.xml", 100, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_ner"))
			ner := getNER(t, env.endpoint)
			t.Logf("NER = %.2f (want %.2f)", ner, tt.wantNER)
			require.InDelta(t, tt.wantNER, ner, ratioDelta)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

func TestNER_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const effectiveCount = 140
	const ineffectiveCount = 60
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", effectiveCount, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", ineffectiveCount, env)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_ner"))
	ner := getNER(t, env.endpoint)
	wantNER := float64(effectiveCount) / float64(effectiveCount+ineffectiveCount) * percentScale
	t.Logf("NER = %.2f (want %.2f)", ner, wantNER)
	require.InDelta(t, wantNER, ner, ratioDelta)

	waitForSessionsZero(t, env.endpoint)
}

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
	require.InDelta(t, percentScale-isa, ner, 0.01, "NER must equal 100 - ISA")

	waitForSessionsZero(t, env.endpoint)
}

func TestNER_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const effectiveCount = 140
	const ineffectiveCount = 60
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", effectiveCount, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", ineffectiveCount, env)

	ner := env.getNERByCarrier(t)
	wantNER := float64(effectiveCount) / float64(effectiveCount+ineffectiveCount) * percentScale
	t.Logf("NER{carrier=%q} = %.2f (want %.2f)", env.carrier, ner, wantNER)
	require.InDelta(t, wantNER, ner, ratioDelta)
}
