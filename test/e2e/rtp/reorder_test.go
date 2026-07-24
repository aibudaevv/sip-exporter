//go:build e2e

package rtp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRTP_OutOfOrderMetric verifies that sip_exporter_rtp_out_of_order_total
// is incremented when out-of-order RTP packets are detected (seq < maxSeq).
//
// Flow: SIPp establishes a SIP dialog with SDP (PCMA) and streams real RTP.
// After the stream is active (rtp_packets_total > 0), we inject 3 RTP packets
// with seq [1, 5, 3] — seq=3 after maxSeq=5 triggers reorder detection.
func TestRTP_OutOfOrderMetric(t *testing.T) {
	ports := allocatePortsN(6)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]

	endpoint := startExporterWithCarrierUA(context.Background(), t, httpPort, uasSIP,
		integrationCarriersYAML, integrationUserAgentsYAML, "")

	waitFn := startSippContainers(context.Background(), t,
		"uas_rtp.xml", "uac_rtp.xml",
		uasSIP, uacSIP, uasMedia, uacMedia,
		"127.0.0.1", "127.0.0.1")

	// Wait for RTP stream to be active and media endpoint registered.
	rtpLabels := []string{labelCarrier, labelUAType, labelCodec}
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtp_packets_total", rtpLabels...) > 0
	}, 15*time.Second, 500*time.Millisecond, "rtp_packets_total must be > 0 — dialog active")

	// Inject out-of-order RTP packets (seq 1, 5, 3) on the UAS media port.
	sendRTPOutOfOrder(t, uasMedia)

	// Complete the SIPp dialog.
	waitFn()

	// Assert reorder metric is present and > 0.
	require.Eventually(t, func() bool {
		return getMetricByLabel(t, endpoint, "sip_exporter_rtp_out_of_order_total", rtpLabels...) > 0
	}, 10*time.Second, 500*time.Millisecond,
		"rtp_out_of_order_total must be > 0 after injecting out-of-order RTP packets")
}
