package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/orchard-suite/platformkit/faro"
	"github.com/orchard-suite/platformkit/orrery"

	"github.com/orchard-suite/myapps/internal/catalog"
)

// fakeSource serves tiles made in the test, or, with entra set, the catalog
// package's products, people, and groups.
type fakeSource struct {
	apps  []catalog.App
	entra bool
	t     *testing.T
	err   error
}

func (f *fakeSource) Directory(context.Context) (catalog.Directory, error) {
	if f.entra {
		d := productDirectory(f.t)
		return d, f.err
	}
	return catalog.Directory{Capabilities: categories, Apps: f.apps, Teams: []catalog.Category{{Key: "it", Name: "IT Operations"}}}, f.err
}

// productDirectory is the catalog package's products fixture as the
// directory reads it: a wiki open to everyone, payroll for the finance
// group, the portal for staff, the lab for a role, and an Entra CRM not
// yet an application.
func productDirectory(t *testing.T) catalog.Directory {
	t.Helper()
	raw, err := os.ReadFile("../catalog/testdata/products.json")
	if err != nil {
		t.Fatal(err)
	}
	var f map[string][]orrery.Record
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	apps := catalog.Applications(f["it_application"])
	entra := catalog.EntraApps(f["entra_app"])
	access := catalog.AccessOf(f["entra_user"], f["entra_group"], f["entra_app_role_assignment"])
	return catalog.Directory{
		Capabilities: catalog.Categories(f["business_capability"]),
		Applications: apps,
		EntraApps:    entra,
		Workloads:    catalog.Categories(f["sw_workload"]),
		Suggestions:  catalog.SuggestionsOf(apps, entra, catalog.Found{Workloads: f["sw_workload"], Services: f["sw_service"], APIs: f["sw_api"]}, nil),
		Access:       access,
		Apps:         catalog.Tiles(apps, entra, access),
		Teams:        []catalog.Category{{Key: "it", Name: "IT Operations"}},
		Waiting:      2,
		Mappings:     catalog.DefaultMappings(),
	}
}

// fakeCurator keeps what it is given; refuse names a key it fails.
type fakeCurator struct {
	apps         map[string]catalog.AppDetails
	ignored      []string
	capabilities map[string]catalog.Category
	deleted      []string
	lenses       map[string]int
	assessments  []catalog.Assessment
	offered      []orrery.Blueprint
	mappings     map[catalog.MappingKind]catalog.Mapping
	refuse       string
}

func newCurator() *fakeCurator {
	return &fakeCurator{apps: map[string]catalog.AppDetails{}, capabilities: map[string]catalog.Category{}, lenses: map[string]int{}}
}

func (f *fakeCurator) SaveApplication(_ context.Context, key string, d catalog.AppDetails) error {
	if key == f.refuse {
		return errors.New("hub refused")
	}
	f.apps[key] = d
	return nil
}

func (f *fakeCurator) DeleteApplication(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}

func (f *fakeCurator) Ignore(_ context.Context, ref, _ string) error {
	f.ignored = append(f.ignored, ref)
	return nil
}

func (f *fakeCurator) PutCapability(_ context.Context, c catalog.Category) error {
	f.capabilities[c.Key] = c
	return nil
}

func (f *fakeCurator) DeleteCapability(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}

func (f *fakeCurator) PutLens(_ context.Context, blueprint string, order int) error {
	f.lenses[blueprint] = order
	return nil
}

func (f *fakeCurator) DeleteLens(_ context.Context, blueprint string) error {
	f.deleted = append(f.deleted, blueprint)
	return nil
}

func (f *fakeCurator) PutAssessment(_ context.Context, a catalog.Assessment) error {
	f.assessments = append(f.assessments, a)
	return nil
}

func (f *fakeCurator) PutMapping(_ context.Context, kind catalog.MappingKind, m catalog.Mapping, _ string) error {
	if f.mappings == nil {
		f.mappings = map[catalog.MappingKind]catalog.Mapping{}
	}
	f.mappings[kind] = m
	return nil
}

func (f *fakeCurator) Offer(_ context.Context, b orrery.Blueprint) error {
	f.offered = append(f.offered, b)
	return nil
}

var categories = []catalog.Category{
	{Key: "product", Name: "Platform as a product", Order: 1},
	{Key: "catalog", Name: "Catalog and ownership", Order: 2},
	{Key: "cost", Name: "Cost", Order: 3},
}

var fixture = []catalog.App{
	{Key: "portal", Title: "Portal", Icon: "portal", Description: "The door.", Tier: "critical", Owner: "team", URL: "https://portal.example.com", Capabilities: []string{"cost", "product"}, Status: catalog.Green},
	{Key: "ledger", Title: "Ledger", URL: "https://ledger.example.com", Capabilities: []string{"cost"}, Audience: []string{"Finance"}, Status: catalog.Red, Reasons: []string{"two alerts"}},
	{Key: "lab", Title: "Lab", URL: "https://lab.example.com", Status: catalog.Grey},
}

// fakeCoverage serves the catalog package's landscape fixture as the
// pillar lens, with its sources.
type fakeCoverage struct {
	c   catalog.Coverage
	err error
}

func (f *fakeCoverage) read(context.Context) (catalog.Coverage, error) { return f.c, f.err }

func coverageFixture(t *testing.T) catalog.Coverage {
	t.Helper()
	raw, err := os.ReadFile("../catalog/testdata/landscape.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Pillars  struct{ Entities []orrery.Record } `json:"pillars"`
		Services struct{ Entities []orrery.Record } `json:"services"`
		APIs     struct{ Entities []orrery.Record } `json:"apis"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("../catalog/testdata/sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var page struct{ Sources []catalog.Source }
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	order := map[string]catalog.BlueprintBit{"order": {Type: "number"}}
	toPillars := map[string]catalog.BlueprintBit{"pillars": {Target: "plat_pillar", Many: true}}
	blueprints := []catalog.Blueprint{
		{ID: "business_capability", Title: "Business capability", Fields: order},
		{ID: "plat_pillar", Title: "Platform pillar", Fields: order},
		{ID: "team", Title: "Team", Fields: order},
		{ID: "sw_service", Title: "Service", Relations: toPillars},
		{ID: "sw_api", Title: "API", Relations: toPillars},
		{ID: "it_application", Relations: map[string]catalog.BlueprintBit{"business_capabilities": {Target: "business_capability"}, "pillars": {Target: "plat_pillar", Many: true}}},
	}
	return catalog.Coverage{
		Blueprints:   blueprints,
		Lenses:       []catalog.Lens{{Blueprint: "business_capability", Title: "Business capability"}, {Blueprint: "plat_pillar", Title: "Platform pillar", Order: 1}},
		Categories:   map[string][]orrery.Record{"plat_pillar": f.Pillars.Entities},
		Services:     f.Services.Entities,
		APIs:         f.APIs.Entities,
		Sources:      page.Sources,
		SourcesKnown: true,
	}
}

// fakeIcons holds a logo for the portal tile and the beacon card.
type fakeIcons struct{}

func (fakeIcons) Index(context.Context) (catalog.IconIndex, error) {
	return catalog.IconIndex{Release: "1.0.0"}, nil
}

func (fakeIcons) Icon(_ context.Context, name string, _ catalog.Theme, _ string) (catalog.Image, error) {
	switch name {
	case "portal":
		return catalog.Image{Type: "image/svg+xml", Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), ETag: `"0123456789abcdef"`}, nil
	case "beacon":
		return catalog.Image{Type: "image/png", Data: []byte("\x89PNG\r\n\x1a\n"), ETag: `"beef"`}, nil
	}
	return catalog.Image{}, catalog.ErrIconNotFound
}

type harness struct {
	t       *testing.T
	src     *fakeSource
	cov     *fakeCoverage
	srv     *Server
	session http.Handler
	open    http.Handler
	curator *fakeCurator
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeActions lets one person run one action.
type fakeActions struct{}

func (fakeActions) ActionsFor(_ context.Context, email string) ([]orrery.ActionSummary, error) {
	if email != "user@example.com" {
		return nil, nil
	}
	return []orrery.ActionSummary{{ID: "deploy_app", Title: "Deploy the app", Group: "Ops", Approval: true}}, nil
}

// newHarness serves the fixture, relaying browser batches to faroURL with
// a key, or dropping them when faroURL is empty.
func newHarness(t *testing.T, faroURL string) *harness {
	t.Helper()
	return newHarnessWith(t, faroURL, nil)
}

// newHarnessWith is a harness whose config and parts a test changes before
// the server is made.
func newHarnessWith(t *testing.T, faroURL string, change func(*Config, *Parts)) *harness {
	t.Helper()
	src := &fakeSource{apps: fixture, t: t}
	cov := &fakeCoverage{c: coverageFixture(t)}
	var cache *catalog.Cache[catalog.Directory]
	metrics, err := NewMetrics(func() time.Duration { return cache.Age() })
	if err != nil {
		t.Fatal(err)
	}
	icons := catalog.NewIcons(fakeIcons{}, quiet, metrics.IconRead)
	cache = catalog.NewTiles(catalog.WithIcons(src.Directory, icons, catalog.Directory.IconNames), time.Millisecond, quiet, metrics.CatalogRead)
	coverage := catalog.NewCoverageCache(catalog.WithIcons(cov.read, icons, catalog.Coverage.IconNames), time.Millisecond, quiet, metrics.LandscapeRead)
	relay, err := faro.NewRelay(faro.Config{Service: Service, Version: "test", URL: faroURL, Key: "secret"}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	actions := catalog.NewActions(fakeActions{}, "https://hub.example.org", time.Millisecond, quiet, metrics.ActionsRead)
	curator := newCurator()
	cfg := Config{Org: "Example", Version: "test", BrowserTelemetry: true, PublicURL: "https://myapps.example.org", OrreryURL: "https://hub.example.org/"}
	parts := Parts{Tiles: cache, Coverage: coverage, Actions: actions, Icons: icons, Relay: relay, Metrics: metrics, Curator: curator}
	if change != nil {
		change(&cfg, &parts)
	}
	srv, err := New(cfg, parts, quiet)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, src: src, cov: cov, srv: srv, session: srv.Session(), open: srv.Open(), curator: curator}
}

func (h *harness) get(path string, groups string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if groups != "-" {
		req.Header.Set("X-Forwarded-Email", "user@example.com")
		req.Header.Set("X-Forwarded-Groups", groups)
	}
	rec := httptest.NewRecorder()
	h.session.ServeHTTP(rec, req)
	return rec
}

func (h *harness) apps(groups string) appsAnswer {
	rec := h.get("/v1/apps", groups)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("/v1/apps answered %d: %s", rec.Code, rec.Body)
	}
	var out appsAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		h.t.Fatal(err)
	}
	return out
}

func keys(apps []catalog.App) string {
	var ks []string
	for _, a := range apps {
		ks = append(ks, a.Key)
	}
	return strings.Join(ks, ",")
}

func TestSessionNeedsTheFrontDoor(t *testing.T) {
	h := newHarness(t, "")
	for _, path := range []string{"/", "/suite", "/landscape", "/coverage", "/v1/apps", "/v1/landscape", "/open/portal", "/icons/portal", "/admin", "/admin/lenses"} {
		rec := h.get(path, "-")
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Sign in first") {
			t.Fatalf("%s without identity answered %d without the portal's page", path, rec.Code)
		}
	}
	if rec := h.get("/assets/style.css", "-"); rec.Code != http.StatusOK {
		t.Fatalf("assets answered %d", rec.Code)
	}
}

func TestAppsFollowTheGroups(t *testing.T) {
	h := newHarness(t, "")
	if got := keys(h.apps("Staff").Apps); got != "portal,lab" {
		t.Fatalf("staff sees %s", got)
	}
	if got := keys(h.apps("Staff, Finance").Apps); got != "portal,ledger,lab" {
		t.Fatalf("finance sees %s", got)
	}
	if got := keys(h.apps("").Apps); got != "portal,lab" {
		t.Fatalf("no groups sees %s", got)
	}
}

func TestGroupsAreInCategoryOrder(t *testing.T) {
	h := newHarness(t, "")
	out := h.apps("Finance")
	if len(out.Groups) != len(categories)+1 {
		t.Fatalf("got %d groups", len(out.Groups))
	}
	if out.Groups[0].Category.Key != "product" || keys(out.Groups[0].Apps) != "portal" {
		t.Fatalf("first group %+v: an app goes to its first category in order", out.Groups[0])
	}
	if out.Groups[2].Category.Key != "cost" || keys(out.Groups[2].Apps) != "ledger" {
		t.Fatalf("cost group %+v", out.Groups[2])
	}
	if last := out.Groups[len(out.Groups)-1]; last.Category.Key != "other" || keys(last.Apps) != "lab" {
		t.Fatalf("last group %+v", last)
	}
}

func TestPagesRenderTiles(t *testing.T) {
	h := newHarness(t, "")
	rec := h.get("/", "Finance")
	body := rec.Body.String()
	for _, want := range []string{
		`href="/open/portal" data-key="portal" data-category="product"`,
		`href="/open/ledger" data-key="ledger" data-category="cost"`,
		`<h2 id="cat-product" title="">Platform as a product<span class="dim">1</span></h2>`, `<h2 id="cat-cost" title="">Cost<span class="dim">1</span>`,
		`<source media="(prefers-color-scheme: dark)" srcset="/icons/portal?v=0123456789ab">`,
		`<img src="/icons/portal?theme=light&amp;v=0123456789ab" alt=""`,
		"title=\"Portal\nThe door.\nTier: critical, Owner: team\"",
		`<span class="mono" aria-hidden="true">L</span>`,
		`class="badge red" data-dot`, `class="badge green" data-dot`, "two alerts",
		`data-fav aria-pressed="false" aria-label="Add Portal to favorites"`,
		`class="logo mini" data-key="portal"`,
		`data-dgroup="fav"`, `data-dgroup="recent"`, `data-dock="fav"`, `data-dock="recent"`,
		`id="hidden-apps"`, `data-hidden-list`, `data-restore-all`, `data-hidden-link`, `id="toast"`,
		`data-m="fav"`, `data-m="hide"`, `data-m="unrecent"`,
		`data-more aria-haspopup="dialog" aria-label="More about Portal"`,
		`id="dock"`, `id="launch-list"`, `id="tile-menu"`, `id="palette"`,
		"faro-web-sdk.iife.js",
		"Tier: critical, Owner: team\" target=\"_blank\" rel=\"noopener\">",
		`data-m="open" href="/" target="_blank" rel="noopener"`,
		`<input type="checkbox" data-newtab checked>Open apps in a new tab</label>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the apps page lacks %q", want)
		}
	}
	if strings.Contains(body, "Catalog unreachable") && !strings.Contains(body, "hidden") {
		t.Fatal("the banner should be hidden while the catalog answers")
	}
	rec = h.get("/", "Staff")
	if strings.Contains(rec.Body.String(), "/open/ledger") {
		t.Fatal("staff should not see the ledger tile")
	}
}

func TestOpenRedirectsOnlyVisibleTiles(t *testing.T) {
	h := newHarness(t, "")
	rec := h.get("/open/ledger", "Finance")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://ledger.example.com" {
		t.Fatalf("finance opening the ledger got %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := h.get("/open/ledger", "Staff"); rec.Code != http.StatusNotFound {
		t.Fatalf("staff opening the ledger got %d", rec.Code)
	}
	if rec := h.get("/open/nothing", "Staff"); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown app got %d", rec.Code)
	}
}

func TestCatalogFailureKeepsTheLastTiles(t *testing.T) {
	h := newHarness(t, "")
	h.apps("Staff")
	h.src.err = errors.New("orrery answered 503")
	time.Sleep(2 * time.Millisecond)
	out := h.apps("Staff")
	if !out.Catalog.Stale || out.Catalog.Error == "" || keys(out.Apps) != "portal,lab" {
		t.Fatalf("after a failure: %+v", out.Catalog)
	}
	body := h.get("/", "Staff").Body.String()
	if !strings.Contains(body, "Catalog unreachable") || !strings.Contains(body, "/open/portal") {
		t.Fatal("the page should say the catalog is unreachable and still show the tiles")
	}
	if strings.Contains(body, `id="catalog-banner" hidden`) {
		t.Fatal("the banner should be visible")
	}
	fetched := out.Catalog.Fetched.UTC()
	stamp := `<time datetime="` + fetched.Format("2006-01-02T15:04:05Z") + `" data-local="clock">` + fetched.Format("15:04") + ` UTC</time>`
	if !strings.Contains(body, stamp) {
		t.Fatal("the banner should give the last read in UTC, for the page script to show in the person's own clock")
	}
}

func TestCatalogDownFromTheStart(t *testing.T) {
	h := newHarness(t, "")
	h.src.err = errors.New("down")
	rec := h.get("/", "Staff")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No apps to show yet") {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String()[:200])
	}
}

func TestOpenPort(t *testing.T) {
	h := newHarness(t, "")
	rec := httptest.NewRecorder()
	h.open.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Fatalf("healthz %d %q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.open.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/apps", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("the open port should not serve the apps, got %d", rec.Code)
	}
}

// The relay sits on the open port, where nobody is signed in, so a batch
// is the portal's web client's and claims no user.
func TestTelemetryRelay(t *testing.T) {
	var got []byte
	var key string
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		key = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer collector.Close()
	h := newHarness(t, collector.URL)
	req := httptest.NewRequest(http.MethodPost, "/api/telemetry", bytes.NewReader([]byte(`{"meta":{"app":{"name":"evil"},"user":{"email":"user@example.com"}},"events":[{"name":"tile.open"}]}`)))
	req.Header.Set("X-Forwarded-Email", "someone@example.com")
	rec := httptest.NewRecorder()
	h.open.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("a batch answered %d", rec.Code)
	}
	if key != "secret" || !strings.Contains(string(got), `"name":"myapps-web"`) || strings.Contains(string(got), "example.com") {
		t.Fatalf("relayed %s with key %q", got, key)
	}
}

func TestTelemetryWithoutACollectorIsDropped(t *testing.T) {
	h := newHarness(t, "")
	rec := httptest.NewRecorder()
	h.open.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/telemetry", strings.NewReader(`{"events":[]}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d", rec.Code)
	}
}

// A person sees the actions they may run, as links into the hub's forms.
func TestActionsShowForThePerson(t *testing.T) {
	h := newHarness(t, "")
	page := h.get("/", "")
	if page.Code != http.StatusOK {
		t.Fatalf("page: %d", page.Code)
	}
	body := page.Body.String()
	for _, want := range []string{"Things you can do", `href="https://hub.example.org/actions/deploy_app"`, "Deploy the app", "needs approval"} {
		if !strings.Contains(body, want) {
			t.Fatalf("page lacks %q", want)
		}
	}
	var answer struct {
		Actions []catalog.Action `json:"actions"`
	}
	json.Unmarshal(h.get("/v1/apps", "").Body.Bytes(), &answer)
	if len(answer.Actions) != 1 || answer.Actions[0].URL != "https://hub.example.org/actions/deploy_app" {
		t.Fatalf("json actions %+v", answer.Actions)
	}
}

// A logo the caches hold is served from memory with its type and the
// headers that keep an SVG inert; anything else is not found.
func TestIconsAreServedFromTheCache(t *testing.T) {
	h := newHarness(t, "")
	h.get("/", "Staff")
	h.get("/analytics", "Platform")
	rec := h.get("/icons/portal", "Staff")
	if rec.Code != http.StatusOK || rec.Body.String() != `<svg xmlns="http://www.w3.org/2000/svg"/>` {
		t.Fatalf("/icons/portal answered %d %q", rec.Code, rec.Body)
	}
	for header, want := range map[string]string{
		"Content-Type":            "image/svg+xml",
		"Cache-Control":           "private, max-age=300",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'",
		"ETag":                    `"0123456789abcdef"`,
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s is %q, want %q", header, got, want)
		}
	}
	png := h.get("/icons/beacon", "Staff")
	if png.Code != http.StatusOK || png.Header().Get("Content-Type") != "image/png" || png.Header().Get("Content-Security-Policy") != "" {
		t.Fatalf("a png answered %d %v", png.Code, png.Header())
	}
	req := httptest.NewRequest(http.MethodGet, "/icons/portal", nil)
	req.Header.Set("X-Forwarded-Email", "user@example.com")
	req.Header.Set("If-None-Match", `"0123456789abcdef"`)
	again := httptest.NewRecorder()
	h.session.ServeHTTP(again, req)
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("a matching If-None-Match answered %d", again.Code)
	}
	if rec := h.get("/icons/portal?theme=light", "Staff"); rec.Code != http.StatusOK {
		t.Errorf("the light copy answered %d", rec.Code)
	}
	for _, path := range []string{"/icons/stack", "/icons/Portal", "/icons/a-b", "/icons/" + strings.Repeat("a", 64), "/icons/portal?theme=sepia"} {
		if rec := h.get(path, "Staff"); rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", path, rec.Code)
		}
	}
}

// The suite page is the landscape: every category as a column with its
// cards, the matrix, and a panel per card with what it provides and calls.
func TestLandscapePageRenders(t *testing.T) {
	h := newHarness(t, "")
	rec := h.get("/analytics?lens=plat_pillar", "Platform")
	if rec.Code != http.StatusOK {
		t.Fatalf("/coverage answered %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<h2 title="The platform, run like a product."><span class="pname">Platform as a product`,
		`<span class="pname">Cost</span><span class="dim">1</span>`,
		`class="lcard small ours" data-card="git/example/atlas" data-home="catalog" data-origin="built" data-kind="built" data-adoption="in_use"`,
		`class="lcard small ours" data-card="git/example/atlas" data-home="agents"`,
		`class="lcard small planned" data-card="git/example/beacon"`,
		`class="lcard large" data-card="comet"`,
		`class="lcard small ours" data-card="git/example/ember"`,
		`<span class="mk built"><svg`, `<span class="mk oss"><svg`, `<span class="mk host kind-saas"><svg`, `<span class="lname">Atlas</span><span class="lkind">Built here</span>`,
		`<span class="lname">Comet</span><span class="lkind">SaaS</span>`,
		`data-tip="Planned"`,
		`data-tip="Built here, development"`,
		`<div class="lbox" style="--span:3;--fit:3">`,
		`<h3>Catalog and inventory</h3>`,
		`<img src="/icons/beacon?theme=light&amp;v=beef" alt="" decoding="async" data-initial="B">`,
		`class="lstatus amber" data-pdot`,
		`data-zoom="-1"`, `data-zoom="1"`,
		`<tr class="group"><th scope="rowgroup" colspan="5">Built here</th></tr>`,
		`<tr class="group"><th scope="rowgroup" colspan="5">SaaS</th></tr>`,
		`<td data-mark="serves"><span class="mark serves"`,
		`<td data-mark="planned"><span class="mark planned"`,
		`<td class="total thin" title="Catalog and ownership: 1 card serves it, fewer than two">1</td>`,
		`<dialog class="sheet detail" id="detail-git/example/atlas"`,
		`href="https://hub.example.org/entities/sw_service/git/example/atlas"`,
		`<a href="https://git.example.com/atlas" rel="noopener">Repository</a>`,
		`<a href="https://api.comet.example.com" rel="noopener">API address</a>`,
		`<li class="flow"><span class="peer">Atlas catalog API</span></li>`,
		`<li class="flow live"><span class="peer">Beacon alerts</span><span class="carries">from Beacon</span></li>`,
		`<li class="flow live"><span class="peer">Comet</span><span class="carries">outside</span></li>`,
		`<li class="flow wanted"><span class="peer">Nimbus</span><span class="carries">outside</span><span class="chip state">Wanted</span></li>`,
		`<li class="flow live"><span class="peer">git/example/ember</span><span class="carries">Atlas catalog API</span><span class="chip state">Live</span></li>`,
		"More apps",
		`<a href="/analytics?lens=plat_pillar" aria-current="page">Platform pillar</a>`,
		"<h1>Platform pillar</h1>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the landscape page lacks %q", want)
		}
	}
	if strings.Contains(body, `data-card="git/example/ghost"`) || strings.Contains(body, `data-card="git/example/plain"`) {
		t.Fatal("a hidden service, or one with no origin, should not be drawn")
	}
	if strings.Contains(body, "Catalog unreachable") {
		t.Fatal("no banner while the catalog answers")
	}
}

func TestLandscapeJSON(t *testing.T) {
	h := newHarness(t, "")
	rec := h.get("/v1/landscape?lens=plat_pillar", "Platform")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("/v1/landscape answered %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"categories", "cards", "edges", "catalog"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("the answer lacks %q: %s", key, rec.Body)
		}
	}
	var out struct {
		Categories []catalog.Category `json:"categories"`
		Cards      []map[string]any   `json:"cards"`
		Edges      []map[string]any   `json:"edges"`
		Catalog    catalogInfo        `json:"catalog"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Categories) != 4 || out.Categories[0].Key != "product" || len(out.Cards) != 5 || len(out.Edges) != 4 || out.Catalog.Stale || out.Catalog.Fetched == nil {
		t.Fatalf("got %d categories, %d cards, %d edges, %+v", len(out.Categories), len(out.Cards), len(out.Edges), out.Catalog)
	}
	atlas := out.Cards[1]
	for _, field := range []string{"key", "blueprint", "kind", "title", "description", "category", "lifecycle", "adoption", "url", "icon", "categories", "orrery", "status"} {
		if _, ok := atlas[field]; !ok {
			t.Fatalf("a card lacks %q: %v", field, atlas)
		}
	}
	if _, ok := atlas["integration"]; !ok {
		t.Fatalf("a card lacks its integration: %v", atlas)
	}
	var withSources struct {
		Sources      []map[string]any `json:"sources"`
		SourcesKnown bool             `json:"sourcesKnown"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &withSources)
	if !withSources.SourcesKnown || len(withSources.Sources) != 5 || withSources.Sources[0]["actsFor"] != "sw_service:git/example/atlas" || withSources.Sources[0]["writes"] == nil {
		t.Fatalf("sources %v", withSources)
	}
	edge := out.Edges[1]
	if len(edge) != 4 || edge["consumer"] != "git/example/atlas" || edge["api"] != "comet" || edge["provider"] != "" || edge["state"] != "live" {
		t.Fatalf("an edge is %v", edge)
	}
}

func TestLandscapeFailureKeepsTheLast(t *testing.T) {
	h := newHarness(t, "")
	h.get("/analytics?lens=plat_pillar", "Platform")
	h.cov.err = errors.New("orrery answered 503")
	time.Sleep(2 * time.Millisecond)
	body := h.get("/analytics?lens=plat_pillar", "Platform").Body.String()
	if !strings.Contains(body, "Catalog unreachable") || !strings.Contains(body, `data-card="git/example/atlas"`) {
		t.Fatal("the page should say the catalog is unreachable and still draw the landscape")
	}
	var out landscapeAnswer
	_ = json.Unmarshal(h.get("/v1/landscape?lens=plat_pillar", "Platform").Body.Bytes(), &out)
	if !out.Catalog.Stale || len(out.Cards) != 5 {
		t.Fatalf("json after a failure: %+v", out.Catalog)
	}
}

func TestLandscapeEmpty(t *testing.T) {
	h := newHarness(t, "")
	h.cov.c = catalog.Coverage{Lenses: h.cov.c.Lenses, Blueprints: h.cov.c.Blueprints}
	body := h.get("/analytics?lens=plat_pillar", "Platform").Body.String()
	if !strings.Contains(body, "Nothing is placed in Platform pillar yet.") {
		t.Fatal("an empty landscape should say so")
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(h.get("/v1/landscape?lens=plat_pillar", "Platform").Body.Bytes(), &raw)
	if string(raw["categories"]) != "[]" || string(raw["cards"]) != "[]" || string(raw["edges"]) != "[]" {
		t.Fatalf("empty lists should be [], got %s, %s, and %s", raw["categories"], raw["cards"], raw["edges"])
	}
}

// The analysis draws the built cards against the categories and their three
// integration marks, with what backs each in its tooltip.
func TestAnalysisRenders(t *testing.T) {
	h := newHarness(t, "")
	body := h.get("/analytics?lens=plat_pillar", "Platform").Body.String()
	for _, want := range []string{
		`data-view="analysis">Analysis</button>`,
		`<div class="analysis" id="view-analysis" role="tabpanel" aria-labelledby="tab-analysis" hidden>`,
		"A count is not maturity: one that fits a category is enough.",
		`<h2>Apps by owning team</h2>`,
		`<tr class="arow" data-row="git/example/atlas" data-origin="built" data-adoption="in_use"`,
		`<td class="role">Catalog and inventory</td>`,
		`<td data-dot="on" title="Atlas names Catalog and ownership"><i class="d"></i></td>`,
		"title=\"Atlas: Pushes data\nwrote inv_host, inv_thing\nfrom sources atlas, atlas-feed\"",
		"title=\"Atlas: Pushes actions\nput atlas.sync\nfrom source atlas\"",
		"title=\"git/example/ember\nOrrery saw it write no data\"",
		`<script type="application/json" id="radar-tracks">[[{"point":`,
		`<polygon class="area" points="`,
		`<td class="im first" data-mcount>1</td>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the analysis lacks %q", want)
		}
	}
	if strings.Contains(body, `class="arow" data-row="comet"`) || strings.Contains(body, `class="arow" data-row="git/example/beacon"`) {
		t.Fatal("the analysis holds only cards built here")
	}
	if strings.Contains(body, "Integration unknown") {
		t.Fatal("no unknown note while the sources are known")
	}
}

func TestAnalysisSaysWhenIntegrationIsUnknown(t *testing.T) {
	h := newHarness(t, "")
	h.cov.c.Sources, h.cov.c.SourcesKnown = nil, false
	body := h.get("/analytics?lens=plat_pillar", "Platform").Body.String()
	if !strings.Contains(body, "<strong>Integration unknown.</strong>") || !strings.Contains(body, `title="Atlas: Data unknown">?</td>`) || !strings.Contains(body, `data-row="git/example/atlas"`) {
		t.Fatal("an unknown integration is said, and the rows still draw")
	}
	var out landscapeAnswer
	_ = json.Unmarshal(h.get("/v1/landscape?lens=plat_pillar", "Platform").Body.Bytes(), &out)
	if out.SourcesKnown || out.Sources == nil || len(out.Sources) != 0 || out.Cards[1].Integration != nil {
		t.Fatalf("json with unknown sources: %+v", out)
	}
}

func TestServesNamesTheChildCapability(t *testing.T) {
	names := map[string]string{"platform": "Platform", "platform-operate": "Operate"}
	if got := servesOf("Platform", names, []string{"platform-operate"}); got != "Platform · Operate" {
		t.Fatalf("a child is named after its top: %q", got)
	}
	if got := servesOf("Platform", names, []string{"platform"}); got != "Platform" {
		t.Fatalf("the top alone stays alone: %q", got)
	}
}

func TestAppNamingPeopleOpensOnlyForThem(t *testing.T) {
	h := newHarness(t, "")
	h.src.apps = append(slices.Clone(fixture), catalog.App{Key: "jira", Title: "Jira", URL: "https://jira.example.com", People: []string{"named@example.com"}})
	for _, tc := range []struct {
		email string
		want  int
	}{{"Named@Example.com", http.StatusFound}, {"user@example.com", http.StatusNotFound}} {
		rec := h.as(http.MethodGet, "/open/jira", tc.email, "", nil, "")
		if rec.Code != tc.want {
			t.Fatalf("%s opening jira answered %d, want %d", tc.email, rec.Code, tc.want)
		}
		home := h.as(http.MethodGet, "/", tc.email, "", nil, "").Body.String()
		if got := strings.Contains(home, `href="/open/jira"`); got != (tc.want == http.StatusFound) {
			t.Fatalf("%s sees the jira tile on Home: %v", tc.email, got)
		}
	}
	if strings.Contains(h.as(http.MethodGet, "/v1/apps", "named@example.com", "", nil, "").Body.String(), "named@example.com") {
		t.Fatal("the people an app names never leave the server")
	}
}

func TestTheOrganizationsLogoTakesTheMarksPlace(t *testing.T) {
	h := newHarness(t, "")
	if body := h.get("/", "").Body.String(); strings.Contains(body, `class="logo-org"`) || !strings.Contains(body, `class="mark"`) {
		t.Fatal("with no logo set, My Apps' mark shows")
	}
	h.srv.cfg.OrgIcon = "portal"
	body := h.get("/", "").Body.String()
	if !strings.Contains(body, `class="logo-org"`) || !strings.Contains(body, `src="/icons/portal`) || strings.Contains(body, `<span class="mark"`) {
		t.Fatal("a logo the cache holds is drawn in the mark's place")
	}
	h.srv.cfg.OrgIcon = "not-held"
	if body := h.get("/", "").Body.String(); strings.Contains(body, `class="logo-org"`) {
		t.Fatal("a logo not held falls back to the mark")
	}
}

// A device adding the portal to a Home Screen, or installing it on a
// desktop, fetches the icons and the manifest without a session.
func TestInstallFilesAnswerWithoutIdentity(t *testing.T) {
	h := newHarness(t, "")
	sizes := map[string]string{"/apple-touch-icon.png": "180x180", "/icon-192.png": "192x192", "/icon-512.png": "512x512"}
	for path, size := range sizes {
		rec := h.get(path, "-")
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("%s without identity answered %d as %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
		png := rec.Body.Bytes()
		if len(png) < 24 || string(png[1:4]) != "PNG" {
			t.Fatalf("%s is not a PNG", path)
		}
		if got := fmt.Sprintf("%dx%d", binary.BigEndian.Uint32(png[16:20]), binary.BigEndian.Uint32(png[20:24])); got != size {
			t.Fatalf("%s is %s, want %s", path, got, size)
		}
	}
	rec := h.get("/manifest.webmanifest", "-")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("manifest without identity answered %d as %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var manifest struct {
		Display  string `json:"display"`
		StartURL string `json:"start_url"`
		Icons    []struct {
			Src   string `json:"src"`
			Sizes string `json:"sizes"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Display != "standalone" || manifest.StartURL != "/" || len(manifest.Icons) == 0 {
		t.Fatalf("manifest = %+v", manifest)
	}
	for _, icon := range manifest.Icons {
		if sizes[icon.Src] != icon.Sizes {
			t.Fatalf("manifest names %s as %s", icon.Src, icon.Sizes)
		}
	}
	for _, path := range []string{"/", "/admin"} {
		page := h.get(path, "Staff, Admin").Body.String()
		for _, link := range []string{`<link rel="apple-touch-icon" href="/apple-touch-icon.png">`, `<link rel="manifest" href="/manifest.webmanifest">`, `<meta name="apple-mobile-web-app-title" content="My Apps">`} {
			if !strings.Contains(page, link) {
				t.Fatalf("%s lacks %s", path, link)
			}
		}
	}
	if page := h.get("/", "-").Body.String(); !strings.Contains(page, `<link rel="manifest" href="/manifest.webmanifest">`) {
		t.Fatal("the sign-in page lacks the manifest")
	}
}
