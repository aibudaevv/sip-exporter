//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHostLabels verifies caller_host and called_host labels on invite_total
// and invite_200_total.
//
// host values come from the SIP URI host part (From.Addr / To.Addr), not from
// the IP packet header. SIPp populates [local_ip] / [remote_ip] in the From /
// To headers respectively.
//
// Subtests:
//   - Loopback: From host = 127.0.0.1, To host = 127.0.0.1
//   - SecondaryIPs: From host = 10.1.0.1 (UAC), To host = 10.2.0.1 (UAS)
//
// For each subtest we verify the expected host labels are present and the
// alternative is absent (0).
func TestHostLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		useLoopback bool
		wantCaller  string
		wantCalled  string
	}{
		{
			name:        "Loopback",
			useLoopback: true,
			wantCaller:  "127.0.0.1",
			wantCalled:  "127.0.0.1",
		},
		{
			name:        "SecondaryIPs",
			useLoopback: false,
			wantCaller:  "10.1.0.1",
			wantCalled:  "10.2.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			callerLabel := `caller_host="` + tt.wantCaller + `"`
			calledLabel := `called_host="` + tt.wantCalled + `"`

			if tt.useLoopback {
				env := newTestEnv(ctx, t)
				runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)
				assertHostLabels(t, env.endpoint, callerLabel, calledLabel, tt.wantCaller, tt.wantCalled)
				waitForSessionsZero(t, env.endpoint)
			} else {
				setupSecondaryIPs(t)
				env := newTestEnv(ctx, t)
				runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", 100, env, "10.2.0.1", "10.1.0.1")
				assertHostLabels(t, env.endpoint, callerLabel, calledLabel, tt.wantCaller, tt.wantCalled)
				waitForSessionsZero(t, env.endpoint)
			}
		})
	}
}

func assertHostLabels(t *testing.T, endpoint, callerLabel, calledLabel, wantCaller, wantCalled string) {
	t.Helper()

	notWantCaller := "10.1.0.1"
	if wantCaller == "10.1.0.1" {
		notWantCaller = "127.0.0.1"
	}
	notWantCalled := "10.2.0.1"
	if wantCalled == "10.2.0.1" {
		notWantCalled = "127.0.0.1"
	}

	inviteOK := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", callerLabel)
	inviteBad := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", `caller_host="`+notWantCaller+`"`)
	t.Logf("invite_total{%s}=%.0f, invite_total{caller_host=%q}=%.0f", callerLabel, inviteOK, notWantCaller, inviteBad)
	require.Equal(t, 100.0, inviteOK, "invite_total should carry caller_host=%q", wantCaller)
	require.Equal(t, 0.0, inviteBad, "invite_total should NOT carry caller_host=%q", notWantCaller)

	inviteCalledOK := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", calledLabel)
	inviteCalledBad := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", `called_host="`+notWantCalled+`"`)
	t.Logf("invite_total{%s}=%.0f, invite_total{called_host=%q}=%.0f", calledLabel, inviteCalledOK, notWantCalled, inviteCalledBad)
	require.Equal(t, 100.0, inviteCalledOK, "invite_total should carry called_host=%q", wantCalled)
	require.Equal(t, 0.0, inviteCalledBad, "invite_total should NOT carry called_host=%q", notWantCalled)

	invite200CallerOK := getMetricWithLabel(t, endpoint, "sip_exporter_invite_200_total", callerLabel)
	invite200CallerBad := getMetricWithLabel(t, endpoint, "sip_exporter_invite_200_total", `caller_host="`+notWantCaller+`"`)
	t.Logf("invite_200_total{%s}=%.0f, invite_200_total{caller_host=%q}=%.0f", callerLabel, invite200CallerOK, notWantCaller, invite200CallerBad)
	require.Equal(t, 100.0, invite200CallerOK, "invite_200_total should carry caller_host=%q", wantCaller)
	require.Equal(t, 0.0, invite200CallerBad, "invite_200_total should NOT carry caller_host=%q", notWantCaller)

	invite200CalledOK := getMetricWithLabel(t, endpoint, "sip_exporter_invite_200_total", calledLabel)
	invite200CalledBad := getMetricWithLabel(t, endpoint, "sip_exporter_invite_200_total", `called_host="`+notWantCalled+`"`)
	t.Logf("invite_200_total{%s}=%.0f, invite_200_total{called_host=%q}=%.0f", calledLabel, invite200CalledOK, notWantCalled, invite200CalledBad)
	require.Equal(t, 100.0, invite200CalledOK, "invite_200_total should carry called_host=%q", wantCalled)
	require.Equal(t, 0.0, invite200CalledBad, "invite_200_total should NOT carry called_host=%q", notWantCalled)
}
