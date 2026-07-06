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

	"github.com/aibudaevv/sip-exporter/internal/version"
	"github.com/aibudaevv/sip-exporter/internal/vq"
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

	counterKey struct {
		Carrier string
		UAType  string
		Country string
	}

	LabeledCount struct {
		Labels map[string]string
		Count  int
	}

	metrics struct {
		systemErrorTotal prometheus.Counter
		sipPacketsTotal  prometheus.Counter

		requestInviteTotal      *prometheus.CounterVec
		requestReinviteTotal    *prometheus.CounterVec
		requestInvite200OKTotal *prometheus.CounterVec
		requestACKTotal         *prometheus.CounterVec
		requestByeTotal         *prometheus.CounterVec
		requestCancelTotal      *prometheus.CounterVec
		requestOptionsTotal     *prometheus.CounterVec
		requestRegisterTotal    *prometheus.CounterVec
		requestUpdateTotal      *prometheus.CounterVec
		requestInfoTotal        *prometheus.CounterVec
		requestReferTotal       *prometheus.CounterVec
		requestSubscribeTotal   *prometheus.CounterVec
		requestNotifyTotal      *prometheus.CounterVec
		requestPrackTotal       *prometheus.CounterVec
		requestPublishTotal     *prometheus.CounterVec
		requestMessageTotal     *prometheus.CounterVec

		statusCounters map[string]*prometheus.CounterVec

		sdc *prometheus.CounterVec
		iss *prometheus.CounterVec

		sessions        *prometheus.GaugeVec
		prevSessionKeys map[string][]string

		rrd *prometheus.HistogramVec
		spd *prometheus.HistogramVec
		ttr *prometheus.HistogramVec
		pdd *prometheus.HistogramVec
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
		vqTable   []vqTableEntry

		rtpPackets       *prometheus.CounterVec
		rtpLost          *prometheus.CounterVec
		rtpJitter        *prometheus.HistogramVec
		rtpMOS           *prometheus.HistogramVec
		rtpActiveStreams *prometheus.GaugeVec
		prevRTPKeys      map[string][]string

		carrierCounters sync.Map

		socketPacketsReceived prometheus.Counter
		socketPacketsDropped  prometheus.Counter
		parseErrorsTotal      *prometheus.CounterVec
		channelLength         prometheus.Gauge
		channelCapacity       prometheus.Gauge
		activeTrackers        *prometheus.GaugeVec
		activeDialogs         prometheus.Gauge
	}

	Metricser interface {
		Request(carrier, uaType, sourceCountry, destinationCountry, callerHost, calledHost string, in []byte)
		Reinvite(carrier, uaType, sourceCountry string)
		Response(carrier, uaType, sourceCountry string, in []byte, isInviteResponse bool)
		ResponseWithMetrics(carrier, uaType, sourceCountry string, status []byte, isInviteResponse, is200OK bool)
		Invite200OK(carrier, uaType, sourceCountry, destinationCountry, callerHost, calledHost string)
		SessionCompleted(carrier, uaType, sourceCountry string)
		UpdateRRD(carrier, uaType, sourceCountry string, delayMs float64)
		UpdateSPD(carrier, uaType, sourceCountry string, duration time.Duration)
		UpdateTTR(carrier, uaType, sourceCountry string, delayMs float64)
		UpdatePDD(carrier, uaType, sourceCountry string, delayMs float64)
		UpdateORD(carrier, uaType, sourceCountry string, delayMs float64)
		UpdateLRD(carrier, uaType, sourceCountry string, delayMs float64)
		UpdateSession(carrier, uaType, sourceCountry string, size int)
		UpdateSessions(counts []LabeledCount)
		UpdateVQReport(carrier, uaType, sourceCountry string, report *vq.SessionReport)
		UpdateRTPPackets(carrier, uaType, codec, sourceCountry string)
		UpdateRTPLoss(carrier, uaType, codec, sourceCountry string, lost uint64)
		UpdateRTPJitter(carrier, uaType, codec, sourceCountry string, jitterMs float64)
		UpdateRTPMOS(carrier, uaType, codec, sourceCountry string, mos float64)
		UpdateRTPActiveStreams(counts []LabeledCount)
		SystemError()
		ParseError(errorType string)
		SocketStats(received, dropped uint32)
		UpdateChannelLength(length int)
		UpdateChannelCapacity(capacity int)
		UpdateTrackerSize(trackerType string, size int)
		UpdateActiveDialogs(size int)
	}

	ratioCollector struct {
		desc   *prometheus.Desc
		m      *metrics
		calcFn func(c *carrierAtomicCounters) float64
	}

	vqTableEntry struct {
		key  string
		hist *prometheus.HistogramVec
		val  func(*vq.SessionReport) float64
	}
)

func (counterKey) labelNames() []string {
	return []string{"carrier", "ua_type", "source_country"}
}

func (k counterKey) labelValues() []string {
	return []string{k.Carrier, k.UAType, k.Country}
}

func (rc *ratioCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- rc.desc
}

func (rc *ratioCollector) Collect(ch chan<- prometheus.Metric) {
	rc.m.carrierCounters.Range(func(key, value any) bool {
		k, ok := key.(counterKey)
		if !ok {
			return true
		}
		counters, ok := value.(*carrierAtomicCounters)
		if !ok {
			return true
		}
		val := rc.calcFn(counters)
		ch <- prometheus.MustNewConstMetric(rc.desc, prometheus.GaugeValue, val, k.labelValues()...)
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
	m.registerBuildInfo(reg)
	m.initRequestCounters(reg)
	m.initStatusCounters(reg)
	m.initSessionMetrics(reg)
	m.initHistograms(reg)
	m.initVQHistograms(reg)
	registerRatioCollectors(m, reg)
	m.initRTPMetrics(reg)
	m.initSelfMetrics(reg)
	return m
}

func (m *metrics) initRequestCounters(reg *prometheus.Registry) {
	cl := []string{"carrier", "ua_type", "source_country"}
	clInvite := []string{
		"carrier", "ua_type", "source_country",
		"destination_country", "caller_host", "called_host",
	}
	m.requestInviteTotal = newCounterVecWithRegistry(
		"sip_exporter_invite_total",
		"Total number of INVITE requests", clInvite, reg)
	m.requestReinviteTotal = newCounterVecWithRegistry(
		"sip_exporter_reinvite_total",
		"Total number of re-INVITE requests (INVITE within an existing dialog)", cl, reg)
	m.requestInvite200OKTotal = newCounterVecWithRegistry(
		"sip_exporter_invite_200_total",
		"Total number of 200 OK responses to INVITE", clInvite, reg)
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
	cl := []string{"carrier", "ua_type", "source_country"}
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
	cl := []string{"carrier", "ua_type", "source_country"}
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
	msBuckets := []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000}
	cl := []string{"carrier", "ua_type", "source_country"}
	m.rrd = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_rrd",
		Help:    "Registration Request Delay in milliseconds (RFC 6076)",
		Buckets: msBuckets,
	}, cl, reg)
	m.spd = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_spd",
		Help:    "Session Process Duration in seconds (RFC 6076)",
		Buckets: []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600},
	}, cl, reg)
	m.ttr = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_ttr",
		Help:    "Time to First Response in milliseconds (time from INVITE to first provisional 1xx response)",
		Buckets: msBuckets,
	}, cl, reg)
	m.pdd = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_pdd",
		Help:    "Post Dial Delay in milliseconds (time from INVITE to 180 Ringing response)",
		Buckets: msBuckets,
	}, cl, reg)
	m.ord = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_ord",
		Help:    "OPTIONS Response Delay in milliseconds (RFC 3261 §11.1)",
		Buckets: msBuckets,
	}, cl, reg)
	m.lrd = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_lrd",
		Help:    "Location Registration Delay in milliseconds: delay between REGISTER and 3xx redirect response",
		Buckets: msBuckets,
	}, cl, reg)
}

func (m *metrics) initVQHistograms(reg *prometheus.Registry) {
	msBuckets := []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000}
	percentBuckets := []float64{0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100}
	jitterBuckets := []float64{0.1, 0.5, 1, 5, 10, 20, 50, 100, 200, 500}
	mosBuckets := []float64{1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0}
	rFactorBuckets := []float64{0, 10, 20, 30, 50, 60, 70, 80, 90, 100, 120}
	rerlBuckets := []float64{0, 5, 10, 15, 20, 30, 40, 50, 60, 80, 100}
	cl := []string{"carrier", "ua_type", "source_country"}
	m.vqNLR = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_nlr_percent",
		Help:    "Voice Quality Network Packet Loss Rate percentage (RFC 6035)",
		Buckets: percentBuckets,
	}, cl, reg)
	m.vqJDR = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_jdr_percent",
		Help:    "Voice Quality Jitter Buffer Discard Rate percentage (RFC 6035)",
		Buckets: percentBuckets,
	}, cl, reg)
	m.vqBLD = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_bld_percent",
		Help:    "Voice Quality Burst Loss Density percentage (RFC 6035)",
		Buckets: percentBuckets,
	}, cl, reg)
	m.vqGLD = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_gld_percent",
		Help:    "Voice Quality Gap Loss Density percentage (RFC 6035)",
		Buckets: percentBuckets,
	}, cl, reg)
	m.vqRTD = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_rtd_ms",
		Help:    "Voice Quality Round Trip Delay in milliseconds (RFC 6035)",
		Buckets: msBuckets,
	}, cl, reg)
	m.vqESD = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_esd_ms",
		Help:    "Voice Quality End System Delay in milliseconds (RFC 6035)",
		Buckets: msBuckets,
	}, cl, reg)
	m.vqIAJ = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_iaj_ms",
		Help:    "Voice Quality Interarrival Jitter in milliseconds (RFC 6035)",
		Buckets: jitterBuckets,
	}, cl, reg)
	m.vqMAJ = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_maj_ms",
		Help:    "Voice Quality Mean Absolute Jitter in milliseconds (RFC 6035)",
		Buckets: jitterBuckets,
	}, cl, reg)
	m.vqMOSLQ = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_mos_lq",
		Help:    "Voice Quality MOS Listening Quality score 0.0-4.9 (RFC 6035)",
		Buckets: mosBuckets,
	}, cl, reg)
	m.vqMOSCQ = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_mos_cq",
		Help:    "Voice Quality MOS Conversational Quality score 0.0-4.9 (RFC 6035)",
		Buckets: mosBuckets,
	}, cl, reg)
	m.vqRLQ = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_rlq",
		Help:    "Voice Quality R-factor Listening Quality 0-120 (RFC 6035)",
		Buckets: rFactorBuckets,
	}, cl, reg)
	m.vqRCQ = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_rcq",
		Help:    "Voice Quality R-factor Conversational Quality 0-120 (RFC 6035)",
		Buckets: rFactorBuckets,
	}, cl, reg)
	m.vqRERL = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_vq_rerl_db",
		Help:    "Voice Quality Residual Echo Return Loss in dB (RFC 6035)",
		Buckets: rerlBuckets,
	}, cl, reg)
	m.vqReports = newCounterVecWithRegistry(
		"sip_exporter_vq_reports_total",
		"Total number of Voice Quality session reports processed (RFC 6035)",
		cl, reg)
	m.vqTable = []vqTableEntry{
		{"NLR", m.vqNLR, func(r *vq.SessionReport) float64 { return r.NLR }},
		{"JDR", m.vqJDR, func(r *vq.SessionReport) float64 { return r.JDR }},
		{"BLD", m.vqBLD, func(r *vq.SessionReport) float64 { return r.BLD }},
		{"GLD", m.vqGLD, func(r *vq.SessionReport) float64 { return r.GLD }},
		{"RTD", m.vqRTD, func(r *vq.SessionReport) float64 { return r.RTD }},
		{"ESD", m.vqESD, func(r *vq.SessionReport) float64 { return r.ESD }},
		{"IAJ", m.vqIAJ, func(r *vq.SessionReport) float64 { return r.IAJ }},
		{"MAJ", m.vqMAJ, func(r *vq.SessionReport) float64 { return r.MAJ }},
		{"MOSLQ", m.vqMOSLQ, func(r *vq.SessionReport) float64 { return r.MOSLQ }},
		{"MOSCQ", m.vqMOSCQ, func(r *vq.SessionReport) float64 { return r.MOSCQ }},
		{"RLQ", m.vqRLQ, func(r *vq.SessionReport) float64 { return r.RLQ }},
		{"RCQ", m.vqRCQ, func(r *vq.SessionReport) float64 { return r.RCQ }},
		{"RERL", m.vqRERL, func(r *vq.SessionReport) float64 { return r.RERL }},
	}
}

func (m *metrics) initRTPMetrics(reg *prometheus.Registry) {
	jitterBuckets := []float64{0.1, 0.5, 1, 5, 10, 20, 50, 100, 200, 500}
	mosBuckets := []float64{1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0}
	rl := []string{"carrier", "ua_type", "codec", "source_country"}
	m.rtpPackets = newCounterVecWithRegistry(
		"sip_exporter_rtp_packets_total",
		"Total number of RTP packets observed (correlated with SIP dialogs)", rl, reg)
	m.rtpLost = newCounterVecWithRegistry(
		"sip_exporter_rtp_packets_lost_total",
		"Total number of RTP packets detected as lost via sequence gaps", rl, reg)
	m.rtpJitter = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_rtp_jitter_milliseconds",
		Help:    "RTP interarrival jitter in milliseconds (RFC 3550 A.8)",
		Buckets: jitterBuckets,
	}, rl, reg)
	m.rtpMOS = newHistogramVecWithRegistry(prometheus.HistogramOpts{
		Name:    "sip_exporter_rtp_mos_score",
		Help:    "RTP MOS-LQ score 1.0-4.9 estimated via ITU-T G.107 E-model",
		Buckets: mosBuckets,
	}, rl, reg)
	m.rtpActiveStreams = newGaugeVecWithRegistry(
		"sip_exporter_rtp_active_streams",
		"Number of active RTP streams correlated with SIP dialogs", rl, reg)
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
		desc:   prometheus.NewDesc(name, help, counterKey{}.labelNames(), nil),
		m:      m,
		calcFn: calcFn,
	}
	if reg != nil {
		reg.MustRegister(rc)
	} else {
		prometheus.MustRegister(rc)
	}
}

func (m *metrics) Request(
	carrier, uaType, sourceCountry, destinationCountry,
	callerHost, calledHost string, in []byte,
) {
	defer m.sipPacketsTotal.Inc()

	switch {
	case bytes.Equal(in, []byte("PUBLISH")):
		m.requestPublishTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	case bytes.Equal(in, []byte("PRACK")):
		m.requestPrackTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	case bytes.Equal(in, []byte("NOTIFY")):
		m.requestNotifyTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	case bytes.Equal(in, []byte("SUBSCRIBE")):
		m.requestSubscribeTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	case bytes.Equal(in, []byte("REFER")):
		m.requestReferTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	case bytes.Equal(in, []byte("INFO")):
		m.requestInfoTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	case bytes.Equal(in, []byte("UPDATE")):
		m.requestUpdateTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	case bytes.Equal(in, []byte("REGISTER")):
		m.requestRegisterTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	case bytes.Equal(in, []byte("OPTIONS")):
		m.requestOptionsTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	case bytes.Equal(in, []byte("CANCEL")):
		m.requestCancelTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	case bytes.Equal(in, []byte("BYE")):
		m.requestByeTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	case bytes.Equal(in, []byte("ACK")):
		m.requestACKTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	case bytes.Equal(in, []byte("INVITE")):
		m.requestInviteTotal.WithLabelValues(
			carrier, uaType, sourceCountry, destinationCountry, callerHost, calledHost,
		).Inc()
		m.getOrCreateCarrierCounters(carrier, uaType, sourceCountry).inviteTotal.Add(1)
	case bytes.Equal(in, []byte("MESSAGE")):
		m.requestMessageTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	default:
		zap.L().Warn("unknown request", zap.ByteString("in", in))
	}
}

func (m *metrics) Reinvite(carrier, uaType, sourceCountry string) {
	defer m.sipPacketsTotal.Inc()
	m.requestReinviteTotal.WithLabelValues(carrier, uaType, sourceCountry).Inc()
}

func (m *metrics) Response(carrier, uaType, sourceCountry string, in []byte, isInviteResponse bool) {
	defer m.sipPacketsTotal.Inc()

	m.incrementStatusCodeCounter(carrier, uaType, sourceCountry, in)

	if isInviteResponse && len(in) == 3 && in[0] == '3' {
		m.getOrCreateCarrierCounters(carrier, uaType, sourceCountry).invite3xxTotal.Add(1)
	}

	if isInviteResponse && isEffectiveResponse(in) {
		m.getOrCreateCarrierCounters(carrier, uaType, sourceCountry).inviteEffectiveTotal.Add(1)
	}

	if isInviteResponse && isIneffectiveResponse(in) {
		m.getOrCreateCarrierCounters(carrier, uaType, sourceCountry).inviteIneffectiveTotal.Add(1)
		m.iss.WithLabelValues(carrier, uaType, sourceCountry).Inc()
	}
}

func (m *metrics) incrementStatusCodeCounter(carrier, uaType, sourceCountry string, in []byte) {
	counter, ok := m.statusCounters[string(in)]
	if ok {
		counter.WithLabelValues(carrier, uaType, sourceCountry).Inc()
		return
	}
	zap.L().Warn("unknown response", zap.ByteString("in", in))
}

func (m *metrics) ResponseWithMetrics(
	carrier, uaType, sourceCountry string,
	status []byte, isInviteResponse, is200OK bool,
) {
	m.Response(carrier, uaType, sourceCountry, status, isInviteResponse)

	if isInviteResponse && is200OK {
		m.getOrCreateCarrierCounters(carrier, uaType, sourceCountry).invite200OKTotal.Add(1)
	}
}

func (m *metrics) Invite200OK(carrier, uaType, sourceCountry, destinationCountry, callerHost, calledHost string) {
	m.requestInvite200OKTotal.WithLabelValues(
		carrier, uaType, sourceCountry, destinationCountry, callerHost, calledHost,
	).Inc()
}

func (m *metrics) SessionCompleted(carrier, uaType, sourceCountry string) {
	m.getOrCreateCarrierCounters(carrier, uaType, sourceCountry).sessionCompletedTotal.Add(1)
	m.sdc.WithLabelValues(carrier, uaType, sourceCountry).Inc()
}

func (m *metrics) UpdateSPD(carrier, uaType, sourceCountry string, duration time.Duration) {
	if duration < 0 {
		return
	}
	m.spd.WithLabelValues(carrier, uaType, sourceCountry).Observe(duration.Seconds())
}

func (m *metrics) UpdateRRD(carrier, uaType, sourceCountry string, delayMs float64) {
	m.rrd.WithLabelValues(carrier, uaType, sourceCountry).Observe(delayMs)
}

func (m *metrics) UpdateTTR(carrier, uaType, sourceCountry string, delayMs float64) {
	m.ttr.WithLabelValues(carrier, uaType, sourceCountry).Observe(delayMs)
}

func (m *metrics) UpdatePDD(carrier, uaType, sourceCountry string, delayMs float64) {
	m.pdd.WithLabelValues(carrier, uaType, sourceCountry).Observe(delayMs)
}

func (m *metrics) UpdateORD(carrier, uaType, sourceCountry string, delayMs float64) {
	m.ord.WithLabelValues(carrier, uaType, sourceCountry).Observe(delayMs)
}

func (m *metrics) UpdateLRD(carrier, uaType, sourceCountry string, delayMs float64) {
	m.lrd.WithLabelValues(carrier, uaType, sourceCountry).Observe(delayMs)
}

func (m *metrics) UpdateSession(carrier, uaType, sourceCountry string, size int) {
	m.sessions.WithLabelValues(carrier, uaType, sourceCountry).Set(float64(size))
}

func setGaugeFromCounts(
	gv *prometheus.GaugeVec,
	prevKeys *map[string][]string,
	counts []LabeledCount,
	labelNames []string,
) {
	current := make(map[string][]string, len(counts))
	for _, lc := range counts {
		vals := make([]string, len(labelNames))
		for i, name := range labelNames {
			vals[i] = lc.Labels[name]
		}
		key := strings.Join(vals, "\x00")
		current[key] = vals
		gv.WithLabelValues(vals...).Set(float64(lc.Count))
	}
	for key, vals := range *prevKeys {
		if _, ok := current[key]; !ok {
			gv.WithLabelValues(vals...).Set(0)
		}
	}
	*prevKeys = current
}

func (m *metrics) UpdateSessions(counts []LabeledCount) {
	setGaugeFromCounts(m.sessions, &m.prevSessionKeys, counts,
		[]string{"carrier", "ua_type", "source_country"})
}

func (m *metrics) SystemError() {
	m.systemErrorTotal.Inc()
}

func (m *metrics) UpdateVQReport(carrier, uaType, sourceCountry string, report *vq.SessionReport) {
	for _, vm := range m.vqTable {
		if report.Present[vm.key] {
			vm.hist.WithLabelValues(carrier, uaType, sourceCountry).Observe(vm.val(report))
		}
	}
	m.vqReports.WithLabelValues(carrier, uaType, sourceCountry).Inc()
}

func (m *metrics) UpdateRTPPackets(carrier, uaType, codec, sourceCountry string) {
	m.rtpPackets.WithLabelValues(carrier, uaType, codec, sourceCountry).Inc()
}

func (m *metrics) UpdateRTPLoss(carrier, uaType, codec, sourceCountry string, lost uint64) {
	if lost == 0 {
		return
	}
	m.rtpLost.WithLabelValues(carrier, uaType, codec, sourceCountry).Add(float64(lost))
}

func (m *metrics) UpdateRTPJitter(carrier, uaType, codec, sourceCountry string, jitterMs float64) {
	m.rtpJitter.WithLabelValues(carrier, uaType, codec, sourceCountry).Observe(jitterMs)
}

func (m *metrics) UpdateRTPMOS(carrier, uaType, codec, sourceCountry string, mos float64) {
	m.rtpMOS.WithLabelValues(carrier, uaType, codec, sourceCountry).Observe(mos)
}

func (m *metrics) UpdateRTPActiveStreams(counts []LabeledCount) {
	setGaugeFromCounts(m.rtpActiveStreams, &m.prevRTPKeys, counts,
		[]string{"carrier", "ua_type", "codec", "source_country"})
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

func (m *metrics) registerBuildInfo(reg *prometheus.Registry) {
	buildInfo := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name:        "sip_exporter_build_info",
		Help:        "Build information",
		ConstLabels: prometheus.Labels{"version": version.Version},
	}, func() float64 { return 1 })
	if reg != nil {
		reg.MustRegister(buildInfo)
	} else {
		promauto.NewGaugeFunc(prometheus.GaugeOpts{
			Name:        "sip_exporter_build_info",
			Help:        "Build information",
			ConstLabels: prometheus.Labels{"version": version.Version},
		}, func() float64 { return 1 })
	}
}

func (m *metrics) getOrCreateCarrierCounters(carrier, uaType, sourceCountry string) *carrierAtomicCounters {
	key := counterKey{Carrier: carrier, UAType: uaType, Country: sourceCountry}
	if v, ok := m.carrierCounters.Load(key); ok {
		c, _ := v.(*carrierAtomicCounters)
		return c
	}
	c := &carrierAtomicCounters{}
	actual, _ := m.carrierCounters.LoadOrStore(key, c)
	result, _ := actual.(*carrierAtomicCounters)
	return result
}
