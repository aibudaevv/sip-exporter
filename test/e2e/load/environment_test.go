//go:build e2e

package load

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExporterContainerRequestHermeticDefaults(t *testing.T) {
	t.Setenv("SIP_EXPORTER_E2E_GOMAXPROCS", "")
	t.Setenv("SIP_EXPORTER_E2E_GODEBUG", "")
	t.Setenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE", "")

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)

	req := exporterContainerRequest(ctx, t, "lo", "31000", "5060")

	require.Equal(t, map[string]string{
		"SIP_EXPORTER_INTERFACE":       "lo",
		"SIP_EXPORTER_HTTP_PORT":       "31000",
		"SIP_EXPORTER_SIP_PORTS":       "5060",
		"SIP_EXPORTER_LOGGER_LEVEL":    "error",
		"SIP_EXPORTER_IGNORE_OUTGOING": "true",
		"SIP_EXPORTER_TELEMETRY":       "false",
	}, req.Env)

	strategy, ok := req.WaitingFor.(interface{ Timeout() *time.Duration })
	require.True(t, ok)
	require.NotNil(t, strategy.Timeout())
	require.InDelta(t, time.Until(deadline), *strategy.Timeout(), float64(100*time.Millisecond))
}

func TestExporterContainerRequestUsesTestDeadline(t *testing.T) {
	deadline, ok := t.Deadline()
	require.True(t, ok)

	req := exporterContainerRequest(t.Context(), t, "lo", "31000", "5060")

	strategy, ok := req.WaitingFor.(interface{ Timeout() *time.Duration })
	require.True(t, ok)
	require.NotNil(t, strategy.Timeout())
	require.InDelta(t, time.Until(deadline), *strategy.Timeout(), float64(100*time.Millisecond))
}

func TestExporterContainerRequestVerboseLogging(t *testing.T) {
	t.Setenv("SIP_EXPORTER_E2E_EXPORTER_VERBOSE", "true")

	req := exporterContainerRequest(t.Context(), t, "lo", "31000", "5060")

	require.Equal(t, "debug", req.Env["SIP_EXPORTER_LOGGER_LEVEL"])
}
