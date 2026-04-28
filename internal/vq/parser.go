package vq

import (
	"errors"
	"strconv"
	"strings"
)

var ErrInvalidReport = errors.New("invalid vq-rtcpxr report") //nolint:gochecknoglobals // sentinel error

const kvParts = 2

func ParseReport(body []byte) (*SessionReport, error) {
	if len(body) == 0 {
		return nil, ErrInvalidReport
	}

	lines := strings.Split(string(body), "\n")
	if len(lines) == 0 {
		return nil, ErrInvalidReport
	}

	first := strings.TrimSpace(lines[0])
	if !strings.Contains(first, "VQSessionReport") {
		return nil, ErrInvalidReport
	}

	report := &SessionReport{
		Present: make(map[string]bool),
	}

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.Contains(line, ":") && !strings.Contains(line, "=") {
			continue
		}

		if !strings.Contains(line, "=") {
			continue
		}

		report.parseKVLine(line)
	}

	return report, nil
}

func (r *SessionReport) parseKVLine(line string) {
	tokens := strings.Fields(line)
	for _, token := range tokens {
		parts := strings.SplitN(token, "=", kvParts)
		if len(parts) != kvParts {
			continue
		}

		key := parts[0]
		if !isKnownMetric(key) {
			continue
		}

		val, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			continue
		}

		r.Present[key] = true

		switch key {
		case "NLR":
			r.NLR = val
		case "JDR":
			r.JDR = val
		case "BLD":
			r.BLD = val
		case "GLD":
			r.GLD = val
		case "RTD":
			r.RTD = val
		case "ESD":
			r.ESD = val
		case "IAJ":
			r.IAJ = val
		case "MAJ":
			r.MAJ = val
		case "MOSLQ":
			r.MOSLQ = val
		case "MOSCQ":
			r.MOSCQ = val
		case "RLQ":
			r.RLQ = val
		case "RCQ":
			r.RCQ = val
		case "RERL":
			r.RERL = val
		}
	}
}

func isKnownMetric(key string) bool {
	switch key {
	case "NLR", "JDR", "BLD", "GLD",
		"RTD", "ESD", "IAJ", "MAJ",
		"MOSLQ", "MOSCQ", "RLQ", "RCQ", "RERL":
		return true
	}
	return false
}
