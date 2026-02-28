package exporter

import (
	"github.com/stretchr/testify/require"
	"gitlab.com/sip-exporter/internal/dto"
	"testing"
)

func TestNormalizeDialogID(t *testing.T) {
	tt := []struct {
		expected    string
		callID      []byte
		fromTag     []byte
		toTag       []byte
		description string
	}{
		{
			description: "positive",
			expected:    "583ce713cb324f27bd614e594db53cc2:8Xy7r28Ne5ZSQ:e2540aafe5474bd7a5f9059b0ffccfec",
			callID:      []byte("583ce713cb324f27bd614e594db53cc2"),
			fromTag:     []byte("e2540aafe5474bd7a5f9059b0ffccfec"),
			toTag:       []byte("8Xy7r28Ne5ZSQ"),
		},
	}

	for _, v := range tt {
		t.Run(v.description, func(t *testing.T) {
			actual, err := normalizeDialogID(v.callID, v.fromTag, v.toTag)
			require.NoError(t, err)
			require.Equal(t, v.expected, actual)
		})
	}

}

func TestSIPPacketParse(t *testing.T) {
	tt := []struct {
		expectedData dto.Packet
		expectedErr  bool
		description  string
		input        []byte
	}{
		{
			input: []byte("SIP/2.0 401 Unauthorized\r\n" +
				"Via: SIP/2.0/UDP 192.168.0.89:55147;rport=55147;branch=z9hG4bKPjda81fdbda2a5464898d03d02ed894a2d\r\n"),
			//"From: <sip:1000@192.168.0.89>;tag=f91725d523814ead93f3503cbe665f2f\r\n" +
			//"To: <sip:1000@192.168.0.89>;tag=r9cpmKK0870HS\r\n" +
			//"Call-ID: c2a9e17343284b48a3863c5281688c5b\r\n" +
			//"CSeq: 33849 REGISTER\r\n" +
			//"User-Agent: FreeSWITCH-mod_sofia/1.10.9-release-21-a615e85afc~64bit\r\n" +
			//"Allow: INVITE, ACK, BYE, CANCEL, OPTIONS, MESSAGE, INFO, UPDATE, REGISTER, REFER, NOTIFY, PUBLISH, SUBSCRIBE\r\n"),
			description: "Unauthorized positive",
			expectedData: dto.Packet{
				IsResponse:     true,
				ResponseStatus: []byte("401"),
			},
		},
		{
			input: []byte("INVITE sip:1001@192.168.0.89 SIP/2.0\r\n" +
				"Via: SIP/2.0/UDP 192.168.0.89:49375;rport;branch=z9hG4bKPjdad03fa8a00c49fb9b08469cc8c2215b\r\n" +
				"Max-Forwards: 70\r\n" +
				"From: <sip:1000@192.168.0.89>;tag=21e4850e69de4f50a3f96a8051e1af35\r\n" +
				"To: <sip:1001@192.168.0.89>\r\n" +
				"Contact: <sip:1000@192.168.0.89:49375;ob>\r\n" +
				"Call-ID: 618e627cb7eb4275a9addb1c6b639656\r\n" +
				"CSeq: 9217 INVITE\r\n"),
			description: "INVITE positive",
			expectedData: dto.Packet{
				Method: []byte("INVITE"),
				From: dto.From{
					Tag: []byte("21e4850e69de4f50a3f96a8051e1af35"),
				},
				CallID: []byte("618e627cb7eb4275a9addb1c6b639656"),
				CSeq: dto.CSeq{
					ID:     []byte("9217"),
					Method: []byte("INVITE"),
				},
			},
		},
	}

	for _, v := range tt {
		t.Run(v.description, func(t *testing.T) {
			e := exporter{}
			actual, err := e.sipPacketParse(v.input)
			require.NoError(t, err)
			require.Equal(t, v.expectedData, actual)
		})
	}

}

func TestParseResponsesPacket(t *testing.T) {
	tt := []struct {
		expected    dto.Packet
		expectedErr bool
		description string
		input       []byte
	}{
		{
			input: []byte("SIP/2.0 401 Unauthorized\r\n" +
				"Via: SIP/2.0/UDP 192.168.0.89:49375;rport=49375;branch=z9hG4bKPjbce993f574bb40a9919447d899e324fa\r\n" +
				"From: <sip:1000@192.168.0.89>;tag=e2540aafe5474bd7a5f9059b0ffccfec\r\n" +
				"To: <sip:1000@192.168.0.89>;tag=8Xy7r28Ne5ZSQ\r\n" +
				"Call-ID: 583ce713cb324f27bd614e594db53cc2\r\n" +
				"CSeq: 6596 REGISTER\r\n" +
				"User-Agent: MicroSIP/3.22.3\r\n",
			),
			description: "401 positive",
			expected: dto.Packet{
				IsResponse:     true,
				ResponseStatus: []byte("401"),
				From: dto.From{
					Tag: []byte("e2540aafe5474bd7a5f9059b0ffccfec"),
				},
				To: dto.To{
					Tag: []byte("8Xy7r28Ne5ZSQ"),
				},
				CallID: []byte("583ce713cb324f27bd614e594db53cc2"),
				CSeq: dto.CSeq{
					ID:     []byte("6596"),
					Method: []byte("REGISTER"),
				},
			},
		},
		{
			input: []byte("SIP/2.0 200 OK\r\n" +
				"Via: SIP/2.0/UDP 192.168.0.89:49375;rport=49375;branch=z9hG4bKPjbce993f574bb40a9919447d899e324fa\r\n" +
				"From: <sip:1000@192.168.0.89>;tag=e2540aafe5474bd7a5f9059b0ffccfec\r\n" +
				"To: <sip:1000@192.168.0.89>;tag=8Xy7r28Ne5ZSQ\r\n" +
				"Call-ID: 583ce713cb324f27bd614e594db53cc2\r\n" +
				"CSeq: 6596 INVITE\r\n" +
				"User-Agent: MicroSIP/3.22.3\r\n",
			),
			description: "200 positive",
			expected: dto.Packet{
				IsResponse:     true,
				ResponseStatus: []byte("200"),
				From: dto.From{
					Tag: []byte("e2540aafe5474bd7a5f9059b0ffccfec"),
				},
				To: dto.To{
					Tag: []byte("8Xy7r28Ne5ZSQ"),
				},
				CallID: []byte("583ce713cb324f27bd614e594db53cc2"),
				CSeq: dto.CSeq{
					ID:     []byte("6596"),
					Method: []byte("INVITE"),
				},
			},
		},
	}

	for _, v := range tt {
		t.Run(v.description, func(t *testing.T) {
			e := exporter{}
			actual, err := e.sipPacketParse(v.input)
			require.NoError(t, err)
			require.Equal(t, v.expected, actual)
		})
	}
}

func TestParseRegisterPacket(t *testing.T) {
	tt := []struct {
		expected    dto.Packet
		expectedErr bool
		description string
		input       []byte
	}{
		{
			input: []byte("REGISTER sip:192.168.0.89:5060 SIP/2.0\r\n" +
				"Via: SIP/2.0/UDP 192.168.0.89:49375;rport;branch=z9hG4bKPjbce993f574bb40a9919447d899e324fa\r\n" +
				"Max-Forwards: 70\r\n" +
				"From: <sip:1000@192.168.0.89>;tag=e2540aafe5474bd7a5f9059b0ffccfec\r\n" +
				"To: <sip:1000@192.168.0.89>\r\n" +
				"Call-ID: 583ce713cb324f27bd614e594db53cc2\r\n" +
				"CSeq: 6596 REGISTER\r\n" +
				"User-Agent: MicroSIP/3.22.3\r\n"),
			description: "REGISTER positive",
			expected: dto.Packet{
				Method: []byte("REGISTER"),
				From: dto.From{
					Tag: []byte("e2540aafe5474bd7a5f9059b0ffccfec"),
				},
				CallID: []byte("583ce713cb324f27bd614e594db53cc2"),
				CSeq: dto.CSeq{
					ID:     []byte("6596"),
					Method: []byte("REGISTER"),
				},
			},
		},
	}

	for _, v := range tt {
		t.Run(v.description, func(t *testing.T) {
			e := exporter{}
			actual, err := e.sipPacketParse(v.input)
			require.NoError(t, err)
			require.Equal(t, v.expected, actual)
		})
	}
}
