//go:build e2e

package e2e

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestINVITE_RetransmissionDedup verifies that INVITE retransmissions (RFC 3261
// Timer A) do not inflate sip_exporter_invite_total.
//
// The UAS runs with -d 600 (600ms delay before each send), so 100 Trying arrives
// at T=600ms — after the first INVITE retransmission at T=500ms (Timer A T1).
// The UAC runs WITHOUT -nr so SIPp actually retransmits the INVITE.
//
// Without dedup: invite_total ≈ 2× callCount (original + 1 retransmission per call).
// With dedup:    invite_total == callCount.
func TestINVITE_RetransmissionDedup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	const callCount = 10

	ctx2, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var stdout, stderr io.Writer = &testWriter{t}, &testWriter{t}
	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") != "true" {
		stdout, stderr = io.Discard, io.Discard
	}

	sippVol := filepath.Dir(absScenarioPath(t, "uac_100.xml"))

	// UAS: -d 600 delays each send by 600ms; 100 Trying arrives after Timer A
	// fires (500ms), forcing 1 INVITE retransmission per call.
	uasCmd := exec.CommandContext(ctx2, "docker", "run", "--rm",
		"--network", "host",
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/uas_100.xml",
		"-i", "127.0.0.1",
		"-p", env.sippPort,
		"-m", strconv.Itoa(callCount),
		"-d", "600",
		"-nr",
		"-nostdin",
	)
	uasCmd.Stdout = stdout
	uasCmd.Stderr = stderr
	require.NoError(t, uasCmd.Start())

	require.Eventually(t, func() bool {
		return isUDPPortInUse(env.sippPort)
	}, 10*time.Second, 50*time.Millisecond, "UAS should start listening on port %s", env.sippPort)

	// UAC: WITHOUT -nr → INVITE retransmissions fire on Timer A (500ms).
	uacCmd := exec.CommandContext(ctx2, "docker", "run", "--rm",
		"--network", "host",
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/uac_100.xml",
		"-i", "127.0.0.1",
		"-p", env.sippClientPort,
		"-m", strconv.Itoa(callCount),
		"-timeout", "30s",
		"127.0.0.1:"+env.sippPort,
	)
	uacCmd.Stdout = stdout
	uacCmd.Stderr = stderr
	require.NoError(t, uacCmd.Run())

	_ = uasCmd.Wait()

	waitForMetricStable(t, env.endpoint)

	// Core assertion: invite_total must equal callCount, not inflated by retransmissions.
	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	require.Equal(t, float64(callCount), inviteTotal,
		"sip_exporter_invite_total must match call count — retransmissions should be deduped")

	// SER should still be ~100% (all calls succeed despite retransmissions).
	ser := getSER(t, env.endpoint)
	require.InDelta(t, 100.0, ser, ratioDelta,
		"SER should be ~100%% despite INVITE retransmissions")

	waitForSessionsZero(t, env.endpoint)
}
