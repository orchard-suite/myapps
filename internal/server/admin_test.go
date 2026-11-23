package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/orchard-suite/myapps/internal/catalog"
	"github.com/orchard-suite/platformkit/orrery"
)

// withProducts is a harness whose catalog holds the products fixture.
func withProducts(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, "")
	h.src.entra = true
	raw, err := os.ReadFile("../catalog/testdata/products.json")
	if err != nil {
		t.Fatal(err)
	}
	var f map[string][]orrery.Record
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	h.cov.c.Categories["business_capability"] = f["business_capability"]
	return h
}

// as is a request from someone the front door signed in.
func (h *harness) as(method, path, email, groups string, form url.Values, origin string) *httptest.ResponseRecorder {
	body := strings.NewReader("")
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("X-Forwarded-Email", email)
	req.Header.Set("X-Forwarded-Groups", groups)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.session.ServeHTTP(rec, req)
	return rec
}

const own = "https://myapps.example.org"

func TestEntraTilesFollowWhatOrreryHolds(t *testing.T) {
	h := withProducts(t)
	rec := h.as(http.MethodGet, "/v1/apps", "finance@example.com", "", nil, "")
	if !strings.Contains(rec.Body.String(), `"payroll"`) || strings.Contains(rec.Body.String(), `"portal"`) {
		t.Fatalf("finance sees payroll through its group, not the portal: %s", rec.Body)
	}
	for _, leak := range []string{"g-finance", "u-fin", "signIn"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("/v1/apps must not name who may sign in, found %q", leak)
		}
	}
	if rec := h.as(http.MethodGet, "/open/payroll", "staff@example.com", "", nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("a tile Entra keeps from them is not there, got %d", rec.Code)
	}
	if rec := h.as(http.MethodGet, "/open/payroll", "finance@example.com", "", nil, ""); rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://payroll.example.com" {
		t.Fatalf("finance opens payroll at its Entra app's address, got %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestHomeGroupsByCapability(t *testing.T) {
	h := withProducts(t)
	body := h.as(http.MethodGet, "/", "finance@example.com", "", nil, "").Body.String()
	for _, want := range []string{`data-category="everyday"`, `<h2 id="cat-everyday" title="">Everyday tools<span class="dim">1</span></h2>`, `<a class="mm" href="#cat-everyday" style="--n:1">`, `<h2 id="cat-finance" title="">Finance<span class="dim">1</span>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("home lacks %q", want)
		}
	}
}

func TestLandscapeIsApplicationsOnly(t *testing.T) {
	h := withProducts(t)
	rec := h.as(http.MethodGet, "/landscape", "staff@example.com", "", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("/landscape answered %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"The Example landscape",
		`class="lcard large ours" data-card="portal"`, `<span class="mk host kind-self_hosted"><svg`, `<span class="mk built"><svg viewBox="0 0 24 24" aria-hidden="true"><path class="seal"`, `data-origin="self_hosted built" data-kind="built"`,
		`<td class="layers"><span class="layer"><span class="kmark kind-self_hosted"><svg`, `Self-hosted</span><span class="layer"><span class="kmark kind-built"><svg`,
		`class="lcard small locked" data-card="payroll"`,
		`class="lcard small" data-card="wiki"`,
		`class="lcard small" data-card="front"`, `<span class="dim">No tile</span>`,
		`<a class="button" href="/open/portal"`, `target="_blank" rel="noopener">Open Portal</a>`,
		`id="view-list"`, `data-view="list"`, `<option value="yes">Only mine</option>`,
		"<h3>Runs</h3>", `<a class="carries" href="https://git.example.com/atlas" rel="noopener">Repository</a>`,
		`<li><span class="kmark kind-self_hosted"><svg`, `<li><span class="kmark kind-built"><svg`, "<td>Everyday tools</td>", "<td>Finance: Payroll</td>",
		`<h2 title=""><span class="pname">Everyday tools</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the landscape lacks %q", want)
		}
	}
	for _, not := range []string{`href="/open/front"`, `data-fav-key="front"`, `<li><span class="kmark kind-installed">`, `data-card="git/example/atlas"`, `class="lenses"`, "Request access", "Apps people open", "<h3>Not sorted</h3>"} {
		if strings.Contains(body, not) {
			t.Fatalf("the landscape has %q: no services, no lens tabs, no request without Orrery's action", not)
		}
	}
	if rec := h.as(http.MethodGet, "/open/front", "staff@example.com", "", nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("an application hidden from login does not open from My Apps: %d", rec.Code)
	}
	if rec := h.get("/suite", ""); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/landscape" {
		t.Fatalf("/suite moved to /landscape, got %d", rec.Code)
	}
}

func TestLandscapeOffersTheRequestForTheAppsMode(t *testing.T) {
	h := withProducts(t)
	h.srv.actions = catalog.NewActions(requestActions{}, "https://hub.example.org", 0, quiet, nil)
	h.session = h.srv.Session()
	body := h.as(http.MethodGet, "/landscape", "staff@example.com", "", nil, "").Body.String()
	if !strings.Contains(body, `<a class="button wide" href="https://hub.example.org/actions/request_group_access?entity=it_application%3Apayroll" rel="noopener">Request access</a>`) {
		t.Fatalf("payroll grants through a group, so its request is the group request, filled in: %s", body)
	}
}

func TestAnalyticsIsForThePlatformRole(t *testing.T) {
	h := withProducts(t)
	for _, path := range []string{"/analytics", "/admin/lenses"} {
		if rec := h.as(http.MethodGet, path, "user@example.com", "User", nil, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("%s opened for a user: %d", path, rec.Code)
		}
		for _, role := range []string{"Platform", "Admin"} {
			if rec := h.as(http.MethodGet, path, "user@example.com", role, nil, ""); rec.Code != http.StatusOK {
				t.Fatalf("%s for %s answered %d", path, role, rec.Code)
			}
		}
	}
	if rec := h.as(http.MethodGet, "/coverage?lens=plat_pillar", "user@example.com", "Platform", nil, ""); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/analytics?lens=plat_pillar" {
		t.Fatalf("/coverage moved to /analytics, got %d %s", rec.Code, rec.Header().Get("Location"))
	}
	body := h.as(http.MethodGet, "/analytics?lens=plat_pillar", "user@example.com", "Platform", nil, "").Body.String()
	for _, want := range []string{"<h1>Platform pillar</h1>", "Strength of each category", "<h2>Overlap</h2>", `data-card="git/example/atlas"`, `data-card="portal"`, `<summary>Assess<span class="for sr"> Catalog and ownership</span></summary>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("pillar analytics lacks %q", want)
		}
	}
	if home := h.as(http.MethodGet, "/", "user@example.com", "User", nil, "").Body.String(); strings.Contains(home, `href="/analytics"`) {
		t.Fatal("a user sees no analytics link")
	}
}

func TestAssess(t *testing.T) {
	h := withProducts(t)
	form := url.Values{"lens": {"plat_pillar"}, "category": {"catalog"}, "rating": {"4"}, "covers": {"Orrery"}, "missing": {"a scorecard"}}
	rec := h.as(http.MethodPost, "/analytics/assess", "user@example.com", "Platform", form, own)
	if rec.Code != http.StatusSeeOther || len(h.curator.assessments) != 1 || h.curator.assessments[0].Rating != 4 || h.curator.assessments[0].By != "user@example.com" {
		t.Fatalf("got %d %+v", rec.Code, h.curator.assessments)
	}
	for name, bad := range map[string]url.Values{
		"a rating of 6":       {"lens": {"plat_pillar"}, "category": {"catalog"}, "rating": {"6"}},
		"an unknown category": {"lens": {"plat_pillar"}, "category": {"made-up"}, "rating": {"3"}},
		"an unknown lens":     {"lens": {"made_up"}, "category": {"catalog"}, "rating": {"3"}},
	} {
		if rec := h.as(http.MethodPost, "/analytics/assess", "user@example.com", "Platform", bad, own); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s answered %d", name, rec.Code)
		}
	}
}

func TestAdminApplications(t *testing.T) {
	h := withProducts(t)
	if rec := h.as(http.MethodGet, "/admin", "user@example.com", "Platform", nil, ""); rec.Code != http.StatusForbidden {
		t.Fatal("the applications are for admins only")
	}
	body := h.as(http.MethodGet, "/admin", "user@example.com", "Admin", nil, "").Body.String()
	for _, want := range []string{
		`<main id="main" class="page-admin fill">`, `Added<span class="n" id="apps-count">6</span>`, `Waiting<span class="n">4</span>`,
		`<div class="gwrap" id="grid" data-writable="true">`, `<script src="/assets/grid.js?v=test" defer></script>`, `<link rel="stylesheet" href="/assets/style.css?v=test">`,
		`<form class="gadd" id="grid-add" method="post" action="/admin/applications">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the admin page lacks %q", want)
		}
	}
	if strings.Contains(body, `value="entra_app:tenant-1/app-crm"`) {
		t.Fatal("the suggestions are on their own sheet")
	}
	waiting := h.as(http.MethodGet, "/admin?sheet=waiting", "user@example.com", "Admin", nil, "").Body.String()
	for _, want := range []string{`value="entra_app:tenant-1/app-crm"`, `<form class="rowone" method="post" action="/admin/suggestions/add">`, `<a href="/admin?sheet=waiting" aria-current="page"`} {
		if !strings.Contains(waiting, want) {
			t.Fatalf("the waiting sheet lacks %q", want)
		}
	}
	if strings.Contains(waiting, `id="grid-data"`) {
		t.Fatal("the waiting sheet carries no grid")
	}
	if home := h.as(http.MethodGet, "/", "user@example.com", "Admin", nil, "").Body.String(); !strings.Contains(home, `<span class="nbadge">4</span>`) {
		t.Fatal("an admin sees how many suggestions wait")
	}
	g := gridIn(t, body)
	var ids []string
	for _, c := range g.Columns {
		ids = append(ids, c.ID)
	}
	for _, want := range []string{colName, colListed, colHosting, colCapability, "place.pillars", colTeam, colSignIn, colRole, colGroup, colWho, colServices, colWorkload, colAPI} {
		if !slices.Contains(ids, want) {
			t.Fatalf("the grid has no %s column: %v", want, ids)
		}
	}
	if len(g.Rows) != 6 || !g.Writable {
		t.Fatalf("one row per application, editable: %d rows", len(g.Rows))
	}
	portal := rowIn(t, g, "portal")
	if !slices.Equal(portal.Values[colSignIn], []string{"tenant-1/app-portal"}) || !slices.Equal(portal.Values[colServices], []string{"git/example/atlas"}) || portal.Values[colWho][0] == "" {
		t.Fatalf("a row carries every value: %+v", portal.Values)
	}
	if front := rowIn(t, g, "front"); front.Values[colListed][0] != "no_login" {
		t.Fatalf("hidden from login is a value of In My Apps: %v", front.Values[colListed])
	}
	col := func(id string) gridColumn {
		i := slices.IndexFunc(g.Columns, func(c gridColumn) bool { return c.ID == id })
		return g.Columns[i]
	}
	if caps := col(colCapability).Options; len(caps) != 4 || caps[2].Key != "finance-payroll" || caps[2].Depth != 1 {
		t.Fatalf("capabilities are offered as their tree: %+v", caps)
	}
	groups := rowIn(t, g, "payroll").Options[colGroup]
	if !col(colGroup).PerRow || !slices.ContainsFunc(groups, func(o gridOption) bool { return o.Key == "tenant-1/g-finance" }) {
		t.Fatalf("the groups a request may add to hang on each row's Entra apps: %+v", groups)
	}
	if strings.Contains(body, "u-fin") {
		t.Fatal("no person who holds an app leaves the server")
	}
}

// gridIn reads the grid the admin page carries.
func gridIn(t *testing.T, body string) adminGrid {
	t.Helper()
	const open = `<script type="application/json" id="grid-data">`
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatal("the page carries no grid")
	}
	raw := body[i+len(open):]
	raw = raw[:strings.Index(raw, "</script>")]
	var g adminGrid
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		t.Fatalf("the grid is not JSON: %v", err)
	}
	return g
}

func rowIn(t *testing.T, g adminGrid, key string) gridRow {
	t.Helper()
	i := slices.IndexFunc(g.Rows, func(r gridRow) bool { return r.Key == key })
	if i < 0 {
		t.Fatalf("no row %s", key)
	}
	return g.Rows[i]
}

// asGrid is a change sent the way the grid sends it, asking for JSON.
func (h *harness) asGrid(path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("X-Forwarded-Email", "user@example.com")
	req.Header.Set("X-Forwarded-Groups", "Admin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", own)
	rec := httptest.NewRecorder()
	h.session.ServeHTTP(rec, req)
	return rec
}

func TestSaveApplication(t *testing.T) {
	h := withProducts(t)
	form := url.Values{"name": {"CRM"}, "url": {"https://crm.example.com"}, "hosting": {"saas"}, "access": {"direct"}, "role": {"Crm.Admin"},
		"sign_in": {"tenant-1/app-crm"}, "capability": {"everyday"}, "team": {"it"}, "services": {"git/example/atlas"}, "place.pillars": {"catalog"}}
	if rec := h.as(http.MethodPost, "/admin/applications", "user@example.com", "Admin", form, "https://evil.example.org"); rec.Code != http.StatusForbidden || len(h.curator.apps) != 0 {
		t.Fatalf("a change from another origin was taken: %d", rec.Code)
	}
	rec := h.as(http.MethodPost, "/admin/applications", "user@example.com", "Admin", form, own)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin?done=1" {
		t.Fatalf("got %d %s", rec.Code, rec.Header().Get("Location"))
	}
	d, ok := h.curator.apps["crm"]
	if !ok || d.Name != "CRM" || d.Hosting != catalog.SaaS || !slices.Equal(d.SignIn, []string{"tenant-1/app-crm"}) || !slices.Equal(d.Capabilities, []string{"everyday"}) ||
		!slices.Equal(d.Services, []string{"git/example/atlas"}) || !slices.Equal(d.Placed["pillars"], []string{"catalog"}) || d.AccessRole != "Crm.Admin" || d.By != "user@example.com" {
		t.Fatalf("saved %+v", d)
	}
	for name, bad := range map[string]url.Values{
		"no name":              {"name": {""}},
		"a plain http address": {"name": {"X"}, "url": {"http://x.example.com"}},
		"an unknown hosting":   {"name": {"X"}, "hosting": {"moon"}},
		"an unknown workload":  {"name": {"X"}, "workload": {"made-up"}},
		"an unknown service":   {"name": {"X"}, "services": {"git/example/atlas, made-up"}},
		"an unknown sign-in":   {"name": {"X"}, "sign_in": {"made-up"}},
		"an unknown category":  {"name": {"X"}, "capability": {"made-up"}},
		"an unknown placing":   {"name": {"X"}, "place.pillars": {"made-up"}},
		"a role with no app":   {"name": {"X"}, "role": {"Crm.Admin"}},
		"a bad icon":           {"name": {"X"}, "icon": {"Not An Icon"}},
		"an unknown listing":   {"name": {"X"}, "listed": {"nowhere"}},
		"a self built maybe":   {"name": {"X"}, "self_built": {"maybe"}},
	} {
		if rec := h.as(http.MethodPost, "/admin/applications", "user@example.com", "Admin", bad, own); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s answered %d", name, rec.Code)
		}
	}
	if rec := h.as(http.MethodPost, "/admin/applications/made-up", "user@example.com", "Admin", form, own); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown application answered %d", rec.Code)
	}
	if rec := h.as(http.MethodPost, "/admin/applications/old/delete", "user@example.com", "Admin", url.Values{}, own); rec.Code != http.StatusSeeOther || !slices.Contains(h.curator.deleted, "old") {
		t.Fatal("an application is removed")
	}
}

func TestGridChangesOneField(t *testing.T) {
	h := withProducts(t)
	rec := h.asGrid("/admin/applications/portal", url.Values{"hosting": {"saas"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("a cell change answered %d: %s", rec.Code, rec.Body)
	}
	d := h.curator.apps["portal"]
	if d.Hosting != catalog.SaaS || d.Name != "Portal" || !slices.Equal(d.SignIn, []string{"tenant-1/app-portal"}) || !slices.Equal(d.Services, []string{"git/example/atlas"}) || d.Workload != "portal" {
		t.Fatalf("only the field named changes, the rest keep: %+v", d)
	}
	if !slices.Equal(d.Placed["pillars"], []string{"catalog"}) {
		t.Fatalf("its place on each lens is kept: %v", d.Placed)
	}
	var answer struct{ Row gridRow }
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil || answer.Row.Key != "portal" || answer.Row.Values[colHosting][0] != "saas" {
		t.Fatalf("the saved row comes back: %s", rec.Body)
	}
	if rec := h.asGrid("/admin/applications/portal", url.Values{"self_built": {"yes"}}); rec.Code != http.StatusOK || !h.curator.apps["portal"].SelfBuilt || h.curator.apps["portal"].Hosting != catalog.SelfHosted {
		t.Fatalf("self built is set on its own: %d %+v", rec.Code, h.curator.apps["portal"])
	}
	if rec := h.asGrid("/admin/applications/portal", url.Values{"self_built": {"no"}}); rec.Code != http.StatusOK || h.curator.apps["portal"].SelfBuilt {
		t.Fatalf("and cleared: %d", rec.Code)
	}
	if rec := h.asGrid("/admin/applications/portal", url.Values{"capability": {""}}); rec.Code != http.StatusOK || len(h.curator.apps["portal"].Capabilities) != 0 {
		t.Fatalf("an empty list clears it: %d %v", rec.Code, h.curator.apps["portal"].Capabilities)
	}
	if rec := h.asGrid("/admin/applications/portal", url.Values{"capability": {"everyday", "finance"}}); rec.Code != http.StatusOK || !slices.Equal(h.curator.apps["portal"].Capabilities, []string{"everyday", "finance"}) {
		t.Fatalf("a list is its values repeated: %v", h.curator.apps["portal"].Capabilities)
	}
	if rec := h.asGrid("/admin/applications/payroll", url.Values{"sign_in": {""}}); rec.Code != http.StatusOK || !slices.Equal(h.curator.apps["payroll"].AccessGroups, []string{"tenant-1/g-finance"}) {
		t.Fatalf("with no Entra app its access groups stay, and now decide who opens it: %+v", h.curator.apps["payroll"])
	}
	if rec := h.asGrid("/admin/applications/payroll", url.Values{"people": {" Someone@Example.com, someone@example.com,other@example.com"}}); rec.Code != http.StatusOK || !slices.Equal(h.curator.apps["payroll"].People, []string{"someone@example.com", "other@example.com"}) {
		t.Fatalf("people are emails, lowercased, each once: %d %v", rec.Code, h.curator.apps["payroll"].People)
	}
	if rec := h.asGrid("/admin/applications/payroll", url.Values{"people": {"not an email"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a person who is not an email is refused: %d", rec.Code)
	}
	rec = h.asGrid("/admin/applications/portal", url.Values{"url": {"http://plain.example.com"}})
	var refused struct{ Error string }
	if rec.Code != http.StatusBadRequest || json.Unmarshal(rec.Body.Bytes(), &refused) != nil || !strings.Contains(refused.Error, "https://") {
		t.Fatalf("a refused change says why, as JSON: %d %s", rec.Code, rec.Body)
	}
	if rec := h.asGrid("/admin/applications/made-up", url.Values{"hosting": {"saas"}}); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown application answered %d", rec.Code)
	}
	h.curator.refuse = "wiki"
	if rec := h.asGrid("/admin/applications/wiki", url.Values{"tier": {"critical"}}); rec.Code != http.StatusBadGateway {
		t.Fatalf("a write Orrery refused answered %d", rec.Code)
	}
	add := h.asGrid("/admin/applications", url.Values{"name": {"New Thing"}})
	if add.Code != http.StatusOK || h.curator.apps["new-thing"].Name != "New Thing" || h.curator.apps["new-thing"].Listed != "" {
		t.Fatalf("a name alone adds an application: %d %s", add.Code, add.Body)
	}
	if rec := h.asGrid("/admin/applications/old/delete", url.Values{}); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"removed":"old"`) {
		t.Fatalf("a removal from the grid answers JSON: %d %s", rec.Code, rec.Body)
	}
}

func TestAttachAndIgnore(t *testing.T) {
	h := withProducts(t)
	rec := h.as(http.MethodPost, "/admin/suggestions/attach", "user@example.com", "Admin", url.Values{"ref": {"entra_app:tenant-1/app-crm"}, "application": {"portal"}}, own)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("attach answered %d", rec.Code)
	}
	d := h.curator.apps["portal"]
	if !slices.Equal(d.SignIn, []string{"tenant-1/app-portal", "tenant-1/app-crm"}) || d.Workload != "portal" || d.Hosting != catalog.SelfHosted || !slices.Equal(d.Services, []string{"git/example/atlas"}) {
		t.Fatalf("attaching adds the sign-in and keeps the rest: %+v", d)
	}
	if rec := h.as(http.MethodPost, "/admin/suggestions/attach", "user@example.com", "Admin", url.Values{"ref": {"entra_app:made-up"}, "application": {"portal"}}, own); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown suggestion answered %d", rec.Code)
	}
	rec = h.as(http.MethodPost, "/admin/suggestions/ignore", "user@example.com", "Admin", url.Values{"ref": {"entra_app:tenant-1/app-nourl", "entra_app:made-up"}}, own)
	if rec.Header().Get("Location") != "/admin?done=1&failed=1&sheet=waiting" || !slices.Equal(h.curator.ignored, []string{"entra_app:tenant-1/app-nourl"}) {
		t.Fatalf("dismissed %v, %s", h.curator.ignored, rec.Header().Get("Location"))
	}
}

func TestMakeApplications(t *testing.T) {
	h := withProducts(t)
	page := h.as(http.MethodGet, "/admin?sheet=waiting", "user@example.com", "Admin", nil, "").Body.String()
	if !strings.Contains(page, `<h3 class="qgroup">Open source`) || !strings.Contains(page, `<h3 class="qgroup">Outside products`) || !strings.Contains(page, `formaction="/admin/suggestions/ignore"`) {
		t.Fatal("open-source services and outside products are suggested, with one bulk form")
	}
	rec := h.as(http.MethodPost, "/admin/suggestions/add", "user@example.com", "Admin", url.Values{"ref": {"sw_service:git/example/beacon", "sw_api:nimbus", "sw_api:made-up"}, "capability": {"everyday"}}, own)
	if rec.Header().Get("Location") != "/admin?done=2&failed=1&sheet=waiting" {
		t.Fatalf("made %s", rec.Header().Get("Location"))
	}
	beacon, nimbus := h.curator.apps["beacon"], h.curator.apps["nimbus"]
	if beacon.Hosting != catalog.SelfHosted || !slices.Equal(beacon.Services, []string{"git/example/beacon"}) || !slices.Equal(beacon.Placed["pillars"], []string{"catalog"}) || !slices.Equal(beacon.Capabilities, []string{"everyday"}) || beacon.By != "user@example.com" {
		t.Fatalf("beacon %+v", beacon)
	}
	if nimbus.Hosting != catalog.SaaS || nimbus.API != "nimbus" || !slices.Equal(nimbus.Capabilities, []string{"everyday"}) {
		t.Fatalf("nimbus %+v", nimbus)
	}
	if rec := h.as(http.MethodPost, "/admin/suggestions/add", "user@example.com", "Admin", url.Values{"ref": {"sw_api:nimbus"}, "capability": {"made-up"}}, own); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown capability answered %d", rec.Code)
	}
	if rec := h.as(http.MethodPost, "/admin/suggestions/add", "user@example.com", "Platform", url.Values{"ref": {"sw_api:nimbus"}}, own); rec.Code != http.StatusForbidden {
		t.Fatal("making applications is for admins")
	}
}

func TestCapabilities(t *testing.T) {
	h := withProducts(t)
	body := h.as(http.MethodGet, "/admin/capabilities", "user@example.com", "Admin", nil, "").Body.String()
	if !strings.Contains(body, "<strong>Payroll</strong> <span class=\"dim\">1 app</span>") || !strings.Contains(body, `padding-left:36px`) {
		t.Fatal("the tree shows each capability at its depth with how many apps use it")
	}
	add := h.as(http.MethodPost, "/admin/capabilities", "user@example.com", "Admin", url.Values{"name": {"Fixed assets"}, "parent": {"finance"}}, own)
	if add.Code != http.StatusSeeOther || h.curator.capabilities["finance-fixed-assets"].Parent != "finance" || h.curator.capabilities["finance-fixed-assets"].Order != 2 {
		t.Fatalf("add answered %d: %+v", add.Code, h.curator.capabilities)
	}
	rename := h.as(http.MethodPost, "/admin/capabilities/finance", "user@example.com", "Admin", url.Values{"name": {"Money"}, "order": {"4"}}, own)
	if rename.Code != http.StatusSeeOther || h.curator.capabilities["finance"].Name != "Money" || h.curator.capabilities["finance"].Order != 4 {
		t.Fatalf("a rename keeps the key: %+v", h.curator.capabilities["finance"])
	}
	if rec := h.as(http.MethodPost, "/admin/capabilities/finance", "user@example.com", "Admin", url.Values{"name": {"Money"}, "parent": {"finance-payroll"}}, own); rec.Code != http.StatusBadRequest {
		t.Fatalf("a capability under its own child answered %d", rec.Code)
	}
	if rec := h.as(http.MethodPost, "/admin/capabilities/finance/delete", "user@example.com", "Admin", url.Values{}, own); rec.Code != http.StatusBadRequest {
		t.Fatalf("removing a capability with children answered %d", rec.Code)
	}
	if rec := h.as(http.MethodPost, "/admin/capabilities/sales/delete", "user@example.com", "Admin", url.Values{}, own); rec.Code != http.StatusSeeOther || !slices.Contains(h.curator.deleted, "sales") {
		t.Fatalf("removing a leaf answered %d", rec.Code)
	}
	if rec := h.as(http.MethodPost, "/admin/capabilities", "user@example.com", "Platform", url.Values{"name": {"X"}}, own); rec.Code != http.StatusForbidden {
		t.Fatal("capabilities are for admins")
	}
}

func TestLenses(t *testing.T) {
	h := withProducts(t)
	h.cov.c.Blueprints = append(h.cov.c.Blueprints, catalog.Blueprint{ID: "dev_pillar", Title: "Development pillar", Fields: map[string]catalog.BlueprintBit{"order": {Type: "number"}}})
	body := h.as(http.MethodGet, "/admin/lenses", "user@example.com", "Platform", nil, "").Body.String()
	if !strings.Contains(body, `<option value="dev_pillar">Development pillar (dev_pillar)</option>`) || strings.Contains(body, `<option value="business_capability">`) {
		t.Fatal("a classification that is not yet a lens is offered; capabilities always are one")
	}
	if rec := h.as(http.MethodPost, "/admin/lenses", "user@example.com", "Platform", url.Values{"blueprint": {"dev_pillar"}}, own); rec.Code != http.StatusSeeOther || h.curator.lenses["dev_pillar"] == 0 {
		t.Fatalf("adding a lens answered %d: %v", rec.Code, h.curator.lenses)
	}
	if len(h.curator.offered) != 1 || h.curator.offered[0]["id"] != "it_application" {
		t.Fatalf("a new lens gives applications a relation to it: %v", h.curator.offered)
	}
	if rels, _ := h.curator.offered[0]["relations"].(map[string]any); rels["dev_pillars"] == nil {
		t.Fatalf("named after the whole id when its thing's name is taken: %v", rels)
	}
	if rec := h.as(http.MethodPost, "/admin/lenses/business_capability/delete", "user@example.com", "Platform", url.Values{}, own); rec.Code != http.StatusNotFound {
		t.Fatal("the capability lens cannot be removed")
	}
}

func TestChangesOffWithoutAPublicURL(t *testing.T) {
	h := withProducts(t)
	h.srv.cfg.PublicURL = ""
	h.session = h.srv.Session()
	if rec := h.as(http.MethodPost, "/admin/applications", "user@example.com", "Admin", url.Values{"name": {"X"}}, own); rec.Code != http.StatusServiceUnavailable || len(h.curator.apps) != 0 {
		t.Fatalf("got %d", rec.Code)
	}
	if body := h.as(http.MethodGet, "/admin", "user@example.com", "Admin", nil, "").Body.String(); !strings.Contains(body, "Changes are off.") || strings.Contains(body, `id="grid-add"`) || !strings.Contains(body, `data-writable="false"`) {
		t.Fatal("the page says changes are off and offers no forms")
	}
}

// requestActions lets everyone run both of Orrery's request actions.
type requestActions struct{}

func (requestActions) ActionsFor(context.Context, string) ([]orrery.ActionSummary, error) {
	return []orrery.ActionSummary{{ID: "request_app_access", Title: "Ask for an app"}, {ID: "request_group_access", Title: "Ask to join an app's group"}}, nil
}

func TestSignOutLink(t *testing.T) {
	h := withProducts(t)
	if strings.Contains(h.as(http.MethodGet, "/", "user@example.com", "", nil, "").Body.String(), "Sign out") {
		t.Fatal("no sign-out without SIGNOUT_URL")
	}
	h.srv.cfg.SignOutURL = "/oauth2/sign_out"
	if !strings.Contains(h.as(http.MethodGet, "/", "user@example.com", "", nil, "").Body.String(), `<a class="signout" href="/oauth2/sign_out">Sign out</a>`) {
		t.Fatal("the header carries the front door's sign-out")
	}
}

func TestAttachingGivesALogolessApplicationTheLogo(t *testing.T) {
	h := withProducts(t)
	attach := func(app string) catalog.AppDetails {
		t.Helper()
		if rec := h.as(http.MethodPost, "/admin/suggestions/attach", "user@example.com", "Admin", url.Values{"ref": {"sw_service:git/example/beacon"}, "application": {app}}, own); rec.Code != http.StatusSeeOther {
			t.Fatalf("attach answered %d", rec.Code)
		}
		return h.curator.apps[app]
	}
	if d := attach("payroll"); d.Icon != "beacon" {
		t.Fatalf("an application with no logo takes the attached one's: %q", d.Icon)
	}
	if d := attach("wiki"); d.Icon != "wiki" {
		t.Fatalf("an application's own logo stays: %q", d.Icon)
	}
	h = withProducts(t)
	h.cov.c.Blueprints = append(h.cov.c.Blueprints, catalog.Blueprint{ID: "entra_app", Icon: "wiki"})
	if d := attach("wiki"); d.Icon != "beacon" {
		t.Fatalf("a logo that is only a blueprint's gives way to the attached one's: %q", d.Icon)
	}
}
