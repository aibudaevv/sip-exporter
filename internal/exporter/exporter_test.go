package exporter

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestHandleInvite(t *testing.T) {
	tt := []struct {
		expectedName string
		expectedErr  bool
		description  string
		input        []byte
	}{
		{
			input: []byte(`
							INVITE sip:1001@192.168.0.89 SIP/2.0
							Via: SIP/2.0/UDP 192.168.0.89:49375;rport;branch=z9hG4bKPjdad03fa8a00c49fb9b08469cc8c2215b
							Max-Forwards: 70
							From: <sip:1000@192.168.0.89>;tag=21e4850e69de4f50a3f96a8051e1af35
							To: <sip:1001@192.168.0.89>
							Contact: <sip:1000@192.168.0.89:49375;ob>
							Call-ID: 618e627cb7eb4275a9addb1c6b639656
							CSeq: 9217 INVITE`),
			description:  "INVITE positive",
			expectedName: "REGISTER",
			expectedErr:  false,
		},
	}

	for _, v := range tt {
		t.Run(v.description, func(t *testing.T) {
			e := exporter{}
			actual := e.handleInvite(v.input)
			t.Log(actual)
		})
	}

}

func TestGetMethodName(t *testing.T) {
	tt := []struct {
		expectedName string
		expectedErr  bool
		description  string
		input        []byte
	}{
		{
			input:        []byte(`REGISTER sip:127.0.0.1:5060 SIP/2.0`),
			description:  "REGISTER positive",
			expectedName: "REGISTER",
			expectedErr:  false,
		},
		{
			input:        []byte(`SIP/2.0 200 OK`),
			description:  "200 OK positive",
			expectedName: "200",
			expectedErr:  false,
		},
	}

	for _, v := range tt {
		t.Run(v.description, func(t *testing.T) {
			e := exporter{}
			actual := e.getMethodOrStatus(v.input)
			require.Equal(t, v.expectedName, string(actual))
		})
	}
}
