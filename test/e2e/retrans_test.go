//go:build e2e

package e2e

import (
	"context"
	"io"
	"net"
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

// TestSIPRetransmission_MetricObserved verifies that the new
// sip_exporter_sip_retransmission_total counter is incremented when INVITE
// retransmissions are detected (same Call-ID arriving twice within the
// inviteTracker TTL, before dialog establishment).
//
// Sends two identical INVITE messages via raw UDP on loopback — no SIPp needed.
// The eBPF filter captures both; the first is counted as Request + stored in
// inviteTracker, the second triggers SIPRetransmission.
func TestSIPRetransmission_MetricObserved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	// Bind a drain listener on the sipp port so the kernel delivers the packet
	// (otherwise ICMP Port Unreachable may interfere).
	drainAddr, err := net.ResolveUDPAddr("udp4", "127.0.0.1:"+env.sippPort)
	require.NoError(t, err)
	drain, err := net.ListenUDP("udp4", drainAddr)
	require.NoError(t, err)
	defer drain.Close()

	go func() {
		buf := make([]byte, 1500)
		for {
			drain.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			if _, _, e := drain.ReadFromUDP(buf); e != nil {
				return
			}
		}
	}()

	const retransCount = 5
	invite := []byte("INVITE sip:test@127.0.0.1 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1:" + env.sippClientPort + ";branch=z9hG4bK-retrans\r\n" +
		"From: <sip:test@127.0.0.1>;tag=retrans\r\n" +
		"To: <sip:test@127.0.0.1>\r\n" +
		"Call-ID: retrans-e2e-test\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n\r\n")

	sender, err := net.DialUDP("udp4", nil, drainAddr)
	require.NoError(t, err)
	defer sender.Close()

	// First INVITE — creates inviteTracker entry.
	_, err = sender.Write(invite)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Retransmissions — should trigger SIPRetransmission each time.
	for i := 1; i <= retransCount; i++ {
		_, err = sender.Write(invite)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
	}

	waitForMetricStable(t, env.endpoint)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_sip_retransmission_total"),
		"sip_retransmission_total must exist after INVITE retransmissions")

	retrans := getMetricWithLabel(t, env.endpoint, "sip_exporter_sip_retransmission_total", `method="INVITE"`)
	t.Logf("sip_retransmission_total{method=INVITE} = %.0f (sent %d retransmissions)", retrans, retransCount)
	require.GreaterOrEqual(t, retrans, float64(retransCount),
		"retransmission counter must be ≥ number of retransmitted INVITEs sent")

	inviteTotal := getMetric(t, env.endpoint, "sip_exporter_invite_total")
	require.Equal(t, 1.0, inviteTotal,
		"invite_total must be 1 — original INVITE counted once, retransmissions deduped")
}
