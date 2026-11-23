package catalog

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/orchard-suite/platformkit/orrery"
)

// productFixture reads testdata/products.json, by blueprint.
func productFixture(t *testing.T) map[string][]orrery.Record {
	t.Helper()
	raw, err := os.ReadFile("testdata/products.json")
	if err != nil {
		t.Fatal(err)
	}
	var f map[string][]orrery.Record
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// foundOf is what the fixture's sources hold besides the Entra apps.
func foundOf(f map[string][]orrery.Record) Found {
	return Found{Workloads: f["sw_workload"], Services: f["sw_service"], APIs: f["sw_api"]}
}

// directoryFixture is the fixture as Directory reads it.
func directoryFixture(t *testing.T) Directory {
	t.Helper()
	f := productFixture(t)
	apps := Applications(f["it_application"])
	entra := EntraApps(f["entra_app"])
	access := AccessOf(f["entra_user"], f["entra_group"], f["entra_app_role_assignment"])
	return Directory{Capabilities: Categories(f["business_capability"]), Applications: apps, EntraApps: entra, Access: access, Apps: Tiles(apps, entra, access),
		Suggestions: SuggestionsOf(apps, entra, foundOf(f), nil)}
}

func appByKey(t *testing.T, apps []Application, key string) Application {
	t.Helper()
	for _, a := range apps {
		if a.Key == key {
			return a
		}
	}
	t.Fatalf("no application %s", key)
	return Application{}
}

func TestApplicationsReadWhatTheAdminKeeps(t *testing.T) {
	d := directoryFixture(t)
	portal := appByKey(t, d.Applications, "portal")
	if portal.Hosting != SelfHosted || portal.Workload != "portal" || !slices.Equal(portal.Services, []string{"git/example/atlas"}) || portal.Tier != "critical" || portal.Access != AccessGroup {
		t.Fatalf("portal %+v", portal)
	}
	if lab := appByKey(t, d.Applications, "lab"); !slices.Equal(lab.Audience, []string{"Platform"}) || lab.Listed != Shown || !lab.SelfBuilt {
		t.Fatalf("lab %+v", lab)
	}
	if portal.SelfBuilt {
		t.Fatal("an application not marked self built is not")
	}
	if old := appByKey(t, d.Applications, "old"); old.Listed != Hidden || old.Hosting != Installed {
		t.Fatalf("old %+v", old)
	}
	if front := appByKey(t, d.Applications, "front"); front.Listed != NoLogin || front.URL == "" {
		t.Fatalf("front %+v", front)
	}
}

func TestTilesAreTheShownApplications(t *testing.T) {
	d := directoryFixture(t)
	if got := keys(d.Apps); got != "lab,payroll,portal,wiki" {
		t.Fatalf("tiles %s: a hidden application, or one hidden from login, is no tile", got)
	}
	pay := d.Apps[1]
	if pay.URL != "https://payroll.example.com" || pay.SignIn == nil || pay.SignIn.Everyone || pay.Ref != "it_application:payroll" || pay.Access != AccessGroup {
		t.Fatalf("an application with no address opens at its first Entra app's: %+v", pay)
	}
	if lab := d.Apps[0]; lab.SignIn != nil || lab.Entra {
		t.Fatal("an application with no Entra app follows its audience only")
	}
	if wiki := d.Apps[3]; wiki.SignIn != nil || !wiki.Entra || wiki.Team != "IT Operations" || wiki.Hosting != SaaS {
		t.Fatalf("wiki %+v", wiki)
	}
}

func TestAccessComesFromOrrery(t *testing.T) {
	d := directoryFixture(t)
	fin := d.Access.PersonFor("Finance@Example.com")
	if fin == nil || fin.ID != "tenant-1/u-fin" || !slices.Equal(fin.Groups, []string{"tenant-1/g-finance"}) {
		t.Fatalf("finance person %+v", fin)
	}
	if d.Access.PersonFor("left@example.com") != nil {
		t.Fatal("a disabled user is nobody")
	}
	visible := func(email string) []string {
		var out []string
		for _, a := range Filter(context.Background(), quiet(), d.Apps, Viewer{Person: d.Access.PersonFor(email)}) {
			out = append(out, a.Key)
		}
		return out
	}
	if got := visible("nobody@example.com"); !slices.Equal(got, []string{"wiki"}) {
		t.Fatalf("someone Entra does not know sees only what is open, got %v", got)
	}
	if got := visible("finance@example.com"); !slices.Contains(got, "payroll") || slices.Contains(got, "portal") {
		t.Fatalf("finance sees payroll through its group, got %v", got)
	}
	if got := visible("staff@example.com"); !slices.Contains(got, "portal") || slices.Contains(got, "payroll") {
		t.Fatalf("staff sees the portal, got %v", got)
	}
	groups := d.Access.GroupsOf([]string{"tenant-1/app-portal", "tenant-1/app-payroll"})
	if len(groups) != 2 || groups[0].Name != "Finance" || !groups[1].Dynamic {
		t.Fatalf("groups of two apps %+v", groups)
	}
}

func TestSignInAdmits(t *testing.T) {
	closed := SignIn{Users: []string{"u-a"}, Groups: []string{"g-fin"}}
	cases := []struct {
		name string
		rule SignIn
		who  *Person
		want bool
	}{
		{"open to everyone, even unknown", SignIn{Everyone: true}, nil, true},
		{"unknown person, closed app", closed, nil, false},
		{"assigned directly", closed, &Person{ID: "u-a"}, true},
		{"in an assigned group", closed, &Person{ID: "u-b", Groups: []string{"g-x", "g-fin"}}, true},
		{"neither", closed, &Person{ID: "u-b", Groups: []string{"g-x"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rule.Admits(tc.who); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSuggestionsAreWhatNothingPairsWith(t *testing.T) {
	d := directoryFixture(t)
	var refs []string
	for _, s := range d.Suggestions {
		refs = append(refs, s.Ref)
	}
	want := []string{"entra_app:tenant-1/app-crm", "entra_app:tenant-1/app-nourl", "sw_service:git/example/beacon", "sw_api:nimbus"}
	if !slices.Equal(refs, want) {
		t.Fatalf("suggestions %v, want %v: paired and missing apps, services built here, and APIs of ours are no suggestion", refs, want)
	}
	f := productFixture(t)
	again := SuggestionsOf(d.Applications, d.EntraApps, foundOf(f), []string{"entra_app:tenant-1/app-crm"})
	if len(again) != 3 || again[0].Key != "tenant-1/app-nourl" {
		t.Fatalf("a dismissed suggestion is gone: %+v", again)
	}
	paired := append(slices.Clone(d.Applications), Application{Key: "alerts", Services: []string{"git/example/beacon"}, API: "nimbus"})
	if got := SuggestionsOf(paired, d.EntraApps, foundOf(f), nil); len(got) != 2 {
		t.Fatalf("an application that runs the service and is the API pairs both: %+v", got)
	}
	if !d.Suggestions[0].Required {
		t.Fatal("the CRM needs an assignment")
	}
}

func TestApplicationEntity(t *testing.T) {
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	e := AppDetails{Name: " CRM ", URL: "https://crm.example.com", Hosting: SaaS, Access: AccessDirect, SignIn: []string{"tenant-1/app-crm"}, Capabilities: []string{"sales"},
		Placed: map[string][]string{"pillars": {"catalog"}}, By: "user@example.com", At: at}.Entity("crm")
	if e.Blueprint != ApplicationBlueprint || e.Key != "crm" || e.Title != "CRM" || e.Fields["hosting"] != "saas" || e.Fields["self_built"] != false || e.Fields["listed"] != "shown" || e.Fields["updated_at"] != "2026-11-04T12:00:00Z" {
		t.Fatalf("entity %+v", e)
	}
	if e.Fields["tier"] != nil || e.Relations["workload"] != nil || len(e.Relations["services"].([]string)) != 0 {
		t.Fatal("empty values clear")
	}
	if p := e.Relations["pillars"].([]string); !slices.Equal(p, []string{"catalog"}) {
		t.Fatal("a lens placement is written under its relation")
	}
	if k := ApplicationKey("Team Wiki", directoryFixture(t).Applications); k != "team-wiki" {
		t.Fatalf("key %q", k)
	}
	if k := ApplicationKey("Wiki", directoryFixture(t).Applications); k != "wiki-2" {
		t.Fatalf("a taken key gets a number, got %q", k)
	}
}

func TestReachMarksEachAppForThePerson(t *testing.T) {
	d := directoryFixture(t)
	v := Viewer{Person: d.Access.PersonFor("staff@example.com")}
	reach := ReachOf(d.Apps, v, func(a App) string { return "https://hub.example.org/request?entity=" + a.Ref })
	if r := reach["it_application:portal"]; !r.Open || r.Tile != "portal" || r.Request != "" {
		t.Fatalf("staff opens the portal: %+v", r)
	}
	if r := reach["it_application:payroll"]; r.Open || r.Request != "https://hub.example.org/request?entity=it_application:payroll" {
		t.Fatalf("staff may ask for payroll: %+v", r)
	}
}

func TestTheLandscapeIsApplicationsOnly(t *testing.T) {
	d := directoryFixture(t)
	f := productFixture(t)
	lf := fixture(t)
	capLens := Lens{Blueprint: CapabilityBlueprint, Title: "Business capability"}
	rel := func(bp string) []string {
		if bp == ApplicationBlueprint {
			return []string{CapabilityRelation}
		}
		return nil
	}
	l := ShapeLandscape(capLens, f["business_capability"], d.Applications, false, lf["services"], lf["apis"], rel, "")
	if got := cardKeys(l.Cards); got != "front,portal,wiki,payroll,lab" {
		t.Fatalf("cards %s: shown and hidden-from-login applications by capability, the unplaced last, never a service", got)
	}
	if lab := cardOf(t, l, "lab"); lab.Kind != "built" || lab.Base() != "self_hosted" || !slices.Equal(lab.Origins, []string{"built"}) || lab.Label() != "Self-hosted · Built here" || lab.KindKeys() != "self_hosted built" {
		t.Fatalf("a self-built application is built here on top of its hosting: %+v", lab)
	}
	if wiki := cardOf(t, l, "wiki"); wiki.Kind != "saas" || wiki.Base() != "saas" || len(wiki.Origins) != 0 || wiki.Label() != "SaaS" {
		t.Fatalf("one not self built is only its hosting: %+v", wiki)
	}
	if lab := cardOf(t, l, "lab"); len(lab.Categories) != 0 || Grid(l.Cards, l.Categories)[len(l.Categories)].Category.Key != OtherKey {
		t.Fatalf("an application placed nowhere shows under Other: %+v", lab)
	}
	if got := cardKeys(ShapeLandscape(capLens, f["business_capability"], d.Applications, true, nil, nil, rel, "").Cards); strings.Contains(got, "lab") {
		t.Fatalf("analytics draws only what the lens places: %s", got)
	}
	adopted := append(slices.Clone(d.Applications), Application{Key: "alerts", Name: "Alerts", Listed: Shown, Seen: true, Services: []string{"git/example/beacon"}, Links: map[string][]string{CapabilityRelation: {"everyday"}}})
	if c := cardOf(t, ShapeLandscape(capLens, f["business_capability"], adopted, false, lf["services"], lf["apis"], rel, ""), "alerts"); c.Kind != "adopted" || c.Base() != "self_hosted" || !slices.Equal(c.Origins, []string{"adopted"}) || len(c.BuiltFrom) != 1 {
		t.Fatalf("an application that runs an open-source service is open source on top of its hosting: %+v", c)
	}
	both := append(slices.Clone(d.Applications), Application{Key: "fork", Name: "Fork", Hosting: SaaS, SelfBuilt: true, Listed: Shown, Seen: true, Services: []string{"git/example/beacon"}, Links: map[string][]string{CapabilityRelation: {"everyday"}}})
	if c := cardOf(t, ShapeLandscape(capLens, f["business_capability"], both, false, lf["services"], lf["apis"], rel, ""), "fork"); c.Kind != "built" || c.Base() != "saas" || !slices.Equal(c.Origins, []string{"built", "adopted"}) || c.Label() != "SaaS · Built here · Open source" {
		t.Fatalf("hosting, built here, and open source layer on one card, counted as built here: %+v", c)
	}
	portal := cardOf(t, l, "portal")
	if portal.Kind != "built" || portal.App != "it_application:portal" || len(portal.BuiltFrom) != 1 || portal.BuiltFrom[0].Repo != "https://git.example.com/atlas" || portal.Size() != Large {
		t.Fatalf("an application built from a service built here is built here, and names it: %+v", portal)
	}
	if wiki := cardOf(t, l, "wiki"); wiki.Kind != "saas" {
		t.Fatalf("wiki is SaaS: %+v", wiki)
	}
	if pay := cardOf(t, l, "payroll"); pay.Category != "Payroll" || !slices.Equal(pay.Categories, []string{"finance"}) {
		t.Fatalf("payroll is boxed by its child capability: %+v", pay)
	}
	b := BreakdownOf(l, d.Apps)
	if len(b.Empty) != 1 || b.Empty[0].Key != "sales" || !slices.Equal(b.Unplaced, []string{"Lab"}) {
		t.Fatalf("gaps %+v", b)
	}
	withServices := ShapeLandscape(pillarLens, lf["pillars"], d.Applications, true, lf["services"], lf["apis"], func(bp string) []string { return []string{"pillars"} }, "")
	if !slices.ContainsFunc(withServices.Cards, func(c Card) bool { return c.Key == "git/example/atlas" }) || !slices.ContainsFunc(withServices.Cards, func(c Card) bool { return c.Key == "portal" }) {
		t.Fatalf("analytics holds both applications and services: %s", cardKeys(withServices.Cards))
	}
}

func TestAssessmentsAndOverlap(t *testing.T) {
	l := landscape(t, "")
	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	older := Assessment{Lens: "plat_pillar", Category: "catalog", Rating: 2, At: at}
	newer := Assessment{Lens: "plat_pillar", Category: "catalog", Rating: 4, Covers: "Orrery", At: at.Add(time.Hour)}
	var recs []orrery.Record
	for _, a := range []Assessment{older, newer, {Lens: "other", Category: "catalog", Rating: 1, At: at}} {
		e := a.Entity()
		raw, _ := json.Marshal(map[string]any{"key": e.Key, "fields": e.Fields})
		var r orrery.Record
		_ = json.Unmarshal(raw, &r)
		recs = append(recs, r)
	}
	rated := RatingsOf(l, Assessments(recs))
	i := slices.IndexFunc(rated, func(r Rated) bool { return r.Category.Key == "catalog" })
	if i < 0 || rated[i].Current == nil || rated[i].Current.Rating != 4 || rated[i].Earlier != 1 || rated[i].Count == 0 {
		t.Fatalf("the newest assessment is current, the older one is history: %+v", rated)
	}
	for _, r := range rated {
		sum := 0
		for _, k := range r.Kinds {
			sum += k.Count
		}
		if sum != r.Count || len(r.Cards) != r.Count {
			t.Fatalf("a category's kinds add up to its count: %+v", r)
		}
	}
	if newer.Key() != "plat_pillar/catalog/20261104T100000Z" {
		t.Fatalf("key %q", newer.Key())
	}
	o := OverlapOf(l)
	if o.Shared == 0 || len(o.Pairs) == 0 {
		t.Fatalf("atlas sits in two categories: %+v", o)
	}
	if n, ok := RatingOf("6"); ok || n != 6 {
		t.Fatal("a rating above 5 is refused")
	}
}

func TestLensRelationNames(t *testing.T) {
	bps := []Blueprint{{ID: ApplicationBlueprint, Relations: map[string]BlueprintBit{"pillars": {Target: "plat_pillar"}}}}
	if name, exists := LensRelation(bps, "plat_pillar"); name != "pillars" || !exists {
		t.Fatalf("an existing relation is used: %s %v", name, exists)
	}
	if name, exists := LensRelation(bps, "dev_pillar"); name != "dev_pillars" || exists {
		t.Fatalf("a taken name takes the whole id: %s", name)
	}
	if name, _ := LensRelation(bps, "waf_principle"); name != "principles" {
		t.Fatalf("a new lens takes its thing's name, plural: %s", name)
	}
}

func TestEveryOfferHasAnIDAndTitle(t *testing.T) {
	for _, b := range CurationBlueprints(DefaultMappings()) {
		if id, _ := b["id"].(string); id == "" {
			t.Fatalf("an offer without an id: %v", b)
		}
		if title, _ := b["title"].(string); title == "" {
			t.Fatalf("%v is offered without a title", b["id"])
		}
	}
}

func TestCapabilityKeysAndEntities(t *testing.T) {
	d := directoryFixture(t)
	if k := CapabilityKey("Fixed Assets!", "finance", d.Capabilities); k != "finance-fixed-assets" {
		t.Fatalf("key %q", k)
	}
	if k := CapabilityKey("Payroll", "finance", d.Capabilities); k != "finance-payroll-2" {
		t.Fatalf("a taken key gets a number, got %q", k)
	}
	e := CapabilityEntity(Category{Key: "finance-fixed-assets", Name: "Fixed assets", Order: 2, Parent: "finance"})
	if e.Blueprint != CapabilityBlueprint || e.Relations["parent"] != "finance" {
		t.Fatalf("entity %+v", e)
	}
}

func TestSuggestionDetails(t *testing.T) {
	d := directoryFixture(t)
	blueprints := []Blueprint{
		{ID: ServiceBlueprint, Relations: map[string]BlueprintBit{"pillars": {Target: "plat_pillar"}}},
		{ID: APIBlueprint, Relations: map[string]BlueprintBit{"pillars": {Target: "plat_pillar"}}},
	}
	lenses := map[string]string{CapabilityBlueprint: CapabilityRelation, "plat_pillar": "pillars"}
	byRef := map[string]Suggestion{}
	for _, s := range d.Suggestions {
		byRef[s.Ref] = s
	}
	svc := byRef["sw_service:git/example/beacon"].Details(blueprints, lenses)
	if svc.Name != "Beacon" || svc.Icon != "beacon" || svc.Hosting != SelfHosted || !slices.Equal(svc.Services, []string{"git/example/beacon"}) || svc.Description != "Alerts, run as it ships." || !slices.Equal(svc.Placed["pillars"], []string{"catalog"}) || svc.Listed != Shown {
		t.Fatalf("an open-source service is self-hosted, runs it, and keeps its pillars: %+v", svc)
	}
	api := byRef["sw_api:nimbus"].Details(blueprints, lenses)
	if api.Hosting != SaaS || api.API != "nimbus" || !slices.Equal(api.Placed["pillars"], []string{"product"}) || len(api.Services) != 0 {
		t.Fatalf("an outside API is SaaS and is the API: %+v", api)
	}
	crm := byRef["entra_app:tenant-1/app-crm"].Details(blueprints, lenses)
	if crm.Access != AccessDirect || !slices.Equal(crm.SignIn, []string{"tenant-1/app-crm"}) || crm.Hosting != "" || len(crm.Placed) != 0 {
		t.Fatalf("an Entra app that needs assignment is granted directly: %+v", crm)
	}
}

func TestGroupSuggestionsByKind(t *testing.T) {
	got := GroupSuggestions([]Suggestion{{Kind: FromAPI, Name: "b"}, {Kind: FromEntra, Name: "a"}, {Kind: FromAPI, Name: "c"}})
	if len(got) != 2 || got[0].Kind != FromEntra || got[1].Kind != FromAPI || len(got[1].Items) != 2 || got[1].Items[0].Name != "b" {
		t.Fatalf("Entra first, then outside products in their order, no empty group: %+v", got)
	}
	if FromService.Heading() != "Open source" {
		t.Fatal("a group heading names the kind")
	}
}

func TestApplicationsReadPeople(t *testing.T) {
	recs := []orrery.Record{
		{Key: "jira", Title: "Jira", Fields: map[string]json.RawMessage{"url": json.RawMessage(`"https://jira.example.com"`), "people": json.RawMessage(`["A@Example.com", "a@example.com", "nope", "b@example.com"]`)}, Relations: map[string][]orrery.Ref{"access_groups": {{Key: "g-eng"}}}},
		{Key: "wiki", Title: "Wiki", Fields: map[string]json.RawMessage{"people": json.RawMessage(`"c@example.com, d@example.com"`)}},
	}
	apps := Applications(recs)
	if !slices.Equal(apps[0].People, []string{"a@example.com", "b@example.com"}) || !slices.Equal(apps[1].People, []string{"c@example.com", "d@example.com"}) {
		t.Fatalf("people read as emails, lowercased, each once: %v %v", apps[0].People, apps[1].People)
	}
	tiles := Tiles(apps, nil, Access{})
	if len(tiles) != 1 || !slices.Equal(tiles[0].People, apps[0].People) || !slices.Equal(tiles[0].Groups, []string{"g-eng"}) {
		t.Fatalf("a tile carries its groups and people: %+v", tiles)
	}
	ent := AppDetails{Name: "Jira", People: []string{"a@example.com"}}.Entity("jira")
	if got, _ := ent.Fields["people"].([]string); !slices.Equal(got, []string{"a@example.com"}) {
		t.Fatalf("people are written back: %v", ent.Fields["people"])
	}
}

func TestAnOpenApplicationIgnoresEntraAssignments(t *testing.T) {
	d := directoryFixture(t)
	apps := slices.Clone(d.Applications)
	for i := range apps {
		if apps[i].Key == "payroll" {
			apps[i].Access, apps[i].Audience, apps[i].AccessGroups, apps[i].People = AccessOpen, nil, nil, nil
		}
	}
	var pay App
	for _, a := range Tiles(apps, d.EntraApps, d.Access) {
		if a.Key == "payroll" {
			pay = a
		}
	}
	if pay.SignIn != nil || !pay.Entra || pay.URL != "https://payroll.example.com" {
		t.Fatalf("an open application keeps its Entra app's address but not its assignments: %+v", pay)
	}
	if ok, why := pay.VisibleTo(Viewer{}); !ok {
		t.Fatalf("an open application is for everyone signed in, even someone Entra does not hold: %s", why)
	}
}
