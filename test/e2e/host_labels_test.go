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
// Host labels are OPT-IN (SIP_EXPORTER_HOST_LABELS, default false): this test
// enables them explicitly. See TestHostLabels_DisabledByDefault for the
// default-off behavior.
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
				env := newTestEnvWithExtraEnv(ctx, t, "", map[string]string{"SIP_EXPORTER_HOST_LABELS": "true"})
				runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)
				assertHostLabels(t, env.endpoint, callerLabel, calledLabel, tt.wantCaller, tt.wantCalled)
				waitForSessionsZero(t, env.endpoint)
			} else {
				setupSecondaryIPs(t)
				env := newTestEnvWithExtraEnv(ctx, t, "", map[string]string{"SIP_EXPORTER_HOST_LABELS": "true"})
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
	require.InDelta(t, 100.0, inviteOK, ratioDelta, "invite_total should carry caller_host=%q", wantCaller)
	require.LessOrEqual(t, inviteBad, 3.0, "invite_total should NOT carry caller_host=%q", notWantCaller)

	inviteCalledOK := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", calledLabel)
	inviteCalledBad := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", `called_host="`+notWantCalled+`"`)
	t.Logf("invite_total{%s}=%.0f, invite_total{called_host=%q}=%.0f", calledLabel, inviteCalledOK, notWantCalled, inviteCalledBad)
	require.InDelta(t, 100.0, inviteCalledOK, ratioDelta, "invite_total should carry called_host=%q", wantCalled)
	require.LessOrEqual(t, inviteCalledBad, 3.0, "invite_total should NOT carry called_host=%q", notWantCalled)

	invite200CallerOK := getMetricWithLabel(t, endpoint, "sip_exporter_invite_200_total", callerLabel)
	invite200CallerBad := getMetricWithLabel(t, endpoint, "sip_exporter_invite_200_total", `caller_host="`+notWantCaller+`"`)
	t.Logf("invite_200_total{%s}=%.0f, invite_200_total{caller_host=%q}=%.0f", callerLabel, invite200CallerOK, notWantCaller, invite200CallerBad)
	require.InDelta(t, 100.0, invite200CallerOK, ratioDelta, "invite_200_total should carry caller_host=%q", wantCaller)
	require.LessOrEqual(t, invite200CallerBad, 3.0, "invite_200_total should NOT carry caller_host=%q", notWantCaller)

	invite200CalledOK := getMetricWithLabel(t, endpoint, "sip_exporter_invite_200_total", calledLabel)
	invite200CalledBad := getMetricWithLabel(t, endpoint, "sip_exporter_invite_200_total", `called_host="`+notWantCalled+`"`)
	t.Logf("invite_200_total{%s}=%.0f, invite_200_total{called_host=%q}=%.0f", calledLabel, invite200CalledOK, notWantCalled, invite200CalledBad)
	require.InDelta(t, 100.0, invite200CalledOK, ratioDelta, "invite_200_total should carry called_host=%q", wantCalled)
	require.LessOrEqual(t, invite200CalledBad, 3.0, "invite_200_total should NOT carry called_host=%q", notWantCalled)
}

// TestHostLabels_DisabledByDefault verifies that when SIP_EXPORTER_HOST_LABELS
// is not set (default false), caller_host/called_host collapse to the empty
// value and do NOT carry the From/To host. This is the cardinality-safe default:
// no unique host can inflate the invite_total / invite_200_total series.
func TestHostLabels_DisabledByDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t) // no SIP_EXPORTER_HOST_LABELS → default false
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 100, env)

	// caller_host/called_host must be empty (""); the loopback host must NOT appear.
	emptyCaller := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `caller_host=""`)
	loopbackCaller := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `caller_host="127.0.0.1"`)
	t.Logf("invite_total{caller_host=\"\"}=%.0f, invite_total{caller_host=\"127.0.0.1\"}=%.0f", emptyCaller, loopbackCaller)
	require.InDelta(t, 100.0, emptyCaller, ratioDelta, "invite_total should collapse to caller_host=\"\" when host labels disabled")
	require.LessOrEqual(t, loopbackCaller, 3.0, "caller_host must not carry the From host when host labels disabled")

	emptyCalled := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `called_host=""`)
	loopbackCalled := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `called_host="127.0.0.1"`)
	t.Logf("invite_total{called_host=\"\"}=%.0f, invite_total{called_host=\"127.0.0.1\"}=%.0f", emptyCalled, loopbackCalled)
	require.InDelta(t, 100.0, emptyCalled, ratioDelta, "invite_total should collapse to called_host=\"\" when host labels disabled")
	require.LessOrEqual(t, loopbackCalled, 3.0, "called_host must not carry the To host when host labels disabled")

	// Same for invite_200_total.
	empty200Caller := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", `caller_host=""`)
	require.InDelta(t, 100.0, empty200Caller, ratioDelta, "invite_200_total should collapse to caller_host=\"\" when host labels disabled")

	waitForSessionsZero(t, env.endpoint)
}
