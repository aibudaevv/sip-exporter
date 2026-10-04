//go:build e2e

package load

import (
	"fmt"
	"math"
	"time"
)

const (
	mixedNominalRate        = 50
	mixedPeakRate           = 80
	mixedSoakRate           = 50
	mixedRTPPacketTolerance = 0.02
	mixedRTPP95Tolerance    = 0.10
)

type (
	mixedGeneratorSpec struct {
		Name      string
		UserAgent string
		Workload  WorkloadSpec
	}

	mixedReleaseProfileSpec struct {
		Generators     []mixedGeneratorSpec
		Limits         WorkloadLimits
		MediaDuration  time.Duration
		Warmup         time.Duration
		RequireScrapes bool
	}

	mixedReleaseEvidence struct {
		Row                releaseRowEvidence
		SER                float64
		ExpectedRTPPackets float64
		ActualRTPPackets   float64
		ExpectedRTPPPSP95  float64
		ActualRTPPPSP95    float64
		GeneratorStarts    []time.Time
		RTPLost            float64
		RTPDuplicate       float64
		RTPOutOfOrder      float64
	}
)

func mixedNominalProfile() mixedReleaseProfileSpec {
	return mixedDualUAProfile(mixedNominalRate, nominalLimits, false)
}

func mixedPeakProfile() mixedReleaseProfileSpec {
	return mixedDualUAProfile(mixedPeakRate, peakLimits, true)
}

func mixedSoakProfile() mixedReleaseProfileSpec {
	return mixedReleaseProfileSpec{
		Generators: []mixedGeneratorSpec{{
			Name:      "default",
			UserAgent: "sipp-rtp-uac",
			Workload: WorkloadSpec{
				Calls: mixedSoakRate * int(releaseSoakDuration/time.Second),
				Rate:  mixedSoakRate,
			},
		}},
		Limits:        nominalLimits,
		MediaDuration: rtpMediaSeconds * time.Second,
		Warmup:        releaseSoakWarmupDuration,
	}
}

func mixedSoakWarmupProfile() mixedReleaseProfileSpec {
	return mixedReleaseProfileSpec{
		Generators: []mixedGeneratorSpec{{
			Name:      "default",
			UserAgent: "sipp-rtp-uac",
			Workload: WorkloadSpec{
				Calls: mixedSoakRate * int(releaseSoakWarmupDuration/time.Second),
				Rate:  mixedSoakRate,
			},
		}},
		Limits:        nominalLimits,
		MediaDuration: rtpMediaSeconds * time.Second,
	}
}

func mixedDualUAProfile(rate int, limits WorkloadLimits, requireScrapes bool) mixedReleaseProfileSpec {
	ratePerGenerator := rate / 2
	callsPerGenerator := ratePerGenerator * int(releaseDuration/time.Second)
	return mixedReleaseProfileSpec{
		Generators: []mixedGeneratorSpec{
			{
				Name:      "yealink",
				UserAgent: "Yealink SIP-T46S 66.15.0.10",
				Workload:  WorkloadSpec{Calls: callsPerGenerator, Rate: float64(ratePerGenerator)},
			},
			{
				Name:      "grandstream",
				UserAgent: "Grandstream GXP2160 1.0.9.50",
				Workload:  WorkloadSpec{Calls: callsPerGenerator, Rate: float64(ratePerGenerator)},
			},
		},
		Limits:         limits,
		MediaDuration:  rtpMediaSeconds * time.Second,
		RequireScrapes: requireScrapes,
	}
}

func validateMixedRelease(profile mixedReleaseProfileSpec, evidence mixedReleaseEvidence) error {
	if err := validateReleaseRow(releaseRowSpec{RequireScrapes: profile.RequireScrapes}, evidence.Row); err != nil {
		return err
	}
	if len(evidence.Row.Generators) != len(profile.Generators) {
		return fmt.Errorf("mixed generator count: got %d, want %d",
			len(evidence.Row.Generators), len(profile.Generators))
	}
	if len(evidence.GeneratorStarts) != len(profile.Generators) {
		return fmt.Errorf("mixed generator start count: got %d, want %d",
			len(evidence.GeneratorStarts), len(profile.Generators))
	}
	for i, generator := range evidence.Row.Generators {
		if generator.Spec != profile.Generators[i].Workload {
			return fmt.Errorf("mixed generator %d workload does not match profile", i)
		}
		if evidence.GeneratorStarts[i].IsZero() ||
			!evidence.GeneratorStarts[i].Equal(generator.Result.Phases.MeasureStart) {
			return fmt.Errorf("mixed generator %d start does not match measurement phase", i)
		}
	}
	if err := validateMixedGeneratorStartSkew(evidence.GeneratorStarts); err != nil {
		return err
	}
	if !finiteFloats(evidence.SER) || evidence.SER != 100 {
		return fmt.Errorf("mixed SER: got %v, want 100", evidence.SER)
	}

	expectedPackets, expectedPPS, err := mixedRTPExpectations(profile)
	if err != nil {
		return err
	}
	if evidence.ExpectedRTPPackets != expectedPackets {
		return fmt.Errorf("expected RTP packets: got %.0f, want %.0f",
			evidence.ExpectedRTPPackets, expectedPackets)
	}
	if evidence.ExpectedRTPPPSP95 != expectedPPS {
		return fmt.Errorf("expected RTP p95: got %.0f, want %.0f",
			evidence.ExpectedRTPPPSP95, expectedPPS)
	}
	if evidence.ActualRTPPackets != evidence.Row.Protocols.RTPPackets {
		return fmt.Errorf("RTP protocol counter does not match mixed evidence")
	}
	if err := validateMixedRelative("RTP packets", evidence.ActualRTPPackets,
		evidence.ExpectedRTPPackets, mixedRTPPacketTolerance); err != nil {
		return err
	}
	if err := validateMixedRelative("RTP p95", evidence.ActualRTPPPSP95,
		evidence.ExpectedRTPPPSP95, mixedRTPP95Tolerance); err != nil {
		return err
	}

	for name, value := range map[string]float64{
		"lost": evidence.RTPLost, "duplicate": evidence.RTPDuplicate,
		"out of order": evidence.RTPOutOfOrder,
	} {
		if !finiteFloats(value) || value < 0 {
			return fmt.Errorf("invalid healthy RTP %s count: %v", name, value)
		}
		if value != 0 {
			return fmt.Errorf("healthy RTP %s count: %.0f", name, value)
		}
	}
	return nil
}

func validateMixedGeneratorStartSkew(starts []time.Time) error {
	if len(starts) == 0 {
		return fmt.Errorf("mixed release has no generator starts")
	}
	earliest := starts[0]
	latest := starts[0]
	for _, started := range starts[1:] {
		if started.Before(earliest) {
			earliest = started
		}
		if started.After(latest) {
			latest = started
		}
	}
	if latest.Sub(earliest) > carrierUAStartSkewLimit {
		return fmt.Errorf("mixed generator start skew %v exceeds %v",
			latest.Sub(earliest), carrierUAStartSkewLimit)
	}
	return nil
}

func mixedRTPExpectations(profile mixedReleaseProfileSpec) (float64, float64, error) {
	mediaSeconds := profile.MediaDuration.Seconds()
	if mediaSeconds <= 0 || math.IsNaN(mediaSeconds) || math.IsInf(mediaSeconds, 0) {
		return 0, 0, fmt.Errorf("invalid mixed media duration: %v", profile.MediaDuration)
	}
	var calls int
	var rate float64
	for _, generator := range profile.Generators {
		calls += generator.Workload.Calls
		rate += generator.Workload.Rate
	}
	return float64(calls) * mediaSeconds * rtpPacketsPerSecond,
		rate * mediaSeconds * rtpPacketsPerSecond, nil
}

func validateMixedRelative(name string, actual, expected, tolerance float64) error {
	if !finiteFloats(actual, expected) || actual < 0 || expected <= 0 {
		return fmt.Errorf("invalid %s evidence: actual=%v expected=%v", name, actual, expected)
	}
	allowed := expected * tolerance
	if math.Abs(actual-expected) > allowed {
		return fmt.Errorf("%s %.0f outside %.0f ± %.0f", name, actual, expected, allowed)
	}
	return nil
}
