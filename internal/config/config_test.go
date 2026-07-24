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
		"SIP_EXPORTER_SIP_PORTS",
		"SIP_EXPORTER_RTP_STREAM_TTL",
		"SIP_EXPORTER_TELEMETRY",
		"SIP_EXPORTER_TELEMETRY_URL",
		"SIP_EXPORTER_TELEMETRY_ID_FILE",
		"SIP_EXPORTER_GEOIP_COUNTRY_DB",
		"SIP_EXPORTER_LOCAL_COUNTRY_CODE",
		"SIP_EXPORTER_HOST_LABELS",
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

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"log level", cfg.LogLevel, "info"},
		{"http port", cfg.Port, "2112"},
		{"interfaces", cfg.Interfaces, "eth0"},
		{"bpf binary path", cfg.BPFBinaryPath, "/usr/local/bin/sip.o"},
		{"sip ports", cfg.SIPPorts, "5060"},
		{"rtp stream ttl", cfg.RTPStreamTTL, 30 * time.Second},
		{"telemetry enabled", cfg.Telemetry, true},
		{"telemetry url", cfg.TelemetryURL, "https://telemetry.sip-exporter.com/v1/beacon"},
		{"telemetry id file", cfg.TelemetryIDFile, "/var/lib/sip-exporter/anon_id"},
		{"geoip country db", cfg.GeoIPCountryDB, ""},
		{"host labels", cfg.HostLabels, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.got)
		})
	}
}

func TestGetConfig_Custom(t *testing.T) {
	tests := []struct {
		name   string
		envKey string
		envVal string
		check  func(*testing.T, *App)
	}{
		{
			name:   "log level",
			envKey: "SIP_EXPORTER_LOGGER_LEVEL",
			envVal: "debug",
			check:  func(t *testing.T, c *App) { require.Equal(t, "debug", c.LogLevel) },
		},
		{
			name:   "http port",
			envKey: "SIP_EXPORTER_HTTP_PORT",
			envVal: "9090",
			check:  func(t *testing.T, c *App) { require.Equal(t, "9090", c.Port) },
		},
		{
			name:   "object file path",
			envKey: "SIP_EXPORTER_OBJECT_FILE_PATH",
			envVal: "/custom/path/sip.o",
			check:  func(t *testing.T, c *App) { require.Equal(t, "/custom/path/sip.o", c.BPFBinaryPath) },
		},
		{
			name:   "sip ports single",
			envKey: "SIP_EXPORTER_SIP_PORTS",
			envVal: "6060",
			check:  func(t *testing.T, c *App) { require.Equal(t, "6060", c.SIPPorts) },
		},
		{
			name:   "sip ports multi",
			envKey: "SIP_EXPORTER_SIP_PORTS",
			envVal: "6060,6062",
			check:  func(t *testing.T, c *App) { require.Equal(t, "6060,6062", c.SIPPorts) },
		},
		{
			name:   "rtp stream ttl",
			envKey: "SIP_EXPORTER_RTP_STREAM_TTL",
			envVal: "2s",
			check:  func(t *testing.T, c *App) { require.Equal(t, 2*time.Second, c.RTPStreamTTL) },
		},
		{
			name:   "telemetry disabled",
			envKey: "SIP_EXPORTER_TELEMETRY",
			envVal: "false",
			check:  func(t *testing.T, c *App) { require.False(t, c.Telemetry) },
		},
		{
			name:   "telemetry url",
			envKey: "SIP_EXPORTER_TELEMETRY_URL",
			envVal: "https://example.com/beacon",
			check:  func(t *testing.T, c *App) { require.Equal(t, "https://example.com/beacon", c.TelemetryURL) },
		},
		{
			name:   "telemetry id file",
			envKey: "SIP_EXPORTER_TELEMETRY_ID_FILE",
			envVal: "/tmp/my-id",
			check:  func(t *testing.T, c *App) { require.Equal(t, "/tmp/my-id", c.TelemetryIDFile) },
		},
		{
			name:   "geoip country db",
			envKey: "SIP_EXPORTER_GEOIP_COUNTRY_DB",
			envVal: "/data/geoip.mmdb",
			check:  func(t *testing.T, c *App) { require.Equal(t, "/data/geoip.mmdb", c.GeoIPCountryDB) },
		},
		{
			name:   "host labels enabled",
			envKey: "SIP_EXPORTER_HOST_LABELS",
			envVal: "true",
			check:  func(t *testing.T, c *App) { require.True(t, c.HostLabels) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetConfigEnv(t)
			t.Setenv("SIP_EXPORTER_INTERFACE", "eth0")
			t.Setenv(tt.envKey, tt.envVal)

			cfg, err := GetConfig()
			require.NoError(t, err)
			require.NotNil(t, cfg)
			tt.check(t, cfg)
		})
	}
}

func TestGetConfig_RequiredInterfaceMissing(t *testing.T) {
	unsetConfigEnv(t)

	cfg, err := GetConfig()

	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "read env:")
}

func TestApp_ParsedInterfaces(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		want      []string
		wantError bool
	}{
		{name: "single interface", input: "eth0", want: []string{"eth0"}},
		{name: "two interfaces", input: "eth0,eth1", want: []string{"eth0", "eth1"}},
		{name: "three interfaces", input: "eth0,eth1,eth2", want: []string{"eth0", "eth1", "eth2"}},
		{name: "with whitespace around names", input: " eth0 , eth1 ", want: []string{"eth0", "eth1"}},
		{name: "trailing comma", input: "eth0,", want: []string{"eth0"}},
		{name: "leading comma", input: ",eth0", want: []string{"eth0"}},
		{name: "empty element in middle", input: "eth0,,eth1", want: []string{"eth0", "eth1"}},
		{name: "whitespace element in middle", input: "eth0, ,eth1", want: []string{"eth0", "eth1"}},
		{name: "duplicate adjacent", input: "eth0,eth0", want: []string{"eth0"}},
		{name: "duplicate three times", input: "eth0,eth0,eth0", want: []string{"eth0"}},
		{name: "duplicate non-adjacent", input: "eth0,eth1,eth0", want: []string{"eth0", "eth1"}},
		{name: "preserves order first occurrence", input: "eth1,eth0,eth1", want: []string{"eth1", "eth0"}},
		{name: "empty string", input: "", want: nil, wantError: true},
		{name: "only comma", input: ",", want: nil, wantError: true},
		{name: "multiple commas", input: ",,,", want: nil, wantError: true},
		{name: "whitespace and commas", input: " , , ", want: nil, wantError: true},
		{name: "only spaces", input: "   ", want: nil, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &App{Interfaces: tc.input}
			got, err := cfg.ParsedInterfaces()
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, got)
				require.Contains(t, err.Error(), "SIP_EXPORTER_INTERFACE")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestApp_ParsedSIPPorts(t *testing.T) {
	cases := []struct {
		name       string
		sipPorts   string
		ifaceCount int
		want       [][]uint16
		wantErr    string
	}{
		// Single group (no ';') applied to all interfaces.
		{
			name:       "single group all ifaces",
			sipPorts:   "5060,5062,5080",
			ifaceCount: 2,
			want:       [][]uint16{{5060, 5062, 5080}, {5060, 5062, 5080}},
		},
		{name: "single port", sipPorts: "5060", ifaceCount: 1, want: [][]uint16{{5060}}},
		{
			name:       "single group whitespace trimmed",
			sipPorts:   " 5060 , 5062 ",
			ifaceCount: 1,
			want:       [][]uint16{{5060, 5062}},
		},

		// Per-interface (';' separator), group count must match ifaceCount.
		{
			name:       "per interface aligned",
			sipPorts:   "5060,5062;5060,5061",
			ifaceCount: 2,
			want:       [][]uint16{{5060, 5062}, {5060, 5061}},
		},
		{
			name:       "per interface single port each",
			sipPorts:   "5060;5061",
			ifaceCount: 2,
			want:       [][]uint16{{5060}, {5061}},
		},
		{
			name:       "three interfaces distinct ports",
			sipPorts:   "5060,5062;5070,5072;5080,5082",
			ifaceCount: 3,
			want:       [][]uint16{{5060, 5062}, {5070, 5072}, {5080, 5082}},
		},
		{
			name:       "three interfaces max ports each",
			sipPorts:   "5060,5061,5062;5070,5071,5072;5080,5081,5082",
			ifaceCount: 3,
			want:       [][]uint16{{5060, 5061, 5062}, {5070, 5071, 5072}, {5080, 5081, 5082}},
		},
		{
			name:       "single group shared across three interfaces",
			sipPorts:   "5060,5062",
			ifaceCount: 3,
			want:       [][]uint16{{5060, 5062}, {5060, 5062}, {5060, 5062}},
		},
		{
			name:       "per interface mixed port counts",
			sipPorts:   "5060;5061,5062;5070,5071,5072",
			ifaceCount: 3,
			want:       [][]uint16{{5060}, {5061, 5062}, {5070, 5071, 5072}},
		},

		// Errors: validation branches (MC/DC).
		{name: "count mismatch", sipPorts: "5060;5061;5062", ifaceCount: 2, wantErr: "interface groups"},
		{name: "too many ports", sipPorts: "5060,5061,5062,5063", ifaceCount: 1, wantErr: "too many ports"},
		{name: "duplicate port", sipPorts: "5060,5060", ifaceCount: 1, wantErr: "duplicate port"},
		{name: "port zero", sipPorts: "0", ifaceCount: 1, wantErr: "1-65535"},
		{name: "port too high", sipPorts: "70000", ifaceCount: 1, wantErr: "1-65535"},
		{name: "port non numeric", sipPorts: "abc", ifaceCount: 1, wantErr: "must be a number"},
		{name: "empty group in middle", sipPorts: "5060;;5061", ifaceCount: 3, wantErr: "invalid port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &App{SIPPorts: tc.sipPorts}
			got, err := cfg.ParsedSIPPorts(tc.ifaceCount)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Nil(t, got)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
