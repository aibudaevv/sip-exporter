package service

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type sessionsLimitsConfig struct {
	SessionsLimits []struct {
		Carrier string `yaml:"carrier"`
		Limit   int    `yaml:"limit"`
	} `yaml:"sessions_limits"`
}

// LoadSessionsLimits reads a YAML file mapping carrier names to session limits.
func LoadSessionsLimits(path string) (map[string]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sessions limits: %w", err)
	}

	var cfg sessionsLimitsConfig
	if unmarshalErr := yaml.Unmarshal(data, &cfg); unmarshalErr != nil {
		return nil, fmt.Errorf("parse sessions limits: %w", unmarshalErr)
	}

	limits := make(map[string]int, len(cfg.SessionsLimits))
	for _, sl := range cfg.SessionsLimits {
		limits[sl.Carrier] = sl.Limit
	}

	return limits, nil
}
