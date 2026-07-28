//go:build e2e

package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// counterDelta tolerates ±3 packets in multi-scenario tests on loopback,
// where delayed delivery from kernel ringbuf can increment counters after
// waitForMetricStable returns.
const counterDelta = 3.0

const percentScale = 100.0

// S4-2.1: Registration Counters & Ratio

// TestRegisterSuccessCountersAndRatio verifies that 50 successful REGISTER
// transactions (200 OK) produce register_success_total=50 and ratio=100%.
// Also checks active_registrations=1 (all calls share the same AOR).
func TestRegisterSuccessCountersAndRatio(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestEnv(ctx, t)

	const callCount = 50
	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", callCount, env)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_success_total"),
		"metric must exist")
	success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")
	ratio := getMetric(t, env.endpoint, "sip_exporter_register_success_ratio")
	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")

	t.Logf("register=%.0f, success=%.0f, ratio=%.1f%%", registerTotal, success, ratio)

	require.Equal(t, float64(callCount), registerTotal, "one REGISTER per call")
	require.Equal(t, float64(callCount), success, "all registrations successful")
	require.InDelta(t, 100.0, ratio, ratioDelta, "ratio should be 100%% when all succeed")

	require.Eventually(t, func() bool {
		return getMetric(t, env.endpoint, "sip_exporter_active_registrations") == 1.0
	}, 5*time.Second, 500*time.Millisecond,
		"active_registrations should be 1 (all calls share AOR sipp@127.0.0.1)")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestRegisterFailureCodes consolidates terminal-failure and challenge/redirect
// registration tests. Each subtest runs N failed registrations and verifies:
//   - register_success_total absent (0 successes)
//   - register_failure_total{code=X}=N
//   - register_success_ratio=0%
func TestRegisterFailureCodes(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const callCount = 50

	tests := []struct {
		name          string
		uasScenario   string
		uacScenario   string
		failCode      string
		ratioExcluded bool
	}{
		{"403_terminal_failure", "reg_uas_403.xml", "reg_uac_403.xml", "403", false},
		{"500_terminal_failure", "reg_uas_500.xml", "reg_uac_500.xml", "500", false},
		{"401_challenge_excluded", "reg_uas_401.xml", "reg_uac_401.xml", "401", true},
		{"302_redirect_excluded", "reg_uas_redirect.xml", "reg_uac_redirect.xml", "302", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, callCount, env)

			require.False(t, metricExists(t, env.endpoint, "sip_exporter_register_success_total"),
				"no successful registrations")
			success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")
			require.Equal(t, 0.0, success, "no successful registrations")

			labelFilter := `code="` + tt.failCode + `"`
			require.True(t,
				metricWithLabelExists(t, env.endpoint, "sip_exporter_register_failure_total", labelFilter),
				"failure counter for code %s should exist", tt.failCode)
			failure := getMetricWithLabel(t, env.endpoint, "sip_exporter_register_failure_total", labelFilter)
			require.Equal(t, float64(callCount), failure, "all registrations get %s", tt.failCode)

			if tt.ratioExcluded {
				require.False(t, metricExists(t, env.endpoint, "sip_exporter_register_success_ratio"),
					"ratio metric absent (denominator=0, %s excluded)", tt.failCode)
			} else {
				require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_success_ratio"),
					"ratio metric should exist")
				ratio := getMetric(t, env.endpoint, "sip_exporter_register_success_ratio")
				require.InDelta(t, 0.0, ratio, ratioDelta, "ratio should be 0%%")
			}

			assertSelfMonitoringHealthy(t, env.endpoint)
		})
	}
}

// TestRegisterMixedRatio consolidates mixed success+failure ratio tests.
// Each subtest runs 30 successful + 30 failed registrations and verifies
// the ratio formula: success / (success + terminal_failures) * 100.
// Challenges (401) and redirects (302) are excluded from the denominator.
func TestRegisterMixedRatio(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const successCount = 30
	const failCount = 30

	tests := []struct {
		name      string
		failUas   string
		failUac   string
		failCode  string
		denomPart int // terminal failures counted in denominator (0 for challenges/redirects)
	}{
		{"TerminalFailure_50percent", "reg_uas_403.xml", "reg_uac_403.xml", "403", failCount},
		{"Challenge_Still100", "reg_uas_401.xml", "reg_uac_401.xml", "401", 0},
		{"Redirect_Still100", "reg_uas_redirect.xml", "reg_uac_redirect.xml", "302", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)
			runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", successCount, env)
			runSippScenario(ctx, t, tt.failUas, tt.failUac, failCount, env)

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_success_total"),
				"success counter should exist")
			success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")
			require.InDelta(t, float64(successCount), success, counterDelta)

			labelFilter := `code="` + tt.failCode + `"`
			require.True(t,
				metricWithLabelExists(t, env.endpoint, "sip_exporter_register_failure_total", labelFilter),
				"failure counter for code %s should exist", tt.failCode)
			failure := getMetricWithLabel(t, env.endpoint, "sip_exporter_register_failure_total", labelFilter)
			require.InDelta(t, float64(failCount), failure, counterDelta)

			require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_success_ratio"),
				"ratio metric should exist")
			ratio := getMetric(t, env.endpoint, "sip_exporter_register_success_ratio")
			wantRatio := float64(successCount) / float64(successCount+tt.denomPart) * percentScale
			require.InDelta(t, wantRatio, ratio, ratioDelta,
				"%d/(%d+%d) = %.0f%%", successCount, successCount, tt.denomPart, wantRatio)

			assertSelfMonitoringHealthy(t, env.endpoint)
		})
	}
}

// S4-2.2 + S4-1: Active Registrations & Expires

// TestRegisterActiveRegistrations consolidates AOR dedup and multi-AOR tests.
func TestRegisterActiveRegistrations(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantActive  int
	}{
		{"SingleAOR_Dedup", "reg_uas.xml", "reg_uac.xml", 50, 1},
		{"MultipleAORs", "reg_uas.xml", "reg_uac_multi.xml", 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount, env)

			require.Eventually(t, func() bool {
				return getMetric(t, env.endpoint, "sip_exporter_active_registrations") == float64(tt.wantActive)
			}, 5*time.Second, 500*time.Millisecond,
				"active_registrations should be %d", tt.wantActive)

			assertSelfMonitoringHealthy(t, env.endpoint)
		})
	}
}

// TestActiveRegistrationsExpiry verifies the full TTL lifecycle:
// 1. 5 registrations with Expires:3 are stored → active=5
// 2. After expiry + cleanup tick → active=0
func TestActiveRegistrationsExpiry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestEnv(ctx, t)

	const callCount = 5
	runSippScenario(ctx, t, "reg_uas_short_expires.xml", "reg_uac_multi.xml", callCount, env)

	require.Eventually(t, func() bool {
		active := getMetric(t, env.endpoint, "sip_exporter_active_registrations")
		t.Logf("active_registrations before expiry: %.0f", active)
		return active == float64(callCount)
	}, 5*time.Second, 500*time.Millisecond,
		"active_registrations should be %d before expiry", callCount)

	require.Eventually(t, func() bool {
		active := getMetric(t, env.endpoint, "sip_exporter_active_registrations")
		t.Logf("active_registrations after expiry: %.0f", active)
		return active == 0.0
	}, 12*time.Second, 500*time.Millisecond,
		"active_registrations should drop to 0 after Expires:3 + cleanup")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// S4-2.1 + S4-2.2: Carrier Labels

// TestRegisterWithCarrierConfig verifies that all registration metrics carry
// the carrier label when carriers.yaml is configured: success, failure{code},
// ratio, and active_registrations.
func TestRegisterWithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestEnvWithCarriers(ctx, t)

	const successCount = 30
	const failCount = 20
	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", successCount, env)
	runSippScenario(ctx, t, "reg_uas_403.xml", "reg_uac_403.xml", failCount, env)

	success := getMetricWithCarrier(t, env.endpoint,
		"sip_exporter_register_success_total", env.carrier)

	carrierFailLabel := fmt.Sprintf(`carrier="%s",code="403"`, env.carrier)
	require.True(t,
		metricWithLabelExists(t, env.endpoint, "sip_exporter_register_failure_total", carrierFailLabel),
		"failure counter for carrier %s code 403 should exist", env.carrier)
	failure403 := getMetricWithLabel(t, env.endpoint, "sip_exporter_register_failure_total", carrierFailLabel)

	ratio := getMetricWithCarrier(t, env.endpoint,
		"sip_exporter_register_success_ratio", env.carrier)

	t.Logf("carrier=%q: success=%.0f, failure_403=%.0f, ratio=%.1f%%",
		env.carrier, success, failure403, ratio)

	require.InDelta(t, float64(successCount), success, counterDelta, "30 successful registrations with carrier label")
	require.InDelta(t, float64(failCount), failure403, counterDelta, "20 terminal failures with carrier label")
	wantRatio := float64(successCount) / float64(successCount+failCount) * percentScale
	require.InDelta(t, wantRatio, ratio, ratioDelta, "%.0f/(%.0f+%.0f) = %.0f%%", successCount, successCount, failCount, wantRatio)

	require.Eventually(t, func() bool {
		active := getMetricWithCarrier(t, env.endpoint,
			"sip_exporter_active_registrations", env.carrier)
		return active == 1.0
	}, 5*time.Second, 500*time.Millisecond,
		"one active registration with carrier label")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// S4-2.1 Integration: Auth Completion

// TestRegisterAuthCompletion401ChallengeThenSuccess verifies the full
// digest-auth registration flow: REGISTER → 401 Challenge → REGISTER+Auth → 200 OK.
// register_total=2N (initial + retry), failure{code="401"}=N, success=N,
// ratio=100% (401 challenges excluded from denominator), active=1.
func TestRegisterAuthCompletion401ChallengeThenSuccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestEnv(ctx, t)

	const callCount = 10
	runSippScenario(ctx, t, "reg_uas_auth.xml", "reg_uac_auth.xml", callCount, env)

	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_success_total"),
		"success counter should exist")
	success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")

	require.True(t,
		metricWithLabelExists(t, env.endpoint, "sip_exporter_register_failure_total", `code="401"`),
		"failure counter for 401 should exist")
	failure401 := getMetricWithLabel(t, env.endpoint,
		"sip_exporter_register_failure_total", `code="401"`)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_success_ratio"),
		"ratio metric should exist")
	ratio := getMetric(t, env.endpoint, "sip_exporter_register_success_ratio")

	t.Logf("register=%.0f, success=%.0f, failure_401=%.0f, ratio=%.1f%%",
		registerTotal, success, failure401, ratio)

	require.InDelta(t, float64(callCount*2), registerTotal, counterDelta,
		"2 REGISTERs per call: initial + auth retry")
	require.InDelta(t, float64(callCount), success, counterDelta, "one successful 200 OK per call")
	require.InDelta(t, float64(callCount), failure401, counterDelta, "one 401 challenge per call")
	require.InDelta(t, 100.0, ratio, ratioDelta,
		"ratio should be 100%% because 401 challenges excluded from denominator")

	require.Eventually(t, func() bool {
		return getMetric(t, env.endpoint, "sip_exporter_active_registrations") == 1.0
	}, 5*time.Second, 500*time.Millisecond,
		"one active registration (AOR user@127.0.0.1)")

	assertSelfMonitoringHealthy(t, env.endpoint)
}
