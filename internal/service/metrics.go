package service

import (
	"bytes"
	"sync/atomic"
	"time"

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

		// SPD metrics (RFC 6076 §4.5)
		spdTotalNs uint64               // Суммарная длительность сессий (наносекунды)
		spdCount   int64                // Количество завершённых сессий
		spd        prometheus.GaugeFunc // Метрика SPD
	}

	Metricser interface {
		Request(in []byte)
		Response(in []byte, isInviteResponse bool)
		ResponseWithMetrics(status []byte, isInviteResponse, is200OK bool)
		Invite200OK()
		SessionCompleted()
		UpdateRRD(delayMs float64)
		UpdateSPD(duration time.Duration)
		UpdateSession(size int)
		SystemError()
	}
)

func NewMetricser() Metricser {
	return newMetricserWithRegistry(nil)
}

func newMetricserWithRegistry(reg *prometheus.Registry) Metricser {
	m := &metrics{
		sessions: newSessionsGaugeWithRegistry(reg),
	}
	initRequestCountersWithRegistry(m, reg)
	initStatusCountersWithRegistry(m, reg)
	initSystemCountersWithRegistry(m, reg)

	m.ser = newSERWithRegistry(m, reg)
	m.seer = newSEERWithRegistry(m, reg)
	m.isa = newISAWithRegistry(m, reg)
	m.scr = newSCRWithRegistry(m, reg)
	m.rrd = newRRDWithRegistry(m, reg)
	m.spd = newSPDWithRegistry(m, reg)

	return m
}

func initRequestCountersWithRegistry(m *metrics, reg *prometheus.Registry) {
	m.requestMessageTotal = newCounterWithRegistry("sip_exporter_message_total", "Total number of MESSAGE requests", reg)
	m.requestPublishTotal = newCounterWithRegistry("sip_exporter_publish_total", "Total number of PUBLISH requests", reg)
	m.requestPrackTotal = newCounterWithRegistry("sip_exporter_prack_total", "Total number of PRACK requests", reg)
	m.requestNotifyTotal = newCounterWithRegistry("sip_exporter_notify_total", "Total number of NOTIFY requests", reg)
	m.requestSubscribeTotal = newCounterWithRegistry("sip_exporter_subscribe_total", "Total number of SUBSCRIBE requests", reg)
	m.requestReferTotal = newCounterWithRegistry("sip_exporter_refer_total", "Total number of REFER requests", reg)
	m.requestInfoTotal = newCounterWithRegistry("sip_exporter_info_total", "Total number of INFO requests", reg)
	m.requestUpdateTotal = newCounterWithRegistry("sip_exporter_update_total", "Total number of UPDATE requests", reg)
	m.requestRegisterTotal = newCounterWithRegistry("sip_exporter_register_total", "Total number of REGISTER requests", reg)
	m.requestOptionsTotal = newCounterWithRegistry("sip_exporter_options_total", "Total number of OPTIONS requests", reg)
	m.requestCancelTotal = newCounterWithRegistry("sip_exporter_cancel_total", "Total number of CANCEL requests", reg)
	m.requestByeTotal = newCounterWithRegistry("sip_exporter_bye_total", "Total number of BYE requests", reg)
	m.requestACKTotal = newCounterWithRegistry("sip_exporter_ack_total", "Total number of ACK requests", reg)
	m.requestInviteTotal = newCounterWithRegistry("sip_exporter_invite_total", "Total number of INVITE requests", reg)
}

func initStatusCountersWithRegistry(m *metrics, reg *prometheus.Registry) {
	m.statusDeclineTotal = newCounterWithRegistry("sip_exporter_603_total", "Total number of 603 Decline responses", reg)
	m.statusBusyEverywhereTotal = newCounterWithRegistry("sip_exporter_600_total", "Total number of 600 Busy Everywhere responses", reg)
	m.statusServiceUnavailableTotal = newCounterWithRegistry(
		"sip_exporter_503_total",
		"Total number of 503 Service Unavailable responses",
		reg,
	)
	m.statusServerTimeoutTotal = newCounterWithRegistry("sip_exporter_504_total", "Total number of 504 Server Time-out responses", reg)
	m.statusServerInternalTotal = newCounterWithRegistry(
		"sip_exporter_500_total",
		"Total number of 500 Server Internal Error responses",
		reg,
	)
	m.statusRequestTimeoutTotal = newCounterWithRegistry("sip_exporter_408_total", "Total number of 408 Request Timeout responses", reg)
	m.statusForbiddenTotal = newCounterWithRegistry("sip_exporter_403_total", "Total number of 403 Forbidden responses", reg)
	m.statusUnauthorizedTotal = newCounterWithRegistry("sip_exporter_401_total", "Total number of 401 Unauthorized responses", reg)
	m.statusBadRequestTotal = newCounterWithRegistry("sip_exporter_400_total", "Total number of 400 Bad Request responses", reg)
	m.statusMovedTemporarilyTotal = newCounterWithRegistry(
		"sip_exporter_302_total",
		"Total number of 302 Moved Temporarily responses",
		reg,
	)
	m.statusMultipleChoiceTotal = newCounterWithRegistry("sip_exporter_300_total", "Total number of 300 Multiple Choices responses", reg)
	m.statusAcceptedTotal = newCounterWithRegistry("sip_exporter_202_total", "Total number of 202 Accepted responses", reg)
	m.statusTryingTotal = newCounterWithRegistry("sip_exporter_100_total", "Total number of 100 Trying responses", reg)
	m.statusOKTotal = newCounterWithRegistry("sip_exporter_200_total", "Total number of 200 OK responses", reg)
	m.statusNotFoundTotal = newCounterWithRegistry("sip_exporter_404_total", "Total number of 404 Not Found responses", reg)
	m.statusSessionProgressTotal = newCounterWithRegistry(
		"sip_exporter_183_total",
		"Total number of 183 Session Progress responses",
		reg,
	)
	m.statusRingingTotal = newCounterWithRegistry("sip_exporter_180_total", "Total number of 180 Ringing responses", reg)
	m.statusBusyHereTotal = newCounterWithRegistry("sip_exporter_486_total", "Total number of 486 Busy Here responses", reg)
	m.statusTemporarilyUnavailableTotal = newCounterWithRegistry(
		"sip_exporter_480_total",
		"Total number of 480 Temporarily Unavailable responses",
		reg,
	)
}

func initSystemCountersWithRegistry(m *metrics, reg *prometheus.Registry) {
	m.systemErrorTotal = newCounterWithRegistry("sip_exporter_system_error_total", "Total number of internal SIP exporter errors", reg)
	m.sipPacketsTotal = newCounterWithRegistry("sip_exporter_packets_total", "Total number of SIP packets processed", reg)
	m.proxyAuthenticationRequired = newCounterWithRegistry("sip_exporter_proxy_authentication_required_total",
		"Total number of 407 Proxy Authentication Required responses", reg)
}

func newSessionsGaugeWithRegistry(reg *prometheus.Registry) prometheus.Gauge {
	if reg != nil {
		return prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "sip_exporter_sessions",
			Help: "Number of active SIP dialogs",
		})
	}
	return promauto.NewGauge(prometheus.GaugeOpts{
		Name: "sip_exporter_sessions",
		Help: "Number of active SIP dialogs",
	})
}

func newCounterWithRegistry(name, help string, reg *prometheus.Registry) prometheus.Counter {
	if reg != nil {
		return prometheus.NewCounter(prometheus.CounterOpts{
			Name: name,
			Help: help,
		})
	}
	return promauto.NewCounter(prometheus.CounterOpts{
		Name: name,
		Help: help,
	})
}

func newSERWithRegistry(m *metrics, reg *prometheus.Registry) prometheus.GaugeFunc {
	fn := func() float64 {
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
	}
	if reg != nil {
		return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "sip_exporter_ser",
			Help: "Session Establishment Ratio percentage (RFC 6076)",
		}, fn)
	}
	return promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sip_exporter_ser",
		Help: "Session Establishment Ratio percentage (RFC 6076)",
	}, fn)
}

func newSEERWithRegistry(m *metrics, reg *prometheus.Registry) prometheus.GaugeFunc {
	fn := func() float64 {
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
	}
	if reg != nil {
		return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "sip_exporter_seer",
			Help: "Session Establishment Effectiveness Ratio percentage (RFC 6076)",
		}, fn)
	}
	return promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sip_exporter_seer",
		Help: "Session Establishment Effectiveness Ratio percentage (RFC 6076)",
	}, fn)
}

func newISAWithRegistry(m *metrics, reg *prometheus.Registry) prometheus.GaugeFunc {
	fn := func() float64 {
		total := atomic.LoadInt64(&m.inviteTotal)
		if total == 0 {
			return 0
		}
		ineffective := atomic.LoadInt64(&m.inviteIneffectiveTotal)
		return float64(ineffective) / float64(total) * 100 //nolint:mnd // percentage formula
	}
	if reg != nil {
		return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "sip_exporter_isa",
			Help: "Ineffective Session Attempts percentage (RFC 6076)",
		}, fn)
	}
	return promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sip_exporter_isa",
		Help: "Ineffective Session Attempts percentage (RFC 6076)",
	}, fn)
}

func newSCRWithRegistry(m *metrics, reg *prometheus.Registry) prometheus.GaugeFunc {
	fn := func() float64 {
		total := atomic.LoadInt64(&m.inviteTotal)
		if total == 0 {
			return 0
		}
		completed := atomic.LoadInt64(&m.sessionCompletedTotal)
		return float64(completed) / float64(total) * 100 //nolint:mnd // percentage formula
	}
	if reg != nil {
		return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "sip_exporter_scr",
			Help: "Session Completion Ratio percentage (RFC 6076)",
		}, fn)
	}
	return promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sip_exporter_scr",
		Help: "Session Completion Ratio percentage (RFC 6076)",
	}, fn)
}

func newRRDWithRegistry(m *metrics, reg *prometheus.Registry) prometheus.GaugeFunc {
	fn := func() float64 {
		count := atomic.LoadInt64(&m.rrdCount)
		if count == 0 {
			return 0
		}
		total := atomic.LoadUint64(&m.rrdTotal)
		return float64(total) / float64(count) / 1e3 //nolint:mnd // convert microseconds to milliseconds
	}
	if reg != nil {
		return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "sip_exporter_rrd",
			Help: "Registration Request Delay in milliseconds (RFC 6076)",
		}, fn)
	}
	return promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sip_exporter_rrd",
		Help: "Registration Request Delay in milliseconds (RFC 6076)",
	}, fn)
}

func newSPDWithRegistry(m *metrics, reg *prometheus.Registry) prometheus.GaugeFunc {
	fn := func() float64 {
		count := atomic.LoadInt64(&m.spdCount)
		if count == 0 {
			return 0
		}
		totalNs := atomic.LoadUint64(&m.spdTotalNs)
		return float64(totalNs) / float64(count) / 1e9 //nolint:mnd // convert nanoseconds to seconds
	}
	if reg != nil {
		return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "sip_exporter_spd",
			Help: "Session Process Duration in seconds (RFC 6076)",
		}, fn)
	}
	return promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sip_exporter_spd",
		Help: "Session Process Duration in seconds (RFC 6076)",
	}, fn)
}

func (m *metrics) UpdateSPD(duration time.Duration) {
	if duration < 0 {
		return
	}
	atomic.AddInt64(&m.spdCount, 1)
	atomic.AddUint64(&m.spdTotalNs, uint64(duration.Nanoseconds()))
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

	switch {
	case bytes.Equal(in, []byte("100")):
		m.statusTryingTotal.Inc()
	case bytes.Equal(in, []byte("180")):
		m.statusRingingTotal.Inc()
	case bytes.Equal(in, []byte("183")):
		m.statusSessionProgressTotal.Inc()
	case bytes.Equal(in, []byte("200")):
		m.statusOKTotal.Inc()
	case bytes.Equal(in, []byte("202")):
		m.statusAcceptedTotal.Inc()
	case bytes.Equal(in, []byte("300")):
		m.statusMultipleChoiceTotal.Inc()
	case bytes.Equal(in, []byte("302")):
		m.statusMovedTemporarilyTotal.Inc()
	case bytes.Equal(in, []byte("400")):
		m.statusBadRequestTotal.Inc()
	case bytes.Equal(in, []byte("401")):
		m.statusUnauthorizedTotal.Inc()
	case bytes.Equal(in, []byte("403")):
		m.statusForbiddenTotal.Inc()
	case bytes.Equal(in, []byte("404")):
		m.statusNotFoundTotal.Inc()
	case bytes.Equal(in, []byte("407")):
		m.proxyAuthenticationRequired.Inc()
	case bytes.Equal(in, []byte("408")):
		m.statusRequestTimeoutTotal.Inc()
	case bytes.Equal(in, []byte("480")):
		m.statusTemporarilyUnavailableTotal.Inc()
	case bytes.Equal(in, []byte("486")):
		m.statusBusyHereTotal.Inc()
	case bytes.Equal(in, []byte("500")):
		m.statusServerInternalTotal.Inc()
	case bytes.Equal(in, []byte("503")):
		m.statusServiceUnavailableTotal.Inc()
	case bytes.Equal(in, []byte("504")):
		m.statusServerTimeoutTotal.Inc()
	case bytes.Equal(in, []byte("600")):
		m.statusBusyEverywhereTotal.Inc()
	case bytes.Equal(in, []byte("603")):
		m.statusDeclineTotal.Inc()
	default:
		zap.L().Warn("unknown response", zap.ByteString("in", in))
	}

	if isInviteResponse && len(in) == 3 && in[0] == '3' {
		atomic.AddInt64(&m.invite3xxTotal, 1)
	}

	if isInviteResponse && isEffectiveResponse(in) {
		atomic.AddInt64(&m.inviteEffectiveTotal, 1)
	}

	if isInviteResponse && isIneffectiveResponse(in) {
		atomic.AddInt64(&m.inviteIneffectiveTotal, 1)
	}
}

func (m *metrics) ResponseWithMetrics(status []byte, isInviteResponse, is200OK bool) {
	m.Response(status, isInviteResponse)

	if isInviteResponse && is200OK {
		atomic.AddInt64(&m.invite200OKTotal, 1)
	}
}

func isEffectiveResponse(code []byte) bool {
	return bytes.Equal(code, []byte("200")) ||
		bytes.Equal(code, []byte("480")) ||
		bytes.Equal(code, []byte("486")) ||
		bytes.Equal(code, []byte("600")) ||
		bytes.Equal(code, []byte("603"))
}

func isIneffectiveResponse(code []byte) bool {
	return bytes.Equal(code, []byte("408")) ||
		bytes.Equal(code, []byte("500")) ||
		bytes.Equal(code, []byte("503")) ||
		bytes.Equal(code, []byte("504"))
}

func (m *metrics) Request(in []byte) {
	defer m.sipPacketsTotal.Inc()

	switch {
	case bytes.Equal(in, []byte("PUBLISH")):
		m.requestPublishTotal.Inc()
	case bytes.Equal(in, []byte("PRACK")):
		m.requestPrackTotal.Inc()
	case bytes.Equal(in, []byte("NOTIFY")):
		m.requestNotifyTotal.Inc()
	case bytes.Equal(in, []byte("SUBSCRIBE")):
		m.requestSubscribeTotal.Inc()
	case bytes.Equal(in, []byte("REFER")):
		m.requestReferTotal.Inc()
	case bytes.Equal(in, []byte("INFO")):
		m.requestInfoTotal.Inc()
	case bytes.Equal(in, []byte("UPDATE")):
		m.requestUpdateTotal.Inc()
	case bytes.Equal(in, []byte("REGISTER")):
		m.requestRegisterTotal.Inc()
	case bytes.Equal(in, []byte("OPTIONS")):
		m.requestOptionsTotal.Inc()
	case bytes.Equal(in, []byte("CANCEL")):
		m.requestCancelTotal.Inc()
	case bytes.Equal(in, []byte("BYE")):
		m.requestByeTotal.Inc()
	case bytes.Equal(in, []byte("ACK")):
		m.requestACKTotal.Inc()
	case bytes.Equal(in, []byte("INVITE")):
		m.requestInviteTotal.Inc()
		atomic.AddInt64(&m.inviteTotal, 1)
	case bytes.Equal(in, []byte("MESSAGE")):
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
