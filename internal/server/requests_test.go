package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchard-suite/myapps/internal/catalog"
	"github.com/orchard-suite/myapps/internal/graft"
)

// fakeDesk is a Graft token port: it keeps what it was sent and who it
// was asked about, and answers what the test sets.
type fakeDesk struct {
	sent    []map[string]any
	asked   []string
	tokens  []string
	status  int
	refusal string
	list    string
}

func (f *fakeDesk) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.tokens = append(f.tokens, r.Header.Get("Authorization"))
	if r.URL.Path != "/v1/requests" {
		http.NotFound(w, r)
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": f.refusal})
		return
	}
	if r.Method == http.MethodGet {
		f.asked = append(f.asked, r.URL.Query().Get("person"))
		_, _ = io.WriteString(w, f.list)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.sent = append(f.sent, body)
	w.WriteHeader(http.StatusCreated)
	_, _ = io.WriteString(w, `{"id":"r9","state":"received"}`)
}

// requestApps are tiles: the portal and the ledger name a repository, the
// lab does not, and the ledger is only for Finance.
var requestApps = []catalog.App{
	{Key: "portal", Title: "Portal", URL: "https://portal.example.com", Repository: "acme/portal", Status: catalog.Green},
	{Key: "ledger", Title: "Ledger", URL: "https://ledger.example.com", Repository: "acme/ledger", Audience: []string{"Finance"}, Status: catalog.Green},
	{Key: "lab", Title: "Lab", URL: "https://lab.example.com", Status: catalog.Green},
}

// withDesk is a harness wired to a fake desk.
func withDesk(t *testing.T) (*harness, *fakeDesk) {
	t.Helper()
	desk := &fakeDesk{list: "[]"}
	srv := httptest.NewServer(desk)
	t.Cleanup(srv.Close)
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("intake-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarnessWith(t, "", func(_ *Config, p *Parts) {
		client, err := graft.New(srv.URL, file, p.Metrics.GraftCall)
		if err != nil {
			t.Fatal(err)
		}
		p.Desk = client
	})
	h.src.apps = requestApps
	return h, desk
}

const problem = "The page stays blank after I sign in."

func TestThePersonIsTheFrontDoorsNeverTheForms(t *testing.T) {
	h, desk := withDesk(t)
	form := url.Values{
		"app": {"portal"}, "text": {"  " + problem + "  "},
		// Everything below is a browser trying to say more than its words.
		"person": {"other@example.com"}, "email": {"other@example.com"}, "repository": {"acme/ledger"}, "lane": {"staff"}, "url": {"https://elsewhere.example.com"}, "context": {"x"},
	}
	rec := h.as(http.MethodPost, "/requests?person=other@example.com", "user@example.com", "", form, own)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/requests?sent=1#mine" {
		t.Fatalf("a send answered %d to %q: %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if len(desk.sent) != 1 {
		t.Fatalf("the desk got %d requests", len(desk.sent))
	}
	got := desk.sent[0]
	about, _ := got["context"].(map[string]any)
	if got["person"] != "user@example.com" || got["text"] != problem || got["repository"] != "acme/portal" || about["url"] != "https://portal.example.com" {
		t.Fatalf("the desk got %v", got)
	}
	if _, has := got["lane"]; has || len(got) != 4 || len(about) != 1 {
		t.Fatalf("only the person, their words, the repository, and the app's address are sent: %v", got)
	}
	if desk.tokens[0] != "Bearer intake-token" {
		t.Fatalf("the desk is called with the token in the file, got %q", desk.tokens[0])
	}
}

func TestRequestsAreOffWithoutADesk(t *testing.T) {
	for name, h := range map[string]*harness{
		"no desk": newHarness(t, ""),
		"no public url": func() *harness {
			h, _ := withDesk(t)
			h.srv.cfg.PublicURL = ""
			h.session = h.srv.Session()
			return h
		}(),
	} {
		if rec := h.as(http.MethodGet, "/requests", "user@example.com", "", nil, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: the page answered %d", name, rec.Code)
		}
		if rec := h.as(http.MethodPost, "/requests", "user@example.com", "", url.Values{"app": {"portal"}, "text": {problem}}, own); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: a send answered %d", name, rec.Code)
		}
		if body := h.as(http.MethodGet, "/", "user@example.com", "", nil, "").Body.String(); strings.Contains(body, "/requests") || strings.Contains(body, "Report a problem") {
			t.Fatalf("%s: the menu offers requests", name)
		}
	}
	h, _ := withDesk(t)
	body := h.as(http.MethodGet, "/", "user@example.com", "", nil, "").Body.String()
	for _, want := range []string{`<a class="mitem" href="/requests" role="menuitem">Report a problem</a>`, `<a class="mitem" href="/requests#mine" role="menuitem">My requests</a>`, `{"title":"Report a problem","url":"/requests"}`} {
		if !strings.Contains(body, want) {
			t.Fatalf("with a desk the menu lacks %q", want)
		}
	}
}

func TestTheDesksWordsReachThePage(t *testing.T) {
	h, desk := withDesk(t)
	for status, words := range map[int]string{
		http.StatusBadRequest:      "Say a little more: at least 10 characters.",
		http.StatusForbidden:       "You hold no lane on that repository.",
		http.StatusTooManyRequests: "You have sent too many this hour.",
	} {
		desk.status, desk.refusal = status, words
		rec := h.as(http.MethodPost, "/requests", "user@example.com", "", url.Values{"app": {"portal"}, "text": {problem}}, own)
		body := rec.Body.String()
		if rec.Code != status || !strings.Contains(body, `<p class="reqerr" role="alert">`+words+`</p>`) {
			t.Fatalf("a %d from the desk answered %d without its words: %s", status, rec.Code, body)
		}
		if !strings.Contains(body, problem+"</textarea>") || !strings.Contains(body, `<option value="portal" selected>Portal</option>`) {
			t.Fatalf("what the person wrote and picked is kept: %s", body)
		}
	}
	// A refused token is not the person's to read.
	desk.status, desk.refusal = http.StatusUnauthorized, "no such token"
	rec := h.as(http.MethodPost, "/requests", "user@example.com", "", url.Values{"app": {"portal"}, "text": {problem}}, own)
	if body := rec.Body.String(); rec.Code != http.StatusBadGateway || strings.Contains(body, "no such token") || !strings.Contains(body, "could not be reached") {
		t.Fatalf("a refused token answered %d: %s", rec.Code, body)
	}
}

func TestTheListIsWhatTheDeskReturned(t *testing.T) {
	h, desk := withDesk(t)
	desk.list = `[
	 {"id":"r2","createdAt":"2026-03-02T10:00:00Z","state":"filed","ended":true,"repo":"acme/portal","title":"Blank page after sign-in","answer":"A person will look at this.","ticket":{"number":7,"url":"https://git.example.com/acme/portal/issues/7"}},
	 {"id":"r1","createdAt":"2026-03-01T10:00:00Z","state":"investigating","repo":"acme/other","text":"Search is slow.\nSince Monday."}
	]`
	rec := h.as(http.MethodGet, "/requests?person=other@example.com", "user@example.com", "", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("the page answered %d", rec.Code)
	}
	if len(desk.asked) != 1 || desk.asked[0] != "user@example.com" {
		t.Fatalf("the desk is asked about the signed-in person only, got %v", desk.asked)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<span class="dim">2</span>`,
		`<li class="req" data-request="r2">`, "<h3>Blank page after sign-in</h3>", `<span class="rstate done">Passed to a person as a ticket</span><span>Portal</span><time datetime="2026-03-02T10:00:00Z">2 Mar 2026</time>`,
		`<p class="ranswer">A person will look at this.</p>`, `<a href="https://git.example.com/acme/portal/issues/7" target="_blank" rel="noopener">See the ticket (number 7)</a>`,
		`<li class="req" data-request="r1">`, "<h3>Search is slow.</h3>", `<span class="rstate working">Being looked into</span><span>acme/other</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the list lacks %q: %s", want, body)
		}
	}
	if strings.Count(body, `<li class="req"`) != 2 || strings.Index(body, `data-request="r2"`) > strings.Index(body, `data-request="r1"`) {
		t.Fatal("the list is the desk's two requests, in its order")
	}
	// The form offers only the person's tiles that name a repository.
	if !strings.Contains(body, `<option value="portal">Portal</option>`) || strings.Contains(body, `value="lab"`) || strings.Contains(body, `value="ledger"`) {
		t.Fatalf("the form offers the wrong apps: %s", body)
	}
	// A failed read says so and lists nothing.
	desk.status = http.StatusInternalServerError
	body = h.as(http.MethodGet, "/requests", "user@example.com", "", nil, "").Body.String()
	if !strings.Contains(body, "Your requests could not be read.") || strings.Contains(body, `<li class="req"`) {
		t.Fatalf("a failed read: %s", body)
	}
}

func TestASendMustBeForOneOfThePersonsAppsFromThePortalsOwnPage(t *testing.T) {
	h, desk := withDesk(t)
	for _, app := range []string{"ledger", "lab", "nope", ""} {
		rec := h.as(http.MethodPost, "/requests", "user@example.com", "", url.Values{"app": {app}, "text": {problem}}, own)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Pick one of your apps.") {
			t.Fatalf("a send about %q answered %d", app, rec.Code)
		}
	}
	if rec := h.as(http.MethodPost, "/requests", "user@example.com", "", url.Values{"app": {"portal"}, "text": {problem}}, "https://elsewhere.example.com"); rec.Code != http.StatusForbidden {
		t.Fatalf("a send from another site answered %d", rec.Code)
	}
	if rec := h.as(http.MethodPost, "/requests", "user@example.com", "", url.Values{"app": {"portal"}, "text": {problem}}, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("a send with no origin answered %d", rec.Code)
	}
	if len(desk.sent) != 0 {
		t.Fatalf("the desk heard %d refused sends", len(desk.sent))
	}
	// Finance may report on the ledger.
	if rec := h.as(http.MethodPost, "/requests", "finance@example.com", "Finance", url.Values{"app": {"ledger"}, "text": {problem}}, own); rec.Code != http.StatusSeeOther || len(desk.sent) != 1 || desk.sent[0]["repository"] != "acme/ledger" {
		t.Fatalf("finance's send answered %d, the desk got %v", rec.Code, desk.sent)
	}
}

func TestAViewAsSomeoneNeverAsksTheDesk(t *testing.T) {
	h, desk := withDesk(t)
	rec := h.asViewing(http.MethodGet, "/requests", "admin@example.com", "Admin", "user@example.com", nil)
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, "not shown while you view as someone else") || strings.Contains(body, "<form method=\"post\" action=\"/requests\">") {
		t.Fatalf("a view answered %d: %s", rec.Code, body)
	}
	if rec := h.asViewing(http.MethodPost, "/requests", "admin@example.com", "Admin", "user@example.com", url.Values{"app": {"portal"}, "text": {problem}}); rec.Code != http.StatusForbidden {
		t.Fatalf("a send while viewing answered %d", rec.Code)
	}
	if len(desk.asked) != 0 || len(desk.sent) != 0 {
		t.Fatalf("the desk was asked %v and sent %v", desk.asked, desk.sent)
	}
}
