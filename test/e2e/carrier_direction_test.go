//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCarrierDirection(t *testing.T) {
	t.Parallel()

	const callCount = 200

	type ipRun struct {
		uas, uac, uasIP, uacIP string
	}

	tests := []struct {
		name           string
		carriersFile   string
		defaultCarrier string
		runs           []ipRun
		check          func(t *testing.T, endpoint string)
	}{
		{
			"InviteResponseMismatch", "carriers_direction.yaml", "carrier-A",
			[]ipRun{{"uas_100.xml", "uac_100.xml", "10.2.0.1", "10.1.0.1"}},
			func(t *testing.T, endpoint string) {
				inviteA := getMetricWithCarrier(t, endpoint, "sip_exporter_invite_total", "carrier-A")
				require.Greater(t, inviteA, 0.0, "carrier-A should have INVITEs")

				require.False(t, metricWithLabelExists(t, endpoint, "sip_exporter_ser", `carrier="carrier-B"`),
					"SER for carrier-B should be absent")
				serB := getMetricWithCarrier(t, endpoint, "sip_exporter_ser", "carrier-B")
				require.Equal(t, 0.0, serB)

				serA := getMetricWithCarrier(t, endpoint, "sip_exporter_ser", "carrier-A")
				require.Greater(t, serA, 0.0, "SER for carrier-A should be > 0")

				sdcA := getMetricWithCarrier(t, endpoint, "sip_exporter_sdc_total", "carrier-A")
				require.Equal(t, float64(callCount), sdcA, "carrier-A should have completed sessions")

				require.False(t, metricWithLabelExists(t, endpoint, "sip_exporter_sdc_total", `carrier="carrier-B"`),
					"SDC for carrier-B should be absent")
				sdcB := getMetricWithCarrier(t, endpoint, "sip_exporter_sdc_total", "carrier-B")
				require.Equal(t, 0.0, sdcB, "carrier-B should have 0 completed sessions")
			},
		},
		{
			"MultipleCarriers", "carriers_direction.yaml", "carrier-A",
			[]ipRun{
				{"uas_100.xml", "uac_100.xml", "10.2.0.1", "10.1.0.1"},
				{"uas_busy.xml", "uac_busy.xml", "10.1.0.1", "10.2.0.1"},
			},
			func(t *testing.T, endpoint string) {
				serA := getMetricWithCarrier(t, endpoint, "sip_exporter_ser", "carrier-A")
				require.Greater(t, serA, 0.0, "carrier-A should have SER > 0")

				require.True(t, metricWithLabelExists(t, endpoint, "sip_exporter_ser", `carrier="carrier-B"`),
					"SER for carrier-B should exist (0/200 = 0%%)")
				serB := getMetricWithCarrier(t, endpoint, "sip_exporter_ser", "carrier-B")
				require.Equal(t, 0.0, serB, "carrier-B should have SER = 0")

				inviteA := getMetricWithCarrier(t, endpoint, "sip_exporter_invite_total", "carrier-A")
				inviteB := getMetricWithCarrier(t, endpoint, "sip_exporter_invite_total", "carrier-B")
				require.Greater(t, inviteA, 0.0, "carrier-A should have INVITEs")
				require.Greater(t, inviteB, 0.0, "carrier-B should have INVITEs")
			},
		},
		{
			"UnknownIPOther", "carriers_direction.yaml", "other",
			[]ipRun{{"uas_100.xml", "uac_100.xml", "172.16.0.2", "172.16.0.1"}},
			func(t *testing.T, endpoint string) {
				inviteOther := getMetricWithCarrier(t, endpoint, "sip_exporter_invite_total", "other")
				require.Greater(t, inviteOther, 0.0, "carrier=other should have INVITEs")

				require.False(t, metricWithLabelExists(t, endpoint, "sip_exporter_invite_total", `carrier="carrier-A"`),
					"carrier-A should have no INVITEs")
				require.False(t, metricWithLabelExists(t, endpoint, "sip_exporter_invite_total", `carrier="carrier-B"`),
					"carrier-B should have no INVITEs")
				inviteA := getMetricWithCarrier(t, endpoint, "sip_exporter_invite_total", "carrier-A")
				inviteB := getMetricWithCarrier(t, endpoint, "sip_exporter_invite_total", "carrier-B")
				require.Equal(t, 0.0, inviteA)
				require.Equal(t, 0.0, inviteB)
			},
		},
		{
			"OverlappingCIDRs", "carriers_direction_overlap.yaml", "carrier-specific",
			[]ipRun{{"uas_100.xml", "uac_100.xml", "10.1.0.1", "10.1.1.5"}},
			func(t *testing.T, endpoint string) {
				inviteSpecific := getMetricWithCarrier(t, endpoint, "sip_exporter_invite_total", "carrier-specific")
				require.Greater(t, inviteSpecific, 0.0, "carrier-specific should match first")

				require.False(t, metricWithLabelExists(t, endpoint, "sip_exporter_invite_total", `carrier="carrier-broad"`),
					"carrier-broad should not match when carrier-specific is listed first")
				inviteBroad := getMetricWithCarrier(t, endpoint, "sip_exporter_invite_total", "carrier-broad")
				require.Equal(t, 0.0, inviteBroad)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			setupSecondaryIPs(t)

			carriersYAML := loadCarriersYAML(t, tt.carriersFile)
			env := newTestEnvWithCarriersYAML(ctx, t, carriersYAML, tt.defaultCarrier)

			for _, r := range tt.runs {
				runSippScenarioWithIPs(ctx, t, r.uas, r.uac, callCount, env, r.uasIP, r.uacIP)
			}

			tt.check(t, env.endpoint)

			assertSelfMonitoringHealthy(t, env.endpoint)
		})
	}
}
