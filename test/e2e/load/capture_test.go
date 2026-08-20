//go:build e2e

package load

import "fmt"

type CaptureResult struct {
	Expected  float64
	Captured  float64
	Missing   float64
	Excess    float64
	LossPct   float64
	ExcessPct float64
}

type ProtocolCounters struct {
	SIPPackets     float64
	RTPPackets     float64
	RTCPReports    float64
	VQReports      float64
	SocketReceived float64
	SocketDropped  float64
}

func (c ProtocolCounters) delta(before ProtocolCounters) ProtocolCounters {
	return ProtocolCounters{
		SIPPackets:     c.SIPPackets - before.SIPPackets,
		RTPPackets:     c.RTPPackets - before.RTPPackets,
		RTCPReports:    c.RTCPReports - before.RTCPReports,
		VQReports:      c.VQReports - before.VQReports,
		SocketReceived: c.SocketReceived - before.SocketReceived,
		SocketDropped:  c.SocketDropped - before.SocketDropped,
	}
}

func exactCaptureComplete(expected, captured, channelLength float64) bool {
	return captured == expected && channelLength == 0
}

func newCaptureResult(expected, captured float64) CaptureResult {
	result := CaptureResult{Expected: expected, Captured: captured}
	if captured < expected {
		result.Missing = expected - captured
	} else if captured > expected {
		result.Excess = captured - expected
	}
	if expected > 0 {
		result.LossPct = result.Missing / expected * 100
		result.ExcessPct = result.Excess / expected * 100
	}
	return result
}

func (r CaptureResult) ValidateExact() error {
	if r.Missing != 0 || r.Excess != 0 {
		return fmt.Errorf("capture mismatch: expected %.0f, captured %.0f, missing %.0f, excess %.0f",
			r.Expected, r.Captured, r.Missing, r.Excess)
	}
	return nil
}
