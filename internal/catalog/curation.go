package catalog

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/orchard-suite/platformkit/orrery"
)

// AssessmentBlueprint is My Apps' record of a coverage assessment: how
// strong one category of one lens is, what covers it, and what is missing.
// Each save is a new record; the newest per category is the current one.
const AssessmentBlueprint = "myapps_assessment"

// Assessment is one coverage assessment.
type Assessment struct {
	Lens     string    `json:"lens"`
	Category string    `json:"category"`
	Rating   int       `json:"rating"`
	Covers   string    `json:"covers,omitempty"`
	Missing  string    `json:"missing,omitempty"`
	By       string    `json:"by,omitempty"`
	At       time.Time `json:"at"`
}

// Key is the record's key: lens, category, and the second it was made.
func (a Assessment) Key() string {
	return a.Lens + "/" + a.Category + "/" + a.At.UTC().Format("20060102T150405Z")
}

// Entity is the assessment as My Apps writes it.
func (a Assessment) Entity() orrery.Entity {
	return orrery.Entity{
		Blueprint: AssessmentBlueprint,
		Key:       a.Key(),
		Title:     a.Lens + " " + a.Category,
		Fields: map[string]any{
			"lens": a.Lens, "category": a.Category, "rating": a.Rating,
			"covers": orNil(a.Covers), "missing": orNil(a.Missing),
			"by": a.By, "at": a.At.UTC().Format(time.RFC3339),
		},
	}
}

// Assessments reads myapps_assessment records, newest first.
func Assessments(records []orrery.Record) []Assessment {
	var out []Assessment
	for _, r := range records {
		if r.Hidden {
			continue
		}
		rating, _ := r.Number("rating")
		at, _ := r.Time("at")
		out = append(out, Assessment{Lens: r.String("lens"), Category: r.String("category"), Rating: int(rating), Covers: r.String("covers"), Missing: r.String("missing"), By: r.String("by"), At: at})
	}
	slices.SortStableFunc(out, func(x, y Assessment) int { return y.At.Compare(x.At) })
	return out
}

// Rated is one category with its current assessment and how many before.
type Rated struct {
	Category Category
	Current  *Assessment
	Earlier  int
	// Count is how many cards the lens places on the category.
	Count int
	// Kinds splits Count by kind, in the order of Kinds, leaving out the
	// kinds with none.
	Kinds []KindShare
	// Cards are the cards placed on it, in the lens's order.
	Cards []Card
}

// KindShare is how many of a category's cards are one kind, and that
// count's share of the category's bar, from 0 to 100.
type KindShare struct {
	Kind  Option
	Count int
	Share int
}

// RatingsOf pairs each top category of a lens with its newest assessment,
// its cards, and their count by kind.
func RatingsOf(l Landscape, all []Assessment) []Rated {
	counts := map[string]int{}
	byKind := map[string]map[string]int{}
	for _, c := range l.Cards {
		for _, k := range c.Categories {
			counts[k]++
			if byKind[k] == nil {
				byKind[k] = map[string]int{}
			}
			byKind[k][c.Kind]++
		}
	}
	out := make([]Rated, 0, len(l.Categories))
	for _, c := range l.Categories {
		r := Rated{Category: c, Count: counts[c.Key]}
		for _, card := range l.Cards {
			if slices.Contains(card.Categories, c.Key) {
				r.Cards = append(r.Cards, card)
			}
		}
		for _, k := range Kinds {
			if n := byKind[c.Key][k.Key]; n > 0 {
				r.Kinds = append(r.Kinds, KindShare{Kind: k, Count: n, Share: n * 100 / r.Count})
			}
		}
		for i := range all {
			if all[i].Lens != l.Lens.Blueprint || all[i].Category != c.Key {
				continue
			}
			if r.Current == nil {
				r.Current = &all[i]
			} else {
				r.Earlier++
			}
		}
		out = append(out, r)
	}
	return out
}

// Overlap is how many cards a lens places on more than one category, and
// the category pairs they share, most first.
type Overlap struct {
	Shared int
	Pairs  []Pair
}

// Pair is two categories and how many cards both hold.
type Pair struct {
	A, B  Category
	Count int
}

// OverlapOf counts the cards on more than one of a lens's categories.
func OverlapOf(l Landscape) Overlap {
	var o Overlap
	counts := map[[2]int]int{}
	for _, c := range l.Cards {
		var at []int
		for _, k := range c.Categories {
			if i := index(l.Categories, k); i >= 0 {
				at = append(at, i)
			}
		}
		if len(at) > 1 {
			o.Shared++
		}
		slices.Sort(at)
		for x := 0; x < len(at); x++ {
			for y := x + 1; y < len(at); y++ {
				counts[[2]int{at[x], at[y]}]++
			}
		}
	}
	for p, n := range counts {
		o.Pairs = append(o.Pairs, Pair{A: l.Categories[p[0]], B: l.Categories[p[1]], Count: n})
	}
	slices.SortFunc(o.Pairs, func(x, y Pair) int {
		return cmp.Or(cmp.Compare(y.Count, x.Count), cmp.Compare(x.A.Order, y.A.Order), cmp.Compare(x.B.Order, y.B.Order))
	})
	return o
}

// LensRelation is the relation an application places itself on a lens
// with: the one it already has to the lens's blueprint, else one named
// after the blueprint's thing, without its prefix, plural, such as pillars
// for plat_pillar; when that name is taken, the whole id, plural.
func LensRelation(blueprints []Blueprint, lens string) (name string, exists bool) {
	if have := RelationsTo(blueprints, ApplicationBlueprint, lens); len(have) > 0 {
		return have[0], true
	}
	thing := lens
	if i := strings.Index(lens, "_"); i >= 0 && i < len(lens)-1 {
		thing = lens[i+1:]
	}
	name = thing + "s"
	for _, b := range blueprints {
		if b.ID == ApplicationBlueprint {
			if _, taken := b.Relations[name]; taken {
				name = lens + "s"
			}
		}
	}
	return name, false
}

// LensRelationOffer is the relation a new lens needs on applications.
func LensRelationOffer(name, lens, title string) orrery.Blueprint {
	return orrery.Blueprint{
		"id":    ApplicationBlueprint,
		"title": "Application",
		"relations": map[string]any{
			name: map[string]any{"target": lens, "many": true, "title": title},
		},
	}
}

// Linked is one relation of an application to something My Apps reads,
// pointed at wherever the mappings hold that.
type Linked struct {
	Relation string
	Kind     MappingKind
	Def      map[string]any
}

// LinkedRelations are an application's relations to what My Apps reads.
var LinkedRelations = []Linked{
	{"sign_in", MapEntraApps, map[string]any{"many": true, "title": "Signs in through", "inverseTitle": "Application"}},
	{"access_groups", MapGroups, map[string]any{"many": true, "title": "Access groups", "inverseTitle": "Grants access to", "description": "With an Entra app, the groups a group request adds a person to. With none, the groups whose direct members may open it."}},
	{"services", MapServices, map[string]any{"many": true, "title": "Runs", "inverseTitle": "Applications"}},
	{"workload", MapWorkloads, map[string]any{"many": false, "title": "Deployed as", "inverseTitle": "Applications"}},
	{"api", MapAPIs, map[string]any{"many": false, "title": "The SaaS it is", "inverseTitle": "Applications"}},
	{"team", MapTeams, map[string]any{"many": false, "title": "Owned by"}},
}

// LinkedTo is the relation of an application to one kind, if it has one.
func LinkedTo(kind MappingKind) (Linked, bool) {
	for _, l := range LinkedRelations {
		if l.Kind == kind {
			return l, true
		}
	}
	return Linked{}, false
}

// CurationBlueprints are the parts of the catalog My Apps owns, offered
// at start and again when an admin maps a source. Orrery
// adds what a blueprint lacks and changes nothing that is there. An
// application links only to the kinds the mappings hold, each at its
// mapped blueprint, since the hub refuses a relation to a blueprint it
// lacks. The capability blueprint comes first, because the others link to
// it.
func CurationBlueprints(m Mappings) []orrery.Blueprint {
	out := make([]orrery.Blueprint, len(curationBlueprints))
	for i, b := range curationBlueprints {
		out[i] = b
		if b["id"] != ApplicationBlueprint {
			continue
		}
		app := maps.Clone(b)
		rels := maps.Clone(b["relations"].(map[string]any))
		for _, l := range LinkedRelations {
			if bp := m.Blueprint(l.Kind); bp != "" {
				def := maps.Clone(l.Def)
				def["target"] = bp
				rels[l.Relation] = def
			}
		}
		app["relations"] = rels
		out[i] = app
	}
	return out
}

var curationBlueprints = []orrery.Blueprint{
	{
		"id":          CapabilityBlueprint,
		"title":       "Business capability",
		"group":       "Organization",
		"description": "What the organization does, such as Finance, Shipping, or Collaboration, nested by parent. Every application is placed on it, so My Apps groups tiles and the Landscape by it for everyone.",
		"icon":        "layers",
		"fields": map[string]any{
			"order":       map[string]any{"type": "number", "title": "Order"},
			"description": map[string]any{"type": "markdown"},
		},
		"relations": map[string]any{
			"parent": map[string]any{"target": CapabilityBlueprint, "many": false, "title": "Part of", "inverseTitle": "Parts"},
		},
	},
	{
		"id":          ApplicationBlueprint,
		"title":       "Application",
		"group":       "Organization",
		"description": "An application people in the organization use: installed on their machine, cloud-hosted on someone else's platform, or self-hosted on ours. Not a service: an application links to the services it runs, the ones we built it from or the open-source project it is. Inventory: admins keep the list in My Apps.",
		"icon":        "app-window",
		"fields": map[string]any{
			"description":  map[string]any{"type": "markdown"},
			"url":          map[string]any{"type": "url", "title": "Address", "description": "Where people open it; for an installed application, where to get it."},
			"hosting":      map[string]any{"type": "enum", "values": []string{string(SelfHosted), string(SaaS), string(Installed)}, "title": "Hosting"},
			"self_built":   map[string]any{"type": "boolean", "title": "Self built", "description": "The organization built it itself, wherever it runs."},
			"lifecycle":    map[string]any{"type": "enum", "values": Lifecycles},
			"tier":         map[string]any{"type": "enum", "values": Tiers},
			"listed":       map[string]any{"type": "enum", "values": []string{string(Shown), string(NoLogin), string(Hidden)}, "title": "In My Apps", "description": "Shown is a tile and a card on the Landscape; no_login is hidden from login, a card with no tile; hidden is neither."},
			"audience":     map[string]any{"type": "array", "description": "The front-door roles that may open it. With no Entra app, anyone holding one of them, in one of its access groups, or among its people may; with none of the three, everyone signed in."},
			"people":       map[string]any{"type": "array", "title": "People", "description": "The emails of people who may open it when no Entra app decides."},
			"access":       map[string]any{"type": "enum", "values": []string{string(AccessOpen), string(AccessDirect), string(AccessGroup), string(AccessPackage)}, "title": "How people get it"},
			"access_role":  map[string]any{"type": "string", "title": "Role a request grants", "description": "The app role a direct request assigns; empty is default access."},
			"assign_teams": map[string]any{"type": "array", "title": "Teams that may grant it", "description": "Mark this field only an admin changes: delegated grant actions read it."},
			"updated_by":   map[string]any{"type": "user", "title": "Last changed by"},
			"updated_at":   map[string]any{"type": "datetime", "title": "Last changed"},
		},
		"relations": map[string]any{
			CapabilityRelation: map[string]any{"target": CapabilityBlueprint, "many": true, "title": "Business capabilities", "inverseTitle": "Applications"},
		},
	},
	{
		"id":          LensBlueprint,
		"title":       "My Apps lens",
		"group":       "Organization",
		"description": "A classification My Apps analyses coverage across, such as the platform pillars. The key is the classification's blueprint id. Record: the platform team keeps it in My Apps.",
		"icon":        "eye",
		"fields": map[string]any{
			"order": map[string]any{"type": "number", "title": "Order"},
		},
	},
	{
		"id":          AssessmentBlueprint,
		"title":       "Coverage assessment",
		"group":       "Organization",
		"description": "How strong one category of one lens is, what covers it, and what is missing. Record: the platform team writes one in My Apps whenever it looks again; the newest is current, older ones are its history.",
		"icon":        "gauge",
		"lifecycle":   map[string]any{"expire": map[string]any{"after": "730d", "field": "at"}},
		"fields": map[string]any{
			"lens":     map[string]any{"type": "string"},
			"category": map[string]any{"type": "string"},
			"rating":   map[string]any{"type": "number", "title": "Strength, 1 to 5"},
			"covers":   map[string]any{"type": "markdown", "title": "What covers it"},
			"missing":  map[string]any{"type": "markdown", "title": "What is missing"},
			"by":       map[string]any{"type": "user"},
			"at":       map[string]any{"type": "datetime"},
		},
	},
	{
		"id":          MappingBlueprint,
		"title":       "My Apps mapping",
		"group":       "Organization",
		"description": "Where My Apps reads one kind of thing in this hub: people, groups, Entra apps, assignments, workloads, services, APIs, or teams. The key is the kind; blueprint names the hub's blueprint, names the hub's name for each part, and values how it words each value My Apps acts on. Record: admins keep it on My Apps' Sources page.",
		"icon":        "link",
		"fields": map[string]any{
			"blueprint":  map[string]any{"type": "string", "title": "Blueprint"},
			"names":      map[string]any{"type": "object", "title": "Names"},
			"values":     map[string]any{"type": "object", "title": "Values"},
			"updated_by": map[string]any{"type": "user", "title": "Last changed by"},
		},
	},
	{
		"id":          IgnoredBlueprint,
		"title":       "My Apps dismissed suggestion",
		"group":       "Organization",
		"description": "An Entra app or workload an admin said is not an application, so My Apps stops suggesting it. The key is its ref.",
		"icon":        "eye-off",
		"fields": map[string]any{
			"by": map[string]any{"type": "user"},
			"at": map[string]any{"type": "datetime"},
		},
	},
	{
		"id":    ApprovalBlueprint,
		"title": "Approval",
		"relations": map[string]any{
			"application": map[string]any{"target": ApplicationBlueprint, "many": false, "title": "Application", "inverseTitle": "Requests"},
		},
	},
}

// RatingOf reads a rating from a form: a whole number from 1 to 5.
func RatingOf(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil && n >= 1 && n <= 5
}
