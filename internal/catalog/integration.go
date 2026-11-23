package catalog

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// A source is one writer Orrery knows: a pusher, a git source, an app
// that writes its own entities. What it acts for and what it wrote are what
// Orrery saw, so a card's integration is evidence, never a claim.

// Source is one of Orrery's sources, as GET /v1/sources lists it, cut
// to what the analysis needs. It carries no token.
type Source struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	// ActsFor is the entity the source speaks for, <blueprint>:<key>, or
	// empty.
	ActsFor string `json:"actsFor,omitempty"`
	// Writes is what the source wrote, or nil when the hub does not say.
	Writes *Writes `json:"writes,omitempty"`
}

// Writes is the last write a source made of each kind, by target.
type Writes struct {
	Entities   []EntityWrite `json:"entities"`
	Blueprints []Write       `json:"blueprints"`
	Actions    []Write       `json:"actions"`
}

// EntityWrite is the entities of one blueprint a source wrote.
type EntityWrite struct {
	Blueprint string    `json:"blueprint"`
	Count     int       `json:"count"`
	LastAt    time.Time `json:"lastAt"`
}

// Write is one blueprint offered or one action put, and when last.
type Write struct {
	ID     string    `json:"id"`
	LastAt time.Time `json:"lastAt"`
}

// Evidence is one integration mark and what backs it: the blueprints or
// action ids, and the sources that wrote them. The mark is on when Items
// is not empty.
type Evidence struct {
	Items   []string `json:"items"`
	Sources []string `json:"sources"`
}

// On says whether the mark is set.
func (e Evidence) On() bool { return len(e.Items) > 0 }

func (e *Evidence) add(source string, items ...string) {
	for _, it := range items {
		if !slices.Contains(e.Items, it) {
			e.Items = append(e.Items, it)
		}
	}
	if len(items) > 0 && !slices.Contains(e.Sources, source) {
		e.Sources = append(e.Sources, source)
	}
}

func (e *Evidence) sort() {
	if e.Items == nil {
		e.Items = []string{}
	}
	if e.Sources == nil {
		e.Sources = []string{}
	}
	slices.Sort(e.Items)
	slices.Sort(e.Sources)
}

// Integration is how a card works with Orrery natively, from what its
// sources wrote.
type Integration struct {
	// Data: wrote entities of any blueprint.
	Data Evidence `json:"data"`
	// Blueprints: offered any blueprint.
	Blueprints Evidence `json:"blueprints"`
	// Actions: put at least one action.
	Actions Evidence `json:"actions"`
}

// Ref is the entity reference a source's ActsFor names a card by.
func (c Card) Ref() string { return c.Blueprint + ":" + c.Key }

// HomeOf is the card a source's work counts for: the card it acts for,
// else the nearest card of the same blueprint whose key its key sits under
// (a pusher keyed github/acme/hub/cmd/pusher is part of github/acme/hub),
// as ownership is inherited down a path. ok is false when no card holds it.
func HomeOf(actsFor string, cards []Card) (home Card, ok bool) {
	blueprint, key, found := strings.Cut(actsFor, ":")
	if !found {
		return Card{}, false
	}
	for _, c := range cards {
		if c.Blueprint != blueprint {
			continue
		}
		if c.Key == key {
			return c, true
		}
		if strings.HasPrefix(key, c.Key+"/") && len(c.Key) > len(home.Key) {
			home, ok = c, true
		}
	}
	return home, ok
}

// IntegrationOf is the union, over every source whose home is the card,
// of what each wrote: one acting for a part of the card adds its data,
// blueprints, and actions as one acting for the card itself does.
func IntegrationOf(c Card, sources []Source, cards []Card) Integration {
	var in Integration
	for _, s := range sources {
		home, ok := HomeOf(s.ActsFor, cards)
		if !ok || home.Ref() != c.Ref() || s.Writes == nil {
			continue
		}
		for _, w := range s.Writes.Entities {
			in.Data.add(s.ID, w.Blueprint)
		}
		for _, b := range s.Writes.Blueprints {
			in.Blueprints.add(s.ID, b.ID)
		}
		for _, a := range s.Writes.Actions {
			in.Actions.add(s.ID, a.ID)
		}
	}
	for _, e := range []*Evidence{&in.Data, &in.Blueprints, &in.Actions} {
		e.sort()
	}
	return in
}

// Reports says whether the hub said what its sources wrote: a list in
// which no source carries writes comes from a hub too old to say, and
// then every integration is unknown, not empty.
func Reports(sources []Source) bool {
	return slices.ContainsFunc(sources, func(s Source) bool { return s.Writes != nil })
}

// WithSources sets the sources on the landscape and each card's
// integration. known false, from a failed read or a hub that does not
// say, leaves every card's integration unknown.
func WithSources(l Landscape, sources []Source, known bool) Landscape {
	known = known && Reports(sources)
	l.SourcesKnown = known
	l.Sources = slices.Clone(sources)
	slices.SortFunc(l.Sources, func(x, y Source) int { return cmp.Compare(x.ID, y.ID) })
	cards := slices.Clone(l.Cards)
	for i := range cards {
		cards[i].Integration = nil
		if known {
			in := IntegrationOf(cards[i], l.Sources, l.Cards)
			cards[i].Integration = &in
		}
	}
	l.Cards = cards
	return l
}
