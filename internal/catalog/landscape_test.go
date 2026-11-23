package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/orchard-suite/platformkit/orrery"
)

// fixture reads the landscape's entities as Orrery lists them.
func fixture(t *testing.T) map[string][]orrery.Record {
	t.Helper()
	raw, err := os.ReadFile("testdata/landscape.json")
	if err != nil {
		t.Fatal(err)
	}
	var f map[string]struct{ Entities []orrery.Record }
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	out := map[string][]orrery.Record{}
	for list, page := range f {
		out[list] = page.Entities
	}
	return out
}

func landscape(t *testing.T, hub string) Landscape {
	t.Helper()
	f := fixture(t)
	return ShapeLandscape(pillarLens, f["pillars"], nil, true, f["services"], f["apis"], pillarRelations, hub)
}

// pillarLens is the platform pillars as a lens, and its relation on
// services and APIs.
var pillarLens = Lens{Blueprint: "plat_pillar", Title: "Platform pillar", Order: 1}

func pillarRelations(string) []string { return []string{"pillars"} }

func cardKeys(cs []Card) string {
	var ks []string
	for _, c := range cs {
		ks = append(ks, c.Key)
	}
	return strings.Join(ks, ",")
}

func cardOf(t *testing.T, l Landscape, key string) Card {
	t.Helper()
	for _, c := range l.Cards {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("no card %s", key)
	return Card{}
}

func TestCategoriesComeFromTheLensBlueprint(t *testing.T) {
	ps := Categories(fixture(t)["pillars"])
	var got []string
	for _, p := range ps {
		got = append(got, p.Key)
	}
	if strings.Join(got, ",") != "product,catalog,agents,cost" {
		t.Fatalf("categories %v: by order, an unordered one last, hidden ones left out", got)
	}
	if c := ps[1]; c.Name != "Catalog and ownership" || c.Order != 2 || c.Description != "What exists and who owns it." {
		t.Fatalf("catalog category %+v", c)
	}
	if ps[3].Order != 4 {
		t.Fatalf("an unordered category is numbered after the rest, got %d", ps[3].Order)
	}
}

func TestCardsAreShaped(t *testing.T) {
	l := landscape(t, "https://hub.example.org/")
	if got := cardKeys(l.Cards); got != "git/example/beacon,git/example/atlas,nimbus,comet,git/example/ember" {
		t.Fatalf("cards in order %s: by first category, unknown categories last; no origin, no category, hidden, and stray left out", got)
	}
	atlas := cardOf(t, l, "git/example/atlas")
	if atlas.Kind != "built" || atlas.Blueprint != "sw_service" || atlas.Title != "Atlas" || atlas.Description != "One graph of what exists." || atlas.Category != "Catalog and inventory" || atlas.Adoption != InUse || atlas.URL != "https://git.example.com/atlas" || atlas.Icon != "layers" || atlas.Status != Green {
		t.Fatalf("atlas shaped as %+v", atlas)
	}
	if strings.Join(atlas.Categories, ",") != "catalog,agents" {
		t.Fatalf("atlas categories %v: once each, in category order", atlas.Categories)
	}
	if atlas.Orrery != "https://hub.example.org/entities/sw_service/git/example/atlas" {
		t.Fatalf("atlas orrery link %q", atlas.Orrery)
	}
	beacon := cardOf(t, l, "git/example/beacon")
	if beacon.Kind != "adopted" || beacon.Adoption != Planned || beacon.Status != Amber || beacon.Reasons[0] != "one alert firing" {
		t.Fatalf("beacon shaped as %+v: an adopted service in development is planned", beacon)
	}
	ember := cardOf(t, l, "git/example/ember")
	if ember.Adoption != InUse || ember.Lifecycle != "development" || ember.Title != "git/example/ember" || ember.Status != Grey {
		t.Fatalf("ember shaped as %+v: a built service in development is in use, and missing is grey", ember)
	}
	comet := cardOf(t, l, "comet")
	if comet.Kind != "saas" || comet.Blueprint != "sw_api" || comet.URL != "https://api.comet.example.com" || comet.Status != Grey || comet.Adoption != InUse {
		t.Fatalf("comet shaped as %+v", comet)
	}
	if nimbus := cardOf(t, l, "nimbus"); nimbus.Adoption != Planned {
		t.Fatalf("an outside API in development is planned, got %+v", nimbus)
	}
	if l := landscape(t, ""); l.Cards[0].Orrery != "" {
		t.Fatal("without Orrery's public address a card has no link to it")
	}
}

func TestEdgesJoinServicesToAPIs(t *testing.T) {
	l := landscape(t, "")
	var got []string
	for _, e := range l.Edges {
		got = append(got, e.Consumer+">"+e.API+"@"+e.Provider+":"+e.State)
	}
	want := "git/example/atlas>nimbus@:wanted," +
		"git/example/atlas>comet@:live," +
		"git/example/atlas>git/example/beacon/alerts@git/example/beacon:live," +
		"git/example/ember>git/example/atlas/catalog@git/example/atlas:live"
	if strings.Join(got, ",") != want {
		t.Fatalf("edges %v: wanted first, an unknown API and a consumer off the landscape left out", got)
	}
}

func TestGridPlacesACardInEveryCategory(t *testing.T) {
	l := landscape(t, "")
	cols := Grid(l.Cards, l.Categories)
	if len(cols) != 5 || cols[4].Category.Key != OtherKey || cols[4].Category.Order != 5 {
		t.Fatalf("got %d columns, want the four categories and Other", len(cols))
	}
	catalog, agents := cols[1], cols[2]
	if catalog.Count != 1 || catalog.Boxes[0].Name != "Catalog and inventory" || catalog.Boxes[0].Cards[0].Key != "git/example/atlas" {
		t.Fatalf("catalog column %+v", catalog)
	}
	if agents.Count != 2 || len(agents.Boxes) != 2 || agents.Boxes[0].Cards[0].Key != "git/example/atlas" || agents.Boxes[1].Name != "Models" {
		t.Fatalf("agents column %+v", agents)
	}
	if other := cols[4]; other.Boxes[0].Name != "" || other.Boxes[0].Cards[0].Key != "git/example/ember" {
		t.Fatalf("other column %+v", other)
	}
	if got := Grid(l.Cards[:1], l.Categories); len(got) != 4 {
		t.Fatalf("Other should be left out when empty, got %d columns", len(got))
	}
}

func TestMatrixMarksAndCounts(t *testing.T) {
	l := landscape(t, "")
	m := MatrixOf(l.Cards, l.Categories)
	var groups []string
	for _, g := range m.Groups {
		var rows []string
		for _, r := range g.Rows {
			rows = append(rows, r.Card.Title)
		}
		groups = append(groups, g.Kind.Key+"="+strings.Join(rows, "+"))
	}
	// Within a kind, rows run by the first category they serve, then title.
	if strings.Join(groups, ",") != "built=Atlas+git/example/ember,saas=Nimbus+Comet,adopted=Beacon" {
		t.Fatalf("groups %v", groups)
	}
	if atlas := m.Groups[0].Rows[0]; !slices.Equal(atlas.Marks, []Mark{"", MarkServes, MarkServes, ""}) {
		t.Fatalf("atlas marks %v", atlas.Marks)
	}
	if beacon := m.Groups[2].Rows[0]; !slices.Equal(beacon.Marks, []Mark{MarkPlanned, "", "", ""}) {
		t.Fatalf("beacon marks %v", beacon.Marks)
	}
	var counts []int
	var thin []string
	for _, total := range m.Totals {
		counts = append(counts, total.Count)
		if total.Thin {
			thin = append(thin, total.Category.Key)
		}
	}
	if !slices.Equal(counts, []int{0, 1, 1, 1}) || len(thin) != 4 {
		t.Fatalf("counts %v thin %v: planned cards are not counted", counts, thin)
	}
}

func TestDetailsCarryWhatACardProvidesAndCalls(t *testing.T) {
	ds := Details(landscape(t, ""))
	byKey := map[string]Detail{}
	for _, d := range ds {
		byKey[d.Card.Key] = d
	}
	atlas := byKey["git/example/atlas"]
	if len(atlas.Categories) != 2 || atlas.Categories[1].Name != "Agents" {
		t.Fatalf("atlas categories %+v", atlas.Categories)
	}
	if len(atlas.Provides) != 1 || atlas.Provides[0].Title != "Atlas catalog API" {
		t.Fatalf("atlas provides %+v", atlas.Provides)
	}
	if len(atlas.Consumes) != 2 || atlas.Consumes[1].API != "Beacon alerts" || atlas.Consumes[1].Provider != "Beacon" || atlas.Consumes[0].Provider != "" {
		t.Fatalf("atlas consumes %+v", atlas.Consumes)
	}
	if len(atlas.WillConsume) != 1 || atlas.WillConsume[0].API != "Nimbus" {
		t.Fatalf("atlas will consume %+v", atlas.WillConsume)
	}
	if len(atlas.ConsumedBy) != 1 || atlas.ConsumedBy[0].Consumer != "git/example/ember" || atlas.ConsumedBy[0].API != "Atlas catalog API" {
		t.Fatalf("atlas consumed by %+v", atlas.ConsumedBy)
	}
	if comet := byKey["comet"]; len(comet.ConsumedBy) != 1 || comet.ConsumedBy[0].Consumer != "Atlas" {
		t.Fatalf("an outside API lists who calls it, got %+v", comet.ConsumedBy)
	}
	if beacon := byKey["git/example/beacon"]; len(beacon.ConsumedBy) != 1 || beacon.ConsumedBy[0].Consumer != "Atlas" {
		t.Fatalf("beacon consumed by %+v", beacon.ConsumedBy)
	}
}

// blueprintsJSON is the hub's blueprints the lens tests read: the pillars
// as a classification, and the relations services and APIs place
// themselves on it with.
const blueprintsJSON = `{"blueprints":[
	{"id":"plat_pillar","title":"Platform pillar","fields":{"order":{"type":"number"}},"relations":{}},
	{"id":"business_capability","title":"Business capability","fields":{"order":{"type":"number"}},"relations":{"parent":{"target":"business_capability"}}},
	{"id":"sw_service","title":"Service","fields":{},"relations":{"pillars":{"target":"plat_pillar","many":true}}},
	{"id":"sw_api","title":"API","fields":{},"relations":{"pillars":{"target":"plat_pillar","many":true}}}
]}`

// coverageHub answers the coverage reads from the fixture, the pillars as a
// lens; fail names a blueprint whose read fails.
func coverageHub(t *testing.T, sources []byte, fail func() string, sourcesFail func() bool) *httptest.Server {
	t.Helper()
	f := fixture(t)
	lists := map[string][]orrery.Record{"plat_pillar": f["pillars"], "sw_service": f["services"], "sw_api": f["apis"]}
	var lens []orrery.Record
	_ = json.Unmarshal([]byte(`[{"key":"plat_pillar","fields":{"order":1}}]`), &lens)
	lists[LensBlueprint] = lens
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/blueprints":
			_, _ = w.Write([]byte(blueprintsJSON))
			return
		case "/v1/sources":
			if sources == nil || sourcesFail() {
				http.Error(w, "no", http.StatusForbidden)
				return
			}
			_, _ = w.Write(sources)
			return
		}
		bp := r.URL.Query().Get("blueprint")
		if r.URL.Path != "/v1/entities" || bp == fail() {
			http.Error(w, "no", http.StatusServiceUnavailable)
			return
		}
		list := lists[bp]
		_ = json.NewEncoder(w).Encode(map[string]any{"entities": list, "total": len(list)})
	}))
	t.Cleanup(hub.Close)
	return hub
}

func TestOrreryReadsTheCoverage(t *testing.T) {
	failing := ""
	hub := coverageHub(t, nil, func() string { return failing }, func() bool { return false })
	o, err := NewOrrery(hub.URL, "token", quiet())
	if err != nil {
		t.Fatal(err)
	}
	c, err := o.Coverage(context.Background())
	if err != nil || len(c.Lenses) != 2 || c.Lenses[0].Blueprint != CapabilityBlueprint || c.Lenses[1].Title != "Platform pillar" {
		t.Fatalf("read %v: lenses %+v", err, c.Lenses)
	}
	l := c.Landscape(c.Lens("plat_pillar"), nil, true, "https://hub.example.org")
	if len(l.Categories) != 4 || len(l.Cards) != 5 || len(l.Edges) != 4 || l.Cards[0].Orrery == "" {
		t.Fatalf("%d categories, %d cards, %d edges", len(l.Categories), len(l.Cards), len(l.Edges))
	}
	if c.Lens("made-up").Blueprint != CapabilityBlueprint {
		t.Fatal("an unknown lens falls back to business capabilities")
	}
	failing = "sw_api"
	if _, err := o.Coverage(context.Background()); err == nil {
		t.Fatal("a failed api read should fail the coverage, so the cache keeps the last good one")
	}
}

func TestCoverageCacheKeepsLastGood(t *testing.T) {
	c := Coverage{Services: fixture(t)["services"]}
	var err error
	var outcomes []string
	cache := NewCoverageCache(func(context.Context) (Coverage, error) { return c, err }, time.Nanosecond, quiet(), func(_ context.Context, o string) { outcomes = append(outcomes, o) })
	cache.Get(context.Background())
	err = errors.New("down")
	time.Sleep(time.Millisecond)
	s := cache.Get(context.Background())
	if !s.Stale || len(s.Value.Services) != len(c.Services) || strings.Join(outcomes, ",") != "ok,failed" {
		t.Fatalf("got %+v with outcomes %v", s, outcomes)
	}
}

func TestCardSizesAndLabels(t *testing.T) {
	cases := []struct {
		name string
		card Card
		size Size
	}{
		{"critical tier", Card{Kind: "saas", Tier: "critical", Adoption: InUse}, Large},
		{"important tier, even planned", Card{Kind: "adopted", Tier: "important", Adoption: Planned}, Medium},
		{"standard tier, even in production", Card{Kind: "built", Tier: "standard", Adoption: InUse, Lifecycle: "production"}, Small},
		{"built in production without a tier", Card{Kind: "built", Adoption: InUse, Lifecycle: "production"}, Small},
		{"application without a tier", Card{Kind: "built", App: "it_application:portal", Adoption: InUse}, Small},
		{"adopted, lifecycle unset", Card{Kind: "adopted", Adoption: InUse}, Small},
		{"built in development", Card{Kind: "built", Adoption: InUse, Lifecycle: "development"}, Small},
		{"adopted and planned", Card{Kind: "adopted", Adoption: Planned, Lifecycle: "development"}, Small},
		{"saas without a tier", Card{Kind: "saas", Adoption: InUse, Lifecycle: "production"}, Small},
	}
	for _, c := range cases {
		if got := SizeOf(c.card); got != c.size {
			t.Errorf("%s: %s, want %s", c.name, got, c.size)
		}
	}
	l := landscape(t, "")
	for key, want := range map[string]string{"git/example/atlas": "small Built here", "comet": "large SaaS", "git/example/beacon": "small Planned", "nimbus": "small Planned", "git/example/ember": "small Built here"} {
		c := cardOf(t, l, key)
		if got := string(c.Size()) + " " + c.Label(); got != want {
			t.Errorf("%s is %q, want %q", key, got, want)
		}
	}
	if comet := cardOf(t, l, "comet"); comet.Tier != "critical" {
		t.Fatalf("a tier is read in lower case, got %q", comet.Tier)
	}
}

func TestGridBoxesPutLargerCardsFirstAndKnowTheirArea(t *testing.T) {
	categories := []Category{{Key: "a", Name: "A", Order: 1}}
	sized := func(tier string) func(string) Card {
		return func(key string) Card {
			return Card{Key: key, Kind: "saas", Tier: tier, Category: "Box", Adoption: InUse, Categories: []string{"a"}}
		}
	}
	small, medium, large := sized(""), sized("important"), sized("critical")
	box := Grid([]Card{small("s1"), medium("m1"), large("l1"), small("s2"), medium("m2"), small("s3")}, categories)[0].Boxes[0]
	if got := cardKeys(box.Cards); got != "l1,m1,m2,s1,s2,s3" {
		t.Fatalf("box order %s, want largest first and each size in its order", got)
	}
	if box.Cells != 20 || box.Span != 7 || box.Fit != 3 {
		t.Fatalf("cells %d span %d fit %d, want 20 cells seven wide, three at least", box.Cells, box.Span, box.Fit)
	}
	for _, c := range []struct {
		cards            []Card
		cells, span, fit int
	}{
		{[]Card{small("s1")}, 1, 1, 1},
		{[]Card{small("s1"), small("s2")}, 2, 2, 1},
		{[]Card{small("s1"), small("s2"), small("s3"), small("s4"), small("s5"), small("s6"), small("s7"), small("s8"), small("s9"), small("s10"), small("s11"), small("s12"), small("s13")}, 13, MaxSpan, 1},
		{[]Card{medium("m1")}, 4, 2, 2},
		{[]Card{medium("m1"), small("s1")}, 5, 3, 2},
		{[]Card{large("l1")}, 9, 3, 3},
		{[]Card{large("l1"), small("s1")}, 10, 4, 3},
		{[]Card{large("l1"), large("l2"), large("l3"), large("l4"), large("l5")}, 45, MaxSpan, 3},
	} {
		b := Grid(c.cards, categories)[0].Boxes[0]
		if b.Cells != c.cells || b.Span != c.span || b.Fit != c.fit {
			t.Errorf("%s: cells %d span %d fit %d, want %d, %d, and %d", cardKeys(c.cards), b.Cells, b.Span, b.Fit, c.cells, c.span, c.fit)
		}
	}
}

// A team count with every app unowned says nothing, and says so; one owned
// app makes it a count again.
func TestBreakdownNoTeams(t *testing.T) {
	none := BreakdownOf(Landscape{}, []App{{Title: "A"}, {Title: "B"}})
	if !none.NoTeams() {
		t.Fatalf("two unowned apps: NoTeams false, ByTeam %v", none.ByTeam)
	}
	some := BreakdownOf(Landscape{}, []App{{Title: "A", Team: "it"}, {Title: "B"}})
	if some.NoTeams() {
		t.Fatalf("one owned app: NoTeams true, ByTeam %v", some.ByTeam)
	}
}

// A column's boxes follow the catalog's order of their sub-categories, not
// the order their cards happen to come in.
func TestGridBoxesFollowTheCatalogOrder(t *testing.T) {
	top := []Category{{Key: "platform", Name: "Platform", Order: 1}}
	cards := []Card{
		{Key: "annals", Title: "Annals", Categories: []string{"platform"}, Category: "Agents", categoryOrder: 3},
		{Key: "dispatch", Title: "Dispatch", Categories: []string{"platform"}, Category: "Operate", categoryOrder: 2},
		{Key: "trellis", Title: "Trellis", Categories: []string{"platform"}, Category: "Build and ship", categoryOrder: 1},
	}
	var got []string
	for _, b := range Grid(cards, top)[0].Boxes {
		got = append(got, b.Name)
	}
	if want := "Build and ship,Operate,Agents"; strings.Join(got, ",") != want {
		t.Fatalf("boxes %v, want %s", got, want)
	}
}

func TestLegendOfKeysEveryKindHeld(t *testing.T) {
	got := LegendOf([]Card{{Kind: "built"}, {Kind: "built", Hosting: SaaS, Origins: []string{"built", "adopted"}}, {Kind: "self_hosted", Hosting: SelfHosted}})
	if cardKeysOf(got) != "built,self_hosted,saas,adopted" {
		t.Fatalf("every kind held, in the order of Kinds: %+v", got)
	}
	if LegendOf(nil) != nil {
		t.Fatal("no cards, no key")
	}
}

func TestCardMarks(t *testing.T) {
	for _, c := range []struct {
		card              Card
		host              string
		built, openSource bool
	}{
		{Card{Kind: "built", Hosting: SelfHosted, Origins: []string{"built"}}, "self_hosted", true, false},
		{Card{Kind: "adopted", Hosting: SelfHosted, Origins: []string{"adopted"}}, "self_hosted", false, true},
		{Card{Kind: "built", Hosting: SaaS, Origins: []string{"built", "adopted"}}, "saas", true, true},
		{Card{Kind: "installed", Hosting: Installed}, "installed", false, false},
		{Card{Kind: "built"}, "", true, false},
		{Card{Kind: "adopted"}, "", false, true},
		{Card{Kind: "saas"}, "saas", false, false},
	} {
		if c.card.Host() != c.host || c.card.Has("built") != c.built || c.card.Has("adopted") != c.openSource {
			t.Errorf("%+v: host %q built %v open source %v, want %q %v %v", c.card, c.card.Host(), c.card.Has("built"), c.card.Has("adopted"), c.host, c.built, c.openSource)
		}
	}
}

func cardKeysOf(list []Option) string {
	keys := make([]string, len(list))
	for i, o := range list {
		keys[i] = o.Key
	}
	return strings.Join(keys, ",")
}

func TestKindsOfNamesOnlyTheKindsHeld(t *testing.T) {
	got := KindsOf([]Card{{Kind: "saas"}, {Kind: "built"}, {Kind: "saas"}})
	if len(got) != 2 || got[0].Key != "built" || got[1].Key != "saas" {
		t.Fatalf("the kinds held, in the order of Kinds: %+v", got)
	}
	if KindsOf(nil) != nil {
		t.Fatal("no cards, no kinds")
	}
}
