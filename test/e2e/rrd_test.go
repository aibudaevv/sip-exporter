//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRRD(t *testing.T) {
	type sippRun struct {
		uas, uac string
		count    int
		uacOnly  bool
	}

	tests := []struct {
		name         string
		carrier      bool
		scenarios    []sippRun
		wantRRD      bool
		statusMetric string
	}{
		{"RegistrationSuccess", false, []sippRun{{"reg_uas.xml", "reg_uac.xml", 50, false}}, true, ""},
		{"Register401", false, []sippRun{{"reg_uas_401.xml", "reg_uac_401.xml", 20, false}}, false, "sip_exporter_401_total"},
		{"Register403", false, []sippRun{{"reg_uas_403.xml", "reg_uac_403.xml", 20, false}}, false, "sip_exporter_403_total"},
		{"Register500", false, []sippRun{{"reg_uas_500.xml", "reg_uac_500.xml", 20, false}}, false, "sip_exporter_500_total"},
		{"RegisterTimeout", false, []sippRun{{"", "reg_uac.xml", 5, true}}, false, ""},
		{"ConcurrentRegistrations", false, []sippRun{{"reg_uas.xml", "reg_uac.xml", 100, false}}, true, ""},
		{"WithCarrierConfig", true, []sippRun{{"reg_uas.xml", "reg_uac.xml", 50, false}}, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			var env *testEnv
			if tt.carrier {
				env = newTestEnvWithCarriers(ctx, t)
			} else {
				env = newTestEnv(ctx, t)
			}

			for _, s := range tt.scenarios {
				if s.uacOnly {
					runSippUACOnly(ctx, t, s.uac, s.count, env)
				} else {
					runSippScenario(ctx, t, s.uas, s.uac, s.count, env)
				}
			}

			registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")
			require.Greater(t, registerTotal, 0.0, "should have REGISTER requests")

			if tt.statusMetric != "" {
				require.True(t, metricExists(t, env.endpoint, tt.statusMetric),
					"%s should exist", tt.statusMetric)
				statusTotal := getMetric(t, env.endpoint, tt.statusMetric)
				require.Greater(t, statusTotal, 0.0, "should have responses: %s", tt.name)
			}

			if !tt.wantRRD {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_rrd_count"),
					"RRD histogram should be absent for non-200 responses: %s", tt.name)
			}

			if tt.carrier {
				carrierLabel := `carrier="` + env.carrier + `"`
				require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_rrd_count", carrierLabel),
					"RRD histogram should exist for carrier %q", env.carrier)
				rrd := env.getRRDByCarrier(t)
				t.Logf("RRD{carrier=%q} = %.2f ms", env.carrier, rrd)
				require.Greater(t, rrd, 0.0, "RRD should be > 0 after successful registrations")
			} else if tt.wantRRD {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_rrd_count"),
					"RRD histogram should exist after successful registrations")
				rrd := getRRD(t, env.endpoint)
				t.Logf("RRD = %.2f ms", rrd)
				require.Greater(t, rrd, 0.0, "RRD should be > 0 after successful registrations")
				require.Greater(t, getMetric(t, env.endpoint, "sip_exporter_rrd_count"), 0.0,
					"RRD histogram should have observations")
			}

			assertSelfMonitoringHealthy(t, env.endpoint)
		})
	}
}
