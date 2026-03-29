package service

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

type (
	metrics struct {
		proxyAuthenticationRequired       prometheus.Counter
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

		// SER metrics (RFC 6076)
		ser              prometheus.Gauge
		inviteTotal      int64
		invite3xxTotal   int64
		invite200OKTotal int64
	}

	Metricser interface {
		Request(in []byte)
		Response(in []byte, isInviteResponse bool)
		Invite200OK()
		UpdateSession(size int)
		SystemError()
	}
)

func NewMetricser() Metricser {
	return &metrics{
		sessions: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "sip_exporter_sessions",
			Help: "Number of active SIP dialogs",
		}),
		proxyAuthenticationRequired: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_proxy_authentication_required_total",
			Help: "Total number of 407 Proxy Authentication Required responses",
		}),
		systemErrorTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_system_error_total",
			Help: "Total number of internal SIP exporter errors",
		}),
		statusBusyHereTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_486_total",
			Help: "Total number of 486 Busy Here responses",
		}),
		statusTemporarilyUnavailableTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_480_total",
			Help: "Total number of 480 Temporarily Unavailable responses",
		}),
		requestMessageTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_message_total",
			Help: "Total number of MESSAGE requests",
		}),
		requestPublishTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_publish_total",
			Help: "Total number of PUBLISH requests",
		}),
		requestPrackTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_prack_total",
			Help: "Total number of PRACK requests",
		}),
		requestNotifyTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_notify_total",
			Help: "Total number of NOTIFY requests",
		}),
		requestSubscribeTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_subscribe_total",
			Help: "Total number of SUBSCRIBE requests",
		}),
		requestReferTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_refer_total",
			Help: "Total number of REFER requests",
		}),
		requestInfoTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_info_total",
			Help: "Total number of INFO requests",
		}),
		requestUpdateTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_update_total",
			Help: "Total number of UPDATE requests",
		}),
		requestRegisterTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_register_total",
			Help: "Total number of REGISTER requests",
		}),
		requestOptionsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_options_total",
			Help: "Total number of OPTIONS requests",
		}),
		requestCancelTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_cancel_total",
			Help: "Total number of CANCEL requests",
		}),
		requestByeTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_bye_total",
			Help: "Total number of BYE requests",
		}),
		requestACKTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_ack_total",
			Help: "Total number of ACK requests",
		}),
		statusDeclineTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_603_total",
			Help: "Total number of 603 Decline responses",
		}),
		statusBusyEverywhereTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_600_total",
			Help: "Total number of 600 Busy Everywhere responses",
		}),
		statusServiceUnavailableTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_503_total",
			Help: "Total number of 503 Service Unavailable responses",
		}),
		statusServerInternalTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_500_total",
			Help: "Total number of 500 Server Internal Error responses",
		}),
		statusRequestTimeoutTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_408_total",
			Help: "Total number of 408 Request Timeout responses",
		}),
		statusForbiddenTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_403_total",
			Help: "Total number of 403 Forbidden responses",
		}),
		statusUnauthorizedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_401_total",
			Help: "Total number of 401 Unauthorized responses",
		}),
		statusBadRequestTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_400_total",
			Help: "Total number of 400 Bad Request responses",
		}),
		statusMovedTemporarilyTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_302_total",
			Help: "Total number of 302 Moved Temporarily responses",
		}),
		statusMultipleChoiceTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_300_total",
			Help: "Total number of 300 Multiple Choices responses",
		}),
		statusAcceptedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_202_total",
			Help: "Total number of 202 Accepted responses",
		}),
		statusTryingTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_100_total",
			Help: "Total number of 100 Trying responses",
		}),
		sipPacketsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_packets_total",
			Help: "Total number of SIP packets processed",
		}),
		requestInviteTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_invite_total",
			Help: "Total number of INVITE requests",
		}),
		statusOKTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_200_total",
			Help: "Total number of 200 OK responses",
		}),
		statusNotFoundTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_404_total",
			Help: "Total number of 404 Not Found responses",
		}),
		statusSessionProgressTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_183_total",
			Help: "Total number of 183 Session Progress responses",
		}),
		statusRingingTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "sip_exporter_180_total",
			Help: "Total number of 180 Ringing responses",
		}),

		// SER metrics (RFC 6076)
		ser: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "sip_exporter_ser",
			Help: "Session Establishment Ratio percentage (RFC 6076)",
		}),
	}
}

func (m *metrics) UpdateSession(size int) {
	m.sessions.Set(float64(size))
}

func (m *metrics) Response(in []byte, isInviteResponse bool) {
	defer m.sipPacketsTotal.Inc()

	switch string(in) {
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
	case "407":
		m.proxyAuthenticationRequired.Inc()
	case "408":
		m.statusRequestTimeoutTotal.Inc()
	case "480":
		m.statusTemporarilyUnavailableTotal.Inc()
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
		zap.L().Warn("unknown response", zap.ByteString("in", in))
	}

	// Считаем 3xx для SER (RFC 6076)
	if isInviteResponse && len(in) == 3 && in[0] == '3' {
		atomic.AddInt64(&m.invite3xxTotal, 1)
		m.updateSER()
	}
}

func (m *metrics) Request(in []byte) {
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
		atomic.AddInt64(&m.inviteTotal, 1)
		m.updateSER()
	case "MESSAGE":
		m.requestMessageTotal.Inc()
	default:
		zap.L().Warn("unknown request", zap.ByteString("in", in))
	}
}

func (m *metrics) Invite200OK() {
	atomic.AddInt64(&m.invite200OKTotal, 1)
	m.updateSER()
}

func (m *metrics) SystemError() {
	m.systemErrorTotal.Inc()
}

// updateSER вычисляет Session Establishment Ratio по формуле RFC 6076:
// SER = (INVITE → 200 OK) / (Total INVITE - INVITE → 3xx) × 100
func (m *metrics) updateSER() {
	total := atomic.LoadInt64(&m.inviteTotal)
	if total == 0 {
		return
	}

	threeXX := atomic.LoadInt64(&m.invite3xxTotal)
	denominator := total - threeXX

	if denominator == 0 {
		return
	}

	ok := atomic.LoadInt64(&m.invite200OKTotal)
	ser := float64(ok) / float64(denominator) * 100
	
	if m.ser != nil {
		m.ser.Set(ser)
	}
}
