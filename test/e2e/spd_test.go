//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSPD(t *testing.T) {
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
		{"SuccessfulCalls", false, []sippRun{{"uas_100.xml", "uac_100.xml", 50}}, false},
		{"NoCompletedCalls", false, []sippRun{{"uas_0.xml", "uac_0.xml", 50}}, true},
		{"Mixed", false, []sippRun{{"uas_100.xml", "uac_100.xml", 30}, {"uas_0.xml", "uac_0.xml", 20}}, false},
		{"WithCarrierConfig", true, []sippRun{{"uas_100.xml", "uac_100.xml", 50}}, false},
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
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_spd_count"),
					"SPD histogram should be absent when no sessions completed")
			}

			if tt.carrier {
				carrierLabel := `carrier="` + env.carrier + `"`
				require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_spd_count", carrierLabel),
					"SPD histogram should exist for carrier %q", env.carrier)
				spd := env.getSPDByCarrier(t)
				t.Logf("SPD{carrier=%q} = %.4f seconds", env.carrier, spd)
				require.Greater(t, spd, 0.0, "SPD should be > 0 after successful calls")
				env.waitForSessionsZeroByCarrier(t)
			} else if !tt.wantZero {
				spd := getSPD(t, env.endpoint)
				t.Logf("SPD = %.4f seconds", spd)
				require.Greater(t, spd, 0.0, "SPD should be > 0 after successful calls")
				require.Greater(t, getMetric(t, env.endpoint, "sip_exporter_spd_count"), 0.0,
					"SPD histogram should have observations")
			}
			if !tt.carrier {
				waitForSessionsZero(t, env.endpoint)
			}
		})
	}
}
