package rtp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodecName_StaticPT(t *testing.T) {
	tests := []struct {
		name string
		pt   uint8
		want string
	}{
		{"PCMU G.711u", 0, "PCMU"},
		{"PCMA G.711a", 8, "PCMA"},
		{"G.722", 9, "G.722"},
		{"G.729", 18, "G.729"},
		{"G.723", 4, "G.723"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, CodecName(tc.pt, nil))
		})
	}
}

func TestCodecName_ReservedPT2_NotG726(t *testing.T) {
	// RFC 3551 Table 4: PT 2 is reserved (was G.721, now unused).
	// G.726 has no static PT — it must use dynamic PT via SDP a=rtpmap.
	require.Equal(t, CodecUnknown, CodecName(2, nil),
		"PT 2 is reserved, not G.726-32")
}

func TestCodecName_DynamicPT_FromSDP(t *testing.T) {
	sdp := map[uint8]string{
		96:  "opus",
		101: "telephone-event",
		111: "opus",
	}
	require.Equal(t, "opus", CodecName(96, sdp))
	require.Equal(t, "telephone-event", CodecName(101, sdp))
	require.Equal(t, "opus", CodecName(111, sdp))
}

func TestCodecName_SDPTakesPrecedenceOverStatic(t *testing.T) {
	// PT 0 is statically PCMU, but SDP may override (rare but valid)
	sdp := map[uint8]string{0: "custom-codec"}
	require.Equal(t, "custom-codec", CodecName(0, sdp))
}

func TestCodecName_UnknownPT(t *testing.T) {
	require.Equal(t, CodecUnknown, CodecName(127, nil))
	require.Equal(t, CodecUnknown, CodecName(127, map[uint8]string{}))
}

func TestCodecName_EmptySDPValue_FallsBack(t *testing.T) {
	// Empty string in SDP map for a PT should fall back to static table
	sdp := map[uint8]string{0: ""}
	require.Equal(t, "PCMU", CodecName(0, sdp))
}
