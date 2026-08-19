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

func TestSIPpGeneratorArgsUseUniqueCallIDs(t *testing.T) {
	first := sippGeneratorArgs("call_highrate_uac.xml", "30002", 30000, 500, "30001")
	second := sippGeneratorArgs("call_highrate_uac.xml", "30002", 30000, 500, "30001")

	callIDs := make([]string, 0, 2)
	for _, args := range [][]string{first, second} {
		index := slices.Index(args, "-cid_str")
		var callIDFlags int
		for _, arg := range args {
			if arg == "-cid_str" {
				callIDFlags++
			}
		}
		require.NotEqual(t, -1, index)
		require.Equal(t, 1, callIDFlags)
		require.Less(t, index+1, len(args))
		require.Contains(t, args[index+1], "%u")
		require.Contains(t, args[index+1], "%p")
		require.Contains(t, args[index+1], "%s")
		require.Equal(t, "127.0.0.1:30001", args[len(args)-1])
		callIDs = append(callIDs, args[index+1])
	}
	require.NotEqual(t, callIDs[0], callIDs[1])
}

func TestParseSIPpStatsUsesFinalCumulativeRow(t *testing.T) {
	stats := []byte("StartTime;CurrentTime;TotalCallCreated;SuccessfulCall(C);FailedCall(C);Retransmissions(C);CallRate(C);\n" +
		"2026-08-19\t10:05:27.760190\t1787133927.760190;2026-08-19\t10:05:28.260190\t1787133928.260190;40;40;0;0;80.0;\n" +
		"2026-08-19\t10:05:27.760190\t1787133927.760190;2026-08-19\t10:05:28.760190\t1787133928.760190;100;100;0;0;100.0;\n" +
		"2026-08-19\t10:05:27.760190\t1787133927.760190;2026-08-19\t10:06:17.781922\t1787133977.781922;100;100;0;0;39.9;\n")

	result, err := parseSIPpStats(stats, 0, validPhaseTimestamps())

	require.NoError(t, err)
	require.Equal(t, 100, result.SuccessfulCalls)
	require.Zero(t, result.FailedCalls)
	require.Zero(t, result.Retransmissions)
	require.Equal(t, 39.9, result.ActualRate)
	require.Equal(t, time.Unix(1787133927, 760190000), result.startedAt)
	require.Equal(t, time.Unix(1787133928, 760190000), result.rampEndAt)
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
		{name: "missing start time", stats: "SuccessfulCall(C);FailedCall(C);Retransmissions(C);CallRate(C);\n100;0;0;100.0;\n"},
		{name: "malformed start time", stats: "StartTime;SuccessfulCall(C);FailedCall(C);Retransmissions(C);CallRate(C);\nbad;100;0;0;100.0;\n"},
		{name: "non-finite start time", stats: "StartTime;SuccessfulCall(C);FailedCall(C);Retransmissions(C);CallRate(C);\n+Inf;100;0;0;100.0;\n"},
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

func TestParseSIPpRampEndRejectsInvalidEvidence(t *testing.T) {
	columns := map[string]int{"CurrentTime": 0, "TotalCallCreated": 1}
	validRows := [][]string{{"100.100000", "99"}, {"100.200000", "100"}, {"100.300000", "100"}}
	tests := []struct {
		name    string
		columns map[string]int
		rows    [][]string
		want    time.Time
		wantErr string
	}{
		{name: "first completed ramp row", columns: columns, rows: validRows, want: time.Unix(100, 200000000)},
		{name: "missing created column", columns: map[string]int{"CurrentTime": 0}, rows: validRows, wantErr: "TotalCallCreated"},
		{name: "malformed created count", columns: columns, rows: [][]string{{"100.100000", "bad"}}, wantErr: "TotalCallCreated"},
		{name: "target never reached", columns: columns, rows: [][]string{{"100.100000", "99"}}, wantErr: "never reached"},
		{name: "missing current time", columns: map[string]int{"TotalCallCreated": 0}, rows: [][]string{{"100"}}, wantErr: "CurrentTime"},
		{name: "malformed current time", columns: columns, rows: [][]string{{"bad", "100"}}, wantErr: "CurrentTime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSIPpRampEnd(tt.columns, tt.rows, 100)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSIPpRampRate(t *testing.T) {
	start := time.Unix(100, 0)
	tests := []struct {
		name    string
		calls   int
		start   time.Time
		end     time.Time
		want    float64
		wantErr string
	}{
		{name: "valid", calls: 2000, start: start, end: start.Add(20 * time.Second), want: 100},
		{name: "missing calls", start: start, end: start.Add(time.Second), wantErr: "positive"},
		{name: "missing start", calls: 1, end: start.Add(time.Second), wantErr: "invalid"},
		{name: "missing end", calls: 1, start: start, wantErr: "invalid"},
		{name: "empty interval", calls: 1, start: start, end: start, wantErr: "after"},
		{name: "reversed interval", calls: 1, start: start, end: start.Add(-time.Nanosecond), wantErr: "after"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sippRampRate(tt.calls, tt.start, tt.end)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseSIPpTimestampRejectsInvalidFraction(t *testing.T) {
	columns := map[string]int{"CurrentTime": 0}
	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "empty", wantErr: "CurrentTime"},
		{name: "excess precision", value: "100.1234567890", wantErr: "precision"},
		{name: "non-numeric fraction", value: "100.bad", wantErr: "CurrentTime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSIPpTimestamp(columns, []string{tt.value}, "CurrentTime")
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
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
