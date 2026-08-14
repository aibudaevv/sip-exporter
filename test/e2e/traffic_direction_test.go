//go:build e2e

package e2e

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTrafficDirectionInbound verifies that on loopback with
// IgnoreOutgoing=true, SIP requests are classified as inbound via pkttype
// (PACKET_HOST) and that direction propagates to dialog-level metrics.
//
// On lo every packet is PACKET_HOST (RX). For requests:
// PACKET_HOST → inbound. INVITE responses inherit the INVITE's direction via
// inviteTracker override → also inbound. Dialog teardown (SDC) inherits from
// the dialog entry → inbound.
func TestTrafficDirectionInbound(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestEnv(ctx, t)

	const callCount = 10
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env)

	inboundLabel := `direction="inbound"`

	require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_invite_total", inboundLabel),
		"invite_total{direction=inbound} must exist")
	inviteInbound := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", inboundLabel)
	t.Logf("invite_total{direction=inbound} = %.0f", inviteInbound)
	require.Equal(t, float64(callCount), inviteInbound,
		"every INVITE request must be classified as inbound")

	require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_invite_200_total", inboundLabel),
		"invite_200_total{direction=inbound} must exist")
	require.Equal(t, float64(callCount),
		getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", inboundLabel),
		"every INVITE 200 response must inherit inbound direction")

	require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_sdc_total", inboundLabel),
		"sdc_total{direction=inbound} must exist")
	sdcInbound := getMetricWithLabel(t, env.endpoint, "sip_exporter_sdc_total", inboundLabel)
	t.Logf("sdc_total{direction=inbound} = %.0f", sdcInbound)
	require.Equal(t, float64(callCount), sdcInbound,
		"every completed session must inherit inbound direction from INVITE")

	for _, metricName := range []string{
		"sip_exporter_invite_total",
		"sip_exporter_invite_200_total",
		"sip_exporter_sdc_total",
	} {
		require.False(t, metricWithLabelExists(t, env.endpoint, metricName, `direction="outbound"`),
			"%s outbound series must remain absent", metricName)
	}

	assertDialogTeardown(t, env.endpoint)
}

// TestTrafficDirectionOutbound verifies that requests from one isolated bridge
// endpoint to the peer UAS are classified as outbound on the peer's host veth
// via pkttype (PACKET_OUTGOING).
//
// The network fixture creates a host veth endpoint plus eth0 in the isolated
// peer netns. Unlike a pair with both ends in the host namespace, this ensures
// traffic genuinely traverses the veth pair.
//
// UAC (10.210.0.1) → INVITE → peer host veth TX (PACKET_OUTGOING) → peer UAS.
// UAS (10.210.0.2) → 200 OK → peer host veth RX (PACKET_HOST, overridden from
// inviteTracker) → UAC.
func TestTrafficDirectionOutbound(t *testing.T) {
	ctx := t.Context()
	fixture := newNetworkFixture(t)
	pauseID := fixture.peerContainerID

	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	extraEnv := map[string]string{
		"SIP_EXPORTER_INTERFACE":       fixture.hostInterface,
		"SIP_EXPORTER_IGNORE_OUTGOING": "false",
	}
	endpoint, container := startExporterWithConfigAndUA(
		ctx, t, exporterHTTPPort, sippPort, sippClientPort, "", "", extraEnv, "", "",
	)
	registerExporterCleanup(t, container, exporterHTTPPort)

	callCount := 10

	uasPath := absScenarioPath(t, "uas_100.xml")
	sippVol := filepath.Dir(uasPath)

	// UAS in the isolated peer netns — detached.
	uasStart := exec.CommandContext(ctx, "docker", "run", "-d",
		"--network", "container:"+pauseID,
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/uas_100.xml",
		"-i", fixture.peerIP,
		"-p", sippPort,
		"-m", strconv.Itoa(callCount),
		"-nr", "-nostdin",
	)
	uasIDBytes, err := uasStart.Output()
	require.NoError(t, err, "failed to start UAS in netns")
	uasID := strings.TrimSpace(string(uasIDBytes))
	t.Cleanup(func() {
		if err := removeSippContainer(uasID); err != nil {
			t.Logf("outbound UAS cleanup: %v", err)
		}
	})

	require.Eventually(t, func() bool {
		return containerUDPPortInUse(t.Context(), uasID, sippPort)
	}, 10*time.Second, 50*time.Millisecond,
		"UAS should start listening on %s:%s", fixture.peerIP, sippPort)

	// UAC runs on the host endpoint of the direct veth pair. Its INVITE leaves
	// through the peer UAS host veth as PACKET_OUTGOING.
	uacCmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--network", "host",
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/uac_100.xml",
		"-i", fixture.hostIP,
		"-p", sippClientPort,
		"-m", strconv.Itoa(callCount),
		"-nr",
		fixture.peerIP+":"+sippPort,
	)
	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
		uacCmd.Stdout = &testWriter{t}
		uacCmd.Stderr = &testWriter{t}
	} else {
		uacCmd.Stdout = io.Discard
		uacCmd.Stderr = io.Discard
	}
	require.NoError(t, uacCmd.Run(), "SIPp UAC failed")

	// Wait for UAS to exit, then log its output.
	require.Eventually(t, func() bool {
		probeCtx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		out, err := exec.CommandContext(probeCtx,
			"docker", "inspect", "-f", "{{.State.Running}}", uasID).Output()
		return err == nil && strings.TrimSpace(string(out)) == "false"
	}, 30*time.Second, 500*time.Millisecond, "UAS container did not exit")

	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
		logCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		uasLogs, _ := exec.CommandContext(logCtx, "docker", "logs", uasID).CombinedOutput()
		t.Logf("UAS logs:\n%s", strings.TrimSpace(string(uasLogs)))
	}

	waitForMetricStable(t, endpoint)

	outboundLabel := `direction="outbound"`

	require.True(t, metricWithLabelExists(t, endpoint, "sip_exporter_invite_total", outboundLabel),
		"invite_total{direction=outbound} must exist")
	inviteOutbound := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", outboundLabel)
	t.Logf("invite_total{direction=outbound} = %.0f", inviteOutbound)
	require.Equal(t, float64(callCount), inviteOutbound,
		"every UAC INVITE must be classified as outbound")
	require.False(t, metricWithLabelExists(t, endpoint,
		"sip_exporter_invite_total", `direction="inbound"`),
		"inbound INVITE series must remain absent")

	assertDialogTeardown(t, endpoint)
}
