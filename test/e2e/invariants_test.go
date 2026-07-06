//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRFC6076_Invariants verifies cross-metric invariants defined in RFC 6076:
//   - SEER ≥ SER (always)
//   - SCR ≤ SER (always)
//   - All ratios ∈ [0, 100]
//
// Traffic mix: 100×200 OK + 50×486 Busy + 50×500 Server Error (total 200 INVITE, no 3xx).
// Expected: SER=50%, SEER=75%, SCR=50%, NER=75%.
func TestRFC6076_Invariants(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 50, env)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 50, env)

	ser := getSER(t, env.endpoint)
	seer := getSEER(t, env.endpoint)
	scr := getSCR(t, env.endpoint)
	asr := getASR(t, env.endpoint)
	ner := getNER(t, env.endpoint)

	t.Logf("SER=%.2f SEER=%.2f SCR=%.2f ASR=%.2f NER=%.2f", ser, seer, scr, asr, ner)

	require.InDelta(t, 50.0, ser, ratioDelta)
	require.InDelta(t, 75.0, seer, ratioDelta)
	require.InDelta(t, 50.0, scr, ratioDelta)
	require.InDelta(t, 75.0, ner, ratioDelta)

	require.GreaterOrEqual(t, seer, ser, "SEER must be >= SER (RFC 6076)")
	require.LessOrEqual(t, scr, ser, "SCR must be <= SER (RFC 6076)")

	for _, v := range []float64{ser, seer, scr, asr, ner} {
		require.GreaterOrEqual(t, v, 0.0, "ratio must be >= 0")
		require.LessOrEqual(t, v, 100.0, "ratio must be <= 100")
	}

	waitForSessionsZero(t, env.endpoint)
}
