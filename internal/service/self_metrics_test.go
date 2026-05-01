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

func getSelfCounterValue(reg *prometheus.Registry, name string) float64 {
	mfs, err := reg.Gather()
	if err != nil {
		return 0
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			for _, m := range mf.GetMetric() {
				return m.GetCounter().GetValue()
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

func getSelfCounterVecValue(reg *prometheus.Registry, name string, labels map[string]string) float64 {
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
		val := getSelfCounterVecValue(reg, "sip_exporter_parse_errors_total", map[string]string{"type": errType})
		require.Equal(t, 1.0, val, "parse_errors_total{type=%q}", errType)
	}
}

func TestSelfMetrics_ParseError_MultipleSameType(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.ParseError("l3")
	m.ParseError("l3")
	m.ParseError("l3")

	val := getSelfCounterVecValue(reg, "sip_exporter_parse_errors_total", map[string]string{"type": "l3"})
	require.Equal(t, 3.0, val)
}

func TestSelfMetrics_ParseError_Independence(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.ParseError("l2")
	m.ParseError("l2")
	m.ParseError("sip")

	require.Equal(t, 2.0, getSelfCounterVecValue(reg, "sip_exporter_parse_errors_total", map[string]string{"type": "l2"}))
	require.Equal(t, 1.0, getSelfCounterVecValue(reg, "sip_exporter_parse_errors_total", map[string]string{"type": "sip"}))
	require.Equal(t, 0.0, getSelfCounterVecValue(reg, "sip_exporter_parse_errors_total", map[string]string{"type": "l3"}))
	require.Equal(t, 0.0, getSelfCounterVecValue(reg, "sip_exporter_parse_errors_total", map[string]string{"type": "l4"}))
	require.Equal(t, 0.0, getSelfCounterVecValue(reg, "sip_exporter_parse_errors_total", map[string]string{"type": "vq"}))
}

func TestSelfMetrics_ParseError_UninitializedIsZero(t *testing.T) {
	_, reg := newTestMetricserWithRegistry()

	for _, errType := range []string{"l2", "l3", "l4", "sip", "vq"} {
		val := getSelfCounterVecValue(reg, "sip_exporter_parse_errors_total", map[string]string{"type": errType})
		require.Equal(t, 0.0, val, "parse_errors_total{type=%q} should be 0 initially", errType)
	}
}

func TestSelfMetrics_SocketStats_ReceivedOnly(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.SocketStats(100, 0)

	require.Equal(t, 100.0, getSelfCounterValue(reg, "sip_exporter_socket_packets_received_total"))
	require.Equal(t, 0.0, getSelfCounterValue(reg, "sip_exporter_socket_packets_dropped_total"))
}

func TestSelfMetrics_SocketStats_DroppedOnly(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.SocketStats(0, 5)

	require.Equal(t, 0.0, getSelfCounterValue(reg, "sip_exporter_socket_packets_received_total"))
	require.Equal(t, 5.0, getSelfCounterValue(reg, "sip_exporter_socket_packets_dropped_total"))
}

func TestSelfMetrics_SocketStats_Accumulation(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.SocketStats(100, 1)
	m.SocketStats(50, 2)

	require.Equal(t, 150.0, getSelfCounterValue(reg, "sip_exporter_socket_packets_received_total"))
	require.Equal(t, 3.0, getSelfCounterValue(reg, "sip_exporter_socket_packets_dropped_total"))
}

func TestSelfMetrics_SocketStats_ZeroDelta(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.SocketStats(0, 0)

	require.Equal(t, 0.0, getSelfCounterValue(reg, "sip_exporter_socket_packets_received_total"))
	require.Equal(t, 0.0, getSelfCounterValue(reg, "sip_exporter_socket_packets_dropped_total"))
}

func TestSelfMetrics_ChannelLength_Updates(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.UpdateChannelLength(0)
	require.Equal(t, 0.0, getSelfGaugeValue(reg, "sip_exporter_channel_length", nil))

	m.UpdateChannelLength(5000)
	require.Equal(t, 5000.0, getSelfGaugeValue(reg, "sip_exporter_channel_length", nil))

	m.UpdateChannelLength(10000)
	require.Equal(t, 10000.0, getSelfGaugeValue(reg, "sip_exporter_channel_length", nil))

	m.UpdateChannelLength(0)
	require.Equal(t, 0.0, getSelfGaugeValue(reg, "sip_exporter_channel_length", nil))
}

func TestSelfMetrics_ChannelCapacity(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.UpdateChannelCapacity(10000)
	require.Equal(t, 10000.0, getSelfGaugeValue(reg, "sip_exporter_channel_capacity", nil))
}

func TestSelfMetrics_TrackerSize_AllTypes(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.UpdateTrackerSize("register", 10)
	m.UpdateTrackerSize("invite", 50)
	m.UpdateTrackerSize("options", 5)

	require.Equal(t, 10.0, getSelfGaugeValue(reg, "sip_exporter_active_trackers", map[string]string{"type": "register"}))
	require.Equal(t, 50.0, getSelfGaugeValue(reg, "sip_exporter_active_trackers", map[string]string{"type": "invite"}))
	require.Equal(t, 5.0, getSelfGaugeValue(reg, "sip_exporter_active_trackers", map[string]string{"type": "options"}))
}

func TestSelfMetrics_TrackerSize_Updates(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.UpdateTrackerSize("invite", 100)
	require.Equal(t, 100.0, getSelfGaugeValue(reg, "sip_exporter_active_trackers", map[string]string{"type": "invite"}))

	m.UpdateTrackerSize("invite", 0)
	require.Equal(t, 0.0, getSelfGaugeValue(reg, "sip_exporter_active_trackers", map[string]string{"type": "invite"}))
}

func TestSelfMetrics_TrackerSize_Independence(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.UpdateTrackerSize("register", 10)
	m.UpdateTrackerSize("invite", 50)

	require.Equal(t, 10.0, getSelfGaugeValue(reg, "sip_exporter_active_trackers", map[string]string{"type": "register"}))
	require.Equal(t, 50.0, getSelfGaugeValue(reg, "sip_exporter_active_trackers", map[string]string{"type": "invite"}))
	require.Equal(t, 0.0, getSelfGaugeValue(reg, "sip_exporter_active_trackers", map[string]string{"type": "options"}))
}

func TestSelfMetrics_ActiveDialogs_Updates(t *testing.T) {
	m, reg := newTestMetricserWithRegistry()

	m.UpdateActiveDialogs(0)
	require.Equal(t, 0.0, getSelfGaugeValue(reg, "sip_exporter_active_dialogs", nil))

	m.UpdateActiveDialogs(42)
	require.Equal(t, 42.0, getSelfGaugeValue(reg, "sip_exporter_active_dialogs", nil))

	m.UpdateActiveDialogs(0)
	require.Equal(t, 0.0, getSelfGaugeValue(reg, "sip_exporter_active_dialogs", nil))
}
