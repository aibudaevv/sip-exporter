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
	if err := cv.WithLabelValues(carrier, uaType, codec, "", "").Write(&d); err != nil {
		return 0
	}
	return d.GetCounter().GetValue()
}

func (m *metrics) rtpHist(hv *prometheus.HistogramVec, carrier, uaType, codec string) (float64, uint64) {
	if hv == nil {
		return 0, 0
	}
	hist, ok := hv.WithLabelValues(carrier, uaType, codec, "", "").(prometheus.Histogram)
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
	if err := gv.WithLabelValues(carrier, uaType, codec, "", "").Write(&d); err != nil {
		return 0
	}
	return d.GetGauge().GetValue()
}

func TestRTPPacketsAndLoss(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTPPackets("carrier-a", "yealink", "PCMU", "", "")
	m.UpdateRTPPackets("carrier-a", "yealink", "PCMU", "", "")
	m.UpdateRTPLoss("carrier-a", "yealink", "PCMU", "", "", 3)

	require.InDelta(t, 2.0, m.rtpCounter(m.rtpPackets, "carrier-a", "yealink", "PCMU"), 0.01)
	require.InDelta(t, 3.0, m.rtpCounter(m.rtpLost, "carrier-a", "yealink", "PCMU"), 0.01)

	// zero loss is a no-op (no Add(0))
	m.UpdateRTPLoss("carrier-a", "yealink", "PCMU", "", "", 0)
	require.InDelta(t, 3.0, m.rtpCounter(m.rtpLost, "carrier-a", "yealink", "PCMU"), 0.01)
}

func TestRTPDuplicates(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTPDuplicates("carrier-a", "yealink", "PCMU", "", "")
	m.UpdateRTPDuplicates("carrier-a", "yealink", "PCMU", "", "")

	require.InDelta(t, 2.0, m.rtpCounter(m.rtpDuplicate, "carrier-a", "yealink", "PCMU"), 0.01)

	// distinct label set → separate counter
	m.UpdateRTPDuplicates("carrier-b", "cisco", "G.729", "", "")
	require.InDelta(t, 1.0, m.rtpCounter(m.rtpDuplicate, "carrier-b", "cisco", "G.729"), 0.01)
}

func TestRTPOutOfOrder(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTPOutOfOrder("carrier-a", "yealink", "PCMU", "", "")
	m.UpdateRTPOutOfOrder("carrier-a", "yealink", "PCMU", "", "")
	m.UpdateRTPOutOfOrder("carrier-a", "yealink", "PCMU", "", "")

	require.InDelta(t, 3.0, m.rtpCounter(m.rtpOutOfOrder, "carrier-a", "yealink", "PCMU"), 0.01)

	// distinct label set → separate counter
	m.UpdateRTPOutOfOrder("carrier-b", "cisco", "G.729", "", "")
	require.InDelta(t, 1.0, m.rtpCounter(m.rtpOutOfOrder, "carrier-b", "cisco", "G.729"), 0.01)
}

func TestRTPJitterAndMOS(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTPJitter("carrier-a", "yealink", "PCMU", "", "", 5.5)
	m.UpdateRTPJitter("carrier-a", "yealink", "PCMU", "", "", 10.5)

	sum, count := m.rtpHist(m.rtpJitter, "carrier-a", "yealink", "PCMU")
	require.InDelta(t, 16.0, sum, 0.01)
	require.Equal(t, uint64(2), count)

	m.UpdateRTPMOS("carrier-a", "yealink", "PCMU", "", "", 4.1)
	msum, mcount := m.rtpHist(m.rtpMOS, "carrier-a", "yealink", "PCMU")
	require.InDelta(t, 4.1, msum, 0.01)
	require.Equal(t, uint64(1), mcount)
}

func TestRTPRFactor(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTPRFactor("carrier-a", "polycom", "PCMU", "", "", 93.2)
	m.UpdateRTPRFactor("carrier-a", "polycom", "PCMU", "", "", 70.0)

	sum, count := m.rtpHist(m.rtpRFactor, "carrier-a", "polycom", "PCMU")
	require.InDelta(t, 163.2, sum, 0.01)
	require.Equal(t, uint64(2), count)
}

func TestRTPLossDistribution(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTPLossDistribution("carrier-b", "cisco", "G.729", "", "", 75.0, 25.0)
	m.UpdateRTPLossDistribution("carrier-b", "cisco", "G.729", "", "", 50.0, 50.0)

	bSum, bCount := m.rtpHist(m.rtpBurstLoss, "carrier-b", "cisco", "G.729")
	require.InDelta(t, 125.0, bSum, 0.01)
	require.Equal(t, uint64(2), bCount)

	gSum, gCount := m.rtpHist(m.rtpGapLoss, "carrier-b", "cisco", "G.729")
	require.InDelta(t, 75.0, gSum, 0.01)
	require.Equal(t, uint64(2), gCount)
}

func TestRTPMOSVariants(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTPMOSVariants("carrier-c", "grandstream", "PCMA", "", "", 3.5, 4.0, 4.2)
	m.UpdateRTPMOSVariants("carrier-c", "grandstream", "PCMA", "", "", 3.0, 3.8, 4.1)

	f1Sum, f1Count := m.rtpHist(m.rtpMOSF1, "carrier-c", "grandstream", "PCMA")
	require.InDelta(t, 6.5, f1Sum, 0.01)
	require.Equal(t, uint64(2), f1Count)

	f2Sum, f2Count := m.rtpHist(m.rtpMOSF2, "carrier-c", "grandstream", "PCMA")
	require.InDelta(t, 7.8, f2Sum, 0.01)
	require.Equal(t, uint64(2), f2Count)

	adaptSum, adaptCount := m.rtpHist(m.rtpMOSAdaptive, "carrier-c", "grandstream", "PCMA")
	require.InDelta(t, 8.3, adaptSum, 0.01)
	require.Equal(t, uint64(2), adaptCount)
}

func TestRTPOneWayAndMissing(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.OneWayCall("carrier-a", "yealink", "US", "")
	m.OneWayCall("carrier-a", "yealink", "US", "")
	m.MissingRTP("carrier-b", "cisco", "", "")

	var d dto.Metric
	require.NoError(t, m.rtpOneWayCalls.WithLabelValues("carrier-a", "yealink", "US", "").Write(&d))
	require.InDelta(t, 2.0, d.GetCounter().GetValue(), 0.01)

	require.NoError(t, m.sessionsMissingRTP.WithLabelValues("carrier-b", "cisco", "", "").Write(&d))
	require.InDelta(t, 1.0, d.GetCounter().GetValue(), 0.01)
}

func TestRTPActiveStreams(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTPActiveStreams([]LabeledCount{
		{Labels: map[string]string{
			"carrier": "carrier-a", "ua_type": "yealink",
			"codec": "PCMU", "source_country": "",
		}, Count: 2},
		{Labels: map[string]string{
			"carrier": "carrier-a", "ua_type": "yealink",
			"codec": "PCMA", "source_country": "",
		}, Count: 1},
		{Labels: map[string]string{
			"carrier": "carrier-b", "ua_type": "cisco",
			"codec": "G.729", "source_country": "",
		}, Count: 1},
	})
	require.InDelta(t, 2.0, m.rtpGauge(m.rtpActiveStreams, "carrier-a", "yealink", "PCMU"), 0.01)
	require.InDelta(t, 1.0, m.rtpGauge(m.rtpActiveStreams, "carrier-a", "yealink", "PCMA"), 0.01)
	require.InDelta(t, 1.0, m.rtpGauge(m.rtpActiveStreams, "carrier-b", "cisco", "G.729"), 0.01)

	// a subsequent snapshot resets stale label combinations.
	m.UpdateRTPActiveStreams([]LabeledCount{
		{Labels: map[string]string{
			"carrier": "carrier-a", "ua_type": "yealink",
			"codec": "PCMU", "source_country": "",
		}, Count: 1},
	})
	require.InDelta(t, 1.0, m.rtpGauge(m.rtpActiveStreams, "carrier-a", "yealink", "PCMU"), 0.01)
	require.InDelta(t, 0.0, m.rtpGauge(m.rtpActiveStreams, "carrier-a", "yealink", "PCMA"),
		0.01, "stale combo must reset")
}

func TestRTPEndpointMismatchDiagnostics(t *testing.T) {
	metricser, reg := newTestMetricserWithRegistry()
	m := metricser.(*metrics)
	m.RTPAliasLearned("carrier-a", "ingress", "source_port")
	m.RTPAliasLearned("carrier-a", "ingress", "source_port")
	m.RTPAliasReleased("carrier-a", "ingress")

	var metric dto.Metric
	require.NoError(t, m.rtpEndpointMismatch.WithLabelValues(
		"carrier-a", "ingress", "source_port",
	).Write(&metric))
	require.InDelta(t, 2.0, metric.GetCounter().GetValue(), 0)

	require.NoError(t, m.rtpAliasActive.WithLabelValues("carrier-a", "ingress").Write(&metric))
	require.InDelta(t, 1.0, metric.GetGauge().GetValue(), 0)

	wantLabels := map[string]map[string]bool{
		"sip_exporter_rtp_endpoint_mismatch_total": {"carrier": true, "direction": true, "type": true},
		"sip_exporter_rtp_alias_active":            {"carrier": true, "direction": true},
	}
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		labels, ok := wantLabels[family.GetName()]
		if !ok {
			continue
		}
		require.Len(t, family.GetMetric(), 1)
		require.Len(t, family.GetMetric()[0].GetLabel(), len(labels))
		for _, label := range family.GetMetric()[0].GetLabel() {
			require.True(t, labels[label.GetName()], "unexpected label %q", label.GetName())
		}
		delete(wantLabels, family.GetName())
	}
	require.Empty(t, wantLabels, "diagnostic metric families missing")
}
