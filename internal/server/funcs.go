package server

import (
	"fmt"
	"hash/fnv"
	"html/template"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/orchard-suite/myapps/internal/catalog"
)

// funcs are the template helpers. They shape what a tile shows, not what it is.
var funcs = template.FuncMap{
	"hue":         hue,
	"initial":     initial,
	"host":        host,
	"tileTip":     tileTip,
	"actionTip":   actionTip,
	"statusText":  statusText,
	"statusWord":  statusWord,
	"hostingWord": hostingWord,
	"hostingKeys": hostingKeys,
	"actionsOf":   catalog.ActionsOf,
	"servesOf":    servesOf,
	"subGroupsOf": catalog.SubGroupsOf,
	"tileOf": func(g catalog.Group, a catalog.App, page any) map[string]any {
		return map[string]any{"G": g, "A": a, "P": page}
	},
	"groupSuggestions": catalog.GroupSuggestions,
	"appPage":          catalog.ApplicationPage,
	"featured":         func(tier string) bool { return tier == "critical" || tier == "important" },
	"adoptionLabel":    func(key string) string { return catalog.OptionLabel(catalog.Adoptions, key) },
	"stateLabel":       func(key string) string { return catalog.OptionLabel(catalog.EdgeStates, key) },
	"home":             home,
	"markText":         markText,
	"inc":              func(n int) int { return n + 1 },
	"add":              func(a, b int) int { return a + b },
	"markTip":          markTip,
	"countTip":         countTip,
	"indent":           func(depth int) int { return depth * 22 },
	"indentDots":       func(depth int) []int { return make([]int, depth) },
	"assessOf":         assessOf,
	"ratingSteps":      func() []int { return []int{1, 2, 3, 4, 5} },
}

// hostingKeys are the hostings as kind keys, for a page that draws the
// mark of whichever hosting it is handed.
func hostingKeys() []string {
	out := make([]string, len(catalog.Hostings))
	for i, h := range catalog.Hostings {
		out[i] = string(h)
	}
	return out
}

// whoOf says who may open an application: Entra's rule when it signs in
// through Entra apps and is not open to everyone; else the roles, groups,
// and people it names, or everyone signed in when it names none. It counts
// people, never names them.
func whoOf(det catalog.AppDetails, entra []catalog.EntraApp, acc catalog.Access) string {
	if len(det.SignIn) > 0 && det.Access != catalog.AccessOpen {
		byKey := make(map[string]catalog.EntraApp, len(entra))
		for _, e := range entra {
			byKey[e.Key] = e
		}
		return signInText(acc.SignInOfApps(det.SignIn, byKey))
	}
	var parts []string
	if len(det.Audience) > 0 {
		parts = append(parts, "Roles: "+strings.Join(det.Audience, ", "))
	}
	if len(det.AccessGroups) > 0 {
		names := make([]string, len(det.AccessGroups))
		for i, k := range det.AccessGroups {
			names[i] = k
			if g, ok := acc.EntraGroup(k); ok {
				names[i] = g.Name
			}
		}
		parts = append(parts, "Groups: "+strings.Join(names, ", "))
	}
	switch n := len(det.People); n {
	case 0:
	case 1:
		parts = append(parts, "1 person")
	default:
		parts = append(parts, fmt.Sprintf("%d people", n))
	}
	if len(parts) == 0 {
		return "Everyone signed in"
	}
	return strings.Join(parts, "; ")
}

// modeText is an access mode in words.
func modeText(m catalog.AccessMode) string {
	switch m {
	case catalog.AccessOpen:
		return "Open to everyone"
	case catalog.AccessDirect:
		return "Assign people to its Entra app"
	case catalog.AccessGroup:
		return "Add people to an access group"
	case catalog.AccessPackage:
		return "An Entra access package"
	}
	return "Not set"
}

// listedText is where My Apps shows an application, in words.
func listedText(l catalog.Listed) string {
	switch l {
	case catalog.Shown:
		return "Shown"
	case catalog.NoLogin:
		return "Hidden from login"
	case catalog.Hidden:
		return "Hidden"
	}
	return "Not set"
}

// servesOf is where an app sits, the top capability and, when it sits
// under a child, that child: "Platform · Operate".
func servesOf(top string, names map[string]string, caps []string) string {
	for _, k := range caps {
		if n := names[k]; n != "" && n != top {
			return top + " · " + n
		}
	}
	return top
}

// hostingWord is where an application runs, in a word or two.
func hostingWord(h catalog.Hosting) string {
	switch h {
	case catalog.SelfHosted:
		return "Self-hosted"
	case catalog.SaaS:
		return "SaaS"
	case catalog.Installed:
		return "Installed"
	}
	return ""
}

// signInText says who Entra lets sign in to an app.
func signInText(s catalog.SignIn) string {
	if s.Everyone {
		return "Everyone in the tenant"
	}
	var parts []string
	if n := len(s.Groups); n > 0 {
		parts = append(parts, plural(n, "group", "groups"))
	}
	if n := len(s.Users); n > 0 {
		parts = append(parts, plural(n, "person", "people"))
	}
	if len(parts) == 0 {
		return "Nobody assigned yet"
	}
	return "Assigned: " + strings.Join(parts, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// markWords are what each integration mark says when set, the verb for
// what backs it, and what an unset mark says.
var markWords = map[string]struct{ on, verb, off string }{
	"data":       {"Pushes data", "wrote", "Orrery saw it write no data"},
	"blueprints": {"Pushes blueprints", "offered", "Orrery saw it offer no blueprints"},
	"actions":    {"Pushes actions", "put", "Orrery saw it put no actions"},
}

// markTip is an integration mark's tooltip: what it is and what backs it,
// or that Orrery saw nothing of the kind.
func markTip(title string, m catalog.IntegrationMark) string {
	w := markWords[m.Kind.Key]
	if !m.On() {
		return title + "\n" + w.off
	}
	from := "source "
	if len(m.Evidence.Sources) > 1 {
		from = "sources "
	}
	return title + ": " + w.on + "\n" + w.verb + " " + strings.Join(m.Evidence.Items, ", ") + "\nfrom " + from + strings.Join(m.Evidence.Sources, ", ")
}

// countTip is a category's hint in the analysis: how many apps name it, and
// which.
func countTip(c catalog.CategoryCount) string {
	apps := "apps"
	if c.Count == 1 {
		apps = "app"
	}
	tip := c.Category.Name + ": " + strconv.Itoa(c.Count) + " " + apps
	if c.Count > 0 {
		tip += ", " + strings.Join(c.Titles, ", ")
	}
	return tip
}

// markText is the words for a matrix mark.
func markText(m catalog.Mark) string {
	switch m {
	case catalog.MarkServes:
		return "Serves"
	case catalog.MarkPlanned:
		return "Planned"
	}
	return ""
}

// hue gives each app a steady colour from its key, so a tile looks the same
// on every visit without anyone choosing a colour.
func hue(key string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % 360)
}

// initial is the monogram on a tile without a picture.
func initial(title string) string {
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return string(unicode.ToUpper(r))
		}
	}
	if r, size := utf8.DecodeRuneInString(title); size > 0 {
		return string(unicode.ToUpper(r))
	}
	return "?"
}

// host is the short address under a tile.
func host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}

// tileTip is a tile's tooltip: its title, what it is, and its tier and
// owner, one to a line.
func tileTip(title, desc, tier, owner string) string {
	lines := []string{title}
	if desc != "" {
		lines = append(lines, desc)
	}
	var meta []string
	if tier != "" {
		meta = append(meta, "Tier: "+tier)
	}
	if owner != "" {
		meta = append(meta, "Owner: "+owner)
	}
	if len(meta) > 0 {
		lines = append(lines, strings.Join(meta, ", "))
	}
	return strings.Join(lines, "\n")
}

// actionTip is an action's tooltip: its title, what it does, its group,
// and whether a run waits for approval.
func actionTip(title, desc, group string, approval bool) string {
	lines := []string{title}
	if desc != "" {
		lines = append(lines, desc)
	}
	if group != "" {
		lines = append(lines, "Group: "+group)
	}
	if approval {
		lines = append(lines, "A run needs approval")
	}
	return strings.Join(lines, "\n")
}

// statusWord is a status in one word, for the pill a tile shows when it
// is not healthy.
func statusWord(s catalog.Status) string {
	switch s {
	case catalog.Green:
		return "Healthy"
	case catalog.Amber:
		return "Degraded"
	case catalog.Red:
		return "Down"
	}
	return "Unknown"
}

// statusText is the dot's tooltip.
func statusText(s catalog.Status, reasons []string) string {
	var text string
	switch s {
	case catalog.Green:
		text = "Healthy"
	case catalog.Amber:
		text = "Needs attention"
	case catalog.Red:
		text = "Down or failing"
	default:
		text = "Status unknown"
	}
	if len(reasons) > 0 {
		text += ": " + strings.Join(reasons, "; ")
	}
	return text
}

// home is the category a card belongs to first: Other when it names none.
func home(keys []string) string {
	if len(keys) == 0 {
		return catalog.OtherKey
	}
	return keys[0]
}

// assessForm is what the assessment form of one category needs: the lens
// it is saved under and the category as rated so far.
type assessForm struct {
	Lens   string
	Rating catalog.Rated
}

func assessOf(lens string, r catalog.Rated) assessForm { return assessForm{Lens: lens, Rating: r} }
