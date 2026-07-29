//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestISSAllScenarios(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantISS     float64
	}{
		{"server_error_500", "uas_server_error.xml", "uac_server_error.xml", 100, 100.0},
		{"unavailable_503", "uas_unavailable.xml", "uac_unavailable.xml", 100, 100.0},
		{"all_200_ok", "uas_100.xml", "uac_100.xml", 100, 0.0},
		{"rejected_486", "uas_0.xml", "uac_0.xml", 100, 0.0},
		{"no_invite", "uas_no_invite.xml", "uac_no_invite.xml", 100, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			if tt.wantISS > 0 {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_iss_total"),
					"ISS metric should exist when server errors occur")
				iss := getISS(t, env.endpoint)
				t.Logf("ISS = %.0f (want %.0f)", iss, tt.wantISS)
				require.Equal(t, float64(tt.callCount), iss)
			} else {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_iss_total"),
					"ISS metric should be absent when no server errors")
			}

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

func TestISSMixed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const successCount = 80
	const busyCount = 60
	const errorCount = 60
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", successCount, env)
	runSippScenario(ctx, t, "uas_busy.xml", "uac_busy.xml", busyCount, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", errorCount, env)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_iss_total"),
		"ISS metric should exist")
	iss := getISS(t, env.endpoint)
	t.Logf("ISS = %.0f (want %.0f)", iss, float64(errorCount))
	require.Equal(t, float64(errorCount), iss)

	waitForSessionsZero(t, env.endpoint)
}

func TestISSWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const callCount = 200
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", callCount, env)

	iss := env.getISSByCarrier(t)
	t.Logf("ISS{carrier=%q} = %.0f (want %.0f)", env.carrier, iss, float64(callCount))
	require.Equal(t, float64(callCount), iss)
}
