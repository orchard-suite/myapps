package server

import (
	"cmp"
	"net/http"
	"slices"
	"strings"

	"github.com/orchard-suite/myapps/internal/catalog"
	"github.com/orchard-suite/platformkit/door"
	"github.com/orchard-suite/platformkit/orrery"
)

// The Sources tab: where My Apps reads each kind of thing in this hub. The
// rules, and reading through a mapping, are catalog's; this only lays the
// choices out and saves an admin's pick.

// partView is one part of a source as the tab shows it: the hub's name for
// it, the names it may pick from, and how the hub words each value.
type partView struct {
	Part    catalog.Part
	Name    string
	Options []string
	Values  []valueView
}

// valueView is one value My Apps acts on and the hub's wording of it.
type valueView struct {
	Value, Hub string
}

// mappingView is one kind of thing and where it is read.
type mappingView struct {
	Spec      catalog.MappingSpec
	Blueprint string
	// OnApps is true for assignments kept on the Entra apps.
	OnApps bool
	Parts  []partView
}

// mappingViews lay every kind out against the hub's blueprints.
func mappingViews(m catalog.Mappings, blueprints []catalog.Blueprint) []mappingView {
	byID := make(map[string]catalog.Blueprint, len(blueprints))
	for _, b := range blueprints {
		byID[b.ID] = b
	}
	names := func(set map[string]catalog.BlueprintBit) []string {
		out := make([]string, 0, len(set))
		for n := range set {
			out = append(out, n)
		}
		slices.Sort(out)
		return out
	}
	views := make([]mappingView, 0, len(catalog.MappingSpecs))
	for _, spec := range catalog.MappingSpecs {
		src := m[spec.Kind]
		v := mappingView{Spec: spec, Blueprint: src.Blueprint, OnApps: spec.Kind == catalog.MapAssignments && src.Blueprint == ""}
		bp, held := byID[src.Blueprint]
		if v.OnApps {
			bp, held = byID[m.Blueprint(catalog.MapEntraApps)]
		}
		for _, p := range spec.Parts {
			if v.OnApps && p.Name == "app" {
				continue
			}
			pv := partView{Part: p, Name: src.Name(p.Name)}
			if held {
				if p.Relation {
					pv.Options = names(bp.Relations)
				} else {
					pv.Options = names(bp.Fields)
				}
			}
			for _, val := range p.Values {
				pv.Values = append(pv.Values, valueView{Value: val, Hub: src.Value(p.Name, val)})
			}
			v.Parts = append(v.Parts, pv)
		}
		views = append(views, v)
	}
	return views
}

// pageMappings shows where My Apps reads each kind of thing.
func (s *Server) pageMappings(w http.ResponseWriter, r *http.Request) {
	snap := s.cache.Get(r.Context())
	blueprints := s.coverage.Get(r.Context()).Value.Blueprints
	ids := make([]catalog.Blueprint, len(blueprints))
	copy(ids, blueprints)
	slices.SortFunc(ids, func(x, y catalog.Blueprint) int {
		return cmp.Compare(strings.ToLower(cmp.Or(x.Title, x.ID)), strings.ToLower(cmp.Or(y.Title, y.ID)))
	})
	q := r.URL.Query()
	s.render(w, r, http.StatusOK, "admin", map[string]any{
		"Tab":        "sources",
		"Mappings":   mappingViews(snap.Value.Mappings, blueprints),
		"Blueprints": ids,
		"Writable":   s.writable(),
		"Done":       q.Get("done"),
		"Failed":     q.Get("failed"),
		"Catalog":    catalogState(snap),
	})
}

// saveMapping saves where My Apps reads one kind of thing. A new blueprint
// takes the parts named alike in it; the admin picks the rest once its
// fields show. Applications are then offered the relation to it, which
// the hub refuses to point anywhere else once it exists.
func (s *Server) saveMapping(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.message(w, r, http.StatusBadRequest, "That did not work", "The form could not be read.")
		return
	}
	kind := catalog.MappingKind(r.PathValue("kind"))
	spec, known := catalog.SpecOf(kind)
	if !known {
		s.message(w, r, http.StatusNotFound, "No such source", "My Apps reads no such kind of thing.")
		return
	}
	d := s.cache.Get(r.Context()).Value
	blueprints := s.coverage.Get(r.Context()).Value.Blueprints
	before := d.Mappings[kind]
	next := catalog.Mapping{Blueprint: strings.TrimSpace(r.PostForm.Get("blueprint")), Names: map[string]string{}, Values: map[string]map[string]string{}}
	moved := next.Blueprint != before.Blueprint
	held := blueprintOf(blueprints, next.Blueprint)
	if kind == catalog.MapAssignments && next.Blueprint == "" {
		held = blueprintOf(blueprints, d.Mappings.Blueprint(catalog.MapEntraApps))
	}
	for _, p := range spec.Parts {
		name := strings.TrimSpace(r.PostForm.Get("part." + p.Name))
		if moved {
			// The old blueprint's names mean nothing here: keep a part
			// named alike, and leave the rest for the admin to pick.
			name = ""
			if held != nil && (has(held.Fields, p.Name) && !p.Relation || has(held.Relations, p.Name) && p.Relation) {
				name = p.Name
			}
		}
		if name != p.Name {
			next.Names[p.Name] = name
		}
		for _, v := range p.Values {
			if got := strings.TrimSpace(r.PostForm.Get("value." + p.Name + "." + v)); got != "" && got != v {
				if next.Values[p.Name] == nil {
					next.Values[p.Name] = map[string]string{}
				}
				next.Values[p.Name][v] = got
			}
		}
	}
	all := cloneMappings(d.Mappings)
	all[kind] = next
	check := all.Check(kind, next, blueprints)
	if kind == catalog.MapAssignments && next.Blueprint == "" {
		check = all.CheckOnApps(next, blueprints)
	}
	if check != nil {
		s.message(w, r, http.StatusBadRequest, "That did not work", "Not saved: "+check.Error()+".")
		return
	}
	if l, linked := catalog.LinkedTo(kind); linked && next.Blueprint != "" {
		if app := blueprintOf(blueprints, catalog.ApplicationBlueprint); app != nil {
			if rel, exists := app.Relations[l.Relation]; exists && rel.Target != next.Blueprint {
				s.message(w, r, http.StatusConflict, "That did not work", "Not saved: applications already link to "+rel.Target+" through "+l.Relation+", and Orrery keeps a relation's target. Change it in Orrery first.")
				return
			}
		}
	}
	err := s.curator.PutMapping(r.Context(), kind, next, door.From(r.Context()).Email)
	if err == nil {
		err = s.curator.Offer(r.Context(), applicationBlueprint(all))
	}
	s.logChange(r, "mapping saved", string(kind), err)
	s.cache.Invalidate()
	s.coverage.Invalidate()
	s.back(w, r, "/admin/sources", ok(err), failed(err))
}

func has(set map[string]catalog.BlueprintBit, name string) bool {
	_, ok := set[name]
	return ok
}

func blueprintOf(blueprints []catalog.Blueprint, id string) *catalog.Blueprint {
	i := slices.IndexFunc(blueprints, func(b catalog.Blueprint) bool { return b.ID == id })
	if id == "" || i < 0 {
		return nil
	}
	return &blueprints[i]
}

// cloneMappings copies the mappings, so a change can be checked before it
// is kept.
func cloneMappings(m catalog.Mappings) catalog.Mappings {
	out := make(catalog.Mappings, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// applicationBlueprint is the application blueprint as the mappings link
// it.
func applicationBlueprint(m catalog.Mappings) orrery.Blueprint {
	for _, b := range catalog.CurationBlueprints(m) {
		if b["id"] == catalog.ApplicationBlueprint {
			return b
		}
	}
	return nil
}
