package examples

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

type dashboard struct {
	Annotations annotations `json:"annotations"`
	Panels      []panel     `json:"panels"`
	Refresh     string      `json:"refresh"`
	Templating  templating  `json:"templating"`
}

type panel struct {
	Description string       `json:"description"`
	FieldConfig fieldConfig  `json:"fieldConfig"`
	ID          int          `json:"id"`
	Options     panelOptions `json:"options"`
	Title       string       `json:"title"`
	Type        string       `json:"type"`
	GridPos     gridPos      `json:"gridPos"`
	Panels      []panel      `json:"panels"`
	Targets     []target     `json:"targets"`
	Collapsed   bool         `json:"collapsed"`
}

type (
	annotations struct {
		List []annotation `json:"list"`
	}
	annotation struct {
		Name string `json:"name"`
	}
	templating struct {
		List []variable `json:"list"`
	}
	variable struct {
		IncludeAll bool   `json:"includeAll"`
		Multi      bool   `json:"multi"`
		Name       string `json:"name"`
		Query      any    `json:"query"`
	}
	target struct {
		Expr         string `json:"expr"`
		LegendFormat string `json:"legendFormat"`
		RefID        string `json:"refId"`
	}
	fieldConfig struct {
		Defaults fieldDefaults `json:"defaults"`
	}
	panelOptions struct {
		Content string `json:"content"`
	}
	fieldDefaults struct {
		Thresholds thresholds `json:"thresholds"`
		Unit       string     `json:"unit"`
	}
	thresholds struct {
		Steps []thresholdStep `json:"steps"`
	}
	thresholdStep struct {
		Value *float64 `json:"value"`
	}
)

type gridPos struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func readDashboard(t *testing.T) dashboard {
	t.Helper()
	return readDashboardFile(t, "grafana-dashboard.json")
}

func readDashboardFile(t *testing.T, path string) dashboard {
	t.Helper()

	b, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}

	var d dashboard
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}

	return d
}

func TestGrafanaDashboardLowTrafficRatios(t *testing.T) {
	d := readDashboard(t)
	for _, id := range []int{331, 43} {
		p := panelByID(t, d, id)
		if strings.Contains(p.Targets[0].Expr, "clamp_min") {
			t.Fatalf("panel %q clamps a rate denominator and distorts low traffic: %s",
				p.Title, p.Targets[0].Expr)
		}
	}

	billable := readDashboardFile(t, "grafana-dashboard-billable-demo.json")
	p := panelByID(t, billable, 20)
	if strings.Contains(p.Targets[0].Expr, "clamp_min") {
		t.Fatalf("panel %q clamps a rate denominator and distorts low traffic: %s",
			p.Title, p.Targets[0].Expr)
	}
}

func TestGrafanaDashboardUniqueIDs(t *testing.T) {
	d := readDashboard(t)
	ids := make(map[int]string)

	for _, row := range d.Panels {
		for _, p := range append([]panel{row}, row.Panels...) {
			if previous, ok := ids[p.ID]; ok {
				t.Fatalf("duplicate panel ID %d: %q and %q", p.ID, previous, p.Title)
			}
			ids[p.ID] = p.Title
		}
	}
}

func TestGrafanaDashboardNoPanelOverlap(t *testing.T) {
	d := readDashboard(t)

	for _, row := range d.Panels {
		for i, left := range row.Panels {
			for _, right := range row.Panels[i+1:] {
				if overlaps(left.GridPos, right.GridPos) {
					t.Fatalf("row %q: panels %q and %q overlap", row.Title, left.Title, right.Title)
				}
			}
		}
	}
}

func overlaps(left, right gridPos) bool {
	return left.X < right.X+right.W && right.X < left.X+left.W &&
		left.Y < right.Y+right.H && right.Y < left.Y+left.H
}

func TestGrafanaDashboardRowHierarchy(t *testing.T) {
	d := readDashboard(t)
	want := []string{
		"Overview", "SIP Traffic", "RTP Media Analysis", "System Health",
		"RFC 6076 Ratios", "Latency Histograms", "Voice Quality (RFC 6035)",
		"Registrations", "Traffic Minutes", "Geographic Distribution", "Multi-NIC",
	}

	for i, title := range want {
		if d.Panels[i].Title != title {
			t.Fatalf("row %d = %q, want %q", i, d.Panels[i].Title, title)
		}
		if d.Panels[i].Collapsed == (i == 0) {
			t.Fatalf("row %q collapsed=%t, want %t", title, d.Panels[i].Collapsed, i != 0)
		}
	}
}

func TestGrafanaDashboardInstanceFilter(t *testing.T) {
	d := readDashboard(t)
	instance, ok := variableByName(d, "instance")
	if !ok || !instance.IncludeAll || !instance.Multi {
		t.Fatal("missing multi-select instance variable")
	}

	for _, p := range childPanels(d) {
		for _, target := range p.Targets {
			if strings.Contains(target.Expr, "sip_exporter_") &&
				!strings.Contains(target.Expr, `instance=~"$instance"`) {
				t.Fatalf("panel %q target lacks instance filter: %s", p.Title, target.Expr)
			}
		}
	}
}

func TestGrafanaDashboardAdaptiveWindows(t *testing.T) {
	d := readDashboard(t)
	literalWindow := regexp.MustCompile(`\[(?:1m|5m)\]`)

	for _, p := range childPanels(d) {
		for _, target := range p.Targets {
			if literalWindow.MatchString(target.Expr) {
				t.Fatalf("panel %q uses fixed window: %s", p.Title, target.Expr)
			}
		}
	}
}

func TestGrafanaDashboardOperationalHelp(t *testing.T) {
	d := readDashboard(t)

	for _, p := range childPanels(d) {
		if strings.TrimSpace(p.Description) == "" || strings.Contains(p.Description, "TODO") {
			t.Fatalf("panel %q lacks operational description", p.Title)
		}
	}

	if !slices.ContainsFunc(d.Annotations.List, func(a annotation) bool {
		return a.Name == "Exporter restarts"
	}) {
		t.Fatal("missing Exporter restarts annotation")
	}
}

func TestGrafanaDashboardVerificationEntryPoint(t *testing.T) {
	d := readDashboard(t)
	overview, ok := rowByTitle(d, "Overview")
	if !ok {
		t.Fatal("Overview row is missing")
	}

	for _, p := range overview.Panels {
		if p.Title != "Verify dashboard data" {
			continue
		}
		if p.Type != "text" {
			t.Fatalf("verification entry point type = %q, want text", p.Type)
		}
		for _, anchor := range []string{
			"#verify-scrape", "#verify-sip", "#verify-dialog-sdp", "#verify-drops",
		} {
			if !strings.Contains(p.Options.Content, anchor) {
				t.Fatalf("verification entry point lacks %q: %s", anchor, p.Options.Content)
			}
		}
		return
	}
	t.Fatal("verification entry point is missing")
}

func TestGrafanaDashboardKPIContracts(t *testing.T) {
	d := readDashboard(t)

	assertPanel(t, d, 102, "RTP Packet Loss %", "percent", "rtp_packets_lost_total", "$__rate_interval")
	assertPanel(t, d, 317, "Suspected FAS Calls/s", "ops", "fas_calls_total", "$__rate_interval")
	assertPanel(t, d, 301, "REGISTER Success %", "percent", "register_failure_total", "$__rate_interval")
	assertPanel(t, d, 7, "Median Session Duration", "s", "spd_bucket", "$__rate_interval")

	overview, ok := rowByTitle(d, "Overview")
	if !ok {
		t.Fatal("Overview row is missing")
	}
	for _, title := range []string{"RTP Packet Loss %", "RTP MOS", "Socket Drop %", "RTP Userspace Drops/s", "Missing RTP Dialogs/s"} {
		if !slices.ContainsFunc(overview.Panels, func(p panel) bool { return p.Title == title }) {
			t.Fatalf("overview KPI %q is missing", title)
		}
	}
}

func TestGrafanaDashboardPDDThresholds(t *testing.T) {
	d := readDashboard(t)
	for _, id := range []int{6, 60} {
		p := panelByID(t, d, id)
		steps := p.FieldConfig.Defaults.Thresholds.Steps
		if len(steps) != 3 || steps[1].Value == nil || steps[2].Value == nil ||
			*steps[1].Value != 1000 || *steps[2].Value != 3000 {
			t.Fatalf("panel %q has incompatible PDD thresholds", p.Title)
		}
	}
}

func TestGrafanaDashboardInterfaceScope(t *testing.T) {
	d := readDashboard(t)
	for _, id := range []int{4, 309, 310} {
		p := panelByID(t, d, id)
		if !strings.Contains(p.Targets[0].Expr, `iface=~"$iface"`) {
			t.Fatalf("panel %q target lacks interface filter: %s", p.Title, p.Targets[0].Expr)
		}
	}
}

func TestGrafanaDashboardOverviewRatioFilterScope(t *testing.T) {
	d := readDashboard(t)

	t.Run("ASR applies interface to both operands", func(t *testing.T) {
		asr := panelByID(t, d, 8)
		if got := strings.Count(asr.Targets[0].Expr, `iface=~"$iface"`); got != 2 {
			t.Fatalf("interface selector count = %d, want 2: %s", got, asr.Targets[0].Expr)
		}
	})

	ner := panelByID(t, d, 9)
	for _, unsupported := range []string{`iface=~"$iface"`, `destination_country=~"$destination_country"`} {
		t.Run("NER excludes "+unsupported, func(t *testing.T) {
			if strings.Contains(ner.Targets[0].Expr, unsupported) {
				t.Fatalf("query applies unsupported filter %q: %s", unsupported, ner.Targets[0].Expr)
			}
		})
	}
	for _, label := range []string{"Interface", "Destination Country", "do not apply"} {
		t.Run("NER describes "+label, func(t *testing.T) {
			if !strings.Contains(ner.Description, label) {
				t.Fatalf("description lacks %q: %s", label, ner.Description)
			}
		})
	}
}

func TestGrafanaDashboardWindowedRatios(t *testing.T) {
	d := readDashboard(t)
	for id := 11; id <= 17; id++ {
		p := panelByID(t, d, id)
		for _, target := range p.Targets {
			if usesLifetimeRatio(target.Expr) {
				t.Fatalf("panel %q uses lifetime ratio gauge", p.Title)
			}
			if !strings.Contains(target.Expr, "$__rate_interval") {
				t.Fatalf("panel %q is not windowed", p.Title)
			}
		}
	}
}

func TestGrafanaDashboardWindowedSERUsesAllInviteRedirects(t *testing.T) {
	d := readDashboard(t)
	for _, id := range []int{11, 17} {
		p := panelByID(t, d, id)
		serTarget := slices.IndexFunc(p.Targets, func(target target) bool {
			return target.LegendFormat == "SER"
		})
		if serTarget < 0 {
			t.Fatalf("panel %q lacks SER target", p.Title)
		}
		expr := p.Targets[serTarget].Expr
		if !strings.Contains(expr, "sip_exporter_invite_3xx_total") {
			t.Fatalf("panel %q SER excludes only selected redirects: %s", p.Title, expr)
		}
		for _, fallbackPart := range []string{
			"sum(sum by (instance, carrier, direction, ua_type, source_country)",
			" or ", "sip_exporter_300_total", "sip_exporter_302_total",
		} {
			if !strings.Contains(expr, fallbackPart) {
				t.Fatalf("panel %q SER lacks rolling-upgrade fallback %q: %s",
					p.Title, fallbackPart, expr)
			}
		}
	}
}

func TestGrafanaDashboardOptionalDataDescriptions(t *testing.T) {
	d := readDashboard(t)
	for _, p := range childPanels(d) {
		if strings.HasPrefix(p.Title, "RTCP") && !strings.Contains(p.Description, "Endpoint-reported") {
			t.Fatalf("RTCP panel %q does not explain endpoint data", p.Title)
		}
	}
	if !strings.Contains(panelByID(t, d, 334).Description, "Optional endpoint-published") {
		t.Fatal("VQ report panel does not explain optional source")
	}
}

func TestGrafanaDashboardSymmetricRTPAliasContract(t *testing.T) {
	d := readDashboard(t)
	p := panelByID(t, d, 336)
	if p.Title != "Symmetric RTP Aliases" || p.Type != "timeseries" ||
		p.FieldConfig.Defaults.Unit != "short" {
		t.Fatalf("panel 336 = %q/%q/%q, want Symmetric RTP Aliases/timeseries/short",
			p.Title, p.Type, p.FieldConfig.Defaults.Unit)
	}
	if p.GridPos != (gridPos{X: 12, Y: 132, W: 12, H: 8}) {
		t.Fatalf("panel 336 grid position = %+v", p.GridPos)
	}
	if len(p.Targets) != 2 {
		t.Fatalf("panel 336 target count = %d, want 2", len(p.Targets))
	}
	if p.Targets[0] != (target{
		RefID:        "A",
		Expr:         `sum by (carrier, direction, type) (rate(sip_exporter_rtp_endpoint_mismatch_total{instance=~"$instance",carrier=~"$carrier",direction=~"$direction",type="source_port"}[$__rate_interval]))`,
		LegendFormat: "new {{carrier}} / {{direction}} / {{type}} remaps/s",
	}) {
		t.Fatalf("remap target = %+v", p.Targets[0])
	}
	if p.Targets[1] != (target{
		RefID:        "B",
		Expr:         `sum by (carrier, direction) (sip_exporter_rtp_alias_active{instance=~"$instance",carrier=~"$carrier",direction=~"$direction"})`,
		LegendFormat: "active {{carrier}} / {{direction}} aliases",
	}) {
		t.Fatalf("active-alias target = %+v", p.Targets[1])
	}
	if !strings.Contains(p.Description, "source-port") || !strings.Contains(p.Description, "IP") {
		t.Fatalf("panel 336 description lacks operational scope: %s", p.Description)
	}
}

func childPanels(d dashboard) []panel {
	var panels []panel
	for _, row := range d.Panels {
		panels = append(panels, row.Panels...)
	}
	return panels
}

func variableByName(d dashboard, name string) (variable, bool) {
	for _, v := range d.Templating.List {
		if v.Name == name {
			return v, true
		}
	}
	return variable{}, false
}

func rowByTitle(d dashboard, title string) (panel, bool) {
	for _, p := range d.Panels {
		if p.Title == title {
			return p, true
		}
	}
	return panel{}, false
}

func panelByID(t *testing.T, d dashboard, id int) panel {
	t.Helper()
	for _, p := range d.Panels {
		if p.ID == id {
			return p
		}
	}
	for _, p := range childPanels(d) {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("panel %d not found", id)
	return panel{}
}

func usesLifetimeRatio(expr string) bool {
	for _, name := range []string{"ser", "seer", "isa", "scr", "asr", "ner"} {
		if strings.Contains(expr, "sip_exporter_"+name+"{") {
			return true
		}
	}
	return false
}

func assertPanel(t *testing.T, d dashboard, id int, title, unit string, queryParts ...string) {
	t.Helper()
	for _, p := range childPanels(d) {
		if p.ID != id {
			continue
		}
		if p.Title != title || p.FieldConfig.Defaults.Unit != unit {
			t.Fatalf("panel %d = %q/%q, want %q/%q", id, p.Title, p.FieldConfig.Defaults.Unit, title, unit)
		}
		for _, part := range queryParts {
			if !strings.Contains(p.Targets[0].Expr, part) {
				t.Fatalf("panel %q query lacks %q: %s", title, part, p.Targets[0].Expr)
			}
		}
		return
	}
	t.Fatalf("panel %d not found", id)
}
