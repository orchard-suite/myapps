package server

import (
	"cmp"
	"slices"
	"strings"

	"github.com/orchard-suite/myapps/internal/catalog"
)

// The admin grid: every application a row, every field a column. The
// server draws the columns and their choices and shapes each row; the page
// script only lays them out and sends a changed cell back as a form.

// cellKind is how a column shows and edits its value.
type cellKind string

const (
	// cellText is one line of words; cellURL an https address; cellIcon an
	// icon name.
	cellText cellKind = "text"
	cellURL  cellKind = "url"
	cellIcon cellKind = "icon"
	// cellList is several words typed with commas between.
	cellList cellKind = "list"
	// cellOne is one of the column's choices, or none.
	cellOne cellKind = "one"
	// cellMany is any of the column's choices.
	cellMany cellKind = "many"
	// cellToggle is one of a few choices, all on view at once.
	cellToggle cellKind = "toggle"
	// cellRead is shown and never edited.
	cellRead cellKind = "read"
)

// gridOption is one choice in a column, at its depth in a tree.
type gridOption struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Depth int    `json:"depth,omitempty"`
	// Off is a choice shown but not pickable, such as a dynamic group.
	Off bool `json:"off,omitempty"`
}

// gridColumn is one field of an application. Its ID is the form field a
// change sends.
type gridColumn struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Kind  cellKind `json:"kind"`
	// Options are the choices of a one, many, or toggle column. PerRow
	// says each row carries its own instead, by the column's ID.
	Options []gridOption `json:"options,omitempty"`
	PerRow  bool         `json:"perRow,omitempty"`
	// None is the words for an empty cell.
	None string `json:"none,omitempty"`
	// Hint is what the column holds, for its header's tooltip.
	Hint  string `json:"hint,omitempty"`
	Width int    `json:"width"`
	// Shown is whether the column shows before a person picks.
	Shown bool `json:"shown"`
}

// gridRow is one application: its values by column, and what the row shows
// beside them.
type gridRow struct {
	Key      string `json:"key"`
	Initial  string `json:"initial"`
	Hue      int    `json:"hue"`
	Logo     string `json:"logo,omitempty"`
	LogoDark string `json:"logoDark,omitempty"`
	// Values are every column's value; one value or none for a single.
	Values map[string][]string `json:"values"`
	// Options are the choices of the columns whose choices hang on the row.
	Options map[string][]gridOption `json:"options,omitempty"`
	// Warnings are what keeps it from showing as it should.
	Warnings []string `json:"warnings,omitempty"`
	// Orrery is the application's page in Orrery, when people can open it.
	Orrery string `json:"orrery,omitempty"`
}

// adminGrid is everything the grid draws.
type adminGrid struct {
	Columns  []gridColumn `json:"columns"`
	Rows     []gridRow    `json:"rows"`
	Writable bool         `json:"writable"`
}

// The column IDs, each the form field a change of it sends.
const (
	colName        = "name"
	colListed      = "listed"
	colHosting     = "hosting"
	colSelfBuilt   = "self_built"
	colCapability  = "capability"
	colTeam        = "team"
	colLifecycle   = "lifecycle"
	colTier        = "tier"
	colURL         = "url"
	colDescription = "description"
	colIcon        = "icon"
	colSignIn      = "sign_in"
	colAccess      = "access"
	colRole        = "role"
	colGroup       = "group"
	colAudience    = "audience"
	colPeople      = "people"
	colWho         = "who"
	colServices    = "services"
	colWorkload    = "workload"
	colAPI         = "api"
	// placePrefix leads a lens's column: place.pillars.
	placePrefix = "place."
)

// The self built column's values: a form sends yes or no.
const (
	selfBuiltYes = "yes"
	selfBuiltNo  = "no"
)

var selfBuiltOptions = []gridOption{{Key: selfBuiltYes, Name: "Yes"}, {Key: selfBuiltNo, Name: "No"}}

// selfBuiltValue is the self built column's value for a row.
func selfBuiltValue(built bool) string {
	if built {
		return selfBuiltYes
	}
	return selfBuiltNo
}

// gridColumns are the columns, in order, with every choice the catalog
// holds.
func gridColumns(d catalog.Directory, cov catalog.Coverage) []gridColumn {
	opts := func(list []option) []gridOption {
		out := make([]gridOption, len(list))
		for i, o := range list {
			out[i] = gridOption{Key: o.Key, Name: o.Name}
		}
		return out
	}
	named := func(list []catalog.Category) []gridOption {
		out := make([]gridOption, len(list))
		for i, c := range list {
			out[i] = gridOption{Key: c.Key, Name: c.Name}
		}
		slices.SortFunc(out, func(x, y gridOption) int { return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)) })
		return out
	}
	tree := func(all []catalog.Category) []gridOption {
		var out []gridOption
		for _, row := range treeOf(all) {
			out = append(out, gridOption{Key: row.Category.Key, Name: row.Category.Name, Depth: row.Depth})
		}
		return out
	}
	words := func(list []string) []gridOption {
		out := make([]gridOption, len(list))
		for i, v := range list {
			out[i] = gridOption{Key: v, Name: strings.ToUpper(v[:1]) + v[1:]}
		}
		return out
	}
	var hostings, listeds, modes []gridOption
	for _, h := range catalog.Hostings {
		hostings = append(hostings, gridOption{Key: string(h), Name: hostingWord(h)})
	}
	for _, l := range catalog.Listeds {
		listeds = append(listeds, gridOption{Key: string(l), Name: listedText(l)})
	}
	for _, m := range catalog.AccessModes {
		modes = append(modes, gridOption{Key: string(m), Name: modeText(m)})
	}
	entra := make([]gridOption, len(d.EntraApps))
	for i, e := range d.EntraApps {
		entra[i] = gridOption{Key: e.Key, Name: e.Name}
	}
	cols := []gridColumn{
		{ID: colName, Title: "Name", Kind: cellText, Width: 240, Shown: true, Hint: "What people call it. A rename keeps its key."},
		{ID: colListed, Title: "In My Apps", Kind: cellToggle, Options: listeds, Width: 284, Shown: true, Hint: "Shown is a tile and a Landscape card. Hidden from login is a card with no tile. Hidden is neither."},
		{ID: colHosting, Title: "Hosting", Kind: cellOne, Options: hostings, None: "Not set", Width: 130, Shown: true, Hint: "Where it runs."},
		{ID: colSelfBuilt, Title: "Self built", Kind: cellToggle, Options: selfBuiltOptions, Width: 150, Shown: true, Hint: "Yes when we built it ourselves, wherever it runs. The Landscape draws it as Built here."},
		{ID: colCapability, Title: "Business capabilities", Kind: cellMany, Options: tree(d.Capabilities), None: "More apps", Width: 230, Shown: true, Hint: "Where everyone finds it. With none it shows under More apps."},
	}
	for _, p := range placingsOf(cov) {
		cols = append(cols, gridColumn{ID: placePrefix + p.Relation, Title: p.Lens.Title, Kind: cellMany, Options: tree(p.Categories), None: "None", Width: 200, Shown: true, Hint: "Its place on the " + p.Lens.Title + " lens."})
	}
	return append(cols,
		gridColumn{ID: colTeam, Title: "Team", Kind: cellOne, Options: named(d.Teams), None: "No team", Width: 160, Shown: true, Hint: "Who owns it."},
		gridColumn{ID: colLifecycle, Title: "Lifecycle", Kind: cellOne, Options: words(catalog.Lifecycles), None: "Not set", Width: 120, Shown: true, Hint: "Planned draws dimmed and dashed."},
		gridColumn{ID: colTier, Title: "Tier", Kind: cellOne, Options: words(catalog.Tiers), None: "Not set", Width: 120, Shown: true, Hint: "Critical and important ones greet a first visit."},
		gridColumn{ID: colURL, Title: "Address", Kind: cellURL, None: "No address", Width: 220, Shown: true, Hint: "Where people open it, https only. An installed app: where to get it."},
		gridColumn{ID: colDescription, Title: "Description", Kind: cellText, None: "None", Width: 260, Shown: true, Hint: "One line of what it is."},
		gridColumn{ID: colIcon, Title: "Logo", Kind: cellIcon, None: "Monogram", Width: 140, Hint: "An icon name in Orrery, such as grafana."},
		gridColumn{ID: colSignIn, Title: "Signs in through", Kind: cellMany, Options: entra, None: "No Entra app", Width: 220, Shown: true, Hint: "The Entra apps people sign in through. Anyone any of them admits may open it. The first is the one a request grants."},
		gridColumn{ID: colAccess, Title: "How people get it", Kind: cellOne, Options: modes, None: "Not set", Width: 200, Hint: "What a request for it does. Open: everyone, whatever Entra assigns."},
		gridColumn{ID: colRole, Title: "Request role", Kind: cellOne, PerRow: true, None: "Default access", Width: 160, Hint: "The role a direct request grants, from its first Entra app."},
		gridColumn{ID: colGroup, Title: "Access groups", Kind: cellMany, PerRow: true, None: "None", Width: 200, Shown: true, Hint: "With an Entra app: the groups a group request adds a person to; Entra fills a dynamic one. With none: the Entra groups whose direct members may open it."},
		gridColumn{ID: colPeople, Title: "People", Kind: cellList, None: "No one named", Width: 220, Shown: true, Hint: "With no Entra app: the emails of people who may open it, with commas between. Works for people Entra does not hold."},
		gridColumn{ID: colAudience, Title: "Front-door roles", Kind: cellList, None: "None", Width: 180, Hint: "Roles that may open it, with commas between. With no Entra app and no role, group, or person named, everyone signed in may."},
		gridColumn{ID: colWho, Title: "Who may open it", Kind: cellRead, Width: 190, Shown: true, Hint: "What Entra or the roles allow."},
		gridColumn{ID: colServices, Title: "Services", Kind: cellMany, Options: opts(servicesOf(cov)), None: "None", Width: 220, Hint: "The services it runs: built here, or the open-source project it is."},
		gridColumn{ID: colWorkload, Title: "Workload", Kind: cellOne, Options: named(d.Workloads), None: "None", Width: 180, Hint: "The workload it runs as."},
		gridColumn{ID: colAPI, Title: "Outside API", Kind: cellOne, Options: opts(externalAPIs(cov)), None: "None", Width: 180, Hint: "The outside API a SaaS application is."},
	)
}

// rowOf is one application's row, from its details as saved.
func (s *Server) rowOf(key string, det catalog.AppDetails, d catalog.Directory) gridRow {
	one := func(v string) []string {
		if v == "" {
			return []string{}
		}
		return []string{v}
	}
	list := func(v []string) []string {
		if v == nil {
			return []string{}
		}
		return v
	}
	row := gridRow{
		Key:     key,
		Initial: initial(det.Name),
		Hue:     hue(key),
		Values: map[string][]string{
			colName:        one(det.Name),
			colListed:      one(string(cmp.Or(det.Listed, catalog.Shown))),
			colHosting:     one(string(det.Hosting)),
			colSelfBuilt:   {selfBuiltValue(det.SelfBuilt)},
			colCapability:  list(det.Capabilities),
			colTeam:        one(det.Team),
			colLifecycle:   one(det.Lifecycle),
			colTier:        one(det.Tier),
			colURL:         one(det.URL),
			colDescription: one(det.Description),
			colIcon:        one(det.Icon),
			colSignIn:      list(det.SignIn),
			colAccess:      one(string(det.Access)),
			colRole:        one(det.AccessRole),
			colGroup:       list(det.AccessGroups),
			colAudience:    list(det.Audience),
			colPeople:      list(det.People),
			colWho:         {whoOf(det, d.EntraApps, d.Access)},
			colServices:    list(det.Services),
			colWorkload:    one(det.Workload),
			colAPI:         one(det.API),
		},
		Options: map[string][]gridOption{colRole: {}, colGroup: {}},
		Orrery:  catalog.ApplicationPage(s.cfg.OrreryURL, key),
	}
	for rel, keys := range det.Placed {
		row.Values[placePrefix+rel] = list(keys)
	}
	if logo := catalog.LogoOf(det.Icon, det.SignIn, d.EntraApps); logo != "" && s.icons != nil {
		row.Logo = s.icons.URL(logo, catalog.OnLight)
		row.LogoDark = s.icons.URL(logo, catalog.OnDark)
	}
	for _, r := range rolesOf(det.SignIn, d.EntraApps) {
		row.Options[colRole] = append(row.Options[colRole], gridOption{Key: r.Key, Name: r.Name})
	}
	for _, g := range groupChoices(det.SignIn, d.Access) {
		name := g.Name
		if g.Dynamic {
			name += " (dynamic)"
		}
		row.Options[colGroup] = append(row.Options[colGroup], gridOption{Key: g.Key, Name: name, Off: !groupPickable(det.SignIn, g)})
	}
	if det.Listed.Tile() || det.Listed == "" {
		if len(det.Capabilities) == 0 {
			row.Warnings = append(row.Warnings, "No capability: it shows under More apps")
		}
		if det.URL == "" && len(det.SignIn) == 0 {
			row.Warnings = append(row.Warnings, "No address: no tile until it has one")
		}
	}
	return row
}

// gridOf is the whole grid: the columns and every application's row.
func (s *Server) gridOf(d catalog.Directory, cov catalog.Coverage) adminGrid {
	rels := lensRelationNames(cov)
	g := adminGrid{Columns: gridColumns(d, cov), Rows: make([]gridRow, 0, len(d.Applications)), Writable: s.writable()}
	for _, a := range d.Applications {
		g.Rows = append(g.Rows, s.rowOf(a.Key, detailsOf(a, rels), d))
	}
	return g
}

// rolesOf are the roles of an application's first Entra app, which a
// direct request grants.
func rolesOf(signIn []string, entra []catalog.EntraApp) []option {
	if len(signIn) == 0 {
		return nil
	}
	i := slices.IndexFunc(entra, func(e catalog.EntraApp) bool { return e.Key == signIn[0] })
	if i < 0 {
		return nil
	}
	out := make([]option, len(entra[i].Roles))
	for j, r := range entra[i].Roles {
		out[j] = option{r, entra[i].RoleTitle(j)}
	}
	return out
}

// groupChoices are the access groups an application may name: with an
// Entra app, the groups assigned to it, where a group request adds a
// person; with none, every Entra group, whose members may open it.
func groupChoices(signIn []string, acc catalog.Access) []catalog.EntraGroup {
	if len(signIn) == 0 {
		return acc.Groups()
	}
	return acc.GroupsOf(signIn)
}

// groupPickable is whether a group may be named. A request cannot add a
// person to a group Entra fills, so with an Entra app a dynamic group is
// shown but not pickable; with none, its members may open it all the same.
func groupPickable(signIn []string, g catalog.EntraGroup) bool {
	return len(signIn) == 0 || !g.Dynamic
}
