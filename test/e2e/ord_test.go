//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestORD_OptionsPing tests ORD histogram with OPTIONS requests.
// ORD measures delay from OPTIONS request to any response.
// On loopback: Call-ID deduplication in tracker → ORD count = unique transactions.
func TestORD_OptionsPing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_no_invite.xml", "uac_no_invite.xml", 50, env)

	ordCount := getORD(t, env.endpoint)
	t.Logf("ORD count = %.0f (want 50.0)", ordCount)
	require.Equal(t, 50.0, ordCount)
}

// TestORD_NoOptions verifies ORD = 0 when no OPTIONS traffic.
func TestORD_NoOptions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 50, env)
	waitForSessionsZero(t, env.endpoint)

	ordCount := getORD(t, env.endpoint)
	t.Logf("ORD count = %.0f (want 0.0)", ordCount)
	require.Equal(t, 0.0, ordCount)
}

// TestORD_MixedWithOptions tests mixed traffic with some OPTIONS.
func TestORD_MixedWithOptions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 25, env)
	runSippScenario(ctx, t, "uas_no_invite.xml", "uac_no_invite.xml", 25, env)
	waitForSessionsZero(t, env.endpoint)

	ordCount := getORD(t, env.endpoint)
	t.Logf("ORD count = %.0f (want 25.0)", ordCount)
	require.Equal(t, 25.0, ordCount)
}

// TestORD_WithCarrierConfig verifies ORD per-carrier.
func TestORD_WithCarrierConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	runSippScenario(ctx, t, "uas_no_invite.xml", "uac_no_invite.xml", 50, env)

	ordCount := env.getORDByCarrier(t)
	t.Logf("ORD{carrier=%q} count = %.0f (want 50.0)", env.carrier, ordCount)
	require.Equal(t, 50.0, ordCount)
}
