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
	alertRules struct {
		Groups []struct {
			Name  string      `yaml:"name"`
			Rules []alertRule `yaml:"rules"`
		} `yaml:"groups"`
	}
	alertRule struct {
		Alert       string            `yaml:"alert"`
		Expr        string            `yaml:"expr"`
		For         string            `yaml:"for"`
		Labels      map[string]string `yaml:"labels"`
		Annotations map[string]string `yaml:"annotations"`
	}
	recordingRule struct {
		Expr   string `yaml:"expr"`
		Record string `yaml:"record"`
	}
	expectedRecordingRule struct {
		name   string
		labels string
	}
	expectedAlertRule struct {
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
	if !strings.Contains(got["sip_exporter:ser_percent:5m"], "sip_exporter_invite_3xx_total") {
		t.Fatalf("SER recording rule excludes only selected redirects: %s",
			got["sip_exporter:ser_percent:5m"])
	}
	for _, fallbackPart := range []string{
		" or ", "sip_exporter_300_total", "sip_exporter_302_total",
	} {
		if !strings.Contains(got["sip_exporter:ser_percent:5m"], fallbackPart) {
			t.Fatalf("SER recording rule lacks rolling-upgrade fallback %q: %s",
				fallbackPart, got["sip_exporter:ser_percent:5m"])
		}
	}
	if strings.Contains(got["sip_exporter:ser_percent:5m"], "clamp_min") {
		t.Fatalf("SER recording rule clamps a rate denominator and distorts low traffic: %s",
			got["sip_exporter:ser_percent:5m"])
	}
	for _, name := range []string{
		"sip_exporter:asr_percent:5m",
		"sip_exporter:socket_drop_percent:5m",
	} {
		if strings.Contains(got[name], "clamp_min") {
			t.Fatalf("recording rule %q clamps a rate denominator and distorts low traffic: %s",
				name, got[name])
		}
	}
	rtpLossExpr := got["sip_exporter:rtp_loss_percent:5m"]
	if strings.Count(rtpLossExpr, "sip_exporter_rtp_packets_lost_total") < 2 {
		t.Fatalf("RTP loss denominator excludes inferred losses: %s", rtpLossExpr)
	}
	if strings.Contains(rtpLossExpr, "clamp_min") {
		t.Fatalf("RTP loss recording rule clamps a rate denominator and distorts low traffic: %s",
			rtpLossExpr)
	}
}

func TestAlertRules(t *testing.T) {
	b, readErr := os.ReadFile("prometheus-alerts.yml")
	if readErr != nil {
		t.Fatal(readErr)
	}

	var rules alertRules
	if unmarshalErr := yaml.Unmarshal(b, &rules); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	if len(rules.Groups) != 1 || rules.Groups[0].Name != "sip_exporter_alerts" {
		t.Fatal("missing sip_exporter_alerts group")
	}

	got := make(map[string]alertRule, len(rules.Groups[0].Rules))
	for _, rule := range rules.Groups[0].Rules {
		got[rule.Alert] = rule
	}
	tests := []expectedAlertRule{
		{"SIPExporterDown", ""},
		{"SIPExporterSocketDropsHigh", ""},
		{"SIPExporterChannelSaturation", ""},
		{"SIPExporterSERDegraded", ""},
		{"SIPExporterRTPLossHigh", ""},
		{"SIPExporterMissingRTPHigh", "instance, carrier, direction"},
		{"SIPExporterOneWayRTPHigh", "instance, carrier, direction"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertAlertRule(t, got, tt)
		})
	}
	for _, name := range []string{"SIPExporterMissingRTPHigh", "SIPExporterOneWayRTPHigh"} {
		if strings.Contains(got[name].Expr, "clamp_min") {
			t.Fatalf("alert %q clamps a rate denominator and distorts low traffic: %s",
				name, got[name].Expr)
		}
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

func assertAlertRule(t *testing.T, got map[string]alertRule, want expectedAlertRule) {
	t.Helper()

	rule, ok := got[want.name]
	if !ok {
		t.Fatalf("missing alert rule %q", want.name)
	}
	if rule.For == "" {
		t.Fatalf("alert %q has empty for", want.name)
	}
	if severity := rule.Labels["severity"]; severity != "warning" && severity != "critical" {
		t.Fatalf("alert %q has invalid severity %q", want.name, severity)
	}
	for _, annotation := range []string{"summary", "description", "runbook_url"} {
		if rule.Annotations[annotation] == "" {
			t.Fatalf("alert %q has empty %q annotation", want.name, annotation)
		}
	}
	if want.labels != "" && !strings.Contains(rule.Expr, "sum by ("+want.labels+")") {
		t.Fatalf("alert %q lacks aggregation labels %q: %s", want.name, want.labels, rule.Expr)
	}
	for _, forbidden := range []string{"caller_host", "called_host", "call_id"} {
		if strings.Contains(rule.Expr, forbidden) {
			t.Fatalf("alert %q contains forbidden %q: %s", want.name, forbidden, rule.Expr)
		}
	}
}
