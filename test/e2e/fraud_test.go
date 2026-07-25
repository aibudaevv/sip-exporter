//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const fraudScanWindow = "60s"
const fraudBurstWindow = "60s"
const scanThreshold = 5
const burstThreshold = 5

// carriersFraudYAML maps 10.1.0.0/16 to carrier-A (country=RU) and
// 10.2.0.0/16 to carrier-B (country=US). Used for tests that need
// per-IP carrier and country resolution.
const carriersFraudYAML = `carriers:
  - name: "carrier-A"
    cidrs:
      - "10.1.0.0/16"
    country: "RU"
  - name: "carrier-B"
    cidrs:
      - "10.2.0.0/16"
    country: "US"
`

func fraudEnv(ctx context.Context, t *testing.T, carriersYAML string, extraEnv map[string]string) *testEnv {
	t.Helper()
	return newTestEnvWithFraudConfig(ctx, t, carriersYAML, "", extraEnv)
}

// ---------------------------------------------------------------------------
// Register Scan (S6-9.1)
// ---------------------------------------------------------------------------

func TestFraud_RegisterScan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	type ipRun struct {
		uas, uac, srcIP, dstIP string
		count                  int
	}

	tests := []struct {
		name  string
		runs  []ipRun
		check func(t *testing.T, endpoint string)
	}{
		{
			"TriggersThreshold",
			[]ipRun{{"reg_uas.xml", "reg_uac_multi.xml", "10.1.0.1", "10.1.0.1", 8}},
			func(t *testing.T, ep string) {
				const callCount = 8
				scan := getMetricWithCarrier(t, ep, "sip_exporter_register_scan_total", "carrier-A")
				require.Equal(t, float64(callCount-scanThreshold+1), scan)
			},
		},
		{
			"BelowThreshold",
			[]ipRun{{"reg_uas.xml", "reg_uac_multi.xml", "10.1.0.1", "10.1.0.1", 3}},
			func(t *testing.T, ep string) {
				require.False(t, metricExists(t, ep, "sip_exporter_register_scan_total"),
					"register_scan_total should not be emitted below threshold")
			},
		},
		{
			"IncrementsPerAOR",
			[]ipRun{{"reg_uas.xml", "reg_uac_multi.xml", "10.1.0.1", "10.1.0.1", 16}},
			func(t *testing.T, ep string) {
				const callCount = 16
				scan := getMetricWithCarrier(t, ep, "sip_exporter_register_scan_total", "carrier-A")
				require.Equal(t, float64(callCount-scanThreshold+1), scan)
			},
		},
		{
			"HighVolume",
			[]ipRun{{"reg_uas.xml", "reg_uac_multi.xml", "10.1.0.1", "10.1.0.1", 50}},
			func(t *testing.T, ep string) {
				const callCount = 50
				scan := getMetricWithCarrier(t, ep, "sip_exporter_register_scan_total", "carrier-A")
				require.Equal(t, float64(callCount-scanThreshold+1), scan)
			},
		},
		{
			"MultipleIPsNoTrigger",
			[]ipRun{
				{"reg_uas.xml", "reg_uac_multi.xml", "10.1.0.1", "10.1.0.1", 3},
				{"reg_uas.xml", "reg_uac_multi.xml", "10.2.0.1", "10.2.0.1", 3},
			},
			func(t *testing.T, ep string) {
				require.False(t, metricExists(t, ep, "sip_exporter_register_scan_total"),
					"no IP crossed threshold — metric must be absent")
			},
		},
		{
			"PerIPCrossThreshold",
			[]ipRun{
				{"reg_uas.xml", "reg_uac_multi.xml", "10.1.0.1", "10.1.0.1", 5},
				{"reg_uas.xml", "reg_uac_multi.xml", "10.2.0.1", "10.2.0.1", 3},
			},
			func(t *testing.T, ep string) {
				const carrierACount = 5
				scanA := getMetricWithCarrier(t, ep, "sip_exporter_register_scan_total", "carrier-A")
				require.Equal(t, float64(carrierACount-scanThreshold+1), scanA,
					"carrier-A crossed threshold")

				require.False(t,
					metricWithLabelExists(t, ep, "sip_exporter_register_scan_total", `carrier="carrier-B"`),
					"carrier-B below threshold — no signal")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			setupSecondaryIPs(t)

			env := fraudEnv(ctx, t, carriersFraudYAML, map[string]string{
				"SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD": "5",
				"SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW":    fraudScanWindow,
			})

			for _, r := range tt.runs {
				runSippScenarioWithIPs(ctx, t, r.uas, r.uac, r.count, env, r.srcIP, r.dstIP)
			}

			tt.check(t, env.endpoint)
		})
	}
}

// ---------------------------------------------------------------------------
// Country Change (S6-9.2)
// ---------------------------------------------------------------------------

func TestFraud_CountryChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name       string
		secondSrc  string
		secondDst  string
		wantChange bool
	}{
		{"DifferentCountry", "10.2.0.1", "10.2.0.1", true},
		{"SameCountry", "10.1.0.1", "10.1.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			setupSecondaryIPs(t)

			env := fraudEnv(ctx, t, carriersFraudYAML, nil)

			runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_fixed.xml", 1, env, "10.1.0.1", "10.1.0.1")
			runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_fixed.xml", 1, env, tt.secondSrc, tt.secondDst)

			if tt.wantChange {
				changeTotal := getMetric(t, env.endpoint, "sip_exporter_register_country_change_total")
				require.Equal(t, 1.0, changeTotal,
					"register_country_change_total should be exactly 1 after country change")
			} else {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_register_country_change_total"),
					"register_country_change_total should not be emitted when country unchanged")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// INVITE Burst (S6-9.3)
// ---------------------------------------------------------------------------

func TestFraud_InviteBurst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name       string
		callCount  int
		wantAbsent bool
	}{
		{"TriggersThreshold", 10, false},
		{"BelowThreshold", 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			setupSecondaryIPs(t)

			env := fraudEnv(ctx, t, carriersFraudYAML, map[string]string{
				"SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD": "5",
				"SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW":    fraudBurstWindow,
			})

			runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", tt.callCount, env, "10.1.0.1", "10.1.0.1")

			if tt.wantAbsent {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_invite_burst_total"),
					"invite_burst_total should not be emitted below threshold")
			} else {
				burst := getMetricWithCarrier(t, env.endpoint, "sip_exporter_invite_burst_total", "carrier-A")
				require.Equal(t, float64(tt.callCount-burstThreshold+1), burst)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Sessions Utilization (S6-9.4)
// ---------------------------------------------------------------------------

func TestSessionsUtilization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name      string
		hasConfig bool
		callCount int
		limit     int
	}{
		{"BelowLimit", true, 5, 100},
		{"NoLimit", false, 3, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var env *testEnv
			if tt.hasConfig {
				carriersYAML := loadCarriersYAML(t, "carriers.yaml")
				sessionsLimitsYAML := `sessions_limits:
  - carrier: "loopback-carrier"
    limit: ` + fmt.Sprintf("%d", tt.limit) + `
`
				env = newTestEnvWithFraudConfig(ctx, t, carriersYAML, sessionsLimitsYAML, nil)
			} else {
				env = newTestEnv(ctx, t)
			}

			runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", tt.callCount, env)

			if tt.hasConfig {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_sessions_limit"),
					"sessions_limit metric should exist")
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_sessions_utilization"),
					"sessions_utilization metric should exist")

				limit := getMetricWithCarrier(t, env.endpoint, "sip_exporter_sessions_limit", "loopback-carrier")
				require.Equal(t, float64(tt.limit), limit)

				util := getMetricWithCarrier(t, env.endpoint, "sip_exporter_sessions_utilization", "loopback-carrier")
				require.LessOrEqual(t, util, float64(tt.callCount),
					"utilization should be at most %d calls", tt.callCount)
			} else {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_sessions_limit"),
					"sessions_limit metric should be absent without config")
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_sessions_utilization"),
					"sessions_utilization metric should be absent without config")
			}
		})
	}
}
