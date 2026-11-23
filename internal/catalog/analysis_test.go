package catalog

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// sourcesFixture reads the sources as Orrery lists them.
func sourcesFixture(t *testing.T) []Source {
	t.Helper()
	raw, err := os.ReadFile("testdata/sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var page struct{ Sources []Source }
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	return page.Sources
}

func TestIntegrationIsTheUnionOfWhatItsSourcesWrote(t *testing.T) {
	l := WithSources(landscape(t, ""), sourcesFixture(t), true)
	atlas := cardOf(t, l, "git/example/atlas").Integration
	if atlas == nil {
		t.Fatal("a card with known sources has an integration")
	}
	for name, c := range map[string]struct {
		got           Evidence
		items, source string
	}{
		"data":       {atlas.Data, "inv_host,inv_thing", "atlas,atlas-feed"},
		"blueprints": {atlas.Blueprints, "inv_thing", "atlas"},
		"actions":    {atlas.Actions, "atlas.sync", "atlas"},
	} {
		if !c.got.On() || strings.Join(c.got.Items, ",") != c.items || strings.Join(c.got.Sources, ",") != c.source {
			t.Errorf("atlas %s is %+v, want %s from %s", name, c.got, c.items, c.source)
		}
	}
	// A source that wrote nothing sets no mark.
	ember := cardOf(t, l, "git/example/ember").Integration
	for _, m := range ember.Marks() {
		if m.On() {
			t.Errorf("ember's %s mark is on: %+v", m.Kind.Key, m.Evidence)
		}
	}
	// A service written is data like any other blueprint.
	comet := cardOf(t, l, "comet").Integration
	if strings.Join(comet.Data.Items, ",") != "billing_line,sw_service" {
		t.Errorf("comet is %+v", comet)
	}
	// A source acting for nothing marks no card.
	if b := cardOf(t, l, "git/example/beacon").Integration; b.Data.On() || b.Actions.On() || b.Blueprints.On() {
		t.Errorf("beacon is %+v", b)
	}
	if !l.SourcesKnown || len(l.Sources) != 5 || l.Sources[0].ID != "atlas" {
		t.Fatalf("sources %v known %v", l.Sources, l.SourcesKnown)
	}
}

func TestIntegrationUnknownWithoutSources(t *testing.T) {
	failed := WithSources(landscape(t, ""), nil, false)
	// A hub too old to say what its sources wrote is unknown too.
	old := WithSources(landscape(t, ""), []Source{{ID: "a", ActsFor: "sw_service:git/example/atlas"}}, true)
	for name, l := range map[string]Landscape{"failed": failed, "old": old} {
		if l.SourcesKnown {
			t.Errorf("%s: sources known", name)
		}
		for _, c := range l.Cards {
			if c.Integration != nil {
				t.Errorf("%s: %s has an integration", name, c.Key)
			}
		}
		if a := AnalysisOf(l); a.Known || len(a.Rows[0].Marks) != 0 {
			t.Errorf("%s: the analysis claims marks", name)
		}
	}
}

func TestAnalysisRowsAndCounts(t *testing.T) {
	a := AnalysisOf(WithSources(landscape(t, ""), sourcesFixture(t), true))
	var keys []string
	for _, r := range a.Rows {
		keys = append(keys, r.Card.Key)
	}
	if strings.Join(keys, ",") != "git/example/atlas,git/example/ember" {
		t.Fatalf("rows %v: built cards only, by title", keys)
	}
	if !slices.Equal(a.Rows[0].Dots, []bool{false, true, true, false}) || slices.Contains(a.Rows[1].Dots, true) {
		t.Fatalf("dots %v and %v", a.Rows[0].Dots, a.Rows[1].Dots)
	}
	var counts []int
	for _, c := range a.Counts {
		counts = append(counts, c.Count)
	}
	if !slices.Equal(counts, []int{0, 1, 1, 0}) || strings.Join(a.Counts[1].Titles, ",") != "Atlas" || a.Counts[0].Titles == nil {
		t.Fatalf("counts %+v", a.Counts)
	}
	if !a.Known || !slices.Equal(a.MarkCounts, []int{1, 1, 1}) {
		t.Fatalf("mark counts %v", a.MarkCounts)
	}
	if len(a.Rows[0].Marks) != 3 || a.Rows[0].Marks[1].Kind.Key != "blueprints" {
		t.Fatalf("marks %+v", a.Rows[0].Marks)
	}
	if len(a.Radar.Axes) != 4 || a.Radar.Max != 1 {
		t.Fatalf("radar %+v", a.Radar)
	}
}

func near(p, q Point) bool { return math.Abs(p.X-q.X) < 0.11 && math.Abs(p.Y-q.Y) < 0.11 }

func TestRadarGeometry(t *testing.T) {
	r := RadarOf([]string{"Top", "Right side", "Bottom", "Left"}, []int{2, 7, 0, 4})
	c := r.Center
	if r.Max != 8 || len(r.Rings) != 4 || r.Rings[0].Value != 2 || r.Rings[3].Value != 8 {
		t.Fatalf("max %d rings %+v: the largest count rounded up to even, rings by two", r.Max, r.Rings)
	}
	top, right, bottom, left := r.Axes[0], r.Axes[1], r.Axes[2], r.Axes[3]
	if !near(top.End, Point{c.X, c.Y - r.Radius}) || !near(right.End, Point{c.X + r.Radius, c.Y}) || !near(left.End, Point{c.X - r.Radius, c.Y}) {
		t.Fatalf("ends %+v %+v %+v: clockwise from the top", top.End, right.End, left.End)
	}
	if len(top.Steps) != r.Max+1 || !near(top.Steps[0].Point, c) || !near(top.At().Point, Point{c.X, c.Y - r.Radius/4}) {
		t.Fatalf("top steps %+v", top.Steps)
	}
	if !near(right.At().Number, Point{c.X + r.Radius*7/8 + numberGap, c.Y}) {
		t.Fatalf("a number sits just past its point, got %+v", right.At().Number)
	}
	if bottom.At().Point != c {
		t.Fatalf("a zero count sits at the centre, got %+v", bottom.At().Point)
	}
	if top.Anchor != "middle" || right.Anchor != "start" || left.Anchor != "end" || bottom.Anchor != "middle" {
		t.Fatalf("anchors %s %s %s %s", top.Anchor, right.Anchor, bottom.Anchor, left.Anchor)
	}
	labelY := c.Y - r.Radius - labelGap
	if l := top.Lines[0].At; l.Y >= labelY || l.Y < labelY-labelLine {
		t.Fatalf("the top label sits above its end, got %+v", l)
	}
	if l := bottom.Lines[0].At; l.Y <= c.Y+r.Radius+labelGap {
		t.Fatalf("the bottom label sits below its end, got %+v", l)
	}
	if !near(top.Tag, Point{c.X, c.Y - r.Radius - tagGap}) || r.Width != r.Height || c.X != r.Width/2 {
		t.Fatalf("tag %+v in a square box %vx%v", top.Tag, r.Width, r.Height)
	}
	if strings.Count(r.Area, ",") != 4 || !strings.HasPrefix(r.Area, num(c.X)+","+num(c.Y-r.Radius/4)+" ") {
		t.Fatalf("area %q", r.Area)
	}
	if got := RadarOf([]string{"a", "b"}, []int{1, 1}); len(got.Axes) != 0 {
		t.Fatal("fewer than three categories draw no radar")
	}
	if got := RadarOf([]string{"a", "b", "c"}, []int{0, 0, 0}); got.Max != 1 || len(got.Rings) != 1 {
		t.Fatalf("nothing counted still draws one ring, got %+v", got)
	}
	if got := RadarMax([]int{3, 5}); got != 5 || RadarStepOf(got) != 1 {
		t.Fatalf("five or under steps by one, got %d", got)
	}
}

func TestWrapKeepsWholeWords(t *testing.T) {
	got := Wrap("Self-service actions and orchestration", 16)
	if strings.Join(got, "|") != "Self-service|actions and|orchestration" {
		t.Fatalf("wrap %q", got)
	}
	if got := Wrap("Cost", 16); len(got) != 1 {
		t.Fatalf("wrap %q", got)
	}
}

func TestOrreryReadsTheSourcesWithTheCoverage(t *testing.T) {
	sources, _ := os.ReadFile("testdata/sources.json")
	fail := false
	hub := coverageHub(t, sources, func() string { return "" }, func() bool { return fail })
	o, err := NewOrrery(hub.URL, "token", quiet())
	if err != nil {
		t.Fatal(err)
	}
	c, err := o.Coverage(context.Background())
	l := c.Landscape(c.Lens("plat_pillar"), nil, true, "")
	if err != nil || !l.SourcesKnown || len(l.Sources) != 5 || cardOf(t, l, "git/example/atlas").Integration == nil {
		t.Fatalf("read %v: known %v, %d sources", err, l.SourcesKnown, len(l.Sources))
	}
	fail = true
	c, err = o.Coverage(context.Background())
	l = c.Landscape(c.Lens("plat_pillar"), nil, true, "")
	if err != nil || l.SourcesKnown || len(l.Cards) != 5 {
		t.Fatalf("a failed sources read keeps the coverage, integration unknown: %v, known %v, %d cards", err, l.SourcesKnown, len(l.Cards))
	}
}

// A source acting for a part of a card, keyed under the card's path, adds
// what it wrote to the card.
func TestIntegrationRollsUpAPath(t *testing.T) {
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	sources := []Source{{
		ID: "ember-pusher", ActsFor: "sw_service:git/example/ember/cmd/pusher",
		Writes: &Writes{
			Entities:   []EntityWrite{{Blueprint: "sw_service", Count: 1, LastAt: at}, {Blueprint: "inv_host", Count: 4, LastAt: at}},
			Blueprints: []Write{{ID: "inv_host", LastAt: at}},
			Actions:    []Write{},
		},
	}, {
		ID: "stray", ActsFor: "sw_service:git/example/emberish",
		Writes: &Writes{Entities: []EntityWrite{{Blueprint: "inv_host", Count: 1, LastAt: at}}, Blueprints: []Write{}, Actions: []Write{}},
	}}
	ember := cardOf(t, WithSources(landscape(t, ""), sources, true), "git/example/ember").Integration
	if strings.Join(ember.Data.Items, ",") != "inv_host,sw_service" || strings.Join(ember.Data.Sources, ",") != "ember-pusher" {
		t.Errorf("data %+v: the part's, and not a key that only shares a prefix", ember.Data)
	}
	if strings.Join(ember.Blueprints.Items, ",") != "inv_host" {
		t.Errorf("blueprints %+v", ember.Blueprints)
	}
	if _, ok := HomeOf("sw_service:git/example/nowhere/cmd/x", nil); ok {
		t.Error("no card holds a source when there are no cards")
	}
	if _, ok := HomeOf("not a ref", nil); ok {
		t.Error("a malformed reference has no home")
	}
}
