//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// counterDelta tolerates ±3 packets in multi-scenario tests on loopback,
// where delayed delivery from kernel ringbuf can increment counters after
// waitForMetricStable returns.
const counterDelta = 3.0

// S4-2.1: Registration Counters & Ratio

// TestRegisterSuccess_CountersAndRatio verifies that 50 successful REGISTER
// transactions (200 OK) produce register_success_total=50 and ratio=100%.
// Also checks active_registrations=1 (all calls share the same AOR).
func TestRegisterSuccess_CountersAndRatio(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
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

// TestRegisterFailure_403_TerminalFailure verifies that 403 Forbidden is counted
// as a terminal failure: register_failure_total{code="403"}=50, ratio=0%.
func TestRegisterFailure_403_TerminalFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const callCount = 50
	runSippScenario(ctx, t, "reg_uas_403.xml", "reg_uac_403.xml", callCount, env)

	success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")
	failure403 := getMetricWithLabel(t, env.endpoint, "sip_exporter_register_failure_total", `code="403"`)
	ratio := getMetric(t, env.endpoint, "sip_exporter_register_success_ratio")

	t.Logf("success=%.0f, failure_403=%.0f, ratio=%.1f%%", success, failure403, ratio)

	require.Equal(t, 0.0, success, "no successful registrations")
	require.Equal(t, float64(callCount), failure403, "all registrations get 403")
	require.InDelta(t, 0.0, ratio, ratioDelta, "ratio should be 0%% for terminal failures only")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestRegisterFailure_500_TerminalFailure verifies that 500 Server Error is
// counted as a terminal failure: register_failure_total{code="500"}=50, ratio=0%.
func TestRegisterFailure_500_TerminalFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const callCount = 50
	runSippScenario(ctx, t, "reg_uas_500.xml", "reg_uac_500.xml", callCount, env)

	success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")
	failure500 := getMetricWithLabel(t, env.endpoint, "sip_exporter_register_failure_total", `code="500"`)
	ratio := getMetric(t, env.endpoint, "sip_exporter_register_success_ratio")

	t.Logf("success=%.0f, failure_500=%.0f, ratio=%.1f%%", success, failure500, ratio)

	require.Equal(t, 0.0, success, "no successful registrations")
	require.Equal(t, float64(callCount), failure500, "all registrations get 500")
	require.InDelta(t, 0.0, ratio, ratioDelta, "ratio should be 0%% for terminal failures only")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestRegisterFailure_401_ChallengeExcludedFromRatio verifies that 401
// digest-auth challenges are counted in register_failure_total{code="401"}
// but excluded from the ratio denominator (ratio=0, denominator=0 → returns 0).
func TestRegisterFailure_401_ChallengeExcludedFromRatio(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const callCount = 50
	runSippScenario(ctx, t, "reg_uas_401.xml", "reg_uac_401.xml", callCount, env)

	success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")
	failure401 := getMetricWithLabel(t, env.endpoint, "sip_exporter_register_failure_total", `code="401"`)
	ratio := getMetric(t, env.endpoint, "sip_exporter_register_success_ratio")

	t.Logf("success=%.0f, failure_401=%.0f, ratio=%.1f%%", success, failure401, ratio)

	require.Equal(t, 0.0, success, "no successful registrations")
	require.Equal(t, float64(callCount), failure401, "401 challenge counted in failure CounterVec")
	require.InDelta(t, 0.0, ratio, ratioDelta,
		"ratio should be 0%% (denominator=0 because 401 excluded)")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestRegisterFailure_302_RedirectExcludedFromRatio verifies that 302 redirects
// are counted in register_failure_total{code="302"} but excluded from the ratio
// denominator (ratio=0, denominator=0 → returns 0).
func TestRegisterFailure_302_RedirectExcludedFromRatio(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const callCount = 50
	runSippScenario(ctx, t, "reg_uas_redirect.xml", "reg_uac_redirect.xml", callCount, env)

	success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")
	failure302 := getMetricWithLabel(t, env.endpoint, "sip_exporter_register_failure_total", `code="302"`)
	ratio := getMetric(t, env.endpoint, "sip_exporter_register_success_ratio")

	t.Logf("success=%.0f, failure_302=%.0f, ratio=%.1f%%", success, failure302, ratio)

	require.Equal(t, 0.0, success, "no successful registrations")
	require.Equal(t, float64(callCount), failure302, "302 redirect counted in failure CounterVec")
	require.InDelta(t, 0.0, ratio, ratioDelta,
		"ratio should be 0%% (denominator=0 because 3xx excluded)")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestRegisterRatio_MixedSuccessAndTerminalFailure verifies ratio=50% when
// 30 successful + 30 terminal-failure (403) registrations are processed.
// Formula: 30 / (30+30) × 100 = 50%.
func TestRegisterRatio_MixedSuccessAndTerminalFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const successCount = 30
	const failCount = 30
	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", successCount, env)
	runSippScenario(ctx, t, "reg_uas_403.xml", "reg_uac_403.xml", failCount, env)

	success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")
	failure403 := getMetricWithLabel(t, env.endpoint, "sip_exporter_register_failure_total", `code="403"`)
	ratio := getMetric(t, env.endpoint, "sip_exporter_register_success_ratio")

	t.Logf("success=%.0f, failure_403=%.0f, ratio=%.1f%%", success, failure403, ratio)

	require.InDelta(t, float64(successCount), success, counterDelta)
	require.InDelta(t, float64(failCount), failure403, counterDelta)
	require.InDelta(t, 50.0, ratio, ratioDelta, "30/(30+30) = 50%%")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestRegisterRatio_MixedSuccessAndChallenge_Still100 verifies that 401
// challenges do NOT lower the ratio: 30 success + 30 challenge → ratio=100%.
// The 401 digest-auth challenge is excluded from the denominator.
func TestRegisterRatio_MixedSuccessAndChallenge_Still100(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const successCount = 30
	const failCount = 30
	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", successCount, env)
	runSippScenario(ctx, t, "reg_uas_401.xml", "reg_uac_401.xml", failCount, env)

	success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")
	failure401 := getMetricWithLabel(t, env.endpoint, "sip_exporter_register_failure_total", `code="401"`)
	ratio := getMetric(t, env.endpoint, "sip_exporter_register_success_ratio")

	t.Logf("success=%.0f, failure_401=%.0f, ratio=%.1f%%", success, failure401, ratio)

	require.InDelta(t, float64(successCount), success, counterDelta)
	require.InDelta(t, float64(failCount), failure401, counterDelta, "401 counted in CounterVec")
	require.InDelta(t, 100.0, ratio, ratioDelta,
		"ratio should stay 100%% because 401 challenges excluded from denominator")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestRegisterRatio_MixedSuccessAndRedirect_Still100 verifies that 3xx
// redirects do NOT lower the ratio: 30 success + 30 redirect → ratio=100%.
func TestRegisterRatio_MixedSuccessAndRedirect_Still100(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const successCount = 30
	const failCount = 30
	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", successCount, env)
	runSippScenario(ctx, t, "reg_uas_redirect.xml", "reg_uac_redirect.xml", failCount, env)

	success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")
	failure302 := getMetricWithLabel(t, env.endpoint, "sip_exporter_register_failure_total", `code="302"`)
	ratio := getMetric(t, env.endpoint, "sip_exporter_register_success_ratio")

	t.Logf("success=%.0f, failure_302=%.0f, ratio=%.1f%%", success, failure302, ratio)

	require.InDelta(t, float64(successCount), success, counterDelta)
	require.InDelta(t, float64(failCount), failure302, counterDelta, "302 counted in CounterVec")
	require.InDelta(t, 100.0, ratio, ratioDelta,
		"ratio should stay 100%% because 3xx redirects excluded from denominator")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// S4-2.2 + S4-1: Active Registrations & Expires

// TestActiveRegistrations_SingleAOR_Dedup verifies that 50 REGISTER calls
// with the same AOR (sipp@127.0.0.1) produce active_registrations=1, not 50.
// storeRegistration overwrites by AOR (refresh), not appends.
func TestActiveRegistrations_SingleAOR_Dedup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", 50, env)

	require.Eventually(t, func() bool {
		return getMetric(t, env.endpoint, "sip_exporter_active_registrations") == 1.0
	}, 5*time.Second, 500*time.Millisecond,
		"active_registrations should be 1 (AOR dedup, not call count)")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestActiveRegistrations_MultipleAORs verifies that 10 REGISTER calls with
// unique AORs (user1@…, user2@…, …) produce active_registrations=10.
func TestActiveRegistrations_MultipleAORs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const callCount = 10
	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac_multi.xml", callCount, env)

	require.Eventually(t, func() bool {
		active := getMetric(t, env.endpoint, "sip_exporter_active_registrations")
		t.Logf("active_registrations = %.0f (want %d)", active, callCount)
		return active == float64(callCount)
	}, 5*time.Second, 500*time.Millisecond,
		"active_registrations should equal call count for unique AORs")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestActiveRegistrations_Expiry verifies the full TTL lifecycle:
// 1. 5 registrations with Expires:3 are stored → active=5
// 2. After expiry + cleanup tick → active=0
// Covers S4-1 (Expires header parsing) and S4-2.2 (cleanupExpiredRegistrations).
func TestActiveRegistrations_Expiry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
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

// TestRegister_WithCarrierConfig verifies that all registration metrics carry
// the carrier label when carriers.yaml is configured: success, failure{code},
// ratio, and active_registrations.
func TestRegister_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	const successCount = 30
	const failCount = 20
	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", successCount, env)
	runSippScenario(ctx, t, "reg_uas_403.xml", "reg_uac_403.xml", failCount, env)

	success := getMetricWithCarrier(t, env.endpoint,
		"sip_exporter_register_success_total", env.carrier)
	failure403 := getMetricWithLabel(t, env.endpoint, "sip_exporter_register_failure_total",
		fmt.Sprintf(`carrier="%s",code="403"`, env.carrier))
	ratio := getMetricWithCarrier(t, env.endpoint,
		"sip_exporter_register_success_ratio", env.carrier)

	t.Logf("carrier=%q: success=%.0f, failure_403=%.0f, ratio=%.1f%%",
		env.carrier, success, failure403, ratio)

	require.InDelta(t, float64(successCount), success, counterDelta, "30 successful registrations with carrier label")
	require.InDelta(t, float64(failCount), failure403, counterDelta, "20 terminal failures with carrier label")
	require.InDelta(t, 60.0, ratio, ratioDelta, "30/(30+20) = 60%%")

	require.Eventually(t, func() bool {
		active := getMetricWithCarrier(t, env.endpoint,
			"sip_exporter_active_registrations", env.carrier)
		return active == 1.0
	}, 5*time.Second, 500*time.Millisecond,
		"one active registration with carrier label")

	assertSelfMonitoringHealthy(t, env.endpoint)
}

// S4-2.1 Integration: Auth Completion

// TestRegisterAuthCompletion_401ChallengeThenSuccess verifies the full
// digest-auth registration flow: REGISTER → 401 Challenge → REGISTER+Auth → 200 OK.
// register_total=2N (initial + retry), failure{code="401"}=N, success=N,
// ratio=100% (401 challenges excluded from denominator), active=1.
func TestRegisterAuthCompletion_401ChallengeThenSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const callCount = 10
	runSippScenario(ctx, t, "reg_uas_auth.xml", "reg_uac_auth.xml", callCount, env)

	registerTotal := getMetric(t, env.endpoint, "sip_exporter_register_total")
	success := getMetric(t, env.endpoint, "sip_exporter_register_success_total")
	failure401 := getMetricWithLabel(t, env.endpoint,
		"sip_exporter_register_failure_total", `code="401"`)
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
