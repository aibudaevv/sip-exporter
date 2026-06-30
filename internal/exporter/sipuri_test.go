package exporter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ==================== ParseURI tests ====================

func TestParseURI_SIPSchemeWithPortAndParams(t *testing.T) {
	user, host := ParseURI([]byte(`<sip:bob@example.com:5060;tag=x>`))
	require.Equal(t, []byte("bob"), user)
	require.Equal(t, []byte("example.com"), host)
}

func TestParseURI_DisplayNameWithBrackets(t *testing.T) {
	user, host := ParseURI([]byte(`"Bob" <sip:bob@example.com:5060>;tag=abc`))
	require.Equal(t, []byte("bob"), user)
	require.Equal(t, []byte("example.com"), host)
}

func TestParseURI_QuotedUserNoBrackets(t *testing.T) {
	user, host := ParseURI([]byte(`"+74951234567"@10.0.0.1`))
	require.Equal(t, []byte("+74951234567"), user)
	require.Equal(t, []byte("10.0.0.1"), host)
}

func TestParseURI_AnonymousNoScheme(t *testing.T) {
	user, host := ParseURI([]byte(`anonymous@anonymous.invalid`))
	require.Equal(t, []byte("anonymous"), user)
	require.Equal(t, []byte("anonymous.invalid"), host)
}

func TestParseURI_IPv6Host(t *testing.T) {
	user, host := ParseURI([]byte(`<sip:bob@[2001:db8::1]:5060>`))
	require.Equal(t, []byte("bob"), user)
	require.Equal(t, []byte("2001:db8::1"), host)
}

func TestParseURI_HostOnly(t *testing.T) {
	user, host := ParseURI([]byte(`<sip:example.com:5060;transport=udp>`))
	require.Empty(t, user)
	require.Equal(t, []byte("example.com"), host)
}

func TestParseURI_NoBracketsWithParams(t *testing.T) {
	user, host := ParseURI([]byte(`sip:bob@example.com;tag=abc`))
	require.Equal(t, []byte("bob"), user)
	require.Equal(t, []byte("example.com"), host)
}

func TestParseURI_SIPSScheme(t *testing.T) {
	user, host := ParseURI([]byte(`<sips:bob@example.com:5061>`))
	require.Equal(t, []byte("bob"), user)
	require.Equal(t, []byte("example.com"), host)
}

func TestParseURI_SIPSchemeCaseInsensitive(t *testing.T) {
	user, host := ParseURI([]byte(`<SIP:bob@example.com>`))
	require.Equal(t, []byte("bob"), user)
	require.Equal(t, []byte("example.com"), host)
}

func TestParseURI_UserWithPassword(t *testing.T) {
	user, host := ParseURI([]byte(`<sip:bob:secret@example.com>`))
	require.Equal(t, []byte("bob"), user)
	require.Equal(t, []byte("example.com"), host)
}

func TestParseURI_UserWithParams(t *testing.T) {
	user, host := ParseURI([]byte(`<sip:bob;phone-context=+7@example.com>`))
	require.Equal(t, []byte("bob"), user)
	require.Equal(t, []byte("example.com"), host)
}

func TestParseURI_IPAddress(t *testing.T) {
	user, host := ParseURI([]byte(`<sip:1000@192.168.0.89>`))
	require.Equal(t, []byte("1000"), user)
	require.Equal(t, []byte("192.168.0.89"), host)
}

func TestParseURI_NoPort(t *testing.T) {
	user, host := ParseURI([]byte(`<sip:bob@example.com>`))
	require.Equal(t, []byte("bob"), user)
	require.Equal(t, []byte("example.com"), host)
}

func TestParseURI_EmptyInput(t *testing.T) {
	user, host := ParseURI([]byte(``))
	require.Empty(t, user)
	require.Empty(t, host)
}

func TestParseURI_NoUserPart(t *testing.T) {
	user, host := ParseURI([]byte(`<sip:example.com>`))
	require.Empty(t, user)
	require.Equal(t, []byte("example.com"), host)
}

func TestParseURI_RealWorldFromHeader(t *testing.T) {
	user, host := ParseURI([]byte(`<sip:1000@192.168.0.89>;tag=e2540aafe5474bd7a5f9059b0ffccfec`))
	require.Equal(t, []byte("1000"), user)
	require.Equal(t, []byte("192.168.0.89"), host)
}

// ==================== parseHeaders integration ====================

func TestSIPPacketParse_FromToUserHost(t *testing.T) {
	e := exporter{}

	input := []byte("INVITE sip:1001@192.168.0.89 SIP/2.0\r\n" +
		"From: <sip:1000@192.168.0.89>;tag=abc\r\n" +
		"To: <sip:1001@192.168.0.89>;tag=xyz\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 INVITE\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.Equal(t, []byte("1000"), p.From.User)
	require.Equal(t, []byte("192.168.0.89"), p.From.Addr)
	require.Equal(t, []byte("1001"), p.To.User)
	require.Equal(t, []byte("192.168.0.89"), p.To.Addr)
}

func TestSIPPacketParse_FromToQuotedNumber(t *testing.T) {
	e := exporter{}

	input := []byte("INVITE sip:test SIP/2.0\r\n" +
		`From: "+74951234567"@10.0.0.1;tag=abc` + "\r\n" +
		`To: "+78121234567"@10.0.0.2;tag=xyz` + "\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 INVITE\r\n")

	p, err := e.sipPacketParse(input)
	require.NoError(t, err)
	require.Equal(t, []byte("+74951234567"), p.From.User)
	require.Equal(t, []byte("10.0.0.1"), p.From.Addr)
	require.Equal(t, []byte("+78121234567"), p.To.User)
	require.Equal(t, []byte("10.0.0.2"), p.To.Addr)
}
