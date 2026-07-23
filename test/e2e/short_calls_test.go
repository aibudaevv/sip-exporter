//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestShortCalls_MetricObserved verifies that sip_exporter_short_calls_total
// is incremented for all three thresholds (20/60/180s) when calls complete
// with duration < 20s (the normal case for fast SIPp E2E calls).
//
// Flow: standard uac_100/uas_100 with carriers config — 50 calls, each lasting
// ~100ms (well under 20s). Each call increments thresholds "20", "60", "180".
func TestShortCalls_MetricObserved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnvWithCarriers(ctx, t)

	const callCount = 50
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env)

	for _, threshold := range []string{"20", "60", "180"} {
		labelFilter := fmt.Sprintf(`threshold=%q`, threshold)
		require.True(t,
			metricWithLabelExists(t, env.endpoint, "sip_exporter_short_calls_total", labelFilter),
			"short_calls_total with %s must exist", labelFilter)

		val := getMetricWithLabel(t, env.endpoint, "sip_exporter_short_calls_total", labelFilter)
		t.Logf("short_calls_total{%s, carrier=%q} = %.0f", labelFilter, env.carrier, val)
		require.Equal(t, float64(callCount), val,
			"short_calls_total{%s} must equal callCount (%d) — all calls < 20s", labelFilter, callCount)
	}
}
