//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSPD_SuccessfulCalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 50, env)

	spd := getSPD(t, env.endpoint)
	t.Logf("SPD = %.4f seconds", spd)
	require.Greater(t, spd, 0.0, "SPD should be greater than 0 after successful calls")

	waitForSessionsZero(t, env.endpoint)
}

func TestSPD_NoCompletedCalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 50, env)

	spd := getSPD(t, env.endpoint)
	t.Logf("SPD = %.4f seconds", spd)
	require.Equal(t, 0.0, spd, "SPD should be 0 when no sessions completed")

	waitForSessionsZero(t, env.endpoint)
}

func TestSPD_Mixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 30, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 20, env)

	spd := getSPD(t, env.endpoint)
	t.Logf("SPD = %.4f seconds", spd)
	require.Greater(t, spd, 0.0, "SPD should be greater than 0 when some sessions completed")

	waitForSessionsZero(t, env.endpoint)
}
