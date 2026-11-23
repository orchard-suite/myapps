package catalog

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchard-suite/platformkit/orrery"
)

func records(t *testing.T, raw string) []orrery.Record {
	t.Helper()
	var out []orrery.Record
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// An estate shaped unlike Orrery's own pushers: people with an employment
// field, groups by another name, enterprise apps holding their assignments.
func workMappings() Mappings {
	m := DefaultMappings()
	m[MapPeople] = Mapping{Blueprint: "person", Names: map[string]string{"enabled": "employment", "aliases": "mail_aliases"}, Values: map[string]map[string]string{"enabled": {"false": "departed"}}}
	m[MapGroups] = Mapping{Blueprint: "security_group", Names: map[string]string{"members": "member_people", "dynamic": "is_dynamic"}}
	m[MapEntraApps] = Mapping{Blueprint: "enterprise_app", Names: map[string]string{"assignment_required": "needs_assignment"}}
	m[MapAssignments] = Mapping{Names: map[string]string{"user": "assigned_people", "group": "assigned_groups"}}
	m[MapServices] = Mapping{Blueprint: "sw_service", Names: map[string]string{"origin": "made"}, Values: map[string]map[string]string{"origin": {"built": "in-house", "adopted": "vendor"}}}
	return m
}

func TestMappingsReadAnotherEstate(t *testing.T) {
	m := workMappings()
	people := m.Normalize(MapPeople, records(t, `[
		{"blueprint":"person","key":"a@example.com","fields":{"email":"A@example.com","employment":"active"}},
		{"blueprint":"person","key":"gone@example.com","fields":{"email":"gone@example.com","employment":"departed"}}]`))
	groups := m.Normalize(MapGroups, records(t, `[
		{"blueprint":"security_group","key":"g-fin","title":"Finance","fields":{"is_dynamic":false},"relations":{"member_people":[{"key":"a@example.com"},{"key":"gone@example.com"}]}}]`))
	apps := m.Normalize(MapEntraApps, records(t, `[
		{"blueprint":"enterprise_app","key":"sp-pay","title":"Payroll","state":"active","fields":{"needs_assignment":true},"relations":{"assigned_groups":[{"key":"g-fin"}]}},
		{"blueprint":"enterprise_app","key":"sp-crm","title":"CRM","state":"active","fields":{"needs_assignment":true},"relations":{"assigned_people":[{"key":"gone@example.com"}]}}]`))
	access := AccessOf(people, groups, m.AssignmentsOnApps(apps))
	entra := EntraApps(apps)
	byKey := map[string]EntraApp{}
	for _, e := range entra {
		byKey[e.Key] = e
	}
	if !byKey["sp-pay"].Required || byKey["sp-pay"].Blueprint != "enterprise_app" {
		t.Fatalf("a renamed field reads under My Apps' name, and the app keeps its blueprint: %+v", byKey["sp-pay"])
	}
	a := access.PersonFor("a@example.com")
	if pay := access.SignInOfApps([]string{"sp-pay"}, byKey); a == nil || !pay.Admits(a) {
		t.Fatalf("a member of a group assigned on the app may open it: %+v %+v", a, pay)
	}
	if access.PersonFor("gone@example.com") != nil {
		t.Fatal("someone whose employment is departed is not a person who may sign in")
	}
	if g, ok := access.EntraGroup("g-fin"); !ok || g.Members != 2 || g.Dynamic {
		t.Fatalf("the group reads its members and dynamic flag: %+v", g)
	}
	services := m.Normalize(MapServices, records(t, `[{"key":"a","fields":{"made":"in-house"}},{"key":"b","fields":{"made":"vendor"}},{"key":"c","fields":{"made":"other"}}]`))
	for i, want := range []string{"built", "adopted", "other"} {
		if got := services[i].String("origin"); got != want {
			t.Fatalf("service %d reads origin %q, want %q", i, got, want)
		}
	}
}

// The front door may send a mail alias rather than the main email: the
// person is still found, and still admitted where they are assigned. An
// alias never takes someone else's main email.
func TestPeopleAreFoundByTheirAliases(t *testing.T) {
	m := workMappings()
	people := m.Normalize(MapPeople, records(t, `[
		{"blueprint":"person","key":"a@example.com","fields":{"email":"a@example.com","employment":"active","mail_aliases":["A@alias.example.com","b@example.com"]}},
		{"blueprint":"person","key":"b@example.com","fields":{"email":"b@example.com","employment":"active"}},
		{"blueprint":"person","key":"gone@example.com","fields":{"email":"gone@example.com","employment":"departed","mail_aliases":["gone@alias.example.com"]}}]`))
	apps := m.Normalize(MapEntraApps, records(t, `[
		{"blueprint":"enterprise_app","key":"sp-tally","title":"Tally","state":"active","fields":{"needs_assignment":true},"relations":{"assigned_people":[{"key":"a@example.com"}]}}]`))
	access := AccessOf(people, nil, m.AssignmentsOnApps(apps))
	byKey := map[string]EntraApp{}
	for _, e := range EntraApps(apps) {
		byKey[e.Key] = e
	}
	a := access.PersonFor(" a@Alias.Example.com ")
	if a == nil || a.ID != "a@example.com" || !access.SignInOfApps([]string{"sp-tally"}, byKey).Admits(a) {
		t.Fatalf("a person signed in by an alias is found and admitted where assigned: %+v", a)
	}
	if b := access.PersonFor("b@example.com"); b == nil || b.ID != "b@example.com" {
		t.Fatalf("an alias never takes another person's main email: %+v", b)
	}
	if access.PersonFor("gone@alias.example.com") != nil {
		t.Fatal("someone who has left is not found by an alias either")
	}
	if got := access.Emails(); strings.Join(got, ",") != "a@example.com,b@example.com" {
		t.Fatalf("an admin views as people by their main emails only: %v", got)
	}
}

func TestDefaultMappingsLeaveOrrerysOwnAlone(t *testing.T) {
	m := DefaultMappings()
	in := records(t, `[{"key":"t/u1","fields":{"email":"u@example.com","enabled":false}}]`)
	got := m.Normalize(MapPeople, in)
	if v, ok := got[0].Bool("enabled"); !ok || v {
		t.Fatalf("Orrery's own names pass through: %+v", got[0].Fields)
	}
}

func TestMappingsKeepOnlyWhatTheHubHolds(t *testing.T) {
	m := DefaultMappings().Within([]Blueprint{{ID: "entra_user"}, {ID: "team"}})
	if m.Blueprint(MapPeople) != "entra_user" || m.Blueprint(MapTeams) != "team" || m.Blueprint(MapEntraApps) != "" || m.Blueprint(MapWorkloads) != "" {
		t.Fatalf("a blueprint the hub lacks is not read: %+v", m)
	}
	var app orrery.Blueprint
	for _, b := range CurationBlueprints(m) {
		if b["id"] == ApplicationBlueprint {
			app = b
		}
	}
	rels := app["relations"].(map[string]any)
	if _, ok := rels["sign_in"]; ok {
		t.Fatal("no relation is offered toward a blueprint the hub lacks")
	}
	if team, ok := rels["team"].(map[string]any); !ok || team["target"] != "team" {
		t.Fatalf("a held one is offered at its blueprint: %v", rels["team"])
	}
	if _, ok := rels[CapabilityRelation]; !ok {
		t.Fatal("the capabilities are always offered")
	}
	ent := AppDetails{Name: "Jira", Team: "it", SignIn: []string{"x"}, Into: m}.Entity("jira")
	if _, ok := ent.Relations["sign_in"]; ok {
		t.Fatal("an application writes no relation the hub has no blueprint for")
	}
	if _, ok := ent.Relations["team"]; !ok {
		t.Fatal("and writes the ones it has")
	}
}

func TestMappingsAreSavedAndReadBack(t *testing.T) {
	w := workMappings()
	e := w[MapPeople].Entity(MapPeople, "user@example.com")
	raw, _ := json.Marshal(map[string]any{"key": e.Key, "fields": e.Fields})
	var r orrery.Record
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	got := MappingsOf([]orrery.Record{r})
	if got[MapPeople].Blueprint != "person" || got[MapPeople].Name("enabled") != "employment" || got[MapPeople].Value("enabled", "false") != "departed" {
		t.Fatalf("a saved mapping reads back: %+v", got[MapPeople])
	}
	if got[MapTeams].Blueprint != "team" {
		t.Fatal("a kind not saved keeps its default")
	}
}

func TestMappingCheck(t *testing.T) {
	hub := []Blueprint{
		{ID: "person", Fields: map[string]BlueprintBit{"email": {}, "employment": {}}},
		{ID: "security_group", Fields: map[string]BlueprintBit{"is_dynamic": {}}, Relations: map[string]BlueprintBit{"member_people": {Target: "person"}, "owners": {Target: "team"}}},
		{ID: "enterprise_app", Fields: map[string]BlueprintBit{}, Relations: map[string]BlueprintBit{"assigned_people": {Target: "person"}, "assigned_groups": {Target: "security_group"}}},
	}
	m := workMappings()
	cases := []struct {
		name string
		kind MappingKind
		src  Mapping
		want string
	}{
		{"fits", MapGroups, m[MapGroups], ""},
		{"no such blueprint", MapGroups, Mapping{Blueprint: "nope"}, "no blueprint"},
		{"no such field", MapGroups, Mapping{Blueprint: "security_group", Names: map[string]string{"members": "member_people"}}, "no field dynamic"},
		{"a relation to the wrong kind", MapGroups, Mapping{Blueprint: "security_group", Names: map[string]string{"members": "owners", "dynamic": "is_dynamic"}}, "points at team"},
		{"a part left out", MapGroups, Mapping{Blueprint: "security_group", Names: map[string]string{"members": "member_people", "dynamic": ""}}, ""},
		{"not read", MapGroups, Mapping{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := m.Check(tc.kind, tc.src, hub)
			if (tc.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
	if err := m.CheckOnApps(m[MapAssignments], hub); err != nil {
		t.Fatalf("assignments on the apps fit: %v", err)
	}
	if err := m.CheckOnApps(Mapping{Names: map[string]string{"user": "assigned_groups"}}, hub); err == nil {
		t.Fatal("people assigned must point at the people")
	}
}

func TestAnAddressMayBeAListOfHosts(t *testing.T) {
	m := DefaultMappings()
	m[MapWorkloads] = Mapping{Blueprint: "k8s_ingress", Names: map[string]string{"url": "hosts"}}
	got := m.Normalize(MapWorkloads, records(t, `[
		{"key":"a","fields":{"hosts":["gitlab.example.com","gitlab.gitlab.example.com"]}},
		{"key":"b","fields":{"hosts":[]}},
		{"key":"c","fields":{"hosts":"https://c.example.com"}}]`))
	for i, want := range []string{"https://gitlab.example.com", "", "https://c.example.com"} {
		if g := got[i].String("url"); g != want {
			t.Fatalf("workload %d reads %q, want %q", i, g, want)
		}
	}
}

func TestABlueprintsIconIsNoThingsLogo(t *testing.T) {
	m := DefaultMappings().Within([]Blueprint{{ID: "entra_app", Icon: "azure_enterprise_applications"}})
	got := m.Normalize(MapEntraApps, records(t, `[{"key":"a","icon":"azure_enterprise_applications"},{"key":"b","icon":"argocd"}]`))
	if got[0].Icon != "" || got[1].Icon != "argocd" {
		t.Fatalf("the blueprint's icon reads as none, a real logo stays: %q %q", got[0].Icon, got[1].Icon)
	}
	if !GenericIcon("azure_enterprise_applications", []Blueprint{{ID: "entra_app", Icon: "azure_enterprise_applications"}}) || GenericIcon("argocd", nil) {
		t.Fatal("GenericIcon knows a blueprint's icon")
	}
}

func TestAnApplicationWithOnlyItsBlueprintsIconHasNoLogo(t *testing.T) {
	in := records(t, `[
		{"blueprint":"it_application","key":"zoom","title":"Zoom","icon":"myapps","fields":{"url":"https://zoom.us"},"relations":{"sign_in":[{"key":"z"}]}},
		{"blueprint":"it_application","key":"gitlab","title":"GitLab","icon":"gitlab","fields":{"url":"https://gitlab.example.com"}}]`)
	got := WithoutBlueprintIcons(in, []Blueprint{{ID: "it_application", Icon: "myapps"}})
	if got[0].Icon != "" || got[1].Icon != "gitlab" || in[0].Icon != "myapps" {
		t.Fatalf("the blueprint's icon reads as none, a real logo stays, the input is kept: %q %q", got[0].Icon, got[1].Icon)
	}
	entra := []EntraApp{{Key: "z", Icon: "entra_zoom"}}
	apps := WithLogos(Applications(got), entra)
	icons := map[string]string{}
	for _, tile := range Tiles(apps, entra, Access{}) {
		icons[tile.Key] = tile.Icon
	}
	if icons["zoom"] != "entra_zoom" || icons["gitlab"] != "gitlab" {
		t.Fatalf("a tile with no logo of its own takes its Entra app's: %v", icons)
	}
	l := ShapeLandscape(Lens{Blueprint: CapabilityBlueprint}, nil, apps, false, nil, nil, func(string) []string { return nil }, "")
	for _, c := range l.Cards {
		icons[c.Key] = c.Icon
	}
	if icons["zoom"] != "entra_zoom" || icons["gitlab"] != "gitlab" || len(l.Cards) != 2 {
		t.Fatalf("a landscape card draws the same logo as its tile: %v", icons)
	}
}

func TestLogoOf(t *testing.T) {
	entra := []EntraApp{{Key: "a", Icon: "entra_a"}, {Key: "b", Icon: "entra_b"}}
	for _, c := range []struct {
		icon   string
		signIn []string
		want   string
	}{
		{"own", []string{"a"}, "own"},
		{"", []string{"b", "a"}, "entra_b"},
		{"", []string{"gone"}, ""},
		{"", nil, ""},
	} {
		if got := LogoOf(c.icon, c.signIn, entra); got != c.want {
			t.Errorf("LogoOf(%q, %v) = %q, want %q", c.icon, c.signIn, got, c.want)
		}
	}
}
