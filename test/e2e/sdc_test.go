//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSDCAllScenarios(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantSDC     float64
	}{
		{"all_completed", "uas_100.xml", "uac_100.xml", 100, 100.0},
		{"rejected_486", "uas_0.xml", "uac_0.xml", 100, 0.0},
		{"server_error", "uas_server_error.xml", "uac_server_error.xml", 100, 0.0},
		{"redirect", "uas_redirect.xml", "uac_redirect.xml", 100, 0.0},
		{"no_invite", "uas_no_invite.xml", "uac_no_invite.xml", 100, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			if tt.wantSDC > 0 {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_sdc_total"),
					"SDC metric should exist when sessions complete")
				sdc := getSDC(t, env.endpoint)
				t.Logf("SDC = %.0f (want %.0f)", sdc, tt.wantSDC)
				require.Equal(t, float64(tt.callCount), sdc)
			} else {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_sdc_total"),
					"SDC metric should be absent when no sessions complete")
			}

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

func TestSDCMixed(t *testing.T) {
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
	}{
		{"Mixed", []sippRun{{"uas_100.xml", "uac_100.xml", 140}, {"uas_0.xml", "uac_0.xml", 60}}, 140},
		{"MixedWith3xx", []sippRun{{"uas_redirect.xml", "uac_redirect.xml", 100}, {"uas_100.xml", "uac_100.xml", 100}}, 100},
		{"Complex", []sippRun{{"uas_100.xml", "uac_100.xml", 80}, {"uas_busy.xml", "uac_busy.xml", 60}, {"uas_server_error.xml", "uac_server_error.xml", 60}}, 80},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)

			for _, r := range tt.runs {
				runSippScenario(ctx, t, r.uas, r.uac, r.count, env)
			}

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_sdc_total"),
				"SDC metric should exist")
			sdc := getSDC(t, env.endpoint)
			t.Logf("SDC = %.0f (want %.0f)", sdc, float64(tt.completedCount))
			require.Equal(t, float64(tt.completedCount), sdc)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

func TestSDCSessionExpires(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestEnv(ctx, t)

	sdcBefore := getSDC(t, env.endpoint)
	t.Logf("Before: SDC = %.0f", sdcBefore)

	const callCount = 10
	runSippScenario(ctx, t, "uas_short_expires.xml", "uac_short_expires.xml", callCount, env)

	require.Eventually(t, func() bool {
		return getMetric(t, env.endpoint, "sip_exporter_sessions") == 0
	}, 15*time.Second, 500*time.Millisecond, "sessions did not expire within timeout")

	sdcAfter := getSDC(t, env.endpoint)
	t.Logf("After: SDC = %.0f", sdcAfter)

	require.Equal(t, sdcBefore+float64(callCount), sdcAfter, "SDC should increase by %d after Session-Expires timeout", callCount)
}

func TestSDCWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const callCount = 200
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env)

	sdc := env.getSDCByCarrier(t)
	t.Logf("SDC{carrier=%q} = %.0f (want %.0f)", env.carrier, sdc, float64(callCount))
	require.Equal(t, float64(callCount), sdc)

	env.waitForSessionsZeroByCarrier(t)
}

func TestSDCMixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const completedCount = 140
	const failCount = 60
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", completedCount, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", failCount, env)

	sdc := env.getSDCByCarrier(t)
	t.Logf("SDC{carrier=%q} = %.0f (want %.0f)", env.carrier, sdc, float64(completedCount))
	require.Equal(t, float64(completedCount), sdc)

	env.waitForSessionsZeroByCarrier(t)
}
