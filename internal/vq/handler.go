// Package vq parses VQ-RTCPXR (RFC 6035) session reports and routes the
// extracted metrics to a [Metricser].
package vq

import "go.uber.org/zap"

// Metricser is the subset of the service metrics interface needed by this
// package to record VQ report data and parsing errors.
type Metricser interface {
	UpdateVQReport(carrier, uaType, sourceCountry string, report *SessionReport)
	SystemError()
	ParseError(errorType string)
}

// Handler parses VQ-RTCPXR bodies and delegates metric updates to a [Metricser].
type Handler struct {
	metricser Metricser
}

// NewHandler creates a [Handler] that reports to the given [Metricser].
func NewHandler(metricser Metricser) *Handler {
	return &Handler{metricser: metricser}
}

// HandleVQReport parses a VQ-RTCPXR body and updates metrics. Logs a warning
// and increments error counters on parse failure.
func (h *Handler) HandleVQReport(body []byte, carrier, uaType, sourceCountry string) {
	report, err := ParseReport(body)
	if err != nil {
		zap.L().Warn("failed to parse vq-rtcpxr report", zap.Error(err))
		h.metricser.SystemError()
		h.metricser.ParseError("vq")
		return
	}
	h.metricser.UpdateVQReport(carrier, uaType, sourceCountry, report)
}
