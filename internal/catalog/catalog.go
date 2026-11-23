// Package catalog is what myapps knows about the apps: the model of a tile,
// the platform engineering categories that group them, who may see each one,
// and the Orrery reader and cache that supply them.
package catalog

import (
	"cmp"
	"context"
	"log/slog"
	"math"
	"slices"
	"strings"

	"github.com/orchard-suite/platformkit/orrery"
)

// Category is one place in a classification: a business capability, a
// platform pillar, or any scheme an estate sorts things by. In Orrery it is
// an entity of the classification's own blueprint, with `order`,
// `description`, and an optional `parent` for a tree.
type Category struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Order       int    `json:"order"`
	Description string `json:"description,omitempty"`
	// Parent is the category this one is part of; empty at the top.
	Parent string `json:"parent,omitempty"`
}

// OtherKey is the category of whatever names no known category.
const OtherKey = "other"

// Other holds what names no known category. It is shown last and only when
// it has something in it.
func Other(categories []Category) Category {
	return Category{Key: OtherKey, Name: "More apps", Order: len(categories) + 1}
}

// Categories shapes a classification's entities, by their order field and
// then name. One without an order goes last. A parent the classification
// does not hold is dropped, so the category is at the top.
func Categories(records []orrery.Record) []Category {
	type ranked struct {
		c     Category
		order float64
	}
	keys := map[string]bool{}
	for _, r := range records {
		if !r.Hidden {
			keys[r.Key] = true
		}
	}
	var rs []ranked
	for _, r := range records {
		if r.Hidden || r.Key == "" || r.Key == OtherKey {
			continue
		}
		order, set := r.Number("order")
		if !set {
			order = math.Inf(1)
		}
		c := Category{Key: r.Key, Name: cmp.Or(r.Title, r.Key), Description: strings.TrimSpace(r.String("description"))}
		if p, ok := r.Related("parent"); ok && keys[p.Key] && p.Key != r.Key {
			c.Parent = p.Key
		}
		rs = append(rs, ranked{c, order})
	}
	slices.SortStableFunc(rs, func(x, y ranked) int {
		if c := cmp.Compare(x.order, y.order); c != 0 {
			return c
		}
		return cmp.Compare(strings.ToLower(x.c.Name), strings.ToLower(y.c.Name))
	})
	out := make([]Category, len(rs))
	for i, r := range rs {
		r.c.Order = i + 1
		out[i] = r.c
	}
	return out
}

// Roots are the categories at the top, in order, numbered among
// themselves.
func Roots(categories []Category) []Category {
	var out []Category
	for _, c := range categories {
		if c.Parent == "" {
			c.Order = len(out) + 1
			out = append(out, c)
		}
	}
	return out
}

// Children are the categories directly under key, in order.
func Children(categories []Category, key string) []Category {
	var out []Category
	for _, c := range categories {
		if c.Parent == key {
			out = append(out, c)
		}
	}
	return out
}

// RootOf is the top of key's tree, or empty when key is unknown. A cycle
// stops where it repeats.
func RootOf(categories []Category, key string) string {
	seen := map[string]bool{}
	for key != "" && !seen[key] {
		seen[key] = true
		i := index(categories, key)
		if i < 0 {
			return ""
		}
		if categories[i].Parent == "" {
			return key
		}
		key = categories[i].Parent
	}
	return ""
}

// categoryKeys is the keys of a classification relation, once each, in
// the order the relation names them.
func categoryKeys(refs []orrery.Ref) []string { return refKeys(refs) }

// index is where a category key sits among the categories, or -1.
func index(categories []Category, key string) int {
	return slices.IndexFunc(categories, func(p Category) bool { return p.Key == key })
}

// First is the first of the top categories, in their order, that keys
// reach, directly or through a child; Other when they reach none.
func First(categories []Category, keys []string) Category {
	roots := make([]string, 0, len(keys))
	for _, k := range keys {
		if r := RootOf(categories, k); r != "" {
			roots = append(roots, r)
		}
	}
	for _, c := range Roots(categories) {
		if slices.Contains(roots, c.Key) {
			return c
		}
	}
	return Other(Roots(categories))
}

// Status is a tile's dot, from Orrery's resolved status.
type Status string

const (
	Green Status = "green"
	Amber Status = "amber"
	Red   Status = "red"
	Grey  Status = "grey"
)

// App is one tile.
type App struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Tier        string `json:"tier,omitempty"`
	Owner       string `json:"owner,omitempty"`
	// Team is the owning team's name.
	Team string `json:"team,omitempty"`
	URL  string `json:"url"`
	// Ref is the product the tile is made from, blueprint:key.
	Ref string `json:"-"`
	// Capabilities are the business capabilities the product is placed on.
	Capabilities []string `json:"capabilities"`
	Audience     []string `json:"audience,omitempty"`
	// Groups are the Entra groups whose direct members may open an app no
	// Entra app signs people in to. People are the emails that may. They
	// name people, so they never leave the server.
	Groups  []string `json:"-"`
	People  []string `json:"-"`
	Status  Status   `json:"status"`
	Reasons []string `json:"reasons,omitempty"`
	// Entra is true for a tile people sign in to through Entra.
	Entra bool `json:"entra,omitempty"`
	// Hosting is where the application runs.
	Hosting Hosting `json:"hosting,omitempty"`
	// Access is how a person gets the application.
	Access AccessMode `json:"-"`
	// Repository is where the application's code lives, as the forge names
	// it; empty when the catalog does not say.
	Repository string `json:"-"`
	// SignIn is who Entra lets sign in to it, when Entra signs people in.
	// It names people, so it never leaves the server.
	SignIn *SignIn `json:"-"`
}

// Directory is one read of the tiles: the business capabilities, the
// applications and the tiles made of the shown ones, the Entra apps, what
// the sources found that no application is paired with yet, who holds what
// in Entra, the teams, and how many approvals wait.
type Directory struct {
	Capabilities []Category
	Apps         []App
	Applications []Application
	EntraApps    []EntraApp
	Workloads    []Category
	Suggestions  []Suggestion
	Access       Access
	// Teams are the hub's teams, for the owner of an application.
	Teams []Category
	// Waiting is the number of it_approval records still pending.
	Waiting int
	// Mappings are where the hub holds what My Apps reads.
	Mappings Mappings
}

// Viewer is who is looking: the front door's email and roles, and the
// person as Entra knows them, nil when Entra was not asked or does not know
// them.
type Viewer struct {
	Email  string
	Roles  []string
	Person *Person
}

// VisibleTo says whether this viewer may see the app, and why not. Role
// names are compared without regard to case or surrounding space.
//
// An app Entra signs people in to is shown only to someone Entra lets sign
// in, and in its audience when it has one. Any other app is for everyone
// signed in until it names a role, an Entra group, or a person; then it is
// for anyone who holds one of those roles, is directly in one of those
// groups, or is one of those people.
func (a App) VisibleTo(v Viewer) (bool, string) {
	if a.SignIn != nil {
		if !a.inAudience(v.Roles) {
			return false, "not in audience"
		}
		if !a.SignIn.Admits(v.Person) {
			return false, "entra does not let them sign in"
		}
		return true, ""
	}
	if len(a.Audience) == 0 && len(a.Groups) == 0 && len(a.People) == 0 {
		return true, ""
	}
	if len(a.Audience) > 0 && a.inAudience(v.Roles) {
		return true, ""
	}
	if v.Person != nil && slices.ContainsFunc(v.Person.Groups, func(g string) bool { return slices.Contains(a.Groups, g) }) {
		return true, ""
	}
	if v.Email != "" && slices.ContainsFunc(a.People, func(p string) bool { return strings.EqualFold(p, strings.TrimSpace(v.Email)) }) {
		return true, ""
	}
	return false, "no role, group, or person of theirs is granted"
}

func (a App) inAudience(groups []string) bool {
	if len(a.Audience) == 0 {
		return true
	}
	for _, want := range a.Audience {
		for _, have := range groups {
			if strings.EqualFold(strings.TrimSpace(want), strings.TrimSpace(have)) {
				return true
			}
		}
	}
	return false
}

// Filter keeps the apps the viewer may see and logs each decision, both
// ways, so a missing tile can be explained from the logs.
func Filter(ctx context.Context, log *slog.Logger, apps []App, v Viewer) []App {
	shown := make([]App, 0, len(apps))
	var hidden []string
	for _, a := range apps {
		if ok, why := a.VisibleTo(v); !ok {
			log.DebugContext(ctx, "app hidden: "+why, "app", a.Key, "audience", a.Audience)
			hidden = append(hidden, a.Key)
			continue
		}
		log.DebugContext(ctx, "app shown", "app", a.Key, "audience", a.Audience)
		shown = append(shown, a)
	}
	log.InfoContext(ctx, "apps filtered", "shown", len(shown), "hidden", len(hidden), "hidden_apps", hidden, "entra_known", v.Person != nil)
	return shown
}

// Group is a category and the apps in it.
type Group struct {
	Category Category `json:"category"`
	Apps     []App    `json:"apps"`
}

// GroupBy sorts each app under the first top capability, in order, that
// it is placed on, directly or through a child; then Other when any app is
// placed on none. Apps keep the order they came in. With no capabilities
// at all, every app is in one group.
func GroupBy(apps []App, capabilities []Category) []Group {
	roots := Roots(capabilities)
	groups := make([]Group, 0, len(roots)+1)
	for _, c := range roots {
		groups = append(groups, Group{Category: c})
	}
	other := Group{Category: Other(roots)}
	for _, a := range apps {
		i := index(roots, First(capabilities, a.Capabilities).Key)
		if i < 0 {
			other.Apps = append(other.Apps, a)
			continue
		}
		groups[i].Apps = append(groups[i].Apps, a)
	}
	if len(other.Apps) > 0 {
		groups = append(groups, other)
	}
	return groups
}

// SubGroup is the apps of one group that sit under one child capability.
type SubGroup struct {
	Category Category
	Apps     []App
}

// SubGroupsOf splits a group's apps by the child capability each sits under,
// in the children's order, so Home names the same boxes as the Landscape.
// An app placed on the group itself, or under no child, comes first with
// an empty category. Fewer than two parts is no split: nil.
func SubGroupsOf(g Group, all []Category) []SubGroup {
	byKey := make(map[string]Category, len(all))
	for _, c := range all {
		byKey[c.Key] = c
	}
	// childOf walks up from a capability to the child of the group's top.
	childOf := func(key string) (Category, bool) {
		for seen := 0; seen < len(all); seen++ {
			c, ok := byKey[key]
			if !ok {
				return Category{}, false
			}
			if c.Parent == g.Category.Key {
				return c, true
			}
			key = c.Parent
		}
		return Category{}, false
	}
	kids := Children(all, g.Category.Key)
	slices.SortStableFunc(kids, func(x, y Category) int { return cmp.Compare(x.Order, y.Order) })
	parts := make([]SubGroup, len(kids)+1)
	for i, k := range kids {
		parts[i+1].Category = k
	}
	for _, a := range g.Apps {
		at := 0
		for _, key := range a.Capabilities {
			if c, ok := childOf(key); ok {
				at = 1 + slices.IndexFunc(kids, func(k Category) bool { return k.Key == c.Key })
				break
			}
		}
		parts[at].Apps = append(parts[at].Apps, a)
	}
	out := slices.DeleteFunc(parts, func(p SubGroup) bool { return len(p.Apps) == 0 })
	if len(out) < 2 {
		return nil
	}
	return out
}
