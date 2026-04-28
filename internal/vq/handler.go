package vq

import "go.uber.org/zap"

type Metricser interface {
	UpdateVQReport(carrier, uaType string, report *SessionReport)
	SystemError()
}

type Handler struct {
	metricser Metricser
}

func NewHandler(metricser Metricser) *Handler {
	return &Handler{metricser: metricser}
}

func (h *Handler) HandleVQReport(body []byte, carrier, uaType string) {
	report, err := ParseReport(body)
	if err != nil {
		zap.L().Warn("failed to parse vq-rtcpxr report", zap.Error(err))
		h.metricser.SystemError()
		return
	}
	h.metricser.UpdateVQReport(carrier, uaType, report)
}
