//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCarrierDirection_InviteResponseMismatch verifies that carrier is determined
// at INVITE time from the sender IP, not from the 200 OK responder IP.
//
// Setup: UAC at 10.1.0.1 (carrier-A) sends INVITE, UAS at 10.2.0.1 (carrier-B) replies 200 OK.
// The exporter should lock carrier to "carrier-A" (from INVITE source IP) and not switch
// to "carrier-B" on the 200 OK response.
//
// Carrier is locked to carrier-A from INVITE source IP, not switched to carrier-B on 200 OK response.
// IGNORE_OUTGOING=true on lo → each packet seen once.
// inviteTotal{carrier-A}=N, invite200OKTotal{carrier-A}=N → SER{carrier-A}≈100%.
// SER{carrier-B}=0 because carrier-B has no INVITEs (carrier resolved from INVITE tracker, not response IP).
// sessionCompletedTotal{carrier-A}=200 (dialog created with carrier-A), sessionCompletedTotal{carrier-B}=0.
func TestCarrierDirection_InviteResponseMismatch(t *testing.T) {
	ctx := context.Background()
	setupSecondaryIPs(t)

	carriersYAML := loadCarriersYAML(t, "carriers_direction.yaml")
	env := newTestEnvWithCarriersYAML(ctx, t, carriersYAML, "carrier-A")

	runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", 200, env, "10.2.0.1", "10.1.0.1")

	inviteA := getMetricWithCarrier(t, env.endpoint, "sip_exporter_invite_total", "carrier-A")
	t.Logf("invite_total{carrier-A}=%.0f", inviteA)
	require.Greater(t, inviteA, 0.0, "carrier-A should have INVITEs from UAC 10.1.0.1")

	serB := getMetricWithCarrier(t, env.endpoint, "sip_exporter_ser", "carrier-B")
	t.Logf("ser{carrier-B}=%.2f", serB)
	require.Equal(t, 0.0, serB, "SER for carrier-B should be 0 (no INVITEs from carrier-B IPs)")

	serA := getMetricWithCarrier(t, env.endpoint, "sip_exporter_ser", "carrier-A")
	t.Logf("ser{carrier-A}=%.2f", serA)
	require.Greater(t, serA, 0.0, "SER for carrier-A should be > 0 (direction fix works)")

	sdcA := getMetricWithCarrier(t, env.endpoint, "sip_exporter_sdc_total", "carrier-A")
	sdcB := getMetricWithCarrier(t, env.endpoint, "sip_exporter_sdc_total", "carrier-B")
	t.Logf("sdc_total{carrier-A}=%.0f, sdc_total{carrier-B}=%.0f", sdcA, sdcB)
	require.Equal(t, 200.0, sdcA, "carrier-A should have 200 completed sessions (dialog created with carrier-A)")
	require.Equal(t, 0.0, sdcB, "carrier-B should have 0 completed sessions")
	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestCarrierDirection_MultipleCarriers verifies that two carriers sending traffic
// through the same exporter get separate, non-mixed metrics.
//
// Session 1: UAC=10.1.0.1 (carrier-A) → UAS=10.2.0.1 (carrier-B), INVITE→200 OK.
//   - inviteTotal{carrier-A}=200, invite200OKTotal{carrier-A}=200 (tracker resolves to carrier-A).
//
// Session 2: UAC=10.2.0.1 (carrier-B) → UAS=10.1.0.1 (carrier-A), INVITE→480 Busy.
//   - inviteTotal{carrier-B}=200, no 200 OK → SER{carrier-B}=0.
//
// carrier-A has SER > 0 (its INVITEs get 200 OK).
// carrier-B has SER = 0 (its INVITEs get 480, not 200 OK).
func TestCarrierDirection_MultipleCarriers(t *testing.T) {
	ctx := context.Background()
	setupSecondaryIPs(t)

	carriersYAML := loadCarriersYAML(t, "carriers_direction.yaml")
	env := newTestEnvWithCarriersYAML(ctx, t, carriersYAML, "carrier-A")

	runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", 200, env, "10.2.0.1", "10.1.0.1")
	runSippScenarioWithIPs(ctx, t, "uas_busy.xml", "uac_busy.xml", 200, env, "10.1.0.1", "10.2.0.1")

	serA := getMetricWithCarrier(t, env.endpoint, "sip_exporter_ser", "carrier-A")
	serB := getMetricWithCarrier(t, env.endpoint, "sip_exporter_ser", "carrier-B")
	t.Logf("ser{carrier-A}=%.2f, ser{carrier-B}=%.2f", serA, serB)
	require.Greater(t, serA, 0.0, "carrier-A should have SER > 0 (200 OK responses)")
	require.Equal(t, 0.0, serB, "carrier-B should have SER = 0 (INVITEs only get 480, not 200 OK)")

	inviteA := getMetricWithCarrier(t, env.endpoint, "sip_exporter_invite_total", "carrier-A")
	inviteB := getMetricWithCarrier(t, env.endpoint, "sip_exporter_invite_total", "carrier-B")
	t.Logf("invite_total{carrier-A}=%.0f, invite_total{carrier-B}=%.0f", inviteA, inviteB)
	require.Greater(t, inviteA, 0.0, "carrier-A should have INVITEs")
	require.Greater(t, inviteB, 0.0, "carrier-B should have INVITEs")
	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestCarrierDirection_UnknownIPOther verifies that when carriers.yaml is configured,
// packets where neither source nor destination IP matches any CIDR receive carrier="other".
//
// UAC at 172.16.0.1 sends INVITE to UAS at 172.16.0.2 — both IPs are outside all CIDRs.
// resolveCarrier tries srcIP (172.16.0.1 → "other"), then dstIP (172.16.0.2 → "other") → "other".
func TestCarrierDirection_UnknownIPOther(t *testing.T) {
	ctx := context.Background()
	setupSecondaryIPs(t)

	carriersYAML := loadCarriersYAML(t, "carriers_direction.yaml")
	env := newTestEnvWithCarriersYAML(ctx, t, carriersYAML, "other")

	// Both UAS and UAC use IPs outside any carrier CIDR
	runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", 200, env, "172.16.0.2", "172.16.0.1")

	inviteOther := getMetricWithCarrier(t, env.endpoint, "sip_exporter_invite_total", "other")
	t.Logf("invite_total{other}=%.0f", inviteOther)
	require.Greater(t, inviteOther, 0.0, "carrier=other should have INVITEs from 172.16.0.1")

	inviteA := getMetricWithCarrier(t, env.endpoint, "sip_exporter_invite_total", "carrier-A")
	inviteB := getMetricWithCarrier(t, env.endpoint, "sip_exporter_invite_total", "carrier-B")
	require.Equal(t, 0.0, inviteA, "carrier-A should have no INVITEs")
	require.Equal(t, 0.0, inviteB, "carrier-B should have no INVITEs")
	assertSelfMonitoringHealthy(t, env.endpoint)
}

// TestCarrierDirection_OverlappingCIDRs verifies that when two carriers have
// overlapping CIDR ranges, the first matching carrier in YAML wins.
//
// carriers_direction_overlap.yaml defines:
//   - carrier-specific: 10.1.1.0/24 (listed first)
//   - carrier-broad:    10.1.0.0/16 (listed second)
//
// INVITE from 10.1.1.5 matches both, but carrier-specific should win.
func TestCarrierDirection_OverlappingCIDRs(t *testing.T) {
	ctx := context.Background()
	setupSecondaryIPs(t)

	carriersYAML := loadCarriersYAML(t, "carriers_direction_overlap.yaml")
	env := newTestEnvWithCarriersYAML(ctx, t, carriersYAML, "carrier-specific")

	runSippScenarioWithIPs(ctx, t, "uas_100.xml", "uac_100.xml", 200, env, "10.1.0.1", "10.1.1.5")

	inviteSpecific := getMetricWithCarrier(t, env.endpoint, "sip_exporter_invite_total", "carrier-specific")
	t.Logf("invite_total{carrier-specific}=%.0f", inviteSpecific)
	require.Greater(t, inviteSpecific, 0.0, "carrier-specific should match first for 10.1.1.5")

	inviteBroad := getMetricWithCarrier(t, env.endpoint, "sip_exporter_invite_total", "carrier-broad")
	t.Logf("invite_total{carrier-broad}=%.0f", inviteBroad)
	require.Equal(t, 0.0, inviteBroad, "carrier-broad should not match when carrier-specific is listed first")
	assertSelfMonitoringHealthy(t, env.endpoint)
}
