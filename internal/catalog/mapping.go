package catalog

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/orchard-suite/platformkit/orrery"
)

// Every estate models what My Apps reads in its own way: one hub calls a
// person entra_user with an enabled flag, another person with an
// employment field. A Mapping says, for one kind of thing My Apps reads,
// which of the hub's blueprints holds it and which of that blueprint's
// fields and relations carry each part. Normalize reads a source's records
// into the names the rest of My Apps uses, so nothing past it knows the
// estate. No estate is built in beyond the defaults, which an admin
// changes on the Mappings page.

// MappingKind is one kind of thing My Apps reads from the hub.
type MappingKind string

const (
	MapPeople      MappingKind = "people"
	MapGroups      MappingKind = "groups"
	MapEntraApps   MappingKind = "apps"
	MapAssignments MappingKind = "assignments"
	MapWorkloads   MappingKind = "workloads"
	MapServices    MappingKind = "services"
	MapAPIs        MappingKind = "apis"
	MapTeams       MappingKind = "teams"
)

// Part is one part of a source: a field, or a relation to another source.
type Part struct {
	// Name is what My Apps calls it.
	Name string `json:"name"`
	// Title and About say what it is, on the Mappings page.
	Title string `json:"title"`
	About string `json:"about,omitempty"`
	// Relation is true for a relation; To is the source it points at.
	Relation bool        `json:"relation,omitempty"`
	To       MappingKind `json:"to,omitempty"`
	// Values are the values My Apps acts on, for a field the hub may word
	// in its own way, such as false for a person who has left.
	Values []string `json:"values,omitempty"`
	// Address is true for a field that holds where something is reached.
	// The hub may hold a list of host names, such as an ingress's hosts:
	// the first is read, as https.
	Address bool `json:"address,omitempty"`
}

// MappingSpec is one kind of thing My Apps reads and its parts.
type MappingSpec struct {
	Kind  MappingKind `json:"kind"`
	Title string      `json:"title"`
	About string      `json:"about"`
	Parts []Part      `json:"parts"`
	// Optional is true when the source may be left unmapped and still
	// mean something: assignments then live on the Entra app.
	Optional bool `json:"optional,omitempty"`
}

// MappingSpecs are every kind of thing My Apps reads, in the order the
// Mappings page shows them. A kind's parts point only at kinds before it.
var MappingSpecs = []MappingSpec{
	{Kind: MapPeople, Title: "People", About: "Everyone who may sign in. My Apps finds the person signed in by any of their emails.", Parts: []Part{
		{Name: "email", Title: "Email", About: "The person's main address."},
		{Name: "aliases", Title: "Other emails", About: "A field with the person's other addresses, such as mail aliases. The front door may send any of them."},
		{Name: "enabled", Title: "Still here", About: "A field that says someone has left. Its value when they have.", Values: []string{"false"}},
	}},
	{Kind: MapGroups, Title: "Groups", About: "Entra groups. Their direct members get what the group is given.", Parts: []Part{
		{Name: "members", Title: "Members", About: "The people directly in the group.", Relation: true, To: MapPeople},
		{Name: "dynamic", Title: "Dynamic", About: "True when Entra fills the group by a rule, so no request can add a person."},
	}},
	{Kind: MapEntraApps, Title: "Entra apps", About: "The enterprise apps people sign in through: whether they need an assignment, their roles, and where they open.", Parts: []Part{
		{Name: "app_id", Title: "Application id"},
		{Name: "assignment_required", Title: "Assignment required", About: "True when only assigned people may sign in."},
		{Name: "launch_url", Title: "Launch address"},
		{Name: "portal", Title: "Admin center page"},
		{Name: "sign_in", Title: "Sign-in kind", About: "oidc, saml, or password."},
		{Name: "publisher", Title: "Publisher"},
		{Name: "roles", Title: "Role values"},
		{Name: "role_titles", Title: "Role names"},
	}},
	{Kind: MapAssignments, Title: "Assignments", Optional: true, About: "Who is assigned to each Entra app: their own blueprint with the app, the person, and the group, or no blueprint, with the people and groups on the Entra app.", Parts: []Part{
		{Name: "app", Title: "Entra app", About: "With a blueprint of their own: the app assigned.", Relation: true, To: MapEntraApps},
		{Name: "user", Title: "Person", About: "The person assigned.", Relation: true, To: MapPeople},
		{Name: "group", Title: "Group", About: "The group assigned.", Relation: true, To: MapGroups},
	}},
	{Kind: MapWorkloads, Title: "Workloads", About: "What runs in a cluster. One with an address is suggested as an application.", Parts: []Part{
		{Name: "url", Title: "Address", About: "An address, or host names, such as an ingress's hosts.", Address: true},
	}},
	{Kind: MapServices, Title: "Services", About: "What an application runs. Built here or adopted decides how the Landscape and Analytics draw it.", Parts: []Part{
		{Name: "origin", Title: "Origin", About: "A field that says whether the organization built the service or runs someone else's.", Values: []string{"built", "adopted"}},
		{Name: "repository_url", Title: "Repository"},
		{Name: "url", Title: "Address"},
	}},
	{Kind: MapAPIs, Title: "APIs", About: "APIs. An outside one may be a SaaS application.", Parts: []Part{
		{Name: "external", Title: "Outside", About: "True for an API someone else runs."},
	}},
	{Kind: MapTeams, Title: "Teams", About: "Who owns an application."},
}

// SpecOf is a kind's spec.
func SpecOf(kind MappingKind) (MappingSpec, bool) {
	i := slices.IndexFunc(MappingSpecs, func(s MappingSpec) bool { return s.Kind == kind })
	if i < 0 {
		return MappingSpec{}, false
	}
	return MappingSpecs[i], true
}

// Mapping is where the hub holds one kind of thing: its blueprint, the name
// the hub gives each part, and how it words each value My Apps acts on. A
// part not named keeps My Apps' own name; an empty name leaves it out.
type Mapping struct {
	Blueprint string                       `json:"blueprint"`
	Names     map[string]string            `json:"names,omitempty"`
	Values    map[string]map[string]string `json:"values,omitempty"`
	// generic is the blueprint's own icon, which is no thing's logo.
	generic string
}

// Name is the hub's name for a part.
func (s Mapping) Name(part string) string {
	if n, ok := s.Names[part]; ok {
		return n
	}
	return part
}

// Value is the hub's wording of one of a part's values.
func (s Mapping) Value(part, value string) string {
	if v, ok := s.Values[part][value]; ok {
		return v
	}
	return value
}

// Mappings are where the hub holds everything My Apps reads, by kind.
type Mappings map[MappingKind]Mapping

// DefaultMappings are the names Orrery's own pushers use.
func DefaultMappings() Mappings {
	return Mappings{
		MapPeople:      {Blueprint: "entra_user"},
		MapGroups:      {Blueprint: "entra_group"},
		MapEntraApps:   {Blueprint: EntraBlueprint},
		MapAssignments: {Blueprint: "entra_app_role_assignment"},
		MapWorkloads:   {Blueprint: WorkloadBlueprint},
		MapServices:    {Blueprint: ServiceBlueprint},
		MapAPIs:        {Blueprint: APIBlueprint},
		MapTeams:       {Blueprint: "team"},
	}
}

// Blueprint is the blueprint a kind is read from; empty when it is not
// mapped or the hub lacks it.
func (s Mappings) Blueprint(kind MappingKind) string { return s[kind].Blueprint }

// Ref is a thing's id in the hub, blueprint:key.
func (s Mappings) Ref(kind MappingKind, key string) string { return s.Blueprint(kind) + ":" + key }

// MappingBlueprint is My Apps' record of where it reads one kind of thing.
const MappingBlueprint = "myapps_mapping"

// MappingsOf reads My Apps' saved sources over the defaults. A kind saved
// with no blueprint is unmapped.
func MappingsOf(records []orrery.Record) Mappings {
	out := DefaultMappings()
	for _, r := range records {
		kind := MappingKind(r.Key)
		if _, known := SpecOf(kind); !known || r.Hidden {
			continue
		}
		src := Mapping{Blueprint: strings.TrimSpace(r.String("blueprint"))}
		if raw := r.Fields["names"]; len(raw) > 0 {
			_ = json.Unmarshal(raw, &src.Names)
		}
		if raw := r.Fields["values"]; len(raw) > 0 {
			_ = json.Unmarshal(raw, &src.Values)
		}
		out[kind] = src
	}
	return out
}

// Entity is a source as My Apps writes it.
func (s Mapping) Entity(kind MappingKind, by string) orrery.Entity {
	return orrery.Entity{
		Blueprint: MappingBlueprint,
		Key:       string(kind),
		Title:     string(kind),
		Fields: map[string]any{
			"blueprint":  s.Blueprint,
			"names":      nonNilMap(s.Names),
			"values":     nonNilMap(s.Values),
			"updated_by": by,
		},
	}
}

func nonNilMap[V any](m map[string]V) map[string]V {
	if m == nil {
		return map[string]V{}
	}
	return m
}

// Within keeps only the kinds whose blueprint the hub holds, so nothing is
// read from, offered toward, or written to a blueprint it lacks.
func (s Mappings) Within(blueprints []Blueprint) Mappings {
	held := make(map[string]bool, len(blueprints))
	for _, b := range blueprints {
		held[b.ID] = true
	}
	icons := make(map[string]string, len(blueprints))
	for _, b := range blueprints {
		icons[b.ID] = b.Icon
	}
	out := make(Mappings, len(s))
	for kind, src := range s {
		if !held[src.Blueprint] {
			src.Blueprint = ""
		}
		src.generic = icons[src.Blueprint]
		out[kind] = src
	}
	return out
}

// WithoutBlueprintIcons are the records with an icon that is only their
// blueprint's own taken away, so a record with no logo of its own reads as
// having none and falls back to what it is linked to, or a monogram.
func WithoutBlueprintIcons(records []orrery.Record, blueprints []Blueprint) []orrery.Record {
	icons := make(map[string]string, len(blueprints))
	for _, b := range blueprints {
		icons[b.ID] = b.Icon
	}
	out := make([]orrery.Record, len(records))
	for i, r := range records {
		if generic := icons[r.Blueprint]; generic != "" && r.Icon == generic {
			r.Icon = ""
		}
		out[i] = r
	}
	return out
}

// GenericIcon says whether an icon is only some blueprint's own, which the
// hub gives an entity that has none: no thing's logo.
func GenericIcon(icon string, blueprints []Blueprint) bool {
	return icon != "" && slices.ContainsFunc(blueprints, func(b Blueprint) bool { return b.Icon == icon })
}

// Normalize reads a kind's records into My Apps' own names: each part the
// hub names otherwise is copied to My Apps' name, and a value the hub
// words otherwise is written in My Apps' words. Everything else on the
// record stays, so a part both name alike needs no mapping.
func (s Mappings) Normalize(kind MappingKind, records []orrery.Record) []orrery.Record {
	spec, _ := SpecOf(kind)
	src := s[kind]
	out := make([]orrery.Record, 0, len(records))
	for _, r := range records {
		n := r
		if src.generic != "" && n.Icon == src.generic {
			n.Icon = ""
		}
		n.Fields = maps.Clone(r.Fields)
		n.Relations = maps.Clone(r.Relations)
		for _, p := range spec.Parts {
			hub := src.Name(p.Name)
			if p.Relation {
				if hub == "" {
					delete(n.Relations, p.Name)
				} else if hub != p.Name {
					n.Relations[p.Name] = r.Relations[hub]
				}
				continue
			}
			raw, ok := r.Fields[hub]
			if hub == "" || !ok {
				delete(n.Fields, p.Name)
				continue
			}
			if len(p.Values) > 0 {
				raw = canonicalValue(raw, p, src)
			}
			if p.Address {
				raw = addressOf(raw)
			}
			n.Fields[p.Name] = raw
		}
		if n.Fields == nil {
			n.Fields = map[string]json.RawMessage{}
		}
		if n.Relations == nil {
			n.Relations = map[string][]orrery.Ref{}
		}
		out = append(out, n)
	}
	return out
}

// canonicalValue is a value in My Apps' words: the value whose hub wording
// matches, as the field's own JSON type would carry it; a value matching
// none passes as it came.
func canonicalValue(raw json.RawMessage, p Part, src Mapping) json.RawMessage {
	got := scalarText(raw)
	for _, v := range p.Values {
		if got != src.Value(p.Name, v) {
			continue
		}
		if b, err := strconv.ParseBool(v); err == nil {
			return json.RawMessage(strconv.FormatBool(b))
		}
		enc, _ := json.Marshal(v)
		return enc
	}
	return raw
}

// addressOf is an address as a URL: the value, or a list's first entry,
// with https added to a bare host name.
func addressOf(raw json.RawMessage) json.RawMessage {
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		if len(list) == 0 {
			return json.RawMessage(`""`)
		}
		raw, _ = json.Marshal(list[0])
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return raw
	}
	if s = strings.TrimSpace(s); s != "" && !strings.Contains(s, "://") {
		s = "https://" + s
	}
	out, _ := json.Marshal(s)
	return out
}

// scalarText is a JSON string, boolean, or number as plain text.
func scalarText(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// AssignmentsOnApps reads assignments kept on the Entra apps, as the
// assignments source with no blueprint means: one assignment per person
// and per group each app names.
func (s Mappings) AssignmentsOnApps(apps []orrery.Record) []orrery.Record {
	src := s[MapAssignments]
	appBP := s.Blueprint(MapEntraApps)
	var out []orrery.Record
	for _, a := range apps {
		if a.Hidden {
			continue
		}
		app := orrery.Ref{Blueprint: appBP, Key: a.Key}
		for _, part := range []string{"user", "group"} {
			name := src.Name(part)
			if name == "" {
				continue
			}
			for _, ref := range a.Relations[name] {
				out = append(out, orrery.Record{
					Key:       a.Key + "/" + part + "/" + ref.Key,
					Relations: map[string][]orrery.Ref{"app": {app}, part: {ref}},
				})
			}
		}
	}
	return out
}

// Check says what is wrong with a source as an admin would save it,
// against the hub's blueprints and the other sources, or nothing.
func (s Mappings) Check(kind MappingKind, src Mapping, blueprints []Blueprint) error {
	spec, ok := SpecOf(kind)
	if !ok {
		return fmt.Errorf("there is no source called %s", kind)
	}
	if src.Blueprint == "" {
		return nil
	}
	i := slices.IndexFunc(blueprints, func(b Blueprint) bool { return b.ID == src.Blueprint })
	if i < 0 {
		return fmt.Errorf("the hub has no blueprint %s", src.Blueprint)
	}
	bp := blueprints[i]
	onApps := kind == MapAssignments && src.Blueprint == ""
	for _, p := range spec.Parts {
		name := src.Name(p.Name)
		if name == "" {
			continue
		}
		if !p.Relation {
			if _, ok := bp.Fields[name]; !ok {
				return fmt.Errorf("%s has no field %s for %s", bp.ID, name, p.Title)
			}
			continue
		}
		if onApps {
			continue
		}
		rel, ok := bp.Relations[name]
		if !ok {
			return fmt.Errorf("%s has no relation %s for %s", bp.ID, name, p.Title)
		}
		if want := s.Blueprint(p.To); want != "" && rel.Target != want {
			return fmt.Errorf("%s on %s points at %s, not %s", name, bp.ID, rel.Target, want)
		}
	}
	return nil
}

// CheckOnApps says what is wrong with assignments kept on the Entra apps:
// each relation named must be one of the app blueprint's, pointing at the
// people or the groups.
func (s Mappings) CheckOnApps(src Mapping, blueprints []Blueprint) error {
	appBP := s.Blueprint(MapEntraApps)
	i := slices.IndexFunc(blueprints, func(b Blueprint) bool { return b.ID == appBP })
	if i < 0 {
		return fmt.Errorf("map the Entra apps before assignments kept on them")
	}
	for _, part := range []struct {
		name string
		to   MappingKind
	}{{"user", MapPeople}, {"group", MapGroups}} {
		name := src.Name(part.name)
		if name == "" {
			continue
		}
		rel, ok := blueprints[i].Relations[name]
		if !ok {
			return fmt.Errorf("%s has no relation %s", appBP, name)
		}
		if want := s.Blueprint(part.to); want != "" && rel.Target != want {
			return fmt.Errorf("%s on %s points at %s, not %s", name, appBP, rel.Target, want)
		}
	}
	return nil
}
