package examples

import (
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type productionCompose struct {
	Services map[string]struct {
		Environment map[string]string `yaml:"environment"`
		Healthcheck struct {
			Test []string `yaml:"test"`
		} `yaml:"healthcheck"`
		Image       string   `yaml:"image"`
		NetworkMode string   `yaml:"network_mode"`
		Privileged  bool     `yaml:"privileged"`
		ReadOnly    bool     `yaml:"read_only"`
		Restart     string   `yaml:"restart"`
		Volumes     []string `yaml:"volumes"`
	} `yaml:"services"`
	Volumes map[string]any `yaml:"volumes"`
}

func TestProductionCompose(t *testing.T) {
	b, readErr := os.ReadFile("docker-compose.production.yml")
	if readErr != nil {
		t.Fatal(readErr)
	}
	versionBytes, readErr := os.ReadFile("../VERSION")
	if readErr != nil {
		t.Fatal(readErr)
	}

	var compose productionCompose
	if unmarshalErr := yaml.Unmarshal(b, &compose); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	service, ok := compose.Services["sip-exporter"]
	if !ok {
		t.Fatal("missing sip-exporter service")
	}
	wantImage := "frzq/sip-exporter:" + strings.TrimSpace(string(versionBytes))
	if service.Image != wantImage {
		t.Fatalf("production image = %q, want current release %q", service.Image, wantImage)
	}
	_, stateVolumeExists := compose.Volumes["sip-exporter-state"]
	wantEnvironment := map[string]string{
		"SIP_EXPORTER_HTTP_PORT":                     "10047",
		"SIP_EXPORTER_LOGGER_LEVEL":                  "info",
		"SIP_EXPORTER_SIP_PORTS":                     "5060",
		"SIP_EXPORTER_OBJECT_FILE_PATH":              "/usr/local/bin/sip.o",
		"SIP_EXPORTER_CARRIERS_CONFIG":               "",
		"SIP_EXPORTER_USER_AGENTS_CONFIG":            "",
		"SIP_EXPORTER_RTP_STREAM_TTL":                "30s",
		"SIP_EXPORTER_IGNORE_OUTGOING":               "false",
		"SIP_EXPORTER_TELEMETRY":                     "true",
		"SIP_EXPORTER_TELEMETRY_URL":                 "https://telemetry.sip-exporter.com/v1/beacon",
		"SIP_EXPORTER_TELEMETRY_ID_FILE":             "/var/lib/sip-exporter/anon_id",
		"SIP_EXPORTER_GEOIP_COUNTRY_DB":              "",
		"SIP_EXPORTER_LOCAL_COUNTRY_CODE":            "",
		"SIP_EXPORTER_HOST_LABELS":                   "false",
		"SIP_EXPORTER_SESSIONS_LIMITS":               "",
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD": "10",
		"SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW":    "60s",
		"SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD":  "100",
		"SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW":     "60s",
		"SIP_EXPORTER_FRAUD_FAS_THRESHOLD":           "10s",
	}

	tests := []struct {
		name string
		ok   bool
	}{
		{"single service", len(compose.Services) == 1},
		{"host network", service.NetworkMode == "host"},
		{"capture privileges", service.Privileged},
		{"restart policy", service.Restart == "unless-stopped"},
		{"read-only root filesystem", service.ReadOnly},
		{"required interface", strings.Contains(service.Environment["SIP_EXPORTER_INTERFACE"], ":?")},
		{
			"health endpoint",
			strings.Contains(strings.Join(service.Healthcheck.Test, " "), "http://127.0.0.1:10047/health"),
		},
		{"telemetry identity mount", slices.Contains(service.Volumes, "sip-exporter-state:/var/lib/sip-exporter")},
		{"telemetry identity volume", stateVolumeExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatalf("production Compose violates %s contract", tt.name)
			}
		})
	}
	for key, want := range wantEnvironment {
		t.Run("environment "+key, func(t *testing.T) {
			if got := service.Environment[key]; got != want {
				t.Fatalf("environment %s = %q, want %q", key, got, want)
			}
		})
	}
}

func TestPublicRepositoryLinks(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"README English", "../README.md"},
		{"README Russian", "../README.ru.md"},
		{"Prometheus alerts", "prometheus-alerts.yml"},
		{"Grafana dashboard", "grafana-dashboard.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			for _, branch := range []string{"main", "develop"} {
				if strings.Contains(string(b), "blob/"+branch) {
					t.Errorf("%s contains mutable or nonexistent branch link blob/%s", tt.path, branch)
				}
			}
		})
	}
}

func TestCanonicalDockerPullUsesCurrentVersion(t *testing.T) {
	versionBytes, err := os.ReadFile("../VERSION")
	if err != nil {
		t.Fatal(err)
	}
	want := "docker pull frzq/sip-exporter:" + strings.TrimSpace(string(versionBytes))
	tests := []struct {
		name    string
		path    string
		heading string
	}{
		{"English", "../README.md", "## Install"},
		{"Russian", "../README.ru.md", "## Установка"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, readErr := os.ReadFile(tt.path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			content := string(b)
			start := strings.Index(content, tt.heading)
			if start < 0 {
				t.Fatalf("%s is missing %q section", tt.path, tt.heading)
			}
			section := content[start+len(tt.heading):]
			if end := strings.Index(section, "\n## "); end >= 0 {
				section = section[:end]
			}
			if strings.Contains(section, "docker pull frzq/sip-exporter:latest") {
				t.Errorf("%s contains mutable canonical Docker pull", tt.path)
			}
			if count := strings.Count(section, want); count != 1 {
				t.Errorf("%s canonical Docker pull count = %d, want 1 for %q", tt.path, count, want)
			}
		})
	}
}

func TestCanonicalSupportLinks(t *testing.T) {
	const issuesURL = "https://github.com/aibudaevv/sip-exporter/issues"
	tests := []struct {
		name    string
		path    string
		heading string
	}{
		{"English", "../README.md", "## Support"},
		{"Russian", "../README.ru.md", "## Поддержка"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			content := string(b)
			start := strings.Index(content, tt.heading)
			if start < 0 {
				t.Fatalf("%s is missing %q section", tt.path, tt.heading)
			}
			section := content[start+len(tt.heading):]
			if end := strings.Index(section, "\n## "); end >= 0 {
				section = section[:end]
			}
			if strings.Count(section, issuesURL) != 1 || strings.Count(section, "https://") != 1 {
				t.Errorf("%s support section must link only to %s", tt.path, issuesURL)
			}
		})
	}
}
