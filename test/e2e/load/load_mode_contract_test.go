//go:build e2e

package load

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func validReleaseRunForReport() RunArtifactV2 {
	run := validRunArtifactV2()
	run.Mode = runModeRelease
	run.ReleaseEligible = true
	run.Results = make([]ScenarioResultV2, len(releaseScenarios))
	for i, name := range releaseScenarios {
		run.Results[i] = validRunArtifactV2().Results[0]
		run.Results[i].Name = name
	}
	return run
}

func TestValidateReleaseRun(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RunArtifactV2)
		want   string
	}{
		{name: "exact release matrix"},
		{name: "targeted mode", mutate: func(run *RunArtifactV2) {
			run.Mode = runModeTargeted
			run.ReleaseEligible = false
		}, want: "mode"},
		{name: "ineligible", mutate: func(run *RunArtifactV2) {
			run.ReleaseEligible = false
		}, want: "eligibility"},
		{name: "missing scenario", mutate: func(run *RunArtifactV2) {
			run.Results = run.Results[:len(run.Results)-1]
		}, want: "missing scenario"},
		{name: "extra scenario", mutate: func(run *RunArtifactV2) {
			extra := run.Results[0]
			extra.Name = "TestReleaseCarrierUA"
			run.Results = append(run.Results, extra)
		}, want: "unexpected scenario"},
		{name: "duplicate scenario", mutate: func(run *RunArtifactV2) {
			run.Results = append(run.Results, run.Results[0])
		}, want: "duplicate scenario"},
		{name: "incomplete scenario", mutate: func(run *RunArtifactV2) {
			run.Results[0].Status = scenarioStatusIncomplete
		}, want: "complete"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := validReleaseRunForReport()
			if tt.mutate != nil {
				tt.mutate(&run)
			}

			err := validateReleaseRun(run)
			if tt.want != "" {
				require.ErrorContains(t, err, tt.want)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateReleaseRunKeepsExactScenarioInventory(t *testing.T) {
	run := validReleaseRunForReport()
	slices.Reverse(run.Results)

	require.NoError(t, validateReleaseRun(run))
}
