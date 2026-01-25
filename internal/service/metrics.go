package service

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

type (
	metrics struct {
		statusBusyHereTotal               prometheus.Counter
		statusTemporarilyUnavailableTotal prometheus.Counter
		statusOKTotal                     prometheus.Counter
		statusNotFoundTotal               prometheus.Counter
		statusSessionProgressTotal        prometheus.Counter
		statusRingingTotal                prometheus.Counter
		statusTryingTotal                 prometheus.Counter
		statusAcceptedTotal               prometheus.Counter
		statusMultipleChoiceTotal         prometheus.Counter
		statusMovedTemporarilyTotal       prometheus.Counter
		statusBadRequestTotal             prometheus.Counter
		statusUnauthorizedTotal           prometheus.Counter
		statusForbiddenTotal              prometheus.Counter
		statusRequestTimeoutTotal         prometheus.Counter
		statusServerInternalTotal         prometheus.Counter
		statusServiceUnavailableTotal     prometheus.Counter
		statusBusyEverywhereTotal         prometheus.Counter
		statusDeclineTotal                prometheus.Counter
		requestInviteTotal                prometheus.Counter
		requestACKTotal                   prometheus.Counter
		requestByeTotal                   prometheus.Counter
		requestCancelTotal                prometheus.Counter
		requestOptionsTotal               prometheus.Counter
		requestRegisterTotal              prometheus.Counter
		requestUpdateTotal                prometheus.Counter
		requestInfoTotal                  prometheus.Counter
		requestReferTotal                 prometheus.Counter
		requestSubscribeTotal             prometheus.Counter
		requestNotifyTotal                prometheus.Counter
		requestPrackTotal                 prometheus.Counter
		requestPublishTotal               prometheus.Counter
		requestMessageTotal               prometheus.Counter
		systemErrorTotal                  prometheus.Counter
		sipPacketsTotal                   prometheus.Counter
		sessions                          prometheus.Gauge
	}
	Metricser interface {
		StatusOrCode(in []byte)
		SystemError()
	}
)

func NewMetricser() Metricser {
	return &metrics{
		sessions: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "sip_exporter_sessions",
		}),
		systemErrorTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_system_error_total",
		}),
		statusBusyHereTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_486_total",
		}),
		statusTemporarilyUnavailableTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_480_total",
		}),
		requestMessageTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_message_total",
		}),
		requestPublishTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_publish_total",
		}),
		requestPrackTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_prack_total",
		}),
		requestNotifyTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_notify_total",
		}),
		requestSubscribeTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_subscribe_total",
		}),
		requestReferTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_refer_total",
		}),
		requestInfoTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_info_total",
		}),
		requestUpdateTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_update_total",
		}),
		requestRegisterTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_register_total",
		}),
		requestOptionsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_options_total",
		}),
		requestCancelTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_cancel_total",
		}),
		requestByeTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_bye_total",
		}),
		requestACKTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_ack_total",
		}),
		statusDeclineTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_603_total",
		}),
		statusBusyEverywhereTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_600_total",
		}),
		statusServiceUnavailableTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_503_total",
		}),
		statusServerInternalTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_500_total",
		}),
		statusRequestTimeoutTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_408_total",
		}),
		statusForbiddenTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_403_total",
		}),
		statusUnauthorizedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_401_total",
		}),
		statusBadRequestTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_400_total",
		}),
		statusMovedTemporarilyTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_302_total",
		}),
		statusMultipleChoiceTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_300_total",
		}),
		statusAcceptedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_202_total",
		}),
		statusTryingTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_100_total",
		}),
		sipPacketsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_packets_total",
		}),
		requestInviteTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_invite_total",
		}),
		statusOKTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_200_total",
		}),
		statusNotFoundTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_404_total",
		}),
		statusSessionProgressTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_183_total",
		}),
		statusRingingTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_180_total",
		}),
	}
}

func (m *metrics) StatusOrCode(in []byte) {
	defer m.sipPacketsTotal.Inc()
	switch string(in) {
	case "PUBLISH":
		m.requestPublishTotal.Inc()
	case "PRACK":
		m.requestPrackTotal.Inc()
	case "NOTIFY":
		m.requestNotifyTotal.Inc()
	case "SUBSCRIBE":
		m.requestSubscribeTotal.Inc()
	case "REFER":
		m.requestReferTotal.Inc()
	case "INFO":
		m.requestInfoTotal.Inc()
	case "UPDATE":
		m.requestUpdateTotal.Inc()
	case "REGISTER":
		m.requestRegisterTotal.Inc()
	case "OPTIONS":
		m.requestOptionsTotal.Inc()
	case "CANCEL":
		m.requestCancelTotal.Inc()
	case "BYE":
		m.requestByeTotal.Inc()
	case "ACK":
		m.requestACKTotal.Inc()
	case "INVITE":
		m.requestInviteTotal.Inc()
	case "100":
		m.statusTryingTotal.Inc()
	case "180":
		m.statusRingingTotal.Inc()
	case "183":
		m.statusSessionProgressTotal.Inc()
	case "200":
		m.statusOKTotal.Inc()
	case "202":
		m.statusAcceptedTotal.Inc()
	case "300":
		m.statusMultipleChoiceTotal.Inc()
	case "302":
		m.statusMovedTemporarilyTotal.Inc()
	case "400":
		m.statusBadRequestTotal.Inc()
	case "401":
		m.statusUnauthorizedTotal.Inc()
	case "403":
		m.statusForbiddenTotal.Inc()
	case "404":
		m.statusNotFoundTotal.Inc()
	case "408":
		m.statusRequestTimeoutTotal.Inc()
	case "480":
		m.statusRequestTimeoutTotal.Inc()
	case "486":
		m.statusBusyHereTotal.Inc()
	case "500":
		m.statusServerInternalTotal.Inc()
	case "503":
		m.statusServiceUnavailableTotal.Inc()
	case "600":
		m.statusBusyEverywhereTotal.Inc()
	case "603":
		m.statusDeclineTotal.Inc()
	default:
		zap.L().Warn("unknown method or status", zap.ByteString("in", in))
	}
}

func (m *metrics) SystemError() {
	m.systemErrorTotal.Inc()
}
