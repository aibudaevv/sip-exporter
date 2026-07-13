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
	require.Equal(t, 1.0, scanTotal, "register_scan_total should be exactly 1 (dedup within same episode)")
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

// TestFraud_RegisterScan_SignalDedup verifies one signal per burst episode.
// 16 calls with threshold=5: signal fires at 5th unique AOR, dedup suppresses
// the remaining 11. register_scan_total must be exactly 1.
func TestFraud_RegisterScan_SignalDedup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupSecondaryIPs(t)

	env := fraudEnv(ctx, t, carriersFraudYAML, map[string]string{
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD": "5",
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW":    fraudScanWindow,
	})

	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_multi.xml", 16, env, "10.1.0.1", "10.1.0.1")

	scanTotal := getMetricWithCarrier(t, env.endpoint, "sip_exporter_register_scan_total", "carrier-A")
	require.Equal(t, 1.0, scanTotal, "register_scan_total should be 1 (dedup within same episode)")
}

// TestFraud_RegisterScan_HighVolumeDedup verifies that a high volume of unique
// AORs (10x threshold) still produces exactly one signal and the exporter
// remains healthy. This is a regression test for the registerScanTracker memory
// cap (S6-A.1): before the fix, the inner map grew unboundedly with each unique
// AOR; after the fix, recording stops once signaled (cap = threshold entries).
func TestFraud_RegisterScan_HighVolumeDedup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setupSecondaryIPs(t)

	env := fraudEnv(ctx, t, carriersFraudYAML, map[string]string{
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD": "5",
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW":    fraudScanWindow,
	})

	runSippScenarioWithIPs(ctx, t, "reg_uas.xml", "reg_uac_multi.xml", 50, env, "10.1.0.1", "10.1.0.1")

	scanTotal := getMetricWithCarrier(t, env.endpoint, "sip_exporter_register_scan_total", "carrier-A")
	require.Equal(t, 1.0, scanTotal,
		"register_scan_total must be exactly 1 even with 50 unique AORs (10x threshold)")
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
// increments when INVITEs from a single IP exceed the threshold.
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
	require.Equal(t, 1.0, burstTotal, "invite_burst_total should be exactly 1 (dedup within same episode)")
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
