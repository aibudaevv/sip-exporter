//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPortAllocatorsDoNotReusePorts(t *testing.T) {
	allocated := make(map[string]struct{})
	for range 500 {
		exporter, sipp, sippClient := allocatePorts()
		for _, port := range []string{exporter, sipp, sippClient, freePort(t)} {
			require.NotContains(t, allocated, port, "port %s was allocated more than once", port)
			allocated[port] = struct{}{}
		}
	}
}

func TestPortAllocatorsAvoidKernelEphemeralRange(t *testing.T) {
	exporter, sipp, sippClient := allocatePorts()
	for _, port := range []string{exporter, sipp, sippClient, freePort(t)} {
		portNumber, err := strconv.Atoi(port)
		require.NoError(t, err)
		require.GreaterOrEqual(t, portNumber, 20000)
		require.Less(t, portNumber, 30000)
	}
}
