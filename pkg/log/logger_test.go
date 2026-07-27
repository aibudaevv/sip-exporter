package log

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestVerbosityError(t *testing.T) {
	err := Verbosity("error")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosityInfo(t *testing.T) {
	err := Verbosity("info")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosityDebug(t *testing.T) {
	err := Verbosity("debug")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosityInfoUppercase(t *testing.T) {
	err := Verbosity("INFO")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosityDebugMixedCase(t *testing.T) {
	err := Verbosity("DeBuG")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosityUnknown(t *testing.T) {
	err := Verbosity("unknown_level")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosityEmpty(t *testing.T) {
	err := Verbosity("")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosityInvalid(t *testing.T) {
	err := Verbosity("invalid")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestInfoLevelConstant(t *testing.T) {
	require.Equal(t, "info", InfoLevel)
}

func TestSetHandlerInfoLevel(t *testing.T) {
	err := setHandler(zap.InfoLevel)
	require.NoError(t, err)
}

func TestSetHandlerDebugLevel(t *testing.T) {
	err := setHandler(zap.DebugLevel)
	require.NoError(t, err)
}

func TestSetHandlerErrorLevel(t *testing.T) {
	err := setHandler(zap.ErrorLevel)
	require.NoError(t, err)
}

func TestLoggerAfterVerbositySet(t *testing.T) {
	levels := []string{"error", "info", "debug"}

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			err := Verbosity(level)
			require.NoError(t, err)

			zap.L().Info("test message", zap.String("level", level))
			zap.L().Debug("debug message")
			zap.L().Error("error message")
		})
	}
}
