package config

import (
	"fmt"
	"github.com/ilyakaznacheev/cleanenv"
)

type (
	App struct {
		Level         string `env:"SIP_EXPORTER_LOGGER_LEVEL" env-default:"info"`
		Port          string `env:"SIP_EXPORTER_HTTP_PORT" env-default:"2112"`
		Interface     string `env:"SIP_EXPORTER_INTERFACE" env-required:"true"`
		BPFBinaryPath string `env:"SIP_EXPORTER_OBJECT_FILE_PATH" env-default:"/usr/local/bin/sip.o"`
	}
)

func GetConfig() (*App, error) {
	cfg := &App{}

	if err := cleanenv.ReadEnv(cfg); err != nil {
		helpText := "error read env"
		help, _ := cleanenv.GetDescription(cfg, &helpText)
		return nil, fmt.Errorf("err: %s, info: %s", err.Error(), help)
	}

	return cfg, nil
}
