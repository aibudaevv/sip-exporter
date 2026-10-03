//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestIfaceLabelMultiInterface verifies that the iface label correctly
// identifies which NIC captured each packet, using a real second interface
// (separate network namespace via pause container + veth pair).
//
// Setup:
//   - lo: standard loopback
//   - a dynamically named host veth linked to peer eth0 through a Docker bridge
//
// Flow 1 (lo): 100 calls on 127.0.0.1 → iface="lo"
// Flow 2 (veth): 50 calls from peer UAC (10.210.0.2) to isolated UAS (10.210.0.3)
//
// On the peer host veth, IGNORE_OUTGOING=true captures only RX (UAC→UAS). The
// 200 OK (UAS→UAC, TX on that veth) is not captured, so invite_200_total is only
// asserted on lo.
func TestIfaceLabelMultiInterface(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newNetworkFixture(t)
	t.Cleanup(fixture.cleanup)

	const loCalls = 100
	const nsCalls = 50

	env := newTestEnvWithExtraEnv(ctx, t, "", map[string]string{
		"SIP_EXPORTER_INTERFACE": fmt.Sprintf("%s,%s", testInterface, fixture.hostInterface),
	})

	// Flow 1: lo (standard SIPp on 127.0.0.1).
	runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", loCalls, env)

	// Flow 2: peer UAC → a second isolated UAS endpoint on the fixture bridge.
	fixture.runSippScenarioFromPeer(ctx, t, "uas_100.xml", "uac_100.xml", nsCalls, env)

	loLabel := `iface="lo"`
	vethLabel := fmt.Sprintf(`iface="%s"`, fixture.hostInterface)
	require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_invite_total", loLabel))
	require.Equal(t, float64(loCalls),
		getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", loLabel))
	require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_invite_total", vethLabel))
	require.Equal(t, float64(nsCalls),
		getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", vethLabel))
	require.Len(t, readMetricSamples(t, env.endpoint, "sip_exporter_invite_total"), 2,
		"only lo and the configured host veth may publish INVITE series")

	require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_invite_200_total", loLabel))
	require.Equal(t, float64(loCalls),
		getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_200_total", loLabel))
	require.False(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_invite_200_total", vethLabel),
		"outgoing veth responses must remain excluded")
	require.Eventually(t, func() bool {
		samples := readMetricSamples(t, env.endpoint, "sip_exporter_socket_packets_received_total")
		seen := make(map[string]bool, len(samples))
		for _, sample := range samples {
			seen[sample.labels["iface"]] = sample.value > 0
		}
		return len(samples) == 2 && seen[testInterface] && seen[fixture.hostInterface]
	}, 3*time.Second, 100*time.Millisecond,
		"TPACKET_V3 socket statistics must remain isolated per configured interface")

}

func TestPacketRingHotUnplugPreservesOtherInterface(t *testing.T) {
	fixture := newNetworkFixture(t)
	env := newTestEnvWithExtraEnv(t.Context(), t, "", map[string]string{
		"SIP_EXPORTER_INTERFACE": fmt.Sprintf("%s,%s", testInterface, fixture.hostInterface),
	})
	errorsBefore := getMetric(t, env.endpoint, "sip_exporter_system_error_total")

	deleteCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	out, err := runHostIP(deleteCtx, "link", "delete", fixture.hostInterface)
	require.NoError(t, err, "delete capture interface: %s", string(out))

	const callCount = 10
	runSippScenario(t.Context(), t, "uas_100.xml", "uac_100.xml", callCount, env)
	require.Equal(t, float64(callCount),
		getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", `iface="lo"`))
	require.Equal(t, errorsBefore, getMetric(t, env.endpoint, "sip_exporter_system_error_total"),
		"hot-unplug must stop only the affected ring without a system error")
}
