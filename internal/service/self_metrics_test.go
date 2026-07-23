package service

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func newTestMetricserWithRegistry() (Metricser, *prometheus.Registry) {
	reg := prometheus.NewRegistry()
	return newMetricserWithRegistry(reg), reg
}

func getSelfCounterValue(reg *prometheus.Registry, name string, labels map[string]string) float64 {
	mfs, err := reg.Gather()
	if err != nil {
		return 0
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			for _, m := range mf.GetMetric() {
				if selfMatchLabels(m, labels) {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func getSelfGaugeValue(reg *prometheus.Registry, name string, labels map[string]string) float64 {
	mfs, err := reg.Gather()
	if err != nil {
		return 0
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			for _, m := range mf.GetMetric() {
				if selfMatchLabels(m, labels) {
					return m.GetGauge().GetValue()
				}
			}
		}
	}
	return 0
}

func getParseErrorsValue(reg *prometheus.Registry, labels map[string]string) float64 {
	mfs, err := reg.Gather()
	if err != nil {
		return 0
	}
	for _, mf := range mfs {
		if mf.GetName() == "sip_exporter_parse_errors_total" {
			for _, m := range mf.GetMetric() {
				if selfMatchLabels(m, labels) {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func selfMatchLabels(m *dto.Metric, labels map[string]string) bool {
	if len(labels) == 0 {
		return len(m.GetLabel()) == 0
	}
	matched := 0
	for _, l := range m.GetLabel() {
		if v, ok := labels[l.GetName()]; ok && v == l.GetValue() {
			matched++
		}
	}
	return matched == len(labels)
}

func TestSelfMetrics_ParseError_AllTypes(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	types := []string{"l2", "l3", "l4", "sip", "vq"}
	for _, errType := range types {
		m.ParseError(errType)
	}

	for _, errType := range types {
		val := getParseErrorsValue(reg, map[string]string{"type": errType})
		require.InDelta(t, 1.0, val, 0.01, "parse_errors_total{type=%q}", errType)
	}
}

func TestSelfMetrics_ParseError_MultipleSameType(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.ParseError("l3")
	m.ParseError("l3")
	m.ParseError("l3")

	val := getParseErrorsValue(reg, map[string]string{"type": "l3"})
	require.InDelta(t, 3.0, val, 0.01)
}

func TestSelfMetrics_ParseError_Independence(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.ParseError("l2")
	m.ParseError("l2")
	m.ParseError("sip")

	require.InDelta(t, 2.0, getParseErrorsValue(reg, map[string]string{"type": "l2"}), 0.01)
	require.InDelta(t, 1.0, getParseErrorsValue(reg, map[string]string{"type": "sip"}), 0.01)
	require.InDelta(t, 0.0, getParseErrorsValue(reg, map[string]string{"type": "l3"}), 0.01)
	require.InDelta(t, 0.0, getParseErrorsValue(reg, map[string]string{"type": "l4"}), 0.01)
	require.InDelta(t, 0.0, getParseErrorsValue(reg, map[string]string{"type": "vq"}), 0.01)
}

func TestSelfMetrics_ParseError_UninitializedIsZero(t *testing.T) {
	_, reg := newTestMetricserWithRegistry()

	for _, errType := range []string{"l2", "l3", "l4", "sip", "vq"} {
		val := getParseErrorsValue(reg, map[string]string{"type": errType})
		require.InDelta(t, 0.0, val, 0.01, "parse_errors_total{type=%q} should be 0 initially", errType)
	}
}

type socketStatCheck struct {
	metric string
	iface  string
	want   float64
}

func TestSelfMetrics_SocketStats(t *testing.T) {
	tests := []struct {
		name   string
		stats  []SocketStat
		checks []socketStatCheck
	}{
		{
			name:  "received_only",
			stats: []SocketStat{{Iface: "eth0", Received: 100}},
			checks: []socketStatCheck{
				{"sip_exporter_socket_packets_received_total", "eth0", 100.0},
				{"sip_exporter_socket_packets_dropped_total", "eth0", 0.0},
			},
		},
		{
			name:  "dropped_only",
			stats: []SocketStat{{Iface: "eth0", Dropped: 5}},
			checks: []socketStatCheck{
				{"sip_exporter_socket_packets_received_total", "eth0", 0.0},
				{"sip_exporter_socket_packets_dropped_total", "eth0", 5.0},
			},
		},
		{
			name: "accumulation",
			stats: []SocketStat{
				{Iface: "eth0", Received: 100, Dropped: 1},
				{Iface: "eth0", Received: 50, Dropped: 2},
			},
			checks: []socketStatCheck{
				{"sip_exporter_socket_packets_received_total", "eth0", 150.0},
				{"sip_exporter_socket_packets_dropped_total", "eth0", 3.0},
			},
		},
		{
			name:  "zero_delta",
			stats: []SocketStat{{Iface: "eth0"}},
			checks: []socketStatCheck{
				{"sip_exporter_socket_packets_received_total", "eth0", 0.0},
				{"sip_exporter_socket_packets_dropped_total", "eth0", 0.0},
			},
		},
		{
			name: "per_interface_independence",
			stats: []SocketStat{
				{Iface: "eth0", Received: 100, Dropped: 1},
				{Iface: "eth1", Received: 50, Dropped: 2},
			},
			checks: []socketStatCheck{
				{"sip_exporter_socket_packets_received_total", "eth0", 100.0},
				{"sip_exporter_socket_packets_dropped_total", "eth0", 1.0},
				{"sip_exporter_socket_packets_received_total", "eth1", 50.0},
				{"sip_exporter_socket_packets_dropped_total", "eth1", 2.0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, reg := newTestMetricserWithRegistry()
			m.SocketStats(tt.stats)
			for _, c := range tt.checks {
				require.InDelta(t, c.want,
					getSelfCounterValue(reg, c.metric, map[string]string{"iface": c.iface}),
					0.01, "%s{iface=%s}", c.metric, c.iface)
			}
		})
	}
}

func TestSelfMetrics_ChannelLength_Updates(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.UpdateChannelLength(0)
	require.InDelta(t, 0.0, getSelfGaugeValue(reg, "sip_exporter_channel_length", nil), 0.01)

	m.UpdateChannelLength(5000)
	require.InDelta(t, 5000.0, getSelfGaugeValue(reg, "sip_exporter_channel_length", nil), 0.01)

	m.UpdateChannelLength(10000)
	require.InDelta(t, 10000.0, getSelfGaugeValue(reg, "sip_exporter_channel_length", nil), 0.01)

	m.UpdateChannelLength(0)
	require.InDelta(t, 0.0, getSelfGaugeValue(reg, "sip_exporter_channel_length", nil), 0.01)
}

func TestSelfMetrics_ChannelCapacity(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.UpdateChannelCapacity(10000)
	require.InDelta(t, 10000.0, getSelfGaugeValue(reg, "sip_exporter_channel_capacity", nil), 0.01)
}

func TestSelfMetrics_TrackerSize_AllTypes(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.UpdateTrackerSize("register", 10)
	m.UpdateTrackerSize("invite", 50)
	m.UpdateTrackerSize("options", 5)

	trackerVal := func(typ string) float64 {
		return getSelfGaugeValue(reg, "sip_exporter_active_trackers", map[string]string{"type": typ})
	}
	require.InDelta(t, 10.0, trackerVal("register"), 0.01)
	require.InDelta(t, 50.0, trackerVal("invite"), 0.01)
	require.InDelta(t, 5.0, trackerVal("options"), 0.01)
}

func TestSelfMetrics_TrackerSize_Updates(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.UpdateTrackerSize("invite", 100)
	require.InDelta(t, 100.0, getSelfGaugeValue(reg, "sip_exporter_active_trackers",
		map[string]string{"type": "invite"}), 0.01)

	m.UpdateTrackerSize("invite", 0)
	require.InDelta(t, 0.0, getSelfGaugeValue(reg, "sip_exporter_active_trackers",
		map[string]string{"type": "invite"}), 0.01)
}

func TestSelfMetrics_TrackerSize_Independence(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.UpdateTrackerSize("register", 10)
	m.UpdateTrackerSize("invite", 50)

	require.InDelta(
		t,
		10.0,
		getSelfGaugeValue(reg, "sip_exporter_active_trackers", map[string]string{"type": "register"}),
		0.01,
	)
	require.InDelta(t, 50.0, getSelfGaugeValue(reg, "sip_exporter_active_trackers",
		map[string]string{"type": "invite"}), 0.01)
	require.InDelta(t, 0.0, getSelfGaugeValue(reg, "sip_exporter_active_trackers",
		map[string]string{"type": "options"}), 0.01)
}

func TestSelfMetrics_ActiveDialogs_Updates(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.UpdateActiveDialogs(0)
	require.InDelta(t, 0.0, getSelfGaugeValue(reg, "sip_exporter_active_dialogs", nil), 0.01)

	m.UpdateActiveDialogs(42)
	require.InDelta(t, 42.0, getSelfGaugeValue(reg, "sip_exporter_active_dialogs", nil), 0.01)

	m.UpdateActiveDialogs(0)
	require.InDelta(t, 0.0, getSelfGaugeValue(reg, "sip_exporter_active_dialogs", nil), 0.01)
}
