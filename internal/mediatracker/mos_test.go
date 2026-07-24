package mediatracker

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMOSFromR(t *testing.T) {
	require.InDelta(t, 1.0, mosFromR(-10), 0.0001) // R<0 → 1.0
	require.InDelta(t, 1.0, mosFromR(0), 0.0001)   // R=0 → 1.0
	require.InDelta(t, 4.5, mosFromR(100), 0.0001) // R=100 → 4.5
	require.InDelta(t, 4.5, mosFromR(150), 0.0001) // R>100 → 4.5
	// mid-range is within [1,4.5]
	mid := mosFromR(50)
	require.True(t, mid > 1.0 && mid < 4.5)
}

func TestComputeMOS_PCMU_Clean(t *testing.T) {
	// G.711, no loss/jitter → R=93.2 → MOS≈4.41
	mos := ComputeMOS("PCMU", 0, 0)
	require.InDelta(t, 4.41, mos, 0.02)
}

func TestComputeMOS_PCMU_LossReducesMOS(t *testing.T) {
	clean := ComputeMOS("PCMU", 0, 0)
	degraded := ComputeMOS("PCMU", 0.05, 0) // 5% loss
	require.Less(t, degraded, clean)
	// R = 93.2 - ie_eff(0.05) ≈ 88.47 → MOS ≈ 4.30
	require.InDelta(t, 4.30, degraded, 0.02)
}

func TestComputeMOS_PCMU_HighJitterReducesMOS(t *testing.T) {
	// jitter 150ms > JB 60ms → discard rate capped at 0.5 → effective loss 0.5
	mos := ComputeMOS("PCMU", 0, 150)
	require.Less(t, mos, 3.0, "high jitter must degrade MOS significantly")
	require.InDelta(t, 2.47, mos, 0.03)
}

func TestComputeMOS_PCMU_LowJitterNoDiscard(t *testing.T) {
	// jitter below JB threshold (60ms) → no extra discard
	mos := ComputeMOS("PCMU", 0, 30)
	clean := ComputeMOS("PCMU", 0, 0)
	require.InDelta(t, clean, mos, 0.0001)
}

func TestComputeMOS_G729LowerThanPCMU(t *testing.T) {
	pcmu := ComputeMOS("PCMU", 0, 0)
	g729 := ComputeMOS("G.729", 0, 0)
	require.Less(t, g729, pcmu, "G.729 must score lower than G.711")
}

func TestComputeMOS_UnknownCodec(t *testing.T) {
	mos := ComputeMOS("other", 0, 0)
	require.True(t, mos >= 1.0 && mos <= 4.5)
	pcmu := ComputeMOS("PCMU", 0, 0)
	require.Less(t, mos, pcmu, "unknown codec (default Ie=10) must score lower than G.711")
}

func TestComputeMOS_LossClampedNegative(t *testing.T) {
	// negative loss treated as 0 (no NaN / nonsense)
	mos := ComputeMOS("PCMU", -0.5, 0)
	require.False(t, math.IsNaN(mos))
	require.InDelta(t, ComputeMOS("PCMU", 0, 0), mos, 0.0001)
}

func TestJitterDiscardRate_UncappedRegion(t *testing.T) {
	// jitter 75ms → (75-60)/60 = 0.25 — within uncapped region (0 < r < 0.5)
	r := jitterDiscardRate(75, jbMsDefault)
	require.InDelta(t, 0.25, r, 0.001)
	// jitter exactly at JB threshold → 0
	require.InDelta(t, 0.0, jitterDiscardRate(60, jbMsDefault), 0.0001)
	// jitter above cap → capped at 0.5
	require.InDelta(t, 0.5, jitterDiscardRate(150, jbMsDefault), 0.0001)
}

func TestComputeMOS_G726_LowerThanPCMU(t *testing.T) {
	pcmu := ComputeMOS("PCMU", 0, 0)
	g726 := ComputeMOS("G.726-32", 0, 0)
	require.Less(t, g726, pcmu, "G.726-32 (Ie=7) must score lower than G.711 (Ie=0)")
}

func TestComputeMOS_G723_LowerThanG726(t *testing.T) {
	g726 := ComputeMOS("G.726-32", 0, 0)
	g723 := ComputeMOS("G.723", 0, 0)
	require.Less(t, g723, g726, "G.723 (Ie=15) must score lower than G.726 (Ie=7)")
}

func TestComputeMOS_HighLossClampsEffLoss(t *testing.T) {
	// loss=0.6 + jitter-discard=0.5 → effLoss=1.1 → clamped to 1.0
	// Without clamp: effLoss=1.1 would produce negative R → MOS < 1.0
	// With clamp: effLoss=1.0 → R = 93.2 - ieEff(1.0) ≈ low but ≥ 1.0
	mos := ComputeMOS("PCMU", 0.6, 150)
	require.GreaterOrEqual(t, mos, 1.0, "effLoss>1 must clamp, MOS must not go below 1.0")
}

func TestComputeRFactor_PCMU_Clean(t *testing.T) {
	r := ComputeRFactor("PCMU", 0, 0)
	require.InDelta(t, 93.2, r, 0.01)
}

func TestComputeRFactor_LossReducesR(t *testing.T) {
	clean := ComputeRFactor("PCMU", 0, 0)
	degraded := ComputeRFactor("PCMU", 0.05, 0)
	require.Less(t, degraded, clean)
}

func TestComputeRFactor_HighLossClamped(t *testing.T) {
	r := ComputeRFactor("PCMU", 0.6, 150)
	require.GreaterOrEqual(t, r, 0.0)
	require.LessOrEqual(t, r, 100.0)
}

func TestComputeRFactor_MOSConsistency(t *testing.T) {
	// MOS computed from ComputeMOS must match mosFromR(ComputeRFactor)
	r := ComputeRFactor("PCMU", 0.05, 30)
	mos := ComputeMOS("PCMU", 0.05, 30)
	require.InDelta(t, mos, mosFromR(r), 0.0001)
}

func TestJitterDiscardRate_Parameterized(t *testing.T) {
	// jbMs=50: jitter 75ms → (75-50)/50 = 0.5 (at cap)
	require.InDelta(t, 0.5, jitterDiscardRate(75, 50), 0.001)
	// jbMs=200: jitter 75ms ≤ 200 → 0 discard
	require.InDelta(t, 0.0, jitterDiscardRate(75, 200), 0.0001)
	// jbMs=500: jitter 300ms ≤ 500 → 0 discard
	require.InDelta(t, 0.0, jitterDiscardRate(300, 500), 0.0001)
	// jbMs=500: jitter 750ms → (750-500)/500 = 0.5 (at cap)
	require.InDelta(t, 0.5, jitterDiscardRate(750, 500), 0.001)
}

func TestComputeMOSF1_StricterJBLowerMOS(t *testing.T) {
	// F1 jbMs=50 is stricter than default 60 → more discard → lower MOS
	// jitter=70ms: F1 discard=(70-50)/50=0.4, default=(70-60)/60≈0.167
	base := ComputeMOS("PCMU", 0, 70)
	f1 := ComputeMOSF1("PCMU", 0, 70)
	require.Less(t, f1, base, "F1 (jb=50) must degrade more than default (jb=60) under jitter")
}

func TestComputeMOSF2_GenerousJBHigherMOS(t *testing.T) {
	// F2 jbMs=200 absorbs more jitter → higher MOS than default
	base := ComputeMOS("PCMU", 0, 150)
	f2 := ComputeMOSF2("PCMU", 0, 150)
	require.Greater(t, f2, base, "F2 (jb=200) must degrade less than default (jb=60) under jitter")
}

func TestComputeMOSAdaptive_MostGenerousJB(t *testing.T) {
	// Adaptive jbMs=500 absorbs most jitter → highest MOS
	f2 := ComputeMOSF2("PCMU", 0, 150)
	adapt := ComputeMOSAdaptive("PCMU", 0, 150)
	require.GreaterOrEqual(t, adapt, f2, "Adaptive (jb=500) must be ≥ F2 (jb=200)")
}

func TestComputeMOSF1_NoJitterSameAsDefault(t *testing.T) {
	// Without jitter, jbMs doesn't matter → all variants equal
	clean := ComputeMOS("PCMU", 0, 0)
	require.InDelta(t, clean, ComputeMOSF1("PCMU", 0, 0), 0.0001)
	require.InDelta(t, clean, ComputeMOSF2("PCMU", 0, 0), 0.0001)
	require.InDelta(t, clean, ComputeMOSAdaptive("PCMU", 0, 0), 0.0001)
}
