package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// asViewing is a request from someone holding a view-as cookie for email.
func (h *harness) asViewing(method, path, email, groups, viewed string, form url.Values) *httptest.ResponseRecorder {
	body := strings.NewReader("")
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("X-Forwarded-Email", email)
	req.Header.Set("X-Forwarded-Groups", groups)
	req.Header.Set("Origin", own)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.AddCookie(&http.Cookie{Name: viewAsCookie, Value: encodeViewing(viewed, nil)})
	rec := httptest.NewRecorder()
	h.session.ServeHTTP(rec, req)
	return rec
}

func TestAdminStartsAndEndsAView(t *testing.T) {
	h := withProducts(t)
	rec := h.as(http.MethodPost, viewAsPath, "admin@example.com", "Admin", url.Values{"email": {" Finance@Example.com "}, "roles": {"Staff, Ops"}}, own)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("starting a view answered %d: %s", rec.Code, rec.Body)
	}
	c := rec.Result().Cookies()
	if len(c) != 1 || c[0].Name != viewAsCookie || !c[0].HttpOnly || !c[0].Secure || c[0].MaxAge <= 0 {
		t.Fatalf("a view is an HttpOnly, Secure cookie that ends on its own: %+v", c)
	}
	if email, roles, ok := decodeViewing(c[0].Value); !ok || email != "finance@example.com" || strings.Join(roles, ",") != "Staff,Ops" {
		t.Fatalf("the cookie names the person and their roles: %q", c[0].Value)
	}
	if rec := h.as(http.MethodPost, viewAsPath, "staff@example.com", "", url.Values{"email": {"finance@example.com"}}, own); rec.Code != http.StatusForbidden || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("someone not an admin cannot start one: %d", rec.Code)
	}
	if rec := h.as(http.MethodPost, viewAsPath, "admin@example.com", "Admin", url.Values{"email": {"finance@example.com"}}, "https://elsewhere.example.org"); rec.Code == http.StatusSeeOther {
		t.Fatal("a view starts only from the portal's own pages")
	}
	if rec := h.as(http.MethodPost, viewAsPath, "admin@example.com", "Admin", url.Values{"email": {"not an email"}}, own); rec.Code != http.StatusBadRequest {
		t.Fatalf("a view names a person by email: %d", rec.Code)
	}
	end := h.asViewing(http.MethodPost, viewAsExit, "admin@example.com", "Admin", "finance@example.com", url.Values{})
	if end.Code != http.StatusSeeOther || len(end.Result().Cookies()) != 1 || end.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("exit clears the cookie: %d %+v", end.Code, end.Result().Cookies())
	}
}

func TestViewingAsSeesWhatTheyWouldSee(t *testing.T) {
	h := withProducts(t)
	finance := h.asViewing(http.MethodGet, "/v1/apps", "admin@example.com", "Admin", "finance@example.com", nil).Body.String()
	staff := h.asViewing(http.MethodGet, "/v1/apps", "admin@example.com", "Admin", "staff@example.com", nil).Body.String()
	if !strings.Contains(finance, `"payroll"`) || strings.Contains(staff, `"payroll"`) {
		t.Fatalf("the tiles are the viewed person's: finance %s; staff %s", finance, staff)
	}
	if rec := h.asViewing(http.MethodGet, "/open/payroll", "admin@example.com", "Admin", "staff@example.com", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("a tile they may not open is not there for them: %d", rec.Code)
	}
	home := h.asViewing(http.MethodGet, "/", "admin@example.com", "Admin", "staff@example.com", nil).Body.String()
	if !strings.Contains(home, "Viewing as staff@example.com") || !strings.Contains(home, `action="/view-as/exit"`) {
		t.Fatal("every page says whose view it is, with a way out")
	}
	if strings.Contains(home, `href="/admin"`) || strings.Contains(home, `action="/view-as"`) {
		t.Fatal("their pages show only what their roles open")
	}
	if rec := h.asViewing(http.MethodGet, "/admin", "admin@example.com", "Admin", "staff@example.com", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("a page their roles do not open is shut: %d", rec.Code)
	}
}

func TestViewingAsChangesNothing(t *testing.T) {
	h := withProducts(t)
	rec := h.asViewing(http.MethodPost, "/admin/applications/portal", "admin@example.com", "Admin", "admin2@example.com", url.Values{"hosting": {"saas"}})
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Exit to make a change") {
		t.Fatalf("a change while viewing is refused: %d", rec.Code)
	}
	if _, saved := h.curator.apps["portal"]; saved {
		t.Fatal("and nothing is saved")
	}
}

func TestAViewCountsOnlyForARealAdmin(t *testing.T) {
	h := withProducts(t)
	rec := h.asViewing(http.MethodGet, "/v1/apps", "staff@example.com", "", "finance@example.com", nil)
	if strings.Contains(rec.Body.String(), `"payroll"`) {
		t.Fatal("someone not an admin holding the cookie still sees only their own")
	}
	if c := rec.Result().Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Fatalf("and the cookie is dropped: %+v", c)
	}
}

func TestAdminMenuOffersAView(t *testing.T) {
	h := withProducts(t)
	body := h.as(http.MethodGet, "/", "admin@example.com", "Admin", nil, "").Body.String()
	if !strings.Contains(body, `action="/view-as"`) || !strings.Contains(body, `<option value="finance@example.com">`) {
		t.Fatal("an admin's menu offers a view, with the people Entra holds")
	}
	if strings.Contains(h.as(http.MethodGet, "/", "staff@example.com", "", nil, "").Body.String(), `action="/view-as"`) {
		t.Fatal("no one else's does")
	}
}
