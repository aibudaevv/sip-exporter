//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCancel_InviteTrackerCleanup(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := newTestEnv(ctx, t)
	callCount := 10
	wantTTR := float64(callCount * 2)

	runSippScenario(ctx, t, "uas_487.xml", "uac_487.xml", callCount, env)

	ttrCount := getMetric(t, env.endpoint, "sip_exporter_ttr_count")
	t.Logf("sip_exporter_ttr_count = %.0f (want %.0f, one TTR per call on 100 Trying)", ttrCount, wantTTR)
	require.Equal(t, wantTTR, ttrCount, "TTR should be measured exactly once per call on 100 Trying, no leaked tracker entries")

	code487 := getMetric(t, env.endpoint, "sip_exporter_487_total")
	want487 := float64(callCount * 2)
	t.Logf("sip_exporter_487_total = %.0f (want %.0f, loopback doubling)", code487, want487)
	require.Equal(t, want487, code487, "487 counter should equal callCount*2")

	cancelTotal := getMetric(t, env.endpoint, "sip_exporter_cancel_total")
	wantCancel := float64(callCount * 2)
	t.Logf("sip_exporter_cancel_total = %.0f (want %.0f, loopback doubling)", cancelTotal, wantCancel)
	require.Equal(t, wantCancel, cancelTotal, "CANCEL counter should equal callCount*2")

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	wantInvite := float64(callCount * 2)
	t.Logf("sip_exporter_invite_total = %.0f (want %.0f, loopback doubling)", inviteTotal, wantInvite)
	require.Equal(t, wantInvite, inviteTotal, "INVITE counter should equal callCount*2")

	waitForSessionsZero(t, env.endpoint)
}
