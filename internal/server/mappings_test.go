package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/orchard-suite/myapps/internal/catalog"
)

// withHub gives the harness a hub of these blueprints, where applications
// already link to Entra apps through sign_in.
func withHub(t *testing.T) *harness {
	h := withProducts(t)
	h.cov.c.Blueprints = []catalog.Blueprint{
		{ID: "entra_user", Title: "Entra user", Fields: map[string]catalog.BlueprintBit{"email": {}, "enabled": {}}},
		{ID: "person", Title: "Person", Fields: map[string]catalog.BlueprintBit{"email": {}, "employment": {}}},
		{ID: "entra_app", Title: "Entra app", Fields: map[string]catalog.BlueprintBit{"app_id": {}}},
		{ID: "enterprise_app", Title: "Enterprise app", Fields: map[string]catalog.BlueprintBit{"app_id": {}}},
		{ID: catalog.ApplicationBlueprint, Title: "Application", Relations: map[string]catalog.BlueprintBit{"sign_in": {Target: "entra_app"}}},
	}
	return h
}

func TestSourcesPageShowsWhereEachKindIsRead(t *testing.T) {
	h := withHub(t)
	rec := h.as(http.MethodGet, "/admin/sources", "admin@example.com", "Admin", nil, "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `action="/admin/sources/people"`) || !strings.Contains(body, `<option value="entra_user" selected>`) {
		t.Fatalf("every kind has a form, on its blueprint: %d", rec.Code)
	}
	if !strings.Contains(body, `name="value.enabled.false"`) {
		t.Fatal("a value the hub may word otherwise can be set")
	}
	if rec := h.as(http.MethodGet, "/admin/sources", "staff@example.com", "Platform", nil, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("only an admin maps sources: %d", rec.Code)
	}
}

func TestSavingAMappingToANewBlueprint(t *testing.T) {
	h := withHub(t)
	rec := h.as(http.MethodPost, "/admin/sources/people", "admin@example.com", "Admin", url.Values{"blueprint": {"person"}, "part.email": {"made-up"}, "value.enabled.false": {"departed"}}, own)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("saving answered %d: %s", rec.Code, rec.Body)
	}
	got := h.curator.mappings[catalog.MapPeople]
	if got.Blueprint != "person" || got.Name("email") != "email" || got.Name("enabled") != "" || got.Value("enabled", "false") != "departed" {
		t.Fatalf("a new blueprint keeps the parts named alike in it and leaves the rest to pick: %+v", got)
	}
	if n := len(h.curator.offered); n == 0 || h.curator.offered[n-1]["id"] != catalog.ApplicationBlueprint {
		t.Fatal("applications are offered their links again")
	}
	h.curator.mappings = nil
	rec = h.as(http.MethodPost, "/admin/sources/people", "admin@example.com", "Admin", url.Values{"blueprint": {""}}, own)
	if rec.Code != http.StatusSeeOther || h.curator.mappings[catalog.MapPeople].Blueprint != "" {
		t.Fatalf("a kind can be left unread: %d", rec.Code)
	}
}

func TestAMappingTheHubCannotHoldIsRefused(t *testing.T) {
	h := withHub(t)
	rec := h.as(http.MethodPost, "/admin/sources/apps", "admin@example.com", "Admin", url.Values{"blueprint": {"enterprise_app"}}, own)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "keeps a relation") || h.curator.mappings != nil {
		t.Fatalf("applications already link to entra_app, which Orrery will not repoint: %d", rec.Code)
	}
	rec = h.as(http.MethodPost, "/admin/sources/people", "admin@example.com", "Admin", url.Values{"blueprint": {"entra_user"}, "part.email": {"made-up"}}, own)
	if rec.Code != http.StatusBadRequest || h.curator.mappings != nil {
		t.Fatalf("a field the blueprint lacks is refused: %d", rec.Code)
	}
}
