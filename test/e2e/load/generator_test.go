//go:build e2e

package load

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	minOfferedRateRatio = 0.98
	maxOfferedRateRatio = 1.02
)

type (
	WorkloadSpec struct {
		Calls int
		Rate  float64
	}

	PhaseTimestamps struct {
		WarmupStart  time.Time
		Ready        time.Time
		MeasureStart time.Time
		MeasureEnd   time.Time
		DrainEnd     time.Time
	}

	GeneratorResult struct {
		ExitCode        int
		SuccessfulCalls int
		FailedCalls     int
		Retransmissions int
		ActualRate      float64
		Phases          PhaseTimestamps
	}
)

func (r GeneratorResult) Validate(spec WorkloadSpec) error {
	switch {
	case r.ExitCode != 0:
		return fmt.Errorf("SIPp exit code: %d", r.ExitCode)
	case r.SuccessfulCalls != spec.Calls:
		return fmt.Errorf("successful calls: got %d, want %d", r.SuccessfulCalls, spec.Calls)
	case r.FailedCalls != 0:
		return fmt.Errorf("failed calls: %d", r.FailedCalls)
	case r.Retransmissions != 0:
		return fmt.Errorf("retransmissions: %d", r.Retransmissions)
	case spec.Rate <= 0 || math.IsNaN(spec.Rate) || math.IsInf(spec.Rate, 0):
		return fmt.Errorf("offered rate must be positive: %v", spec.Rate)
	case math.IsNaN(r.ActualRate) || math.IsInf(r.ActualRate, 0):
		return fmt.Errorf("actual rate must be finite: %v", r.ActualRate)
	case r.ActualRate < spec.Rate*minOfferedRateRatio || r.ActualRate > spec.Rate*maxOfferedRateRatio:
		return fmt.Errorf("actual rate %.2f outside [%.2f, %.2f]",
			r.ActualRate, spec.Rate*minOfferedRateRatio, spec.Rate*maxOfferedRateRatio)
	case !r.Phases.valid():
		return fmt.Errorf("invalid phase timestamps: %+v", r.Phases)
	default:
		return nil
	}
}

func (p PhaseTimestamps) valid() bool {
	return !p.WarmupStart.IsZero() &&
		!p.Ready.IsZero() &&
		!p.MeasureStart.IsZero() &&
		!p.MeasureEnd.IsZero() &&
		!p.DrainEnd.IsZero() &&
		!p.Ready.Before(p.WarmupStart) &&
		!p.MeasureStart.Before(p.Ready) &&
		!p.MeasureEnd.Before(p.MeasureStart) &&
		!p.DrainEnd.Before(p.MeasureEnd)
}

func parseSIPpStats(data []byte, exitCode int, phases PhaseTimestamps) (GeneratorResult, error) {
	result := GeneratorResult{ExitCode: exitCode, Phases: phases}
	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.Comma = ';'
	reader.FieldsPerRecord = -1

	rows, err := reader.ReadAll()
	if err != nil {
		return result, fmt.Errorf("read SIPp statistics: %w", err)
	}
	if len(rows) < 2 {
		return result, fmt.Errorf("SIPp statistics have %d rows", len(rows))
	}

	columns := make(map[string]int, len(rows[0]))
	for i, name := range rows[0] {
		columns[strings.TrimSpace(name)] = i
	}
	row := rows[len(rows)-1]
	if len(row) == 1 && row[0] == "" {
		return result, io.ErrUnexpectedEOF
	}

	successful, err := parseSIPpInt(columns, row, "SuccessfulCall(C)")
	if err != nil {
		return result, err
	}
	result.SuccessfulCalls = successful
	failed, err := parseSIPpInt(columns, row, "FailedCall(C)")
	if err != nil {
		return result, err
	}
	result.FailedCalls = failed
	retransmissions, err := parseSIPpInt(columns, row, "Retransmissions(C)")
	if err != nil {
		return result, err
	}
	result.Retransmissions = retransmissions
	actualRate, err := parseSIPpFloat(columns, row, "CallRate(C)")
	if err != nil {
		return result, err
	}
	result.ActualRate = actualRate

	return result, nil
}

func parseSIPpInt(columns map[string]int, row []string, name string) (int, error) {
	value, err := sippField(columns, row, name)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse SIPp counter %s=%q: %w", name, value, err)
	}
	return parsed, nil
}

func parseSIPpFloat(columns map[string]int, row []string, name string) (float64, error) {
	value, err := sippField(columns, row, name)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse SIPp counter %s=%q: %w", name, value, err)
	}
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("parse SIPp counter %s=%q: non-finite value", name, value)
	}
	return parsed, nil
}

func sippField(columns map[string]int, row []string, name string) (string, error) {
	index, ok := columns[name]
	if !ok {
		return "", fmt.Errorf("SIPp statistics missing %s", name)
	}
	if index >= len(row) {
		return "", fmt.Errorf("SIPp statistics row missing %s", name)
	}
	return strings.TrimSpace(row[index]), nil
}
