package log

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestVerbosity_Error(t *testing.T) {
	err := Verbosity("error")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosity_Info(t *testing.T) {
	err := Verbosity("info")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosity_Debug(t *testing.T) {
	err := Verbosity("debug")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosity_Info_Uppercase(t *testing.T) {
	err := Verbosity("INFO")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosity_Debug_MixedCase(t *testing.T) {
	err := Verbosity("DeBuG")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosity_Unknown(t *testing.T) {
	err := Verbosity("unknown_level")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosity_Empty(t *testing.T) {
	err := Verbosity("")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestVerbosity_Invalid(t *testing.T) {
	err := Verbosity("invalid")
	require.NoError(t, err)
	require.NotNil(t, zap.L())
}

func TestInfoLevel_Constant(t *testing.T) {
	require.Equal(t, "info", InfoLevel)
}

func TestSetHandler_InfoLevel(t *testing.T) {
	err := setHandler(zap.InfoLevel)
	require.NoError(t, err)
}

func TestSetHandler_DebugLevel(t *testing.T) {
	err := setHandler(zap.DebugLevel)
	require.NoError(t, err)
}

func TestSetHandler_ErrorLevel(t *testing.T) {
	err := setHandler(zap.ErrorLevel)
	require.NoError(t, err)
}

func TestLogger_AfterVerbositySet(t *testing.T) {
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
