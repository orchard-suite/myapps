package server

import (
	"cmp"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/orchard-suite/platformkit/door"

	"github.com/orchard-suite/myapps/internal/catalog"
)

// pageAdmin is the applications: what the sources suggest first, then
// every application in one grid, each cell edited where it is.
func (s *Server) pageAdmin(w http.ResponseWriter, r *http.Request) {
	snap := s.cache.Get(r.Context())
	d := snap.Value
	cov := s.coverage.Get(r.Context()).Value
	q := r.URL.Query()
	sheet := "apps"
	if q.Get("sheet") == "waiting" {
		sheet = "waiting"
	}
	data := map[string]any{
		"Tab":          "apps",
		"Sheet":        sheet,
		"Fill":         true,
		"Applications": d.Applications,
		"Suggestions":  d.Suggestions,
		"Tree":         treeOf(d.Capabilities),
		"Writable":     s.writable(),
		"Done":         q.Get("done"),
		"Failed":       q.Get("failed"),
		"Catalog":      catalogState(snap),
	}
	if sheet == "apps" {
		data["Grid"] = s.gridOf(d, cov)
	}
	s.render(w, r, http.StatusOK, "admin", data)
}

// option is one choice in a picker: a key and the words for it.
type option struct{ Key, Name string }

// servicesOf are the services an application may be built from, by name.
func servicesOf(c catalog.Coverage) []option {
	out := make([]option, 0, len(c.Services))
	for _, s := range c.Services {
		if !s.Hidden {
			out = append(out, option{s.Key, cmp.Or(s.Title, s.Key)})
		}
	}
	slices.SortFunc(out, func(x, y option) int { return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)) })
	return out
}

// externalAPIs are the outside APIs a SaaS application may be.
func externalAPIs(c catalog.Coverage) []option {
	var out []option
	for _, a := range c.APIs {
		if ext, _ := a.Bool("external"); ext && !a.Hidden {
			out = append(out, option{a.Key, cmp.Or(a.Title, a.Key)})
		}
	}
	slices.SortFunc(out, func(x, y option) int { return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)) })
	return out
}

// placing is one lens beyond business capabilities an application can be
// placed on, through its relation, with the lens's top categories.
type placing struct {
	Lens       catalog.Lens
	Relation   string
	Categories []catalog.Category
}

// placingsOf are the lenses an application's form places it on: every lens
// whose relation applications already have.
func placingsOf(c catalog.Coverage) []placing {
	var out []placing
	for _, l := range c.Lenses {
		if l.Blueprint == catalog.CapabilityBlueprint {
			continue
		}
		rel, ok := catalog.LensRelation(c.Blueprints, l.Blueprint)
		if !ok {
			continue
		}
		out = append(out, placing{Lens: l, Relation: rel, Categories: catalog.Categories(c.Categories[l.Blueprint])})
	}
	return out
}

// lensRelations names, per lens blueprint, the relation an application is
// placed on it with: business capabilities, and every lens the form shows.
func lensRelations(c catalog.Coverage) map[string]string {
	out := map[string]string{catalog.CapabilityBlueprint: catalog.CapabilityRelation}
	for _, p := range placingsOf(c) {
		out[p.Lens.Blueprint] = p.Relation
	}
	return out
}

// lensRelationNames are the relations of every lens beyond business
// capabilities that applications can be placed on.
func lensRelationNames(c catalog.Coverage) []string {
	var out []string
	for _, p := range placingsOf(c) {
		out = append(out, p.Relation)
	}
	return out
}

// writable says whether a change can be saved: there is somewhere to write
// it and an origin to check its page against.
func (s *Server) writable() bool { return s.curator != nil && s.cfg.PublicURL != "" }

// sameOrigin takes a change only from the portal's own pages.
func (s *Server) sameOrigin(next http.Handler) http.Handler {
	if !s.writable() {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.log.WarnContext(r.Context(), "change refused: the admin pages only read here", "curator", s.curator != nil, "public_url", s.cfg.PublicURL != "")
			s.message(w, r, http.StatusServiceUnavailable, "Changes are off", "This portal cannot save changes: it has no push token or no PUBLIC_URL.")
		})
	}
	return door.SameOrigin(strings.TrimRight(s.cfg.PublicURL, "/"), s.log, next)
}

// saveApplication adds an application (no key in the path) or changes
// one. Only the fields the request names change; the rest keep their
// values, so the grid sends one cell at a time. A list field is its values
// repeated, or one value with commas between; present and empty clears it.
// Every pick is checked against what the catalog holds. A request that
// asks for JSON gets the saved row back, or what was wrong.
func (s *Server) saveApplication(w http.ResponseWriter, r *http.Request) {
	asJSON := wantsJSON(r)
	if err := r.ParseForm(); err != nil {
		s.refuseChange(w, r, asJSON, http.StatusBadRequest, "The form could not be read.")
		return
	}
	d := s.cache.Get(r.Context()).Value
	cov := s.coverage.Get(r.Context()).Value
	key := r.PathValue("key")
	var det catalog.AppDetails
	if key != "" {
		i := slices.IndexFunc(d.Applications, func(a catalog.Application) bool { return a.Key == key })
		if i < 0 {
			s.refuseChange(w, r, asJSON, http.StatusNotFound, "There is no application by that name.")
			return
		}
		det = detailsOf(d.Applications[i], lensRelationNames(cov))
	}
	if msg := applyForm(&det, r.PostForm, d, cov); msg != "" {
		s.refuseChange(w, r, asJSON, http.StatusBadRequest, msg)
		return
	}
	if key == "" {
		key = catalog.ApplicationKey(det.Name, d.Applications)
	}
	det.By, det.At = door.From(r.Context()).Email, time.Now()
	det.Into = d.Mappings
	err := s.curator.SaveApplication(r.Context(), key, det)
	s.logChange(r, "application saved", key, err)
	s.cache.Invalidate()
	s.coverage.Invalidate()
	if !asJSON {
		s.back(w, r, "/admin", ok(err), failed(err))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Not saved. Orrery refused or could not be reached. Try again."})
		return
	}
	// Read again now, so the next change starts from this one and the
	// row's logo is one the cache holds.
	d = s.cache.Get(r.Context()).Value
	writeJSON(w, http.StatusOK, map[string]any{"row": s.rowOf(key, det, d)})
}

// applyForm sets each field the form names on an application, checked
// against the catalog. It answers what was wrong, or nothing.
func applyForm(det *catalog.AppDetails, f url.Values, d catalog.Directory, cov catalog.Coverage) string {
	text := func(name string) (string, bool) { return strings.TrimSpace(f.Get(name)), f.Has(name) }
	list := func(name string) ([]string, bool) {
		var out []string
		for _, v := range f[name] {
			for _, part := range splitList(v) {
				if !slices.Contains(out, part) {
					out = append(out, part)
				}
			}
		}
		return out, f.Has(name)
	}
	known := func(keys []string, in func(string) bool) bool {
		return !slices.ContainsFunc(keys, func(k string) bool { return !in(k) })
	}
	if v, ok := text(colName); ok {
		det.Name = v
	}
	if det.Name == "" || len(det.Name) > 80 {
		return "Give the application a name of 80 characters or fewer."
	}
	if v, ok := text(colURL); ok {
		if v != "" && !httpsURL(v) {
			return "The address must start with https://."
		}
		det.URL = v
	}
	if v, ok := text(colDescription); ok {
		if len(v) > 200 {
			return "Keep the description to 200 characters."
		}
		det.Description = v
	}
	if v, ok := text(colIcon); ok {
		if v != "" && !catalog.IconName(v) {
			return "An icon is a name such as grafana: lowercase letters, digits, and underscores."
		}
		det.Icon = v
	}
	if v, ok := text(colHosting); ok {
		if v != "" && !slices.Contains(catalog.Hostings, catalog.Hosting(v)) {
			return "Pick where it runs."
		}
		det.Hosting = catalog.Hosting(v)
	}
	if v, ok := text(colSelfBuilt); ok {
		if v != selfBuiltYes && v != selfBuiltNo {
			return "Say whether we built it."
		}
		det.SelfBuilt = v == selfBuiltYes
	}
	if v, ok := text(colLifecycle); ok {
		if v != "" && !slices.Contains(catalog.Lifecycles, v) {
			return "Pick a lifecycle."
		}
		det.Lifecycle = v
	}
	if v, ok := text(colTier); ok {
		if v != "" && !slices.Contains(catalog.Tiers, v) {
			return "Pick a tier."
		}
		det.Tier = v
	}
	if v, ok := text(colListed); ok {
		l := catalog.Listed(cmp.Or(v, string(catalog.Shown)))
		if !slices.Contains(catalog.Listeds, l) {
			return "Pick whether My Apps shows it."
		}
		det.Listed = l
	}
	if v, ok := text(colAccess); ok {
		if v != "" && !slices.Contains(catalog.AccessModes, catalog.AccessMode(v)) {
			return "Pick how people get it."
		}
		det.Access = catalog.AccessMode(v)
	}
	if v, ok := text(colWorkload); ok {
		if v != "" && !slices.ContainsFunc(d.Workloads, func(c catalog.Category) bool { return c.Key == v }) {
			return "There is no workload by that name."
		}
		det.Workload = v
	}
	if v, ok := text(colAPI); ok {
		if v != "" && !slices.ContainsFunc(externalAPIs(cov), func(o option) bool { return o.Key == v }) {
			return "There is no outside API by that name."
		}
		det.API = v
	}
	if v, ok := text(colTeam); ok {
		if v != "" && !slices.ContainsFunc(d.Teams, func(c catalog.Category) bool { return c.Key == v }) {
			return "There is no team by that name."
		}
		det.Team = v
	}
	if v, ok := list(colAudience); ok {
		det.Audience = v
	}
	if v, ok := list(colPeople); ok {
		people := make([]string, 0, len(v))
		for _, p := range v {
			e, ok := catalog.Email(p)
			if !ok {
				return "Name each person by their email, with commas between."
			}
			if !slices.Contains(people, e) {
				people = append(people, e)
			}
		}
		det.People = people
	}
	if v, ok := list(colServices); ok {
		if !known(v, func(k string) bool {
			return slices.ContainsFunc(servicesOf(cov), func(o option) bool { return o.Key == k })
		}) {
			return "Some services named are not in the catalog. Name each by its key."
		}
		det.Services = v
	}
	if v, ok := list(colCapability); ok {
		if !known(v, func(k string) bool {
			return slices.ContainsFunc(d.Capabilities, func(c catalog.Category) bool { return c.Key == k })
		}) {
			return "There is no business capability by that name."
		}
		det.Capabilities = v
	}
	for _, p := range placingsOf(cov) {
		v, ok := list(placePrefix + p.Relation)
		if !ok {
			continue
		}
		if !known(v, func(k string) bool {
			return slices.ContainsFunc(p.Categories, func(c catalog.Category) bool { return c.Key == k })
		}) {
			return "There is no " + p.Lens.Title + " category by that name."
		}
		if det.Placed == nil {
			det.Placed = map[string][]string{}
		}
		det.Placed[p.Relation] = v
	}
	if v, ok := list(colSignIn); ok {
		if !known(v, func(k string) bool {
			return slices.ContainsFunc(d.EntraApps, func(e catalog.EntraApp) bool { return e.Key == k })
		}) {
			return "There is no Entra app by that name."
		}
		det.SignIn = v
	}
	// The role and the groups hang on the Entra apps: a change of those
	// keeps only the ones still theirs, and a pick must be one of them.
	roles := rolesOf(det.SignIn, d.EntraApps)
	isRole := func(k string) bool { return slices.ContainsFunc(roles, func(o option) bool { return o.Key == k }) }
	if v, ok := text(colRole); ok {
		if v != "" && !isRole(v) {
			return "That role is not one of its first Entra app's."
		}
		det.AccessRole = v
	} else if det.AccessRole != "" && !isRole(det.AccessRole) {
		det.AccessRole = ""
	}
	groups := groupChoices(det.SignIn, d.Access)
	isGroup := func(k string) bool {
		return slices.ContainsFunc(groups, func(g catalog.EntraGroup) bool { return g.Key == k && groupPickable(det.SignIn, g) })
	}
	if v, ok := list(colGroup); ok {
		if !known(v, isGroup) {
			if len(det.SignIn) == 0 {
				return "There is no Entra group by that name."
			}
			return "A group request can add people only to its Entra apps' groups that Entra does not fill."
		}
		det.AccessGroups = v
	} else {
		det.AccessGroups = slices.DeleteFunc(slices.Clone(det.AccessGroups), func(k string) bool { return !isGroup(k) })
	}
	return ""
}

// wantsJSON is whether the page's script asked, rather than a form.
func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

// refuseChange says why a change was not taken: as JSON to the page's
// script, else as a page.
func (s *Server) refuseChange(w http.ResponseWriter, r *http.Request, asJSON bool, status int, msg string) {
	s.log.InfoContext(r.Context(), "change refused", "path", r.URL.Path, "status", status, "reason", msg)
	if asJSON {
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	title := "That did not work"
	if status == http.StatusNotFound {
		title = "No such application"
	}
	s.message(w, r, status, title, msg)
}

// splitList reads a comma or newline separated list.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// deleteApplication removes an application. Its tile goes with it.
func (s *Server) deleteApplication(w http.ResponseWriter, r *http.Request) {
	asJSON := wantsJSON(r)
	key := r.PathValue("key")
	if !slices.ContainsFunc(s.cache.Get(r.Context()).Value.Applications, func(a catalog.Application) bool { return a.Key == key }) {
		s.refuseChange(w, r, asJSON, http.StatusNotFound, "There is no application by that name.")
		return
	}
	err := s.curator.DeleteApplication(r.Context(), key)
	s.logChange(r, "application removed", key, err)
	s.cache.Invalidate()
	s.coverage.Invalidate()
	switch {
	case !asJSON:
		s.back(w, r, "/admin", ok(err), failed(err))
	case err != nil:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Not removed. Orrery refused or could not be reached. Try again."})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"removed": key})
	}
}

// attach pairs a suggestion with an application: an Entra app becomes one
// it signs in through, a workload the one it is deployed as, an open-source
// service one it runs, an outside API the one it is.
func (s *Server) attach(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.message(w, r, http.StatusBadRequest, "That did not work", "The form could not be read.")
		return
	}
	d := s.cache.Get(r.Context()).Value
	ref, key := r.PostForm.Get("ref"), r.PostForm.Get("application")
	si := slices.IndexFunc(d.Suggestions, func(x catalog.Suggestion) bool { return x.Ref == ref })
	ai := slices.IndexFunc(d.Applications, func(a catalog.Application) bool { return a.Key == key })
	if si < 0 || ai < 0 {
		s.message(w, r, http.StatusBadRequest, "That did not work", "Pick a suggestion and an application.")
		return
	}
	a, sug := d.Applications[ai], d.Suggestions[si]
	det := detailsOf(a, nil)
	switch sug.Kind {
	case catalog.FromEntra:
		det.SignIn = append(det.SignIn, sug.Key)
	case catalog.FromWorkload:
		det.Workload = sug.Key
	case catalog.FromService:
		det.Services = append(slices.Clone(det.Services), sug.Key)
	case catalog.FromAPI:
		det.API = sug.Key
	}
	// An application with no logo of its own, or only a blueprint's, takes
	// the logo of what it is paired with.
	if sug.Icon != "" && (det.Icon == "" || catalog.GenericIcon(det.Icon, s.coverage.Get(r.Context()).Value.Blueprints)) {
		det.Icon = sug.Icon
	}
	det.By, det.At = door.From(r.Context()).Email, time.Now()
	det.Into = d.Mappings
	err := s.curator.SaveApplication(r.Context(), a.Key, det)
	s.logChange(r, "suggestion attached", ref, err)
	s.cache.Invalidate()
	s.back(w, r, waiting, ok(err), failed(err))
}

// detailsOf is an application as it would be written back, so a change
// to one part keeps the rest. It carries its place on each lens relation
// named.
func detailsOf(a catalog.Application, lensRelations []string) catalog.AppDetails {
	det := catalog.AppDetails{
		Name: a.Name, Icon: a.Icon, Description: a.Description, URL: a.URL, Hosting: a.Hosting, SelfBuilt: a.SelfBuilt, Lifecycle: a.Lifecycle, Tier: a.Tier,
		Listed: a.Listed, Audience: a.Audience, Access: a.Access, AccessRole: a.AccessRole, SignIn: slices.Clone(a.SignIn),
		AccessGroups: a.AccessGroups, People: a.People, Services: a.Services, Workload: a.Workload, API: a.API, Team: a.Team, Capabilities: a.Capabilities,
	}
	if len(lensRelations) > 0 {
		det.Placed = make(map[string][]string, len(lensRelations))
		for _, rel := range lensRelations {
			det.Placed[rel] = slices.Clone(a.Links[rel])
		}
	}
	return det
}

// addSuggested makes an application of each suggestion picked, filled in
// from its source and placed where the source is, on the business
// capability picked too when there is one.
func (s *Server) addSuggested(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.message(w, r, http.StatusBadRequest, "That did not work", "The form could not be read.")
		return
	}
	d := s.cache.Get(r.Context()).Value
	cov := s.coverage.Get(r.Context()).Value
	capability := r.PostForm.Get("capability")
	if capability != "" && !slices.ContainsFunc(d.Capabilities, func(c catalog.Category) bool { return c.Key == capability }) {
		s.message(w, r, http.StatusBadRequest, "That did not work", "There is no business capability by that name.")
		return
	}
	lenses := lensRelations(cov)
	taken := slices.Clone(d.Applications)
	done, bad := 0, 0
	for _, ref := range slices.Compact(slices.Sorted(slices.Values(r.PostForm["ref"]))) {
		i := slices.IndexFunc(d.Suggestions, func(x catalog.Suggestion) bool { return x.Ref == ref })
		if i < 0 {
			bad++
			continue
		}
		det := d.Suggestions[i].Details(cov.Blueprints, lenses)
		if capability != "" && !slices.Contains(det.Capabilities, capability) {
			det.Capabilities = append(det.Capabilities, capability)
		}
		det.By, det.At = door.From(r.Context()).Email, time.Now()
		key := catalog.ApplicationKey(det.Name, taken)
		taken = append(taken, catalog.Application{Key: key})
		det.Into = d.Mappings
		err := s.curator.SaveApplication(r.Context(), key, det)
		s.logChange(r, "suggestion made an application", ref, err)
		done += ok(err)
		bad += failed(err)
	}
	s.cache.Invalidate()
	s.coverage.Invalidate()
	s.back(w, r, waiting, done, bad)
}

// ignore dismisses a suggestion: My Apps stops offering it.
func (s *Server) ignore(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.message(w, r, http.StatusBadRequest, "That did not work", "The form could not be read.")
		return
	}
	refs := slices.Compact(slices.Sorted(slices.Values(r.PostForm["ref"])))
	sugs := s.cache.Get(r.Context()).Value.Suggestions
	done, bad := 0, 0
	for _, ref := range refs {
		if !slices.ContainsFunc(sugs, func(x catalog.Suggestion) bool { return x.Ref == ref }) {
			bad++
			continue
		}
		err := s.curator.Ignore(r.Context(), ref, door.From(r.Context()).Email)
		s.logChange(r, "suggestion dismissed", ref, err)
		done += ok(err)
		bad += failed(err)
	}
	s.cache.Invalidate()
	s.back(w, r, waiting, done, bad)
}

// pageCapabilities is the tree of business capabilities, to add, rename,
// nest, reorder, and remove.
func (s *Server) pageCapabilities(w http.ResponseWriter, r *http.Request) {
	snap := s.cache.Get(r.Context())
	d := snap.Value
	// An app counts once on each capability it is placed on and on every
	// capability above those, so a parent shows what its tree holds.
	parent := map[string]string{}
	for _, c := range d.Capabilities {
		parent[c.Key] = c.Parent
	}
	used := map[string]int{}
	for _, a := range d.Applications {
		seen := map[string]bool{}
		for _, k := range a.Capabilities {
			for ; k != "" && !seen[k]; k = parent[k] {
				seen[k] = true
				used[k]++
			}
		}
	}
	q := r.URL.Query()
	s.render(w, r, http.StatusOK, "admin", map[string]any{
		"Tab":          "capabilities",
		"Capabilities": d.Capabilities,
		"Tree":         treeOf(d.Capabilities),
		"Used":         used,
		"Writable":     s.writable(),
		"Done":         q.Get("done"),
		"Failed":       q.Get("failed"),
		"Catalog":      catalogState(snap),
	})
}

// treeRow is one capability on the admin page, at its depth.
type treeRow struct {
	Category catalog.Category
	Depth    int
}

// treeOf lists the capabilities depth first, children after their parent,
// each level in order.
func treeOf(all []catalog.Category) []treeRow {
	var out []treeRow
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, c := range catalog.Children(all, parent) {
			out = append(out, treeRow{c, depth})
			if depth < 8 {
				walk(c.Key, depth+1)
			}
		}
	}
	walk("", 0)
	return out
}

// saveCapability adds a capability (no key in the path) or changes one: its
// name, parent, order, and description. A parent may not be the capability
// itself or anything under it.
func (s *Server) saveCapability(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.message(w, r, http.StatusBadRequest, "That did not work", "The form could not be read.")
		return
	}
	all := s.cache.Get(r.Context()).Value.Capabilities
	f := r.PostForm
	key := r.PathValue("key")
	c := catalog.Category{Name: strings.TrimSpace(f.Get("name")), Parent: strings.TrimSpace(f.Get("parent")), Description: strings.TrimSpace(f.Get("description"))}
	if c.Name == "" || len(c.Name) > 80 {
		s.message(w, r, http.StatusBadRequest, "That did not work", "Give the capability a name of 80 characters or fewer.")
		return
	}
	if c.Parent != "" && !slices.ContainsFunc(all, func(x catalog.Category) bool { return x.Key == c.Parent }) {
		s.message(w, r, http.StatusBadRequest, "That did not work", "There is no capability by that name to put it under.")
		return
	}
	if key == "" {
		c.Key = catalog.CapabilityKey(c.Name, c.Parent, all)
		c.Order = len(catalog.Children(all, c.Parent)) + 1
	} else {
		i := slices.IndexFunc(all, func(x catalog.Category) bool { return x.Key == key })
		if i < 0 {
			s.message(w, r, http.StatusNotFound, "No such capability", "There is no capability by that name.")
			return
		}
		c.Key = key
		c.Order = all[i].Order
		if n, err := strconv.Atoi(f.Get("order")); err == nil && n > 0 {
			c.Order = n
		}
		for p := c.Parent; p != ""; p = parentOf(all, p) {
			if p == key {
				s.message(w, r, http.StatusBadRequest, "That did not work", "A capability cannot be part of itself.")
				return
			}
		}
	}
	err := s.curator.PutCapability(r.Context(), c)
	s.logChange(r, "capability saved", c.Key, err)
	s.cache.Invalidate()
	s.coverage.Invalidate()
	s.back(w, r, "/admin/capabilities", ok(err), failed(err))
}

// deleteCapability removes a capability nothing is under.
func (s *Server) deleteCapability(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	all := s.cache.Get(r.Context()).Value.Capabilities
	if !slices.ContainsFunc(all, func(x catalog.Category) bool { return x.Key == key }) {
		s.message(w, r, http.StatusNotFound, "No such capability", "There is no capability by that name.")
		return
	}
	if len(catalog.Children(all, key)) > 0 {
		s.message(w, r, http.StatusBadRequest, "That did not work", "Move or remove the capabilities under it first.")
		return
	}
	err := s.curator.DeleteCapability(r.Context(), key)
	s.logChange(r, "capability removed", key, err)
	s.cache.Invalidate()
	s.coverage.Invalidate()
	s.back(w, r, "/admin/capabilities", ok(err), failed(err))
}

func parentOf(all []catalog.Category, key string) string {
	if i := slices.IndexFunc(all, func(x catalog.Category) bool { return x.Key == key }); i >= 0 {
		return all[i].Parent
	}
	return ""
}

// pageLenses lists the lenses and the hub's other classifications that
// could be one.
func (s *Server) pageLenses(w http.ResponseWriter, r *http.Request) {
	snap := s.coverage.Get(r.Context())
	c := snap.Value
	var candidates []catalog.Blueprint
	for _, b := range c.Blueprints {
		if b.Classification() && b.ID != catalog.CapabilityBlueprint && !slices.ContainsFunc(c.Lenses, func(l catalog.Lens) bool { return l.Blueprint == b.ID }) {
			candidates = append(candidates, b)
		}
	}
	slices.SortFunc(candidates, func(x, y catalog.Blueprint) int { return cmp.Compare(x.Title, y.Title) })
	q := r.URL.Query()
	s.render(w, r, http.StatusOK, "admin", map[string]any{
		"Tab":        "lenses",
		"Lenses":     c.Lenses,
		"Candidates": candidates,
		"Writable":   s.writable(),
		"Done":       q.Get("done"),
		"Failed":     q.Get("failed"),
		"Catalog":    catalogState(snap),
	})
}

// saveLens adds a classification as a lens, or moves one to an order. A new
// lens also gives applications a relation to place themselves on it.
func (s *Server) saveLens(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.message(w, r, http.StatusBadRequest, "That did not work", "The form could not be read.")
		return
	}
	c := s.coverage.Get(r.Context()).Value
	id := cmp.Or(r.PathValue("blueprint"), strings.TrimSpace(r.PostForm.Get("blueprint")))
	if id == catalog.CapabilityBlueprint || !slices.ContainsFunc(c.Blueprints, func(b catalog.Blueprint) bool { return b.ID == id && b.Classification() }) {
		s.message(w, r, http.StatusBadRequest, "That did not work", "That is not a classification in the hub.")
		return
	}
	order := len(c.Lenses)
	if n, err := strconv.Atoi(r.PostForm.Get("order")); err == nil && n > 0 {
		order = n
	}
	if rel, exists := catalog.LensRelation(c.Blueprints, id); !exists {
		err := s.curator.Offer(r.Context(), catalog.LensRelationOffer(rel, id, catalog.TitleOf(c.Blueprints, id)))
		s.logChange(r, "lens relation offered", rel, err)
		if err != nil {
			s.back(w, r, "/admin/lenses", 0, 1)
			return
		}
	}
	err := s.curator.PutLens(r.Context(), id, order)
	s.logChange(r, "lens saved", id, err)
	s.coverage.Invalidate()
	s.back(w, r, "/admin/lenses", ok(err), failed(err))
}

// deleteLens stops analysing a classification. The classification itself
// is untouched.
func (s *Server) deleteLens(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("blueprint")
	if !slices.ContainsFunc(s.coverage.Get(r.Context()).Value.Lenses, func(l catalog.Lens) bool { return l.Blueprint == id && id != catalog.CapabilityBlueprint }) {
		s.message(w, r, http.StatusNotFound, "No such lens", "That is not a lens you can remove.")
		return
	}
	err := s.curator.DeleteLens(r.Context(), id)
	s.logChange(r, "lens removed", id, err)
	s.coverage.Invalidate()
	s.back(w, r, "/admin/lenses", ok(err), failed(err))
}

// assess saves a new coverage assessment of one category of one lens.
func (s *Server) assess(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.message(w, r, http.StatusBadRequest, "That did not work", "The form could not be read.")
		return
	}
	f := r.PostForm
	c := s.coverage.Get(r.Context()).Value
	lens := c.Lens(f.Get("lens"))
	rating, good := catalog.RatingOf(f.Get("rating"))
	cats := catalog.Roots(catalog.Categories(c.Categories[lens.Blueprint]))
	if lens.Blueprint != f.Get("lens") || !good || !slices.ContainsFunc(cats, func(x catalog.Category) bool { return x.Key == f.Get("category") }) {
		s.message(w, r, http.StatusBadRequest, "That did not work", "Pick a lens, one of its categories, and a strength from 1 to 5.")
		return
	}
	a := catalog.Assessment{Lens: lens.Blueprint, Category: f.Get("category"), Rating: rating, Covers: strings.TrimSpace(f.Get("covers")), Missing: strings.TrimSpace(f.Get("missing")), By: door.From(r.Context()).Email, At: time.Now()}
	err := s.curator.PutAssessment(r.Context(), a)
	s.logChange(r, "assessment saved", a.Key(), err)
	s.coverage.Invalidate()
	q := url.Values{"lens": {lens.Blueprint}, "done": {strconv.Itoa(ok(err))}}
	if err != nil {
		q.Set("failed", "1")
	}
	http.Redirect(w, r, "/analytics?"+q.Encode()+"#ratings", http.StatusSeeOther)
}

func (s *Server) logChange(r *http.Request, what, key string, err error) {
	if err != nil {
		s.metrics.Change(r.Context(), what, "failed")
		s.log.ErrorContext(r.Context(), what+": failed", "key", key, "error", err)
		return
	}
	s.metrics.Change(r.Context(), what, "ok")
	s.log.InfoContext(r.Context(), what, "key", key)
}

func ok(err error) int {
	if err == nil {
		return 1
	}
	return 0
}

func failed(err error) int { return 1 - ok(err) }

// waiting is the admin's sheet of what the sources found.
const waiting = "/admin?sheet=waiting"

// back sends the admin back to a page, which may carry its own query, with
// what happened.
func (s *Server) back(w http.ResponseWriter, r *http.Request, page string, done, failed int) {
	path, query, _ := strings.Cut(page, "?")
	q, _ := url.ParseQuery(query)
	q.Set("done", strconv.Itoa(done))
	if failed > 0 {
		q.Set("failed", strconv.Itoa(failed))
	}
	http.Redirect(w, r, path+"?"+q.Encode(), http.StatusSeeOther)
}

func httpsURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
}
