//go:build e2e

package load

import (
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
)

func TestSIPpContainerRequestNetworkMode(t *testing.T) {
	tests := []struct {
		name        string
		networkMode string
		want        container.NetworkMode
	}{
		{name: "host", networkMode: "host", want: "host"},
		{name: "peer namespace", networkMode: "container:peer-id", want: "container:peer-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := sippContainerRequest(
				t.Context(), t, nil, "/scenarios", "", false, tt.networkMode,
			)
			require.Equal(t, tt.want, req.NetworkMode)
		})
	}
}

func TestRouteDevice(t *testing.T) {
	tests := []struct {
		name  string
		route string
		want  string
		ok    bool
	}{
		{name: "veth", route: "10.240.1.1 dev eth1 src 10.240.1.2", want: "eth1", ok: true},
		{name: "loopback", route: "local 10.240.1.1 dev lo", want: "lo", ok: true},
		{name: "wrong device remains visible", route: "10.240.1.1 dev eth2 src 10.240.1.2", want: "eth2", ok: true},
		{name: "missing device", route: "10.240.1.1 via 10.240.1.254"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := routeDevice(tt.route)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMultiNICFixtureRoutes(t *testing.T) {
	fixture := newMultiNICFixture(t, 3)
	require.Len(t, fixture.links, 3)

	for _, link := range fixture.links {
		route := fixture.peerRoute(t, link.hostIP)
		t.Logf("peer route to %s: %s", link.hostIP, route)
		device, ok := routeDevice(route)
		require.True(t, ok)
		require.NotEqual(t, "lo", device)
		require.Equal(t, link.peerInterface, device)
	}
}

func TestMultiNICBusinessValues(t *testing.T) {
	links := []multiNICLink{
		{hostInterface: "host0", hostIP: "10.240.0.1"},
		{hostInterface: "host1", hostIP: "10.240.1.1"},
		{hostInterface: "host2", hostIP: "10.240.2.1"},
	}
	want := map[string]float64{
		"invites_iface_1": 15000, "invites_iface_2": 15000, "invites_iface_3": 15000,
		"cross_interface_series": 0, "unexpected_series": 0,
	}
	tests := []struct {
		name    string
		samples []metricSample
		want    map[string]float64
	}{
		{
			name: "exact",
			samples: []metricSample{
				{labels: map[string]string{"iface": "host0", "called_host": "10.240.0.1"}, value: 15000},
				{labels: map[string]string{"iface": "host1", "called_host": "10.240.1.1"}, value: 15000},
				{labels: map[string]string{"iface": "host2", "called_host": "10.240.2.1"}, value: 15000},
			},
			want: want,
		},
		{
			name: "missing iface 2",
			samples: []metricSample{
				{labels: map[string]string{"iface": "host0", "called_host": "10.240.0.1"}, value: 15000},
				{labels: map[string]string{"iface": "host2", "called_host": "10.240.2.1"}, value: 15000},
			},
			want: map[string]float64{
				"invites_iface_1": 15000, "invites_iface_2": 0, "invites_iface_3": 15000,
				"cross_interface_series": 0, "unexpected_series": 0,
			},
		},
		{
			name: "swapped called host",
			samples: []metricSample{
				{labels: map[string]string{"iface": "host0", "called_host": "10.240.1.1"}, value: 15000},
			},
			want: map[string]float64{
				"invites_iface_1": 0, "invites_iface_2": 0, "invites_iface_3": 0,
				"cross_interface_series": 1, "unexpected_series": 0,
			},
		},
		{
			name: "unexpected interface",
			samples: []metricSample{
				{labels: map[string]string{"iface": "other", "called_host": "10.240.0.1"}, value: 15000},
			},
			want: map[string]float64{
				"invites_iface_1": 0, "invites_iface_2": 0, "invites_iface_3": 0,
				"cross_interface_series": 0, "unexpected_series": 1,
			},
		},
		{
			name:    "missing called host",
			samples: []metricSample{{labels: map[string]string{"iface": "host0"}, value: 15000}},
			want: map[string]float64{
				"invites_iface_1": 0, "invites_iface_2": 0, "invites_iface_3": 0,
				"cross_interface_series": 0, "unexpected_series": 1,
			},
		},
		{
			name: "wrong count",
			samples: []metricSample{
				{labels: map[string]string{"iface": "host0", "called_host": "10.240.0.1"}, value: 14999},
			},
			want: map[string]float64{
				"invites_iface_1": 14999, "invites_iface_2": 0, "invites_iface_3": 0,
				"cross_interface_series": 0, "unexpected_series": 0,
			},
		},
		{
			name: "duplicate expected pair",
			samples: []metricSample{
				{labels: map[string]string{"iface": "host0", "called_host": "10.240.0.1"}, value: 15000},
				{labels: map[string]string{"iface": "host0", "called_host": "10.240.0.1"}, value: 15000},
			},
			want: map[string]float64{
				"invites_iface_1": 15000, "invites_iface_2": 0, "invites_iface_3": 0,
				"cross_interface_series": 0, "unexpected_series": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, multiNICBusinessValues(tt.samples, links, 15000))
		})
	}
}

func TestMultiNICExporterRequest(t *testing.T) {
	fixture := newMultiNICFixture(t, 1)
	env := newMultiNICEnv(t.Context(), t, fixture.peerContainerID, fixture.links)
	inspection, err := env.exporterContainer.Inspect(t.Context())
	require.NoError(t, err)
	require.Contains(t, inspection.Config.Env, "SIP_EXPORTER_HOST_LABELS=true")

	for _, setting := range inspection.Config.Env {
		if strings.HasPrefix(setting, "SIP_EXPORTER_INTERFACE=") {
			require.NotContains(t, strings.Split(strings.TrimPrefix(setting,
				"SIP_EXPORTER_INTERFACE="), ","), "lo")
			return
		}
	}
	require.Fail(t, "exporter interface setting is missing")
}

func TestMultiNICGeneratorOverlap(t *testing.T) {
	valid := validMultiNICGenerators()
	require.NoError(t, validateMultiNICGeneratorOverlap(valid))
	require.NoError(t, validateMultiNICGeneratorOverlap(valid[:1]))
	require.NoError(t, validateMultiNICGeneratorOverlap(valid[:2]))

	tooWide := append([]GeneratorResult(nil), valid...)
	tooWide[2].Phases.WarmupStart = valid[0].Phases.WarmupStart.Add(
		600*time.Millisecond + time.Nanosecond,
	)
	require.Error(t, validateMultiNICGeneratorOverlap(tooWide))

	noOverlap := append([]GeneratorResult(nil), valid...)
	noOverlap[2].Phases.MeasureStart = valid[0].Phases.MeasureEnd
	require.Error(t, validateMultiNICGeneratorOverlap(noOverlap))

	require.Error(t, validateMultiNICGeneratorOverlap(nil))
}

func TestAggregateMultiNICGenerators(t *testing.T) {
	generators := validMultiNICGenerators()
	generators[1].FailedCalls = 2
	generators[2].Retransmissions = 3
	aggregate := aggregateMultiNICGenerators(generators)

	require.Equal(t, 45000, aggregate.SuccessfulCalls)
	require.Equal(t, 2, aggregate.FailedCalls)
	require.Equal(t, 3, aggregate.Retransmissions)
	require.InDelta(t, 1500, aggregate.ActualRate, 0.001)
	require.Equal(t, generators[0].Phases.MeasureStart, aggregate.Phases.MeasureStart)
	require.Equal(t, generators[2].Phases.MeasureEnd, aggregate.Phases.MeasureEnd)
}

func TestReleaseMultiNICRowMutationMatrix(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*releaseRowEvidence)
	}{
		{name: "generator 1 calls", mutate: func(e *releaseRowEvidence) { e.Generators[0].Result.SuccessfulCalls-- }},
		{name: "generator 2 calls", mutate: func(e *releaseRowEvidence) { e.Generators[1].Result.SuccessfulCalls-- }},
		{name: "generator 3 calls", mutate: func(e *releaseRowEvidence) { e.Generators[2].Result.SuccessfulCalls-- }},
		{name: "generator 1 rate", mutate: func(e *releaseRowEvidence) { e.Generators[0].Result.ActualRate = 0 }},
		{name: "generator 2 rate", mutate: func(e *releaseRowEvidence) { e.Generators[1].Result.ActualRate = 0 }},
		{name: "generator 3 rate", mutate: func(e *releaseRowEvidence) { e.Generators[2].Result.ActualRate = 0 }},
		{name: "capture", mutate: func(e *releaseRowEvidence) { e.Capture = newCaptureResult(45000, 44999) }},
		{name: "interface 1", mutate: mutateMultiNICBusiness("invites_iface_1", 14999)},
		{name: "interface 2", mutate: mutateMultiNICBusiness("invites_iface_2", 14999)},
		{name: "interface 3", mutate: mutateMultiNICBusiness("invites_iface_3", 14999)},
		{name: "cross interface", mutate: mutateMultiNICBusiness("cross_interface_series", 1)},
		{name: "unexpected series", mutate: mutateMultiNICBusiness("unexpected_series", 1)},
	}

	require.NoError(t, validateReleaseRow(releaseRowSpec{}, validMultiNICReleaseRow()))
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			evidence := validMultiNICReleaseRow()
			tt.mutate(&evidence)
			require.Error(t, validateReleaseRow(releaseRowSpec{}, evidence))
		})
	}
}

func TestTargetedMultiNICMetrics(t *testing.T) {
	result := loadResult{
		Generator:  GeneratorResult{ActualRate: 500},
		Capture:    newCaptureResult(15000, 15000),
		ErrorCount: 0,
		Resources:  ResourceSummaryV2{Limits: peakLimits},
	}
	metrics := targetedMultiNICMetrics(result, map[string]float64{
		"invites_iface_1":        15000,
		"cross_interface_series": 0,
		"unexpected_series":      0,
	})

	require.Equal(t, MetricEntry{Value: 500, Unit: "cps", Direction: dirHigherIsBetter},
		metrics["generator_cps"])
	require.Equal(t, MetricEntry{Value: 15000, Unit: "count", Direction: dirHigherIsBetter},
		metrics["captured_packets"])
	require.Equal(t, MetricEntry{Value: 0, Unit: "count", Direction: dirLowerIsBetter},
		metrics["system_errors"])
	require.Equal(t, MetricEntry{Value: 15000, Unit: "count", Direction: dirHigherIsBetter},
		metrics["invites_iface_1"])
	require.Equal(t, MetricEntry{Value: 0, Unit: "count", Direction: dirLowerIsBetter},
		metrics["cross_interface_series"])
	require.Equal(t, MetricEntry{Value: 0, Unit: "count", Direction: dirLowerIsBetter},
		metrics["unexpected_series"])
}

func validMultiNICGenerators() []GeneratorResult {
	start := time.Unix(100, 0)
	measureStart := start.Add(2 * time.Second)
	measureEnd := measureStart.Add(30 * time.Second)
	return []GeneratorResult{
		{SuccessfulCalls: 15000, ActualRate: 500,
			Phases: multiNICPhaseInterval(start, measureStart, measureEnd)},
		{SuccessfulCalls: 15000, ActualRate: 500,
			Phases: multiNICPhaseInterval(start.Add(300*time.Millisecond), measureStart, measureEnd)},
		{SuccessfulCalls: 15000, ActualRate: 500,
			Phases: multiNICPhaseInterval(start.Add(600*time.Millisecond), measureStart, measureEnd)},
	}
}

func multiNICPhaseInterval(
	dockerStartedAt, measureStart, measureEnd time.Time,
) PhaseTimestamps {
	return PhaseTimestamps{
		WarmupStart: dockerStartedAt,
		Ready:       measureStart, MeasureStart: measureStart, MeasureEnd: measureEnd, DrainEnd: measureEnd,
	}
}

func validMultiNICReleaseRow() releaseRowEvidence {
	profile := releaseMultiNICProfile()
	return releaseMultiNICRowFromLoad(profile, validReleaseLoadResult(profile), validMultiNICGenerators(),
		map[string]float64{
			"invites_iface_1": 15000, "invites_iface_2": 15000, "invites_iface_3": 15000,
			"cross_interface_series": 0, "unexpected_series": 0,
		})
}

func mutateMultiNICBusiness(name string, value float64) func(*releaseRowEvidence) {
	return func(evidence *releaseRowEvidence) {
		business := evidence.Business[name]
		business.Actual = value
		evidence.Business[name] = business
	}
}
