package mediatracker

// E-model (ITU-T G.107) parameters for MOS-LQ estimation.
// Simplified for passive monitoring: R = R0 - Ie_eff, where Ie_eff accounts for
// codec impairment and packet loss (incl. jitter-induced discard). No echo/noise/RTT.
const (
	// R-factor base and bounds (G.107).
	r0Factor   = 93.2 // simplified base R-factor (no echo, no room noise)
	rFactorMax = 100.0
	mosBase    = 1.0
	mosMin     = 1.0
	mosMax     = 4.5
	// G.107 §3.5 R→MOS transform coefficients.
	mosACoeff = 0.035
	mosBCoeff = 0.000007
	mosHinge  = 60.0
	// G.107 ieEff scale constant.
	ieScale = 95.0
	// Jitter-buffer assumption for discard modelling.
	jbMsDefault = 60.0
	jbMsF1      = 50.0  // strict jitter buffer (low-latency)
	jbMsF2      = 200.0 // generous jitter buffer (managed)
	jbMsAdapt   = 500.0 // adaptive jitter buffer (large)
	discardCap  = 0.5
	// Codec impairment defaults for unknown codecs.
	ieDefault  = 10.0
	bplDefault = 10.0
	// ITU-T G.113 codec impairment factors (equipment impairment Ie and
	// packet-loss robustness Bpl).
	ieNone  = 0.0  // toll-quality codecs: G.711, G.722, Opus
	ieG726  = 7.0  // G.726-32
	ieLow   = 11.0 // G.728, G.729
	ieG723  = 15.0 // G.723.1
	bplLow  = 10.0 // robustness for toll-quality codecs
	bplHigh = 19.0 // robustness for low-bitrate codecs
)

// CodecParams holds codec-specific E-model impairment factors (ITU-T G.113).
type CodecParams struct {
	Ie  float64 // equipment impairment factor (no loss)
	Bpl float64 // packet-loss robustness factor
}

// codecParams returns the G.113 E-model parameters for a codec (named via
// rtp.CodecName). Unknown codecs get a conservative default.
func codecParams(codec string) CodecParams {
	switch codec {
	case "PCMU", "PCMA", "G.722", "opus":
		return CodecParams{Ie: ieNone, Bpl: bplLow}
	case "G.726-32":
		return CodecParams{Ie: ieG726, Bpl: bplHigh}
	case "G.723":
		return CodecParams{Ie: ieG723, Bpl: bplHigh}
	case "G.728", "G.729":
		return CodecParams{Ie: ieLow, Bpl: bplHigh}
	default:
		return CodecParams{Ie: ieDefault, Bpl: bplDefault}
	}
}

// mosFromR converts an R-factor to MOS via the G.107 §3.5 transform.
func mosFromR(r float64) float64 {
	switch {
	case r < 0:
		return mosMin
	case r > rFactorMax:
		return mosMax
	default:
		return mosBase + mosACoeff*r + mosBCoeff*r*(r-mosHinge)*(rFactorMax-r)
	}
}

// ieEff returns the effective equipment impairment for a given loss rate (fraction 0..1).
// Formula: Ie + (ieScale - Ie) * lossRate / (lossRate/Bpl + 1)  (G.107).
func ieEff(p CodecParams, lossRate float64) float64 {
	if lossRate < 0 {
		lossRate = 0
	}
	return p.Ie + (ieScale-p.Ie)*lossRate/(lossRate/p.Bpl+1)
}

// jitterDiscardRate models packets discarded by the jitter buffer as equivalent loss.
// Jitter up to jbMs is absorbed; beyond that, a growing fraction is discarded (capped).
func jitterDiscardRate(jitterMs, jbMs float64) float64 {
	if jitterMs <= jbMs {
		return 0
	}
	r := (jitterMs - jbMs) / jbMs
	if r > discardCap {
		r = discardCap
	}
	return r
}

// computeRFactorWithJB estimates the E-model R-factor with a specific jitter-buffer
// assumption (jbMs). R ∈ [0, 100].
func computeRFactorWithJB(codec string, lossRate, jitterMs, jbMs float64) float64 {
	p := codecParams(codec)
	effLoss := lossRate + jitterDiscardRate(jitterMs, jbMs)
	if effLoss > 1 {
		effLoss = 1
	} else if effLoss < 0 {
		effLoss = 0
	}
	r := r0Factor - ieEff(p, effLoss)
	if r < 0 {
		r = 0
	} else if r > rFactorMax {
		r = rFactorMax
	}
	return r
}

// ComputeRFactor estimates the E-model R-factor (ITU-T G.107) from codec,
// measured loss rate (fraction 0..1) and jitter (ms). R ∈ [0, 100].
func ComputeRFactor(codec string, lossRate, jitterMs float64) float64 {
	return computeRFactorWithJB(codec, lossRate, jitterMs, jbMsDefault)
}

// ComputeMOS estimates MOS-LQ from codec, measured loss rate (fraction) and jitter (ms).
func ComputeMOS(codec string, lossRate, jitterMs float64) float64 {
	return mosFromR(ComputeRFactor(codec, lossRate, jitterMs))
}

// ComputeMOSF1 estimates MOS-LQ with a strict jitter buffer (jbMs=50ms).
func ComputeMOSF1(codec string, lossRate, jitterMs float64) float64 {
	return mosFromR(computeRFactorWithJB(codec, lossRate, jitterMs, jbMsF1))
}

// ComputeMOSF2 estimates MOS-LQ with a generous jitter buffer (jbMs=200ms).
func ComputeMOSF2(codec string, lossRate, jitterMs float64) float64 {
	return mosFromR(computeRFactorWithJB(codec, lossRate, jitterMs, jbMsF2))
}

// ComputeMOSAdaptive estimates MOS-LQ with an adaptive jitter buffer (jbMs=500ms).
func ComputeMOSAdaptive(codec string, lossRate, jitterMs float64) float64 {
	return mosFromR(computeRFactorWithJB(codec, lossRate, jitterMs, jbMsAdapt))
}
