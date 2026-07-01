//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSourceCountry_CarrierCountry verifies that when carriers.yaml defines
// a country field for a carrier, all traffic from that carrier's CIDR gets
// source_country set to that country code.
//
// Setup: carriers_country.yaml maps 127.0.0.0/8 to "loopback-carrier" with country="RU".
// On loopback, GeoIP is useless (private IP), so carrier.country is the only source.
// Expected: source_country="RU" on invite_total, SER, SCR.
func TestSourceCountry_CarrierCountry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	carriersYAML := loadCarriersYAML(t, "carriers_country.yaml")
	env := newTestEnvWithCarriersYAML(ctx, t, carriersYAML, "loopback-carrier")

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)

	inviteRU := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `source_country="RU"`)
	t.Logf("invite_total{source_country=\"RU\"}=%.0f", inviteRU)
	require.Greater(t, inviteRU, 0.0, "invite_total should have source_country=RU from carrier.country")

	serRU := getMetricWithLabel(t, env.endpoint, "sip_exporter_ser", `source_country="RU"`)
	t.Logf("ser{source_country=\"RU\"}=%.2f", serRU)
	require.Greater(t, serRU, 0.0, "SER should have source_country=RU")

	scrRU := getMetricWithLabel(t, env.endpoint, "sip_exporter_scr", `source_country="RU"`)
	t.Logf("scr{source_country=\"RU\"}=%.2f", scrRU)
	require.Greater(t, scrRU, 0.0, "SCR should have source_country=RU")

	env.waitForSessionsZeroByCarrier(t)
}
