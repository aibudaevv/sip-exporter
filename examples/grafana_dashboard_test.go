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
		Instant      bool   `json:"instant"`
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

func TestGrafanaDashboardTrafficAccountingSemantics(t *testing.T) {
	d := readDashboard(t)

	total := panelByID(t, d, 91)
	if total.Type != "bargauge" || total.FieldConfig.Defaults.Unit != "m" || !total.Targets[0].Instant {
		t.Errorf("traffic total = type %q/unit %q/instant %t, want bargauge/m/true",
			total.Type, total.FieldConfig.Defaults.Unit, total.Targets[0].Instant)
	}
	if !strings.Contains(total.Targets[0].Expr, "increase(") ||
		!strings.Contains(total.Targets[0].Expr, "[$__range]") ||
		!strings.Contains(total.Targets[0].Expr, "/ 60") ||
		strings.Contains(total.Targets[0].Expr, "$__rate_interval") {
		t.Errorf("traffic total does not use selected range: %s", total.Targets[0].Expr)
	}

	intensity := panelByID(t, d, 92)
	if intensity.Title != "Completed Traffic Intensity by Carrier" ||
		intensity.FieldConfig.Defaults.Unit != "suffix:Erlang" {
		t.Errorf("traffic intensity = %q/%q, want Completed Traffic Intensity by Carrier/suffix:Erlang",
			intensity.Title, intensity.FieldConfig.Defaults.Unit)
	}
	if !strings.Contains(intensity.Targets[0].Expr, "rate(") ||
		strings.Contains(intensity.Targets[0].Expr, "/ 60") {
		t.Errorf("traffic intensity has wrong dimensional conversion: %s", intensity.Targets[0].Expr)
	}
	for _, part := range []string{"Erlang", "at dialog teardown"} {
		if !strings.Contains(intensity.Description, part) {
			t.Errorf("traffic intensity description lacks %q: %s", part, intensity.Description)
		}
	}
}

func TestGrafanaDashboardOneWayAudioPanels(t *testing.T) {
	d := readDashboard(t)

	ratePanel := panelByID(t, d, 337)
	assertPanel(t, d, 337, "One-Way Audio Calls/s", "ops",
		"rtp_oneway_calls_total", "$__rate_interval")
	ratioPanel := panelByID(t, d, 338)
	assertPanel(t, d, 338, "One-Way Audio Calls %", "percent",
		"100 *", "rtp_oneway_calls_total", "sdc_total", "$__rate_interval")

	for _, p := range []panel{ratePanel, ratioPanel} {
		for _, part := range []string{"two registered media endpoints", "one direction"} {
			if !strings.Contains(p.Description, part) {
				t.Errorf("panel %q description lacks %q: %s", p.Title, part, p.Description)
			}
		}
	}

	positions := map[int]gridPos{
		100: {X: 0, Y: 63, W: 4, H: 5},
		101: {X: 4, Y: 63, W: 4, H: 5},
		102: {X: 8, Y: 63, W: 4, H: 5},
		337: {X: 12, Y: 63, W: 6, H: 5},
		338: {X: 18, Y: 63, W: 6, H: 5},
	}
	for id, want := range positions {
		if got := panelByID(t, d, id).GridPos; got != want {
			t.Errorf("panel %d grid position = %+v, want %+v", id, got, want)
		}
	}
}

func TestGrafanaDashboardZeroEventCountersUseTrafficBaseline(t *testing.T) {
	d := readDashboard(t)
	tests := []struct {
		id             int
		legend         string
		eventMetric    string
		baselineMetric string
	}{
		{id: 9, eventMetric: "sip_exporter_iss_total", baselineMetric: "sip_exporter_invite_total"},
		{id: 13, eventMetric: "sip_exporter_iss_total", baselineMetric: "sip_exporter_invite_total"},
		{id: 16, eventMetric: "sip_exporter_iss_total", baselineMetric: "sip_exporter_invite_total"},
		{id: 17, legend: "ISA", eventMetric: "sip_exporter_iss_total", baselineMetric: "sip_exporter_invite_total"},
		{id: 17, legend: "NER", eventMetric: "sip_exporter_iss_total", baselineMetric: "sip_exporter_invite_total"},
		{id: 337, eventMetric: "sip_exporter_rtp_oneway_calls_total", baselineMetric: "sip_exporter_sdc_total"},
		{id: 338, eventMetric: "sip_exporter_rtp_oneway_calls_total", baselineMetric: "sip_exporter_sdc_total"},
	}

	for _, tt := range tests {
		p := panelByID(t, d, tt.id)
		targetIndex := 0
		if tt.legend != "" {
			targetIndex = slices.IndexFunc(p.Targets, func(target target) bool {
				return target.LegendFormat == tt.legend
			})
			if targetIndex < 0 {
				t.Fatalf("panel %d lacks %s target", tt.id, tt.legend)
			}
		}
		expr := p.Targets[targetIndex].Expr
		fallback := ")) or 0 * sum(rate(" + tt.baselineMetric
		if !strings.Contains(expr, "(sum(rate("+tt.eventMetric) || !strings.Contains(expr, fallback) {
			t.Errorf("panel %d target %q lacks traffic-backed zero fallback: %s",
				tt.id, tt.legend, expr)
		}
	}

	ratioExpr := panelByID(t, d, 338).Targets[0].Expr
	if strings.LastIndex(ratioExpr, "/ sum(rate(sip_exporter_sdc_total") < strings.Index(ratioExpr, "or 0 *") {
		t.Errorf("one-way ratio does not divide the zero-safe numerator by completed sessions: %s", ratioExpr)
	}
}

func TestGrafanaDashboardSelectedFailureCodes(t *testing.T) {
	p := panelByID(t, readDashboard(t), 33)
	if p.Title != "Selected SIP Failure Codes" {
		t.Errorf("failure panel title = %q, want Selected SIP Failure Codes", p.Title)
	}
	for _, metric := range []string{"sip_exporter_481_total", "sip_exporter_502_total"} {
		if !slices.ContainsFunc(p.Targets, func(target target) bool {
			return strings.Contains(target.Expr, metric)
		}) {
			t.Errorf("failure panel lacks %s", metric)
		}
	}
	if !strings.Contains(p.Description, "Authentication challenges are excluded") {
		t.Errorf("failure panel scope is unclear: %s", p.Description)
	}
}

func TestGrafanaDashboardOperationalUnits(t *testing.T) {
	d := readDashboard(t)
	tests := []struct {
		id   int
		unit string
	}{
		{id: 31, unit: "ops"},
		{id: 41, unit: "ops"},
		{id: 42, unit: "ops"},
		{id: 44, unit: "ops"},
		{id: 312, unit: "ops"},
		{id: 315, unit: "ops"},
		{id: 322, unit: "percent"},
	}
	for _, tt := range tests {
		p := panelByID(t, d, tt.id)
		if p.FieldConfig.Defaults.Unit != tt.unit {
			t.Errorf("panel %q unit = %q, want %q", p.Title, p.FieldConfig.Defaults.Unit, tt.unit)
		}
	}
}

func TestGrafanaDashboardSIPRequestSeriesIdentity(t *testing.T) {
	p := panelByID(t, readDashboard(t), 31)
	if len(p.Targets) != 14 {
		t.Fatalf("SIP request target count = %d, want 14", len(p.Targets))
	}
	for _, target := range p.Targets {
		for _, part := range []string{
			"sum by (instance, carrier, direction, ua_type, source_country)",
			"{{instance}}", "{{direction}}", "{{carrier}}", "{{ua_type}}", "{{source_country}}",
		} {
			if !strings.Contains(target.Expr+target.LegendFormat, part) {
				t.Errorf("target %s lacks series identity %q: %s | %s",
					target.RefID, part, target.Expr, target.LegendFormat)
			}
		}
	}
}

func TestGrafanaDashboardInviteOnlyRFCFailureRatios(t *testing.T) {
	d := readDashboard(t)
	targets := []target{
		panelByID(t, d, 9).Targets[0],
		panelByID(t, d, 13).Targets[0],
		panelByID(t, d, 16).Targets[0],
	}
	ratioTrend := panelByID(t, d, 17)
	for _, legend := range []string{"ISA", "NER"} {
		index := slices.IndexFunc(ratioTrend.Targets, func(target target) bool {
			return target.LegendFormat == legend
		})
		if index < 0 {
			t.Fatalf("ratio trend lacks %s target", legend)
		}
		targets = append(targets, ratioTrend.Targets[index])
	}

	for _, target := range targets {
		if !strings.Contains(target.Expr, "sip_exporter_iss_total") ||
			!strings.Contains(target.Expr, "$__rate_interval") {
			t.Errorf("target %s is not windowed INVITE-only ISS: %s", target.RefID, target.Expr)
		}
		for _, generic := range []string{"408_total", "500_total", "503_total", "504_total"} {
			if strings.Contains(target.Expr, generic) {
				t.Errorf("target %s mixes generic SIP responses via %s: %s",
					target.RefID, generic, target.Expr)
			}
		}
	}
}

func TestGrafanaDashboardOmitsWindowedSEER(t *testing.T) {
	d := readDashboard(t)
	if slices.ContainsFunc(childPanels(d), func(p panel) bool { return p.ID == 12 }) {
		t.Error("dashboard still contains windowed SEER panel 12")
	}
	for _, p := range childPanels(d) {
		for _, target := range p.Targets {
			if target.LegendFormat == "SEER" || strings.Contains(target.Expr, "sip_exporter_seer{") {
				t.Errorf("panel %q still exposes unrepresentable windowed SEER: %s", p.Title, target.Expr)
			}
		}
	}

	positions := map[int]gridPos{
		11: {X: 0, Y: 165, W: 5, H: 5},
		13: {X: 5, Y: 165, W: 5, H: 5},
		14: {X: 10, Y: 165, W: 5, H: 5},
		15: {X: 15, Y: 165, W: 5, H: 5},
		16: {X: 20, Y: 165, W: 4, H: 5},
	}
	for id, want := range positions {
		if got := panelByID(t, d, id).GridPos; got != want {
			t.Errorf("panel %d grid position = %+v, want %+v", id, got, want)
		}
	}
	if !strings.Contains(panelByID(t, d, 17).Description, "Windowed SEER is unavailable") {
		t.Errorf("ratio trend does not explain SEER omission: %s", panelByID(t, d, 17).Description)
	}
}

func TestGrafanaDashboardCompleteResponseClasses(t *testing.T) {
	p := panelByID(t, readDashboard(t), 32)
	tests := []struct {
		legend  string
		metrics []string
	}{
		{legend: "1xx Provisional", metrics: []string{"100_total", "180_total", "181_total", "182_total", "183_total"}},
		{legend: "2xx Success", metrics: []string{"200_total", "202_total"}},
		{legend: "3xx Redirection", metrics: []string{"300_total", "302_total"}},
		{legend: "4xx Client Error", metrics: []string{
			"400_total", "401_total", "403_total", "404_total", "405_total",
			"proxy_authentication_required_total", "408_total", "480_total", "481_total",
			"486_total", "487_total", "488_total",
		}},
		{legend: "5xx Server Error", metrics: []string{
			"500_total", "501_total", "502_total", "503_total", "504_total",
		}},
		{legend: "6xx Global Failure", metrics: []string{
			"600_total", "603_total", "604_total", "606_total",
		}},
	}
	for _, tt := range tests {
		index := slices.IndexFunc(p.Targets, func(target target) bool {
			return target.LegendFormat == tt.legend
		})
		if index < 0 {
			t.Fatalf("response-class panel lacks %q", tt.legend)
		}
		expr := p.Targets[index].Expr
		if !strings.Contains(expr, `{__name__=~"`) || strings.Contains(expr, " + sum(rate(") {
			t.Errorf("%s response class is not resilient to absent status series: %s", tt.legend, expr)
		}
		for _, metric := range tt.metrics {
			if !strings.Contains(expr, "sip_exporter_"+metric) {
				t.Errorf("%s response class lacks %s: %s", tt.legend, metric, expr)
			}
		}
	}
	if !strings.Contains(p.Description, "all observed SIP methods") {
		t.Errorf("response-class scope is unclear: %s", p.Description)
	}
}

func TestGrafanaDashboardSPDIsNotLabelledACD(t *testing.T) {
	d := readDashboard(t)
	tests := []struct {
		id    int
		title string
	}{
		{id: 23, title: "SPD p95 (Session Duration)"},
		{id: 27, title: "SPD (Session Duration Percentiles)"},
	}
	for _, tt := range tests {
		p := panelByID(t, d, tt.id)
		if p.Title != tt.title {
			t.Errorf("panel %d title = %q, want %q", tt.id, p.Title, tt.title)
		}
		if strings.Contains(p.Title, "ACD") {
			t.Errorf("percentile panel %d is incorrectly labelled ACD: %s", tt.id, p.Title)
		}
		description := strings.ToLower(p.Description)
		for _, part := range []string{"completed-session duration", "not an arithmetic mean"} {
			if !strings.Contains(description, part) {
				t.Errorf("panel %d description lacks %q: %s", tt.id, part, p.Description)
			}
		}
	}
}

func TestGrafanaDashboardWindowedRatios(t *testing.T) {
	d := readDashboard(t)
	for _, id := range []int{11, 13, 14, 15, 16, 17} {
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
