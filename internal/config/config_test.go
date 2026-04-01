package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetConfig_Defaults(t *testing.T) {
	// Сохраняем оригинальные значения
	originalVars := map[string]string{
		"SIP_EXPORTER_LOGGER_LEVEL":     os.Getenv("SIP_EXPORTER_LOGGER_LEVEL"),
		"SIP_EXPORTER_HTTP_PORT":        os.Getenv("SIP_EXPORTER_HTTP_PORT"),
		"SIP_EXPORTER_INTERFACE":        os.Getenv("SIP_EXPORTER_INTERFACE"),
		"SIP_EXPORTER_OBJECT_FILE_PATH": os.Getenv("SIP_EXPORTER_OBJECT_FILE_PATH"),
		"SIP_EXPORTER_SIP_PORT":         os.Getenv("SIP_EXPORTER_SIP_PORT"),
		"SIP_EXPORTER_SIPS_PORT":        os.Getenv("SIP_EXPORTER_SIPS_PORT"),
	}
	defer func() {
		// Восстанавливаем оригинальные значения
		for k, v := range originalVars {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}()

	// Очищаем переменные окружения
	for k := range originalVars {
		_ = os.Unsetenv(k)
	}

	// Устанавливаем только обязательный параметр
	_ = os.Setenv("SIP_EXPORTER_INTERFACE", "eth0")

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
	// Сохраняем оригинальные значения
	originalVars := map[string]string{
		"SIP_EXPORTER_LOGGER_LEVEL":     os.Getenv("SIP_EXPORTER_LOGGER_LEVEL"),
		"SIP_EXPORTER_HTTP_PORT":        os.Getenv("SIP_EXPORTER_HTTP_PORT"),
		"SIP_EXPORTER_INTERFACE":        os.Getenv("SIP_EXPORTER_INTERFACE"),
		"SIP_EXPORTER_OBJECT_FILE_PATH": os.Getenv("SIP_EXPORTER_OBJECT_FILE_PATH"),
		"SIP_EXPORTER_SIP_PORT":         os.Getenv("SIP_EXPORTER_SIP_PORT"),
		"SIP_EXPORTER_SIPS_PORT":        os.Getenv("SIP_EXPORTER_SIPS_PORT"),
	}
	defer func() {
		for k, v := range originalVars {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}()

	// Устанавливаем кастомные значения
	_ = os.Setenv("SIP_EXPORTER_LOGGER_LEVEL", "debug")
	_ = os.Setenv("SIP_EXPORTER_HTTP_PORT", "9090")
	_ = os.Setenv("SIP_EXPORTER_INTERFACE", "lo")
	_ = os.Setenv("SIP_EXPORTER_OBJECT_FILE_PATH", "/custom/path/sip.o")
	_ = os.Setenv("SIP_EXPORTER_SIP_PORT", "6060")
	_ = os.Setenv("SIP_EXPORTER_SIPS_PORT", "6061")

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
	// Сохраняем оригинальные значения
	originalVars := map[string]string{
		"SIP_EXPORTER_LOGGER_LEVEL":     os.Getenv("SIP_EXPORTER_LOGGER_LEVEL"),
		"SIP_EXPORTER_HTTP_PORT":        os.Getenv("SIP_EXPORTER_HTTP_PORT"),
		"SIP_EXPORTER_INTERFACE":        os.Getenv("SIP_EXPORTER_INTERFACE"),
		"SIP_EXPORTER_OBJECT_FILE_PATH": os.Getenv("SIP_EXPORTER_OBJECT_FILE_PATH"),
		"SIP_EXPORTER_SIP_PORT":         os.Getenv("SIP_EXPORTER_SIP_PORT"),
		"SIP_EXPORTER_SIPS_PORT":        os.Getenv("SIP_EXPORTER_SIPS_PORT"),
	}
	defer func() {
		for k, v := range originalVars {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}()

	// Очищаем все переменные
	for k := range originalVars {
		_ = os.Unsetenv(k)
	}

	cfg, err := GetConfig()

	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "err:")
}

func TestGetConfig_OnlySIPPortCustom(t *testing.T) {
	originalVars := map[string]string{
		"SIP_EXPORTER_LOGGER_LEVEL":     os.Getenv("SIP_EXPORTER_LOGGER_LEVEL"),
		"SIP_EXPORTER_HTTP_PORT":        os.Getenv("SIP_EXPORTER_HTTP_PORT"),
		"SIP_EXPORTER_INTERFACE":        os.Getenv("SIP_EXPORTER_INTERFACE"),
		"SIP_EXPORTER_OBJECT_FILE_PATH": os.Getenv("SIP_EXPORTER_OBJECT_FILE_PATH"),
		"SIP_EXPORTER_SIP_PORT":         os.Getenv("SIP_EXPORTER_SIP_PORT"),
		"SIP_EXPORTER_SIPS_PORT":        os.Getenv("SIP_EXPORTER_SIPS_PORT"),
	}
	defer func() {
		for k, v := range originalVars {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}()

	for k := range originalVars {
		_ = os.Unsetenv(k)
	}

	_ = os.Setenv("SIP_EXPORTER_INTERFACE", "eth0")
	_ = os.Setenv("SIP_EXPORTER_SIP_PORT", "7070")

	cfg, err := GetConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, 7070, cfg.SIPPort)
	require.Equal(t, 5061, cfg.SIPSPort) // default
}

func TestGetConfig_OnlySIPSPortCustom(t *testing.T) {
	originalVars := map[string]string{
		"SIP_EXPORTER_LOGGER_LEVEL":     os.Getenv("SIP_EXPORTER_LOGGER_LEVEL"),
		"SIP_EXPORTER_HTTP_PORT":        os.Getenv("SIP_EXPORTER_HTTP_PORT"),
		"SIP_EXPORTER_INTERFACE":        os.Getenv("SIP_EXPORTER_INTERFACE"),
		"SIP_EXPORTER_OBJECT_FILE_PATH": os.Getenv("SIP_EXPORTER_OBJECT_FILE_PATH"),
		"SIP_EXPORTER_SIP_PORT":         os.Getenv("SIP_EXPORTER_SIP_PORT"),
		"SIP_EXPORTER_SIPS_PORT":        os.Getenv("SIP_EXPORTER_SIPS_PORT"),
	}
	defer func() {
		for k, v := range originalVars {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}()

	for k := range originalVars {
		_ = os.Unsetenv(k)
	}

	_ = os.Setenv("SIP_EXPORTER_INTERFACE", "eth0")
	_ = os.Setenv("SIP_EXPORTER_SIPS_PORT", "8080")

	cfg, err := GetConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, 5060, cfg.SIPPort) // default
	require.Equal(t, 8080, cfg.SIPSPort)
}
