//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestASR_AllScenarios(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantASR     float64
	}{
		{"100_percent", "uas_100.xml", "uac_100.xml", 100, 100.0},
		{"0_percent", "uas_0.xml", "uac_0.xml", 100, 0.0},
		{"redirect", "uas_redirect.xml", "uac_redirect.xml", 100, 0.0},
		{"no_invite", "uas_no_invite.xml", "uac_no_invite.xml", 100, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_asr"))
			asr := getASR(t, env.endpoint)
			t.Logf("ASR = %.2f (want %.2f)", asr, tt.wantASR)
			require.InDelta(t, tt.wantASR, asr, ratioDelta)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

func TestASR_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	type sippRun struct {
		uas, uac string
		count    int
	}

	tests := []struct {
		name         string
		runs         []sippRun
		successCount int
		denomCount   int
	}{
		{"Mixed", []sippRun{{"uas_100.xml", "uac_100.xml", 140}, {"uas_0.xml", "uac_0.xml", 60}}, 140, 200},
		{"MixedWith3xx", []sippRun{{"uas_redirect.xml", "uac_redirect.xml", 100}, {"uas_100.xml", "uac_100.xml", 100}}, 100, 200},
		{"Complex", []sippRun{{"uas_100.xml", "uac_100.xml", 80}, {"uas_busy.xml", "uac_busy.xml", 60}, {"uas_server_error.xml", "uac_server_error.xml", 60}}, 80, 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)

			for _, r := range tt.runs {
				runSippScenario(ctx, t, r.uas, r.uac, r.count, env)
			}

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_asr"))
			asr := getASR(t, env.endpoint)
			wantASR := float64(tt.successCount) / float64(tt.denomCount) * percentScale
			t.Logf("ASR = %.2f (want %.2f)", asr, wantASR)
			require.InDelta(t, wantASR, asr, ratioDelta)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

func TestASR_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const callCount = 200
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_asr"))
	asr := env.getASRByCarrier(t)
	t.Logf("ASR{carrier=%q} = %.2f (want %.2f)", env.carrier, asr, 100.0)
	require.InDelta(t, 100.0, asr, ratioDelta)

	env.waitForSessionsZeroByCarrier(t)
}

func TestASR_MixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const successCount = 140
	const failCount = 60
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", successCount, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", failCount, env)

	asr := env.getASRByCarrier(t)
	wantASR := float64(successCount) / float64(successCount+failCount) * percentScale
	t.Logf("ASR{carrier=%q} = %.2f (want %.2f)", env.carrier, asr, wantASR)
	require.InDelta(t, wantASR, asr, ratioDelta)

	env.waitForSessionsZeroByCarrier(t)
}
