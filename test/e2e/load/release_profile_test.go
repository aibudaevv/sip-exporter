//go:build e2e

package load

import (
	"fmt"
	"math"
)

type (
	releaseGeneratorEvidence struct {
		Spec   WorkloadSpec
		Result GeneratorResult
	}

	releaseBusinessEvidence struct {
		Expected float64
		Actual   float64
	}

	releaseRowEvidence struct {
		Generators []releaseGeneratorEvidence
		Capture    CaptureResult
		Protocols  ProtocolCounters
		ErrorCount float64
		Resources  ResourceSummaryV2
		Limits     WorkloadLimits
		Business   map[string]releaseBusinessEvidence
		Scrapes    *ScrapeSummary
	}

	releaseRowSpec struct {
		RequireScrapes       bool
		ExpectedSystemErrors float64
	}
)

func validateReleaseRow(spec releaseRowSpec, evidence releaseRowEvidence) error {
	if len(evidence.Generators) == 0 {
		return fmt.Errorf("release row has no generator evidence")
	}
	for i, generator := range evidence.Generators {
		if err := generator.Result.Validate(generator.Spec); err != nil {
			return fmt.Errorf("generator %d: %w", i, err)
		}
	}
	if err := validateReleaseCapture(evidence.Capture); err != nil {
		return fmt.Errorf("capture: %w", err)
	}
	if err := validateReleaseProtocolCounters(evidence.Protocols); err != nil {
		return err
	}
	if evidence.Protocols.SIPPackets != evidence.Capture.Captured {
		return fmt.Errorf("SIP protocol counter does not match capture")
	}
	if math.IsNaN(evidence.ErrorCount) || math.IsInf(evidence.ErrorCount, 0) || evidence.ErrorCount < 0 {
		return fmt.Errorf("invalid system error count: %v", evidence.ErrorCount)
	}
	if evidence.ErrorCount != spec.ExpectedSystemErrors {
		return fmt.Errorf("system errors: got %.0f, want %.0f", evidence.ErrorCount, spec.ExpectedSystemErrors)
	}
	if evidence.Protocols.SocketDropped != 0 {
		return fmt.Errorf("protocol socket drops: %.0f", evidence.Protocols.SocketDropped)
	}
	if evidence.Resources.Limits != evidence.Limits {
		return fmt.Errorf("resource limits do not match release profile")
	}
	if err := validateAbsoluteResourceGates(evidence.Resources); err != nil {
		return fmt.Errorf("resources: %w", err)
	}
	if len(evidence.Business) == 0 {
		return fmt.Errorf("release row has no business evidence")
	}
	for name, business := range evidence.Business {
		if math.IsNaN(business.Expected) || math.IsInf(business.Expected, 0) ||
			math.IsNaN(business.Actual) || math.IsInf(business.Actual, 0) {
			return fmt.Errorf("business %q contains a non-finite value", name)
		}
		if business.Actual != business.Expected {
			return fmt.Errorf("business %q: got %v, want %v", name, business.Actual, business.Expected)
		}
	}
	if spec.RequireScrapes {
		if evidence.Scrapes == nil {
			return fmt.Errorf("release row requires scrape evidence")
		}
		if err := validateScrapeGates(*evidence.Scrapes); err != nil {
			return fmt.Errorf("scrapes: %w", err)
		}
	}
	return nil
}

func validateReleaseCapture(capture CaptureResult) error {
	values := []struct {
		name  string
		value float64
	}{
		{name: "expected", value: capture.Expected},
		{name: "captured", value: capture.Captured},
		{name: "missing", value: capture.Missing},
		{name: "excess", value: capture.Excess},
		{name: "loss percentage", value: capture.LossPct},
		{name: "excess percentage", value: capture.ExcessPct},
	}
	for _, field := range values {
		if math.IsNaN(field.value) || math.IsInf(field.value, 0) || field.value < 0 {
			return fmt.Errorf("invalid capture %s: %v", field.name, field.value)
		}
	}

	wantMissing := max(0, capture.Expected-capture.Captured)
	wantExcess := max(0, capture.Captured-capture.Expected)
	if capture.Missing != wantMissing || capture.Excess != wantExcess {
		return fmt.Errorf("inconsistent capture accounting")
	}
	wantLossPct := 0.0
	wantExcessPct := 0.0
	if capture.Expected > 0 {
		wantLossPct = wantMissing / capture.Expected * 100
		wantExcessPct = wantExcess / capture.Expected * 100
	}
	if capture.LossPct != wantLossPct || capture.ExcessPct != wantExcessPct {
		return fmt.Errorf("inconsistent capture percentages")
	}
	if err := capture.ValidateExact(); err != nil {
		return err
	}
	return nil
}

func validateReleaseProtocolCounters(counters ProtocolCounters) error {
	values := []struct {
		name  string
		value float64
	}{
		{name: "SIPPackets", value: counters.SIPPackets},
		{name: "RTPPackets", value: counters.RTPPackets},
		{name: "RTCPReports", value: counters.RTCPReports},
		{name: "VQReports", value: counters.VQReports},
		{name: "SocketReceived", value: counters.SocketReceived},
		{name: "SocketDropped", value: counters.SocketDropped},
	}
	for _, field := range values {
		if math.IsNaN(field.value) || math.IsInf(field.value, 0) || field.value < 0 {
			return fmt.Errorf("invalid protocol counter %s: %v", field.name, field.value)
		}
	}
	return nil
}
