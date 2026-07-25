//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMethodCounters_FullCallFlow verifies that request method counters are
// incremented correctly for a full INVITE → 200 OK → ACK → BYE → 200 OK flow.
func TestMethodCounters_FullCallFlow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const callCount = 100
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env)

	tests := []struct {
		name       string
		metricName string
	}{
		{"invite_total", "sip_exporter_invite_total"},
		{"invite_200_total", "sip_exporter_invite_200_total"},
		{"ack_total", "sip_exporter_ack_total"},
		{"bye_total", "sip_exporter_bye_total"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := getMetric(t, env.endpoint, tt.metricName)
			t.Logf("%s = %.0f (want %.0f)", tt.metricName, val, float64(callCount))
			require.Equal(t, float64(callCount), val)
		})
	}

	waitForSessionsZero(t, env.endpoint)
}

// TestMethodCounters_Options verifies the OPTIONS request counter.
func TestMethodCounters_Options(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const callCount = 50
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_no_invite.xml", "uac_no_invite.xml", callCount, env)

	optionsTotal := getMetric(t, env.endpoint, "sip_exporter_options_total")
	t.Logf("options_total = %.0f (want %.0f)", optionsTotal, float64(callCount))
	require.Equal(t, float64(callCount), optionsTotal)
}

// TestMethodCounters_OtherMethods verifies counters for SUBSCRIBE, UPDATE, INFO,
// REFER, PRACK, and MESSAGE requests.
func TestMethodCounters_OtherMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const callCount = 50

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		metricName  string
	}{
		{"subscribe", "uas_subscribe.xml", "uac_subscribe.xml", "sip_exporter_subscribe_total"},
		{"update", "uas_update.xml", "uac_update.xml", "sip_exporter_update_total"},
		{"info", "uas_info.xml", "uac_info.xml", "sip_exporter_info_total"},
		{"refer", "uas_refer.xml", "uac_refer.xml", "sip_exporter_refer_total"},
		{"prack", "uas_prack.xml", "uac_prack.xml", "sip_exporter_prack_total"},
		{"message", "uas_message.xml", "uac_message.xml", "sip_exporter_message_total"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, callCount, env)

			val := getMetric(t, env.endpoint, tt.metricName)
			t.Logf("%s = %.0f (want %.0f)", tt.metricName, val, float64(callCount))
			require.Equal(t, float64(callCount), val)
		})
	}
}
