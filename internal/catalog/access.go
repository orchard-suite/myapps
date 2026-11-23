package catalog

import (
	"slices"
	"strings"

	"github.com/orchard-suite/platformkit/orrery"
)

// Person is someone as Entra knows them: their entra_user key and the
// groups they are directly in.
type Person struct {
	ID     string
	Groups []string
}

// SignIn is who Entra lets sign in to an app.
type SignIn struct {
	// Everyone is true when the app does not require an assignment, so
	// anyone in the tenant may sign in.
	Everyone bool
	// Users and Groups are the entra_user and entra_group keys assigned to
	// the app.
	Users  []string
	Groups []string
}

// Admits says whether Entra lets this person sign in. A person Entra does
// not know (nil) gets only the apps open to everyone. Entra does not pass
// app access through nested groups, so only direct groups count.
func (s SignIn) Admits(p *Person) bool {
	if s.Everyone {
		return true
	}
	if p == nil {
		return false
	}
	if slices.Contains(s.Users, p.ID) {
		return true
	}
	return slices.ContainsFunc(p.Groups, func(g string) bool { return slices.Contains(s.Groups, g) })
}

// EntraGroup is an Entra group a person may be added to.
type EntraGroup struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Dynamic bool   `json:"dynamic,omitempty"`
	Members int    `json:"members"`
}

// Access is who holds what in Entra, as Orrery's Entra pusher wrote it:
// each app's assignments, each group's direct members, and each user's
// email. It names people, so it never leaves the server.
type Access struct {
	byApp map[string]SignIn
	// byEmail finds a person by any of their addresses; emails are their
	// main ones.
	byEmail map[string]string
	emails  []string
	groupOf map[string][]string
	groups  map[string]EntraGroup
}

// AccessOf reads the entra_user, entra_group, and entra_app_role_assignment
// entities. A person is found by their email or any of their aliases; an
// alias never takes another person's email. An assignment to a user or
// group Orrery does not hold still counts, by key.
func AccessOf(users, groups, assignments []orrery.Record) Access {
	a := Access{byApp: map[string]SignIn{}, byEmail: map[string]string{}, groupOf: map[string][]string{}, groups: map[string]EntraGroup{}}
	users = slices.DeleteFunc(slices.Clone(users), func(u orrery.Record) bool {
		enabled, set := u.Bool("enabled")
		return u.Hidden || set && !enabled
	})
	for _, u := range users {
		if email := strings.ToLower(strings.TrimSpace(u.String("email"))); email != "" {
			a.byEmail[email] = u.Key
			a.emails = append(a.emails, email)
		}
	}
	for _, u := range users {
		for _, alias := range u.Strings("aliases") {
			if alias = strings.ToLower(strings.TrimSpace(alias)); alias != "" {
				if _, taken := a.byEmail[alias]; !taken {
					a.byEmail[alias] = u.Key
				}
			}
		}
	}
	for _, g := range groups {
		if g.Hidden {
			continue
		}
		dynamic, _ := g.Bool("dynamic")
		members := refKeys(g.Relations["members"])
		a.groups[g.Key] = EntraGroup{Key: g.Key, Name: g.Title, Dynamic: dynamic, Members: len(members)}
		for _, m := range members {
			a.groupOf[m] = append(a.groupOf[m], g.Key)
		}
	}
	for _, r := range assignments {
		if r.Hidden {
			continue
		}
		app, ok := r.Related("app")
		if !ok {
			continue
		}
		s := a.byApp[app.Key]
		if u, ok := r.Related("user"); ok && !slices.Contains(s.Users, u.Key) {
			s.Users = append(s.Users, u.Key)
		}
		if g, ok := r.Related("group"); ok && !slices.Contains(s.Groups, g.Key) {
			s.Groups = append(s.Groups, g.Key)
		}
		a.byApp[app.Key] = s
	}
	return a
}

// SignInOfApps is who may open an application through its Entra apps:
// anyone, when any of them needs no assignment; else everyone assigned to
// any of them.
func (a Access) SignInOfApps(keys []string, entra map[string]EntraApp) SignIn {
	var s SignIn
	for _, k := range keys {
		if e, ok := entra[k]; ok && !e.Required {
			s.Everyone = true
		}
		got := a.byApp[k]
		for _, u := range got.Users {
			if !slices.Contains(s.Users, u) {
				s.Users = append(s.Users, u)
			}
		}
		for _, g := range got.Groups {
			if !slices.Contains(s.Groups, g) {
				s.Groups = append(s.Groups, g)
			}
		}
	}
	return s
}

// PersonFor is the person the front door's email names, or nil when Entra
// does not know them.
func (a Access) PersonFor(email string) *Person {
	id, ok := a.byEmail[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return nil
	}
	return &Person{ID: id, Groups: a.groupOf[id]}
}

// Emails are the main emails of every person Entra holds, in order: who
// an admin may view the portal as.
func (a Access) Emails() []string {
	out := slices.Clone(a.emails)
	slices.Sort(out)
	return slices.Compact(out)
}

// EntraGroup is one group by key, and whether Orrery holds it.
func (a Access) EntraGroup(key string) (EntraGroup, bool) {
	g, ok := a.groups[key]
	return g, ok
}

// Groups are every Entra group Orrery holds, by name.
func (a Access) Groups() []EntraGroup {
	out := make([]EntraGroup, 0, len(a.groups))
	for _, g := range a.groups {
		out = append(out, g)
	}
	slices.SortFunc(out, func(x, y EntraGroup) int { return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)) })
	return out
}

// GroupsOf are the groups assigned to any of these Entra apps, by name:
// where a group request may add a person.
func (a Access) GroupsOf(keys []string) []EntraGroup {
	var out []EntraGroup
	seen := map[string]bool{}
	for _, k := range keys {
		for _, g := range a.byApp[k].Groups {
			if seen[g] {
				continue
			}
			seen[g] = true
			if got, ok := a.groups[g]; ok {
				out = append(out, got)
			} else {
				out = append(out, EntraGroup{Key: g, Name: g})
			}
		}
	}
	slices.SortFunc(out, func(x, y EntraGroup) int { return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)) })
	return out
}
