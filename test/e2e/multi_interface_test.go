//go:build e2e

package e2e

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// freePort returns a process-unique UDP port number for SIPp -p allocation.
func freePort(t *testing.T) string {
	t.Helper()
	portMu.Lock()
	defer portMu.Unlock()
	return strconv.Itoa(allocateUDPPort())
}

// TestMultiPortPerInterface verifies capture on every configured SIP port of a
// single interface. Exact totals make extra or duplicate packets fail.
func TestMultiPortPerInterface(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	sipPorts := []string{freePort(t), freePort(t), freePort(t)}
	env := newTestEnvWithExtraEnv(ctx, t, "", map[string]string{
		"SIP_EXPORTER_SIP_PORTS": strings.Join(sipPorts, ","),
	})

	const callCount = 5
	for _, port := range sipPorts {
		flowEnv := &testEnv{
			endpoint:       env.endpoint,
			sippPort:       port,
			sippClientPort: freePort(t),
		}
		runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", callCount, flowEnv)
	}

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_total"))
	require.Equal(t, float64(len(sipPorts)*callCount),
		getMetric(t, env.endpoint, "sip_exporter_register_total"))
	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestMultiPortPerInterfaceDifferentPorts proves that each configured
// interface receives its own port set. REGISTER traffic exercises both lo
// ports; INVITE traffic from the isolated peer proves the veth port by its
// exact iface label.
func TestMultiPortPerInterfaceDifferentPorts(t *testing.T) {
	ctx := t.Context()
	fixture := newNetworkFixture(t)
	t.Cleanup(fixture.cleanup)

	loPort1 := freePort(t)
	loPort2 := freePort(t)
	vethPort := freePort(t)
	env := newTestEnvWithExtraEnv(ctx, t, "", map[string]string{
		"SIP_EXPORTER_INTERFACE": testInterface + "," + fixture.hostInterface,
		"SIP_EXPORTER_SIP_PORTS": loPort1 + "," + loPort2 + ";" + vethPort,
	})

	const callCount = 5
	for _, port := range []string{loPort1, loPort2} {
		flowEnv := &testEnv{
			endpoint:       env.endpoint,
			sippPort:       port,
			sippClientPort: freePort(t),
		}
		runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", callCount, flowEnv)
	}

	vethEnv := &testEnv{
		endpoint:       env.endpoint,
		sippPort:       vethPort,
		sippClientPort: freePort(t),
	}
	fixture.runSippScenarioFromPeer(ctx, t, "uas_0.xml", "uac_0.xml", callCount, vethEnv)

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_total"))
	require.Equal(t, float64(2*callCount),
		getMetric(t, env.endpoint, "sip_exporter_register_total"))

	vethLabel := `iface="` + fixture.hostInterface + `"`
	require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_invite_total", vethLabel))
	require.Equal(t, float64(callCount),
		getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", vethLabel))
	require.False(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_invite_total", `iface="lo"`))
}

// TestMultiPortPerInterfacePortIsolation verifies exact absence when traffic
// traverses the veth on a port assigned only to lo.
func TestMultiPortPerInterfacePortIsolation(t *testing.T) {
	const callCount = 5
	t.Run("configured veth port", func(t *testing.T) {
		ctx := t.Context()
		fixture := newNetworkFixture(t)
		t.Cleanup(fixture.cleanup)

		loPort := freePort(t)
		vethPort := freePort(t)
		env := newTestEnvWithExtraEnv(ctx, t, "", map[string]string{
			"SIP_EXPORTER_INTERFACE": testInterface + "," + fixture.hostInterface,
			"SIP_EXPORTER_SIP_PORTS": loPort + ";" + vethPort,
		})
		flowEnv := &testEnv{
			endpoint:       env.endpoint,
			sippPort:       vethPort,
			sippClientPort: freePort(t),
		}
		fixture.runSippScenarioFromPeer(ctx, t, "uas_0.xml", "uac_0.xml", callCount, flowEnv)

		vethLabel := `iface="` + fixture.hostInterface + `"`
		require.True(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_invite_total", vethLabel))
		require.Equal(t, float64(callCount),
			getMetricWithLabel(t, env.endpoint, "sip_exporter_invite_total", vethLabel))
		require.False(t, metricWithLabelExists(t, env.endpoint, "sip_exporter_invite_total", `iface="lo"`))
	})

	t.Run("lo-only port absent on veth", func(t *testing.T) {
		ctx := t.Context()
		fixture := newNetworkFixture(t)
		t.Cleanup(fixture.cleanup)

		loPort := freePort(t)
		vethPort := freePort(t)
		env := newTestEnvWithExtraEnv(ctx, t, "", map[string]string{
			"SIP_EXPORTER_INTERFACE": testInterface + "," + fixture.hostInterface,
			"SIP_EXPORTER_SIP_PORTS": loPort + ";" + vethPort,
		})
		dropEnv := &testEnv{
			endpoint:       env.endpoint,
			sippPort:       loPort,
			sippClientPort: freePort(t),
		}
		fixture.runSippScenarioFromPeer(ctx, t, "uas_0.xml", "uac_0.xml", callCount, dropEnv)

		require.False(t, metricExists(t, env.endpoint, "sip_exporter_invite_total"),
			"INVITE series must be absent when the veth port is not configured")
	})
}

// TestMultiPortUnconfiguredPortDropped verifies exact absence for traffic on a
// port missing from the only configured interface's port set.
func TestMultiPortUnconfiguredPortDropped(t *testing.T) {
	const callCount = 5
	t.Run("configured port", func(t *testing.T) {
		ctx := t.Context()
		configuredPort := freePort(t)
		env := newTestEnvWithExtraEnv(ctx, t, "", map[string]string{
			"SIP_EXPORTER_SIP_PORTS": configuredPort,
		})
		flowEnv := &testEnv{
			endpoint:       env.endpoint,
			sippPort:       configuredPort,
			sippClientPort: freePort(t),
		}
		runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", callCount, flowEnv)

		require.True(t, metricExists(t, env.endpoint, "sip_exporter_register_total"))
		require.Equal(t, float64(callCount),
			getMetric(t, env.endpoint, "sip_exporter_register_total"))
	})

	t.Run("unconfigured port absent", func(t *testing.T) {
		ctx := t.Context()
		configuredPort := freePort(t)
		unconfiguredPort := freePort(t)
		env := newTestEnvWithExtraEnv(ctx, t, "", map[string]string{
			"SIP_EXPORTER_SIP_PORTS": configuredPort,
		})
		dropEnv := &testEnv{
			endpoint:       env.endpoint,
			sippPort:       unconfiguredPort,
			sippClientPort: freePort(t),
		}
		runSippScenario(ctx, t, "reg_uas.xml", "reg_uac.xml", callCount, dropEnv)

		require.False(t, metricExists(t, env.endpoint, "sip_exporter_register_total"),
			"REGISTER series must be absent on an unconfigured port")
	})
}

// TestMultiPortINVITEFlow verifies full dialog capture across every configured
// SIP port of one interface. Exact totals make duplicate capture fail.
func TestMultiPortINVITEFlow(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	sipPorts := []string{freePort(t), freePort(t), freePort(t)}
	env := newTestEnvWithExtraEnv(ctx, t, "", map[string]string{
		"SIP_EXPORTER_SIP_PORTS": strings.Join(sipPorts, ","),
	})

	const callCount = 5
	for _, port := range sipPorts {
		flowEnv := &testEnv{
			endpoint:       env.endpoint,
			sippPort:       port,
			sippClientPort: freePort(t),
		}
		runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", callCount, flowEnv)
	}

	require.True(t, metricExists(t, env.endpoint, "sip_exporter_invite_total"))
	require.True(t, metricExists(t, env.endpoint, "sip_exporter_invite_200_total"))
	want := float64(len(sipPorts) * callCount)
	require.Equal(t, want, getMetric(t, env.endpoint, "sip_exporter_invite_total"))
	require.Equal(t, want, getMetric(t, env.endpoint, "sip_exporter_invite_200_total"))
	assertDialogTeardown(t, env.endpoint)
}
