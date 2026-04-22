//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusCodes_AllCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		metricName  string
		callCount   int
	}{
		{
			name:        "181_call_forwarding",
			uasScenario: "uas_181.xml",
			uacScenario: "uac_181.xml",
			metricName:  "sip_exporter_181_total",
			callCount:   50,
		},
		{
			name:        "182_queued",
			uasScenario: "uas_182.xml",
			uacScenario: "uac_182.xml",
			metricName:  "sip_exporter_182_total",
			callCount:   50,
		},
		{
			name:        "405_method_not_allowed",
			uasScenario: "uas_405.xml",
			uacScenario: "uac_405.xml",
			metricName:  "sip_exporter_405_total",
			callCount:   50,
		},
		{
			name:        "481_dialog_not_exist",
			uasScenario: "uas_481.xml",
			uacScenario: "uac_481.xml",
			metricName:  "sip_exporter_481_total",
			callCount:   50,
		},
		{
			name:        "487_request_terminated",
			uasScenario: "uas_487.xml",
			uacScenario: "uac_487.xml",
			metricName:  "sip_exporter_487_total",
			callCount:   50,
		},
		{
			name:        "488_not_acceptable_here",
			uasScenario: "uas_488.xml",
			uacScenario: "uac_488.xml",
			metricName:  "sip_exporter_488_total",
			callCount:   50,
		},
		{
			name:        "501_not_implemented",
			uasScenario: "uas_501.xml",
			uacScenario: "uac_501.xml",
			metricName:  "sip_exporter_501_total",
			callCount:   50,
		},
		{
			name:        "502_bad_gateway",
			uasScenario: "uas_502.xml",
			uacScenario: "uac_502.xml",
			metricName:  "sip_exporter_502_total",
			callCount:   50,
		},
		{
			name:        "604_does_not_exist_anywhere",
			uasScenario: "uas_604.xml",
			uacScenario: "uac_604.xml",
			metricName:  "sip_exporter_604_total",
			callCount:   50,
		},
		{
			name:        "606_not_acceptable",
			uasScenario: "uas_606.xml",
			uacScenario: "uac_606.xml",
			metricName:  "sip_exporter_606_total",
			callCount:   50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			env := newTestEnv(ctx, t)

			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			value := getMetric(t, env.endpoint, tt.metricName)
			want := float64(tt.callCount * 2)
			t.Logf("%s = %.0f (want %.0f, loopback doubling)", tt.metricName, value, want)
			require.Equal(t, want, value, "metric %s should equal callCount*2 due to loopback", tt.metricName)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}

func TestStatusCodes_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		metricName  string
		callCount   int
	}{
		{
			name:        "181_call_forwarding",
			uasScenario: "uas_181.xml",
			uacScenario: "uac_181.xml",
			metricName:  "sip_exporter_181_total",
			callCount:   25,
		},
		{
			name:        "487_request_terminated",
			uasScenario: "uas_487.xml",
			uacScenario: "uac_487.xml",
			metricName:  "sip_exporter_487_total",
			callCount:   25,
		},
		{
			name:        "502_bad_gateway",
			uasScenario: "uas_502.xml",
			uacScenario: "uac_502.xml",
			metricName:  "sip_exporter_502_total",
			callCount:   25,
		},
		{
			name:        "604_does_not_exist_anywhere",
			uasScenario: "uas_604.xml",
			uacScenario: "uac_604.xml",
			metricName:  "sip_exporter_604_total",
			callCount:   25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			env := newTestEnvWithCarriers(ctx, t)

			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			value := getMetricWithCarrier(t, env.endpoint, tt.metricName, env.carrier)
			want := float64(tt.callCount * 2)
			t.Logf("%s{carrier=%q} = %.0f (want %.0f)", tt.metricName, env.carrier, value, want)
			require.Equal(t, want, value)

			waitForSessionsZero(t, env.endpoint)
		})
	}
}
