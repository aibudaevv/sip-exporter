package service

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func (m *metrics) initSelfMetrics(reg *prometheus.Registry) {
	m.socketPacketsReceived = newCounterVecWithRegistry(
		"sip_exporter_socket_packets_received_total",
		"Total number of packets received from kernel AF_PACKET socket.",
		[]string{"iface"},
		reg,
	)
	m.socketPacketsDropped = newCounterVecWithRegistry(
		"sip_exporter_socket_packets_dropped_total",
		"Total number of packets dropped by kernel due to socket receive buffer overflow.",
		[]string{"iface"},
		reg,
	)
	m.rtpDropped = newCounterWithRegistry(
		"sip_exporter_rtp_dropped_total",
		"RTP packets dropped in userspace due to full internal channel buffer.",
		reg,
	)
	m.parseErrorsTotal = newCounterVecWithRegistry(
		"sip_exporter_parse_errors_total",
		"Total number of packet parse errors by type.",
		[]string{"type"},
		reg,
	)
	m.channelLength = newGaugeWithRegistry(
		"sip_exporter_channel_length",
		"Current number of packets in the internal messages channel buffer.",
		reg,
	)
	m.channelCapacity = newGaugeWithRegistry(
		"sip_exporter_channel_capacity",
		"Capacity of the internal messages channel buffer.",
		reg,
	)
	m.activeTrackers = newGaugeVecWithRegistry(
		"sip_exporter_active_trackers",
		"Current number of entries in tracker maps.",
		[]string{"type"},
		reg,
	)
	m.activeDialogs = newGaugeWithRegistry(
		"sip_exporter_active_dialogs",
		"Current number of active SIP dialogs.",
		reg,
	)
}

func (m *metrics) ParseError(errorType string) {
	m.parseErrorsTotal.WithLabelValues(errorType).Inc()
}

func (m *metrics) SocketStats(stats []SocketStat) {
	for _, s := range stats {
		if s.Received > 0 {
			m.socketPacketsReceived.WithLabelValues(s.Iface).Add(float64(s.Received))
		}
		if s.Dropped > 0 {
			m.socketPacketsDropped.WithLabelValues(s.Iface).Add(float64(s.Dropped))
		}
	}
}

func (m *metrics) RTPDropped() {
	m.rtpDropped.Inc()
}

func (m *metrics) UpdateChannelLength(length int) {
	m.channelLength.Set(float64(length))
}

func (m *metrics) UpdateChannelCapacity(capacity int) {
	m.channelCapacity.Set(float64(capacity))
}

func (m *metrics) UpdateTrackerSize(trackerType string, size int) {
	m.activeTrackers.WithLabelValues(trackerType).Set(float64(size))
}

func (m *metrics) UpdateActiveDialogs(size int) {
	m.activeDialogs.Set(float64(size))
}

func newGaugeWithRegistry(name, help string, reg *prometheus.Registry) prometheus.Gauge {
	if reg != nil {
		g := prometheus.NewGauge(prometheus.GaugeOpts{
			Name: name,
			Help: help,
		})
		reg.MustRegister(g)
		return g
	}
	return promauto.NewGauge(prometheus.GaugeOpts{
		Name: name,
		Help: help,
	})
}
