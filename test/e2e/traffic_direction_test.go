//go:build e2e

package e2e

import (
	"context"
	"testing"

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

// TestTrafficDirection_Outbound verifies that on a veth pair with
// IgnoreOutgoing=false, SIP requests sent by UAC are classified as outbound
// via pkttype (PACKET_OUTGOING). This is the discriminating test: on lo with
// IgnoreOutgoing=true, all packets are PACKET_HOST → inbound. Only on a real
// (or virtual) interface can we observe PACKET_OUTGOING.
//
// veth0a (10.10.0.1) ← exporter captures here (IgnoreOutgoing=false)
// veth0b (10.10.0.2) ← UAS listens here
//
// UAC binds to 10.10.0.1 (veth0a) → sends INVITE to 10.10.0.2 (veth0b).
// INVITE is TX on veth0a → PACKET_OUTGOING → outbound.
// UAS responds from 10.10.0.2 → RX on veth0a → PACKET_HOST → INVITE responses
// are overridden from inviteTracker → outbound.
func TestTrafficDirection_Outbound(t *testing.T) {
	ctx := context.Background()
	setupVethPair(t)

	exporterHTTPPort, sippPort, sippClientPort := allocatePorts()

	extraEnv := map[string]string{
		"SIP_EXPORTER_INTERFACE":       veth0aName,
		"SIP_EXPORTER_IGNORE_OUTGOING": "false",
	}
	endpoint, container := startExporterWithConfigAndUA(
		ctx, t, exporterHTTPPort, sippPort, sippClientPort, "", "", extraEnv, "", "",
	)
	registerExporterCleanup(t, container, exporterHTTPPort)

	env := &testEnv{
		endpoint:       endpoint,
		sippPort:       sippPort,
		sippClientPort: sippClientPort,
	}

	callCount := 10
	runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", callCount, env, veth0bIP, veth0aIP)

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
