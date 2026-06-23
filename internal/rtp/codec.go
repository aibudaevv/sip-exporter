package rtp

// CodecUnknown is the label used when payload type cannot be resolved to a codec.
const CodecUnknown = "other"

// staticCodecs maps static RTP payload types (RFC 3551) to codec names.
// Dynamic payload types (96-127) must be resolved via SDP a=rtpmap.
var staticCodecs = map[uint8]string{
	0:  "PCMU", // G.711 µ-law
	8:  "PCMA", // G.711 A-law
	9:  "G.722",
	2:  "G.726-32",
	4:  "G.723",
	15: "G.728",
	18: "G.729",
	13: "CN", // Comfort Noise (RFC 3389)
}

// CodecName resolves an RTP payload type to a codec name.
// SDP a=rtpmap mapping (sdpMap) takes precedence for both static and dynamic PTs;
// static PTs fall back to the built-in table; unknown PTs yield CodecUnknown.
func CodecName(pt uint8, sdpMap map[uint8]string) string {
	if sdpMap != nil {
		if name, ok := sdpMap[pt]; ok && name != "" {
			return name
		}
	}
	if name, ok := staticCodecs[pt]; ok {
		return name
	}
	return CodecUnknown
}
