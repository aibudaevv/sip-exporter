package vq

import "go.uber.org/zap"

type Metricser interface {
	UpdateVQReport(carrier, uaType, sourceCountry string, report *SessionReport)
	SystemError()
	ParseError(errorType string)
}

type Handler struct {
	metricser Metricser
}

func NewHandler(metricser Metricser) *Handler {
	return &Handler{metricser: metricser}
}

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
