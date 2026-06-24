package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// configEnvVars are all env vars consumed by GetConfig. unsetConfigEnv removes
// them so each test starts from a clean state regardless of the host
// environment. os.Unsetenv is used (there is no t.Unsetenv); cross-test
// isolation holds because every test clears first and `go test` runs in a
// throwaway subprocess.
func unsetConfigEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"SIP_EXPORTER_LOGGER_LEVEL",
		"SIP_EXPORTER_HTTP_PORT",
		"SIP_EXPORTER_INTERFACE",
		"SIP_EXPORTER_OBJECT_FILE_PATH",
		"SIP_EXPORTER_SIP_PORT",
		"SIP_EXPORTER_SIPS_PORT",
		"SIP_EXPORTER_RTP_CAPTURE",
		"SIP_EXPORTER_RTP_STREAM_TTL",
	} {
		os.Unsetenv(k)
	}
}

func TestGetConfig_Defaults(t *testing.T) {
	unsetConfigEnv(t)
	t.Setenv("SIP_EXPORTER_INTERFACE", "eth0")

	cfg, err := GetConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "info", cfg.LogLevel)
	require.Equal(t, "2112", cfg.Port)
	require.Equal(t, "eth0", cfg.Interface)
	require.Equal(t, "/usr/local/bin/sip.o", cfg.BPFBinaryPath)
	require.Equal(t, 5060, cfg.SIPPort)
	require.Equal(t, 5061, cfg.SIPSPort)
}

func TestGetConfig_CustomValues(t *testing.T) {
	unsetConfigEnv(t)
	t.Setenv("SIP_EXPORTER_LOGGER_LEVEL", "debug")
	t.Setenv("SIP_EXPORTER_HTTP_PORT", "9090")
	t.Setenv("SIP_EXPORTER_INTERFACE", "lo")
	t.Setenv("SIP_EXPORTER_OBJECT_FILE_PATH", "/custom/path/sip.o")
	t.Setenv("SIP_EXPORTER_SIP_PORT", "6060")
	t.Setenv("SIP_EXPORTER_SIPS_PORT", "6061")

	cfg, err := GetConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "debug", cfg.LogLevel)
	require.Equal(t, "9090", cfg.Port)
	require.Equal(t, "lo", cfg.Interface)
	require.Equal(t, "/custom/path/sip.o", cfg.BPFBinaryPath)
	require.Equal(t, 6060, cfg.SIPPort)
	require.Equal(t, 6061, cfg.SIPSPort)
}

func TestGetConfig_RequiredInterfaceMissing(t *testing.T) {
	unsetConfigEnv(t)

	cfg, err := GetConfig()

	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "err:")
}

func TestGetConfig_OnlySIPPortCustom(t *testing.T) {
	unsetConfigEnv(t)
	t.Setenv("SIP_EXPORTER_INTERFACE", "eth0")
	t.Setenv("SIP_EXPORTER_SIP_PORT", "7070")

	cfg, err := GetConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, 7070, cfg.SIPPort)
	require.Equal(t, 5061, cfg.SIPSPort) // default
}

func TestGetConfig_OnlySIPSPortCustom(t *testing.T) {
	unsetConfigEnv(t)
	t.Setenv("SIP_EXPORTER_INTERFACE", "eth0")
	t.Setenv("SIP_EXPORTER_SIPS_PORT", "8080")

	cfg, err := GetConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, 5060, cfg.SIPPort) // default
	require.Equal(t, 8080, cfg.SIPSPort)
}

func TestGetConfig_RTPCaptureDefault(t *testing.T) {
	unsetConfigEnv(t)
	t.Setenv("SIP_EXPORTER_INTERFACE", "eth0")

	cfg, err := GetConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.True(t, cfg.RTPCapture, "RTP capture must be enabled by default")
}

func TestGetConfig_RTPCaptureDisabled(t *testing.T) {
	unsetConfigEnv(t)
	t.Setenv("SIP_EXPORTER_INTERFACE", "eth0")
	t.Setenv("SIP_EXPORTER_RTP_CAPTURE", "false")

	cfg, err := GetConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.False(t, cfg.RTPCapture, "RTP capture must be disabled when env is false")
}

func TestGetConfig_RTPStreamTTLDefault(t *testing.T) {
	unsetConfigEnv(t)
	t.Setenv("SIP_EXPORTER_INTERFACE", "eth0")

	cfg, err := GetConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, 30*time.Second, cfg.RTPStreamTTL, "RTP stream TTL must default to 30s")
}

func TestGetConfig_RTPStreamTTLCustom(t *testing.T) {
	unsetConfigEnv(t)
	t.Setenv("SIP_EXPORTER_INTERFACE", "eth0")
	t.Setenv("SIP_EXPORTER_RTP_STREAM_TTL", "2s")

	cfg, err := GetConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, 2*time.Second, cfg.RTPStreamTTL, "RTP stream TTL must be parsed from env")
}
