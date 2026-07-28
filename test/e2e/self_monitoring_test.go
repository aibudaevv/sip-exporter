//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSelfMonitoringSocketPacketsReceived(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("after_traffic_received_gt_zero", func(t *testing.T) {
		t.Parallel()
		env := newTestEnv(ctx, t)
		runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 10, env)

		val := getMetric(t, env.endpoint, "sip_exporter_socket_packets_received_total")
		require.Greater(t, val, 0.0, "socket_packets_received_total should be > 0 after traffic")

		waitForSessionsZero(t, env.endpoint)
	})
}

func TestSelfMonitoringSocketPacketsDropped(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("no_drops_under_normal_load", func(t *testing.T) {
		t.Parallel()
		env := newTestEnv(ctx, t)
		runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 10, env)

		require.True(t, metricExists(t, env.endpoint, "sip_exporter_socket_packets_dropped_total"),
			"socket_packets_dropped_total metric should exist")
		val := getMetric(t, env.endpoint, "sip_exporter_socket_packets_dropped_total")
		require.Equal(t, 0.0, val, "socket_packets_dropped_total should be 0 (no drops expected)")

		waitForSessionsZero(t, env.endpoint)
	})
}

func TestSelfMonitoringChannelMetrics(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("channel_length_in_range", func(t *testing.T) {
		t.Parallel()
		env := newTestEnv(ctx, t)

		require.True(t, metricExists(t, env.endpoint, "sip_exporter_channel_length"),
			"channel_length metric should exist")
		length := getMetric(t, env.endpoint, "sip_exporter_channel_length")
		require.GreaterOrEqual(t, length, 0.0)
		require.LessOrEqual(t, length, 10000.0)
	})

	t.Run("channel_capacity_is_10000", func(t *testing.T) {
		t.Parallel()
		env := newTestEnv(ctx, t)

		require.True(t, metricExists(t, env.endpoint, "sip_exporter_channel_capacity"),
			"channel_capacity metric should exist")
		capacity := getMetric(t, env.endpoint, "sip_exporter_channel_capacity")
		require.Equal(t, 10000.0, capacity, "channel_capacity should be 10000")
	})
}

func TestSelfMonitoringParseErrorsZeroForValidTraffic(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("all_error_types_zero", func(t *testing.T) {
		t.Parallel()
		env := newTestEnv(ctx, t)
		runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 10, env)

		for _, errType := range []string{"l2", "l3", "l4", "sip", "vq"} {
			labelFilter := `type="` + errType + `"`
			if metricWithLabelExists(t, env.endpoint, "sip_exporter_parse_errors_total", labelFilter) {
				val := getMetricWithLabel(t, env.endpoint, "sip_exporter_parse_errors_total", labelFilter)
				require.Equal(t, 0.0, val, "parse_errors_total{type=%q} should be 0 for valid SIPp traffic", errType)
			}
		}

		waitForSessionsZero(t, env.endpoint)
	})
}

func TestSelfMonitoringActiveTrackers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("all_tracker_types_exist", func(t *testing.T) {
		t.Parallel()
		env := newTestEnv(ctx, t)

		for _, trackerType := range []string{"register", "invite", "options"} {
			labelFilter := `type="` + trackerType + `"`
			require.Eventually(t, func() bool {
				return metricWithLabelExists(t, env.endpoint, "sip_exporter_active_trackers", labelFilter)
			}, 5*time.Second, 500*time.Millisecond,
				"active_trackers{type=%q} should exist after first metrics tick", trackerType)
			val := getMetricWithLabel(t, env.endpoint, "sip_exporter_active_trackers", labelFilter)
			require.GreaterOrEqual(t, val, 0.0, "active_trackers{type=%q} should be >= 0", trackerType)
		}
	})
}

func TestSelfMonitoringActiveDialogs(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("zero_before_traffic", func(t *testing.T) {
		t.Parallel()
		env := newTestEnv(ctx, t)

		require.True(t, metricExists(t, env.endpoint, "sip_exporter_active_dialogs"),
			"active_dialogs metric should exist")
		val := getMetric(t, env.endpoint, "sip_exporter_active_dialogs")
		require.Equal(t, 0.0, val, "active_dialogs should be 0 before traffic")
	})

	t.Run("gt_zero_during_active_calls", func(t *testing.T) {
		t.Parallel()
		env := newTestEnv(ctx, t)

		sippDone := make(chan struct{})
		go func() {
			defer close(sippDone)
			runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 50, env)
		}()

		require.Eventually(t, func() bool {
			return getMetric(t, env.endpoint, "sip_exporter_active_dialogs") > 0
		}, 30*time.Second, 100*time.Millisecond, "active_dialogs should be > 0 during active calls")

		<-sippDone
		waitForSessionsZero(t, env.endpoint)
	})
}
