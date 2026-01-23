package exporter

import (
	"github.com/stretchr/testify/require"
	"testing"
)

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
