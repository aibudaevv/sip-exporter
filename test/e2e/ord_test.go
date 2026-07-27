//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestORD(t *testing.T) {
	type sippRun struct {
		uas, uac string
		count    int
	}

	tests := []struct {
		name      string
		carrier   bool
		scenarios []sippRun
		wantCount int
	}{
		{"OptionsPing", false, []sippRun{{"uas_no_invite.xml", "uac_no_invite.xml", 50}}, 50},
		{"NoOptions", false, []sippRun{{"uas_100.xml", "uac_100.xml", 50}}, 0},
		{"MixedWithOptions", false, []sippRun{{"uas_100.xml", "uac_100.xml", 25}, {"uas_no_invite.xml", "uac_no_invite.xml", 25}}, 25},
		{"WithCarrierConfig", true, []sippRun{{"uas_no_invite.xml", "uac_no_invite.xml", 50}}, 50},
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

			if tt.wantCount == 0 {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_ord_count"),
					"ORD histogram should be absent (no OPTIONS)")
			}

			if tt.carrier {
				carrierLabel := `carrier="` + env.carrier + `"`
				require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_ord_count", carrierLabel),
					"ORD histogram should exist for carrier %q", env.carrier)
			}

			var ordCount float64
			if tt.carrier {
				ordCount = env.getORDByCarrier(t)
				t.Logf("ORD{carrier=%q} count = %.0f", env.carrier, ordCount)
			} else {
				ordCount = getORD(t, env.endpoint)
				t.Logf("ORD count = %.0f", ordCount)
			}
			require.Equal(t, float64(tt.wantCount), ordCount)

			if !tt.carrier {
				waitForSessionsZero(t, env.endpoint)
			}
		})
	}
}
