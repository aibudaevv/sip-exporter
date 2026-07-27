package vq

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type mockMetricser struct {
	vqReportCalled    bool
	systemErrorCalled bool
	lastCarrier       string
	lastUAType        string
	lastReport        *SessionReport
}

func (m *mockMetricser) UpdateVQReport(carrier, uaType, _, _ string, report *SessionReport) {
	m.vqReportCalled = true
	m.lastCarrier = carrier
	m.lastUAType = uaType
	m.lastReport = report
}

func (m *mockMetricser) SystemError() {
	m.systemErrorCalled = true
}

func (m *mockMetricser) ParseError(string) {}

func TestHandlerFullReport(t *testing.T) {
	mock := &mockMetricser{}
	h := NewHandler(mock)

	body := []byte("VQSessionReport: CallTerm\r\nMOSLQ=4.5 NLR=0.50\r\n")
	h.HandleVQReport(body, "carrier-a", "yealink", "US", "")

	require.True(t, mock.vqReportCalled)
	require.False(t, mock.systemErrorCalled)
	require.Equal(t, "carrier-a", mock.lastCarrier)
	require.Equal(t, "yealink", mock.lastUAType)
	require.NotNil(t, mock.lastReport)
	require.InDelta(t, 4.5, mock.lastReport.MOSLQ, 0.01)
	require.InDelta(t, 0.50, mock.lastReport.NLR, 0.01)
	require.True(t, mock.lastReport.Present["MOSLQ"])
	require.True(t, mock.lastReport.Present["NLR"])
}

func TestHandlerPartialReport(t *testing.T) {
	mock := &mockMetricser{}
	h := NewHandler(mock)

	body := []byte("VQSessionReport: CallTerm\r\nMOSLQ=3.2\r\n")
	h.HandleVQReport(body, "carrier-b", "polycom", "US", "")

	require.True(t, mock.vqReportCalled)
	require.False(t, mock.systemErrorCalled)
	require.NotNil(t, mock.lastReport)
	require.InDelta(t, 3.2, mock.lastReport.MOSLQ, 0.01)
	require.True(t, mock.lastReport.Present["MOSLQ"])
	require.False(t, mock.lastReport.Present["NLR"])
}

func TestHandlerInvalidBody(t *testing.T) {
	mock := &mockMetricser{}
	h := NewHandler(mock)

	h.HandleVQReport([]byte("invalid"), "carrier-a", "yealink", "US", "")

	require.True(t, mock.systemErrorCalled)
	require.False(t, mock.vqReportCalled)
}

func TestHandlerEmptyBody(t *testing.T) {
	mock := &mockMetricser{}
	h := NewHandler(mock)

	h.HandleVQReport([]byte{}, "carrier-a", "yealink", "US", "")

	require.True(t, mock.systemErrorCalled)
	require.False(t, mock.vqReportCalled)
}

func TestHandlerNilBody(t *testing.T) {
	mock := &mockMetricser{}
	h := NewHandler(mock)

	h.HandleVQReport(nil, "carrier-a", "yealink", "US", "")

	require.True(t, mock.systemErrorCalled)
	require.False(t, mock.vqReportCalled)
}

func TestHandlerCarrierLabel(t *testing.T) {
	mock := &mockMetricser{}
	h := NewHandler(mock)

	body := []byte("VQSessionReport: CallTerm\r\nMOSLQ=4.0\r\n")
	h.HandleVQReport(body, "mobile-operator", "yealink", "US", "")

	require.True(t, mock.vqReportCalled)
	require.Equal(t, "mobile-operator", mock.lastCarrier)
}

func TestHandlerUATypeLabel(t *testing.T) {
	mock := &mockMetricser{}
	h := NewHandler(mock)

	body := []byte("VQSessionReport: CallTerm\r\nMOSLQ=4.0\r\n")
	h.HandleVQReport(body, "carrier-a", "cisco", "US", "")

	require.True(t, mock.vqReportCalled)
	require.Equal(t, "cisco", mock.lastUAType)
}
