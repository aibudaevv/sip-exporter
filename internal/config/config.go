package config

import (
	"fmt"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type (
	App struct {
		LogLevel                  string        `env:"SIP_EXPORTER_LOGGER_LEVEL"                  env-default:"info"`
		Port                      string        `env:"SIP_EXPORTER_HTTP_PORT"                     env-default:"2112"`
		Interface                 string        `env:"SIP_EXPORTER_INTERFACE"                                                                                env-required:"true"`
		BPFBinaryPath             string        `env:"SIP_EXPORTER_OBJECT_FILE_PATH"              env-default:"/usr/local/bin/sip.o"`
		SIPPort                   int           `env:"SIP_EXPORTER_SIP_PORT"                      env-default:"5060"`
		SIPSPort                  int           `env:"SIP_EXPORTER_SIPS_PORT"                     env-default:"5061"`
		CarriersConfigPath        string        `env:"SIP_EXPORTER_CARRIERS_CONFIG"`
		UserAgentsConfigPath      string        `env:"SIP_EXPORTER_USER_AGENTS_CONFIG"`
		IgnoreOutgoing            bool          `env:"SIP_EXPORTER_IGNORE_OUTGOING"               env-default:"false"`
		RTPCapture                bool          `env:"SIP_EXPORTER_RTP_CAPTURE"                   env-default:"true"`
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

func GetConfig() (*App, error) {
	cfg := &App{}

	if err := cleanenv.ReadEnv(cfg); err != nil {
		helpText := "error read env"
		help, _ := cleanenv.GetDescription(cfg, &helpText)
		return nil, fmt.Errorf("err: %s, info: %s", err.Error(), help)
	}

	if err := cfg.validatePorts(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *App) validatePorts() error {
	const minPort, maxPort = 1, 65535
	if c.SIPPort < minPort || c.SIPPort > maxPort {
		return fmt.Errorf("invalid SIP_EXPORTER_SIP_PORT: %d (must be %d-%d)", c.SIPPort, minPort, maxPort)
	}
	if c.SIPSPort < minPort || c.SIPSPort > maxPort {
		return fmt.Errorf("invalid SIP_EXPORTER_SIPS_PORT: %d (must be %d-%d)", c.SIPSPort, minPort, maxPort)
	}
	if c.SIPPort == c.SIPSPort {
		return fmt.Errorf("SIP_EXPORTER_SIP_PORT and SIP_EXPORTER_SIPS_PORT must differ, both: %d", c.SIPPort)
	}
	return nil
}
