package vq

// SessionReport holds metrics extracted from a VQ-RTCPXR session block.
type SessionReport struct {
	NLR   float64
	JDR   float64
	BLD   float64
	GLD   float64
	RTD   float64
	ESD   float64
	IAJ   float64
	MAJ   float64
	MOSLQ float64
	MOSCQ float64
	RLQ   float64
	RCQ   float64
	RERL  float64

	Present map[string]bool
}
