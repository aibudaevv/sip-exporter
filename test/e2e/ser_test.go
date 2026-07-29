//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSERAllScenarios(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantSER     float64
		wantExists  bool
	}{
		{"100_percent", "uas_100.xml", "uac_100.xml", 100, 100.0, true},
		{"0_percent", "uas_0.xml", "uac_0.xml", 100, 0.0, true},
		{"redirect", "uas_redirect.xml", "uac_redirect.xml", 100, 0.0, true},
		{"no_invite", "uas_no_invite.xml", "uac_no_invite.xml", 100, 0.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			if tt.wantExists {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_ser"))
				ser := getSER(t, env.endpoint)
				t.Logf("SER = %.2f (want %.2f)", ser, tt.wantSER)
				require.InDelta(t, tt.wantSER, ser, ratioDelta)
			} else {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_ser"),
					"SER metric should be absent when no INVITEs")
			}

			assertDialogTeardown(t, env.endpoint)
		})
	}
}

func TestSERMixed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

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
		{
			"Mixed",
			[]sippRun{{"uas_100.xml", "uac_100.xml", 140}, {"uas_0.xml", "uac_0.xml", 60}},
			140, 200,
		},
		{
			"Mixed3xx",
			[]sippRun{{"uas_redirect.xml", "uac_redirect.xml", 100}, {"uas_100.xml", "uac_100.xml", 100}},
			100, 100,
		},
		{
			"Concurrent",
			[]sippRun{
				{"uas_100.xml", "uac_100.xml", 120},
				{"uas_0.xml", "uac_0.xml", 40},
				{"uas_redirect.xml", "uac_redirect.xml", 40},
			},
			120, 160,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)

			for _, r := range tt.runs {
				runSippScenario(ctx, t, r.uas, r.uac, r.count, env)
			}

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_ser"))
			ser := getSER(t, env.endpoint)
			wantSER := float64(tt.successCount) / float64(tt.denomCount) * percentScale
			t.Logf("SER = %.2f (want %.2f)", ser, wantSER)
			require.InDelta(t, wantSER, ser, ratioDelta)

			assertDialogTeardown(t, env.endpoint)
		})
	}
}

func TestSERWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantSER     float64
	}{
		{"100_percent", "uas_100.xml", "uac_100.xml", 100, 100.0},
		{"0_percent", "uas_0.xml", "uac_0.xml", 100, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnvWithCarriers(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_ser"))
			ser := env.getSERByCarrier(t)
			t.Logf("SER{carrier=%q} = %.2f (want %.2f)", env.carrier, ser, tt.wantSER)
			require.InDelta(t, tt.wantSER, ser, ratioDelta)

			env.assertDialogTeardownByCarrier(t)
		})
	}
}

func TestSERMixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestEnvWithCarriers(ctx, t)

	const successCount = 140
	const failCount = 60
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", successCount, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", failCount, env)

	ser := env.getSERByCarrier(t)
	wantSER := float64(successCount) / float64(successCount+failCount) * percentScale
	t.Logf("SER{carrier=%q} = %.2f (want %.2f)", env.carrier, ser, wantSER)
	require.InDelta(t, wantSER, ser, ratioDelta)

	env.assertDialogTeardownByCarrier(t)
}
