//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSPD_SuccessfulCalls(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)
	time.Sleep(2 * time.Second)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 50)

	time.Sleep(3 * time.Second)

	spd := getSPD(t, endpoint)
	t.Logf("SPD = %.4f seconds", spd)
	require.Greater(t, spd, 0.0, "SPD should be greater than 0 after successful calls")
}

func TestSPD_NoCompletedCalls(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)
	time.Sleep(2 * time.Second)

	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 50)

	time.Sleep(3 * time.Second)

	spd := getSPD(t, endpoint)
	t.Logf("SPD = %.4f seconds", spd)
	require.Equal(t, 0.0, spd, "SPD should be 0 when no sessions completed")
}

func TestSPD_Mixed(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)
	time.Sleep(2 * time.Second)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 30)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 20)

	time.Sleep(3 * time.Second)

	spd := getSPD(t, endpoint)
	t.Logf("SPD = %.4f seconds", spd)
	require.Greater(t, spd, 0.0, "SPD should be greater than 0 when some sessions completed")
}
