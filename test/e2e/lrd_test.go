//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLRD_RegisterRedirect tests LRD histogram with REGISTER 3xx redirect.
// LRD measures delay from REGISTER to 3xx response.
// On loopback: registerTracker keyed by Call-ID → LRD count = unique transactions.

func TestLRD_RegisterRedirect(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "reg_uas_redirect.xml", "reg_uac_redirect.xml", 50, env)

	lrdCount := getLRD(t, env.endpoint)
	t.Logf("LRD count = %.0f (want 50.0)", lrdCount)
	require.Equal(t, 50.0, lrdCount)
}

// TestLRD_Register200OK verifies LRD = 0 for REGISTER 200 OK (RRD measured, not LRD).

func TestLRD_Register200OK(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", 50, env)

	lrdCount := getLRD(t, env.endpoint)
	t.Logf("LRD count = %.0f (want 0.0)", lrdCount)
	require.Equal(t, 0.0, lrdCount)
}

func TestLRD_RegisterError(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "reg_uas_500.xml", "reg_uac_500.xml", 50, env)

	lrdCount := getLRD(t, env.endpoint)
	t.Logf("LRD count = %.0f (want 0.0)", lrdCount)
	require.Equal(t, 0.0, lrdCount)
}

// TestLRD_Mixed tests 25×REGISTER 200 OK + 25×REGISTER redirect.

func TestLRD_Mixed(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", 25, env)
	runSippScenario(ctx, t, "reg_uas_redirect.xml", "reg_uac_redirect.xml", 25, env)

	lrdCount := getLRD(t, env.endpoint)
	t.Logf("LRD count = %.0f (want 25.0)", lrdCount)
	require.Equal(t, 25.0, lrdCount)
}
