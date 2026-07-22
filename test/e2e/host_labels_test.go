//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHostLabels verifies caller_host and called_host labels on invite_total
// and invite_200_total, both with host labels enabled (SIP_EXPORTER_HOST_LABELS=true)
// and disabled (default).
//
// Host values come from the SIP URI host part (From.Addr / To.Addr), not from
// the IP packet header. SIPp populates [local_ip] / [remote_ip] in the From /
// To headers respectively.
//
// When host labels are disabled (default), caller_host/called_host collapse to
// the empty value ("") — this is the cardinality-safe default: no unique host
// can inflate the invite_total / invite_200_total series.
func TestHostLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description       string
		hostLabelsEnabled bool
		useLoopback       bool
		wantCaller        string
		wantCalled        string
		notWantCaller     string
		notWantCalled     string
	}{
		{
			description:       "host labels enabled, loopback IPs",
			hostLabelsEnabled: true,
			useLoopback:       true,
			wantCaller:        "127.0.0.1",
			wantCalled:        "127.0.0.1",
			notWantCaller:     "10.1.0.1",
			notWantCalled:     "10.2.0.1",
		},
		{
			description:       "host labels enabled, secondary IPs",
			hostLabelsEnabled: true,
			useLoopback:       false,
			wantCaller:        "10.1.0.1",
			wantCalled:        "10.2.0.1",
			notWantCaller:     "127.0.0.1",
			notWantCalled:     "127.0.0.1",
		},
		{
			description:       "host labels disabled (default), loopback IPs",
			hostLabelsEnabled: false,
			useLoopback:       true,
			wantCaller:        "",
			wantCalled:        "",
			notWantCaller:     "127.0.0.1",
			notWantCalled:     "127.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			callCount := 100

			extraEnv := map[string]string{}
			if tt.hostLabelsEnabled {
				extraEnv["SIP_EXPORTER_HOST_LABELS"] = "true"
			}

			var env *testEnv
			if tt.useLoopback {
				env = newTestEnvWithExtraEnv(ctx, t, "", extraEnv)
				runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env)
			} else {
				setupSecondaryIPs(t)
				env = newTestEnvWithExtraEnv(ctx, t, "", extraEnv)
				runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env, "10.2.0.1", "10.1.0.1")
			}

			assertHostLabels(t, env.endpoint, tt.wantCaller, tt.wantCalled, tt.notWantCaller, tt.notWantCalled, callCount)
			waitForSessionsZero(t, env.endpoint)
		})
	}
}

// assertHostLabels verifies that invite_total and invite_200_total carry the
// expected caller_host/called_host labels (wantCaller/wantCalled) and do NOT
// carry the alternative hosts (notWantCaller/notWantCalled).
func assertHostLabels(t *testing.T, endpoint, wantCaller, wantCalled, notWantCaller, notWantCalled string, callCount int) {
	t.Helper()

	callerLabel := `caller_host="` + wantCaller + `"`
	calledLabel := `called_host="` + wantCalled + `"`

	// Guards: confirm the expected label combinations exist before reading
	// values. Prevents vacuum-pass if the metric name or label syntax is wrong.
	require.True(t, metricWithLabelExists(t, endpoint, "sip_exporter_invite_total", callerLabel),
		"sip_exporter_invite_total{%s} should exist", callerLabel)
	require.True(t, metricWithLabelExists(t, endpoint, "sip_exporter_invite_total", calledLabel),
		"sip_exporter_invite_total{%s} should exist", calledLabel)
	require.True(t, metricWithLabelExists(t, endpoint, "sip_exporter_invite_200_total", callerLabel),
		"sip_exporter_invite_200_total{%s} should exist", callerLabel)
	require.True(t, metricWithLabelExists(t, endpoint, "sip_exporter_invite_200_total", calledLabel),
		"sip_exporter_invite_200_total{%s} should exist", calledLabel)

	inviteOK := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", callerLabel)
	inviteBad := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", `caller_host="`+notWantCaller+`"`)
	t.Logf("invite_total{%s}=%.0f, invite_total{caller_host=%q}=%.0f", callerLabel, inviteOK, notWantCaller, inviteBad)
	require.InDelta(t, float64(callCount), inviteOK, ratioDelta, "invite_total should carry caller_host=%q", wantCaller)
	require.LessOrEqual(t, inviteBad, 3.0, "invite_total should NOT carry caller_host=%q", notWantCaller)

	inviteCalledOK := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", calledLabel)
	inviteCalledBad := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", `called_host="`+notWantCalled+`"`)
	t.Logf("invite_total{%s}=%.0f, invite_total{called_host=%q}=%.0f", calledLabel, inviteCalledOK, notWantCalled, inviteCalledBad)
	require.InDelta(t, float64(callCount), inviteCalledOK, ratioDelta, "invite_total should carry called_host=%q", wantCalled)
	require.LessOrEqual(t, inviteCalledBad, 3.0, "invite_total should NOT carry called_host=%q", notWantCalled)

	invite200CallerOK := getMetricWithLabel(t, endpoint, "sip_exporter_invite_200_total", callerLabel)
	invite200CallerBad := getMetricWithLabel(t, endpoint, "sip_exporter_invite_200_total", `caller_host="`+notWantCaller+`"`)
	t.Logf("invite_200_total{%s}=%.0f, invite_200_total{caller_host=%q}=%.0f", callerLabel, invite200CallerOK, notWantCaller, invite200CallerBad)
	require.InDelta(t, float64(callCount), invite200CallerOK, ratioDelta, "invite_200_total should carry caller_host=%q", wantCaller)
	require.LessOrEqual(t, invite200CallerBad, 3.0, "invite_200_total should NOT carry caller_host=%q", notWantCaller)

	invite200CalledOK := getMetricWithLabel(t, endpoint, "sip_exporter_invite_200_total", calledLabel)
	invite200CalledBad := getMetricWithLabel(t, endpoint, "sip_exporter_invite_200_total", `called_host="`+notWantCalled+`"`)
	t.Logf("invite_200_total{%s}=%.0f, invite_200_total{called_host=%q}=%.0f", calledLabel, invite200CalledOK, notWantCalled, invite200CalledBad)
	require.InDelta(t, float64(callCount), invite200CalledOK, ratioDelta, "invite_200_total should carry called_host=%q", wantCalled)
	require.LessOrEqual(t, invite200CalledBad, 3.0, "invite_200_total should NOT carry called_host=%q", notWantCalled)
}
