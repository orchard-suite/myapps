package catalog

import (
	"cmp"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/orchard-suite/platformkit/orrery"
)

// Business capabilities are the core classification every person sees:
// what part of the organization an app serves. Products link to them
// through CapabilityRelation.
const (
	CapabilityBlueprint = "business_capability"
	CapabilityRelation  = "business_capabilities"
)

// LensBlueprint is My Apps' own record of the classifications it analyses
// coverage across. Each entity's key is a classification's blueprint id.
const LensBlueprint = "myapps_lens"

// Lens is one classification My Apps analyses: business capabilities, a
// department's pillars, or any scheme with categories of its own.
type Lens struct {
	// Blueprint is the classification's blueprint id.
	Blueprint string `json:"blueprint"`
	Title     string `json:"title"`
	Order     int    `json:"order"`
}

// Blueprint is what My Apps needs of a blueprint in the hub: its title,
// whether it can be a classification, and the relations it declares.
type Blueprint struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Group string `json:"group,omitempty"`
	// Icon is the blueprint's own icon, which the hub also gives each of
	// its entities that has none.
	Icon      string                  `json:"icon,omitempty"`
	Fields    map[string]BlueprintBit `json:"fields"`
	Relations map[string]BlueprintBit `json:"relations"`
}

// BlueprintBit is a field's type or a relation's target.
type BlueprintBit struct {
	Type   string `json:"type,omitempty"`
	Target string `json:"target,omitempty"`
	Many   bool   `json:"many,omitempty"`
}

// Classification says whether the blueprint can be a lens: its entities
// carry an order, as the standard's classifications do.
func (b Blueprint) Classification() bool {
	_, ok := b.Fields["order"]
	return ok && b.ID != LensBlueprint
}

// RelationsTo are the relations of the blueprint from that point at the
// blueprint to, sorted.
func RelationsTo(blueprints []Blueprint, from, to string) []string {
	var out []string
	for _, b := range blueprints {
		if b.ID != from {
			continue
		}
		for name, r := range b.Relations {
			if r.Target == to && name != "parent" {
				out = append(out, name)
			}
		}
	}
	slices.Sort(out)
	return out
}

// TitleOf is a blueprint's title, else its id.
func TitleOf(blueprints []Blueprint, id string) string {
	for _, b := range blueprints {
		if b.ID == id {
			return cmp.Or(b.Title, id)
		}
	}
	return id
}

// Lenses are the classifications to analyse: business capabilities first,
// always, then the myapps_lens records by order, each one only while its
// blueprint is in the hub.
func Lenses(records []orrery.Record, blueprints []Blueprint) []Lens {
	out := []Lens{{Blueprint: CapabilityBlueprint, Title: cmp.Or(TitleOf(blueprints, CapabilityBlueprint), "Business capability"), Order: 0}}
	type ranked struct {
		l     Lens
		order float64
	}
	var rs []ranked
	for _, r := range records {
		if r.Hidden || r.Key == CapabilityBlueprint || !slices.ContainsFunc(blueprints, func(b Blueprint) bool { return b.ID == r.Key }) {
			continue
		}
		order, set := r.Number("order")
		if !set {
			order = math.Inf(1)
		}
		rs = append(rs, ranked{Lens{Blueprint: r.Key, Title: TitleOf(blueprints, r.Key)}, order})
	}
	slices.SortStableFunc(rs, func(x, y ranked) int {
		return cmp.Or(cmp.Compare(x.order, y.order), cmp.Compare(x.l.Title, y.l.Title))
	})
	for i, r := range rs {
		r.l.Order = i + 1
		out = append(out, r.l)
	}
	return out
}

// LensEntity is a lens as My Apps writes it.
func LensEntity(blueprint string, order int) orrery.Entity {
	return orrery.Entity{Blueprint: LensBlueprint, Key: blueprint, Title: blueprint, Fields: map[string]any{"order": order}}
}

// CapabilityEntity is a business capability as My Apps writes it. An
// empty parent puts it at the top.
func CapabilityEntity(c Category) orrery.Entity {
	return orrery.Entity{
		Blueprint: CapabilityBlueprint,
		Key:       c.Key,
		Title:     c.Name,
		Fields:    map[string]any{"order": c.Order, "description": orNil(c.Description)},
		Relations: map[string]any{"parent": orNil(c.Parent)},
	}
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

// CapabilityKey is a new capability's key, from its name and its parent's
// key, unique among those taken. It never changes after: a rename keeps it.
func CapabilityKey(name, parent string, taken []Category) string {
	base := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if base == "" {
		base = "capability"
	}
	if parent != "" {
		base = parent + "-" + base
	}
	key := base
	for n := 2; key == OtherKey || index(taken, key) >= 0; n++ {
		key = base + "-" + strconv.Itoa(n)
	}
	return key
}

// Coverage is one read of everything the lenses draw from: the hub's
// blueprints, the lenses, each lens's category entities, and every
// service and API, with the sources for their integration.
type Coverage struct {
	Blueprints   []Blueprint
	Lenses       []Lens
	Categories   map[string][]orrery.Record
	Services     []orrery.Record
	APIs         []orrery.Record
	Assessments  []Assessment
	Sources      []Source
	SourcesKnown bool
}

// Lens is the lens on this blueprint, or the first lens when it is not
// one of them.
func (c Coverage) Lens(blueprint string) Lens {
	for _, l := range c.Lenses {
		if l.Blueprint == blueprint {
			return l
		}
	}
	if len(c.Lenses) > 0 {
		return c.Lenses[0]
	}
	return Lens{Blueprint: CapabilityBlueprint, Title: "Business capability"}
}

// Landscape shapes one lens with the applications and, with services set,
// the services and outside APIs, and adds the integration. hub is Orrery's
// public address.
func (c Coverage) Landscape(lens Lens, apps []Application, withServices bool, hub string) Landscape {
	relations := func(blueprint string) []string { return RelationsTo(c.Blueprints, blueprint, lens.Blueprint) }
	l := ShapeLandscape(lens, c.Categories[lens.Blueprint], apps, withServices, c.Services, c.APIs, relations, hub)
	return WithSources(l, c.Sources, c.SourcesKnown)
}

// IconNames is every icon a lens may draw from the services and APIs; the
// applications' icons come with the tiles.
func (c Coverage) IconNames() []string {
	out := make([]string, 0, len(c.Services)+len(c.APIs))
	for _, r := range c.Services {
		out = append(out, r.Icon)
	}
	for _, r := range c.APIs {
		out = append(out, r.Icon)
	}
	return out
}

// Tally is one count in a breakdown.
type Tally struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Breakdown is the shown apps of a lens counted other ways: by owning
// team, by who may open them, and the gaps: categories nothing is placed
// on, and shown apps the lens does not place.
type Breakdown struct {
	ByTeam   []Tally
	ByAccess []Tally
	Empty    []Category
	Unplaced []string
}

// NoTeams is whether no shown app has an owning team yet, so the team
// count says nothing but how many apps there are.
func (b Breakdown) NoTeams() bool {
	return len(b.ByTeam) == 1 && b.ByTeam[0].Name == NoTeamName
}

// Access words, in the order a breakdown lists them.
const (
	OpenToAll  = "Open to everyone"
	ByRoles    = "Front-door roles"
	InEntra    = "Assigned in Entra"
	NoTeamName = "No owning team"
)

// BreakdownOf counts the shown apps for one lens's landscape.
func BreakdownOf(l Landscape, apps []App) Breakdown {
	var b Breakdown
	placed := map[string]bool{}
	for _, c := range l.Cards {
		if len(c.Categories) > 0 {
			placed[c.App] = true
		}
	}
	teams, access := map[string]int{}, map[string]int{}
	for _, a := range apps {
		teams[cmp.Or(a.Team, NoTeamName)]++
		switch {
		case a.SignIn != nil && !a.SignIn.Everyone:
			access[InEntra]++
		case len(a.Audience) > 0:
			access[ByRoles]++
		default:
			access[OpenToAll]++
		}
		if !placed[a.Ref] {
			b.Unplaced = append(b.Unplaced, a.Title)
		}
	}
	b.ByTeam = tallies(teams)
	for _, name := range []string{OpenToAll, ByRoles, InEntra} {
		if n := access[name]; n > 0 {
			b.ByAccess = append(b.ByAccess, Tally{name, n})
		}
	}
	counts := map[string]int{}
	for _, c := range l.Cards {
		for _, k := range c.Categories {
			counts[k]++
		}
	}
	for _, c := range l.Categories {
		if counts[c.Key] == 0 {
			b.Empty = append(b.Empty, c)
		}
	}
	slices.Sort(b.Unplaced)
	return b
}

// tallies are the counts, most first, then by name.
func tallies(m map[string]int) []Tally {
	out := make([]Tally, 0, len(m))
	for name, n := range m {
		out = append(out, Tally{name, n})
	}
	slices.SortFunc(out, func(x, y Tally) int { return cmp.Or(cmp.Compare(y.Count, x.Count), cmp.Compare(x.Name, y.Name)) })
	return out
}
