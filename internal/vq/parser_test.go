package vq

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var fullReportBody = []byte("VQSessionReport: CallTerm\r\n" +
	"CallID: abc123@10.0.1.5\r\n" +
	"LocalID: sip:user1@example.com\r\n" +
	"NLR=0.50 JDR=1.20 BLD=0.30 GLD=0.10\r\n" +
	"RTD=45.5 ESD=20.3 IAJ=5.2 MAJ=3.1\r\n" +
	"MOSLQ=4.5 MOSCQ=4.2 RLQ=92.0 RCQ=88.0\r\n" +
	"RERL=55.0\r\n")

func TestParseReport_FullReport(t *testing.T) {
	report, err := ParseReport(fullReportBody)
	require.NoError(t, err)
	require.NotNil(t, report)

	require.Equal(t, 0.50, report.NLR)
	require.Equal(t, 1.20, report.JDR)
	require.Equal(t, 0.30, report.BLD)
	require.Equal(t, 0.10, report.GLD)
	require.Equal(t, 45.5, report.RTD)
	require.Equal(t, 20.3, report.ESD)
	require.Equal(t, 5.2, report.IAJ)
	require.Equal(t, 3.1, report.MAJ)
	require.Equal(t, 4.5, report.MOSLQ)
	require.Equal(t, 4.2, report.MOSCQ)
	require.Equal(t, 92.0, report.RLQ)
	require.Equal(t, 88.0, report.RCQ)
	require.Equal(t, 55.0, report.RERL)

	expectedPresent := map[string]bool{
		"NLR": true, "JDR": true, "BLD": true, "GLD": true,
		"RTD": true, "ESD": true, "IAJ": true, "MAJ": true,
		"MOSLQ": true, "MOSCQ": true, "RLQ": true, "RCQ": true, "RERL": true,
	}
	require.Equal(t, expectedPresent, report.Present)
}

func TestParseReport_PartialReport(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\nMOSLQ=4.5 NLR=1.0\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.Equal(t, 4.5, report.MOSLQ)
	require.Equal(t, 1.0, report.NLR)
	require.Equal(t, 2, len(report.Present))
	require.True(t, report.Present["MOSLQ"])
	require.True(t, report.Present["NLR"])
}

func TestParseReport_EmptyBody(t *testing.T) {
	_, err := ParseReport([]byte{})
	require.ErrorIs(t, err, ErrInvalidReport)
}

func TestParseReport_InvalidFormat(t *testing.T) {
	_, err := ParseReport([]byte("NOT A VALID REPORT\nNLR=1.0\n"))
	require.ErrorIs(t, err, ErrInvalidReport)
}

func TestParseReport_UnknownKeysIgnored(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\nFOOBAR=123 NLR=1.0\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.Equal(t, 1.0, report.NLR)
	_, ok := report.Present["FOOBAR"]
	require.False(t, ok)
}

func TestParseReport_InvalidValueSkipped(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\nMOSLQ=abc NLR=1.0\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.Equal(t, 1.0, report.NLR)
	_, ok := report.Present["MOSLQ"]
	require.False(t, ok)
}

func TestParseReport_HeaderLinesIgnored(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\nCallID: abc123@10.0.1.5\nLocalID: sip:user1@example.com\nNLR=1.0\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.Equal(t, 1.0, report.NLR)
	require.Equal(t, 1, len(report.Present))
}

func TestParseReport_MultiValueLine(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\nNLR=0.50 JDR=1.20 MOSLQ=4.5\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.Equal(t, 0.50, report.NLR)
	require.Equal(t, 1.20, report.JDR)
	require.Equal(t, 4.5, report.MOSLQ)
	require.Equal(t, 3, len(report.Present))
}

func TestParseReport_LFLineEndings(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\nNLR=1.0\nJDR=2.0\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.Equal(t, 1.0, report.NLR)
	require.Equal(t, 2.0, report.JDR)
}

func TestParseReport_NilBody(t *testing.T) {
	_, err := ParseReport(nil)
	require.ErrorIs(t, err, ErrInvalidReport)
}

func TestParseReport_NLR(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nNLR=0.75\n"))
	require.NoError(t, err)
	require.Equal(t, 0.75, report.NLR)
	require.True(t, report.Present["NLR"])
}

func TestParseReport_JDR(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nJDR=2.5\n"))
	require.NoError(t, err)
	require.Equal(t, 2.5, report.JDR)
	require.True(t, report.Present["JDR"])
}

func TestParseReport_BLD(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nBLD=0.40\n"))
	require.NoError(t, err)
	require.Equal(t, 0.40, report.BLD)
	require.True(t, report.Present["BLD"])
}

func TestParseReport_GLD(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nGLD=0.15\n"))
	require.NoError(t, err)
	require.Equal(t, 0.15, report.GLD)
	require.True(t, report.Present["GLD"])
}

func TestParseReport_RTD(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nRTD=50.0\n"))
	require.NoError(t, err)
	require.Equal(t, 50.0, report.RTD)
	require.True(t, report.Present["RTD"])
}

func TestParseReport_ESD(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nESD=15.7\n"))
	require.NoError(t, err)
	require.Equal(t, 15.7, report.ESD)
	require.True(t, report.Present["ESD"])
}

func TestParseReport_IAJ(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nIAJ=6.3\n"))
	require.NoError(t, err)
	require.Equal(t, 6.3, report.IAJ)
	require.True(t, report.Present["IAJ"])
}

func TestParseReport_MAJ(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nMAJ=4.2\n"))
	require.NoError(t, err)
	require.Equal(t, 4.2, report.MAJ)
	require.True(t, report.Present["MAJ"])
}

func TestParseReport_MOSLQ(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nMOSLQ=3.8\n"))
	require.NoError(t, err)
	require.Equal(t, 3.8, report.MOSLQ)
	require.True(t, report.Present["MOSLQ"])
}

func TestParseReport_MOSCQ(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nMOSCQ=3.5\n"))
	require.NoError(t, err)
	require.Equal(t, 3.5, report.MOSCQ)
	require.True(t, report.Present["MOSCQ"])
}

func TestParseReport_RLQ(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nRLQ=90.0\n"))
	require.NoError(t, err)
	require.Equal(t, 90.0, report.RLQ)
	require.True(t, report.Present["RLQ"])
}

func TestParseReport_RCQ(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nRCQ=85.0\n"))
	require.NoError(t, err)
	require.Equal(t, 85.0, report.RCQ)
	require.True(t, report.Present["RCQ"])
}

func TestParseReport_RERL(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nRERL=60.0\n"))
	require.NoError(t, err)
	require.Equal(t, 60.0, report.RERL)
	require.True(t, report.Present["RERL"])
}
