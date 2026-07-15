package vq

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseReport_FullReport(t *testing.T) {
	fullReportBody := []byte("VQSessionReport: CallTerm\r\n" +
		"CallID: abc123@10.0.1.5\r\n" +
		"LocalID: sip:user1@example.com\r\n" +
		"NLR=0.50 JDR=1.20 BLD=0.30 GLD=0.10\r\n" +
		"RTD=45.5 ESD=20.3 IAJ=5.2 MAJ=3.1\r\n" +
		"MOSLQ=4.5 MOSCQ=4.2 RLQ=92.0 RCQ=88.0\r\n" +
		"RERL=55.0\r\n")

	report, err := ParseReport(fullReportBody)
	require.NoError(t, err)
	require.NotNil(t, report)

	require.InDelta(t, 0.50, report.NLR, 0.01)
	require.InDelta(t, 1.20, report.JDR, 0.01)
	require.InDelta(t, 0.30, report.BLD, 0.01)
	require.InDelta(t, 0.10, report.GLD, 0.01)
	require.InDelta(t, 45.5, report.RTD, 0.01)
	require.InDelta(t, 20.3, report.ESD, 0.01)
	require.InDelta(t, 5.2, report.IAJ, 0.01)
	require.InDelta(t, 3.1, report.MAJ, 0.01)
	require.InDelta(t, 4.5, report.MOSLQ, 0.01)
	require.InDelta(t, 4.2, report.MOSCQ, 0.01)
	require.InDelta(t, 92.0, report.RLQ, 0.01)
	require.InDelta(t, 88.0, report.RCQ, 0.01)
	require.InDelta(t, 55.0, report.RERL, 0.01)

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
	require.InDelta(t, 4.5, report.MOSLQ, 0.01)
	require.InDelta(t, 1.0, report.NLR, 0.01)
	require.Len(t, report.Present, 2)
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
	require.InDelta(t, 1.0, report.NLR, 0.01)
	_, ok := report.Present["FOOBAR"]
	require.False(t, ok)
}

func TestParseReport_InvalidValueSkipped(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\nMOSLQ=abc NLR=1.0\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.InDelta(t, 1.0, report.NLR, 0.01)
	_, ok := report.Present["MOSLQ"]
	require.False(t, ok)
}

func TestParseReport_HeaderLinesIgnored(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\nCallID: abc123@10.0.1.5\nLocalID: sip:user1@example.com\nNLR=1.0\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.InDelta(t, 1.0, report.NLR, 0.01)
	require.Len(t, report.Present, 1)
}

func TestParseReport_MultiValueLine(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\nNLR=0.50 JDR=1.20 MOSLQ=4.5\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.InDelta(t, 0.50, report.NLR, 0.01)
	require.InDelta(t, 1.20, report.JDR, 0.01)
	require.InDelta(t, 4.5, report.MOSLQ, 0.01)
	require.Len(t, report.Present, 3)
}

func TestParseReport_LFLineEndings(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\nNLR=1.0\nJDR=2.0\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.InDelta(t, 1.0, report.NLR, 0.01)
	require.InDelta(t, 2.0, report.JDR, 0.01)
}

func TestParseReport_NilBody(t *testing.T) {
	_, err := ParseReport(nil)
	require.ErrorIs(t, err, ErrInvalidReport)
}

func TestParseReport_NLR(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nNLR=0.75\n"))
	require.NoError(t, err)
	require.InDelta(t, 0.75, report.NLR, 0.01)
	require.True(t, report.Present["NLR"])
}

func TestParseReport_JDR(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nJDR=2.5\n"))
	require.NoError(t, err)
	require.InDelta(t, 2.5, report.JDR, 0.01)
	require.True(t, report.Present["JDR"])
}

func TestParseReport_BLD(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nBLD=0.40\n"))
	require.NoError(t, err)
	require.InDelta(t, 0.40, report.BLD, 0.01)
	require.True(t, report.Present["BLD"])
}

func TestParseReport_GLD(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nGLD=0.15\n"))
	require.NoError(t, err)
	require.InDelta(t, 0.15, report.GLD, 0.01)
	require.True(t, report.Present["GLD"])
}

func TestParseReport_RTD(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nRTD=50.0\n"))
	require.NoError(t, err)
	require.InDelta(t, 50.0, report.RTD, 0.01)
	require.True(t, report.Present["RTD"])
}

func TestParseReport_ESD(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nESD=15.7\n"))
	require.NoError(t, err)
	require.InDelta(t, 15.7, report.ESD, 0.01)
	require.True(t, report.Present["ESD"])
}

func TestParseReport_IAJ(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nIAJ=6.3\n"))
	require.NoError(t, err)
	require.InDelta(t, 6.3, report.IAJ, 0.01)
	require.True(t, report.Present["IAJ"])
}

func TestParseReport_MAJ(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nMAJ=4.2\n"))
	require.NoError(t, err)
	require.InDelta(t, 4.2, report.MAJ, 0.01)
	require.True(t, report.Present["MAJ"])
}

func TestParseReport_MOSLQ(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nMOSLQ=3.8\n"))
	require.NoError(t, err)
	require.InDelta(t, 3.8, report.MOSLQ, 0.01)
	require.True(t, report.Present["MOSLQ"])
}

func TestParseReport_MOSCQ(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nMOSCQ=3.5\n"))
	require.NoError(t, err)
	require.InDelta(t, 3.5, report.MOSCQ, 0.01)
	require.True(t, report.Present["MOSCQ"])
}

func TestParseReport_RLQ(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nRLQ=90.0\n"))
	require.NoError(t, err)
	require.InDelta(t, 90.0, report.RLQ, 0.01)
	require.True(t, report.Present["RLQ"])
}

func TestParseReport_RCQ(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nRCQ=85.0\n"))
	require.NoError(t, err)
	require.InDelta(t, 85.0, report.RCQ, 0.01)
	require.True(t, report.Present["RCQ"])
}

func TestParseReport_RERL(t *testing.T) {
	report, err := ParseReport([]byte("VQSessionReport: CallTerm\nRERL=60.0\n"))
	require.NoError(t, err)
	require.InDelta(t, 60.0, report.RERL, 0.01)
	require.True(t, report.Present["RERL"])
}

func TestParseReport_RFCFormat_CategoryHeaders(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\r\n" +
		"CallID: 12345@atlanta.example.com\r\n" +
		"LocalMetrics:\r\n" +
		"PacketLoss:NLR=5.0 JDR=2.0\r\n" +
		"BurstGapLoss:BLD=0 BD=0 GLD=2.0 GD=500 GMIN=16\r\n" +
		"Delay:RTD=200 ESD=140 IAJ=2 MAJ=10\r\n" +
		"Signal:SL=-18 NL=-50 RERL=55\r\n" +
		"QualityEst:RLQ=88 RCQ=85 MOSLQ=4.1 MOSCQ=4.0\r\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.NotNil(t, report)

	require.InDelta(t, 5.0, report.NLR, 0.01)
	require.InDelta(t, 2.0, report.JDR, 0.01)
	require.InDelta(t, 0.0, report.BLD, 0.01)
	require.InDelta(t, 2.0, report.GLD, 0.01)
	require.InDelta(t, 200.0, report.RTD, 0.01)
	require.InDelta(t, 140.0, report.ESD, 0.01)
	require.InDelta(t, 2.0, report.IAJ, 0.01)
	require.InDelta(t, 10.0, report.MAJ, 0.01)
	require.InDelta(t, 55.0, report.RERL, 0.01)
	require.InDelta(t, 88.0, report.RLQ, 0.01)
	require.InDelta(t, 85.0, report.RCQ, 0.01)
	require.InDelta(t, 4.1, report.MOSLQ, 0.01)
	require.InDelta(t, 4.0, report.MOSCQ, 0.01)

	expectedPresent := map[string]bool{
		"NLR": true, "JDR": true, "BLD": true, "GLD": true,
		"RTD": true, "ESD": true, "IAJ": true, "MAJ": true,
		"MOSLQ": true, "MOSCQ": true, "RLQ": true, "RCQ": true, "RERL": true,
	}
	require.Equal(t, expectedPresent, report.Present)
}

func TestParseReport_RemoteMetricsSkipped(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\r\n" +
		"LocalMetrics:\r\n" +
		"NLR=5.0 MOSLQ=4.1\r\n" +
		"RemoteMetrics:\r\n" +
		"NLR=3.0 MOSLQ=4.5\r\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.NotNil(t, report)

	require.InDelta(t, 5.0, report.NLR, 0.01)
	require.InDelta(t, 4.1, report.MOSLQ, 0.01)
	require.Len(t, report.Present, 2)
	require.True(t, report.Present["NLR"])
	require.True(t, report.Present["MOSLQ"])
}

func TestParseReport_RemoteMetricsNoLocal(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\r\n" +
		"RemoteMetrics:\r\n" +
		"NLR=3.0 MOSLQ=4.5\r\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.NotNil(t, report)
	require.Empty(t, report.Present)
}

func TestParseReport_LocalMetricsAfterRemote(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\r\n" +
		"LocalMetrics:\r\n" +
		"NLR=5.0\r\n" +
		"RemoteMetrics:\r\n" +
		"NLR=3.0\r\n" +
		"\r\n" +
		"MOSLQ=4.2\r\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.InDelta(t, 5.0, report.NLR, 0.01)
	require.InDelta(t, 4.2, report.MOSLQ, 0.01)
	require.True(t, report.Present["NLR"])
	require.True(t, report.Present["MOSLQ"])
}

func TestParseReport_InvalidPrefix(t *testing.T) {
	_, err := ParseReport([]byte("XVQSessionReport: CallTerm\nNLR=1.0\n"))
	require.ErrorIs(t, err, ErrInvalidReport)
}

func TestParseReport_FlatFormatNoRemoteMetrics(t *testing.T) {
	body := []byte("VQSessionReport: CallTerm\n" +
		"NLR=0.50 JDR=1.20\n" +
		"MOSLQ=4.5\n")
	report, err := ParseReport(body)
	require.NoError(t, err)
	require.InDelta(t, 0.50, report.NLR, 0.01)
	require.InDelta(t, 1.20, report.JDR, 0.01)
	require.InDelta(t, 4.5, report.MOSLQ, 0.01)
	require.Len(t, report.Present, 3)
}
