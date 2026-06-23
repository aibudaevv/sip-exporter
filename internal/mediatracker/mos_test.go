package mediatracker

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMosFromR(t *testing.T) {
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
