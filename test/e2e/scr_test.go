//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSCR_AllScenarios tests SCR metric with various scenarios.
// SCR = (Successfully Completed Sessions) / (Total INVITE) × 100
// 3xx NOT excluded from denominator (same as ISA).
func TestSCR_AllScenarios(t *testing.T) {
	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantSCR     float64
	}{
		{
			name:        "all_completed",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   50,
			wantSCR:     100.0,
		},
		{
			name:        "none_completed_486",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   50,
			wantSCR:     0.0,
		},
		{
			name:        "none_completed_500",
			uasScenario: "uas_server_error.xml",
			uacScenario: "uac_server_error.xml",
			callCount:   50,
			wantSCR:     0.0,
		},
		{
			name:        "redirect_only",
			uasScenario: "uas_redirect.xml",
			uacScenario: "uac_redirect.xml",
			callCount:   50,
			wantSCR:     0.0,
		},
		{
			name:        "no_invite",
			uasScenario: "uas_no_invite.xml",
			uacScenario: "uac_no_invite.xml",
			callCount:   50,
			wantSCR:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			endpoint := startExporter(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount)

			scr := getSCR(t, endpoint)
			t.Logf("SCR = %.2f (want %.2f)", scr, tt.wantSCR)
			require.Equal(t, tt.wantSCR, scr)

			sessions := getSessions(t, endpoint)
			require.Equal(t, 0.0, sessions, "sessions should be 0 after all calls terminated")
		})
		if t.Failed() {
			break
		}
	}
}

// TestSCR_Mixed tests 50% completed + 50% rejected (486) → SCR = 50%.
func TestSCR_Mixed(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 35)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 15)

	scr := getSCR(t, endpoint)
	t.Logf("SCR = %.2f (want %.2f)", scr, 70.0)
	require.Equal(t, 70.0, scr)

	sessions := getSessions(t, endpoint)
	require.Equal(t, 0.0, sessions, "sessions should be 0 after all calls terminated")
}

// TestSCR_MixedWith3xx tests that 3xx are NOT excluded from SCR denominator.
// 25 redirect (3xx) + 25 successful → SCR = 25/50 × 100 = 50%.
// (SER would be 100% because 3xx excluded, but SCR keeps them.)
func TestSCR_MixedWith3xx(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)

	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 25)
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 25)

	scr := getSCR(t, endpoint)
	t.Logf("SCR = %.2f (want %.2f)", scr, 50.0)
	require.Equal(t, 50.0, scr)

	sessions := getSessions(t, endpoint)
	require.Equal(t, 0.0, sessions, "sessions should be 0 after all calls terminated")
}

// TestSCR_Complex tests mixed scenarios.
// 20×completed + 15×486 + 15×500 → SCR = 20/50 × 100 = 40%.
func TestSCR_Complex(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 20)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 15)
	runSippScenario(ctx, t, "uas_server_error.xml", "uac_server_error.xml", 15)

	scr := getSCR(t, endpoint)
	t.Logf("SCR = %.2f (want %.2f)", scr, 40.0)
	require.Equal(t, 40.0, scr)

	sessions := getSessions(t, endpoint)
	require.Equal(t, 0.0, sessions, "sessions should be 0 after all calls terminated")
}
