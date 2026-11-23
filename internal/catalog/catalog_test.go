package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/orchard-suite/platformkit/orrery"
)

func TestVisibleTo(t *testing.T) {
	cases := []struct {
		name     string
		audience []string
		groups   []string
		want     bool
	}{
		{"no audience is for everyone", nil, nil, true},
		{"empty audience is for everyone", []string{}, []string{"Staff"}, true},
		{"a matching group", []string{"Platform"}, []string{"Staff", "Platform"}, true},
		{"case and space do not matter", []string{"platform "}, []string{" PLATFORM"}, true},
		{"no matching group", []string{"Platform"}, []string{"Staff"}, false},
		{"no groups at all", []string{"Platform"}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := (App{Audience: tc.audience}).VisibleTo(Viewer{Roles: tc.groups}); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVisibleToWithNoEntraApp(t *testing.T) {
	inFinance := &Person{ID: "u-1", Groups: []string{"g-finance"}}
	jira := App{Audience: []string{"Engineering"}, Groups: []string{"g-finance"}, People: []string{"named@example.com"}}
	cases := []struct {
		name string
		app  App
		v    Viewer
		want bool
	}{
		{"naming no one is for everyone", App{}, Viewer{Email: "anyone@example.com"}, true},
		{"a role it names", jira, Viewer{Roles: []string{"engineering"}}, true},
		{"a group they are directly in", jira, Viewer{Person: inFinance}, true},
		{"their email, whatever its case", jira, Viewer{Email: " Named@Example.COM"}, true},
		{"a person Entra does not hold, named", App{People: []string{"guest@example.com"}}, Viewer{Email: "guest@example.com"}, true},
		{"none of them", jira, Viewer{Email: "other@example.com", Roles: []string{"Staff"}, Person: &Person{ID: "u-2", Groups: []string{"g-sales"}}}, false},
		{"only groups named, and Entra does not hold them", App{Groups: []string{"g-finance"}}, Viewer{Email: "guest@example.com"}, false},
		{"only people named, no email", App{People: []string{"named@example.com"}}, Viewer{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, why := tc.app.VisibleTo(tc.v); got != tc.want {
				t.Fatalf("got %v (%s), want %v", got, why, tc.want)
			}
		})
	}
}

func TestVisibleToWithAnEntraAppIgnoresGroupsAndPeople(t *testing.T) {
	a := App{SignIn: &SignIn{Users: []string{"u-1"}}, Groups: []string{"g-finance"}, People: []string{"named@example.com"}}
	if ok, _ := a.VisibleTo(Viewer{Email: "named@example.com", Person: &Person{ID: "u-2", Groups: []string{"g-finance"}}}); ok {
		t.Fatal("Entra decides an app it signs people in to; its groups and people do not open it")
	}
	if ok, _ := a.VisibleTo(Viewer{Person: &Person{ID: "u-1"}}); !ok {
		t.Fatal("someone Entra lets sign in sees it")
	}
}

func TestFilterKeepsOnlyVisible(t *testing.T) {
	apps := []App{
		{Key: "open"},
		{Key: "platform", Audience: []string{"Platform"}},
		{Key: "finance", Audience: []string{"Finance"}},
	}
	got := Filter(context.Background(), quiet(), apps, Viewer{Roles: []string{"Platform"}})
	if len(got) != 2 || got[0].Key != "open" || got[1].Key != "platform" {
		t.Fatalf("got %+v", got)
	}
}

var testCategories = []Category{
	{Key: "product", Name: "Product", Order: 1},
	{Key: "cost", Name: "Cost", Order: 2},
	{Key: "cost-invoices", Name: "Invoices", Order: 3, Parent: "cost"},
}

func TestGroupBy(t *testing.T) {
	apps := []App{
		{Key: "a", Capabilities: []string{"made-up", "cost-invoices"}},
		{Key: "b", Capabilities: []string{"cost", "product"}},
		{Key: "c", Capabilities: []string{"product"}},
		{Key: "d", Capabilities: []string{"made-up"}},
		{Key: "e"},
	}
	groups := GroupBy(apps, testCategories)
	if len(groups) != 3 || groups[0].Category != testCategories[0] || groups[1].Category != testCategories[1] {
		t.Fatalf("got %+v, want the top capabilities in order and Other", groups)
	}
	if keys(groups[0].Apps) != "b,c" {
		t.Fatalf("product has %s: an app goes to the first top capability it reaches, in order", keys(groups[0].Apps))
	}
	if keys(groups[1].Apps) != "a" {
		t.Fatalf("cost has %s: an app on a child goes under its top", keys(groups[1].Apps))
	}
	last := groups[2]
	if last.Category.Key != OtherKey || last.Category.Name != "More apps" || keys(last.Apps) != "d,e" {
		t.Fatalf("other is %+v with %s", last.Category, keys(last.Apps))
	}
	if g := GroupBy(apps[:1], testCategories); len(g) != 2 {
		t.Fatalf("Other should be left out when empty, got %d groups", len(g))
	}
	if g := GroupBy(apps, nil); len(g) != 1 || len(g[0].Apps) != 5 {
		t.Fatalf("with no capabilities every app is in one group, got %+v", g)
	}
}

func TestCategoriesAreATree(t *testing.T) {
	var records []orrery.Record
	if err := json.Unmarshal([]byte(`[
		{"key":"ops-ship","title":"Shipping","fields":{"order":1},"relations":{"parent":[{"key":"ops"}]}},
		{"key":"ops","title":"Operations","fields":{"order":2}},
		{"key":"fin","title":"Finance","fields":{"order":1}},
		{"key":"lost","title":"Lost","relations":{"parent":[{"key":"nowhere"}]}},
		{"key":"other","title":"Reserved"}
	]`), &records); err != nil {
		t.Fatal(err)
	}
	cats := Categories(records)
	roots := Roots(cats)
	if len(roots) != 3 || roots[0].Key != "fin" || roots[1].Key != "ops" || roots[2].Key != "lost" {
		t.Fatalf("roots %+v: by order, an unknown parent puts a category at the top, and other is reserved", roots)
	}
	if RootOf(cats, "ops-ship") != "ops" || RootOf(cats, "made-up") != "" {
		t.Fatal("RootOf follows the parent to the top, and knows only held keys")
	}
	if c := Children(cats, "ops"); len(c) != 1 || c[0].Name != "Shipping" {
		t.Fatalf("children %+v", c)
	}
	loop := []Category{{Key: "a", Parent: "b"}, {Key: "b", Parent: "a"}}
	if RootOf(loop, "a") != "" {
		t.Fatal("a cycle has no top")
	}
}

// ORRERY_URL is the ingest service's base; the reads add /api. A base that
// still ends in /api reads the same, with one deprecation warning.
func TestOrreryTakesTheBase(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/blueprints" && r.Header.Get("Authorization") == "Bearer token" {
			_, _ = io.WriteString(w, blueprintsAnswer("business_capability", "it_application", "sw_workload", "sw_service", "sw_api", "entra_user", "entra_group", "entra_app_role_assignment", "team"))
			return
		}
		if r.URL.Path != "/v1/entities" || r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "unexpected "+r.URL.String(), http.StatusTeapot)
			return
		}
		switch r.URL.Query().Get("blueprint") {
		case "business_capability":
			_, _ = io.WriteString(w, `{"entities":[{"key":"product","title":"Product","fields":{"order":1}}],"total":1}`)
		case "it_application":
			_, _ = io.WriteString(w, `{"entities":[{"blueprint":"it_application","key":"portal","state":"active","fields":{"url":"https://portal.example.com"}}],"total":1}`)
		case "entra_app":
			http.Error(w, `{"error":"Unknown blueprint"}`, http.StatusNotFound)
		case "sw_workload", "sw_service", "sw_api", "entra_user", "entra_group", "entra_app_role_assignment", "team", "it_approval", "myapps_ignored", "myapps_mapping":
			_, _ = io.WriteString(w, `{"entities":[],"total":0}`)
		default:
			http.Error(w, "unexpected "+r.URL.String(), http.StatusTeapot)
		}
	}))
	defer hub.Close()
	for _, base := range []string{hub.URL, hub.URL + "/"} {
		o, err := NewOrrery(base, "token", quiet())
		if err != nil {
			t.Fatalf("%s: %v", base, err)
		}
		d, err := o.Directory(context.Background())
		if err != nil || keys(d.Apps) != "portal" || len(d.Capabilities) != 1 || d.Capabilities[0].Key != "product" {
			t.Fatalf("%s: %v %+v", base, err, d)
		}
	}
	if _, err := NewOrrery("", "token", quiet()); err == nil {
		t.Fatal("an empty ORRERY_URL should be refused")
	}
	if _, err := NewOrrery(hub.URL, "", quiet()); err == nil {
		t.Fatal("an empty ORRERY_TOKEN should be refused")
	}
}

type fakeSource struct {
	apps  []App
	err   error
	calls int
}

func (f *fakeSource) Directory(context.Context) (Directory, error) {
	f.calls++
	return Directory{Capabilities: testCategories, Apps: f.apps}, f.err
}

func TestCacheKeepsLastGoodThroughFailure(t *testing.T) {
	src := &fakeSource{apps: []App{{Key: "a", URL: "https://a.example.com"}}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var outcomes []string
	c := NewTiles(src.Directory, 30*time.Second, quiet(), func(_ context.Context, o string) { outcomes = append(outcomes, o) })
	c.now = func() time.Time { return now }
	ctx := context.Background()

	s := c.Get(ctx)
	if s.Stale || s.Err != nil || len(s.Value.Apps) != 1 || !s.Fetched.Equal(now) {
		t.Fatalf("first read: %+v", s)
	}
	now = now.Add(10 * time.Second)
	c.Get(ctx)
	if src.calls != 1 {
		t.Fatalf("a read inside the TTL should hit the cache, source called %d times", src.calls)
	}

	src.err = errors.New("orrery answered 503")
	now = now.Add(30 * time.Second)
	s = c.Get(ctx)
	if src.calls != 2 {
		t.Fatalf("source called %d times, want 2", src.calls)
	}
	if !s.Stale || s.Err == nil || len(s.Value.Apps) != 1 || s.Value.Apps[0].Key != "a" {
		t.Fatalf("after a failure the last good tiles should stay, got %+v", s)
	}
	if c.Age() != 40*time.Second {
		t.Fatalf("age %s, want 40s from the last good read", c.Age())
	}
	now = now.Add(5 * time.Second)
	c.Get(ctx)
	if src.calls != 2 {
		t.Fatal("a failure should not be retried before the TTL passes")
	}

	src.err = nil
	src.apps = append(src.apps, App{Key: "b"})
	now = now.Add(30 * time.Second)
	s = c.Get(ctx)
	if s.Stale || len(s.Value.Apps) != 2 {
		t.Fatalf("recovery: %+v", s)
	}
	if got := join(outcomes); got != "ok,failed,ok" {
		t.Fatalf("outcomes %s", got)
	}
}

func TestCacheWithNoGoodReadYet(t *testing.T) {
	src := &fakeSource{err: errors.New("down")}
	c := NewTiles(src.Directory, time.Minute, quiet(), nil)
	s := c.Get(context.Background())
	if s.Value.Apps != nil || s.Err == nil || !s.Stale || c.Age() != 0 {
		t.Fatalf("got %+v age %s", s, c.Age())
	}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func keys(apps []App) string {
	var ks []string
	for _, a := range apps {
		ks = append(ks, a.Key)
	}
	return join(ks)
}

func join(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

// The Entra apps are read with the tiles and blended in, with who holds
// them; a failed read of them fails the whole read, so a blip never takes
// tiles away.
func TestOrreryReadsEntraApps(t *testing.T) {
	answers := map[string]string{
		"entra_app":                 `{"entities":[{"blueprint":"entra_app","key":"t/app-pay","title":"Payroll","state":"active","fields":{"app_id":"app-pay","assignment_required":true,"launch_url":"https://pay.example.com"}},{"blueprint":"entra_app","key":"t/app-new","title":"New","state":"active","fields":{"assignment_required":false}}],"total":2}`,
		"it_application":            `{"entities":[{"blueprint":"it_application","key":"payroll","title":"Payroll","state":"active","fields":{"listed":"shown"},"relations":{"sign_in":[{"key":"t/app-pay"}]}}],"total":1}`,
		"entra_user":                `{"entities":[{"key":"t/u1","fields":{"email":"User@Example.com","enabled":true}}],"total":1}`,
		"entra_group":               `{"entities":[{"key":"t/g1","title":"Finance","relations":{"members":[{"key":"t/u1"}]}}],"total":1}`,
		"entra_app_role_assignment": `{"entities":[{"key":"t/a1","relations":{"app":[{"key":"t/app-pay"}],"group":[{"key":"t/g1"}]}}],"total":1}`,
		"it_approval":               `{"entities":[{"key":"r1","fields":{"decision":"pending"}},{"key":"r2","fields":{"decision":"pending"}}],"total":2}`,
	}
	fail := false
	var approvalsFilter string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/blueprints" {
			_, _ = io.WriteString(w, blueprintsAnswer("entra_app", "it_application", "entra_user", "entra_group", "entra_app_role_assignment", "it_approval"))
			return
		}
		bp := r.URL.Query().Get("blueprint")
		if bp == "it_approval" {
			approvalsFilter = r.URL.RawQuery
		}
		if fail && bp == "entra_app" {
			http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
			return
		}
		if a, ok := answers[bp]; ok {
			_, _ = io.WriteString(w, a)
			return
		}
		_, _ = io.WriteString(w, `{"entities":[],"total":0}`)
	}))
	defer hub.Close()
	o, err := NewOrrery(hub.URL, "token", quiet())
	if err != nil {
		t.Fatal(err)
	}
	d, err := o.Directory(context.Background())
	if err != nil || keys(d.Apps) != "payroll" || len(d.Applications) != 1 || len(d.Suggestions) != 1 || d.Suggestions[0].Key != "t/app-new" || d.Waiting != 2 {
		t.Fatalf("%v %+v", err, d)
	}
	if !strings.Contains(approvalsFilter, "decision") || !strings.Contains(approvalsFilter, "pending") {
		t.Fatalf("only pending approvals are counted, asked %q", approvalsFilter)
	}
	who := d.Access.PersonFor("user@example.com")
	if who == nil || !d.Apps[0].SignIn.Admits(who) || d.Apps[0].SignIn.Admits(nil) {
		t.Fatalf("a person in an assigned group may open it, nobody else: %+v", who)
	}
	fail = true
	if _, err := o.Directory(context.Background()); err == nil {
		t.Fatal("a failed read of the Entra apps must fail the read")
	}
}

func TestChildrenSplitAGroupLikeTheWall(t *testing.T) {
	all := []Category{{Key: "platform", Name: "Platform"}, {Key: "ops", Name: "Operate", Parent: "platform", Order: 2}, {Key: "build", Name: "Build", Parent: "platform", Order: 1}, {Key: "alerts", Name: "Alerts", Parent: "ops"}}
	g := Group{Category: all[0], Apps: []App{{Key: "a", Capabilities: []string{"ops"}}, {Key: "b", Capabilities: []string{"build"}}, {Key: "c", Capabilities: []string{"alerts"}}, {Key: "d", Capabilities: []string{"platform"}}}}
	got := SubGroupsOf(g, all)
	if len(got) != 3 || got[0].Category.Key != "" || got[1].Category.Key != "build" || got[2].Category.Key != "ops" || len(got[2].Apps) != 2 {
		t.Fatalf("the group itself first, then children in order, a grandchild under its child: %+v", got)
	}
	if SubGroupsOf(Group{Category: all[0], Apps: []App{{Key: "b", Capabilities: []string{"build"}}}}, all) != nil {
		t.Fatal("one part is no split")
	}
}

// blueprintsAnswer is the hub's answer to /v1/blueprints holding these.
func blueprintsAnswer(ids ...string) string {
	var bs []map[string]any
	for _, id := range ids {
		bs = append(bs, map[string]any{"id": id, "title": id, "fields": map[string]any{}, "relations": map[string]any{}})
	}
	out, _ := json.Marshal(map[string]any{"blueprints": bs})
	return string(out)
}
