//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPDD(t *testing.T) {
	type sippRun struct {
		uas, uac string
		count    int
		uacOnly  bool
	}

	tests := []struct {
		name      string
		carrier   bool
		scenarios []sippRun
		wantPDD   bool
		desc      string
	}{
		{"180Ringing_Measured", false, []sippRun{{"uas_100.xml", "uac_100.xml", 50, false}}, true, "C1=T, C2=T, C3=T"},
		{"ConcurrentCalls", false, []sippRun{{"uas_100.xml", "uac_100.xml", 100, false}}, true, ""},
		{"MixedScenarios", false, []sippRun{{"uas_100.xml", "uac_100.xml", 30, false}, {"uas_100only.xml", "uac_100only.xml", 20, false}}, true, ""},
		{"WithCarrierConfig", true, []sippRun{{"uas_100.xml", "uac_100.xml", 50, false}}, true, ""},
		{"100TryingOnly_NoPDD", false, []sippRun{{"uas_100only.xml", "uac_100only.xml", 50, false}}, false, "C1=F: status != 180"},
		{"181CallForwarded_NoPDD", false, []sippRun{{"uas_181.xml", "uac_181.xml", 50, false}}, false, "C1=F: status != 180"},
		{"182Queued_NoPDD", false, []sippRun{{"uas_182.xml", "uac_182.xml", 50, false}}, false, "C1=F: status != 180"},
		{"BusyNo180_NoPDD", false, []sippRun{{"uas_0.xml", "uac_0.xml", 50, false}}, false, "C1=F: no 180 in flow"},
		{"RegisterOnly_NoPDD", false, []sippRun{{"reg_uas.xml", "reg_uac.xml", 50, false}}, false, "C2=F: not INVITE"},
		{"TimeoutNoResponse_NoPDD", false, []sippRun{{"", "uac_100.xml", 5, true}}, false, "C3=F: no tracker entry"},
		{"NoInviteTraffic_NoPDD", false, []sippRun{{"uas_no_invite.xml", "uac_no_invite.xml", 50, false}}, false, ""},
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
				} else {
					runSippScenario(ctx, t, s.uas, s.uac, s.count, env)
				}
			}

			if tt.wantPDD {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_pdd_count"),
					"PDD histogram should exist: %s", tt.name)
			} else {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_pdd_count"),
					"PDD histogram should be absent: %s (%s)", tt.name, tt.desc)
			}

			if tt.carrier {
				carrierLabel := `carrier="` + env.carrier + `"`
				require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_pdd_count", carrierLabel),
					"PDD histogram should exist for carrier %q", env.carrier)
				pdd := env.getPDDByCarrier(t)
				t.Logf("PDD{carrier=%q} = %.2f ms", env.carrier, pdd)
				require.Greater(t, pdd, 0.0, "PDD should be > 0: %s", tt.name)
				env.waitForSessionsZeroByCarrier(t)
			} else if tt.wantPDD {
				pdd := getPDD(t, env.endpoint)
				t.Logf("PDD = %.2f ms", pdd)
				require.Greater(t, pdd, 0.0, "PDD should be > 0: %s (%s)", tt.name, tt.desc)
				require.Greater(t, getMetric(t, env.endpoint, "sip_exporter_pdd_count"), 0.0,
					"PDD histogram should have observations: %s", tt.name)
				waitForSessionsZero(t, env.endpoint)
			}
		})
	}
}
