//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

const fraudScanWindow = "60s"
const fraudBurstWindow = "60s"

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

// TestFraud_RegisterScan_TriggersThreshold verifies that register_scan_total
// increments when unique AORs from a single IP exceed the threshold.
func TestFraud_RegisterScan_TriggersThreshold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupSecondaryIPs(t)

	env := fraudEnv(ctx, t, carriersFraudYAML, map[string]string{
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD": "5",
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW":    fraudScanWindow,
	})

	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_multi.xml", 8, env, "10.1.0.1", "10.1.0.1")

	scanTotal := getMetricWithCarrier(t, env.endpoint, "sip_exporter_register_scan_total", "carrier-A")
	require.Equal(t, 4.0, scanTotal, "register_scan_total should be 4 (8 AORs - threshold 5 + 1)")
}

// TestFraud_RegisterScan_BelowThreshold verifies no signal when below threshold.
func TestFraud_RegisterScan_BelowThreshold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupSecondaryIPs(t)

	env := fraudEnv(ctx, t, carriersFraudYAML, map[string]string{
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD": "5",
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW":    fraudScanWindow,
	})

	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_multi.xml", 3, env, "10.1.0.1", "10.1.0.1")

	require.False(t, metricExists(t, env.endpoint, "sip_exporter_register_scan_total"),
		"register_scan_total should not be emitted below threshold (no counter child created)")
}

// TestFraud_RegisterScan_IncrementsPerAOR verifies that register_scan_total
// increments for each AOR at or above the threshold.
// 16 calls with threshold=5: counter = 16 - 5 + 1 = 12.
func TestFraud_RegisterScan_IncrementsPerAOR(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupSecondaryIPs(t)

	env := fraudEnv(ctx, t, carriersFraudYAML, map[string]string{
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD": "5",
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW":    fraudScanWindow,
	})

	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_multi.xml", 16, env, "10.1.0.1", "10.1.0.1")

	scanTotal := getMetricWithCarrier(t, env.endpoint, "sip_exporter_register_scan_total", "carrier-A")
	require.Equal(t, 12.0, scanTotal, "register_scan_total should be 12 (16 AORs - threshold 5 + 1)")
}

// TestFraud_RegisterScan_HighVolume verifies that a high volume of unique
// AORs (10x threshold) produces the expected counter value and the exporter
// remains healthy. register_scan_total = 50 - 5 + 1 = 46.
func TestFraud_RegisterScan_HighVolume(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupSecondaryIPs(t)

	env := fraudEnv(ctx, t, carriersFraudYAML, map[string]string{
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD": "5",
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW":    fraudScanWindow,
	})

	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_multi.xml", 50, env, "10.1.0.1", "10.1.0.1")

	scanTotal := getMetricWithCarrier(t, env.endpoint, "sip_exporter_register_scan_total", "carrier-A")
	require.Equal(t, 46.0, scanTotal,
		"register_scan_total must be 46 (50 AORs - threshold 5 + 1)")
}

// TestFraud_RegisterScan_MultipleIPsNoTrigger verifies that registrations
// from multiple IPs (each below threshold) do NOT aggregate into a false
// positive. This is a regression guard for per-IP source IP threading: if
// the threading broke (all AORs attributed to one server IP), 6 unique AORs
// would exceed threshold=5 and falsely signal.
func TestFraud_RegisterScan_MultipleIPsNoTrigger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupSecondaryIPs(t)

	env := fraudEnv(ctx, t, carriersFraudYAML, map[string]string{
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD": "5",
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW":    fraudScanWindow,
	})

	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_multi.xml", 3, env, "10.1.0.1", "10.1.0.1")
	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_multi.xml", 3, env, "10.2.0.1", "10.2.0.1")

	require.False(t, metricExists(t, env.endpoint, "sip_exporter_register_scan_total"),
		"no IP crossed threshold — metric must be absent")
}

// TestFraud_RegisterScan_PerIPCrossThreshold verifies that when one IP crosses
// the threshold and another does not, only the crossing IP's carrier signals.
func TestFraud_RegisterScan_PerIPCrossThreshold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupSecondaryIPs(t)

	env := fraudEnv(ctx, t, carriersFraudYAML, map[string]string{
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD": "5",
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW":    fraudScanWindow,
	})

	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_multi.xml", 5, env, "10.1.0.1", "10.1.0.1")
	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_multi.xml", 3, env, "10.2.0.1", "10.2.0.1")

	scanA := getMetricWithCarrier(t, env.endpoint, "sip_exporter_register_scan_total", "carrier-A")
	require.Equal(t, 1.0, scanA, "carrier-A crossed threshold — exactly 1 (5 AORs - threshold 5 + 1)")

	scanB := getMetricWithCarrier(t, env.endpoint, "sip_exporter_register_scan_total", "carrier-B")
	require.Zero(t, scanB, "carrier-B below threshold — no signal (counter child absent, returns 0)")
}

// ---------------------------------------------------------------------------
// Country Change (S6-9.2)
// ---------------------------------------------------------------------------

// TestFraud_CountryChange_DifferentCountry verifies that register_country_change_total
// increments when the same AOR re-registers from a different source country.
func TestFraud_CountryChange_DifferentCountry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupSecondaryIPs(t)

	env := fraudEnv(ctx, t, carriersFraudYAML, nil)

	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_fixed.xml", 1, env, "10.1.0.1", "10.1.0.1")
	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_fixed.xml", 1, env, "10.2.0.1", "10.2.0.1")

	changeTotal := getMetric(t, env.endpoint, "sip_exporter_register_country_change_total")
	require.Equal(t, 1.0, changeTotal,
		"register_country_change_total should be exactly 1 after country change")
}

// TestFraud_CountryChange_SameCountry verifies no signal when same country.
func TestFraud_CountryChange_SameCountry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupSecondaryIPs(t)

	env := fraudEnv(ctx, t, carriersFraudYAML, nil)

	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_fixed.xml", 1, env, "10.1.0.1", "10.1.0.1")
	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_fixed.xml", 1, env, "10.1.0.1", "10.1.0.1")

	require.False(t, metricExists(t, env.endpoint, "sip_exporter_register_country_change_total"),
		"register_country_change_total should not be emitted when country unchanged")
}

// ---------------------------------------------------------------------------
// INVITE Burst (S6-9.3)
// ---------------------------------------------------------------------------

// TestFraud_InviteBurst_TriggersThreshold verifies that invite_burst_total
// increments for each INVITE at or above the threshold.
// 10 INVITEs with threshold=5: counter = 10 - 5 + 1 = 6.
func TestFraud_InviteBurst_TriggersThreshold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupSecondaryIPs(t)

	env := fraudEnv(ctx, t, carriersFraudYAML, map[string]string{
		"SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD": "5",
		"SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW":    fraudBurstWindow,
	})

	runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", 10, env, "10.1.0.1", "10.1.0.1")

	burstTotal := getMetricWithCarrier(t, env.endpoint, "sip_exporter_invite_burst_total", "carrier-A")
	require.Equal(t, 6.0, burstTotal, "invite_burst_total should be 6 (10 INVITEs - threshold 5 + 1)")
}

// TestFraud_InviteBurst_BelowThreshold verifies no signal when below threshold.
func TestFraud_InviteBurst_BelowThreshold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupSecondaryIPs(t)

	env := fraudEnv(ctx, t, carriersFraudYAML, map[string]string{
		"SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD": "5",
		"SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW":    fraudBurstWindow,
	})

	runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", 3, env, "10.1.0.1", "10.1.0.1")

	require.False(t, metricExists(t, env.endpoint, "sip_exporter_invite_burst_total"),
		"invite_burst_total should not be emitted below threshold (no counter child created)")
}

// ---------------------------------------------------------------------------
// Sessions Utilization (S6-9.4)
// ---------------------------------------------------------------------------

// TestSessionsUtilization_BelowLimit verifies sessions_limit and sessions_utilization
// metrics are present with correct values when sessions_limits config is provided.
func TestSessionsUtilization_BelowLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	carriersYAML := loadCarriersYAML(t, "carriers.yaml")
	sessionsLimitsYAML := `sessions_limits:
  - carrier: "loopback-carrier"
    limit: 100
`
	env := newTestEnvWithFraudConfig(ctx, t, carriersYAML, sessionsLimitsYAML, nil)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 5, env)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_sessions_limit"),
		"sessions_limit metric should exist")
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_sessions_utilization"),
		"sessions_utilization metric should exist")

	limit := getMetricWithCarrier(t, env.endpoint, "sip_exporter_sessions_limit", "loopback-carrier")
	require.Equal(t, 100.0, limit, "sessions_limit should be 100")

	util := getMetricWithCarrier(t, env.endpoint, "sip_exporter_sessions_utilization", "loopback-carrier")
	require.LessOrEqual(t, util, 5.0,
		"utilization should be at most 5%% (5 calls / limit 100, allowing timing overlap on last call)")
}

// TestSessionsUtilization_NoLimit verifies metrics are absent without config.
func TestSessionsUtilization_NoLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 3, env)

	require.False(t, metricExists(t, env.endpoint, "sip_exporter_sessions_limit"),
		"sessions_limit metric should be absent without config")
	require.False(t, metricExists(t, env.endpoint, "sip_exporter_sessions_utilization"),
		"sessions_utilization metric should be absent without config")
}
