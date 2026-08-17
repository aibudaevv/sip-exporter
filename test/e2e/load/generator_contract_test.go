//go:build e2e

package load

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSIPpGeneratorRequestIsPinnedAndWritesStatistics(t *testing.T) {
	statsDir := t.TempDir()
	req := sippContainerRequest(t.Context(), t,
		[]string{"-sf", "/scenarios/flood_uac.xml"},
		"/host/scenarios", statsDir, true, "host")

	require.Contains(t, req.Image, "@sha256:")
	require.NotContains(t, req.Image, ":latest")
	require.True(t, slices.Contains(req.Cmd, "-trace_stat"))
	require.True(t, slices.Contains(req.Cmd, "-stf"))
	require.True(t, slices.Contains(req.Cmd, "/artifacts/stats.csv"))

	var foundStatsMount bool
	for _, mount := range req.Mounts {
		if mount.Source.Source() == statsDir && mount.Target.Target() == "/artifacts" {
			foundStatsMount = true
			require.False(t, mount.ReadOnly)
		}
	}
	require.True(t, foundStatsMount)
	require.True(t, strings.Contains(req.Image, "pbertera/sipp"))
}

func TestParseSIPpStatsUsesFinalCumulativeRow(t *testing.T) {
	stats := []byte("SuccessfulCall(C);FailedCall(C);Retransmissions(C);CallRate(C);\n" +
		"40;0;0;80.0;\n" +
		"100;0;0;100.0;\n")

	result, err := parseSIPpStats(stats, 0, validPhaseTimestamps())

	require.NoError(t, err)
	require.Equal(t, 100, result.SuccessfulCalls)
	require.Zero(t, result.FailedCalls)
	require.Zero(t, result.Retransmissions)
	require.Equal(t, 100.0, result.ActualRate)
}

func TestParseSIPpStatsRejectsIncompleteInput(t *testing.T) {
	tests := []struct {
		name  string
		stats string
	}{
		{name: "missing statistics", stats: ""},
		{name: "header only", stats: "SuccessfulCall(C);FailedCall(C);Retransmissions(C);CallRate(C);\n"},
		{name: "missing counter", stats: "SuccessfulCall(C);FailedCall(C);CallRate(C);\n100;0;100.0;\n"},
		{name: "malformed counter", stats: "SuccessfulCall(C);FailedCall(C);Retransmissions(C);CallRate(C);\n100;bad;0;100.0;\n"},
		{name: "not-a-number rate", stats: "SuccessfulCall(C);FailedCall(C);Retransmissions(C);CallRate(C);\n100;0;0;NaN;\n"},
		{name: "positive infinite rate", stats: "SuccessfulCall(C);FailedCall(C);Retransmissions(C);CallRate(C);\n100;0;0;+Inf;\n"},
		{name: "negative infinite rate", stats: "SuccessfulCall(C);FailedCall(C);Retransmissions(C);CallRate(C);\n100;0;0;-Inf;\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSIPpStats([]byte(tt.stats), 0, validPhaseTimestamps())
			require.Error(t, err)
		})
	}
}

func TestParseSIPpStatsPreservesKnownStateOnMalformedStatistics(t *testing.T) {
	phases := validPhaseTimestamps()
	result, err := parseSIPpStats([]byte("broken"), 97, phases)

	require.Error(t, err)
	require.Equal(t, 97, result.ExitCode)
	require.Equal(t, phases, result.Phases)
}

func TestGeneratorResultValidateFailsClosed(t *testing.T) {
	spec := WorkloadSpec{Calls: 100, Rate: 100}
	valid := GeneratorResult{
		ExitCode:        0,
		SuccessfulCalls: 100,
		FailedCalls:     0,
		Retransmissions: 0,
		ActualRate:      100,
		Phases:          validPhaseTimestamps(),
	}

	tests := []struct {
		name   string
		mutate func(*GeneratorResult)
	}{
		{name: "non-zero exit", mutate: func(r *GeneratorResult) { r.ExitCode = 1 }},
		{name: "partial success", mutate: func(r *GeneratorResult) { r.SuccessfulCalls = 99 }},
		{name: "failed call", mutate: func(r *GeneratorResult) { r.FailedCalls = 1 }},
		{name: "unexpected retransmission", mutate: func(r *GeneratorResult) { r.Retransmissions = 1 }},
		{name: "rate below tolerance", mutate: func(r *GeneratorResult) { r.ActualRate = 97.99 }},
		{name: "rate above tolerance", mutate: func(r *GeneratorResult) { r.ActualRate = 102.01 }},
		{name: "not-a-number rate", mutate: func(r *GeneratorResult) { r.ActualRate = math.NaN() }},
		{name: "positive infinite rate", mutate: func(r *GeneratorResult) { r.ActualRate = math.Inf(1) }},
		{name: "negative infinite rate", mutate: func(r *GeneratorResult) { r.ActualRate = math.Inf(-1) }},
		{name: "missing readiness", mutate: func(r *GeneratorResult) { r.Phases.Ready = time.Time{} }},
		{name: "measure before readiness", mutate: func(r *GeneratorResult) { r.Phases.MeasureStart = r.Phases.Ready.Add(-time.Nanosecond) }},
		{name: "drain before measure end", mutate: func(r *GeneratorResult) { r.Phases.DrainEnd = r.Phases.MeasureEnd.Add(-time.Nanosecond) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := valid
			tt.mutate(&result)
			require.Error(t, result.Validate(spec))
		})
	}
}

func TestGeneratorResultValidateRejectsInvalidRequestedRate(t *testing.T) {
	result := GeneratorResult{
		SuccessfulCalls: 100,
		ActualRate:      100,
		Phases:          validPhaseTimestamps(),
	}
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		require.Error(t, result.Validate(WorkloadSpec{Calls: 100, Rate: rate}))
	}
}

func TestGeneratorResultValidateAcceptsRateBoundaries(t *testing.T) {
	for _, rate := range []float64{98, 100, 102} {
		t.Run(time.Duration(rate).String(), func(t *testing.T) {
			result := GeneratorResult{
				ExitCode:        0,
				SuccessfulCalls: 100,
				ActualRate:      rate,
				Phases:          validPhaseTimestamps(),
			}
			require.NoError(t, result.Validate(WorkloadSpec{Calls: 100, Rate: 100}))
		})
	}
}

func validPhaseTimestamps() PhaseTimestamps {
	warmup := time.Unix(1, 0)
	return PhaseTimestamps{
		WarmupStart:  warmup,
		Ready:        warmup.Add(time.Second),
		MeasureStart: warmup.Add(2 * time.Second),
		MeasureEnd:   warmup.Add(3 * time.Second),
		DrainEnd:     warmup.Add(4 * time.Second),
	}
}
