package service

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func (m *metrics) rtpCounter(cv *prometheus.CounterVec, carrier, uaType, codec string) float64 {
	if cv == nil {
		return 0
	}
	var d dto.Metric
	if err := cv.WithLabelValues(carrier, uaType, codec).Write(&d); err != nil {
		return 0
	}
	return d.GetCounter().GetValue()
}

func (m *metrics) rtpHist(hv *prometheus.HistogramVec, carrier, uaType, codec string) (float64, uint64) {
	if hv == nil {
		return 0, 0
	}
	hist, ok := hv.WithLabelValues(carrier, uaType, codec).(prometheus.Histogram)
	if !ok {
		return 0, 0
	}
	var d dto.Metric
	if err := hist.Write(&d); err != nil {
		return 0, 0
	}
	h := d.GetHistogram()
	return h.GetSampleSum(), h.GetSampleCount()
}

func (m *metrics) rtpGauge(gv *prometheus.GaugeVec, carrier, uaType, codec string) float64 {
	if gv == nil {
		return 0
	}
	var d dto.Metric
	if err := gv.WithLabelValues(carrier, uaType, codec).Write(&d); err != nil {
		return 0
	}
	return d.GetGauge().GetValue()
}

func TestRTP_PacketsAndLoss(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTPPackets("carrier-a", "yealink", "PCMU")
	m.UpdateRTPPackets("carrier-a", "yealink", "PCMU")
	m.UpdateRTPLoss("carrier-a", "yealink", "PCMU", 3)

	require.InDelta(t, 2.0, m.rtpCounter(m.rtpPackets, "carrier-a", "yealink", "PCMU"), 0.01)
	require.InDelta(t, 3.0, m.rtpCounter(m.rtpLost, "carrier-a", "yealink", "PCMU"), 0.01)

	// zero loss is a no-op (no Add(0))
	m.UpdateRTPLoss("carrier-a", "yealink", "PCMU", 0)
	require.InDelta(t, 3.0, m.rtpCounter(m.rtpLost, "carrier-a", "yealink", "PCMU"), 0.01)
}

func TestRTP_JitterAndMOS(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTPJitter("carrier-a", "yealink", "PCMU", 5.5)
	m.UpdateRTPJitter("carrier-a", "yealink", "PCMU", 10.5)

	sum, count := m.rtpHist(m.rtpJitter, "carrier-a", "yealink", "PCMU")
	require.InDelta(t, 16.0, sum, 0.01)
	require.Equal(t, uint64(2), count)

	m.UpdateRTPMOS("carrier-a", "yealink", "PCMU", 4.1)
	msum, mcount := m.rtpHist(m.rtpMOS, "carrier-a", "yealink", "PCMU")
	require.InDelta(t, 4.1, msum, 0.01)
	require.Equal(t, uint64(1), mcount)
}

func TestRTP_ActiveStreams(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTPActiveStreams([]LabeledCount{
		{Labels: map[string]string{"carrier": "carrier-a", "ua_type": "yealink", "codec": "PCMU"}, Count: 2},
		{Labels: map[string]string{"carrier": "carrier-a", "ua_type": "yealink", "codec": "PCMA"}, Count: 1},
		{Labels: map[string]string{"carrier": "carrier-b", "ua_type": "cisco", "codec": "G.729"}, Count: 1},
	})
	require.InDelta(t, 2.0, m.rtpGauge(m.rtpActiveStreams, "carrier-a", "yealink", "PCMU"), 0.01)
	require.InDelta(t, 1.0, m.rtpGauge(m.rtpActiveStreams, "carrier-a", "yealink", "PCMA"), 0.01)
	require.InDelta(t, 1.0, m.rtpGauge(m.rtpActiveStreams, "carrier-b", "cisco", "G.729"), 0.01)

	// a subsequent snapshot resets stale label combinations.
	m.UpdateRTPActiveStreams([]LabeledCount{
		{Labels: map[string]string{"carrier": "carrier-a", "ua_type": "yealink", "codec": "PCMU"}, Count: 1},
	})
	require.InDelta(t, 1.0, m.rtpGauge(m.rtpActiveStreams, "carrier-a", "yealink", "PCMU"), 0.01)
	require.InDelta(t, 0.0, m.rtpGauge(m.rtpActiveStreams, "carrier-a", "yealink", "PCMA"),
		0.01, "stale combo must reset")
}
