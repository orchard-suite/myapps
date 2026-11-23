package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/orchard-suite/myapps/internal/catalog"
	"github.com/orchard-suite/platformkit/door"
)

// An admin may see the portal as another person, read-only, to check that
// the access rules do what was meant. Starting it sets a cookie; while it
// is set, viewAs makes every request on the session port that person's
// before any gate reads it, so Home, the Landscape, /open, the APIs, and
// the pages a role opens answer as they would for them. The cookie counts
// only while the front door says the real caller is an admin.

const (
	viewAsCookie = "myapps_view_as"
	viewAsPath   = "/view-as"
	viewAsExit   = "/view-as/exit"
	// viewAsHours is how long a view lasts before it ends on its own.
	viewAsHours = 8
)

// viewing is a view as another person: who looks, and as whom.
type viewing struct {
	By    string
	Email string
	Roles []string
}

// RolesText is the roles the view gives, in words.
func (v viewing) RolesText() string {
	if len(v.Roles) == 0 {
		return "No front-door roles."
	}
	return "Roles: " + strings.Join(v.Roles, ", ") + "."
}

type viewingKey struct{}

// viewingFrom is the view a request is made in, if any.
func viewingFrom(ctx context.Context) (viewing, bool) {
	v, ok := ctx.Value(viewingKey{}).(viewing)
	return v, ok
}

// encodeViewing is the cookie's value: the email, then the roles.
func encodeViewing(email string, roles []string) string {
	return url.Values{"email": {email}, "roles": {strings.Join(roles, ",")}}.Encode()
}

// decodeViewing reads the cookie's value, and whether it names a person.
func decodeViewing(raw string) (string, []string, bool) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return "", nil, false
	}
	email, ok := catalog.Email(q.Get("email"))
	return email, door.ParseRoles(q.Get("roles")), ok
}

// viewAs puts the viewed person on a request from a real admin holding the
// cookie. Starting and ending a view are the admin's own; anything else
// that is not a read is refused. A cookie anyone else holds is dropped.
func (s *Server) viewAs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(viewAsCookie)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		real, signed := door.Read(r)
		email, roles, ok := decodeViewing(c.Value)
		if !signed || !real.Has(AdminRole) || !ok {
			s.log.WarnContext(r.Context(), "view as dropped", "actor", real.Email, "admin", real.Has(AdminRole), "readable", ok)
			s.clearViewing(w)
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == viewAsPath || r.URL.Path == viewAsExit {
			next.ServeHTTP(w, r)
			return
		}
		v := viewing{By: real.Email, Email: email, Roles: roles}
		ctx := context.WithValue(r.Context(), viewingKey{}, v)
		as := r.Clone(ctx)
		as.Header.Set(door.HeaderEmail, email)
		as.Header.Set(door.HeaderGroups, strings.Join(roles, ","))
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			s.log.WarnContext(ctx, "view as refused: a change", "actor", real.Email, "viewing", email, "method", r.Method, "path", r.URL.Path)
			s.message(w, as.WithContext(door.With(ctx, door.Person{Email: email, Roles: roles})), http.StatusForbidden, "Viewing as someone else", "Nothing changes while you view as "+email+". Exit to make a change.")
			return
		}
		s.log.DebugContext(ctx, "viewing as", "actor", real.Email, "viewing", email, "path", r.URL.Path)
		next.ServeHTTP(w, as)
	})
}

// startViewing begins a view as the person a form names, with the
// front-door roles it gives them, for an admin.
func (s *Server) startViewing(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.message(w, r, http.StatusBadRequest, "That did not work", "The form could not be read.")
		return
	}
	email, ok := catalog.Email(r.PostForm.Get("email"))
	if !ok {
		s.message(w, r, http.StatusBadRequest, "That did not work", "Name the person by their email.")
		return
	}
	roles := door.ParseRoles(r.PostForm.Get("roles"))
	http.SetCookie(w, &http.Cookie{
		Name: viewAsCookie, Value: encodeViewing(email, roles), Path: "/", MaxAge: viewAsHours * 3600,
		HttpOnly: true, Secure: strings.HasPrefix(s.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode,
	})
	s.log.InfoContext(r.Context(), "view as started", "actor", door.From(r.Context()).Email, "viewing", email, "roles", roles, "entra_known", s.cache.Get(r.Context()).Value.Access.PersonFor(email) != nil)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// endViewing ends a view, for whoever holds one.
func (s *Server) endViewing(w http.ResponseWriter, r *http.Request) {
	s.clearViewing(w)
	s.log.InfoContext(r.Context(), "view as ended", "actor", door.From(r.Context()).Email)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) clearViewing(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: viewAsCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: strings.HasPrefix(s.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode})
}

// viewOrigin takes starting and ending a view only from the portal's own
// pages. It needs the portal's public address, not a push token: a view
// writes nothing to Orrery.
func (s *Server) viewOrigin(next http.Handler) http.Handler {
	if s.cfg.PublicURL == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.message(w, r, http.StatusServiceUnavailable, "Viewing as someone is off", "This portal has no PUBLIC_URL, so it cannot tell its own pages from another site's.")
		})
	}
	return door.SameOrigin(strings.TrimRight(s.cfg.PublicURL, "/"), s.log, next)
}
