package catalog

import (
	"cmp"
	"net/url"
	"slices"
	"strings"

	"github.com/orchard-suite/platformkit/orrery"
)

// A landscape is one lens over the catalog: the lens's categories, every
// app people open, service built or adopted, and outside API placed in
// them, and every API one service consumes or wants to. It comes from the
// lens's own blueprint, the shown products, and Orrery's sw_service and
// sw_api entities.

// Kinds are where a card comes from, in the order the matrix groups them:
// an application built here, self-hosted, SaaS, installed, or an
// open-source project the organization runs, and, in analytics, a service
// adopted as open source.
var Kinds = []Option{
	{"built", "Built here"},
	{"self_hosted", "Self-hosted"},
	{"saas", "SaaS"},
	{"installed", "Installed"},
	{"adopted", "Open source"},
}

// KindsOf is the kinds the cards hold, in the order of Kinds, so a key
// names only what the page shows.
func KindsOf(cards []Card) []Option {
	return held(cards, func(c Card, k string) bool { return c.Kind == k })
}

// LegendOf is a wall's key: every kind its cards carry, in the order of
// Kinds, only those held.
func LegendOf(cards []Card) []Option {
	return held(cards, Card.Has)
}

// held is the kinds, in the order of Kinds, that some card has.
func held(cards []Card, has func(Card, string) bool) []Option {
	var out []Option
	for _, k := range Kinds {
		if slices.ContainsFunc(cards, func(c Card) bool { return has(c, k.Key) }) {
			out = append(out, k)
		}
	}
	return out
}

// Adoptions are whether a card runs today or is planned.
var Adoptions = []Option{
	{"in_use", "In use"},
	{"planned", "Planned"},
}

const (
	InUse   = "in_use"
	Planned = "planned"
)

// EdgeStates are whether a service consumes an API today or wants to.
var EdgeStates = []Option{
	{"live", "Live"},
	{"wanted", "Wanted"},
}

const (
	Live   = "live"
	Wanted = "wanted"
)

// Option is one value of a fixed list and the words for it.
type Option struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func optionOf(list []Option, key string) (Option, bool) {
	for _, o := range list {
		if o.Key == key {
			return o, true
		}
	}
	return Option{}, false
}

// OptionLabel is the words for a key of a fixed list, or the key itself.
func OptionLabel(list []Option, key string) string {
	if o, ok := optionOf(list, key); ok {
		return o.Label
	}
	return key
}

// Card is one thing on the landscape: a service built or adopted, or an
// outside API.
type Card struct {
	Key       string `json:"key"`
	Blueprint string `json:"blueprint"`
	// Kind is the one kind the card is counted under: built, adopted, or
	// for an application not built from either, its hosting.
	Kind string `json:"kind"`
	// Hosting is where an application runs; empty for a service or an API.
	Hosting Hosting `json:"hosting,omitempty"`
	// Origins are the origins an application has on top of its hosting,
	// in the order of Kinds: built, adopted, both, or neither.
	Origins     []string `json:"origins,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Category    string   `json:"category,omitempty"`
	// categoryOrder is where Category sits among its siblings, so the
	// wall boxes them in the catalog's order.
	categoryOrder int
	Lifecycle     string `json:"lifecycle,omitempty"`
	// Adoption is planned for an adopted service or an outside API still in
	// development, otherwise in use.
	Adoption string `json:"adoption"`
	// URL is the service's repository or the API's base address.
	URL  string `json:"url,omitempty"`
	Icon string `json:"icon,omitempty"`
	// Tier is the service's or API's tier, such as critical, when set.
	Tier string `json:"tier,omitempty"`
	// Categories are the category keys the card names: known ones in category
	// order, then any others.
	Categories []string `json:"categories"`
	// Team is the owning team's name, for an app people open.
	Team string `json:"team,omitempty"`
	// App is the application, blueprint:key, people open from this card;
	// empty for a service or an API.
	App string `json:"app,omitempty"`
	// BuiltFrom are the services an application is built from.
	BuiltFrom []Built `json:"builtFrom,omitempty"`
	// Orrery is the card's page in Orrery, when its public address is known.
	Orrery  string   `json:"orrery,omitempty"`
	Status  Status   `json:"status"`
	Reasons []string `json:"reasons,omitempty"`
	// Integration is how the card works with Orrery, from what its
	// sources wrote; nil when the sources are unknown.
	Integration *Integration `json:"integration,omitempty"`
}

// Edge is one service consuming one API, today (live) or wanted. Provider
// is the service the API is built from, empty for an outside API.
type Edge struct {
	Consumer string `json:"consumer"`
	API      string `json:"api"`
	Provider string `json:"provider"`
	State    string `json:"state"`
}

// API is what the panels need of every API, on the landscape or not.
type API struct {
	Key      string
	Title    string
	Provider string
}

// Landscape is one lens's categories, the cards, and the edges between
// them. Categories are the lens's top categories: a card on a child is
// placed on its top.
type Landscape struct {
	Lens       Lens
	Categories []Category
	Cards      []Card
	Edges      []Edge
	// APIs and Titles name the APIs and services the edges reach, for the
	// panels.
	APIs   []API
	Titles map[string]string
	// Sources are Orrery's sources, read with the landscape. SourcesKnown
	// is false when they could not be read or the hub does not say what
	// each wrote: every card's integration is then unknown.
	Sources      []Source
	SourcesKnown bool
}

// ShapeLandscape turns one lens's category entities, the applications,
// and, with services set, Orrery's sw_service and sw_api entities into that
// lens's landscape. The Landscape everyone sees is applications only;
// analytics adds the services and outside APIs. relations names, for a
// blueprint, its relations that point at the lens. hub is Orrery's public
// address, for each card's page there; empty, cards carry no link to it.
func ShapeLandscape(lens Lens, categories []orrery.Record, apps []Application, withServices bool, services, apis []orrery.Record, relations func(blueprint string) []string, hub string) Landscape {
	hub = strings.TrimRight(hub, "/")
	all := Categories(categories)
	l := Landscape{Lens: lens, Categories: Roots(all), Cards: []Card{}, Edges: []Edge{}, Titles: map[string]string{}}
	place := func(blueprint string, links map[string][]string) []string {
		var keys []string
		for _, rel := range relations(blueprint) {
			for _, k := range links[rel] {
				// A key the lens does not hold stays as it is, so the card
				// still shows, under Other.
				if r := cmp.Or(RootOf(all, k), k); !slices.Contains(keys, r) {
					keys = append(keys, r)
				}
			}
		}
		return keys
	}
	svc := make(map[string]orrery.Record, len(services))
	for _, s := range services {
		if !s.Hidden {
			svc[s.Key] = s
			l.Titles[s.Key] = cmp.Or(s.Title, s.Key)
		}
	}
	for _, a := range apps {
		if !a.Listed.OnLandscape() || !a.Seen {
			continue
		}
		// On the Landscape an application placed nowhere still shows,
		// under Other; in analytics it is only where the lens places it.
		if c, ok := appCard(a, place(ApplicationBlueprint, a.Links), !withServices, all, svc, hub); ok {
			l.Cards = append(l.Cards, c)
		}
	}
	known := make(map[string]API, len(apis))
	for _, a := range apis {
		if a.Hidden {
			continue
		}
		api := API{Key: a.Key, Title: cmp.Or(a.Title, a.Key)}
		external, _ := a.Bool("external")
		if p, ok := a.Related("service"); ok && !external {
			api.Provider = p.Key
		}
		known[a.Key] = api
		l.APIs = append(l.APIs, api)
		l.Titles[a.Key] = api.Title
		if external && withServices {
			if c, ok := card(a, "saas", a.String("base_url"), true, place(a.Blueprint, linksOf(a)), l.Categories, hub); ok {
				l.Cards = append(l.Cards, c)
			}
		}
	}
	if withServices {
		for _, s := range services {
			kind := s.String("origin")
			if s.Hidden || (kind != "built" && kind != "adopted") {
				continue
			}
			if c, ok := card(s, kind, s.String("repository_url"), kind == "adopted", place(s.Blueprint, linksOf(s)), l.Categories, hub); ok {
				l.Cards = append(l.Cards, c)
			}
		}
	}
	sortCards(l.Cards, l.Categories)
	onLandscape := make(map[string]bool, len(l.Cards))
	for _, c := range l.Cards {
		onLandscape[c.Key] = true
	}
	for _, s := range services {
		if s.Hidden || !onLandscape[s.Key] {
			continue
		}
		for _, rel := range []struct{ name, state string }{{"consumes", Live}, {"will_consume", Wanted}} {
			for _, ref := range s.Relations[rel.name] {
				if api, ok := known[ref.Key]; ok {
					l.Edges = append(l.Edges, Edge{Consumer: s.Key, API: api.Key, Provider: api.Provider, State: rel.state})
				}
			}
		}
	}
	// Wanted edges first: they are the gaps.
	slices.SortStableFunc(l.Edges, func(x, y Edge) int {
		return cmp.Or(cmp.Compare(y.State, x.State), cmp.Compare(x.Consumer, y.Consumer), cmp.Compare(x.API, y.API))
	})
	return l
}

// Built is a service an application runs, and its repository.
type Built struct {
	Title string `json:"title"`
	Repo  string `json:"repo,omitempty"`
}

// appCard shapes a shown application placed on at least one of the lens's
// top categories, or on none when unplaced is set. Its box is the child
// category it names first, if any. Its hosting is always set. It is built
// here when it is self built or a service it runs was built here, and open
// source when it runs an adopted one; it may be both. Its kind is built
// here, else open source, else its hosting.
func appCard(a Application, keys []string, unplaced bool, all []Category, services map[string]orrery.Record, hub string) (Card, bool) {
	if len(keys) == 0 && !unplaced {
		return Card{}, false
	}
	roots := Roots(all)
	slices.SortStableFunc(keys, func(x, y string) int { return cmp.Compare(rank(roots, x), rank(roots, y)) })
	c := Card{
		Key:         a.Key,
		Blueprint:   ApplicationBlueprint,
		Hosting:     cmp.Or(a.Hosting, SelfHosted),
		Title:       a.Name,
		Description: a.Description,
		Lifecycle:   a.Lifecycle,
		Adoption:    InUse,
		URL:         a.URL,
		Icon:        a.Logo,
		Tier:        a.Tier,
		Categories:  keys,
		Team:        a.TeamName,
		App:         a.Ref(),
		Status:      a.status,
		Reasons:     a.reason,
	}
	if a.Lifecycle == "planned" {
		c.Adoption = Planned
	}
	built, adopted := a.SelfBuilt, false
	for _, k := range a.Services {
		s, ok := services[k]
		if !ok {
			continue
		}
		c.BuiltFrom = append(c.BuiltFrom, Built{Title: cmp.Or(s.Title, s.Key), Repo: cmp.Or(s.String("repository_url"), s.String("url"))})
		switch s.String("origin") {
		case "built":
			built = true
		case AdoptedOrigin:
			adopted = true
		}
	}
	c.Kind = string(c.Hosting)
	if adopted {
		c.Origins = []string{AdoptedOrigin}
		c.Kind = AdoptedOrigin
	}
	if built {
		c.Origins = slices.Insert(c.Origins, 0, "built")
		c.Kind = "built"
	}
	for _, k := range a.Links[CapabilityRelation] {
		if i := index(all, k); i >= 0 && all[i].Parent != "" {
			c.Category = all[i].Name
			c.categoryOrder = all[i].Order
			break
		}
	}
	c.Orrery = ApplicationPage(hub, a.Key)
	return c, true
}

// ApplicationPage is where an application's record opens in Orrery, or
// nothing when people are not given Orrery's address.
func ApplicationPage(hub, key string) string {
	if hub == "" {
		return ""
	}
	return hub + "/entities/" + url.PathEscape(ApplicationBlueprint) + "/" + escapeKey(key)
}

// card shapes a service or an outside API placed on at least one of the
// lens's top categories. plannedInDev says a development lifecycle reads
// as planned.
func card(e orrery.Record, kind, link string, plannedInDev bool, keys []string, categories []Category, hub string) (Card, bool) {
	if len(keys) == 0 {
		return Card{}, false
	}
	slices.SortStableFunc(keys, func(x, y string) int {
		return cmp.Compare(rank(categories, x), rank(categories, y))
	})
	c := Card{
		Key:         e.Key,
		Blueprint:   e.Blueprint,
		Kind:        kind,
		Title:       cmp.Or(e.Title, e.Key),
		Description: strings.TrimSpace(e.String("description")),
		Category:    strings.TrimSpace(e.String("category")),
		Lifecycle:   e.String("lifecycle"),
		Adoption:    InUse,
		URL:         link,
		Icon:        e.Icon,
		Tier:        strings.ToLower(strings.TrimSpace(e.String("tier"))),
		Categories:  keys,
		Status:      statusOf(e.State, e.Status.Level),
		Reasons:     e.Status.Reasons,
	}
	if plannedInDev && c.Lifecycle == "development" {
		c.Adoption = Planned
	}
	if hub != "" {
		c.Orrery = hub + "/entities/" + url.PathEscape(e.Blueprint) + "/" + escapeKey(e.Key)
	}
	return c, true
}

// escapeKey escapes each segment of a key that holds slashes.
func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// rank is a category key's place, with unknown keys after every known one.
func rank(categories []Category, key string) int {
	if i := index(categories, key); i >= 0 {
		return i
	}
	return len(categories)
}

// sortCards orders cards by their first category, then category and title.
func sortCards(cards []Card, categories []Category) {
	slices.SortStableFunc(cards, func(x, y Card) int {
		if c := cmp.Compare(rank(categories, first(x.Categories)), rank(categories, first(y.Categories))); c != 0 {
			return c
		}
		if c := cmp.Compare(strings.ToLower(x.Category), strings.ToLower(y.Category)); c != 0 {
			return c
		}
		return cmp.Compare(strings.ToLower(x.Title), strings.ToLower(y.Title))
	})
}

// Serves says whether the card names the category.
func (c Card) Serves(category string) bool { return slices.Contains(c.Categories, category) }

// Size is how large a card is drawn on the wall.
type Size string

const (
	// Large is a big logo over its title and kinds: a card that matters
	// most. It is three cells square.
	Large Size = "large"
	// Medium is a logo over its title, two cells square.
	Medium Size = "medium"
	// Small is a logo alone, one cell; its title shows on hover and focus.
	Small Size = "small"
)

// Side is how many cells wide and tall a card of the size is.
func (s Size) Side() int {
	switch s {
	case Large:
		return 3
	case Medium:
		return 2
	}
	return 1
}

// SizeOf decides a card's size from its tier: critical is large,
// important medium. A card with no tier is standard, so it is small, like
// everything else.
func SizeOf(c Card) Size {
	switch c.Tier {
	case "critical":
		return Large
	case "important":
		return Medium
	}
	return Small
}

// Size is the card's size on the wall.
func (c Card) Size() Size { return SizeOf(c) }

// Base is a card's first layer: an application's hosting, or the kind of
// a service or an API.
func (c Card) Base() string { return cmp.Or(string(c.Hosting), c.Kind) }

// Layers are every kind a card has: its base, then its origins.
func (c Card) Layers() []Option {
	out := []Option{{c.Base(), OptionLabel(Kinds, c.Base())}}
	for _, k := range c.Origins {
		out = append(out, Option{k, OptionLabel(Kinds, k)})
	}
	return out
}

// Has says whether a card has a kind among its layers.
func (c Card) Has(kind string) bool {
	return c.Base() == kind || slices.Contains(c.Origins, kind)
}

// Host is where a card runs, as a kind: a hosting, or SaaS for an outside
// API. It is empty for a service, which runs inside an application.
func (c Card) Host() string {
	if slices.Contains(Hostings, Hosting(c.Base())) {
		return c.Base()
	}
	return ""
}

// KindKeys are the keys of a card's layers, spaced, for the page's kind
// filter to match any one of them.
func (c Card) KindKeys() string {
	keys := make([]string, 0, 1+len(c.Origins))
	for _, l := range c.Layers() {
		keys = append(keys, l.Key)
	}
	return strings.Join(keys, " ")
}

// Label is the words on a card's strip: Planned, or every layer it has.
func (c Card) Label() string {
	if c.Adoption == Planned {
		return OptionLabel(Adoptions, Planned)
	}
	return c.LayerLabel()
}

// LayerLabel is the words for every layer a card has.
func (c Card) LayerLabel() string {
	words := make([]string, 0, 1+len(c.Origins))
	for _, l := range c.Layers() {
		words = append(words, l.Label)
	}
	return strings.Join(words, " · ")
}

// Box is one category inside a category's column. Its cards are the
// largest first, each size in the order the cards came.
type Box struct {
	Name  string
	Cards []Card
	// Cells is the wall's area the box needs, in cells: a card covers its
	// side squared.
	Cells int
	// Span is how many cells wide the box wants to be: as wide as its
	// cards need at the height of its tallest card, up to MaxSpan. The
	// wall grows a box past it to fill a row.
	Span int
	// Fit is the fewest cells wide the box may be: its tallest card's side.
	Fit int
	// order is the catalog order of the category the box is named for.
	order int
}

// Column is one category in the grid and how many cards serve it.
type Column struct {
	Category Category
	Boxes    []Box
	Count    int
}

// Grid lays the cards out by category: one column per category, a card in
// every category it names, then Other for cards that name no known category.
// A column holds a box per category, in the catalog's order of those
// categories; cards with no category share a box with no name.
func Grid(cards []Card, categories []Category) []Column {
	cols := make([]Column, 0, len(categories)+1)
	for _, p := range categories {
		cols = append(cols, Column{Category: p})
	}
	other := Column{Category: Other(categories)}
	for _, c := range cards {
		placed := false
		for _, key := range c.Categories {
			if i := index(categories, key); i >= 0 {
				addCard(&cols[i], c)
				placed = true
			}
		}
		if !placed {
			addCard(&other, c)
		}
	}
	if other.Count > 0 {
		cols = append(cols, other)
	}
	for i := range cols {
		slices.SortStableFunc(cols[i].Boxes, func(x, y Box) int { return cmp.Compare(x.order, y.order) })
		for j := range cols[i].Boxes {
			shapeBox(&cols[i].Boxes[j])
		}
	}
	return cols
}

// MaxSpan caps how wide a box asks to be before its cards wrap to more
// rows.
const MaxSpan = 12

// shapeBox puts the largest cards first and works out the box's area and
// widths.
func shapeBox(b *Box) {
	slices.SortStableFunc(b.Cards, func(x, y Card) int {
		return cmp.Compare(y.Size().Side(), x.Size().Side())
	})
	b.Cells, b.Fit = 0, 1
	for _, c := range b.Cards {
		side := c.Size().Side()
		b.Cells += side * side
		b.Fit = max(b.Fit, side)
	}
	b.Span = max(b.Fit, min(MaxSpan, (b.Cells+b.Fit-1)/b.Fit))
}

func addCard(col *Column, c Card) {
	col.Count++
	name := c.Category
	for i := range col.Boxes {
		if col.Boxes[i].Name == name {
			col.Boxes[i].Cards = append(col.Boxes[i].Cards, c)
			return
		}
	}
	col.Boxes = append(col.Boxes, Box{Name: name, Cards: []Card{c}, order: c.categoryOrder})
}

// Mark is how a card relates to a category in the matrix.
type Mark string

const (
	MarkNone    Mark = ""
	MarkServes  Mark = "serves"
	MarkPlanned Mark = "planned"
)

// MarkOf says how the card relates to the category: it serves it, it is
// planned for it, or neither.
func (c Card) MarkOf(category string) Mark {
	switch {
	case !c.Serves(category):
		return MarkNone
	case c.Adoption == Planned:
		return MarkPlanned
	}
	return MarkServes
}

// Row is one card across the categories, a mark per category in order.
type Row struct {
	Card  Card
	Marks []Mark
}

// RowGroup is the rows of one kind.
type RowGroup struct {
	Kind Option
	Rows []Row
}

// Total is how many cards serve a category today. Thin is true below two: a
// category one card carries alone.
type Total struct {
	Category Category
	Count    int
	Thin     bool
}

// Matrix is the cards against the categories.
type Matrix struct {
	Categories []Category
	Groups     []RowGroup
	Totals     []Total
}

// ThinBelow is the count under which a category is marked thin.
const ThinBelow = 2

// MatrixOf lays the cards out as rows grouped by kind, in the order of
// Kinds, against the categories, with a total per category of the cards that
// serve it today. A planned card is marked but not counted.
func MatrixOf(cards []Card, categories []Category) Matrix {
	m := Matrix{Categories: categories}
	for _, kind := range Kinds {
		g := RowGroup{Kind: kind}
		for _, c := range cards {
			if c.Kind != kind.Key {
				continue
			}
			row := Row{Card: c, Marks: make([]Mark, len(categories))}
			for i, p := range categories {
				row.Marks[i] = c.MarkOf(p.Key)
			}
			g.Rows = append(g.Rows, row)
		}
		if len(g.Rows) > 0 {
			// Rows run in the order of the first category each serves, so
			// the marks form blocks; within one, by title.
			first := func(r Row) int {
				if i := slices.IndexFunc(r.Marks, func(m Mark) bool { return m != MarkNone }); i >= 0 {
					return i
				}
				return len(categories)
			}
			slices.SortStableFunc(g.Rows, func(x, y Row) int {
				return cmp.Or(cmp.Compare(first(x), first(y)), cmp.Compare(strings.ToLower(x.Card.Title), strings.ToLower(y.Card.Title)))
			})
			m.Groups = append(m.Groups, g)
		}
	}
	for _, p := range categories {
		t := Total{Category: p}
		for _, c := range cards {
			if c.MarkOf(p.Key) == MarkServes {
				t.Count++
			}
		}
		t.Thin = t.Count < ThinBelow
		m.Totals = append(m.Totals, t)
	}
	return m
}

// Link is one edge as a card's panel shows it, with its ends named.
type Link struct {
	Edge     Edge
	API      string
	Provider string
	Consumer string
}

// Detail is everything a card's panel shows.
type Detail struct {
	Card       Card
	Categories []Category
	// Provides is the APIs the service is built from.
	Provides []API
	// Consumes and WillConsume are the APIs the service calls today and
	// wants to; ConsumedBy is who calls the card's own APIs.
	Consumes, WillConsume, ConsumedBy []Link
}

// Details shapes one panel per card, in the cards' order.
func Details(l Landscape) []Detail {
	link := func(e Edge) Link {
		return Link{Edge: e, API: l.Titles[e.API], Provider: l.Titles[e.Provider], Consumer: l.Titles[e.Consumer]}
	}
	out := make([]Detail, 0, len(l.Cards))
	for _, c := range l.Cards {
		d := Detail{Card: c}
		for _, key := range c.Categories {
			if i := index(l.Categories, key); i >= 0 {
				d.Categories = append(d.Categories, l.Categories[i])
			}
		}
		for _, a := range l.APIs {
			if a.Provider == c.Key {
				d.Provides = append(d.Provides, a)
			}
		}
		for _, e := range l.Edges {
			switch {
			case e.Consumer == c.Key && e.State == Live:
				d.Consumes = append(d.Consumes, link(e))
			case e.Consumer == c.Key:
				d.WillConsume = append(d.WillConsume, link(e))
			}
			if e.Provider == c.Key || e.API == c.Key {
				d.ConsumedBy = append(d.ConsumedBy, link(e))
			}
		}
		out = append(out, d)
	}
	return out
}
