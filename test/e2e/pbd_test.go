//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPBD_MetricObserved verifies that sip_exporter_pbd histogram records
// the delay between BYE and 200 OK BYE responses.
//
// Flow: standard uac_100/uas_100 — INVITE → 200 OK → ACK → BYE → 200 OK BYE.
// The BYE → 200 OK BYE delay is near-instantaneous but measurable (> 0 ms).
func TestPBD_MetricObserved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 50, env)

	pbdCount := getMetric(t, env.endpoint, "sip_exporter_pbd_count")
	t.Logf("PBD count = %.0f", pbdCount)
	require.Greater(t, pbdCount, 0.0, "PBD histogram must have observations for completed calls")

	pbdAvg := getPBD(t, env.endpoint)
	t.Logf("PBD avg = %.4f ms", pbdAvg)
	require.Greater(t, pbdAvg, 0.0, "PBD average must be positive — BYE to 200 OK BYE delay > 0")
}
