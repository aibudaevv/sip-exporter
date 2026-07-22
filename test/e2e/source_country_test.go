//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSourceCountry verifies source_country label behavior under all
// resolution mechanisms:
//   - carrier.country → ISO code (loopback, carrier-A/B with secondary IPs)
//   - no carrier.country, no GeoIP → "unknown" (loopback)
//   - GeoIP country DB → ISO code from source IP (secondary IP 81.2.69.142 → GB)
//
// On loopback (127.0.0.1), GeoIP is useless (private IP) — source_country is
// determined solely by carrier.country precedence. Secondary IPs allow testing
// carrier matching by network range and GeoIP resolution simultaneously.
//
// Each subtest runs an independent exporter instance. For each subtest we
// verify the expected label value is present and the alternative is absent (0)
// — this proves the label is set correctly, not just that traffic flowed.
func TestSourceCountry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description      string
		carriersYAMLFile string // "" → GeoIP mode
		uacIP            string // "" → loopback
		uasIP            string
		wantCountry      string
		notWantCountry   string
	}{
		{
			description:      "carrier country=RU, loopback",
			carriersYAMLFile: "carriers_country.yaml",
			wantCountry:      "RU",
			notWantCountry:   "unknown",
		},
		{
			description:      "no carrier country, loopback",
			carriersYAMLFile: "carriers.yaml",
			wantCountry:      "unknown",
			notWantCountry:   "RU",
		},
		{
			description:      "per-carrier: carrier-A (10.1.0.0/16) country=RU",
			carriersYAMLFile: "carriers_direction_country.yaml",
			uacIP:            "10.1.0.1",
			uasIP:            "10.2.0.1",
			wantCountry:      "RU",
			notWantCountry:   "unknown",
		},
		{
			description:      "per-carrier: carrier-B (10.2.0.0/16) country=US",
			carriersYAMLFile: "carriers_direction_country.yaml",
			uacIP:            "10.2.0.1",
			uasIP:            "10.1.0.1",
			wantCountry:      "US",
			notWantCountry:   "unknown",
		},
		{
			description:    "GeoIP resolves 81.2.69.142 to GB",
			uacIP:          "81.2.69.142",
			uasIP:          "127.0.0.1",
			wantCountry:    "GB",
			notWantCountry: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			callCount := 100

			if tt.uacIP != "" {
				setupSecondaryIPs(t)
			}

			var env *testEnv
			if tt.carriersYAMLFile != "" {
				carriersYAML := loadCarriersYAML(t, tt.carriersYAMLFile)
				env = newTestEnvWithCarriersYAML(ctx, t, carriersYAML, "loopback-carrier")
			} else {
				addLoopbackIP(t, "81.2.69.142/32")
				geoipDBPath := filepath.Join(projectRoot, "test", "e2e", "data", "GeoIP2-Country-Test.mmdb")
				env = newTestEnvWithGeoIP(ctx, t, geoipDBPath)
			}

			if tt.uacIP != "" {
				runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env, tt.uasIP, tt.uacIP)
			} else {
				runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env)
			}

			wantLabel := `source_country="` + tt.wantCountry + `"`
			notWantLabel := `source_country="` + tt.notWantCountry + `"`

			require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_invite_total", wantLabel),
				"sip_exporter_invite_total{%s} should exist", wantLabel)
			inviteOK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", wantLabel)
			inviteBad := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", notWantLabel)
			t.Logf("invite_total{%s}=%.0f, invite_total{%s}=%.0f", wantLabel, inviteOK, notWantLabel, inviteBad)
			require.InDelta(t, float64(callCount), inviteOK, ratioDelta, "invite_total should carry %s", wantLabel)
			require.LessOrEqual(t, inviteBad, 3.0, "invite_total should NOT carry %s", notWantLabel)

			require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_ser", wantLabel),
				"sip_exporter_ser{%s} should exist", wantLabel)
			serOK := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", wantLabel)
			serBad := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", notWantLabel)
			t.Logf("ser{%s}=%.2f, ser{%s}=%.2f", wantLabel, serOK, notWantLabel, serBad)
			require.InDelta(t, float64(callCount), serOK, ratioDelta, "SER should carry %s", wantLabel)
			require.LessOrEqual(t, serBad, 3.0, "SER should NOT carry %s", notWantLabel)

			require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_scr", wantLabel),
				"sip_exporter_scr{%s} should exist", wantLabel)
			scrOK := getMetricWithLabel(t, env.endpoint, "sip_exporter_scr", wantLabel)
			scrBad := getMetricWithLabel(t, env.endpoint, "sip_exporter_scr", notWantLabel)
			t.Logf("scr{%s}=%.2f, scr{%s}=%.2f", wantLabel, scrOK, notWantLabel, scrBad)
			require.InDelta(t, float64(callCount), scrOK, ratioDelta, "SCR should carry %s", wantLabel)
			require.LessOrEqual(t, scrBad, 3.0, "SCR should NOT carry %s", notWantLabel)

			assertSelfMonitoringHealthy(t, env.endpoint)
		})
	}
}
