//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestISAAllScenarios(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantISA     float64
	}{
		{"all_500", "uas_server_error.xml", "uac_server_error.xml", 100, 100.0},
		{"all_503", "uas_unavailable.xml", "uac_unavailable.xml", 100, 100.0},
		{"all_408", "uas_timeout_408.xml", "uac_timeout_408.xml", 100, 100.0},
		{"all_504", "uas_server_error_504.xml", "uac_server_error_504.xml", 100, 100.0},
		{"all_200", "uas_100.xml", "uac_100.xml", 100, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_isa"))
			isa := getISA(t, env.endpoint)
			t.Logf("ISA = %.2f (want %.2f)", isa, tt.wantISA)
			require.InDelta(t, tt.wantISA, isa, ratioDelta)

			assertDialogTeardown(t, env.endpoint)
		})
	}
}

func TestISAMixed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	type sippRun struct {
		uas, uac string
		count    int
	}

	tests := []struct {
		name             string
		runs             []sippRun
		ineffectiveCount int
		denomCount       int
	}{
		{"Mixed", []sippRun{{"uas_100.xml", "uac_100.xml", 100}, {"uas_unavailable.xml", "uac_unavailable.xml", 100}}, 100, 200},
		{"MixedWith3xx", []sippRun{{"uas_redirect.xml", "uac_redirect.xml", 100}, {"uas_server_error.xml", "uac_server_error.xml", 100}}, 100, 200},
		{"Complex", []sippRun{{"uas_100.xml", "uac_100.xml", 80}, {"uas_server_error.xml", "uac_server_error.xml", 60}, {"uas_unavailable.xml", "uac_unavailable.xml", 60}}, 120, 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)

			for _, r := range tt.runs {
				runSippScenario(ctx, t, r.uas, r.uac, r.count, env)
			}

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_isa"))
			isa := getISA(t, env.endpoint)
			wantISA := float64(tt.ineffectiveCount) / float64(tt.denomCount) * percentScale
			t.Logf("ISA = %.2f (want %.2f)", isa, wantISA)
			require.InDelta(t, wantISA, isa, ratioDelta)

			assertDialogTeardown(t, env.endpoint)
		})
	}
}

func TestISAWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const callCount = 200
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", callCount, env)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_isa"))
	isa := env.getISAByCarrier(t)
	t.Logf("ISA{carrier=%q} = %.2f (want %.2f)", env.carrier, isa, 100.0)
	require.InDelta(t, 100.0, isa, ratioDelta)

	env.assertDialogTeardownByCarrier(t)
}

func TestISAMixedWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const ineffectiveCount = 100
	const effectiveCount = 100
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", effectiveCount, env)
	runSippScenario(ctx, t, "uas_unavailable.xml", "uac_unavailable.xml", ineffectiveCount, env)

	isa := env.getISAByCarrier(t)
	wantISA := float64(ineffectiveCount) / float64(ineffectiveCount+effectiveCount) * percentScale
	t.Logf("ISA{carrier=%q} = %.2f (want %.2f)", env.carrier, isa, wantISA)
	require.InDelta(t, wantISA, isa, ratioDelta)

	env.assertDialogTeardownByCarrier(t)
}
