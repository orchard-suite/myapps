// Package server answers on two ports, one per kind of caller:
//
//   - Session: the pages (apps, the landscape, coverage, admin, requests),
//     /v1/apps, /v1/landscape, and the icons. Trusts the front door's identity
//     headers, so only the front door may reach it.
//   - Open: /healthz and the browser telemetry relay. Trusts nothing and
//     identifies no one.
package server

import (
	"embed"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/orchard-suite/platformkit/door"
	"github.com/orchard-suite/platformkit/telemetry"

	"github.com/orchard-suite/myapps/internal/catalog"
	"github.com/orchard-suite/myapps/internal/graft"
)

// Service is the portal's name in its logs, spans, and metrics, and the
// prefix of every metric of its own.
const Service = "myapps"

//go:embed templates
var templateFS embed.FS

//go:embed assets
var assetFS embed.FS

// pages are parsed once; each server clones them to bind its icons.
var pages = map[string]*template.Template{}

func init() {
	for _, name := range []string{"apps", "landscape", "coverage", "admin", "requests", "message"} {
		pages[name] = template.Must(template.New("layout.html").Funcs(funcs).Funcs(template.FuncMap{"icon": func(string, string) string { return "" }}).ParseFS(templateFS, "templates/layout.html", "templates/"+name+".html"))
	}
}

// Config is what the server needs to know.
type Config struct {
	// Org is the name over the tiles, such as the company or the team.
	Org string
	// OrgIcon is the organization's logo: an icon name in Orrery, drawn in
	// place of My Apps' mark once the icon cache holds it.
	OrgIcon string
	// Version is the release running, shown on the page and in build_info.
	Version string
	// BrowserTelemetry says whether the page loads the Faro SDK. It is on
	// when FARO_URL is set, key or no key.
	BrowserTelemetry bool
	// PublicURL is the portal's own origin, such as
	// https://myapps.example.org. A decision on the admin page is taken
	// only from a page with this origin; empty, the admin page only reads.
	PublicURL string
	// OrreryURL is where people open Orrery, for its approvals list and
	// the request-access action; empty, the pages link to neither.
	OrreryURL string
	// SignOutURL is where the front door signs a person out of this app;
	// empty, the page offers no sign-out.
	SignOutURL string
}

// The front door's roles. Admin opens everything; Platform opens coverage
// and the lenses, for whoever watches the suite's coverage.
const (
	AdminRole    = "Admin"
	PlatformRole = "Platform"
)

// RequestActions are the Orrery actions a person asks for an application
// with, by how the application grants access.
var RequestActions = map[catalog.AccessMode]string{
	catalog.AccessDirect: "request_app_access",
	catalog.AccessGroup:  "request_group_access",
}

// Parts are what the server reads and writes through.
type Parts struct {
	// Tiles holds the capabilities, the tiles, the products, and who holds
	// what in Entra.
	Tiles *catalog.Cache[catalog.Directory]
	// Coverage holds what every lens draws from.
	Coverage *catalog.Cache[catalog.Coverage]
	// Actions may be nil, and then no page shows any.
	Actions *catalog.Actions
	// Icons holds the logos both caches fetch.
	Icons *catalog.Icons
	// Curator writes what admins decide; nil, the admin pages only read.
	Curator catalog.Curator
	// Desk is the request desk people report problems to; nil, the portal
	// offers no requests.
	Desk *graft.Client
	// Relay answers the browser's telemetry batches.
	Relay   http.Handler
	Metrics *Metrics
}

// Server holds the catalog and answers the pages.
type Server struct {
	cfg      Config
	cache    *catalog.Cache[catalog.Directory]
	coverage *catalog.Cache[catalog.Coverage]
	actions  *catalog.Actions
	icons    *catalog.Icons
	curator  catalog.Curator
	desk     *graft.Client
	pages    map[string]*template.Template
	relay    http.Handler
	metrics  *Metrics
	log      *slog.Logger
	gate     *door.Gate
	admin    *door.Gate
	platform *door.Gate
}

// New wires a server from its parts.
func New(cfg Config, p Parts, log *slog.Logger) (*Server, error) {
	if cfg.Org == "" {
		cfg.Org = "My Apps"
	}
	// Every signed-in person may open the portal; the tiles follow their roles.
	gate, err := door.NewGate(log)
	if err != nil {
		return nil, err
	}
	admin, err := door.NewGate(log, AdminRole)
	if err != nil {
		return nil, err
	}
	platform, err := door.NewGate(log, AdminRole, PlatformRole)
	if err != nil {
		return nil, err
	}
	cfg.OrreryURL = strings.TrimRight(cfg.OrreryURL, "/")
	icons := p.Icons
	s := &Server{cfg: cfg, cache: p.Tiles, coverage: p.Coverage, actions: p.Actions, icons: icons, curator: p.Curator, desk: p.Desk, pages: map[string]*template.Template{}, relay: p.Relay, metrics: p.Metrics, log: log, gate: gate, admin: admin, platform: platform}
	// A page draws a logo only for an icon held, and a monogram otherwise.
	for name, page := range pages {
		bound, err := page.Clone()
		if err != nil {
			return nil, err
		}
		s.pages[name] = bound.Funcs(template.FuncMap{"icon": func(name, theme string) string { return icons.URL(name, catalog.Theme(theme)) }})
	}
	gate.Refuse = s.refuse
	admin.Refuse = s.refuse
	platform.Refuse = s.refuse
	return s, nil
}

// refuse answers a request the gate turned away with the portal's own page.
func (s *Server) refuse(w http.ResponseWriter, r *http.Request, status int, message string) {
	title := "Sign in first"
	if status == http.StatusForbidden {
		title = "Not yours to open"
	}
	s.message(w, r, status, title, message)
}

// Session is the signed-in port: the pages, the JSON, and the tile opener.
func (s *Server) Session() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /{$}", s.gate.Func(s.pageApps))
	mux.Handle("GET /landscape", s.gate.Func(s.pageLandscape))
	mux.Handle("GET /suite", s.gate.Func(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/landscape", http.StatusMovedPermanently)
	}))
	mux.Handle("GET /analytics", s.platform.Func(s.pageAnalytics))
	mux.Handle("GET /coverage", s.platform.Func(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/analytics?"+r.URL.RawQuery, http.StatusMovedPermanently)
	}))
	mux.Handle("POST /analytics/assess", s.platform.Wrap(s.sameOrigin(http.HandlerFunc(s.assess))))
	mux.Handle("GET /v1/apps", s.gate.Func(s.apiApps))
	mux.Handle("GET /v1/landscape", s.gate.Func(s.apiLandscape))
	mux.Handle("GET /open/{key}", s.gate.Func(s.open))
	mux.Handle("GET /icons/{name}", s.gate.Func(s.icon))
	mux.Handle("GET /admin", s.admin.Func(s.pageAdmin))
	mux.Handle("POST /admin/applications", s.admin.Wrap(s.sameOrigin(http.HandlerFunc(s.saveApplication))))
	mux.Handle("POST /admin/applications/{key}", s.admin.Wrap(s.sameOrigin(http.HandlerFunc(s.saveApplication))))
	mux.Handle("POST /admin/applications/{key}/delete", s.admin.Wrap(s.sameOrigin(http.HandlerFunc(s.deleteApplication))))
	mux.Handle("POST /admin/suggestions/attach", s.admin.Wrap(s.sameOrigin(http.HandlerFunc(s.attach))))
	mux.Handle("POST /admin/suggestions/ignore", s.admin.Wrap(s.sameOrigin(http.HandlerFunc(s.ignore))))
	mux.Handle("POST /admin/suggestions/add", s.admin.Wrap(s.sameOrigin(http.HandlerFunc(s.addSuggested))))
	mux.Handle("GET /admin/capabilities", s.admin.Func(s.pageCapabilities))
	mux.Handle("POST /admin/capabilities", s.admin.Wrap(s.sameOrigin(http.HandlerFunc(s.saveCapability))))
	mux.Handle("POST /admin/capabilities/{key}", s.admin.Wrap(s.sameOrigin(http.HandlerFunc(s.saveCapability))))
	mux.Handle("POST /admin/capabilities/{key}/delete", s.admin.Wrap(s.sameOrigin(http.HandlerFunc(s.deleteCapability))))
	mux.Handle("GET /admin/sources", s.admin.Func(s.pageMappings))
	mux.Handle("POST /admin/sources/{kind}", s.admin.Wrap(s.sameOrigin(http.HandlerFunc(s.saveMapping))))
	mux.Handle("GET /admin/lenses", s.platform.Func(s.pageLenses))
	mux.Handle("POST /admin/lenses", s.platform.Wrap(s.sameOrigin(http.HandlerFunc(s.saveLens))))
	mux.Handle("POST /admin/lenses/{blueprint}", s.platform.Wrap(s.sameOrigin(http.HandlerFunc(s.saveLens))))
	mux.Handle("POST /admin/lenses/{blueprint}/delete", s.platform.Wrap(s.sameOrigin(http.HandlerFunc(s.deleteLens))))
	mux.Handle("POST "+viewAsPath, s.admin.Wrap(s.viewOrigin(http.HandlerFunc(s.startViewing))))
	mux.Handle("POST "+viewAsExit, s.gate.Wrap(s.viewOrigin(http.HandlerFunc(s.endViewing))))
	if s.requestsOn() {
		mux.Handle("GET "+requestsPath, s.gate.Func(s.pageRequests))
		mux.Handle("POST "+requestsPath, s.gate.Wrap(door.SameOrigin(strings.TrimRight(s.cfg.PublicURL, "/"), s.log, http.HandlerFunc(s.sendRequest))))
	}
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", s.assets()))
	for path, typ := range installFiles {
		mux.Handle("GET "+path, installFile(path, typ))
	}
	return telemetry.Handler(s.viewAs(mux), "session")
}

// Open is the port anything may reach: the health check and the browser
// telemetry relay. Nothing here identifies a person, so a relayed batch
// carries no user.
func (s *Server) Open() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("POST /api/telemetry", s.relay)
	return telemetry.Handler(mux, "open")
}

func (s *Server) assets() http.Handler {
	fs := http.FileServerFS(assetFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "assets/" + r.URL.Path
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fs.ServeHTTP(w, r)
	})
}

// installFiles are what a device reads to add the portal to a Home Screen
// or install it on a desktop, by path and content type. The device fetches
// them without a session, so they skip the gate, like the assets.
var installFiles = map[string]string{
	"/apple-touch-icon.png": "image/png",
	"/icon-192.png":         "image/png",
	"/icon-512.png":         "image/png",
	"/manifest.webmanifest": "application/manifest+json",
}

func installFile(path, typ string) http.Handler {
	body, err := assetFS.ReadFile("assets" + path)
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(body)
	})
}

// viewer is who is asking: their front-door roles, and who they are in
// Entra, from what Orrery holds.
func (s *Server) viewer(r *http.Request, d catalog.Directory) catalog.Viewer {
	who := door.From(r.Context())
	return catalog.Viewer{Email: who.Email, Roles: who.Roles, Person: d.Access.PersonFor(who.Email)}
}

// visible is the tiles this person may see, with the catalog's state.
func (s *Server) visible(r *http.Request) (catalog.Snapshot[catalog.Directory], []catalog.App) {
	snap := s.cache.Get(r.Context())
	return snap, catalog.Filter(r.Context(), s.log, snap.Value.Apps, s.viewer(r, snap.Value))
}

func (s *Server) pageApps(w http.ResponseWriter, r *http.Request) {
	snap, apps := s.visible(r)
	names := make(map[string]string, len(snap.Value.Capabilities))
	for _, c := range snap.Value.Capabilities {
		names[c.Key] = c.Name
	}
	s.render(w, r, http.StatusOK, "apps", map[string]any{
		"CapNames": names,
		"Caps":     snap.Value.Capabilities,
		"Groups":   catalog.GroupBy(apps, snap.Value.Capabilities),
		"Count":    len(apps),
		"Catalog":  catalogState(snap),
		"Actions":  s.actions.For(r.Context(), door.From(r.Context()).Email),
		"Hub":      s.cfg.OrreryURL,
	})
}

// pageLandscape is every application the organization offers as one
// wall, like the CNCF landscape, for everyone: grouped by business
// capability, with filters, a list view, and a panel per card. Services
// are not on it: an application names what it is built from in its panel.
// An application the person may not open is dimmed, with where to ask for
// it; it names applications, never who holds them.
func (s *Server) pageLandscape(w http.ResponseWriter, r *http.Request) {
	snap := s.coverage.Get(r.Context())
	d := s.cache.Get(r.Context()).Value
	l := snap.Value.Landscape(snap.Value.Lens(catalog.CapabilityBlueprint), d.Applications, false, s.cfg.OrreryURL)
	names := map[string]string{catalog.OtherKey: catalog.Other(l.Categories).Name}
	for _, c := range l.Categories {
		names[c.Key] = c.Name
	}
	s.render(w, r, http.StatusOK, "landscape", map[string]any{
		"Names":     names,
		"Columns":   catalog.Grid(l.Cards, l.Categories),
		"Cards":     l.Cards,
		"Details":   catalog.Details(l),
		"Reach":     catalog.ReachOf(d.Apps, s.viewer(r, d), s.requestFor(r)),
		"Actions":   s.actions.For(r.Context(), door.From(r.Context()).Email),
		"Count":     len(l.Cards),
		"Kinds":     catalog.Kinds,
		"Legend":    catalog.LegendOf(l.Cards),
		"Adoptions": catalog.Adoptions,
		"Catalog":   catalogState(snap),
	})
}

// requestFor is where this person asks for an application they may not
// open: Orrery's request action for the way it grants access, with the
// application filled in, when Orrery lets them run it; otherwise nowhere.
func (s *Server) requestFor(r *http.Request) func(catalog.App) string {
	urls := map[string]string{}
	for _, a := range s.actions.For(r.Context(), door.From(r.Context()).Email) {
		urls[a.ID] = a.URL
	}
	return func(a catalog.App) string {
		base := urls[RequestActions[a.Access]]
		if base == "" {
			return ""
		}
		return base + "?entity=" + url.QueryEscape(a.Ref)
	}
}

// lensOf is the lens a request asks for, and its landscape: applications,
// services, and outside APIs.
func (s *Server) lensOf(r *http.Request) (catalog.Snapshot[catalog.Coverage], catalog.Directory, catalog.Landscape) {
	snap := s.coverage.Get(r.Context())
	d := s.cache.Get(r.Context()).Value
	lens := snap.Value.Lens(r.URL.Query().Get("lens"))
	return snap, d, snap.Value.Landscape(lens, d.Applications, true, s.cfg.OrreryURL)
}

// pageAnalytics is one lens over the suite, for the platform team: the
// applications and the services placed on its categories as a wall and a
// matrix, the overlap, the radar and table, the gaps, the counts by team
// and access, and each category's assessment.
func (s *Server) pageAnalytics(w http.ResponseWriter, r *http.Request) {
	snap, d, l := s.lensOf(r)
	q := r.URL.Query()
	s.render(w, r, http.StatusOK, "coverage", map[string]any{
		"Lens":         l.Lens,
		"Lenses":       snap.Value.Lenses,
		"Columns":      catalog.Grid(l.Cards, l.Categories),
		"Matrix":       catalog.MatrixOf(l.Cards, l.Categories),
		"Details":      catalog.Details(l),
		"Analysis":     catalog.AnalysisOf(l),
		"Breakdown":    catalog.BreakdownOf(l, d.Apps),
		"Overlap":      catalog.OverlapOf(l),
		"Ratings":      catalog.RatingsOf(l, snap.Value.Assessments),
		"Integrations": catalog.IntegrationKinds,
		"Count":        len(l.Cards),
		"Kinds":        catalog.Kinds,
		"Held":         catalog.KindsOf(l.Cards),
		"Legend":       catalog.LegendOf(l.Cards),
		"Adoptions":    catalog.Adoptions,
		"Writable":     s.writable(),
		"Done":         q.Get("done"),
		"Failed":       q.Get("failed"),
		"Catalog":      catalogState(snap),
	})
}

// landscapeAnswer is the JSON of /v1/landscape: one lens.
type landscapeAnswer struct {
	Lens       catalog.Lens       `json:"lens"`
	Categories []catalog.Category `json:"categories"`
	Cards      []catalog.Card     `json:"cards"`
	Edges      []catalog.Edge     `json:"edges"`
	// Sources are Orrery's sources, what each acts for and wrote; empty
	// when they are unknown, and then no card carries an integration.
	Sources      []catalog.Source `json:"sources"`
	SourcesKnown bool             `json:"sourcesKnown"`
	Catalog      catalogInfo      `json:"catalog"`
}

func (s *Server) apiLandscape(w http.ResponseWriter, r *http.Request) {
	snap, _, l := s.lensOf(r)
	out := landscapeAnswer{Lens: l.Lens, Categories: l.Categories, Cards: l.Cards, Edges: l.Edges, Sources: l.Sources, SourcesKnown: l.SourcesKnown, Catalog: catalogState(snap)}
	if out.Categories == nil {
		out.Categories = []catalog.Category{}
	}
	if out.Sources == nil {
		out.Sources = []catalog.Source{}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

// appsAnswer is the JSON of /v1/apps.
type appsAnswer struct {
	Apps    []catalog.App    `json:"apps"`
	Groups  []catalog.Group  `json:"groups"`
	Actions []catalog.Action `json:"actions"`
	Catalog catalogInfo      `json:"catalog"`
}

type catalogInfo struct {
	Fetched *time.Time `json:"fetched,omitempty"`
	Stale   bool       `json:"stale"`
	Error   string     `json:"error,omitempty"`
}

func catalogState[T any](snap catalog.Snapshot[T]) catalogInfo {
	info := catalogInfo{Stale: snap.Stale}
	if !snap.Fetched.IsZero() {
		t := snap.Fetched
		info.Fetched = &t
	}
	if snap.Err != nil {
		info.Error = "catalog unreachable"
	}
	return info
}

func (s *Server) apiApps(w http.ResponseWriter, r *http.Request) {
	snap, apps := s.visible(r)
	w.Header().Set("Cache-Control", "no-store")
	actions := s.actions.For(r.Context(), door.From(r.Context()).Email)
	if actions == nil {
		actions = []catalog.Action{}
	}
	writeJSON(w, http.StatusOK, appsAnswer{Apps: apps, Groups: catalog.GroupBy(apps, snap.Value.Capabilities), Actions: actions, Catalog: catalogState(snap)})
}

// open counts the tile by category and sends the person to the app. A tile
// the person may not see is not there.
func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	snap, apps := s.visible(r)
	for _, a := range apps {
		if a.Key != key {
			continue
		}
		capability := catalog.First(snap.Value.Capabilities, a.Capabilities).Key
		if v, ok := viewingFrom(r.Context()); ok {
			// An admin checking a view is not the person using the app.
			s.log.InfoContext(r.Context(), "tile opened while viewing as", "app", a.Key, "actor", v.By, "viewing", v.Email)
		} else {
			s.metrics.TileOpen(r.Context(), capability)
			s.log.InfoContext(r.Context(), "tile opened", "app", a.Key, "capability", capability)
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, a.URL, http.StatusFound)
		return
	}
	s.log.WarnContext(r.Context(), "open refused: no such visible tile", "app", key)
	s.message(w, r, http.StatusNotFound, "No such app", "There is no app by that name that you may open.")
}

// icon serves a logo the caches hold. It never asks Orrery: a name not
// held is not found, and the page draws a monogram for it.
func (s *Server) icon(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	theme, known := catalog.ThemeOf(r.URL.Query().Get("theme"))
	img, ok := s.icons.Get(name, theme)
	if !catalog.IconName(name) || !known || !ok {
		http.Error(w, "no such icon", http.StatusNotFound)
		return
	}
	h := w.Header()
	h.Set("Content-Type", img.Type)
	h.Set("Cache-Control", "private, max-age=300")
	h.Set("X-Content-Type-Options", "nosniff")
	if img.ETag != "" {
		h.Set("ETag", img.ETag)
	}
	if img.Type == "image/svg+xml" {
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	}
	if img.ETag != "" && r.Header.Get("If-None-Match") == img.ETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(img.Data)
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, page string, data map[string]any) {
	who := door.From(r.Context())
	data["Email"] = who.Email
	if who.HasAny(AdminRole, PlatformRole) {
		d := s.cache.Get(r.Context()).Value
		data["Platform"] = true
		if s.cfg.OrreryURL != "" && d.Waiting > 0 {
			data["Approvals"] = d.Waiting
			data["ApprovalsURL"] = s.cfg.OrreryURL + "/entities/" + catalog.ApprovalBlueprint
		}
		if who.Has(AdminRole) {
			data["Admin"] = true
			data["Waiting"] = len(d.Suggestions)
		}
	}
	if v, ok := viewingFrom(r.Context()); ok {
		data["ViewingAs"] = v
	} else if who.Has(AdminRole) {
		data["People"] = s.cache.Get(r.Context()).Value.Access.Emails()
	}
	data["Requests"] = s.requestsOn()
	data["SignOut"] = s.cfg.SignOutURL
	data["Nav"] = page
	data["Org"] = s.cfg.Org
	if s.cfg.OrgIcon != "" && s.icons != nil {
		data["OrgLogo"] = s.icons.URL(s.cfg.OrgIcon, catalog.OnLight)
		data["OrgLogoDark"] = s.icons.URL(s.cfg.OrgIcon, catalog.OnDark)
	}
	data["Version"] = s.cfg.Version
	data["BrowserTelemetry"] = s.cfg.BrowserTelemetry
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.WriteHeader(status)
	if err := s.pages[page].Execute(w, data); err != nil {
		s.log.ErrorContext(r.Context(), "page render failed", "page", page, "error", err)
	}
}

func (s *Server) message(w http.ResponseWriter, r *http.Request, status int, title, text string) {
	s.render(w, r, status, "message", map[string]any{"Title": title, "Text": text})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
