//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSER_AllScenarios(t *testing.T) {
	tests := []struct {
		name        string
		uasScenario string
		uacScenario string
		callCount   int
		wantSER     float64
	}{
		{
			name:        "100_percent",
			uasScenario: "uas_100.xml",
			uacScenario: "uac_100.xml",
			callCount:   50,
			wantSER:     100.0,
		},
		{
			name:        "0_percent",
			uasScenario: "uas_0.xml",
			uacScenario: "uac_0.xml",
			callCount:   50,
			wantSER:     0.0,
		},
		{
			name:        "redirect",
			uasScenario: "uas_redirect.xml",
			uacScenario: "uac_redirect.xml",
			callCount:   50,
			wantSER:     0.0,
		},
		{
			name:        "no_invite",
			uasScenario: "uas_no_invite.xml",
			uacScenario: "uac_no_invite.xml",
			callCount:   50,
			wantSER:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			endpoint := startExporter(ctx, t)
			runSippScenario(ctx, t, tt.uasScenario, tt.uacScenario, tt.callCount)

			ser := getSER(t, endpoint)
			t.Logf("SER = %.2f (want %.2f)", ser, tt.wantSER)
			require.Equal(t, tt.wantSER, ser)

			sessions := getSessions(t, endpoint)
			require.Equal(t, 0.0, sessions, "sessions should be 0 after all calls terminated")
		})
		if t.Failed() {
			break
		}
	}
}

// TestSER_Mixed tests mixed scenario: some calls successful, some rejected.
func TestSER_Mixed(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)

	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 35)
	runSippScenario(ctx, t, "uas_0.xml", "uac_0.xml", 15)

	ser := getSER(t, endpoint)
	t.Logf("SER = %.2f (want %.2f)", ser, 70.0)
	require.Equal(t, 70.0, ser)

	sessions := getSessions(t, endpoint)
	require.Equal(t, 0.0, sessions, "sessions should be 0 after all calls terminated")
}

// TestSER_Mixed3xx tests that 3xx responses are correctly excluded from denominator.
// 50 redirect (3xx) + 50 successful (200 OK) → SER = 100% (all non-3xx successful).
func TestSER_Mixed3xx(t *testing.T) {
	ctx := context.Background()

	endpoint := startExporter(ctx, t)

	runSippScenario(ctx, t, "uas_redirect.xml", "uac_redirect.xml", 25)
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 25)

	ser := getSER(t, endpoint)
	t.Logf("SER = %.2f (want %.2f)", ser, 100.0)
	require.Equal(t, 100.0, ser)

	sessions := getSessions(t, endpoint)
	require.Equal(t, 0.0, sessions, "sessions should be 0 after all calls terminated")
}
