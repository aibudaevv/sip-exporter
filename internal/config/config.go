// Package config reads environment variables into a typed [App] struct.
package config

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ilyakaznacheev/cleanenv"
)

type (
	// App holds all configuration values sourced from SIP_EXPORTER_* env vars.
	App struct {
		LogLevel                  string        `env:"SIP_EXPORTER_LOGGER_LEVEL"                  env-default:"info"`
		Port                      string        `env:"SIP_EXPORTER_HTTP_PORT"                     env-default:"2112"`
		Interfaces                string        `env:"SIP_EXPORTER_INTERFACE"                                                                                env-required:"true"`
		BPFBinaryPath             string        `env:"SIP_EXPORTER_OBJECT_FILE_PATH"              env-default:"/usr/local/bin/sip.o"`
		SIPPorts                  string        `env:"SIP_EXPORTER_SIP_PORTS"                     env-default:"5060"`
		CarriersConfigPath        string        `env:"SIP_EXPORTER_CARRIERS_CONFIG"`
		UserAgentsConfigPath      string        `env:"SIP_EXPORTER_USER_AGENTS_CONFIG"`
		IgnoreOutgoing            bool          `env:"SIP_EXPORTER_IGNORE_OUTGOING"               env-default:"false"`
		RTPStreamTTL              time.Duration `env:"SIP_EXPORTER_RTP_STREAM_TTL"                env-default:"30s"`
		Telemetry                 bool          `env:"SIP_EXPORTER_TELEMETRY"                     env-default:"true"`
		TelemetryURL              string        `env:"SIP_EXPORTER_TELEMETRY_URL"                 env-default:"https://telemetry.sip-exporter.com/v1/beacon"`
		TelemetryIDFile           string        `env:"SIP_EXPORTER_TELEMETRY_ID_FILE"             env-default:"/var/lib/sip-exporter/anon_id"`
		GeoIPCountryDB            string        `env:"SIP_EXPORTER_GEOIP_COUNTRY_DB"`
		LocalCountryCode          string        `env:"SIP_EXPORTER_LOCAL_COUNTRY_CODE"`
		HostLabels                bool          `env:"SIP_EXPORTER_HOST_LABELS"                   env-default:"false"`
		SessionsLimitsPath        string        `env:"SIP_EXPORTER_SESSIONS_LIMITS"`
		FraudRegScanThreshold     int           `env:"SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD" env-default:"10"`
		FraudRegScanWindow        time.Duration `env:"SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW"    env-default:"60s"`
		FraudInviteBurstThreshold int           `env:"SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD"  env-default:"100"`
		FraudInviteBurstWindow    time.Duration `env:"SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW"     env-default:"60s"`
	}
)

// GetConfig reads all SIP_EXPORTER_* environment variables and returns a
// populated [*App]. Returns an error if a required variable is missing.
func GetConfig() (*App, error) {
	cfg := &App{}

	if err := cleanenv.ReadEnv(cfg); err != nil {
		helpText := "error read env"
		help, _ := cleanenv.GetDescription(cfg, &helpText)
		return nil, fmt.Errorf("read env: %w, info: %s", err, help)
	}

	return cfg, nil
}

// ParsedInterfaces splits the SIP_EXPORTER_INTERFACE env value into a list of
// interface names. Accepts comma-separated values with optional whitespace.
// Empty elements are dropped; duplicates are removed with a warning. Returns
// an error if no valid interface name remains after parsing.
func (c *App) ParsedInterfaces() ([]string, error) {
	raw := strings.Split(c.Interfaces, ",")
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, name := range raw {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			zap.L().Warn("duplicate interface in SIP_EXPORTER_INTERFACE, deduplicating",
				zap.String("interface", name))
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, errors.New("SIP_EXPORTER_INTERFACE has no valid interface names after parsing")
	}
	return out, nil
}

// ParsedSIPPorts parses SIP_EXPORTER_SIP_PORTS into a per-interface list of
// port sets. The value is either a single port list shared by all interfaces
// ("5060,5062,5080") or a semicolon-separated per-interface list
// ("5060,5062;5060,5061") whose group count must match interfaceCount.
// Each group may contain 1-3 unique ports in range 1-65535.
func (c *App) ParsedSIPPorts(interfaceCount int) ([][]uint16, error) {
	groups := strings.Split(c.SIPPorts, ";")
	if len(groups) == 1 {
		ports, err := parsePortGroup(groups[0])
		if err != nil {
			return nil, err
		}
		out := make([][]uint16, interfaceCount)
		for i := range out {
			out[i] = slices.Clone(ports)
		}
		return out, nil
	}
	if len(groups) != interfaceCount {
		return nil, fmt.Errorf(
			"SIP_EXPORTER_SIP_PORTS has %d interface groups but SIP_EXPORTER_INTERFACE has %d",
			len(groups),
			interfaceCount,
		)
	}
	out := make([][]uint16, interfaceCount)
	for i, g := range groups {
		ports, err := parsePortGroup(g)
		if err != nil {
			return nil, fmt.Errorf("SIP_EXPORTER_SIP_PORTS group %d: %w", i+1, err)
		}
		out[i] = ports
	}
	return out, nil
}

// parsePortGroup parses a comma-separated port list ("5060,5062,5080") into a
// validated []uint16. Enforces: numeric, range 1-65535, no duplicates, max 3.
func parsePortGroup(s string) ([]uint16, error) {
	const minPort, maxPort = 1, 65535
	const maxPortsPerInterface = 3 // must match SIP_MAX_PORTS in internal/bpf/sip.c
	parts := strings.Split(s, ",")
	if len(parts) > maxPortsPerInterface {
		return nil, fmt.Errorf("too many ports (%d): max %d per interface", len(parts), maxPortsPerInterface)
	}
	ports := make([]uint16, 0, len(parts))
	seen := make(map[uint16]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		port, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: must be a number", p)
		}
		if port < minPort || port > maxPort {
			return nil, fmt.Errorf("invalid port %d: must be %d-%d", port, minPort, maxPort)
		}
		if _, dup := seen[uint16(port)]; dup {
			return nil, fmt.Errorf("duplicate port %d", port)
		}
		seen[uint16(port)] = struct{}{}
		ports = append(ports, uint16(port))
	}
	return ports, nil
}
