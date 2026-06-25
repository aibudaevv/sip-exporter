package rtp

// CodecUnknown is the label used when payload type cannot be resolved to a codec.
const CodecUnknown = "other"

// Static RTP payload types (RFC 3551, Table 4).
const (
	ptPCMU = 0  // G.711 µ-law
	ptG723 = 4  // G.723.1
	ptPCMA = 8  // G.711 A-law
	ptG722 = 9  // G.722
	ptCN   = 13 // Comfort Noise (RFC 3389)
	ptG728 = 15 // G.728
	ptG729 = 18 // G.729
)

// staticCodecName resolves a static RTP payload type (RFC 3551) to a codec name.
// Returns CodecUnknown for dynamic payload types (96-127), which must be resolved
// from SDP a=rtpmap.
func staticCodecName(pt uint8) string {
	switch pt {
	case ptPCMU:
		return "PCMU"
	case ptPCMA:
		return "PCMA"
	case ptG722:
		return "G.722"
	case ptG723:
		return "G.723"
	case ptG728:
		return "G.728"
	case ptG729:
		return "G.729"
	case ptCN:
		return "CN"
	default:
		return CodecUnknown
	}
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
	return staticCodecName(pt)
}
