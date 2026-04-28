package service

import (
	"bytes"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	"gitlab.com/sip-exporter/internal/vq"
)

const compositeKeyParts = 2

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

		statusCounters map[string]*prometheus.CounterVec

		sdc *prometheus.CounterVec
		iss *prometheus.CounterVec

		sessions *prometheus.GaugeVec

		rrd *prometheus.HistogramVec
		spd *prometheus.HistogramVec
		ttr *prometheus.HistogramVec
		ord *prometheus.HistogramVec
		lrd *prometheus.HistogramVec

		vqNLR     *prometheus.HistogramVec
		vqJDR     *prometheus.HistogramVec
		vqBLD     *prometheus.HistogramVec
		vqGLD     *prometheus.HistogramVec
		vqRTD     *prometheus.HistogramVec
		vqESD     *prometheus.HistogramVec
		vqIAJ     *prometheus.HistogramVec
		vqMAJ     *prometheus.HistogramVec
		vqMOSLQ   *prometheus.HistogramVec
		vqMOSCQ   *prometheus.HistogramVec
		vqRLQ     *prometheus.HistogramVec
		vqRCQ     *prometheus.HistogramVec
		vqRERL    *prometheus.HistogramVec
		vqReports *prometheus.CounterVec

		carrierCounters sync.Map
	}

	Metricser interface {
		Request(carrier string, uaType string, in []byte)
		Response(carrier string, uaType string, in []byte, isInviteResponse bool)
		ResponseWithMetrics(carrier string, uaType string, status []byte, isInviteResponse, is200OK bool)
		Invite200OK(carrier string, uaType string)
		SessionCompleted(carrier string, uaType string)
		UpdateRRD(carrier string, uaType string, delayMs float64)
		UpdateSPD(carrier string, uaType string, duration time.Duration)
		UpdateTTR(carrier string, uaType string, delayMs float64)
		UpdateORD(carrier string, uaType string, delayMs float64)
		UpdateLRD(carrier string, uaType string, delayMs float64)
		UpdateSession(carrier string, uaType string, size int)
		UpdateSessionsByCarrierAndUA(counts map[string]map[string]int)
		UpdateVQReport(carrier string, uaType string, report *vq.SessionReport)
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
		keyStr, ok := key.(string)
		if !ok {
			return true
		}
		parts := strings.SplitN(keyStr, "\x00", compositeKeyParts) //nolint:mnd // split into exactly 2 parts
		carrier := parts[0]
		uaType := "other"
		if len(parts) == compositeKeyParts {
			uaType = parts[1]
		}
		counters, ok := value.(*carrierAtomicCounters)
		if !ok {
			return true
		}
		val := rc.calcFn(counters)
		ch <- prometheus.MustNewConstMetric(rc.desc, prometheus.GaugeValue, val, carrier, uaType)
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
	cl := []string{"carrier", "ua_type"}
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
	cl := []string{"carrier", "ua_type"}
	statusCodes := []struct {
		code string
		help string
	}{
		{"100", "Total number of 100 Trying responses"},
		{"180", "Total number of 180 Ringing responses"},
		{"181", "Total number of 181 Call Is Being Forwarded responses"},
		{"182", "Total number of 182 Queued responses"},
		{"183", "Total number of 183 Session Progress responses"},
		{"200", "Total number of 200 OK responses"},
		{"202", "Total number of 202 Accepted responses"},
		{"300", "Total number of 300 Multiple Choices responses"},
		{"302", "Total number of 302 Moved Temporarily responses"},
		{"400", "Total number of 400 Bad Request responses"},
		{"401", "Total number of 401 Unauthorized responses"},
		{"403", "Total number of 403 Forbidden responses"},
		{"404", "Total number of 404 Not Found responses"},
		{"405", "Total number of 405 Method Not Allowed responses"},
		{"407", "Total number of 407 Proxy Authentication Required responses"},
		{"408", "Total number of 408 Request Timeout responses"},
		{"480", "Total number of 480 Temporarily Unavailable responses"},
		{"481", "Total number of 481 Dialog/Transaction Does Not Exist responses"},
		{"486", "Total number of 486 Busy Here responses"},
		{"487", "Total number of 487 Request Terminated responses"},
		{"488", "Total number of 488 Not Acceptable Here responses"},
		{"500", "Total number of 500 Server Internal Error responses"},
		{"501", "Total number of 501 Not Implemented responses"},
		{"502", "Total number of 502 Bad Gateway responses"},
		{"503", "Total number of 503 Service Unavailable responses"},
		{"504", "Total number of 504 Server Time-out responses"},
		{"600", "Total number of 600 Busy Everywhere responses"},
		{"603", "Total number of 603 Decline responses"},
		{"604", "Total number of 604 Does Not Exist Anywhere responses"},
		{"606", "Total number of 606 Not Acceptable responses"},
	}
	m.statusCounters = make(map[string]*prometheus.CounterVec, len(statusCodes))
	for _, sc := range statusCodes {
		m.statusCounters[sc.code] = newCounterVecWithRegistry(
			"sip_exporter_"+sc.code+"_total", sc.help, cl, reg)
	}
	m.statusCounters["407"] = newCounterVecWithRegistry(
		"sip_exporter_proxy_authentication_required_total",
		"Total number of 407 Proxy Authentication Required responses", cl, reg)
}

func (m *metrics) initSessionMetrics(reg *prometheus.Registry) {
	cl := []string{"carrier", "ua_type"}
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
	cl := []string{"carrier", "ua_type"}
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
	m.vqNLR = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_nlr_percent",
		Help:    "Voice Quality Network Packet Loss Rate percentage (RFC 6035)",
		Buckets: []float64{0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100},
	}, cl, reg)
	m.vqJDR = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_jdr_percent",
		Help:    "Voice Quality Jitter Buffer Discard Rate percentage (RFC 6035)",
		Buckets: []float64{0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100},
	}, cl, reg)
	m.vqBLD = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_bld_percent",
		Help:    "Voice Quality Burst Loss Density percentage (RFC 6035)",
		Buckets: []float64{0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100},
	}, cl, reg)
	m.vqGLD = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_gld_percent",
		Help:    "Voice Quality Gap Loss Density percentage (RFC 6035)",
		Buckets: []float64{0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100},
	}, cl, reg)
	m.vqRTD = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_rtd_ms",
		Help:    "Voice Quality Round Trip Delay in milliseconds (RFC 6035)",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000},
	}, cl, reg)
	m.vqESD = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_esd_ms",
		Help:    "Voice Quality End System Delay in milliseconds (RFC 6035)",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000},
	}, cl, reg)
	m.vqIAJ = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_iaj_ms",
		Help:    "Voice Quality Interarrival Jitter in milliseconds (RFC 6035)",
		Buckets: []float64{0.1, 0.5, 1, 5, 10, 20, 50, 100, 200, 500},
	}, cl, reg)
	m.vqMAJ = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_maj_ms",
		Help:    "Voice Quality Mean Absolute Jitter in milliseconds (RFC 6035)",
		Buckets: []float64{0.1, 0.5, 1, 5, 10, 20, 50, 100, 200, 500},
	}, cl, reg)
	m.vqMOSLQ = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_mos_lq",
		Help:    "Voice Quality MOS Listening Quality score 1.0-4.9 (RFC 6035)",
		Buckets: []float64{1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0},
	}, cl, reg)
	m.vqMOSCQ = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_mos_cq",
		Help:    "Voice Quality MOS Conversational Quality score 1.0-4.9 (RFC 6035)",
		Buckets: []float64{1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0},
	}, cl, reg)
	m.vqRLQ = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_rlq",
		Help:    "Voice Quality R-factor Listening Quality 0-120 (RFC 6035)",
		Buckets: []float64{0, 10, 20, 30, 50, 60, 70, 80, 90, 100, 120},
	}, cl, reg)
	m.vqRCQ = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_rcq",
		Help:    "Voice Quality R-factor Conversational Quality 0-120 (RFC 6035)",
		Buckets: []float64{0, 10, 20, 30, 50, 60, 70, 80, 90, 100, 120},
	}, cl, reg)
	m.vqRERL = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_rerl_db",
		Help:    "Voice Quality Residual Echo Return Loss in dB (RFC 6035)",
		Buckets: []float64{0, 5, 10, 15, 20, 30, 40, 50, 60, 80, 100},
	}, cl, reg)
	m.vqReports = newCounterVecWithRegistry(
		"sip_exporter_vq_reports_total",
		"Total number of Voice Quality session reports processed (RFC 6035)",
		cl, reg)
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
		desc:   prometheus.NewDesc(name, help, []string{"carrier", "ua_type"}, nil),
		m:      m,
		calcFn: calcFn,
	}
	if reg != nil {
		reg.MustRegister(rc)
	} else {
		prometheus.MustRegister(rc)
	}
}

func (m *metrics) Request(carrier string, uaType string, in []byte) {
	defer m.sipPacketsTotal.Inc()

	switch {
	case bytes.Equal(in, []byte("PUBLISH")):
		m.requestPublishTotal.WithLabelValues(carrier, uaType).Inc()
	case bytes.Equal(in, []byte("PRACK")):
		m.requestPrackTotal.WithLabelValues(carrier, uaType).Inc()
	case bytes.Equal(in, []byte("NOTIFY")):
		m.requestNotifyTotal.WithLabelValues(carrier, uaType).Inc()
	case bytes.Equal(in, []byte("SUBSCRIBE")):
		m.requestSubscribeTotal.WithLabelValues(carrier, uaType).Inc()
	case bytes.Equal(in, []byte("REFER")):
		m.requestReferTotal.WithLabelValues(carrier, uaType).Inc()
	case bytes.Equal(in, []byte("INFO")):
		m.requestInfoTotal.WithLabelValues(carrier, uaType).Inc()
	case bytes.Equal(in, []byte("UPDATE")):
		m.requestUpdateTotal.WithLabelValues(carrier, uaType).Inc()
	case bytes.Equal(in, []byte("REGISTER")):
		m.requestRegisterTotal.WithLabelValues(carrier, uaType).Inc()
	case bytes.Equal(in, []byte("OPTIONS")):
		m.requestOptionsTotal.WithLabelValues(carrier, uaType).Inc()
	case bytes.Equal(in, []byte("CANCEL")):
		m.requestCancelTotal.WithLabelValues(carrier, uaType).Inc()
	case bytes.Equal(in, []byte("BYE")):
		m.requestByeTotal.WithLabelValues(carrier, uaType).Inc()
	case bytes.Equal(in, []byte("ACK")):
		m.requestACKTotal.WithLabelValues(carrier, uaType).Inc()
	case bytes.Equal(in, []byte("INVITE")):
		m.requestInviteTotal.WithLabelValues(carrier, uaType).Inc()
		m.getOrCreateCarrierCounters(carrier, uaType).inviteTotal.Add(1)
	case bytes.Equal(in, []byte("MESSAGE")):
		m.requestMessageTotal.WithLabelValues(carrier, uaType).Inc()
	default:
		zap.L().Warn("unknown request", zap.ByteString("in", in))
	}
}

func (m *metrics) Response(carrier string, uaType string, in []byte, isInviteResponse bool) {
	defer m.sipPacketsTotal.Inc()

	m.incrementStatusCodeCounter(carrier, uaType, in)

	if isInviteResponse && len(in) == 3 && in[0] == '3' {
		m.getOrCreateCarrierCounters(carrier, uaType).invite3xxTotal.Add(1)
	}

	if isInviteResponse && isEffectiveResponse(in) {
		m.getOrCreateCarrierCounters(carrier, uaType).inviteEffectiveTotal.Add(1)
	}

	if isInviteResponse && isIneffectiveResponse(in) {
		m.getOrCreateCarrierCounters(carrier, uaType).inviteIneffectiveTotal.Add(1)
		m.iss.WithLabelValues(carrier, uaType).Inc()
	}
}

func (m *metrics) incrementStatusCodeCounter(carrier string, uaType string, in []byte) {
	counter, ok := m.statusCounters[string(in)]
	if ok {
		counter.WithLabelValues(carrier, uaType).Inc()
		return
	}
	zap.L().Warn("unknown response", zap.ByteString("in", in))
}

func (m *metrics) ResponseWithMetrics(carrier string, uaType string, status []byte, isInviteResponse, is200OK bool) {
	m.Response(carrier, uaType, status, isInviteResponse)

	if isInviteResponse && is200OK {
		m.getOrCreateCarrierCounters(carrier, uaType).invite200OKTotal.Add(1)
	}
}

func (m *metrics) Invite200OK(carrier string, uaType string) {
	m.getOrCreateCarrierCounters(carrier, uaType).invite200OKTotal.Add(1)
}

func (m *metrics) SessionCompleted(carrier string, uaType string) {
	m.getOrCreateCarrierCounters(carrier, uaType).sessionCompletedTotal.Add(1)
	m.sdc.WithLabelValues(carrier, uaType).Inc()
}

func (m *metrics) UpdateSPD(carrier string, uaType string, duration time.Duration) {
	if duration < 0 {
		return
	}
	m.spd.WithLabelValues(carrier, uaType).Observe(duration.Seconds())
}

func (m *metrics) UpdateRRD(carrier string, uaType string, delayMs float64) {
	m.rrd.WithLabelValues(carrier, uaType).Observe(delayMs)
}

func (m *metrics) UpdateTTR(carrier string, uaType string, delayMs float64) {
	m.ttr.WithLabelValues(carrier, uaType).Observe(delayMs)
}

func (m *metrics) UpdateORD(carrier string, uaType string, delayMs float64) {
	m.ord.WithLabelValues(carrier, uaType).Observe(delayMs)
}

func (m *metrics) UpdateLRD(carrier string, uaType string, delayMs float64) {
	m.lrd.WithLabelValues(carrier, uaType).Observe(delayMs)
}

func (m *metrics) UpdateSession(carrier string, uaType string, size int) {
	m.sessions.WithLabelValues(carrier, uaType).Set(float64(size))
}

func (m *metrics) UpdateSessionsByCarrierAndUA(counts map[string]map[string]int) {
	m.sessions.Reset()
	for carrier, uaCounts := range counts {
		for uaType, count := range uaCounts {
			m.sessions.WithLabelValues(carrier, uaType).Set(float64(count))
		}
	}
}

func (m *metrics) SystemError() {
	m.systemErrorTotal.Inc()
}

func (m *metrics) UpdateVQReport(carrier string, uaType string, report *vq.SessionReport) {
	if report.Present["NLR"] {
		m.vqNLR.WithLabelValues(carrier, uaType).Observe(report.NLR)
	}
	if report.Present["JDR"] {
		m.vqJDR.WithLabelValues(carrier, uaType).Observe(report.JDR)
	}
	if report.Present["BLD"] {
		m.vqBLD.WithLabelValues(carrier, uaType).Observe(report.BLD)
	}
	if report.Present["GLD"] {
		m.vqGLD.WithLabelValues(carrier, uaType).Observe(report.GLD)
	}
	if report.Present["RTD"] {
		m.vqRTD.WithLabelValues(carrier, uaType).Observe(report.RTD)
	}
	if report.Present["ESD"] {
		m.vqESD.WithLabelValues(carrier, uaType).Observe(report.ESD)
	}
	if report.Present["IAJ"] {
		m.vqIAJ.WithLabelValues(carrier, uaType).Observe(report.IAJ)
	}
	if report.Present["MAJ"] {
		m.vqMAJ.WithLabelValues(carrier, uaType).Observe(report.MAJ)
	}
	if report.Present["MOSLQ"] {
		m.vqMOSLQ.WithLabelValues(carrier, uaType).Observe(report.MOSLQ)
	}
	if report.Present["MOSCQ"] {
		m.vqMOSCQ.WithLabelValues(carrier, uaType).Observe(report.MOSCQ)
	}
	if report.Present["RLQ"] {
		m.vqRLQ.WithLabelValues(carrier, uaType).Observe(report.RLQ)
	}
	if report.Present["RCQ"] {
		m.vqRCQ.WithLabelValues(carrier, uaType).Observe(report.RCQ)
	}
	if report.Present["RERL"] {
		m.vqRERL.WithLabelValues(carrier, uaType).Observe(report.RERL)
	}
	m.vqReports.WithLabelValues(carrier, uaType).Inc()
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

func (m *metrics) getOrCreateCarrierCounters(carrier string, uaType string) *carrierAtomicCounters {
	key := carrier + "\x00" + uaType
	if v, ok := m.carrierCounters.Load(key); ok {
		c, _ := v.(*carrierAtomicCounters)
		return c
	}
	c := &carrierAtomicCounters{}
	actual, _ := m.carrierCounters.LoadOrStore(key, c)
	result, _ := actual.(*carrierAtomicCounters)
	return result
}
