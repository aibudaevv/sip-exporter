//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTTR(t *testing.T) {
	type sippRun struct {
		uas, uac string
		count    int
		uacOnly  bool
	}

	tests := []struct {
		name      string
		carrier   bool
		scenarios []sippRun
		wantTTR   bool
	}{
		{"SuccessfulCalls", false, []sippRun{{"uas_100.xml", "uac_100.xml", 50, false}}, true},
		{"BusyCalls", false, []sippRun{{"uas_0.xml", "uac_0.xml", 50, false}}, true},
		{"ConcurrentCalls", false, []sippRun{{"uas_100.xml", "uac_100.xml", 100, false}}, true},
		{"MixedScenarios", false, []sippRun{{"uas_100.xml", "uac_100.xml", 30, false}, {"uas_0.xml", "uac_0.xml", 20, false}}, true},
		{"WithCarrierConfig", true, []sippRun{{"uas_100.xml", "uac_100.xml", 50, false}}, true},
		{"RegisterScenario", false, []sippRun{{"reg_uas.xml", "reg_uac.xml", 50, false}}, false},
		{"TimeoutNoResponse", false, []sippRun{{"", "uac_100.xml", 5, true}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			var env *testEnv
			if tt.carrier {
				env = newTestEnvWithCarriers(ctx, t)
			} else {
				env = newTestEnv(ctx, t)
			}

			for _, s := range tt.scenarios {
				if s.uacOnly {
					runSippUACOnly(ctx, t, s.uac, s.count, env)
					require.True(t, metricExists(t, env.endpoint, "sip_exporter_invite_total"),
						"timeout scenario must emit INVITE traffic")
					require.Greater(t, getMetric(t, env.endpoint, "sip_exporter_invite_total"), 0.0,
						"timeout scenario must emit at least one INVITE")
				} else {
					runSippScenario(ctx, t, s.uas, s.uac, s.count, env)
				}
			}

			if tt.wantTTR {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_ttr_count"),
					"TTR histogram should exist: %s", tt.name)
			} else {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_ttr_count"),
					"TTR histogram should be absent: %s", tt.name)
			}

			if tt.carrier {
				carrierLabel := `carrier="` + env.carrier + `"`
				require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_ttr_count", carrierLabel),
					"TTR histogram should exist for carrier %q", env.carrier)
				ttr := env.getTTRByCarrier(t)
				t.Logf("TTR{carrier=%q} = %.2f ms", env.carrier, ttr)
				require.Greater(t, ttr, 0.0, "TTR should be > 0: %s", tt.name)
				env.assertDialogTeardownByCarrier(t)
			} else if tt.wantTTR {
				ttr := getTTR(t, env.endpoint)
				t.Logf("TTR = %.2f ms", ttr)
				require.Greater(t, ttr, 0.0, "TTR should be > 0: %s", tt.name)
				require.Greater(t, getMetric(t, env.endpoint, "sip_exporter_ttr_count"), 0.0,
					"TTR histogram should have observations: %s", tt.name)
				assertDialogTeardown(t, env.endpoint)
			}
		})
	}
}
