package server

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/orchard-suite/platformkit/door"

	"github.com/orchard-suite/myapps/internal/catalog"
	"github.com/orchard-suite/myapps/internal/graft"
)

// A signed-in person reports a problem with an application, or asks for a
// change to it, and sees what became of everything they sent. The request
// desk, Graft, does the work; this page only hands it the person's words
// and reads their requests back. The person is always the one the front
// door signed in: nothing the browser sends names them.

const requestsPath = "/requests"

// maxRequestForm bounds the form a send carries: the desk's longest text,
// form-encoded, with room to spare.
const maxRequestForm = 128 << 10

// maxUserAgent is how much of a browser's name is passed on.
const maxUserAgent = 300

// requestsOn says whether the request desk is wired: a desk to call, and
// the portal's own origin to tell its pages from another site's.
func (s *Server) requestsOn() bool { return s.desk != nil && s.cfg.PublicURL != "" }

// requestForm is the form as it is drawn again after a send that did not
// go: what was picked, what was written, and why it did not go.
type requestForm struct {
	App, Text, Error string
}

// requestView is one request as the list draws it.
type requestView struct {
	graft.Request
	// About is the application it was sent about, by name when it is one
	// of the person's, else its repository.
	About string
}

// reportable are the person's tiles whose repository the catalog knows:
// the applications the desk can be told about.
func (s *Server) reportable(r *http.Request) []catalog.App {
	_, apps := s.visible(r)
	return slices.DeleteFunc(slices.Clone(apps), func(a catalog.App) bool { return a.Repository == "" })
}

func (s *Server) pageRequests(w http.ResponseWriter, r *http.Request) {
	s.drawRequests(w, r, http.StatusOK, requestForm{App: r.URL.Query().Get("app")})
}

// drawRequests draws the form and the person's requests. A view as someone
// else draws neither: the desk is only ever asked about the person the
// front door signed in.
func (s *Server) drawRequests(w http.ResponseWriter, r *http.Request, status int, form requestForm) {
	data := map[string]any{"Form": form, "Sent": r.Method == http.MethodGet && r.URL.Query().Get("sent") != ""}
	if _, viewing := viewingFrom(r.Context()); viewing {
		data["Viewing"] = true
		s.render(w, r, status, "requests", data)
		return
	}
	apps := s.reportable(r)
	names := make(map[string]string, len(apps))
	for _, a := range apps {
		if _, taken := names[a.Repository]; !taken {
			names[a.Repository] = a.Title
		}
	}
	list, err := s.desk.List(r.Context(), door.From(r.Context()).Email)
	if err != nil {
		s.log.WarnContext(r.Context(), "requests read failed", "error", err)
		data["ListFailed"] = true
	}
	views := make([]requestView, 0, len(list))
	for _, q := range list {
		about := q.Repo
		if name, ok := names[q.Repo]; ok {
			about = name
		}
		views = append(views, requestView{Request: q, About: about})
	}
	data["Apps"] = apps
	data["Mine"] = views
	s.render(w, r, status, "requests", data)
}

// sendRequest hands the desk the person's words about one of their
// applications. The form names the application and carries the words, and
// nothing else in it is read. What the desk refuses in words for the
// person is drawn over the form, with what they wrote kept.
func (s *Server) sendRequest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestForm)
	if err := r.ParseForm(); err != nil {
		s.drawRequests(w, r, http.StatusBadRequest, requestForm{Error: "The form could not be read. It may be too long."})
		return
	}
	form := requestForm{App: r.PostForm.Get("app"), Text: strings.TrimSpace(r.PostForm.Get("text"))}
	apps := s.reportable(r)
	i := slices.IndexFunc(apps, func(a catalog.App) bool { return a.Key == form.App })
	if i < 0 {
		s.log.WarnContext(r.Context(), "request refused: not an app this person may report on")
		form.Error = "Pick one of your apps."
		s.drawRequests(w, r, http.StatusBadRequest, form)
		return
	}
	app := apps[i]
	agent := r.UserAgent()
	if len(agent) > maxUserAgent {
		agent = agent[:maxUserAgent]
	}
	made, err := s.desk.Send(r.Context(), graft.Report{
		Person:     door.From(r.Context()).Email,
		Text:       form.Text,
		Repository: app.Repository,
		URL:        app.URL,
		UserAgent:  agent,
	})
	if refusal, ok := errors.AsType[*graft.Refusal](err); ok {
		s.log.InfoContext(r.Context(), "request refused by the desk", "app", app.Key, "status", refusal.Status)
		form.Error = refusal.Words
		s.drawRequests(w, r, refusal.Status, form)
		return
	}
	if err != nil {
		s.log.ErrorContext(r.Context(), "request send failed", "app", app.Key, "error", err)
		form.Error = "The request desk could not be reached, so nothing was sent. Your words are still here. Try again in a minute."
		s.drawRequests(w, r, http.StatusBadGateway, form)
		return
	}
	s.log.InfoContext(r.Context(), "request sent", "app", app.Key, "request", made.ID)
	http.Redirect(w, r, requestsPath+"?sent=1#mine", http.StatusSeeOther)
}
