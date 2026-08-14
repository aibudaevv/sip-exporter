//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLRD(t *testing.T) {
	type sippRun struct {
		uas, uac string
		count    int
	}

	tests := []struct {
		name      string
		carrier   bool
		scenarios []sippRun
		wantZero  bool
	}{
		{"RegisterRedirect", false, []sippRun{{"reg_uas_redirect.xml", "reg_uac_redirect.xml", 50}}, false},
		{"Register200OK", false, []sippRun{{"reg_uas.xml", "reg_uac.xml", 50}}, true},
		{"RegisterError", false, []sippRun{{"reg_uas_500.xml", "reg_uac_500.xml", 50}}, true},
		{"Mixed", false, []sippRun{{"reg_uas.xml", "reg_uac.xml", 25}, {"reg_uas_redirect.xml", "reg_uac_redirect.xml", 25}}, false},
		{"WithCarrierConfig", true, []sippRun{{"reg_uas_redirect.xml", "reg_uac_redirect.xml", 50}}, false},
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
				runSippScenario(ctx, t, s.uas, s.uac, s.count, env)
			}

			if tt.wantZero {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_lrd_count"),
					"LRD histogram should be absent (no 3xx redirects)")
			}

			if tt.carrier {
				carrierLabel := `carrier="` + env.carrier + `"`
				require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_lrd_count", carrierLabel),
					"LRD histogram should exist for carrier %q", env.carrier)
				lrdCount := env.getLRDByCarrier(t)
				t.Logf("LRD{carrier=%q} count = %.0f", env.carrier, lrdCount)
				require.Greater(t, lrdCount, 0.0, "LRD count should be > 0 for redirect scenarios")
			} else if !tt.wantZero {
				lrdCount := getLRD(t, env.endpoint)
				t.Logf("LRD count = %.0f", lrdCount)
				require.Greater(t, lrdCount, 0.0, "LRD count should be > 0 for redirect scenarios")
			}

			assertSelfMonitoringHealthy(t, env.endpoint)
		})
	}
}
