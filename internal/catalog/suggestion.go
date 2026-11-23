package catalog

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/orchard-suite/platformkit/orrery"
)

// EntraApp is what Orrery's Entra pusher says about one enterprise app.
type EntraApp struct {
	// Blueprint is the hub's blueprint it is read from.
	Blueprint              string
	Key, AppID, Name, Icon string
	LaunchURL, Portal      string
	// Kind is how people sign in: oidc, saml, or password.
	Kind, Publisher string
	// Required is true when Entra lets only assigned people sign in.
	Required bool
	// Roles are its app roles by value; RoleTitles their names.
	Roles, RoleTitles []string
	Seen              bool
}

// EntraApps reads entra_app entities, by name.
func EntraApps(records []orrery.Record) []EntraApp {
	out := make([]EntraApp, 0, len(records))
	for _, r := range records {
		if r.Hidden || r.Key == "" {
			continue
		}
		required, set := r.Bool("assignment_required")
		out = append(out, EntraApp{
			Blueprint: cmp.Or(r.Blueprint, EntraBlueprint), Key: r.Key, AppID: cmp.Or(r.String("app_id"), r.Key[strings.LastIndex(r.Key, "/")+1:]),
			Name: cmp.Or(r.Title, r.Key), Icon: r.Icon,
			LaunchURL: r.String("launch_url"), Portal: r.String("portal"),
			Kind: r.String("sign_in"), Publisher: r.String("publisher"),
			// An app Orrery does not say is open is treated as closed.
			Required: !set || required,
			Roles:    r.Strings("roles"), RoleTitles: r.Strings("role_titles"),
			Seen: r.State == "" || r.State == "active",
		})
	}
	slices.SortStableFunc(out, func(x, y EntraApp) int { return cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)) })
	return out
}

// RoleTitle is the name of the app's role i, else its value.
func (e EntraApp) RoleTitle(i int) string {
	if i < len(e.RoleTitles) && e.RoleTitles[i] != "" {
		return e.RoleTitles[i]
	}
	return e.Roles[i]
}

// Suggestion is something a source found that may be an application, that
// no application is paired with and no admin has dismissed: an Entra app, a
// workload with an address, an open-source project the organization runs,
// or an outside product it uses.
type Suggestion struct {
	// Ref is its id in Orrery, blueprint:key.
	Ref  string
	Kind SuggestionKind
	Key  string
	Name string
	Icon string
	URL  string
	// About is what the source says it is: a description, or an Entra
	// app's sign-in kind and publisher.
	About string
	// Portal is where an Entra app is managed.
	Portal string
	// Required is true for an Entra app only assigned people may open.
	Required bool
	// Links are the source's relations, by name, so a new application
	// keeps where the source is placed, such as its pillars.
	Links map[string][]string
}

// SuggestionKind is the source a suggestion comes from.
type SuggestionKind string

const (
	FromEntra    SuggestionKind = "entra"
	FromWorkload SuggestionKind = "workload"
	// FromService is an open-source project the organization runs: a
	// sw_service of origin adopted.
	FromService SuggestionKind = "service"
	// FromAPI is an outside product the organization uses: an external
	// sw_api.
	FromAPI SuggestionKind = "api"
)

// Words are how the admin page names the kind.
func (k SuggestionKind) Words() string {
	switch k {
	case FromEntra:
		return "Entra app"
	case FromService:
		return "open source"
	case FromAPI:
		return "outside product"
	}
	return "workload"
}

// Heading names a group of suggestions of this kind on the admin page.
func (k SuggestionKind) Heading() string {
	switch k {
	case FromEntra:
		return "In Entra"
	case FromService:
		return "Open source"
	case FromAPI:
		return "Outside products"
	}
	return "Workloads"
}

// SuggestionGroup is the suggestions of one kind.
type SuggestionGroup struct {
	Kind  SuggestionKind
	Items []Suggestion
}

// GroupSuggestions splits suggestions by kind: Entra apps, workloads, open
// source, then outside products, each keeping its order, none empty.
func GroupSuggestions(all []Suggestion) []SuggestionGroup {
	var out []SuggestionGroup
	for _, k := range []SuggestionKind{FromEntra, FromWorkload, FromService, FromAPI} {
		g := SuggestionGroup{Kind: k}
		for _, s := range all {
			if s.Kind == k {
				g.Items = append(g.Items, s)
			}
		}
		if len(g.Items) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// Hosting is where an application made from the suggestion runs, when the
// source says.
func (k SuggestionKind) Hosting() Hosting {
	switch k {
	case FromWorkload, FromService:
		return SelfHosted
	case FromAPI:
		return SaaS
	}
	return ""
}

// AdoptedOrigin is the origin of a sw_service the organization runs but did
// not build.
const AdoptedOrigin = "adopted"

// Found is what the sources hold that may be an application, besides the
// Entra apps: workloads, open-source services, and outside APIs.
type Found struct {
	Workloads, Services, APIs []orrery.Record
}

// SuggestionsOf lists what no application is paired with and nobody
// dismissed: Entra apps, workloads with an address, open-source services,
// then outside APIs, each by name. Services built here are never
// suggested: they are what an application is built from, not one.
func SuggestionsOf(apps []Application, entra []EntraApp, found Found, ignored []string) []Suggestion {
	// An application names what it is paired with by key alone; a dismissal
	// names it by its id in the hub.
	paired := map[SuggestionKind]map[string]bool{FromEntra: {}, FromService: {}, FromWorkload: {}, FromAPI: {}}
	for _, a := range apps {
		for _, k := range a.SignIn {
			paired[FromEntra][k] = true
		}
		for _, k := range a.Services {
			paired[FromService][k] = true
		}
		paired[FromWorkload][a.Workload] = true
		paired[FromAPI][a.API] = true
	}
	dismissed := make(map[string]bool, len(ignored))
	for _, ref := range ignored {
		dismissed[ref] = true
	}
	taken := func(kind SuggestionKind, ref, key string) bool { return paired[kind][key] || dismissed[ref] }
	var out []Suggestion
	for _, e := range entra {
		ref := e.Blueprint + ":" + e.Key
		if taken(FromEntra, ref, e.Key) || !e.Seen {
			continue
		}
		about := strings.TrimSpace(strings.Join(slices.DeleteFunc([]string{e.Kind, e.Publisher}, func(s string) bool { return s == "" }), ", "))
		out = append(out, Suggestion{Ref: ref, Kind: FromEntra, Key: e.Key, Name: e.Name, Icon: e.Icon, URL: e.LaunchURL, About: about, Portal: e.Portal, Required: e.Required})
	}
	from := func(r orrery.Record, kind SuggestionKind, url string) Suggestion {
		return Suggestion{Ref: r.Blueprint + ":" + r.Key, Kind: kind, Key: r.Key, Name: cmp.Or(r.Title, r.Key), Icon: r.Icon, URL: url, About: strings.TrimSpace(r.String("description")), Links: linksOf(r)}
	}
	for _, w := range found.Workloads {
		if !taken(FromWorkload, w.Blueprint+":"+w.Key, w.Key) && !w.Hidden && w.String("url") != "" {
			out = append(out, from(w, FromWorkload, w.String("url")))
		}
	}
	var more []Suggestion
	for _, s := range found.Services {
		if !taken(FromService, s.Blueprint+":"+s.Key, s.Key) && !s.Hidden && s.String("origin") == AdoptedOrigin {
			more = append(more, from(s, FromService, ""))
		}
	}
	for _, a := range found.APIs {
		if external, _ := a.Bool("external"); external && !taken(FromAPI, a.Blueprint+":"+a.Key, a.Key) && !a.Hidden {
			more = append(more, from(a, FromAPI, ""))
		}
	}
	slices.SortStableFunc(more, func(x, y Suggestion) int { return cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)) })
	return append(out, more...)
}

// Blueprint is the blueprint of the source the suggestion comes from.
func (s Suggestion) Blueprint() string {
	bp, _, _ := strings.Cut(s.Ref, ":")
	return bp
}

// Details are a new application made from the suggestion: its name, logo,
// address, where it runs, and its source paired with it. lenses names, per
// lens blueprint, the application's relation to it; the application is
// placed wherever the source is placed on each, so an open-source service
// keeps its pillars and its business capabilities.
func (s Suggestion) Details(blueprints []Blueprint, lenses map[string]string) AppDetails {
	d := AppDetails{Name: s.Name, Icon: s.Icon, URL: s.URL, Hosting: s.Kind.Hosting(), Listed: Shown, Placed: map[string][]string{}}
	switch s.Kind {
	case FromEntra:
		d.SignIn = []string{s.Key}
		d.Access = AccessOpen
		if s.Required {
			d.Access = AccessDirect
		}
	case FromWorkload:
		d.Workload = s.Key
	case FromService:
		d.Services = []string{s.Key}
	case FromAPI:
		d.API = s.Key
	}
	if s.Kind != FromEntra {
		d.Description = s.About
	}
	for lens, rel := range lenses {
		var keys []string
		for _, from := range RelationsTo(blueprints, s.Blueprint(), lens) {
			for _, k := range s.Links[from] {
				if !slices.Contains(keys, k) {
					keys = append(keys, k)
				}
			}
		}
		if len(keys) == 0 {
			continue
		}
		if lens == CapabilityBlueprint {
			d.Capabilities = keys
		} else {
			d.Placed[rel] = keys
		}
	}
	return d
}

// IgnoredBlueprint is My Apps' record of a suggestion an admin dismissed;
// the key is the suggestion's ref.
const IgnoredBlueprint = "myapps_ignored"

// IgnoredEntity is a dismissal as My Apps writes it.
func IgnoredEntity(ref, by string, at time.Time) orrery.Entity {
	return orrery.Entity{Blueprint: IgnoredBlueprint, Key: ref, Title: ref, Fields: map[string]any{"by": by, "at": at.UTC().Format(time.RFC3339)}}
}
