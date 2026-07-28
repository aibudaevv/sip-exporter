package examples

import (
	"encoding/json"
	"os"
	"testing"
)

type dashboard struct {
	Panels []panel `json:"panels"`
}

type panel struct {
	ID      int     `json:"id"`
	Title   string  `json:"title"`
	Type    string  `json:"type"`
	GridPos gridPos `json:"gridPos"`
	Panels  []panel `json:"panels"`
}

type gridPos struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func readDashboard(t *testing.T) dashboard {
	t.Helper()

	b, err := os.ReadFile("grafana-dashboard.json")
	if err != nil {
		t.Fatal(err)
	}

	var d dashboard
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}

	return d
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
