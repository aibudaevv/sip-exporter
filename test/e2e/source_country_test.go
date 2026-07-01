//go:build e2e

package e2e

import (
	"context"
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
			require.Equal(t, 100.0, inviteOK, "invite_total should carry %s", wantLabel)
			require.Equal(t, 0.0, inviteBad, "invite_total should NOT carry %s", notWantLabel)

			serOK := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", wantLabel)
			serBad := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", notWantLabel)
			t.Logf("ser{%s}=%.2f, ser{%s}=%.2f", wantLabel, serOK, notWantLabel, serBad)
			require.Equal(t, 100.0, serOK, "SER should carry %s", wantLabel)
			require.Equal(t, 0.0, serBad, "SER should NOT carry %s", notWantLabel)

			scrOK := getMetricWithLabel(t, env.endpoint, "sip_exporter_scr", wantLabel)
			scrBad := getMetricWithLabel(t, env.endpoint, "sip_exporter_scr", notWantLabel)
			t.Logf("scr{%s}=%.2f, scr{%s}=%.2f", wantLabel, scrOK, notWantLabel, scrBad)
			require.Equal(t, 100.0, scrOK, "SCR should carry %s", wantLabel)
			require.Equal(t, 0.0, scrBad, "SCR should NOT carry %s", notWantLabel)

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
	require.Equal(t, 100.0, inviteRU, "carrier-A (RU) should have exactly 100 INVITEs")
	require.Equal(t, 100.0, inviteUS, "carrier-B (US) should have exactly 100 INVITEs")
	require.Equal(t, 0.0, inviteUnknown, "no traffic should have source_country=unknown when carriers have country fields")

	assertSelfMonitoringHealthy(t, env.endpoint)
}
