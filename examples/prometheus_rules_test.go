package examples

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type recordingRules struct {
	Groups []struct {
		Name  string          `yaml:"name"`
		Rules []recordingRule `yaml:"rules"`
	} `yaml:"groups"`
}

type (
	recordingRule struct {
		Expr   string `yaml:"expr"`
		Record string `yaml:"record"`
	}
	expectedRecordingRule struct {
		name   string
		labels string
	}
)

func TestRecordingRules(t *testing.T) {
	b, readErr := os.ReadFile("prometheus-recording-rules.yml")
	if readErr != nil {
		t.Fatal(readErr)
	}

	var rules recordingRules
	if unmarshalErr := yaml.Unmarshal(b, &rules); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	if len(rules.Groups) != 1 || rules.Groups[0].Name != "sip_exporter_recording" {
		t.Fatal("missing sip_exporter_recording group")
	}

	got := make(map[string]string, len(rules.Groups[0].Rules))
	for _, rule := range rules.Groups[0].Rules {
		got[rule.Record] = rule.Expr
	}
	tests := []expectedRecordingRule{
		{"sip_exporter:invite_rate:5m", "instance, carrier, direction"},
		{"sip_exporter:ser_percent:5m", "instance, carrier, direction"},
		{"sip_exporter:asr_percent:5m", "instance, carrier, direction"},
		{"sip_exporter:rtp_loss_percent:5m", "instance, carrier, direction"},
		{"sip_exporter:missing_rtp_rate:5m", "instance, carrier, direction"},
		{"sip_exporter:socket_drop_percent:5m", "instance, iface"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRecordingRule(t, got, tt)
		})
	}
}

func assertRecordingRule(t *testing.T, got map[string]string, want expectedRecordingRule) {
	t.Helper()

	expr, ok := got[want.name]
	if !ok {
		t.Fatalf("missing recording rule %q", want.name)
	}
	for _, required := range []string{"[5m]", "sum by (" + want.labels + ")"} {
		if !strings.Contains(expr, required) {
			t.Fatalf("rule %q lacks %q: %s", want.name, required, expr)
		}
	}
	for _, forbidden := range []string{"$__rate_interval", "caller_host", "called_host", "call_id"} {
		if strings.Contains(expr, forbidden) {
			t.Fatalf("rule %q contains forbidden %q: %s", want.name, forbidden, expr)
		}
	}
}
