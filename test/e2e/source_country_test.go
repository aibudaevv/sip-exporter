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
func TestSourceCountry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		carriersYAMLFile string
		wantCountry      string
	}{
		{
			name:             "CarrierCountry_RU",
			carriersYAMLFile: "carriers_country.yaml",
			wantCountry:      "RU",
		},
		{
			name:             "Unknown_NoCarrierCountry",
			carriersYAMLFile: "carriers.yaml",
			wantCountry:      "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			carriersYAML := loadCarriersYAML(t, tt.carriersYAMLFile)
			env := newTestEnvWithCarriersYAML(ctx, t, carriersYAML, "loopback-carrier")

			runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)

			label := `source_country="` + tt.wantCountry + `"`

			invite := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", label)
			t.Logf("invite_total{%s}=%.0f", label, invite)
			require.Greater(t, invite, 0.0, "invite_total should have %s", label)

			ser := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", label)
			t.Logf("ser{%s}=%.2f", label, ser)
			require.Greater(t, ser, 0.0, "SER should have %s", label)

			scr := getMetricWithLabel(t, env.endpoint, "sip_exporter_scr", label)
			t.Logf("scr{%s}=%.2f", label, scr)
			require.Greater(t, scr, 0.0, "SCR should have %s", label)

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
// Expected: invite_total{source_country="RU"} > 0, invite_total{source_country="US"} > 0.
func TestSourceCountry_PerCarrier(t *testing.T) {
	ctx := context.Background()
	setupSecondaryIPs(t)

	carriersYAML := loadCarriersYAML(t, "carriers_direction_country.yaml")
	env := newTestEnvWithCarriersYAML(ctx, t, carriersYAML, "carrier-A")

	runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", 100, env, "10.2.0.1", "10.1.0.1")
	runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", 100, env, "10.1.0.1", "10.2.0.1")

	inviteRU := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `source_country="RU"`)
	inviteUS := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `source_country="US"`)
	t.Logf("invite_total{source_country=\"RU\"}=%.0f, invite_total{source_country=\"US\"}=%.0f", inviteRU, inviteUS)
	require.Greater(t, inviteRU, 0.0, "carrier-A (RU) should have INVITEs")
	require.Greater(t, inviteUS, 0.0, "carrier-B (US) should have INVITEs")

	assertSelfMonitoringHealthy(t, env.endpoint)
}
