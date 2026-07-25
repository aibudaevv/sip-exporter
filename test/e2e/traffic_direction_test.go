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

// TestTrafficDirection_Inbound verifies that on loopback with
// IgnoreOutgoing=true, SIP requests are classified as inbound via pkttype
// (PACKET_HOST) and that direction propagates to dialog-level metrics.
//
// On lo every packet is PACKET_HOST (RX). For requests:
// PACKET_HOST → inbound. INVITE responses inherit the INVITE's direction via
// inviteTracker override → also inbound. Dialog teardown (SDC) inherits from
// the dialog entry → inbound.
func TestTrafficDirection_Inbound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newTestEnv(ctx, t)

	callCount := 10
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env)

	inboundLabel := `direction="inbound"`

	require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_invite_total", inboundLabel),
		"invite_total{direction=inbound} must exist")
	inviteInbound := getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", inboundLabel)
	t.Logf("invite_total{direction=inbound} = %.0f", inviteInbound)
	require.Greater(t, inviteInbound, 0.0, "INVITE requests must be classified as inbound")

	require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_sdc_total", inboundLabel),
		"sdc_total{direction=inbound} must exist")
	sdcInbound := getMetricWithLabel(t, env.endpoint, "sip_exporter_sdc_total", inboundLabel)
	t.Logf("sdc_total{direction=inbound} = %.0f", sdcInbound)
	require.Greater(t, sdcInbound, 0.0, "completed sessions must inherit inbound direction from INVITE")

	waitForSessionsZero(t, env.endpoint)
}

// TestTrafficDirection_Outbound verifies that on a veth pair bridging to an
// isolated network namespace (pause container), SIP requests sent by a
// host-side UAC are classified as outbound via pkttype (PACKET_OUTGOING).
//
// setupVethNetns creates: sipns0 (host, 10.210.0.1) + sipns1 (pause netns,
// 10.210.0.2). Unlike setupVethPair (both ends in host netns → kernel local
// routing sends traffic via lo → 0 packets visible on the veth), the netns
// approach ensures traffic genuinely traverses the veth pair.
//
// UAC (host, 10.210.0.1) → INVITE → sipns0 TX (PACKET_OUTGOING) → sipns1 → UAS.
// UAS (netns, 10.210.0.2) → 200 OK → sipns1 → sipns0 RX (PACKET_HOST, overridden
// from inviteTracker) → UAC.
func TestTrafficDirection_Outbound(t *testing.T) {
	ctx := context.Background()
	pauseID := setupVethNetns(t)

	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	extraEnv := map[string]string{
		"SIP_EXPORTER_INTERFACE":       nsVethHost,
		"SIP_EXPORTER_IGNORE_OUTGOING": "false",
	}
	endpoint, container := startExporterWithConfigAndUA(
		ctx, t, exporterHTTPPort, sippPort, sippClientPort, "", "", extraEnv, "", "",
	)
	registerExporterCleanup(t, container, exporterHTTPPort)

	callCount := 10

	uasPath := absScenarioPath(t, "uas_100.xml")
	sippVol := filepath.Dir(uasPath)

	sippCtx, sippCancel := context.WithTimeout(ctx, 60*time.Second)
	defer sippCancel()

	// UAS in pause netns (nsGuestIP = 10.210.0.2) — detached.
	uasStart := exec.CommandContext(sippCtx, "docker", "run", "-d",
		"--network", "container:"+pauseID,
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/uas_100.xml",
		"-i", nsGuestIP,
		"-p", sippPort,
		"-m", strconv.Itoa(callCount),
		"-nr", "-nostdin",
	)
	uasIDBytes, err := uasStart.Output()
	require.NoError(t, err, "failed to start UAS in netns")
	uasID := strings.TrimSpace(string(uasIDBytes))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", uasID).Run() })

	time.Sleep(1 * time.Second)

	// UAC in host netns (nsHostIP = 10.210.0.1) → sends to nsGuestIP:sippPort.
	// TX on sipns0 = PACKET_OUTGOING → direction=outbound.
	uacCmd := exec.CommandContext(sippCtx, "docker", "run", "--rm",
		"--network", "host",
		"-v", sippVol+":/scenarios:ro",
		sippImage,
		"-sf", "/scenarios/uac_100.xml",
		"-i", nsHostIP,
		"-p", sippClientPort,
		"-m", strconv.Itoa(callCount),
		"-nr",
		nsGuestIP+":"+sippPort,
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
		out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", uasID).Output()
		return err == nil && strings.TrimSpace(string(out)) == "false"
	}, 30*time.Second, 500*time.Millisecond, "UAS container did not exit")

	if os.Getenv("SIP_EXPORTER_E2E_SIPP_VERBOSE") == "true" {
		uasLogs, _ := exec.Command("docker", "logs", uasID).CombinedOutput()
		t.Logf("UAS logs:\n%s", strings.TrimSpace(string(uasLogs)))
	}

	waitForMetricStable(t, endpoint)

	outboundLabel := `direction="outbound"`

	require.True(t, metricWithLabelExists(t, endpoint, "sip_exporter_invite_total", outboundLabel),
		"invite_total{direction=outbound} must exist")
	inviteOutbound := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", outboundLabel)
	t.Logf("invite_total{direction=outbound} = %.0f", inviteOutbound)
	require.Greater(t, inviteOutbound, 0.0, "INVITE requests from UAC must be classified as outbound")

	if metricWithLabelExists(t, endpoint, "sip_exporter_invite_total", `direction="inbound"`) {
		inviteInbound := getMetricWithLabel(t, endpoint, "sip_exporter_invite_total", `direction="inbound"`)
		t.Logf("invite_total{direction=inbound} = %.0f", inviteInbound)
		require.Equal(t, 0.0, inviteInbound, "no inbound INVITEs expected (UAC sends TX only)")
	}

	waitForSessionsZero(t, endpoint)
}
