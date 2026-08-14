//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSCRAllScenarios(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantSCR     float64
		wantExists  bool
	}{
		{"all_completed", "uas_100.xml", "uac_100.xml", 100, 100.0, true},
		{"none_completed_486", "uas_0.xml", "uac_0.xml", 100, 0.0, true},
		{"none_completed_500", "uas_server_error.xml", "uac_server_error.xml", 100, 0.0, true},
		{"redirect_only", "uas_redirect.xml", "uac_redirect.xml", 100, 0.0, true},
		{"no_invite", "uas_no_invite.xml", "uac_no_invite.xml", 100, 0.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			if tt.wantExists {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_scr"))
				scr := getSCR(t, env.endpoint)
				t.Logf("SCR = %.2f (want %.2f)", scr, tt.wantSCR)
				require.InDelta(t, tt.wantSCR, scr, ratioDelta)
			} else {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_scr"),
					"SCR metric should be absent when no INVITEs")
			}

			assertDialogTeardown(t, env.endpoint)
		})
	}
}

func TestSCRMixed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	type sippRun struct {
		uas, uac string
		count    int
	}

	tests := []struct {
		name           string
		runs           []sippRun
		completedCount int
		denomCount     int
	}{
		{"Mixed", []sippRun{{"uas_100.xml", "uac_100.xml", 140}, {"uas_0.xml", "uac_0.xml", 60}}, 140, 200},
		{"MixedWith3xx", []sippRun{{"uas_redirect.xml", "uac_redirect.xml", 100}, {"uas_100.xml", "uac_100.xml", 100}}, 100, 200},
		{"Complex", []sippRun{{"uas_100.xml", "uac_100.xml", 80}, {"uas_0.xml", "uac_0.xml", 60}, {"uas_server_error.xml", "uac_server_error.xml", 60}}, 80, 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)

			for _, r := range tt.runs {
				runSippScenario(ctx, t, r.uas, r.uac, r.count, env)
			}

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_scr"))
			scr := getSCR(t, env.endpoint)
			wantSCR := float64(tt.completedCount) / float64(tt.denomCount) * percentScale
			t.Logf("SCR = %.2f (want %.2f)", scr, wantSCR)
			require.InDelta(t, wantSCR, scr, ratioDelta)

			assertDialogTeardown(t, env.endpoint)
		})
	}
}

func TestSCRSessionExpires(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestEnv(ctx, t)

	require.False(t, metricExists(t, env.endpoint, "sip_exporter_scr"))
	require.False(t, metricExists(t, env.endpoint, "sip_exporter_sessions"))

	const callCount = 10
	runSippScenario(ctx, t, "uas_short_expires.xml", "uac_short_expires.xml", callCount, env)

	waitForSessionsZero(t, env.endpoint)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_scr"))
	scrAfter := getSCR(t, env.endpoint)
	sessionsAfter := getSessions(t, env.endpoint)
	t.Logf("After: SCR = %.2f, sessions = %.0f", scrAfter, sessionsAfter)

	require.Equal(t, 0.0, sessionsAfter, "sessions should be 0 after Session-Expires timeout")
	require.Equal(t, 100.0, scrAfter, "SCR should be 100%% after Session-Expires timeout")
}

func TestSCRWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const callCount = 200
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_scr"))
	scr := env.getSCRByCarrier(t)
	t.Logf("SCR{carrier=%q} = %.2f (want %.2f)", env.carrier, scr, 100.0)
	require.InDelta(t, 100.0, scr, ratioDelta)

	env.assertDialogTeardownByCarrier(t)
}

func TestSCRMixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const completedCount = 140
	const failCount = 60
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", completedCount, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", failCount, env)

	scr := env.getSCRByCarrier(t)
	wantSCR := float64(completedCount) / float64(completedCount+failCount) * percentScale
	t.Logf("SCR{carrier=%q} = %.2f (want %.2f)", env.carrier, scr, wantSCR)
	require.InDelta(t, wantSCR, scr, ratioDelta)

	env.assertDialogTeardownByCarrier(t)
}
