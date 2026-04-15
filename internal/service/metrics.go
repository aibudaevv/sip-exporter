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
		statusServerTimeoutTotal          prometheus.Counter
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
		ser              prometheus.GaugeFunc
		inviteTotal      int64
		invite3xxTotal   int64
		invite200OKTotal int64

		// SEER metrics (RFC 6076)
		seer                 prometheus.GaugeFunc
		inviteEffectiveTotal int64

		// ISA metric (RFC 6076)
		isa                    prometheus.GaugeFunc
		inviteIneffectiveTotal int64

		// SCR metric (RFC 6076)
		scr                   prometheus.GaugeFunc
		sessionCompletedTotal int64

		// RRD metrics (RFC 6076 for REGISTER)
		rrdTotal uint64               // Суммарная задержка для расчета среднего
		rrdCount int64                // Количество измерений для среднего
		rrd      prometheus.GaugeFunc // Метрика RRD для REGISTER
	}

	Metricser interface {
		Request(in []byte)
		Response(in []byte, isInviteResponse bool)
		Invite200OK()
		SessionCompleted()
		UpdateRRD(delayMs float64)
		UpdateSession(size int)
		SystemError()
	}
)

func NewMetricser() Metricser {
	m := &metrics{
		sessions: newSessionsGauge(),
	}
	initRequestCounters(m)
	initStatusCounters(m)
	initSystemCounters(m)

	m.ser = newSER(m)
	m.seer = newSEER(m)
	m.isa = newISA(m)
	m.scr = newSCR(m)
	m.rrd = newRRD(m)

	return m
}

func initRequestCounters(m *metrics) {
	m.requestMessageTotal = newCounter("sip_exporter_message_total", "Total number of MESSAGE requests")
	m.requestPublishTotal = newCounter("sip_exporter_publish_total", "Total number of PUBLISH requests")
	m.requestPrackTotal = newCounter("sip_exporter_prack_total", "Total number of PRACK requests")
	m.requestNotifyTotal = newCounter("sip_exporter_notify_total", "Total number of NOTIFY requests")
	m.requestSubscribeTotal = newCounter("sip_exporter_subscribe_total", "Total number of SUBSCRIBE requests")
	m.requestReferTotal = newCounter("sip_exporter_refer_total", "Total number of REFER requests")
	m.requestInfoTotal = newCounter("sip_exporter_info_total", "Total number of INFO requests")
	m.requestUpdateTotal = newCounter("sip_exporter_update_total", "Total number of UPDATE requests")
	m.requestRegisterTotal = newCounter("sip_exporter_register_total", "Total number of REGISTER requests")
	m.requestOptionsTotal = newCounter("sip_exporter_options_total", "Total number of OPTIONS requests")
	m.requestCancelTotal = newCounter("sip_exporter_cancel_total", "Total number of CANCEL requests")
	m.requestByeTotal = newCounter("sip_exporter_bye_total", "Total number of BYE requests")
	m.requestACKTotal = newCounter("sip_exporter_ack_total", "Total number of ACK requests")
	m.requestInviteTotal = newCounter("sip_exporter_invite_total", "Total number of INVITE requests")
}

func initStatusCounters(m *metrics) {
	m.statusDeclineTotal = newCounter("sip_exporter_603_total", "Total number of 603 Decline responses")
	m.statusBusyEverywhereTotal = newCounter("sip_exporter_600_total", "Total number of 600 Busy Everywhere responses")
	m.statusServiceUnavailableTotal = newCounter(
		"sip_exporter_503_total",
		"Total number of 503 Service Unavailable responses",
	)
	m.statusServerTimeoutTotal = newCounter("sip_exporter_504_total", "Total number of 504 Server Time-out responses")
	m.statusServerInternalTotal = newCounter(
		"sip_exporter_500_total",
		"Total number of 500 Server Internal Error responses",
	)
	m.statusRequestTimeoutTotal = newCounter("sip_exporter_408_total", "Total number of 408 Request Timeout responses")
	m.statusForbiddenTotal = newCounter("sip_exporter_403_total", "Total number of 403 Forbidden responses")
	m.statusUnauthorizedTotal = newCounter("sip_exporter_401_total", "Total number of 401 Unauthorized responses")
	m.statusBadRequestTotal = newCounter("sip_exporter_400_total", "Total number of 400 Bad Request responses")
	m.statusMovedTemporarilyTotal = newCounter(
		"sip_exporter_302_total",
		"Total number of 302 Moved Temporarily responses",
	)
	m.statusMultipleChoiceTotal = newCounter("sip_exporter_300_total", "Total number of 300 Multiple Choices responses")
	m.statusAcceptedTotal = newCounter("sip_exporter_202_total", "Total number of 202 Accepted responses")
	m.statusTryingTotal = newCounter("sip_exporter_100_total", "Total number of 100 Trying responses")
	m.statusOKTotal = newCounter("sip_exporter_200_total", "Total number of 200 OK responses")
	m.statusNotFoundTotal = newCounter("sip_exporter_404_total", "Total number of 404 Not Found responses")
	m.statusSessionProgressTotal = newCounter(
		"sip_exporter_183_total",
		"Total number of 183 Session Progress responses",
	)
	m.statusRingingTotal = newCounter("sip_exporter_180_total", "Total number of 180 Ringing responses")
	m.statusBusyHereTotal = newCounter("sip_exporter_486_total", "Total number of 486 Busy Here responses")
	m.statusTemporarilyUnavailableTotal = newCounter(
		"sip_exporter_480_total",
		"Total number of 480 Temporarily Unavailable responses",
	)
}

func initSystemCounters(m *metrics) {
	m.systemErrorTotal = newCounter("sip_exporter_system_error_total", "Total number of internal SIP exporter errors")
	m.sipPacketsTotal = newCounter("sip_exporter_packets_total", "Total number of SIP packets processed")
	m.proxyAuthenticationRequired = newCounter("sip_exporter_proxy_authentication_required_total",
		"Total number of 407 Proxy Authentication Required responses")
}

func newSessionsGauge() prometheus.Gauge {
	return promauto.NewGauge(prometheus.GaugeOpts{
		Name: "sip_exporter_sessions",
		Help: "Number of active SIP dialogs",
	})
}

func newCounter(name, help string) prometheus.Counter {
	return promauto.NewCounter(prometheus.CounterOpts{
		Name: name,
		Help: help,
	})
}

func newSER(m *metrics) prometheus.GaugeFunc {
	return promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sip_exporter_ser",
		Help: "Session Establishment Ratio percentage (RFC 6076)",
	}, func() float64 {
		total := atomic.LoadInt64(&m.inviteTotal)
		if total == 0 {
			return 0
		}
		threeXX := atomic.LoadInt64(&m.invite3xxTotal)
		denominator := total - threeXX
		if denominator == 0 {
			return 0
		}
		ok := atomic.LoadInt64(&m.invite200OKTotal)
		return float64(ok) / float64(denominator) * 100 //nolint:mnd // percentage formula
	})
}

func newSEER(m *metrics) prometheus.GaugeFunc {
	return promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sip_exporter_seer",
		Help: "Session Establishment Effectiveness Ratio percentage (RFC 6076)",
	}, func() float64 {
		total := atomic.LoadInt64(&m.inviteTotal)
		if total == 0 {
			return 0
		}
		threeXX := atomic.LoadInt64(&m.invite3xxTotal)
		denominator := total - threeXX
		if denominator == 0 {
			return 0
		}
		effective := atomic.LoadInt64(&m.inviteEffectiveTotal)
		return float64(effective) / float64(denominator) * 100 //nolint:mnd // percentage formula
	})
}

func newISA(m *metrics) prometheus.GaugeFunc {
	return promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sip_exporter_isa",
		Help: "Ineffective Session Attempts percentage (RFC 6076)",
	}, func() float64 {
		total := atomic.LoadInt64(&m.inviteTotal)
		if total == 0 {
			return 0
		}
		ineffective := atomic.LoadInt64(&m.inviteIneffectiveTotal)
		return float64(ineffective) / float64(total) * 100 //nolint:mnd // percentage formula
	})
}

func newSCR(m *metrics) prometheus.GaugeFunc {
	return promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sip_exporter_scr",
		Help: "Session Completion Ratio percentage (RFC 6076)",
	}, func() float64 {
		total := atomic.LoadInt64(&m.inviteTotal)
		if total == 0 {
			return 0
		}
		completed := atomic.LoadInt64(&m.sessionCompletedTotal)
		return float64(completed) / float64(total) * 100 //nolint:mnd // percentage formula
	})
}

func newRRD(m *metrics) prometheus.GaugeFunc {
	return promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sip_exporter_rrd",
		Help: "Registration Request Delay in milliseconds (RFC 6076)",
	}, func() float64 {
		count := atomic.LoadInt64(&m.rrdCount)
		if count == 0 {
			return 0
		}
		total := atomic.LoadUint64(&m.rrdTotal)
		return float64(total) / float64(count) / 1e3 //nolint:mnd // convert microseconds to milliseconds
	})
}

func (m *metrics) UpdateRRD(delayMs float64) {
	atomic.AddInt64(&m.rrdCount, 1)
	//nolint:mnd // convert milliseconds to microseconds for precision
	atomic.AddUint64(&m.rrdTotal, uint64(delayMs*1e3))
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
	case "504":
		m.statusServerTimeoutTotal.Inc()
	case "600":
		m.statusBusyEverywhereTotal.Inc()
	case "603":
		m.statusDeclineTotal.Inc()
	default:
		zap.L().Warn("unknown response", zap.ByteString("in", in))
	}

	// Count 3xx for SER (RFC 6076)
	if isInviteResponse && len(in) == 3 && in[0] == '3' {
		atomic.AddInt64(&m.invite3xxTotal, 1)
	}

	// ADDED: SEER numerator
	if isInviteResponse && isEffectiveResponse(string(in)) {
		atomic.AddInt64(&m.inviteEffectiveTotal, 1)
	}

	// ADDED: ISA numerator
	if isInviteResponse && isIneffectiveResponse(string(in)) {
		atomic.AddInt64(&m.inviteIneffectiveTotal, 1)
	}
}

// isEffectiveResponse returns true if the response code is part of SEER numerator (RFC 6076).
func isEffectiveResponse(code string) bool {
	switch code {
	case "200", "480", "486", "600", "603":
		return true
	default:
		return false
	}
}

// isIneffectiveResponse returns true if the response code is part of ISA numerator (RFC 6076).
func isIneffectiveResponse(code string) bool {
	switch code {
	case "408", "500", "503", "504":
		return true
	default:
		return false
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
	case "MESSAGE":
		m.requestMessageTotal.Inc()
	default:
		zap.L().Warn("unknown request", zap.ByteString("in", in))
	}
}

func (m *metrics) Invite200OK() {
	atomic.AddInt64(&m.invite200OKTotal, 1)
}

func (m *metrics) SystemError() {
	m.systemErrorTotal.Inc()
}

func (m *metrics) SessionCompleted() {
	atomic.AddInt64(&m.sessionCompletedTotal, 1)
}
