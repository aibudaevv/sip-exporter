package mediatracker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewBinaryEndpointKey(t *testing.T) {
	tests := []struct {
		name       string
		ip         string
		port       uint16
		want       binaryEndpointKey
		wantString string
	}{
		{
			name:       "valid IPv4",
			ip:         "192.0.2.1",
			port:       5004,
			want:       binaryEndpointKey{ipv4: [4]byte{192, 0, 2, 1}, port: 5004, isIPv4: true},
			wantString: "192.0.2.1",
		},
		{
			name:       "zero IPv4 and port",
			ip:         "0.0.0.0",
			want:       binaryEndpointKey{ipv4: [4]byte{}, isIPv4: true},
			wantString: "0.0.0.0",
		},
		{
			name:       "maximum IPv4 and port",
			ip:         "255.255.255.255",
			port:       65535,
			want:       binaryEndpointKey{ipv4: [4]byte{255, 255, 255, 255}, port: 65535, isIPv4: true},
			wantString: "255.255.255.255",
		},
		{
			name:       "IPv6 uses fallback",
			ip:         "2001:db8::1",
			port:       5004,
			want:       binaryEndpointKey{fallbackIP: "2001:db8::1", port: 5004},
			wantString: "2001:db8::1",
		},
		{
			name:       "invalid IP uses fallback",
			ip:         "not-an-ip",
			port:       5004,
			want:       binaryEndpointKey{fallbackIP: "not-an-ip", port: 5004},
			wantString: "not-an-ip",
		},
		{
			name: "empty IP uses fallback",
			want: binaryEndpointKey{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newBinaryEndpointKey(tt.ip, tt.port)

			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantString, got.ipString())
		})
	}
}

func TestBinaryEndpointKeyEquality(t *testing.T) {
	validFirst := newBinaryEndpointKey("192.0.2.1", 5004)
	validSecond := newBinaryEndpointKey("192.0.2.1", 5004)
	invalidFirst := newBinaryEndpointKey("not-an-ip", 5004)
	invalidSecond := newBinaryEndpointKey("not-an-ip", 5004)
	keys := map[binaryEndpointKey]struct{}{validFirst: {}, invalidFirst: {}}

	require.Contains(t, keys, validSecond)
	require.Contains(t, keys, invalidSecond)
	require.NotEqual(t, validFirst, newBinaryEndpointKey("192.0.2.2", 5004))
	require.NotEqual(t, validFirst, newBinaryEndpointKey("192.0.2.1", 5005))
	require.NotEqual(t, invalidFirst, newBinaryEndpointKey("another-invalid-ip", 5004))
	require.NotEqual(t, newBinaryEndpointKey("0.0.0.0", 0), newBinaryEndpointKey("", 0))
}
