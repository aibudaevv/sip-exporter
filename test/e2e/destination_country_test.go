//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDestinationCountry verifies destination_country label on invite_total
// and invite_200_total for three LookupDestination branches:
//   - E.164 prefix match (+7495… → RU)
//   - local fallback (domestic number + LOCAL_COUNTRY_CODE=RU → RU)
//   - unknown fallback (domestic number, no LOCAL_COUNTRY_CODE → "unknown")
//
// For each subtest we verify the expected label value is present and the
// alternative is absent (0) — this proves the label resolves to the correct
// country, not just that traffic flowed.
func TestDestinationCountry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		uacScenario  string
		localCountry string
		wantCountry  string
		notWantLabel string
	}{
		{
			name:         "E164_RU",
			uacScenario:  "uac_e164_ru.xml",
			localCountry: "",
			wantCountry:  "RU",
			notWantLabel: `destination_country="unknown"`,
		},
		{
			name:         "LocalFallback_RU",
			uacScenario:  "uac_e164_domestic.xml",
			localCountry: "RU",
			wantCountry:  "RU",
			notWantLabel: `destination_country="unknown"`,
		},
		{
			name:         "Unknown_NoLocalCode",
			uacScenario:  "uac_e164_domestic.xml",
			localCountry: "",
			wantCountry:  "unknown",
			notWantLabel: `destination_country="RU"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			extraEnv := map[string]string{}
			if tt.localCountry != "" {
				extraEnv["SIP_EXPORTER_LOCAL_COUNTRY_CODE"] = tt.localCountry
			}

			env := newTestEnvWithExtraEnv(ctx, t, "", extraEnv)

			runSippScenario(ctx, t, "uas_100.xml", tt.uacScenario, 100, env)

			wantLabel := `destination_country="` + tt.wantCountry + `"`

			inviteOK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", wantLabel)
			inviteBad := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", tt.notWantLabel)
			t.Logf("invite_total{%s}=%.0f, invite_total{%s}=%.0f", wantLabel, inviteOK, tt.notWantLabel, inviteBad)
			require.InDelta(t, 100.0, inviteOK, ratioDelta, "invite_total should carry %s", wantLabel)
			require.LessOrEqual(t, inviteBad, 3.0, "invite_total should NOT carry %s", tt.notWantLabel)

			invite200OK := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", wantLabel)
			invite200Bad := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", tt.notWantLabel)
			t.Logf("invite_200_total{%s}=%.0f, invite_200_total{%s}=%.0f", wantLabel, invite200OK, tt.notWantLabel, invite200Bad)
			require.InDelta(t, 100.0, invite200OK, ratioDelta, "invite_200_total should carry %s", wantLabel)
			require.LessOrEqual(t, invite200Bad, 3.0, "invite_200_total should NOT carry %s", tt.notWantLabel)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}
