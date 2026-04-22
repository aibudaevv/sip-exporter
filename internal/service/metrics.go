package service

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

type (
	carrierAtomicCounters struct {
		inviteTotal            atomic.Int64
		invite3xxTotal         atomic.Int64
		invite200OKTotal       atomic.Int64
		inviteEffectiveTotal   atomic.Int64
		inviteIneffectiveTotal atomic.Int64
		sessionCompletedTotal  atomic.Int64
	}

	metrics struct {
		systemErrorTotal prometheus.Counter
		sipPacketsTotal  prometheus.Counter

		requestInviteTotal    *prometheus.CounterVec
		requestACKTotal       *prometheus.CounterVec
		requestByeTotal       *prometheus.CounterVec
		requestCancelTotal    *prometheus.CounterVec
		requestOptionsTotal   *prometheus.CounterVec
		requestRegisterTotal  *prometheus.CounterVec
		requestUpdateTotal    *prometheus.CounterVec
		requestInfoTotal      *prometheus.CounterVec
		requestReferTotal     *prometheus.CounterVec
		requestSubscribeTotal *prometheus.CounterVec
		requestNotifyTotal    *prometheus.CounterVec
		requestPrackTotal     *prometheus.CounterVec
		requestPublishTotal   *prometheus.CounterVec
		requestMessageTotal   *prometheus.CounterVec

		statusOKTotal                     *prometheus.CounterVec
		statusTryingTotal                 *prometheus.CounterVec
		statusRingingTotal                *prometheus.CounterVec
		statusSessionProgressTotal        *prometheus.CounterVec
		statusAcceptedTotal               *prometheus.CounterVec
		statusMultipleChoiceTotal         *prometheus.CounterVec
		statusMovedTemporarilyTotal       *prometheus.CounterVec
		statusBadRequestTotal             *prometheus.CounterVec
		statusUnauthorizedTotal           *prometheus.CounterVec
		statusForbiddenTotal              *prometheus.CounterVec
		statusNotFoundTotal               *prometheus.CounterVec
		proxyAuthenticationRequired       *prometheus.CounterVec
		statusRequestTimeoutTotal         *prometheus.CounterVec
		statusTemporarilyUnavailableTotal *prometheus.CounterVec
		statusBusyHereTotal               *prometheus.CounterVec
		statusServerInternalTotal         *prometheus.CounterVec
		statusServiceUnavailableTotal     *prometheus.CounterVec
		statusServerTimeoutTotal          *prometheus.CounterVec
		statusBusyEverywhereTotal         *prometheus.CounterVec
		statusDeclineTotal                *prometheus.CounterVec

		sdc *prometheus.CounterVec
		iss *prometheus.CounterVec

		sessions *prometheus.GaugeVec

		rrd *prometheus.HistogramVec
		spd *prometheus.HistogramVec
		ttr *prometheus.HistogramVec
		ord *prometheus.HistogramVec
		lrd *prometheus.HistogramVec

		carrierCounters sync.Map
	}

	Metricser interface {
		Request(carrier string, in []byte)
		Response(carrier string, in []byte, isInviteResponse bool)
		ResponseWithMetrics(carrier string, status []byte, isInviteResponse, is200OK bool)
		Invite200OK(carrier string)
		SessionCompleted(carrier string)
		UpdateRRD(carrier string, delayMs float64)
		UpdateSPD(carrier string, duration time.Duration)
		UpdateTTR(carrier string, delayMs float64)
		UpdateORD(carrier string, delayMs float64)
		UpdateLRD(carrier string, delayMs float64)
		UpdateSession(carrier string, size int)
		UpdateSessionsByCarrier(counts map[string]int)
		SystemError()
	}

	ratioCollector struct {
		desc   *prometheus.Desc
		m      *metrics
		calcFn func(c *carrierAtomicCounters) float64
	}
)

func (rc *ratioCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- rc.desc
}

func (rc *ratioCollector) Collect(ch chan<- prometheus.Metric) {
	rc.m.carrierCounters.Range(func(key, value any) bool {
		carrier, ok := key.(string)
		if !ok {
			return true
		}
		counters, ok := value.(*carrierAtomicCounters)
		if !ok {
			return true
		}
		val := rc.calcFn(counters)
		ch <- prometheus.MustNewConstMetric(rc.desc, prometheus.GaugeValue, val, carrier)
		return true
	})
}

func NewMetricser() Metricser {
	return newMetricserWithRegistry(nil)
}

func newMetricserWithRegistry(reg *prometheus.Registry) Metricser {
	m := &metrics{
		systemErrorTotal: newCounterWithRegistry(
			"sip_exporter_system_error_total",
			"Total number of internal SIP exporter errors", reg),
		sipPacketsTotal: newCounterWithRegistry(
			"sip_exporter_packets_total",
			"Total number of SIP packets processed", reg),
	}
	m.initRequestCounters(reg)
	m.initStatusCounters(reg)
	m.initSessionMetrics(reg)
	m.initHistograms(reg)
	registerRatioCollectors(m, reg)
	return m
}

func (m *metrics) initRequestCounters(reg *prometheus.Registry) {
	cl := []string{"carrier"}
	m.requestInviteTotal = newCounterVecWithRegistry(
		"sip_exporter_invite_total",
		"Total number of INVITE requests", cl, reg)
	m.requestACKTotal = newCounterVecWithRegistry(
		"sip_exporter_ack_total",
		"Total number of ACK requests", cl, reg)
	m.requestByeTotal = newCounterVecWithRegistry(
		"sip_exporter_bye_total",
		"Total number of BYE requests", cl, reg)
	m.requestCancelTotal = newCounterVecWithRegistry(
		"sip_exporter_cancel_total",
		"Total number of CANCEL requests", cl, reg)
	m.requestOptionsTotal = newCounterVecWithRegistry(
		"sip_exporter_options_total",
		"Total number of OPTIONS requests", cl, reg)
	m.requestRegisterTotal = newCounterVecWithRegistry(
		"sip_exporter_register_total",
		"Total number of REGISTER requests", cl, reg)
	m.requestUpdateTotal = newCounterVecWithRegistry(
		"sip_exporter_update_total",
		"Total number of UPDATE requests", cl, reg)
	m.requestInfoTotal = newCounterVecWithRegistry(
		"sip_exporter_info_total",
		"Total number of INFO requests", cl, reg)
	m.requestReferTotal = newCounterVecWithRegistry(
		"sip_exporter_refer_total",
		"Total number of REFER requests", cl, reg)
	m.requestSubscribeTotal = newCounterVecWithRegistry(
		"sip_exporter_subscribe_total",
		"Total number of SUBSCRIBE requests", cl, reg)
	m.requestNotifyTotal = newCounterVecWithRegistry(
		"sip_exporter_notify_total",
		"Total number of NOTIFY requests", cl, reg)
	m.requestPrackTotal = newCounterVecWithRegistry(
		"sip_exporter_prack_total",
		"Total number of PRACK requests", cl, reg)
	m.requestPublishTotal = newCounterVecWithRegistry(
		"sip_exporter_publish_total",
		"Total number of PUBLISH requests", cl, reg)
	m.requestMessageTotal = newCounterVecWithRegistry(
		"sip_exporter_message_total",
		"Total number of MESSAGE requests", cl, reg)
}

func (m *metrics) initStatusCounters(reg *prometheus.Registry) {
	cl := []string{"carrier"}
	m.statusOKTotal = newCounterVecWithRegistry(
		"sip_exporter_200_total",
		"Total number of 200 OK responses", cl, reg)
	m.statusTryingTotal = newCounterVecWithRegistry(
		"sip_exporter_100_total",
		"Total number of 100 Trying responses", cl, reg)
	m.statusRingingTotal = newCounterVecWithRegistry(
		"sip_exporter_180_total",
		"Total number of 180 Ringing responses", cl, reg)
	m.statusSessionProgressTotal = newCounterVecWithRegistry(
		"sip_exporter_183_total",
		"Total number of 183 Session Progress responses", cl, reg)
	m.statusAcceptedTotal = newCounterVecWithRegistry(
		"sip_exporter_202_total",
		"Total number of 202 Accepted responses", cl, reg)
	m.statusMultipleChoiceTotal = newCounterVecWithRegistry(
		"sip_exporter_300_total",
		"Total number of 300 Multiple Choices responses", cl, reg)
	m.statusMovedTemporarilyTotal = newCounterVecWithRegistry(
		"sip_exporter_302_total",
		"Total number of 302 Moved Temporarily responses", cl, reg)
	m.statusBadRequestTotal = newCounterVecWithRegistry(
		"sip_exporter_400_total",
		"Total number of 400 Bad Request responses", cl, reg)
	m.statusUnauthorizedTotal = newCounterVecWithRegistry(
		"sip_exporter_401_total",
		"Total number of 401 Unauthorized responses", cl, reg)
	m.statusForbiddenTotal = newCounterVecWithRegistry(
		"sip_exporter_403_total",
		"Total number of 403 Forbidden responses", cl, reg)
	m.statusNotFoundTotal = newCounterVecWithRegistry(
		"sip_exporter_404_total",
		"Total number of 404 Not Found responses", cl, reg)
	m.proxyAuthenticationRequired = newCounterVecWithRegistry(
		"sip_exporter_proxy_authentication_required_total",
		"Total number of 407 Proxy Authentication Required responses",
		cl, reg)
	m.statusRequestTimeoutTotal = newCounterVecWithRegistry(
		"sip_exporter_408_total",
		"Total number of 408 Request Timeout responses", cl, reg)
	m.statusTemporarilyUnavailableTotal = newCounterVecWithRegistry(
		"sip_exporter_480_total",
		"Total number of 480 Temporarily Unavailable responses",
		cl, reg)
	m.statusBusyHereTotal = newCounterVecWithRegistry(
		"sip_exporter_486_total",
		"Total number of 486 Busy Here responses", cl, reg)
	m.statusServerInternalTotal = newCounterVecWithRegistry(
		"sip_exporter_500_total",
		"Total number of 500 Server Internal Error responses",
		cl, reg)
	m.statusServiceUnavailableTotal = newCounterVecWithRegistry(
		"sip_exporter_503_total",
		"Total number of 503 Service Unavailable responses",
		cl, reg)
	m.statusServerTimeoutTotal = newCounterVecWithRegistry(
		"sip_exporter_504_total",
		"Total number of 504 Server Time-out responses", cl, reg)
	m.statusBusyEverywhereTotal = newCounterVecWithRegistry(
		"sip_exporter_600_total",
		"Total number of 600 Busy Everywhere responses", cl, reg)
	m.statusDeclineTotal = newCounterVecWithRegistry(
		"sip_exporter_603_total",
		"Total number of 603 Decline responses", cl, reg)
}

func (m *metrics) initSessionMetrics(reg *prometheus.Registry) {
	cl := []string{"carrier"}
	m.sdc = newCounterVecWithRegistry(
		"sip_exporter_sdc_total",
		"Total number of completed SIP sessions", cl, reg)
	m.iss = newCounterVecWithRegistry(
		"sip_exporter_iss_total",
		"Total number of ineffective INVITE responses (408, 500, 503, 504) per RFC 6076 §4.8",
		cl, reg)
	m.sessions = newGaugeVecWithRegistry(
		"sip_exporter_sessions",
		"Number of active SIP dialogs", cl, reg)
}

func (m *metrics) initHistograms(reg *prometheus.Registry) {
	cl := []string{"carrier"}
	m.rrd = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_rrd",
		Help:    "Registration Request Delay in milliseconds (RFC 6076)",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000},
	}, cl, reg)
	m.spd = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_spd",
		Help:    "Session Process Duration in seconds (RFC 6076)",
		Buckets: []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600},
	}, cl, reg)
	m.ttr = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_ttr",
		Help:    "Time to First Response in milliseconds (time from INVITE to first provisional 1xx response)",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000},
	}, cl, reg)
	m.ord = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_ord",
		Help:    "OPTIONS Response Delay in milliseconds (RFC 3261 §11.1)",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000},
	}, cl, reg)
	m.lrd = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_lrd",
		Help:    "Location Registration Delay in milliseconds: delay between REGISTER and 3xx redirect response",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000},
	}, cl, reg)
}

func registerRatioCollectors(m *metrics, reg *prometheus.Registry) {
	registerRatioCollector(m, "sip_exporter_ser",
		"Session Establishment Ratio percentage (RFC 6076)", reg,
		func(c *carrierAtomicCounters) float64 {
			total := c.inviteTotal.Load()
			if total == 0 {
				return 0
			}
			threeXX := c.invite3xxTotal.Load()
			denominator := total - threeXX
			if denominator == 0 {
				return 0
			}
			return float64(c.invite200OKTotal.Load()) / float64(denominator) * 100 //nolint:mnd // percentage formula
		},
	)
	registerRatioCollector(m, "sip_exporter_seer",
		"Session Establishment Effectiveness Ratio percentage (RFC 6076)", reg,
		func(c *carrierAtomicCounters) float64 {
			total := c.inviteTotal.Load()
			if total == 0 {
				return 0
			}
			threeXX := c.invite3xxTotal.Load()
			denominator := total - threeXX
			if denominator == 0 {
				return 0
			}
			effective := float64(c.inviteEffectiveTotal.Load())
			return effective / float64(denominator) * 100 //nolint:mnd // percentage formula
		},
	)
	registerRatioCollector(m, "sip_exporter_isa",
		"Ineffective Session Attempts percentage (RFC 6076)", reg,
		func(c *carrierAtomicCounters) float64 {
			total := c.inviteTotal.Load()
			if total == 0 {
				return 0
			}
			return float64(c.inviteIneffectiveTotal.Load()) / float64(total) * 100 //nolint:mnd // percentage formula
		},
	)
	registerRatioCollector(m, "sip_exporter_scr",
		"Session Completion Ratio percentage (RFC 6076)", reg,
		func(c *carrierAtomicCounters) float64 {
			total := c.inviteTotal.Load()
			if total == 0 {
				return 0
			}
			return float64(c.sessionCompletedTotal.Load()) / float64(total) * 100 //nolint:mnd // percentage formula
		},
	)
	registerRatioCollector(m, "sip_exporter_asr",
		"Answer Seizure Ratio (ITU-T E.411): ratio of INVITE 200 OK to total INVITE requests", reg,
		func(c *carrierAtomicCounters) float64 {
			total := c.inviteTotal.Load()
			if total == 0 {
				return 0
			}
			return float64(c.invite200OKTotal.Load()) / float64(total) * 100 //nolint:mnd // percentage formula
		},
	)
	registerRatioCollector(m, "sip_exporter_ner",
		"Network Effectiveness Ratio (GSMA IR.42): percentage of INVITEs without server errors", reg,
		func(c *carrierAtomicCounters) float64 {
			total := c.inviteTotal.Load()
			if total == 0 {
				return 0
			}
			ineffective := c.inviteIneffectiveTotal.Load()
			return float64(total-ineffective) / float64(total) * 100 //nolint:mnd // percentage formula
		},
	)
}

func newCounterWithRegistry(name, help string, reg *prometheus.Registry) prometheus.Counter {
	if reg != nil {
		c := prometheus.NewCounter(prometheus.CounterOpts{
			Name: name,
			Help: help,
		})
		reg.MustRegister(c)
		return c
	}
	return promauto.NewCounter(prometheus.CounterOpts{
		Name: name,
		Help: help,
	})
}

func newCounterVecWithRegistry(name, help string, labels []string, reg *prometheus.Registry) *prometheus.CounterVec {
	if reg != nil {
		cv := prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: name,
			Help: help,
		}, labels)
		reg.MustRegister(cv)
		return cv
	}
	return promauto.NewCounterVec(prometheus.CounterOpts{
		Name: name,
		Help: help,
	}, labels)
}

func newHistogramVecWithRegistry(
	opts prometheus.HistogramOpts, labels []string, reg *prometheus.Registry,
) *prometheus.HistogramVec {
	if reg != nil {
		hv := prometheus.NewHistogramVec(opts, labels)
		reg.MustRegister(hv)
		return hv
	}
	return promauto.NewHistogramVec(opts, labels)
}

func newGaugeVecWithRegistry(name, help string, labels []string, reg *prometheus.Registry) *prometheus.GaugeVec {
	if reg != nil {
		gv := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: name,
			Help: help,
		}, labels)
		reg.MustRegister(gv)
		return gv
	}
	return promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: name,
		Help: help,
	}, labels)
}

func registerRatioCollector(
	m *metrics, name, help string, reg *prometheus.Registry,
	calcFn func(c *carrierAtomicCounters) float64,
) {
	rc := &ratioCollector{
		desc:   prometheus.NewDesc(name, help, []string{"carrier"}, nil),
		m:      m,
		calcFn: calcFn,
	}
	if reg != nil {
		reg.MustRegister(rc)
	} else {
		prometheus.MustRegister(rc)
	}
}

func (m *metrics) Request(carrier string, in []byte) {
	defer m.sipPacketsTotal.Inc()

	switch {
	case bytes.Equal(in, []byte("PUBLISH")):
		m.requestPublishTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("PRACK")):
		m.requestPrackTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("NOTIFY")):
		m.requestNotifyTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("SUBSCRIBE")):
		m.requestSubscribeTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("REFER")):
		m.requestReferTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("INFO")):
		m.requestInfoTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("UPDATE")):
		m.requestUpdateTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("REGISTER")):
		m.requestRegisterTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("OPTIONS")):
		m.requestOptionsTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("CANCEL")):
		m.requestCancelTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("BYE")):
		m.requestByeTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("ACK")):
		m.requestACKTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("INVITE")):
		m.requestInviteTotal.WithLabelValues(carrier).Inc()
		m.getOrCreateCarrierCounters(carrier).inviteTotal.Add(1)
	case bytes.Equal(in, []byte("MESSAGE")):
		m.requestMessageTotal.WithLabelValues(carrier).Inc()
	default:
		zap.L().Warn("unknown request", zap.ByteString("in", in))
	}
}

func (m *metrics) Response(carrier string, in []byte, isInviteResponse bool) {
	defer m.sipPacketsTotal.Inc()

	m.incrementStatusCodeCounter(carrier, in)

	if isInviteResponse && len(in) == 3 && in[0] == '3' {
		m.getOrCreateCarrierCounters(carrier).invite3xxTotal.Add(1)
	}

	if isInviteResponse && isEffectiveResponse(in) {
		m.getOrCreateCarrierCounters(carrier).inviteEffectiveTotal.Add(1)
	}

	if isInviteResponse && isIneffectiveResponse(in) {
		m.getOrCreateCarrierCounters(carrier).inviteIneffectiveTotal.Add(1)
		m.iss.WithLabelValues(carrier).Inc()
	}
}

func (m *metrics) incrementStatusCodeCounter(carrier string, in []byte) {
	switch {
	case bytes.Equal(in, []byte("100")):
		m.statusTryingTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("180")):
		m.statusRingingTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("183")):
		m.statusSessionProgressTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("200")):
		m.statusOKTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("202")):
		m.statusAcceptedTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("300")):
		m.statusMultipleChoiceTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("302")):
		m.statusMovedTemporarilyTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("400")):
		m.statusBadRequestTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("401")):
		m.statusUnauthorizedTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("403")):
		m.statusForbiddenTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("404")):
		m.statusNotFoundTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("407")):
		m.proxyAuthenticationRequired.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("408")):
		m.statusRequestTimeoutTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("480")):
		m.statusTemporarilyUnavailableTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("486")):
		m.statusBusyHereTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("500")):
		m.statusServerInternalTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("503")):
		m.statusServiceUnavailableTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("504")):
		m.statusServerTimeoutTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("600")):
		m.statusBusyEverywhereTotal.WithLabelValues(carrier).Inc()
	case bytes.Equal(in, []byte("603")):
		m.statusDeclineTotal.WithLabelValues(carrier).Inc()
	default:
		zap.L().Warn("unknown response", zap.ByteString("in", in))
	}
}

func (m *metrics) ResponseWithMetrics(carrier string, status []byte, isInviteResponse, is200OK bool) {
	m.Response(carrier, status, isInviteResponse)

	if isInviteResponse && is200OK {
		m.getOrCreateCarrierCounters(carrier).invite200OKTotal.Add(1)
	}
}

func (m *metrics) Invite200OK(carrier string) {
	m.getOrCreateCarrierCounters(carrier).invite200OKTotal.Add(1)
}

func (m *metrics) SessionCompleted(carrier string) {
	m.getOrCreateCarrierCounters(carrier).sessionCompletedTotal.Add(1)
	m.sdc.WithLabelValues(carrier).Inc()
}

func (m *metrics) UpdateSPD(carrier string, duration time.Duration) {
	if duration < 0 {
		return
	}
	m.spd.WithLabelValues(carrier).Observe(duration.Seconds())
}

func (m *metrics) UpdateRRD(carrier string, delayMs float64) {
	m.rrd.WithLabelValues(carrier).Observe(delayMs)
}

func (m *metrics) UpdateTTR(carrier string, delayMs float64) {
	m.ttr.WithLabelValues(carrier).Observe(delayMs)
}

func (m *metrics) UpdateORD(carrier string, delayMs float64) {
	m.ord.WithLabelValues(carrier).Observe(delayMs)
}

func (m *metrics) UpdateLRD(carrier string, delayMs float64) {
	m.lrd.WithLabelValues(carrier).Observe(delayMs)
}

func (m *metrics) UpdateSession(carrier string, size int) {
	m.sessions.WithLabelValues(carrier).Set(float64(size))
}

func (m *metrics) UpdateSessionsByCarrier(counts map[string]int) {
	m.sessions.Reset()
	for carrier, count := range counts {
		m.sessions.WithLabelValues(carrier).Set(float64(count))
	}
}

func (m *metrics) SystemError() {
	m.systemErrorTotal.Inc()
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

func (m *metrics) getOrCreateCarrierCounters(carrier string) *carrierAtomicCounters {
	if v, ok := m.carrierCounters.Load(carrier); ok {
		c, _ := v.(*carrierAtomicCounters)
		return c
	}
	c := &carrierAtomicCounters{}
	actual, _ := m.carrierCounters.LoadOrStore(carrier, c)
	result, _ := actual.(*carrierAtomicCounters)
	return result
}
