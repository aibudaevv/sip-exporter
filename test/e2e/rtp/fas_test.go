//go:build e2e

package rtp

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const fasE2EThreshold = 3 * time.Second

// TestFASNoRTPFiresAfterThreshold verifies the FAS detection happy path
// end-to-end: a SIP dialog is established with SDP (media endpoints registered),
// but no RTP flows. After the FAS threshold, sip_exporter_fas_calls_total must
// fire — the call answered (200 OK) but carried no media within the window.
//
// Uses the no-RTP SIPp scenarios (uas_nortp/uac_nortp): they exchange INVITE +
// 200 OK with SDP, pause 10s (call held up), then BYE. No RTP is injected, so
// the FAS pending entry outlives the (short, 3s) threshold.
func TestFASNoRTPFiresAfterThreshold(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	endpoint := startExporterWithExtraEnv(t.Context(), t, httpPort, uasSIP, testInterface, "",
		map[string]string{"SIP_EXPORTER_FRAUD_FAS_THRESHOLD": fasE2EThreshold.String()})

	wait := startSippContainers(t.Context(), t,
		"uas_nortp.xml", "uac_nortp.xml", uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")

	// Dialog established (200 OK processed → FAS pending stored).
	require.Eventually(t, func() bool {
		return metricExists(t, endpoint, "sip_exporter_sessions") &&
			getMetricByLabel(t, endpoint, "sip_exporter_sessions") >= 1
	}, 10*time.Second, 200*time.Millisecond, "dialog must be established (200 OK)")

	// No RTP injected → after the threshold + ≤1s sweep tick, FAS must fire.
	require.Eventually(t, func() bool {
		return metricExists(t, endpoint, "sip_exporter_fas_calls_total") &&
			getMetricByLabel(t, endpoint, "sip_exporter_fas_calls_total") >= 1
	}, 20*time.Second, 500*time.Millisecond,
		"fas_calls_total must fire for an answered call with no RTP within the threshold")

	wait()
}

// TestFASRTPPreventsFire verifies the FAS clear path end-to-end: when ≥2 RTP
// packets arrive for the dialog's media endpoint before the threshold, FAS must
// NOT fire (real media observed). Complements the unit tests
// (TestFASRTPBeforeThresholdPreventsFire / TestFASSingleRTPDoesNotClear) by
// exercising the full BPF → correlation → clear → sweep path through the binary.
func TestFASRTPPreventsFire(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	uacMediaNum, _ := strconv.Atoi(uacMedia)
	endpoint := startExporterWithExtraEnv(t.Context(), t, httpPort, uasSIP, testInterface, "",
		map[string]string{"SIP_EXPORTER_FRAUD_FAS_THRESHOLD": fasE2EThreshold.String()})

	wait := startSippContainers(t.Context(), t,
		"uas_nortp.xml", "uac_nortp.xml", uasSIP, uacSIP, uasMedia, uacMedia, "127.0.0.1", "127.0.0.1")

	// Wait for the 200 OK to register media endpoints (FAS pending stored).
	require.Eventually(t, func() bool {
		return metricExists(t, endpoint, "sip_exporter_sessions") &&
			getMetricByLabel(t, endpoint, "sip_exporter_sessions") >= 1
	}, 10*time.Second, 200*time.Millisecond, "dialog must be established (200 OK)")

	// Inject ≥2 RTP packets to the UAC media endpoint → media established → FAS cleared.
	sendControlledRTP(t, uacMediaNum, []uint16{1, 2, 3, 4, 5})

	// Wait past the threshold so a pending entry would have fired if not cleared.
	time.Sleep(2 * fasE2EThreshold)

	// FAS must NOT have fired: counters only appear after the first increment.
	require.False(t, metricExists(t, endpoint, "sip_exporter_fas_calls_total"),
		"established media (≥2 RTP) must prevent FAS")

	wait()
}
