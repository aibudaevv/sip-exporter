//go:build e2e

package load

import (
	"context"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
)

const testLimitsMemory = int64(256 << 20)

var testLimits = WorkloadLimits{CPUCores: 2, MemoryBytes: testLimitsMemory}

func TestExporterContainerRequestHermeticDefaults(t *testing.T) {
	t.Setenv("SIP_EXPORTER_E2E_GOMAXPROCS", "")
	t.Setenv("SIP_EXPORTER_E2E_GODEBUG", "")
	t.Setenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE", "")

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)

	req := exporterContainerRequest(ctx, t, "lo", "31000", "5060", testLimits)

	require.Equal(t, map[string]string{
		"GODEBUG":                      "gctrace=1",
		"SIP_EXPORTER_INTERFACE":       "lo",
		"SIP_EXPORTER_HTTP_PORT":       "31000",
		"SIP_EXPORTER_SIP_PORTS":       "5060",
		"SIP_EXPORTER_LOGGER_LEVEL":    "error",
		"SIP_EXPORTER_IGNORE_OUTGOING": "true",
		"SIP_EXPORTER_TELEMETRY":       "false",
	}, req.Env)
	require.NotNil(t, req.HostConfigModifier)
	hostConfig := &container.HostConfig{}
	req.HostConfigModifier(hostConfig)
	require.True(t, hostConfig.Privileged)
	require.Equal(t, container.NetworkMode("host"), hostConfig.NetworkMode)
	require.Equal(t, int64(2_000_000_000), hostConfig.NanoCPUs)
	require.Equal(t, testLimitsMemory, hostConfig.Memory)

	strategy, ok := req.WaitingFor.(interface{ Timeout() *time.Duration })
	require.True(t, ok)
	require.NotNil(t, strategy.Timeout())
	require.InDelta(t, time.Until(deadline), *strategy.Timeout(), float64(100*time.Millisecond))
}

func TestExporterContainerRequestUsesTestDeadline(t *testing.T) {
	deadline, ok := t.Deadline()
	require.True(t, ok)

	req := exporterContainerRequest(t.Context(), t, "lo", "31000", "5060", testLimits)

	strategy, ok := req.WaitingFor.(interface{ Timeout() *time.Duration })
	require.True(t, ok)
	require.NotNil(t, strategy.Timeout())
	require.InDelta(t, time.Until(deadline), *strategy.Timeout(), float64(100*time.Millisecond))
}

func TestExporterContainerRequestVerboseLogging(t *testing.T) {
	t.Setenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE", "true")

	req := exporterContainerRequest(t.Context(), t, "lo", "31000", "5060", testLimits)

	require.Equal(t, "debug", req.Env["SIP_EXPORTER_LOGGER_LEVEL"])
}

func TestExporterContainerRequestForcesGCTrace(t *testing.T) {
	tests := []struct {
		name string
		set  string
		want string
	}{
		{name: "default", want: "gctrace=1"},
		{name: "preserves other setting", set: "madvdontneed=1", want: "madvdontneed=1,gctrace=1"},
		{name: "replaces disabled trace", set: "gctrace=0,madvdontneed=1", want: "madvdontneed=1,gctrace=1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SIP_EXPORTER_E2E_GODEBUG", tt.set)
			req := exporterContainerRequest(t.Context(), t, "lo", "31000", "5060", testLimits)
			require.Equal(t, tt.want, req.Env["GODEBUG"])
		})
	}
}

func TestValidateContainerLimitsRequiresExactEffectiveValues(t *testing.T) {
	tests := []struct {
		name     string
		nanoCPUs int64
		memory   int64
		wantErr  bool
	}{
		{name: "exact", nanoCPUs: 2_000_000_000, memory: testLimitsMemory},
		{name: "CPU mismatch", nanoCPUs: 1_000_000_000, memory: testLimitsMemory, wantErr: true},
		{name: "memory mismatch", nanoCPUs: 2_000_000_000, memory: 128 << 20, wantErr: true},
		{name: "missing CPU", memory: testLimitsMemory, wantErr: true},
		{name: "missing memory", nanoCPUs: 2_000_000_000, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateContainerLimits(tt.nanoCPUs, tt.memory, testLimits)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
