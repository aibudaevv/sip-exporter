//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSourceCountry verifies source_country label behavior on loopback.
//
// On loopback (127.0.0.1), GeoIP is useless (private IP) — source_country is
// determined solely by carrier.country precedence:
//   - carrier with country field → that ISO code
//   - carrier without country, no GeoIP DB → "unknown"
//
// For each subtest we verify the expected label value is present and the
// alternative is absent (0) — this proves the label is set correctly, not
// just that traffic flowed.
func TestSourceCountry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		carriersYAMLFile string
		wantCountry      string
		notWantCountry   string
	}{
		{
			name:             "CarrierCountry_RU",
			carriersYAMLFile: "carriers_country.yaml",
			wantCountry:      "RU",
			notWantCountry:   "unknown",
		},
		{
			name:             "Unknown_NoCarrierCountry",
			carriersYAMLFile: "carriers.yaml",
			wantCountry:      "unknown",
			notWantCountry:   "RU",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			carriersYAML := loadCarriersYAML(t, tt.carriersYAMLFile)
			env := newTestEnvWithCarriersYAML(ctx, t, carriersYAML, "loopback-carrier")

			runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)

			wantLabel := `source_country="` + tt.wantCountry + `"`
			notWantLabel := `source_country="` + tt.notWantCountry + `"`

			inviteOK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", wantLabel)
			inviteBad := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", notWantLabel)
			t.Logf("invite_total{%s}=%.0f, invite_total{%s}=%.0f", wantLabel, inviteOK, notWantLabel, inviteBad)
			require.InDelta(t, 100.0, inviteOK, ratioDelta, "invite_total should carry %s", wantLabel)
			require.LessOrEqual(t, inviteBad, 3.0, "invite_total should NOT carry %s", notWantLabel)

			serOK := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", wantLabel)
			serBad := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", notWantLabel)
			t.Logf("ser{%s}=%.2f, ser{%s}=%.2f", wantLabel, serOK, notWantLabel, serBad)
			require.InDelta(t, 100.0, serOK, ratioDelta, "SER should carry %s", wantLabel)
			require.LessOrEqual(t, serBad, 3.0, "SER should NOT carry %s", notWantLabel)

			scrOK := getMetricWithLabel(t, env.endpoint, "sip_exporter_scr", wantLabel)
			scrBad := getMetricWithLabel(t, env.endpoint, "sip_exporter_scr", notWantLabel)
			t.Logf("scr{%s}=%.2f, scr{%s}=%.2f", wantLabel, scrOK, notWantLabel, scrBad)
			require.InDelta(t, 100.0, scrOK, ratioDelta, "SCR should carry %s", wantLabel)
			require.LessOrEqual(t, scrBad, 3.0, "SCR should NOT carry %s", notWantLabel)

			env.waitForSessionsZeroByCarrier(t)
		})
	}
}

// TestSourceCountry_PerCarrier verifies that two carriers with different country
// fields produce separate source_country labels.
//
// Setup: carriers_direction_country.yaml defines carrier-A (10.1.0.0/16, country=RU)
// and carrier-B (10.2.0.0/16, country=US). Secondary IPs are added to loopback.
//
// Session 1: UAC=10.1.0.1 (carrier-A, RU) → UAS=10.2.0.1 → 200 OK.
// Session 2: UAC=10.2.0.1 (carrier-B, US) → UAS=10.1.0.1 → 200 OK.
//
// Expected: invite_total{source_country="RU"}=100, invite_total{source_country="US"}=100.
func TestSourceCountry_PerCarrier(t *testing.T) {
	ctx := context.Background()
	setupSecondaryIPs(t)

	carriersYAML := loadCarriersYAML(t, "carriers_direction_country.yaml")
	env := newTestEnvWithCarriersYAML(ctx, t, carriersYAML, "carrier-A")

	runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", 100, env, "10.2.0.1", "10.1.0.1")
	runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", 100, env, "10.1.0.1", "10.2.0.1")

	inviteRU := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `source_country="RU"`)
	inviteUS := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `source_country="US"`)
	inviteUnknown := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `source_country="unknown"`)
	t.Logf("RU=%.0f, US=%.0f, unknown=%.0f", inviteRU, inviteUS, inviteUnknown)
	require.InDelta(t, 100.0, inviteRU, ratioDelta, "carrier-A (RU) should have ~100 INVITEs")
	require.InDelta(t, 100.0, inviteUS, ratioDelta, "carrier-B (US) should have ~100 INVITEs")
	require.LessOrEqual(t, inviteUnknown, 3.0, "no traffic should have source_country=unknown when carriers have country fields")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestSourceCountry_GeoIP verifies that source_country is resolved from a
// GeoIP country database when the source IP is not covered by carrier.country.
//
// Uses the open-source MaxMind test database (GeoIP2-Country-Test.mmdb) which
// maps 81.2.69.142 → GB. No carrier config — carrier resolves to "other",
// so source_country comes solely from GeoIP.
//
// Expected: invite_total{source_country="GB"}=100, {source_country="unknown"}=0.
func TestSourceCountry_GeoIP(t *testing.T) {
	ctx := context.Background()
	setupSecondaryIPs(t)
	addLoopbackIP(t, "81.2.69.142/32")

	geoipDBPath := filepath.Join(projectRoot, "test", "e2e", "data", "GeoIP2-Country-Test.mmdb")
	env := newTestEnvWithGeoIP(ctx, t, geoipDBPath)

	runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", 100, env, "127.0.0.1", "81.2.69.142")

	inviteGB := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `source_country="GB"`)
	inviteUnknown := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `source_country="unknown"`)
	t.Logf("invite_total{GB}=%.0f, invite_total{unknown}=%.0f", inviteGB, inviteUnknown)
	require.InDelta(t, 100.0, inviteGB, ratioDelta, "GeoIP should resolve 81.2.69.142 to GB")
	require.LessOrEqual(t, inviteUnknown, 3.0, "no traffic should have source_country=unknown when GeoIP resolves")

	serGB := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", `source_country="GB"`)
	t.Logf("ser{GB}=%.2f", serGB)
	require.InDelta(t, 100.0, serGB, ratioDelta, "SER should carry source_country=GB")

	assertSelfMonitoringHealthy(t, env.endpoint)
}
