//go:build e2e

package rtp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRTP_BothDirections verifies that both legs of the media flow are captured.
// SIPp UAC and UAS each stream G.711a RTP from their own media port, so the
// exporter must track two distinct streams (keyed by media endpoint + SSRC).
// Streams persist for the tracker TTL after the call, so once the dialog
// completes the rtp_active_streams gauge reflects both directions.
func TestRTP_BothDirections(t *testing.T) {
	ports := allocatePortsN(5)
	httpPort, uasSIP, uacSIP, uasMedia, uacMedia := ports[0], ports[1], ports[2], ports[3], ports[4]
	endpoint := startExporter(context.Background(), t, httpPort, uasSIP, "0", true)

	runSippRTP(context.Background(), t, uasSIP, uacSIP, uasMedia, uacMedia)

	// Both UAC→UAS and UAS→UAC legs observed → at least two active PCMA streams.
	require.Eventually(t, func() bool {
		return getRTPMetric(t, endpoint, "sip_exporter_rtp_active_streams") >= 2
	}, 15*time.Second, 500*time.Millisecond,
		"rtp_active_streams{codec=PCMA} must reflect both media directions (>=2)")
}
