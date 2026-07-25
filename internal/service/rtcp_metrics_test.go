package service

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestRTCP_Jitter(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTCPJitter("carrier-a", "yealink", "PCMU", "", "", 5.5)
	m.UpdateRTCPJitter("carrier-a", "yealink", "PCMU", "", "", 12.5)

	sum, count := m.rtpHist(m.rtcpJitter, "carrier-a", "yealink", "PCMU")
	require.InDelta(t, 18.0, sum, 0.01)
	require.Equal(t, uint64(2), count)
}

func TestRTCP_LossFraction(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTCPLossFraction("carrier-a", "yealink", "PCMU", "", "", 2.5)
	m.UpdateRTCPLossFraction("carrier-a", "yealink", "PCMU", "", "", 7.5)

	sum, count := m.rtpHist(m.rtcpLossFraction, "carrier-a", "yealink", "PCMU")
	require.InDelta(t, 10.0, sum, 0.01)
	require.Equal(t, uint64(2), count)
}

func TestRTCP_CumulativeLoss(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	// The caller passes the loss delta since the previous RR; the counter accumulates.
	m.UpdateRTCPCumulativeLoss("carrier-a", "yealink", "PCMU", "", "", 5)
	m.UpdateRTCPCumulativeLoss("carrier-a", "yealink", "PCMU", "", "", 3)
	// Zero delta must be a no-op (no empty series created).
	m.UpdateRTCPCumulativeLoss("carrier-a", "yealink", "PCMU", "", "", 0)

	require.InDelta(t, 8.0, m.rtpCounter(m.rtcpCumulativeLoss, "carrier-a", "yealink", "PCMU"), 0.01)
}

func TestRTCP_RTT(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTCPRTT("carrier-a", "yealink", "PCMU", "", "", 45.0)
	m.UpdateRTCPRTT("carrier-a", "yealink", "PCMU", "", "", 75.0)

	sum, count := m.rtpHist(m.rtcpRTT, "carrier-a", "yealink", "PCMU")
	require.InDelta(t, 120.0, sum, 0.01)
	require.Equal(t, uint64(2), count)
}

func TestRTCP_Reports(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateRTCPReport("carrier-a", "yealink", "RU", "inbound", "sr")
	m.UpdateRTCPReport("carrier-a", "yealink", "RU", "inbound", "sr")
	m.UpdateRTCPReport("carrier-a", "yealink", "RU", "inbound", "rr")

	// rtcp_reports_total labels: {carrier,ua_type,source_country,direction,type} (no codec).
	// Distinct type values (sr vs rr) must produce distinct series.
	sr := m.rtcpReports.WithLabelValues("carrier-a", "yealink", "RU", "inbound", "sr")
	rr := m.rtcpReports.WithLabelValues("carrier-a", "yealink", "RU", "inbound", "rr")
	var ds, dr dto.Metric
	require.NoError(t, sr.Write(&ds))
	require.NoError(t, rr.Write(&dr))
	require.InDelta(t, 2.0, ds.GetCounter().GetValue(), 0.01)
	require.InDelta(t, 1.0, dr.GetCounter().GetValue(), 0.01)
}
