//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSEER_AllScenarios tests SEER metric with various single-code scenarios.
// SEER = (INVITE → 200, 480, 486, 600, 603) / (Total INVITE - INVITE → 3xx) × 100
func TestSEER_AllScenarios(t *testing.T) {
	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantSEER    float64
	}{
		{
			name:        "all_200",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   50,
			wantSEER:    100.0,
		},
		{
			name:        "all_486",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   50,
			wantSEER:    100.0,
		},
		{
			name:        "all_480",
			uasScenario: "uas_busy.xml",
			uacScenario: "uac_busy.xml",
			callCount:   50,
			wantSEER:    100.0,
		},
		{
			name:        "all_603",
			uasScenario: "uas_decline.xml",
			uacScenario: "uac_decline.xml",
			callCount:   50,
			wantSEER:    100.0,
		},
		{
			name:        "all_500",
			uasScenario: "uas_server_error.xml",
			uacScenario: "uac_server_error.xml",
			callCount:   50,
			wantSEER:    0.0,
		},
		{
			name:        "redirect_only",
			uasScenario: "uas_redirect.xml",
			uacScenario: "uac_redirect.xml",
			callCount:   50,
			wantSEER:    0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			endpoint := startExporter(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount)

			seer := getSEER(t, endpoint)
			t.Logf("SEER = %.2f (want %.2f)", seer, tt.wantSEER)
			require.Equal(t, tt.wantSEER, seer)

			sessions := getSessions(t, endpoint)
			require.Equal(t, 0.0, sessions, "sessions should be 0 after all calls terminated")
		})
	}
}

// TestSEER_MixedEffective tests 50% 200 OK + 50% 480 Busy Here → SEER = 100%.
// Both codes are "effective" per RFC 6076.
func TestSEER_MixedEffective(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 25)
	runSippScenario(ctx, t, "uas_busy.xml", "uac_busy.xml", 25)

	seer := getSEER(t, endpoint)
	t.Logf("SEER = %.2f (want %.2f)", seer, 100.0)
	require.Equal(t, 100.0, seer)

	sessions := getSessions(t, endpoint)
	require.Equal(t, 0.0, sessions, "sessions should be 0 after all calls terminated")
}

// TestSEER_MixedWithErrors tests 50% 200 OK + 50% 500 Server Error → SEER = 50%.
// 500 is NOT effective, so only 200 OK counts in numerator.
func TestSEER_MixedWithErrors(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 25)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 25)

	seer := getSEER(t, endpoint)
	t.Logf("SEER = %.2f (want %.2f)", seer, 50.0)
	require.Equal(t, 50.0, seer)

	sessions := getSessions(t, endpoint)
	require.Equal(t, 0.0, sessions, "sessions should be 0 after all calls terminated")
}

// TestSEER_Mixed3xx tests 50% 302 Redirect + 50% 200 OK → SEER = 100%.
// 3xx excluded from denominator, all non-3xx are 200 OK.
func TestSEER_Mixed3xx(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)

	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 25)
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 25)

	seer := getSEER(t, endpoint)
	t.Logf("SEER = %.2f (want %.2f)", seer, 100.0)
	require.Equal(t, 100.0, seer)

	sessions := getSessions(t, endpoint)
	require.Equal(t, 0.0, sessions, "sessions should be 0 after all calls terminated")
}

// TestSEER_Complex tests mixed effective and non-effective codes.
// 20×200 OK + 15×480 Busy + 15×500 Error → SEER = (20+15)/(50-0) = 70%.
func TestSEER_Complex(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 20)
	runSippScenario(ctx, t, "uas_busy.xml", "uac_busy.xml", 15)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 15)

	seer := getSEER(t, endpoint)
	t.Logf("SEER = %.2f (want %.2f)", seer, 70.0)
	require.Equal(t, 70.0, seer)

	sessions := getSessions(t, endpoint)
	require.Equal(t, 0.0, sessions, "sessions should be 0 after all calls terminated")
}
