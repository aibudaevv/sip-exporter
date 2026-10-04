package examples

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestDocumentedPromQL evaluates the published rules and queries, not copies of
// their expressions. The opt-in target requires Docker and the pinned promtool.
func TestDocumentedPromQL(t *testing.T) {
	if os.Getenv("SIP_EXPORTER_TEST_PROMQL") != "true" {
		t.Skip("run make test-docs-promql to evaluate documentation with promtool")
	}
	for _, language := range []string{"", ".ru"} {
		for _, fraudGuide := range []bool{false, true} {
			t.Run(fmt.Sprintf("language=%s/fraud=%t", language, fraudGuide), func(t *testing.T) {
				runDocumentedPromQL(t, language, fraudGuide)
			})
		}
	}
}

func runDocumentedPromQL(t *testing.T, language string, fraudGuide bool) {
	t.Helper()
	dir := t.TempDir()
	rules := documentAlerts(t, "ALERTING"+language+".md")
	if fraudGuide {
		for name, rule := range documentAlerts(t, "fraud-detection"+language+".md") {
			rules[name] = rule
		}
	}
	queries := documentQueries(t, "METRICS"+language+".md")
	var suite yaml.Node
	if err := yaml.Unmarshal(readExample(t, "prometheus-behavior.test.yml"), &suite); err != nil {
		t.Fatal(err)
	}
	resolveDocumentExpressions(t, &suite, queries, rules)
	writeTestYAML(t, dir, "tests.yml", &suite)
	var extracted []alertRule
	for _, rule := range rules {
		extracted = append(extracted, rule)
	}
	writeTestYAML(t, dir, "documented-alerts.yml", map[string]any{
		"groups": []any{map[string]any{"name": "documented", "rules": extracted}},
	})
	for _, name := range []string{"prometheus-recording-rules.yml", "prometheus-alerts.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), readExample(t, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--network=none", "--user", "0:0",
		"-v", dir+":/work:ro", "-w", "/work", "--entrypoint", "promtool",
		"prom/prometheus:v3.13.0", "test", "rules", "tests.yml")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("promtool: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func readExample(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeTestYAML(t *testing.T, dir, name string, value any) {
	t.Helper()
	data, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func documentAlerts(t *testing.T, name string) map[string]alertRule {
	t.Helper()
	result := make(map[string]alertRule)
	text := string(readExample(t, filepath.Join("..", "docs", name)))
	for _, block := range strings.Split(text, "```yaml")[1:] {
		block, _, _ = strings.Cut(block, "```")
		if !strings.Contains(block, "- alert:") {
			continue
		}
		var root yaml.Node
		if err := yaml.Unmarshal([]byte(block), &root); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		collectDocumentAlerts(t, &root, result)
	}
	return result
}

func collectDocumentAlerts(t *testing.T, node *yaml.Node, result map[string]alertRule) {
	t.Helper()
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == "alert" {
				var rule alertRule
				if err := node.Decode(&rule); err != nil {
					t.Fatal(err)
				}
				result[rule.Alert] = rule
				return
			}
		}
	}
	for _, child := range node.Content {
		collectDocumentAlerts(t, child, result)
	}
}

// Example IDs live in executable code-block comments and are language-neutral.
func documentQueries(t *testing.T, name string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	text := string(readExample(t, filepath.Join("..", "docs", name)))
	for _, block := range strings.Split(text, "```promql")[1:] {
		block, _, _ = strings.Cut(block, "```")
		id := ""
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if value, ok := strings.CutPrefix(line, "# example: "); ok {
				id = value
				continue
			}
			if line == "" || strings.HasPrefix(line, "#") {
				id = ""
				continue
			}
			if id != "" {
				result[id] += " " + line
			}
		}
	}
	return result
}

func resolveDocumentExpressions(t *testing.T, node *yaml.Node, queries map[string]string, rules map[string]alertRule) {
	t.Helper()
	if node.Kind != yaml.ScalarNode {
		for _, child := range node.Content {
			resolveDocumentExpressions(t, child, queries, rules)
		}
		return
	}
	if id, ok := strings.CutPrefix(node.Value, "doc:"); ok {
		value, found := queries[id]
		if !found {
			t.Fatalf("missing documented query %q", id)
		}
		node.Value = value
		return
	}
	if id, ok := strings.CutPrefix(node.Value, "alert:"); ok {
		rule, found := rules[id]
		if !found {
			t.Fatalf("missing documented alert %q", id)
		}
		node.Value = rule.Expr
	}
}
