//go:build e2e

package load

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReleaseSoakWarmupProfile(t *testing.T) {
	profile := releaseSoakWarmupProfile()

	require.Equal(t, WorkloadSpec{Calls: 30000, Rate: 500}, profile.Workload)
	require.Equal(t, 7.0, profile.PacketsPerCall)
	require.Equal(t, time.Minute, releaseSoakWarmupDuration)
}

func TestValidateReleaseSoakWarmupRejectsEachFailure(t *testing.T) {
	started := time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC)
	profile := releaseSoakWarmupProfile()
	valid := func() soakWarmupEvidence {
		return soakWarmupEvidence{
			Generator: GeneratorResult{
				SuccessfulCalls: 30000, ActualRate: 500,
				Phases: phaseInterval(started, started.Add(time.Minute)),
			},
			Capture:   CaptureResult{Expected: 210000, Captured: 210000},
			Protocols: ProtocolCounters{SIPPackets: 210000, SocketReceived: 210000},
		}
	}
	tests := []struct {
		name   string
		mutate func(*soakWarmupEvidence)
	}{
		{name: "valid"},
		{name: "failed call", mutate: func(e *soakWarmupEvidence) { e.Generator.FailedCalls = 1 }},
		{name: "retransmission", mutate: func(e *soakWarmupEvidence) { e.Generator.Retransmissions = 1 }},
		{name: "capture missing", mutate: func(e *soakWarmupEvidence) { e.Capture = newCaptureResult(210000, 209999) }},
		{name: "capture excess", mutate: func(e *soakWarmupEvidence) { e.Capture = newCaptureResult(210000, 210001) }},
		{name: "capture inconsistent", mutate: func(e *soakWarmupEvidence) { e.Capture.Captured = 209999 }},
		{name: "capture non-finite", mutate: func(e *soakWarmupEvidence) { e.Capture.Captured = math.NaN() }},
		{name: "protocol mismatch", mutate: func(e *soakWarmupEvidence) { e.Protocols.SIPPackets = 209999 }},
		{name: "socket receive mismatch", mutate: func(e *soakWarmupEvidence) { e.Protocols.SocketReceived = 209999 }},
		{name: "unexpected RTP", mutate: func(e *soakWarmupEvidence) { e.Protocols.RTPPackets = 1 }},
		{name: "unexpected RTCP", mutate: func(e *soakWarmupEvidence) { e.Protocols.RTCPReports = 1 }},
		{name: "unexpected VQ", mutate: func(e *soakWarmupEvidence) { e.Protocols.VQReports = 1 }},
		{name: "invalid protocol counter", mutate: func(e *soakWarmupEvidence) { e.Protocols.RTPPackets = math.NaN() }},
		{name: "socket drop", mutate: func(e *soakWarmupEvidence) { e.Protocols.SocketDropped = 1 }},
		{name: "system error", mutate: func(e *soakWarmupEvidence) { e.ErrorCount = 1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := valid()
			if tt.mutate != nil {
				tt.mutate(&evidence)
			}
			err := validateReleaseSoakWarmup(profile, evidence)
			if tt.mutate == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
}

func TestReleaseProfileSpecs(t *testing.T) {
	tests := []struct {
		name         string
		profile      releaseProfileSpec
		wantCalls    int
		wantRate     float64
		wantLimits   WorkloadLimits
		wantScrapes  bool
		wantBusiness map[string]float64
	}{
		{
			name:         "full call nominal",
			profile:      releaseFullCallNominalProfile(),
			wantCalls:    30000,
			wantRate:     1000,
			wantLimits:   nominalLimits,
			wantBusiness: map[string]float64{"invites": 30000, "ser": 100},
		},
		{
			name:         "full call peak",
			profile:      releaseFullCallPeakProfile(),
			wantCalls:    54000,
			wantRate:     1800,
			wantLimits:   peakLimits,
			wantScrapes:  true,
			wantBusiness: map[string]float64{"invites": 54000, "ser": 100},
		},
		{
			name:         "ten minute soak",
			profile:      releaseSoakProfile(),
			wantCalls:    300000,
			wantRate:     500,
			wantLimits:   nominalLimits,
			wantBusiness: map[string]float64{"invites": 300000, "ser": 100},
		},
		{
			name:         "invite flood",
			profile:      releaseINVITEFloodProfile(),
			wantCalls:    150000,
			wantRate:     5000,
			wantLimits:   peakLimits,
			wantBusiness: map[string]float64{"invites": 150000},
		},
		{
			name:         "concurrent dialogs",
			profile:      releaseConcurrentDialogsProfile(),
			wantCalls:    2000,
			wantRate:     100,
			wantLimits:   peakLimits,
			wantBusiness: map[string]float64{"invites": 2000, "peak_sessions": 2000},
		},
		{
			name:        "multi NIC",
			profile:     releaseMultiNICProfile(),
			wantCalls:   45000,
			wantRate:    1500,
			wantLimits:  peakLimits,
			wantScrapes: false,
			wantBusiness: map[string]float64{
				"invites_iface_1": 15000, "invites_iface_2": 15000, "invites_iface_3": 15000,
				"cross_interface_series": 0, "unexpected_series": 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantCalls, tt.profile.Workload.Calls)
			require.Equal(t, tt.wantRate, tt.profile.Workload.Rate)
			require.Equal(t, tt.wantLimits, tt.profile.Limits)
			require.Equal(t, tt.wantScrapes, tt.profile.RequireScrapes)
			require.Equal(t, tt.wantBusiness, tt.profile.Business)
		})
	}
}

func TestMixedReleaseProfileSpecs(t *testing.T) {
	tests := []struct {
		name        string
		profile     mixedReleaseProfileSpec
		generators  []mixedGeneratorSpec
		limits      WorkloadLimits
		warmup      time.Duration
		wantScrapes bool
	}{
		{
			name:    "nominal",
			profile: mixedNominalProfile(),
			generators: []mixedGeneratorSpec{
				{Name: "yealink", UserAgent: "Yealink SIP-T46S 66.15.0.10", Workload: WorkloadSpec{Calls: 750, Rate: 25}},
				{Name: "grandstream", UserAgent: "Grandstream GXP2160 1.0.9.50", Workload: WorkloadSpec{Calls: 750, Rate: 25}},
			},
			limits: nominalLimits,
		},
		{
			name:    "peak",
			profile: mixedPeakProfile(),
			generators: []mixedGeneratorSpec{
				{Name: "yealink", UserAgent: "Yealink SIP-T46S 66.15.0.10", Workload: WorkloadSpec{Calls: 1200, Rate: 40}},
				{Name: "grandstream", UserAgent: "Grandstream GXP2160 1.0.9.50", Workload: WorkloadSpec{Calls: 1200, Rate: 40}},
			},
			limits:      peakLimits,
			wantScrapes: true,
		},
		{
			name:    "soak",
			profile: mixedSoakProfile(),
			generators: []mixedGeneratorSpec{
				{Name: "default", UserAgent: "sipp-rtp-uac", Workload: WorkloadSpec{Calls: 30000, Rate: 50}},
			},
			limits: nominalLimits,
			warmup: time.Minute,
		},
		{
			name:    "soak warmup",
			profile: mixedSoakWarmupProfile(),
			generators: []mixedGeneratorSpec{
				{Name: "default", UserAgent: "sipp-rtp-uac", Workload: WorkloadSpec{Calls: 3000, Rate: 50}},
			},
			limits: nominalLimits,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.generators, tt.profile.Generators)
			require.Equal(t, tt.limits, tt.profile.Limits)
			require.Equal(t, 18*time.Second, tt.profile.MediaDuration)
			require.Equal(t, tt.warmup, tt.profile.Warmup)
			require.Equal(t, tt.wantScrapes, tt.profile.RequireScrapes)
		})
	}
}

func TestMixedReleaseRowMutationMatrix(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*mixedReleaseEvidence)
		wantErr bool
	}{
		{name: "valid"},
		{name: "RTP total lower boundary", mutate: func(e *mixedReleaseEvidence) {
			e.ActualRTPPackets = 4233600
			e.Row.Protocols.RTPPackets = 4233600
		}},
		{name: "RTP total upper boundary", mutate: func(e *mixedReleaseEvidence) {
			e.ActualRTPPackets = 4406400
			e.Row.Protocols.RTPPackets = 4406400
		}},
		{name: "RTP p95 lower boundary", mutate: func(e *mixedReleaseEvidence) {
			e.ActualRTPPPSP95 = 129600
		}},
		{name: "RTP p95 upper boundary", mutate: func(e *mixedReleaseEvidence) {
			e.ActualRTPPPSP95 = 158400
		}},
		{name: "scrape boundaries pass", mutate: func(e *mixedReleaseEvidence) {
			e.Row.Scrapes = &ScrapeSummary{Count: 1, P50MS: 10, P95MS: 99.999, P99MS: 199.999}
		}},
		{name: "missing generator", mutate: func(e *mixedReleaseEvidence) {
			e.Row.Generators = nil
			e.GeneratorStarts = nil
		}, wantErr: true},
		{name: "one generator under rate", mutate: func(e *mixedReleaseEvidence) {
			e.Row.Generators[1].Result.ActualRate = 39.19
		}, wantErr: true},
		{name: "generator failed call", mutate: func(e *mixedReleaseEvidence) {
			e.Row.Generators[0].Result.FailedCalls = 1
		}, wantErr: true},
		{name: "generator start skew", mutate: func(e *mixedReleaseEvidence) {
			e.GeneratorStarts[1] = e.GeneratorStarts[0].Add(600*time.Millisecond + time.Nanosecond)
		}, wantErr: true},
		{name: "SIP missing", mutate: func(e *mixedReleaseEvidence) {
			e.Row.Capture = newCaptureResult(14400, 14399)
			e.Row.Protocols.SIPPackets = 14399
		}, wantErr: true},
		{name: "SIP excess", mutate: func(e *mixedReleaseEvidence) {
			e.Row.Capture = newCaptureResult(14400, 14401)
			e.Row.Protocols.SIPPackets = 14401
		}, wantErr: true},
		{name: "SER below 100", mutate: func(e *mixedReleaseEvidence) {
			e.SER = 99.999
		}, wantErr: true},
		{name: "RTP total below tolerance", mutate: func(e *mixedReleaseEvidence) {
			e.ActualRTPPackets = 4233599
			e.Row.Protocols.RTPPackets = 4233599
		}, wantErr: true},
		{name: "RTP total above tolerance", mutate: func(e *mixedReleaseEvidence) {
			e.ActualRTPPackets = 4406401
			e.Row.Protocols.RTPPackets = 4406401
		}, wantErr: true},
		{name: "RTP p95 below tolerance", mutate: func(e *mixedReleaseEvidence) {
			e.ActualRTPPPSP95 = 129599
		}, wantErr: true},
		{name: "RTP p95 above tolerance", mutate: func(e *mixedReleaseEvidence) {
			e.ActualRTPPPSP95 = 158401
		}, wantErr: true},
		{name: "socket drop", mutate: func(e *mixedReleaseEvidence) {
			e.Row.Protocols.SocketDropped = 1
		}, wantErr: true},
		{name: "userspace RTP drop", mutate: func(e *mixedReleaseEvidence) {
			e.Row.Resources.RTPDrops = 1
		}, wantErr: true},
		{name: "exporter error", mutate: func(e *mixedReleaseEvidence) {
			e.Row.ErrorCount = 1
		}, wantErr: true},
		{name: "healthy RTP loss", mutate: func(e *mixedReleaseEvidence) {
			e.RTPLost = 1
		}, wantErr: true},
		{name: "healthy RTP duplicate", mutate: func(e *mixedReleaseEvidence) {
			e.RTPDuplicate = 1
		}, wantErr: true},
		{name: "healthy RTP out of order", mutate: func(e *mixedReleaseEvidence) {
			e.RTPOutOfOrder = 1
		}, wantErr: true},
		{name: "resource gate", mutate: func(e *mixedReleaseEvidence) {
			e.Row.Resources.CPUP95Percent = 80.000001
		}, wantErr: true},
		{name: "missing scrapes", mutate: func(e *mixedReleaseEvidence) {
			e.Row.Scrapes = nil
		}, wantErr: true},
		{name: "scrape p95 boundary fails", mutate: func(e *mixedReleaseEvidence) {
			e.Row.Scrapes = &ScrapeSummary{Count: 1, P50MS: 10, P95MS: 100, P99MS: 190}
		}, wantErr: true},
		{name: "scrape p99 boundary fails", mutate: func(e *mixedReleaseEvidence) {
			e.Row.Scrapes = &ScrapeSummary{Count: 1, P50MS: 10, P95MS: 90, P99MS: 200}
		}, wantErr: true},
	}

	profile := mixedPeakProfile()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := validMixedReleaseEvidence()
			if tt.mutate != nil {
				tt.mutate(&evidence)
			}

			err := validateMixedRelease(profile, evidence)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func validMixedReleaseEvidence() mixedReleaseEvidence {
	started := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	firstPhases := phaseInterval(started, started.Add(48*time.Second))
	secondPhases := phaseInterval(started.Add(600*time.Millisecond), started.Add(48*time.Second))
	return mixedReleaseEvidence{
		Row: releaseRowEvidence{
			Generators: []releaseGeneratorEvidence{
				{
					Spec:   WorkloadSpec{Calls: 1200, Rate: 40},
					Result: GeneratorResult{SuccessfulCalls: 1200, ActualRate: 40, Phases: firstPhases},
				},
				{
					Spec:   WorkloadSpec{Calls: 1200, Rate: 40},
					Result: GeneratorResult{SuccessfulCalls: 1200, ActualRate: 40, Phases: secondPhases},
				},
			},
			Capture:   newCaptureResult(14400, 14400),
			Protocols: ProtocolCounters{SIPPackets: 14400, RTPPackets: 4320000},
			Resources: ResourceSummaryV2{Limits: peakLimits},
			Limits:    peakLimits,
			Business: map[string]releaseBusinessEvidence{
				"calls": {Expected: 2400, Actual: 2400},
			},
			Scrapes: &ScrapeSummary{Count: 1, P50MS: 10, P95MS: 90, P99MS: 190},
		},
		SER:                100,
		ExpectedRTPPackets: 4320000,
		ActualRTPPackets:   4320000,
		ExpectedRTPPPSP95:  144000,
		ActualRTPPPSP95:    144000,
		GeneratorStarts:    []time.Time{started, started.Add(600 * time.Millisecond)},
	}
}

func TestMixedReleaseHarnessArguments(t *testing.T) {
	ports := []mixedRTPPorts{
		{UASSIP: "31001", UACSIP: "31002", UASMedia: "31003", UACMedia: "31004"},
		{UASSIP: "31005", UACSIP: "31006", UASMedia: "31007", UACMedia: "31008"},
	}
	plans, err := mixedSippPlans(mixedPeakProfile(), ports)
	require.NoError(t, err)
	require.Len(t, plans, 2)

	for i, plan := range plans {
		require.Equal(t, ports[i], plan.Ports)
		require.Contains(t, plan.UASArgs, ports[i].UASSIP)
		require.Contains(t, plan.UASArgs, ports[i].UASMedia)
		require.Contains(t, plan.UACArgs, ports[i].UACSIP)
		require.Contains(t, plan.UACArgs, ports[i].UACMedia)
		require.Equal(t, "127.0.0.1:"+ports[i].UASSIP, plan.UACArgs[len(plan.UACArgs)-1])
		key := slices.Index(plan.UACArgs, "-key")
		require.NotEqual(t, -1, key)
		require.Less(t, key+2, len(plan.UACArgs))
		require.Equal(t, "user_agent", plan.UACArgs[key+1])
		require.Equal(t, mixedPeakProfile().Generators[i].UserAgent, plan.UACArgs[key+2])
	}
	require.NotEqual(t, plans[0].Ports, plans[1].Ports)
}

func TestMixedReleaseHarnessPortIsolation(t *testing.T) {
	_, ports := allocateMixedRTPPorts(2)
	require.Len(t, ports, 2)

	reservedMedia := make(map[int]struct{})
	for _, portSet := range ports {
		for _, media := range []string{portSet.UASMedia, portSet.UACMedia} {
			port, err := strconv.Atoi(media)
			require.NoError(t, err)
			reservedMedia[port] = struct{}{}
			reservedMedia[port+2] = struct{}{}
		}
	}
	for _, portSet := range ports {
		for _, sip := range []string{portSet.UASSIP, portSet.UACSIP} {
			port, err := strconv.Atoi(sip)
			require.NoError(t, err)
			_, conflicts := reservedMedia[port]
			require.False(t, conflicts, "SIP port %d overlaps SIPp RTP reservation", port)
		}
	}
}

func TestMixedReleaseHarnessLifecycle(t *testing.T) {
	ready := time.Date(2026, time.October, 3, 13, 0, 0, 0, time.UTC)
	valid := func() mixedLifecycleEvidence {
		return mixedLifecycleEvidence{
			UASReady:          []time.Time{ready, ready.Add(time.Millisecond)},
			MeasurementStart:  ready.Add(2 * time.Millisecond),
			GeneratorStarts:   []time.Time{ready.Add(3 * time.Millisecond), ready.Add(500 * time.Millisecond)},
			MeasurementEnd:    ready.Add(49 * time.Second),
			EvidenceCollected: ready.Add(50 * time.Second),
			CleanupStart:      ready.Add(51 * time.Second),
			ScrapeInterval:    100 * time.Millisecond,
		}
	}
	tests := []struct {
		name   string
		mutate func(*mixedLifecycleEvidence)
	}{
		{name: "measurement before readiness", mutate: func(e *mixedLifecycleEvidence) {
			e.MeasurementStart = ready
		}},
		{name: "generator before measurement", mutate: func(e *mixedLifecycleEvidence) {
			e.GeneratorStarts[0] = e.MeasurementStart.Add(-time.Nanosecond)
		}},
		{name: "generator start skew", mutate: func(e *mixedLifecycleEvidence) {
			e.GeneratorStarts[1] = e.GeneratorStarts[0].Add(600*time.Millisecond + time.Nanosecond)
		}},
		{name: "evidence before measurement end", mutate: func(e *mixedLifecycleEvidence) {
			e.EvidenceCollected = e.MeasurementEnd
		}},
		{name: "cleanup before evidence", mutate: func(e *mixedLifecycleEvidence) {
			e.CleanupStart = e.EvidenceCollected
		}},
	}

	require.NoError(t, validateMixedLifecycle(mixedPeakProfile(), valid()))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := valid()
			tt.mutate(&evidence)
			require.Error(t, validateMixedLifecycle(mixedPeakProfile(), evidence))
		})
	}
}

func TestMixedReleaseHarnessScrapeScheduling(t *testing.T) {
	require.Equal(t, time.Duration(0), mixedScrapeInterval(mixedNominalProfile()))
	require.Equal(t, 100*time.Millisecond, mixedScrapeInterval(mixedPeakProfile()))
}

func TestMixedReleaseHarnessWarmupPlanning(t *testing.T) {
	_, ok := mixedWarmupPhase(mixedNominalProfile())
	require.False(t, ok)

	soak := mixedSoakProfile()
	warmup, ok := mixedWarmupPhase(soak)
	require.True(t, ok)
	require.Equal(t, mixedSoakWarmupProfile(), warmup)
	require.Equal(t, mixedSoakProfile(), soak, "warmup planning must not mutate measured profile")
}

func TestMixedReleaseScenarioContracts(t *testing.T) {
	t.Run("dual UA business evidence is exact", func(t *testing.T) {
		profile := mixedNominalProfile()
		invites := []metricSample{
			{name: "sip_exporter_invite_total", labels: map[string]string{
				"carrier": "loopback-carrier", "ua_type": "yealink",
			}, value: 750},
			{name: "sip_exporter_invite_total", labels: map[string]string{
				"carrier": "loopback-carrier", "ua_type": "grandstream",
			}, value: 750},
		}
		ser := []metricSample{
			{name: "sip_exporter_ser", labels: map[string]string{
				"carrier": "loopback-carrier", "ua_type": "yealink",
			}, value: 100},
			{name: "sip_exporter_ser", labels: map[string]string{
				"carrier": "loopback-carrier", "ua_type": "grandstream",
			}, value: 100},
		}

		business := mixedDualUABusiness(invites, ser)
		require.Equal(t, map[string]float64{
			"invites_total": 1500, "invites_loopback_yealink": 750,
			"invites_loopback_grandstream": 750, "ser_loopback_yealink": 100,
			"ser_loopback_grandstream": 100, "unexpected_label_series": 0,
		}, business)
		require.NoError(t, validateMixedRelease(profile,
			mixedReleaseEvidenceFromResult(profile, validMixedScenarioResult(profile), business)))

		invites = append(invites, metricSample{name: "sip_exporter_invite_total",
			labels: map[string]string{"carrier": "other", "ua_type": "other"}, value: 1})
		business = mixedDualUABusiness(invites, ser)
		require.Equal(t, 1.0, business["unexpected_label_series"])
		require.Error(t, validateMixedRelease(profile,
			mixedReleaseEvidenceFromResult(profile, validMixedScenarioResult(profile), business)))
	})

	t.Run("soak excludes warmup counters and samples", func(t *testing.T) {
		profile := mixedSoakProfile()
		result := validMixedScenarioResult(profile)
		start := result.Load.Generator.Phases.MeasureStart
		end := result.Load.Generator.Phases.MeasureEnd
		result.InvitesBefore = 3000
		result.InvitesAfter = 33000
		result.Load.ResourceSamples.Resources = []resourceSample{
			{At: start.Add(-time.Second), WorkingSetBytes: 1 << 20},
			{At: start, WorkingSetBytes: 64 << 20},
			{At: end.Add(-time.Minute), WorkingSetBytes: 72 << 20},
			{At: end, WorkingSetBytes: 1 << 20},
		}

		require.Equal(t, 30000.0, mixedMeasuredInvites(result))
		growth, err := summarizeMixedSoakWorkingSet(result)
		require.NoError(t, err)
		require.Equal(t, soakWorkingSetGrowth{
			FirstMinuteMedianMB: 64, LastMinuteMedianMB: 72,
			GrowthMB: 8, AllowedGrowthMB: 8,
		}, growth)
	})

	t.Run("peak records mixed RTP and scrape evidence", func(t *testing.T) {
		recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
			validRunArtifactV2().Environment, "3addda1", time.Now())
		require.NoError(t, err)
		previous := activeRunRecorder
		activeRunRecorder = recorder
		t.Cleanup(func() { activeRunRecorder = previous })

		t.Run("row", func(t *testing.T) {
			beginScenario(t)
			profile := mixedPeakProfile()
			recordScenarioLimits(t, profile.Limits)
			result := validMixedScenarioResult(profile)
			business := map[string]float64{
				"invites_total": 2400, "invites_loopback_yealink": 1200,
				"invites_loopback_grandstream": 1200, "ser_loopback_yealink": 100,
				"ser_loopback_grandstream": 100, "unexpected_label_series": 0,
			}
			recordMixedReleaseResult(t, result, business)
		})

		metrics := recorder.Snapshot().Results[0].Metrics
		for _, name := range []string{
			"rtp_packets", "rtp_pps_p95", "rtp_lost", "rtp_duplicate",
			"rtp_out_of_order", "scrape_p50_ms", "scrape_p95_ms", "scrape_p99_ms",
			"invites_loopback_yealink", "invites_loopback_grandstream",
			"ser_loopback_yealink", "ser_loopback_grandstream",
		} {
			require.Contains(t, metrics, name)
		}
	})
}

func validMixedScenarioResult(profile mixedReleaseProfileSpec) mixedReleaseResult {
	started := time.Date(2026, time.October, 3, 14, 0, 0, 0, time.UTC)
	duration := releaseDuration + time.Duration(rtpMediaSeconds)*time.Second
	if profile.Warmup != 0 {
		duration = releaseSoakDuration + time.Duration(rtpMediaSeconds)*time.Second
	}
	generators := make([]GeneratorResult, len(profile.Generators))
	for i, generator := range profile.Generators {
		generatorStart := started.Add(time.Duration(i) * time.Millisecond)
		generators[i] = GeneratorResult{
			SuccessfulCalls: generator.Workload.Calls,
			ActualRate:      generator.Workload.Rate,
			Phases:          phaseInterval(generatorStart, started.Add(duration)),
		}
	}
	expectedRTPPackets, expectedRTPPPS, err := mixedRTPExpectations(profile)
	if err != nil {
		panic(err)
	}
	expectedSIP := mixedExpectedSIPPackets(profile)
	aggregate := aggregateMixedGenerators(generators)
	return mixedReleaseResult{
		Load: loadResult{
			Generator: aggregate, Capture: newCaptureResult(expectedSIP, expectedSIP),
			Protocols: ProtocolCounters{SIPPackets: expectedSIP, RTPPackets: expectedRTPPackets},
			RTPPPSP95: expectedRTPPPS, Resources: ResourceSummaryV2{Limits: profile.Limits},
		},
		Generators: generators, Scrapes: &ScrapeSummary{Count: 10, P50MS: 10, P95MS: 20, P99MS: 30},
		SER: 100,
	}
}

func TestReleaseVQMixedProfile(t *testing.T) {
	profile := releaseVQMixedProfile()

	require.Equal(t, WorkloadSpec{Calls: 30000, Rate: 1000}, profile.Workload)
	require.Equal(t, 1.0, profile.PacketsPerCall)
	require.Equal(t, peakLimits, profile.Limits)
	require.Equal(t, map[string]float64{
		"vq_reports":      20000,
		"vq_nlr_count":    10000,
		"vq_mos_lq_count": 20000,
		"vq_rlq_count":    20000,
		"vq_parse_errors": 10000,
	}, profile.Business)
}

func TestValidateVQMixedGeneratorOverlap(t *testing.T) {
	started := time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC)
	valid := func() [vqMixedGeneratorCount]GeneratorResult {
		return [vqMixedGeneratorCount]GeneratorResult{
			{Phases: phaseInterval(started, started.Add(releaseDuration))},
			{Phases: phaseInterval(started.Add(300*time.Millisecond), started.Add(releaseDuration))},
			{Phases: phaseInterval(started.Add(vqMixedStartSkewLimit), started.Add(releaseDuration))},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*[vqMixedGeneratorCount]GeneratorResult)
		wantErr string
	}{
		{name: "valid"},
		{name: "start skew", mutate: func(generators *[vqMixedGeneratorCount]GeneratorResult) {
			generators[2].Phases.MeasureStart = started.Add(vqMixedStartSkewLimit + time.Nanosecond)
		}, wantErr: "start skew"},
		{name: "zero measure start", mutate: func(generators *[vqMixedGeneratorCount]GeneratorResult) {
			generators[1].Phases.MeasureStart = time.Time{}
		}, wantErr: "interval is missing"},
		{name: "zero measure end", mutate: func(generators *[vqMixedGeneratorCount]GeneratorResult) {
			generators[1].Phases.MeasureEnd = time.Time{}
		}, wantErr: "interval is missing"},
		{name: "non-overlap", mutate: func(generators *[vqMixedGeneratorCount]GeneratorResult) {
			generators[0].Phases.MeasureEnd = started.Add(500 * time.Millisecond)
			generators[1].Phases.MeasureEnd = started.Add(500 * time.Millisecond)
		}, wantErr: "intervals do not overlap"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generators := valid()
			if tt.mutate != nil {
				tt.mutate(&generators)
			}
			err := validateVQMixedGeneratorOverlap(generators)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestReleaseProfileBusinessEvidence(t *testing.T) {
	tests := []struct {
		name    string
		profile releaseProfileSpec
		actual  map[string]float64
		mutate  func(map[string]float64)
		wantErr bool
	}{
		{
			name:    "full call nominal",
			profile: releaseFullCallNominalProfile(),
			actual:  map[string]float64{"invites": 30000, "ser": 100},
		},
		{
			name:    "full call nominal invite mutation",
			profile: releaseFullCallNominalProfile(),
			actual:  map[string]float64{"invites": 30000, "ser": 100},
			mutate:  func(actual map[string]float64) { actual["invites"] = 29999 },
			wantErr: true,
		},
		{
			name:    "full call nominal ser mutation",
			profile: releaseFullCallNominalProfile(),
			actual:  map[string]float64{"invites": 30000, "ser": 100},
			mutate:  func(actual map[string]float64) { actual["ser"] = 99 },
			wantErr: true,
		},
		{
			name:    "full call peak",
			profile: releaseFullCallPeakProfile(),
			actual:  map[string]float64{"invites": 54000, "ser": 100},
		},
		{
			name:    "ten minute soak",
			profile: releaseSoakProfile(),
			actual:  map[string]float64{"invites": 300000, "ser": 100},
		},
		{
			name:    "ten minute soak invite mutation",
			profile: releaseSoakProfile(),
			actual:  map[string]float64{"invites": 300000, "ser": 100},
			mutate:  func(actual map[string]float64) { actual["invites"] = 299999 },
			wantErr: true,
		},
		{
			name:    "ten minute soak ser mutation",
			profile: releaseSoakProfile(),
			actual:  map[string]float64{"invites": 300000, "ser": 100},
			mutate:  func(actual map[string]float64) { actual["ser"] = 99 },
			wantErr: true,
		},
		{
			name:    "full call peak invite mutation",
			profile: releaseFullCallPeakProfile(),
			actual:  map[string]float64{"invites": 54000, "ser": 100},
			mutate:  func(actual map[string]float64) { actual["invites"] = 53999 },
			wantErr: true,
		},
		{
			name:    "full call peak ser mutation",
			profile: releaseFullCallPeakProfile(),
			actual:  map[string]float64{"invites": 54000, "ser": 100},
			mutate:  func(actual map[string]float64) { actual["ser"] = 99 },
			wantErr: true,
		},
		{
			name:    "invite flood",
			profile: releaseINVITEFloodProfile(),
			actual:  map[string]float64{"invites": 150000},
		},
		{
			name:    "invite flood mutation",
			profile: releaseINVITEFloodProfile(),
			actual:  map[string]float64{"invites": 150000},
			mutate:  func(actual map[string]float64) { actual["invites"] = 149999 },
			wantErr: true,
		},
		{
			name:    "concurrent dialogs",
			profile: releaseConcurrentDialogsProfile(),
			actual:  map[string]float64{"invites": 2000, "peak_sessions": 2000},
		},
		{
			name:    "concurrent invite mutation",
			profile: releaseConcurrentDialogsProfile(),
			actual:  map[string]float64{"invites": 2000, "peak_sessions": 2000},
			mutate:  func(actual map[string]float64) { actual["invites"] = 1999 },
			wantErr: true,
		},
		{
			name:    "concurrent peak session mutation",
			profile: releaseConcurrentDialogsProfile(),
			actual:  map[string]float64{"invites": 2000, "peak_sessions": 2000},
			mutate:  func(actual map[string]float64) { actual["peak_sessions"] = 1999 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mutate != nil {
				tt.mutate(tt.actual)
			}
			var scrapes *ScrapeSummary
			if tt.profile.RequireScrapes {
				scrapes = &ScrapeSummary{Count: 1, P50MS: 1, P95MS: 1, P99MS: 1}
			}
			evidence := releaseRowFromLoad(tt.profile, validReleaseLoadResult(tt.profile), tt.actual, scrapes)
			err := validateReleaseRow(releaseRowSpec{RequireScrapes: tt.profile.RequireScrapes}, evidence)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestReleaseProfileMutationMatrix(t *testing.T) {
	tests := []struct {
		name           string
		businessKey    string
		requireScrapes bool
		evidence       func() releaseRowEvidence
	}{
		{
			name:        "full call nominal",
			businessKey: "invites",
			evidence: func() releaseRowEvidence {
				profile := releaseFullCallNominalProfile()
				return releaseRowFromLoad(profile, validReleaseLoadResult(profile),
					map[string]float64{"invites": 30000, "ser": 100}, nil)
			},
		},
		{
			name:           "full call peak",
			businessKey:    "invites",
			requireScrapes: true,
			evidence: func() releaseRowEvidence {
				profile := releaseFullCallPeakProfile()
				return releaseRowFromLoad(profile, validReleaseLoadResult(profile),
					map[string]float64{"invites": 54000, "ser": 100},
					&ScrapeSummary{Count: 1, P50MS: 1, P95MS: 1, P99MS: 1})
			},
		},
		{
			name:        "invite flood",
			businessKey: "invites",
			evidence: func() releaseRowEvidence {
				profile := releaseINVITEFloodProfile()
				return releaseRowFromLoad(profile, validReleaseLoadResult(profile),
					map[string]float64{"invites": 150000}, nil)
			},
		},
		{
			name:        "concurrent dialogs",
			businessKey: "peak_sessions",
			evidence: func() releaseRowEvidence {
				profile := releaseConcurrentDialogsProfile()
				return releaseRowFromLoad(profile, validReleaseLoadResult(profile),
					map[string]float64{"invites": 2000, "peak_sessions": 2000}, nil)
			},
		},
		{
			name:        "carrier UA",
			businessKey: "invites_total",
			evidence: func() releaseRowEvidence {
				profile := releaseCarrierUAProfile()
				generators := [2]GeneratorResult{
					{SuccessfulCalls: 27000, ActualRate: 900, Phases: validPhaseTimestamps()},
					{SuccessfulCalls: 27000, ActualRate: 900, Phases: validPhaseTimestamps()},
				}
				return releaseCarrierUARowFromLoad(profile, validReleaseLoadResult(profile), generators,
					map[string]float64{
						"invites_total":                54000,
						"invites_loopback_yealink":     27000,
						"invites_loopback_grandstream": 27000,
						"ser_loopback_yealink":         100,
						"ser_loopback_grandstream":     100,
						"unexpected_label_series":      0,
					})
			},
		},
		{
			name:        "multi NIC",
			businessKey: "invites_iface_1",
			evidence: func() releaseRowEvidence {
				profile := releaseMultiNICProfile()
				generators := validMultiNICGenerators()
				return releaseMultiNICRowFromLoad(profile, validReleaseLoadResult(profile), generators,
					map[string]float64{
						"invites_iface_1": 15000, "invites_iface_2": 15000,
						"invites_iface_3": 15000, "cross_interface_series": 0,
						"unexpected_series": 0,
					})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := releaseRowSpec{RequireScrapes: tt.requireScrapes}
			require.NoError(t, validateReleaseRow(spec, tt.evidence()))

			t.Run("generator calls", func(t *testing.T) {
				evidence := tt.evidence()
				evidence.Generators[0].Result.SuccessfulCalls--
				require.Error(t, validateReleaseRow(spec, evidence))
			})
			t.Run("generator rate", func(t *testing.T) {
				evidence := tt.evidence()
				evidence.Generators[0].Result.ActualRate = 0
				require.Error(t, validateReleaseRow(spec, evidence))
			})
			t.Run("capture", func(t *testing.T) {
				evidence := tt.evidence()
				evidence.Capture = newCaptureResult(evidence.Capture.Expected, evidence.Capture.Expected-1)
				require.Error(t, validateReleaseRow(spec, evidence))
			})
			t.Run("business", func(t *testing.T) {
				evidence := tt.evidence()
				business := evidence.Business[tt.businessKey]
				business.Actual--
				evidence.Business[tt.businessKey] = business
				require.Error(t, validateReleaseRow(spec, evidence))
			})
			if tt.requireScrapes {
				t.Run("scrapes", func(t *testing.T) {
					evidence := tt.evidence()
					evidence.Scrapes = nil
					require.Error(t, validateReleaseRow(spec, evidence))
				})
			}
		})
	}
}

func TestReleaseRowRejectsWrongProfileLimits(t *testing.T) {
	tests := []struct {
		name         string
		profile      releaseProfileSpec
		actualLimits WorkloadLimits
		business     map[string]float64
		scrapes      *ScrapeSummary
	}{
		{name: "nominal with peak limits", profile: releaseFullCallNominalProfile(),
			actualLimits: peakLimits, business: map[string]float64{"invites": 30000, "ser": 100}},
		{name: "peak with nominal limits", profile: releaseFullCallPeakProfile(),
			actualLimits: nominalLimits, business: map[string]float64{"invites": 54000, "ser": 100},
			scrapes: &ScrapeSummary{Count: 1, P50MS: 1, P95MS: 1, P99MS: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validReleaseLoadResult(tt.profile)
			result.Resources.Limits = tt.actualLimits
			evidence := releaseRowFromLoad(tt.profile, result, tt.business, tt.scrapes)

			require.ErrorContains(t,
				validateReleaseRow(releaseRowSpec{RequireScrapes: tt.profile.RequireScrapes}, evidence),
				"resource limits",
			)
		})
	}
}

func TestReleaseCarrierUAProfile(t *testing.T) {
	profile := releaseCarrierUAProfile()

	require.Equal(t, WorkloadSpec{Calls: 54000, Rate: 1800}, profile.Workload)
	require.Equal(t, fullCallPacketsPerCall, profile.PacketsPerCall)
	require.Equal(t, peakLimits, profile.Limits)
	require.False(t, profile.RequireScrapes)
	require.Equal(t, map[string]float64{
		"invites_total":                54000,
		"invites_loopback_yealink":     27000,
		"invites_loopback_grandstream": 27000,
		"ser_loopback_yealink":         100,
		"ser_loopback_grandstream":     100,
		"unexpected_label_series":      0,
	}, profile.Business)
}

func TestReleaseCarrierUABusinessEvidence(t *testing.T) {
	profile := releaseCarrierUAProfile()
	valid := func() map[string]float64 {
		return map[string]float64{
			"invites_total":                54000,
			"invites_loopback_yealink":     27000,
			"invites_loopback_grandstream": 27000,
			"ser_loopback_yealink":         100,
			"ser_loopback_grandstream":     100,
			"unexpected_label_series":      0,
		}
	}
	tests := []struct {
		name    string
		mutate  func(map[string]float64)
		wantErr bool
	}{
		{name: "valid"},
		{name: "aggregate mutation", mutate: func(actual map[string]float64) { actual["invites_total"] = 53999 }, wantErr: true},
		{name: "yealink invite mutation", mutate: func(actual map[string]float64) { actual["invites_loopback_yealink"] = 26999 }, wantErr: true},
		{name: "grandstream invite mutation", mutate: func(actual map[string]float64) { actual["invites_loopback_grandstream"] = 26999 }, wantErr: true},
		{name: "yealink SER mutation", mutate: func(actual map[string]float64) { actual["ser_loopback_yealink"] = 99 }, wantErr: true},
		{name: "grandstream SER mutation", mutate: func(actual map[string]float64) { actual["ser_loopback_grandstream"] = 99 }, wantErr: true},
		{name: "unexpected labels", mutate: func(actual map[string]float64) { actual["unexpected_label_series"] = 1 }, wantErr: true},
		{name: "missing unexpected label evidence", mutate: func(actual map[string]float64) { delete(actual, "unexpected_label_series") }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := valid()
			if tt.mutate != nil {
				tt.mutate(actual)
			}
			evidence := releaseRowFromLoad(profile, validReleaseLoadResult(profile), actual, nil)
			err := validateReleaseRow(releaseRowSpec{}, evidence)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestUnexpectedCarrierUASeries(t *testing.T) {
	tests := []struct {
		name   string
		sample metricSample
		want   float64
	}{
		{name: "allowed yealink", sample: metricSample{labels: map[string]string{"carrier": "loopback-carrier", "ua_type": "yealink"}}},
		{name: "allowed grandstream", sample: metricSample{labels: map[string]string{"carrier": "loopback-carrier", "ua_type": "grandstream"}}},
		{name: "both other", sample: metricSample{labels: map[string]string{"carrier": "other", "ua_type": "other"}}, want: 1},
		{name: "unknown carrier", sample: metricSample{labels: map[string]string{"carrier": "unknown", "ua_type": "yealink"}}, want: 1},
		{name: "unknown UA", sample: metricSample{labels: map[string]string{"carrier": "loopback-carrier", "ua_type": "unknown"}}, want: 1},
		{name: "missing labels", sample: metricSample{labels: map[string]string{}}, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, unexpectedCarrierUASeries([]metricSample{tt.sample}))
		})
	}
}

func TestReleaseCarrierUARowValidatesBothGenerators(t *testing.T) {
	profile := releaseCarrierUAProfile()
	result := validReleaseLoadResult(profile)
	generators := [2]GeneratorResult{
		{SuccessfulCalls: 27000, ActualRate: 900, Phases: validPhaseTimestamps()},
		{SuccessfulCalls: 27000, ActualRate: 900, Phases: validPhaseTimestamps()},
	}
	evidence := releaseCarrierUARowFromLoad(profile, result, generators, map[string]float64{
		"invites_total":                54000,
		"invites_loopback_yealink":     27000,
		"invites_loopback_grandstream": 27000,
		"ser_loopback_yealink":         100,
		"ser_loopback_grandstream":     100,
		"unexpected_label_series":      0,
	})

	require.Equal(t, []releaseGeneratorEvidence{
		{Spec: WorkloadSpec{Calls: 27000, Rate: 900}, Result: generators[0]},
		{Spec: WorkloadSpec{Calls: 27000, Rate: 900}, Result: generators[1]},
	}, evidence.Generators)
	require.NoError(t, validateReleaseRow(releaseRowSpec{}, evidence))

	evidence.Generators[1].Result.ExitCode = 1
	require.Error(t, validateReleaseRow(releaseRowSpec{}, evidence))
}

func TestValidateCarrierUAAggregateRateRejectsSequentialGenerators(t *testing.T) {
	started := time.Date(2026, time.August, 17, 15, 0, 0, 0, time.UTC)
	parallelPhases := phaseInterval(started, started.Add(30*time.Second))
	skewedPhases := phaseInterval(started.Add(10*time.Millisecond), started.Add(30*time.Second+10*time.Millisecond))
	delayedPhases := phaseInterval(started.Add(time.Second), started.Add(31*time.Second))
	sequentialPhases := phaseInterval(started.Add(30*time.Second), started.Add(60*time.Second))
	first := GeneratorResult{SuccessfulCalls: 27000, ActualRate: 900, Phases: parallelPhases}
	parallel := [2]GeneratorResult{first, {
		SuccessfulCalls: 27000, ActualRate: 900, Phases: parallelPhases,
	}}
	aggregate, err := carrierUAAggregateGenerator(parallel)
	require.NoError(t, err)
	require.Equal(t, 54000, aggregate.SuccessfulCalls)
	require.Equal(t, 1800.0, aggregate.ActualRate)

	require.NoError(t, validateCarrierUAAggregateRate(releaseCarrierUAProfile(), parallel))
	require.NoError(t, validateCarrierUAAggregateRate(
		releaseCarrierUAProfile(), [2]GeneratorResult{first, {
			SuccessfulCalls: 27000, ActualRate: 900, Phases: skewedPhases,
		}},
	))
	require.Error(t, validateCarrierUAAggregateRate(
		releaseCarrierUAProfile(), [2]GeneratorResult{first, {
			SuccessfulCalls: 27000, ActualRate: 900, Phases: delayedPhases,
		}},
	))
	require.Error(t, validateCarrierUAAggregateRate(
		releaseCarrierUAProfile(), [2]GeneratorResult{first, {
			SuccessfulCalls: 27000, ActualRate: 900, Phases: sequentialPhases,
		}},
	))
}

func TestReleaseBusinessMetricEntry(t *testing.T) {
	tests := []struct {
		name string
		want MetricEntry
	}{
		{name: "invites_total", want: MetricEntry{Value: 3, Unit: "count", Direction: dirHigherIsBetter}},
		{name: "ser", want: MetricEntry{Value: 3, Unit: "%", Direction: dirHigherIsBetter}},
		{name: "ser_loopback_yealink", want: MetricEntry{Value: 3, Unit: "%", Direction: dirHigherIsBetter}},
		{name: "unexpected_label_series", want: MetricEntry{Value: 3, Unit: "count", Direction: dirLowerIsBetter}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, releaseBusinessMetricEntry(tt.name, 3))
		})
	}
}

func TestRecordReleaseResultIncludesSystemErrors(t *testing.T) {
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "3addda1", time.Now())
	require.NoError(t, err)
	previous := activeRunRecorder
	activeRunRecorder = recorder
	t.Cleanup(func() { activeRunRecorder = previous })

	t.Run("row", func(t *testing.T) {
		beginScenario(t)
		recordScenarioLimits(t, peakLimits)
		result := validReleaseLoadResult(releaseCarrierUAProfile())
		recordLoadResultEvidence(t, result)
		recordReleaseResult(t, result, map[string]float64{"invites": 54000}, nil)
	})

	run := recorder.Snapshot()
	require.Equal(t, MetricEntry{Value: 0, Unit: "count", Direction: dirLowerIsBetter},
		run.Results[0].Metrics["system_errors"])
}

func TestRecordSoakReleaseResultIncludesWorkingSetGrowth(t *testing.T) {
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "3addda1", time.Now())
	require.NoError(t, err)
	previous := activeRunRecorder
	activeRunRecorder = recorder
	t.Cleanup(func() { activeRunRecorder = previous })

	t.Run("row", func(t *testing.T) {
		beginScenario(t)
		profile := releaseSoakProfile()
		recordScenarioLimits(t, profile.Limits)
		result := validReleaseLoadResult(profile)
		recordLoadResultEvidence(t, result)
		postDrainBody := []byte("sip_exporter_channel_length 0\n")
		recordScenarioArtifact(t, "metrics-post-drain.prom", postDrainBody)
		recordSoakReleaseResult(t, result, profile.Business, soakWorkingSetGrowth{
			FirstMinuteMedianMB: 64, LastMinuteMedianMB: 72, GrowthMB: 8, AllowedGrowthMB: 8,
		}, postDrainSnapshot{})
	})

	run := recorder.Snapshot()
	require.Equal(t, map[string]MetricEntry{
		"working_set_first_minute_median_mb": {Value: 64, Unit: "MiB", Direction: dirLowerIsBetter},
		"working_set_last_minute_median_mb":  {Value: 72, Unit: "MiB", Direction: dirLowerIsBetter},
		"working_set_growth_mb":              {Value: 8, Unit: "MiB", Direction: dirLowerIsBetter},
		"post_drain_channel_length":          {Value: 0, Unit: "count", Direction: dirLowerIsBetter},
		"post_drain_active_dialogs":          {Value: 0, Unit: "count", Direction: dirLowerIsBetter},
		"post_drain_active_trackers":         {Value: 0, Unit: "count", Direction: dirLowerIsBetter},
	}, map[string]MetricEntry{
		"working_set_first_minute_median_mb": run.Results[0].Metrics["working_set_first_minute_median_mb"],
		"working_set_last_minute_median_mb":  run.Results[0].Metrics["working_set_last_minute_median_mb"],
		"working_set_growth_mb":              run.Results[0].Metrics["working_set_growth_mb"],
		"post_drain_channel_length":          run.Results[0].Metrics["post_drain_channel_length"],
		"post_drain_active_dialogs":          run.Results[0].Metrics["post_drain_active_dialogs"],
		"post_drain_active_trackers":         run.Results[0].Metrics["post_drain_active_trackers"],
	})
	require.Contains(t, run.Results[0].Artifacts, "scenarios/000/metrics-post-drain.prom")
}

func TestRecordSoakReleaseOutcomeRecordsBeforeReturningGateError(t *testing.T) {
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "3addda1", time.Now())
	require.NoError(t, err)
	previous := activeRunRecorder
	activeRunRecorder = recorder
	t.Cleanup(func() { activeRunRecorder = previous })
	profile := releaseSoakProfile()
	gateErr := errors.New("working-set growth exceeded")
	var outcomeErr error

	t.Run("row", func(t *testing.T) {
		beginScenario(t)
		recordScenarioLimits(t, profile.Limits)
		result := validReleaseLoadResult(profile)
		recordLoadResultEvidence(t, result)
		outcomeErr = recordSoakReleaseOutcome(t, result, profile.Business,
			soakWorkingSetGrowth{FirstMinuteMedianMB: 64, LastMinuteMedianMB: 80, GrowthMB: 16, AllowedGrowthMB: 8},
			postDrainSnapshot{}, gateErr)
	})

	require.ErrorIs(t, outcomeErr, gateErr)
	row := recorder.Snapshot().Results[0]
	require.Equal(t, scenarioStatusFailed, row.Status)
	require.ErrorContains(t, errors.New(row.Failure), gateErr.Error())
	require.Equal(t, 16.0, row.Metrics["working_set_growth_mb"].Value)
}

func TestRecordSoakReleaseOutcomeRecordsBusinessFailure(t *testing.T) {
	recorder, err := newRunRecorderV2(runModeTargeted, t.TempDir(),
		validRunArtifactV2().Environment, "3addda1", time.Now())
	require.NoError(t, err)
	previous := activeRunRecorder
	activeRunRecorder = recorder
	t.Cleanup(func() { activeRunRecorder = previous })
	profile := releaseSoakProfile()
	business := map[string]float64{"invites": 300000, "ser": 98.11}
	var outcomeErr error

	t.Run("row", func(t *testing.T) {
		beginScenario(t)
		recordScenarioLimits(t, profile.Limits)
		result := validReleaseLoadResult(profile)
		recordLoadResultEvidence(t, result)
		recordScenarioArtifact(t, "metrics-post-drain.prom", []byte("sip_exporter_channel_length 0\n"))
		outcomeErr = recordSoakReleaseOutcome(t, result, business,
			soakWorkingSetGrowth{FirstMinuteMedianMB: 64, LastMinuteMedianMB: 64, AllowedGrowthMB: 8},
			postDrainSnapshot{}, nil)
	})

	require.ErrorContains(t, outcomeErr, `business "ser": got 98.11, want 100`)
	row := recorder.Snapshot().Results[0]
	require.Equal(t, scenarioStatusFailed, row.Status)
	require.NoError(t, row.validate())
	require.ErrorContains(t, errors.New(row.Failure), `business "ser": got 98.11, want 100`)
	require.Equal(t, 98.11, row.Metrics["ser"].Value)
}

func validReleaseLoadResult(profile releaseProfileSpec) loadResult {
	expectedPackets := float64(profile.Workload.Calls) * profile.PacketsPerCall
	return loadResult{
		Generator: GeneratorResult{
			SuccessfulCalls: profile.Workload.Calls,
			ActualRate:      profile.Workload.Rate,
			Phases:          validPhaseTimestamps(),
		},
		Capture:   newCaptureResult(expectedPackets, expectedPackets),
		Protocols: ProtocolCounters{SIPPackets: expectedPackets},
		Resources: ResourceSummaryV2{Limits: profile.Limits},
	}
}

func TestValidateReleaseRow(t *testing.T) {
	tests := []struct {
		name    string
		spec    releaseRowSpec
		mutate  func(*releaseRowEvidence)
		wantErr bool
		wantMsg string
	}{
		{name: "valid"},
		{name: "missing generator", mutate: func(e *releaseRowEvidence) { e.Generators = nil }, wantErr: true},
		{name: "generator exit", mutate: func(e *releaseRowEvidence) { e.Generators[0].Result.ExitCode = 1 }, wantErr: true},
		{name: "generator incomplete success", mutate: func(e *releaseRowEvidence) { e.Generators[0].Result.SuccessfulCalls = 99 }, wantErr: true},
		{name: "generator failed calls", mutate: func(e *releaseRowEvidence) { e.Generators[0].Result.FailedCalls = 1 }, wantErr: true},
		{name: "generator retransmissions", mutate: func(e *releaseRowEvidence) { e.Generators[0].Result.Retransmissions = 1 }, wantErr: true},
		{name: "generator rate outside tolerance", mutate: func(e *releaseRowEvidence) { e.Generators[0].Result.ActualRate = 97.99 }, wantErr: true},
		{name: "second generator invalid", mutate: func(e *releaseRowEvidence) {
			e.Generators = append(e.Generators, e.Generators[0])
			e.Generators[1].Result.ExitCode = 1
		}, wantErr: true},
		{name: "capture missing", mutate: func(e *releaseRowEvidence) { e.Capture = newCaptureResult(100, 99) }, wantErr: true},
		{name: "capture excess", mutate: func(e *releaseRowEvidence) { e.Capture = newCaptureResult(100, 101) }, wantErr: true},
		{name: "capture inconsistent counts", mutate: func(e *releaseRowEvidence) {
			e.Capture = CaptureResult{Expected: 100, Captured: 99}
		}, wantErr: true},
		{name: "capture negative expected", mutate: func(e *releaseRowEvidence) { e.Capture.Expected = -1 }, wantErr: true},
		{name: "capture negative captured", mutate: func(e *releaseRowEvidence) { e.Capture.Captured = -1 }, wantErr: true},
		{name: "capture negative missing", mutate: func(e *releaseRowEvidence) { e.Capture.Missing = -1 }, wantErr: true},
		{name: "capture negative excess", mutate: func(e *releaseRowEvidence) { e.Capture.Excess = -1 }, wantErr: true},
		{name: "capture negative loss percent", mutate: func(e *releaseRowEvidence) { e.Capture.LossPct = -1 }, wantErr: true},
		{name: "capture negative excess percent", mutate: func(e *releaseRowEvidence) { e.Capture.ExcessPct = -1 }, wantErr: true},
		{name: "capture not-a-number expected", mutate: func(e *releaseRowEvidence) { e.Capture.Expected = math.NaN() }, wantErr: true},
		{name: "capture not-a-number captured", mutate: func(e *releaseRowEvidence) { e.Capture.Captured = math.NaN() }, wantErr: true},
		{name: "capture not-a-number missing", mutate: func(e *releaseRowEvidence) { e.Capture.Missing = math.NaN() }, wantErr: true},
		{name: "capture not-a-number excess", mutate: func(e *releaseRowEvidence) { e.Capture.Excess = math.NaN() }, wantErr: true},
		{name: "capture not-a-number loss percent", mutate: func(e *releaseRowEvidence) { e.Capture.LossPct = math.NaN() }, wantErr: true},
		{name: "capture not-a-number excess percent", mutate: func(e *releaseRowEvidence) { e.Capture.ExcessPct = math.NaN() }, wantErr: true},
		{name: "capture infinite expected", mutate: func(e *releaseRowEvidence) { e.Capture.Expected = math.Inf(1) }, wantErr: true},
		{name: "capture inconsistent loss percent", mutate: func(e *releaseRowEvidence) { e.Capture.LossPct = 1 }, wantErr: true},
		{name: "capture inconsistent excess percent", mutate: func(e *releaseRowEvidence) { e.Capture.ExcessPct = 1 }, wantErr: true},
		{name: "system errors", mutate: func(e *releaseRowEvidence) { e.ErrorCount = 1 }, wantErr: true},
		{name: "negative system errors", mutate: func(e *releaseRowEvidence) { e.ErrorCount = -1 }, wantErr: true},
		{name: "not-a-number system errors", mutate: func(e *releaseRowEvidence) { e.ErrorCount = math.NaN() }, wantErr: true},
		{name: "non-finite system errors", mutate: func(e *releaseRowEvidence) { e.ErrorCount = math.Inf(1) }, wantErr: true},
		{name: "negative protocol counter", mutate: func(e *releaseRowEvidence) { e.Protocols.SIPPackets = -1 }, wantErr: true, wantMsg: "SIPPackets"},
		{name: "non-finite protocol counter", mutate: func(e *releaseRowEvidence) { e.Protocols.RTPPackets = math.NaN() }, wantErr: true},
		{name: "infinite protocol counter", mutate: func(e *releaseRowEvidence) { e.Protocols.RTCPReports = math.Inf(1) }, wantErr: true},
		{name: "protocol SIP below capture", mutate: func(e *releaseRowEvidence) { e.Protocols.SIPPackets = 99 }, wantErr: true},
		{name: "protocol SIP above capture", mutate: func(e *releaseRowEvidence) { e.Protocols.SIPPackets = 101 }, wantErr: true},
		{name: "protocol socket drops", mutate: func(e *releaseRowEvidence) { e.Protocols.SocketDropped = 1 }, wantErr: true},
		{name: "resource gate failure", mutate: func(e *releaseRowEvidence) { e.Resources.CPUP95Percent = 80.01 }, wantErr: true},
		{name: "resource socket drops", mutate: func(e *releaseRowEvidence) { e.Resources.SocketDrops = 1 }, wantErr: true},
		{name: "resource RTP drops", mutate: func(e *releaseRowEvidence) { e.Resources.RTPDrops = 1 }, wantErr: true},
		{name: "missing business evidence", mutate: func(e *releaseRowEvidence) { e.Business = nil }, wantErr: true},
		{name: "business mismatch", mutate: func(e *releaseRowEvidence) { e.Business["calls"] = releaseBusinessEvidence{Expected: 1, Actual: 0} }, wantErr: true},
		{name: "business matching infinities", mutate: func(e *releaseRowEvidence) {
			e.Business["calls"] = releaseBusinessEvidence{Expected: math.Inf(1), Actual: math.Inf(1)}
		}, wantErr: true},
		{name: "required scrapes missing", spec: releaseRowSpec{RequireScrapes: true}, wantErr: true},
		{name: "required scrapes invalid", spec: releaseRowSpec{RequireScrapes: true}, mutate: func(e *releaseRowEvidence) { e.Scrapes = &ScrapeSummary{} }, wantErr: true},
		{name: "required scrapes negative", spec: releaseRowSpec{RequireScrapes: true}, mutate: func(e *releaseRowEvidence) {
			e.Scrapes = &ScrapeSummary{Count: 1, P50MS: -1, P95MS: 1, P99MS: 2}
		}, wantErr: true},
		{name: "required scrapes unordered", spec: releaseRowSpec{RequireScrapes: true}, mutate: func(e *releaseRowEvidence) {
			e.Scrapes = &ScrapeSummary{Count: 1, P50MS: 2, P95MS: 1, P99MS: 3}
		}, wantErr: true},
		{name: "optional scrapes ignored", mutate: func(e *releaseRowEvidence) { e.Scrapes = &ScrapeSummary{} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := validReleaseRowEvidence()
			if tt.mutate != nil {
				tt.mutate(&evidence)
			}

			err := validateReleaseRow(tt.spec, evidence)
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantMsg != "" {
					require.ErrorContains(t, err, tt.wantMsg)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateReleaseRowAllowsExpectedSystemErrors(t *testing.T) {
	evidence := validReleaseRowEvidence()
	evidence.ErrorCount = 10000

	require.NoError(t, validateReleaseRow(releaseRowSpec{ExpectedSystemErrors: 10000}, evidence))
}

func validReleaseRowEvidence() releaseRowEvidence {
	return releaseRowEvidence{
		Generators: []releaseGeneratorEvidence{{
			Spec: WorkloadSpec{Calls: 100, Rate: 100},
			Result: GeneratorResult{
				SuccessfulCalls: 100,
				ActualRate:      100,
				Phases:          validPhaseTimestamps(),
			},
		}},
		Capture:   newCaptureResult(100, 100),
		Protocols: ProtocolCounters{SIPPackets: 100},
		Resources: ResourceSummaryV2{Limits: peakLimits},
		Limits:    peakLimits,
		Business: map[string]releaseBusinessEvidence{
			"calls": {Expected: 1, Actual: 1},
		},
	}
}
