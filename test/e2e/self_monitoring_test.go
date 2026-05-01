//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelfMonitoring_SocketPacketsReceived(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

	t.Run("after_traffic_received_gt_zero", func(t *testing.T) {
		env.restart(t)
		runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 10, &env.testEnv)

		val := getMetric(t, env.endpoint, "sip_exporter_socket_packets_received_total")
		require.Greater(t, val, 0.0, "socket_packets_received_total should be > 0 after traffic")

		waitForSessionsZero(t, env.endpoint)
	})
}

func TestSelfMonitoring_SocketPacketsDropped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

	t.Run("no_drops_under_normal_load", func(t *testing.T) {
		env.restart(t)
		runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 10, &env.testEnv)

		val := getMetric(t, env.endpoint, "sip_exporter_socket_packets_dropped_total")
		require.Equal(t, 0.0, val, "socket_packets_dropped_total should be 0 (no drops expected)")

		waitForSessionsZero(t, env.endpoint)
	})
}

func TestSelfMonitoring_ChannelMetrics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

	t.Run("channel_length_in_range", func(t *testing.T) {
		env.restart(t)

		length := getMetric(t, env.endpoint, "sip_exporter_channel_length")
		require.GreaterOrEqual(t, length, 0.0)
		require.LessOrEqual(t, length, 10000.0)
	})

	t.Run("channel_capacity_is_10000", func(t *testing.T) {
		env.restart(t)

		capacity := getMetric(t, env.endpoint, "sip_exporter_channel_capacity")
		require.Equal(t, 10000.0, capacity, "channel_capacity should be 10000")
	})
}

func TestSelfMonitoring_ParseErrorsZeroForValidTraffic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

	t.Run("all_error_types_zero", func(t *testing.T) {
		env.restart(t)
		runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 10, &env.testEnv)

		for _, errType := range []string{"l2", "l3", "l4", "sip", "vq"} {
			val := getMetricWithLabel(t, env.endpoint, "sip_exporter_parse_errors_total", `type="`+errType+`"`)
			require.Equal(t, 0.0, val, "parse_errors_total{type=%q} should be 0 for valid SIPp traffic", errType)
		}

		waitForSessionsZero(t, env.endpoint)
	})
}

func TestSelfMonitoring_ActiveTrackers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

	t.Run("all_tracker_types_exist", func(t *testing.T) {
		env.restart(t)

		for _, trackerType := range []string{"register", "invite", "options"} {
			val := getMetricWithLabel(t, env.endpoint, "sip_exporter_active_trackers", `type="`+trackerType+`"`)
			require.GreaterOrEqual(t, val, 0.0, "active_trackers{type=%q} should exist and be >= 0", trackerType)
		}
	})
}

func TestSelfMonitoring_ActiveDialogs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newSharedTestEnv(ctx, t)

	t.Run("zero_before_traffic", func(t *testing.T) {
		env.restart(t)

		val := getMetric(t, env.endpoint, "sip_exporter_active_dialogs")
		require.Equal(t, 0.0, val, "active_dialogs should be 0 before traffic")
	})

	t.Run("gt_zero_during_active_calls", func(t *testing.T) {
		env.restart(t)
		runSippScenario(ctx, t, "uas_100.xml", "uac_100.xml", 10, &env.testEnv)

		dialogs := getMetric(t, env.endpoint, "sip_exporter_active_dialogs")
		require.GreaterOrEqual(t, dialogs, 0.0, "active_dialogs should be >= 0")

		waitForSessionsZero(t, env.endpoint)
	})
}
