//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSEERAllScenarios(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantSEER    float64
	}{
		{"all_200", "uas_100.xml", "uac_100.xml", 100, 100.0},
		{"all_486", "uas_0.xml", "uac_0.xml", 100, 100.0},
		{"all_480", "uas_busy.xml", "uac_busy.xml", 100, 100.0},
		{"all_603", "uas_decline.xml", "uac_decline.xml", 100, 100.0},
		{"all_600", "uas_decline_600.xml", "uac_decline_600.xml", 100, 100.0},
		{"all_500", "uas_server_error.xml", "uac_server_error.xml", 100, 0.0},
		{"redirect_only", "uas_redirect.xml", "uac_redirect.xml", 100, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_seer"))
			seer := getSEER(t, env.endpoint)
			t.Logf("SEER = %.2f (want %.2f)", seer, tt.wantSEER)
			require.InDelta(t, tt.wantSEER, seer, ratioDelta)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

func TestSEERMixed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	type sippRun struct {
		uas, uac string
		count    int
	}

	tests := []struct {
		name           string
		runs           []sippRun
		effectiveCount int
		denomCount     int
	}{
		{"MixedEffective", []sippRun{{"uas_100.xml", "uac_100.xml", 100}, {"uas_busy.xml", "uac_busy.xml", 100}}, 200, 200},
		{"MixedWithErrors", []sippRun{{"uas_100.xml", "uac_100.xml", 100}, {"uas_server_error.xml", "uac_server_error.xml", 100}}, 100, 200},
		{"Mixed3xx", []sippRun{{"uas_redirect.xml", "uac_redirect.xml", 100}, {"uas_100.xml", "uac_100.xml", 100}}, 100, 100},
		{"Complex", []sippRun{{"uas_100.xml", "uac_100.xml", 80}, {"uas_busy.xml", "uac_busy.xml", 60}, {"uas_server_error.xml", "uac_server_error.xml", 60}}, 140, 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)

			for _, r := range tt.runs {
				runSippScenario(ctx, t, r.uas, r.uac, r.count, env)
			}

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_seer"))
			seer := getSEER(t, env.endpoint)
			wantSEER := float64(tt.effectiveCount) / float64(tt.denomCount) * percentScale
			t.Logf("SEER = %.2f (want %.2f)", seer, wantSEER)
			require.InDelta(t, wantSEER, seer, ratioDelta)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

func TestSEERWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const callCount = 100
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env)
	runSippScenario(ctx, t, "uas_busy.xml", "uac_busy.xml", callCount, env)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_seer"))
	seer := env.getSEERByCarrier(t)
	t.Logf("SEER{carrier=%q} = %.2f (want %.2f)", env.carrier, seer, 100.0)
	require.InDelta(t, 100.0, seer, ratioDelta)

	env.waitForSessionsZeroByCarrier(t)
}
