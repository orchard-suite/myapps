package catalog

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/orchard-suite/platformkit/orrery"
)

// An application is a defined deployment people in the organization use:
// installed on their machine, cloud-hosted on someone else's platform, or
// self-hosted on ours. It is not a service: most services are jobs, APIs,
// backends, or products sold to customers. An application links to the
// services it runs: the ones the organization built it from, or the
// open-source project it is. A SaaS application links to the outside API
// it is. Admins
// keep the list in My Apps; it lives in Orrery as it_application.
const ApplicationBlueprint = "it_application"

// The blueprints an application may be paired with.
const (
	EntraBlueprint    = "entra_app"
	WorkloadBlueprint = "sw_workload"
	ServiceBlueprint  = "sw_service"
	APIBlueprint      = "sw_api"
)

// Listed says where My Apps shows an application.
type Listed string

const (
	// Shown is a tile for the people who may open it, and a card on the
	// Landscape for everyone.
	Shown Listed = "shown"
	// NoLogin is a card on the Landscape with no tile: hidden from login,
	// such as My Apps itself, where people already are.
	NoLogin Listed = "no_login"
	// Hidden keeps the application in the catalog with no tile and no card.
	Hidden Listed = "hidden"
)

// Listeds are the values, in the order the admin page offers them.
var Listeds = []Listed{Shown, NoLogin, Hidden}

// Tile is whether the application may be a tile.
func (l Listed) Tile() bool { return l == Shown }

// OnLandscape is whether the application is a card on the Landscape.
func (l Listed) OnLandscape() bool { return l == Shown || l == NoLogin }

// Hosting is where an application runs.
type Hosting string

const (
	Installed  Hosting = "installed"
	SaaS       Hosting = "saas"
	SelfHosted Hosting = "self_hosted"
)

// Hostings are the hostings, in the order the admin page offers them.
var Hostings = []Hosting{SelfHosted, SaaS, Installed}

// AccessMode is how a person comes to hold an application: anyone may open
// it, an approver assigns them to its Entra app, or adds them to one of its
// access groups, or Entra grants it through an access package.
type AccessMode string

const (
	AccessOpen    AccessMode = "open"
	AccessDirect  AccessMode = "direct"
	AccessGroup   AccessMode = "group"
	AccessPackage AccessMode = "package"
)

// AccessModes are the modes, in the order the admin page offers them.
var AccessModes = []AccessMode{AccessOpen, AccessDirect, AccessGroup, AccessPackage}

// Lifecycles are the shared lifecycle values.
var Lifecycles = []string{"planned", "active", "retiring", "retired"}

// Tiers are the tiers an application may carry.
var Tiers = []string{"critical", "important", "standard"}

// oneOf reads a value of a fixed list; anything else is empty.
func oneOf[T ~string](list []T, s string) T {
	if v := T(strings.TrimSpace(s)); slices.Contains(list, v) {
		return v
	}
	return ""
}

// Application is one it_application entity.
type Application struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
	// Logo is the icon it is drawn with (LogoOf); WithLogos sets it.
	Logo        string  `json:"logo,omitempty"`
	Description string  `json:"description,omitempty"`
	URL         string  `json:"url,omitempty"`
	Hosting     Hosting `json:"hosting,omitempty"`
	// SelfBuilt is true for an application the organization built itself,
	// wherever it runs.
	SelfBuilt  bool       `json:"selfBuilt,omitempty"`
	Lifecycle  string     `json:"lifecycle,omitempty"`
	Tier       string     `json:"tier,omitempty"`
	Listed     Listed     `json:"listed"`
	Audience   []string   `json:"audience,omitempty"`
	Access     AccessMode `json:"access,omitempty"`
	AccessRole string     `json:"accessRole,omitempty"`
	// SignIn are the entra_app keys people sign in through; the first is
	// the one a request grants.
	SignIn       []string `json:"signIn,omitempty"`
	AccessGroups []string `json:"accessGroups,omitempty"`
	// People are the emails that may open it when no Entra app decides.
	People   []string `json:"people,omitempty"`
	Services []string `json:"services,omitempty"`
	// Repository is where its code lives, as the forge names it, such as
	// acme/portal; WithRepositories sets it from the services it runs.
	Repository   string   `json:"-"`
	Workload     string   `json:"workload,omitempty"`
	API          string   `json:"api,omitempty"`
	Team         string   `json:"team,omitempty"`
	TeamName     string   `json:"teamName,omitempty"`
	Capabilities []string `json:"capabilities"`
	// Links are every relation, by name, so any lens can place it.
	Links map[string][]string `json:"-"`
	// Seen is false once Orrery has marked it missing.
	Seen bool `json:"seen"`

	status Status
	reason []string
}

// Ref is the application's id in Orrery.
func (a Application) Ref() string { return ApplicationBlueprint + ":" + a.Key }

// Applications reads it_application entities, by name. Hidden entities are
// left out.
func Applications(records []orrery.Record) []Application {
	out := make([]Application, 0, len(records))
	for _, r := range records {
		if r.Hidden || r.Key == "" {
			continue
		}
		a := Application{
			Key:          r.Key,
			Name:         cmp.Or(r.Title, r.Key),
			Icon:         r.Icon,
			Description:  strings.TrimSpace(r.String("description")),
			URL:          strings.TrimSpace(r.String("url")),
			Hosting:      oneOf(Hostings, r.String("hosting")),
			SelfBuilt:    selfBuilt(r),
			Lifecycle:    oneOf(Lifecycles, r.String("lifecycle")),
			Tier:         oneOf(Tiers, r.String("tier")),
			Listed:       oneOf(Listeds, r.String("listed")),
			Audience:     parseAudience(r.Fields["audience"]),
			Access:       oneOf(AccessModes, r.String("access")),
			AccessRole:   strings.TrimSpace(r.String("access_role")),
			SignIn:       refKeys(r.Relations["sign_in"]),
			AccessGroups: refKeys(r.Relations["access_groups"]),
			People:       parsePeople(r.Fields["people"]),
			Services:     refKeys(r.Relations["services"]),
			Capabilities: refKeys(r.Relations[CapabilityRelation]),
			Links:        linksOf(r),
			Seen:         r.State == "" || r.State == "active",
			status:       statusOf(r.State, r.Status.Level),
			reason:       r.Status.Reasons,
		}
		if a.Listed == "" {
			a.Listed = Shown
		}
		if w, ok := r.Related("workload"); ok {
			a.Workload = w.Key
		}
		if api, ok := r.Related("api"); ok {
			a.API = api.Key
		}
		if t, ok := r.Related("team"); ok {
			a.Team, a.TeamName = t.Key, cmp.Or(t.Title, t.Key)
		}
		out = append(out, a)
	}
	slices.SortStableFunc(out, func(x, y Application) int {
		return cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
	})
	return out
}

// selfBuilt reads the self_built field; anything but true is false.
func selfBuilt(r orrery.Record) bool {
	v, ok := r.Bool("self_built")
	return ok && v
}

// refKeys is the keys a relation names, once each, in its order.
func refKeys(refs []orrery.Ref) []string {
	out := []string{}
	for _, r := range refs {
		if r.Key != "" && !slices.Contains(out, r.Key) {
			out = append(out, r.Key)
		}
	}
	return out
}

// linksOf is every relation of an entity, by name, as keys.
func linksOf(e orrery.Record) map[string][]string {
	out := make(map[string][]string, len(e.Relations))
	for name, refs := range e.Relations {
		out[name] = refKeys(refs)
	}
	return out
}

// Tiles are the shown applications as tiles, in their order. Who may open
// one is decided by its Entra sign-in when it has one, unless it is open to
// everyone, else by the roles, groups, and people it names. An open
// application keeps its own accounts, so Entra's assignments do not limit
// who sees it.
func Tiles(apps []Application, entra []EntraApp, access Access) []App {
	byKey := make(map[string]EntraApp, len(entra))
	for _, e := range entra {
		byKey[e.Key] = e
	}
	var out []App
	for _, a := range apps {
		if !a.Listed.Tile() || !a.Seen {
			continue
		}
		url := a.URL
		if url == "" && len(a.SignIn) > 0 {
			url = byKey[a.SignIn[0]].LaunchURL
		}
		if url == "" {
			continue
		}
		t := App{
			Key:          a.Key,
			Ref:          a.Ref(),
			Title:        a.Name,
			Description:  a.Description,
			Icon:         a.Logo,
			Tier:         a.Tier,
			Team:         a.TeamName,
			URL:          url,
			Capabilities: a.Capabilities,
			Audience:     a.Audience,
			Groups:       a.AccessGroups,
			People:       a.People,
			Status:       a.status,
			Reasons:      a.reason,
			Entra:        len(a.SignIn) > 0,
			Hosting:      a.Hosting,
			Access:       a.Access,
			Repository:   a.Repository,
		}
		if len(a.SignIn) > 0 && a.Access != AccessOpen {
			rule := access.SignInOfApps(a.SignIn, byKey)
			t.SignIn = &rule
		}
		out = append(out, t)
	}
	return out
}

// LogoOf is the icon an application is drawn with: its own, else the logo
// of the Entra app people first sign in through, else none.
func LogoOf(icon string, signIn []string, entra []EntraApp) string {
	if icon != "" || len(signIn) == 0 {
		return icon
	}
	for _, e := range entra {
		if e.Key == signIn[0] {
			return e.Icon
		}
	}
	return ""
}

// WithLogos sets every application's Logo from its own icon and its
// Entra apps.
func WithLogos(apps []Application, entra []EntraApp) []Application {
	out := slices.Clone(apps)
	for i := range out {
		out[i].Logo = LogoOf(out[i].Icon, out[i].SignIn, entra)
	}
	return out
}

func first(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}

// AppDetails are an application as an admin sets it. Empty clears.
type AppDetails struct {
	Name, Icon, Description, URL string
	Hosting                      Hosting
	SelfBuilt                    bool
	Lifecycle, Tier              string
	Listed                       Listed
	Audience                     []string
	Access                       AccessMode
	AccessRole                   string
	SignIn, AccessGroups         []string
	People                       []string
	Services                     []string
	Workload, API, Team          string
	Capabilities                 []string
	// Placed is the application's categories per lens relation, such as
	// pillars: [platform/catalog].
	Placed map[string][]string
	By     string
	At     time.Time
	// Into are where the hub holds what the application links to. A link
	// to a kind they leave unmapped is not written, since the hub has no
	// such relation; nil writes every link.
	Into Mappings
}

// Entity is the application as My Apps writes it. Placed holds only the
// lens relations the form showed, so a lens not shown keeps its value.
func (d AppDetails) Entity(key string) orrery.Entity {
	relations := map[string]any{
		"sign_in":          nonNil(d.SignIn),
		"access_groups":    nonNil(d.AccessGroups),
		"services":         nonNil(d.Services),
		"workload":         orNil(d.Workload),
		"api":              orNil(d.API),
		"team":             orNil(d.Team),
		CapabilityRelation: nonNil(d.Capabilities),
	}
	for rel, keys := range d.Placed {
		relations[rel] = nonNil(keys)
	}
	if d.Into != nil {
		for _, l := range LinkedRelations {
			if d.Into.Blueprint(l.Kind) == "" {
				delete(relations, l.Relation)
			}
		}
	}
	return orrery.Entity{
		Blueprint: ApplicationBlueprint,
		Key:       key,
		Title:     strings.TrimSpace(d.Name),
		Icon:      strings.TrimSpace(d.Icon),
		Fields: map[string]any{
			"description": orNil(d.Description),
			"url":         orNil(d.URL),
			"hosting":     orNil(string(d.Hosting)),
			"self_built":  d.SelfBuilt,
			"lifecycle":   orNil(d.Lifecycle),
			"tier":        orNil(d.Tier),
			"listed":      string(cmp.Or(d.Listed, Shown)),
			"audience":    nonNil(d.Audience),
			"people":      nonNil(d.People),
			"access":      orNil(string(d.Access)),
			"access_role": orNil(d.AccessRole),
			"updated_by":  d.By,
			"updated_at":  d.At.UTC().Format(time.RFC3339),
		},
		Relations: relations,
	}
}

// ApplicationKey is a new application's key, from its name, unique among
// those taken. It never changes after: a rename keeps it.
func ApplicationKey(name string, taken []Application) string {
	keys := make([]Category, len(taken))
	for i, a := range taken {
		keys[i] = Category{Key: a.Key}
	}
	return CapabilityKey(name, "", keys)
}

// orNil is nil for an empty string, which Orrery reads as clearing the
// value My Apps wrote.
func orNil(s string) any {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	return s
}

// nonNil is never a nil slice, so an empty list is written as empty.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
