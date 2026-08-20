//go:build e2e

package load

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitForSIPpUDPPortFree(t *testing.T) {
	t.Run("already free", func(t *testing.T) {
		listener, err := net.ListenPacket("udp", "127.0.0.1:0")
		require.NoError(t, err)
		address := listener.LocalAddr().String()
		require.NoError(t, listener.Close())

		require.NoError(t, waitForSIPpUDPPortFree(t.Context(), address))
	})

	t.Run("released", func(t *testing.T) {
		listener, err := net.ListenPacket("udp", "127.0.0.1:0")
		require.NoError(t, err)
		address := listener.LocalAddr().String()
		released := make(chan struct{})
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = listener.Close()
			close(released)
		}()

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		require.NoError(t, waitForSIPpUDPPortFree(ctx, address))
		<-released
	})

	t.Run("remains occupied", func(t *testing.T) {
		listener, err := net.ListenPacket("udp", "127.0.0.1:0")
		require.NoError(t, err)
		defer listener.Close()

		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		err = waitForSIPpUDPPortFree(ctx, listener.LocalAddr().String())
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("invalid address", func(t *testing.T) {
		err := waitForSIPpUDPPortFree(t.Context(), "invalid")
		require.ErrorContains(t, err, "check SIPp UDP port")
	})
}
