package catalog

import (
	"encoding/json"
	"testing"

	"github.com/orchard-suite/platformkit/orrery"
)

func TestRepositoryPath(t *testing.T) {
	for address, want := range map[string]string{
		"https://git.example.com/acme/portal":        "acme/portal",
		"https://git.example.com/acme/portal.git":    "acme/portal",
		"https://git.example.com/acme/tools/portal/": "acme/tools/portal",
		"git@git.example.com:acme/portal.git":        "acme/portal",
		"https://git.example.com":                    "",
		"acme/portal":                                "",
		"":                                           "",
	} {
		if got := RepositoryPath(address); got != want {
			t.Errorf("RepositoryPath(%q) = %q, want %q", address, got, want)
		}
	}
}

func service(key, origin, repo string) orrery.Record {
	fields := map[string]json.RawMessage{}
	for name, v := range map[string]string{"origin": origin, "repository_url": repo} {
		if v != "" {
			raw, _ := json.Marshal(v)
			fields[name] = raw
		}
	}
	return orrery.Record{Blueprint: ServiceBlueprint, Key: key, Fields: fields}
}

func TestAnApplicationsRepositoryIsItsFirstBuiltServices(t *testing.T) {
	services := []orrery.Record{
		service("beacon", "adopted", "https://git.example.com/upstream/beacon"),
		service("worker", "built", ""),
		service("portal", "built", "https://git.example.com/acme/portal.git"),
		service("api", "built", "https://git.example.com/acme/api"),
	}
	apps := WithRepositories([]Application{
		{Key: "portal", Services: []string{"beacon", "worker", "portal", "api"}},
		{Key: "alerts", Services: []string{"beacon"}},
		{Key: "mail"},
	}, services)
	if got := apps[0].Repository; got != "acme/portal" {
		t.Fatalf("the first built service with a repository decides, got %q", got)
	}
	if apps[1].Repository != "" || apps[2].Repository != "" {
		t.Fatalf("an adopted service's repository is not ours, and no service is no repository: %q %q", apps[1].Repository, apps[2].Repository)
	}
	tiles := Tiles([]Application{{Key: "portal", Name: "Portal", URL: "https://portal.example.com", Listed: Shown, Seen: true, Repository: "acme/portal"}}, nil, Access{})
	if len(tiles) != 1 || tiles[0].Repository != "acme/portal" {
		t.Fatalf("the tile carries the repository: %+v", tiles)
	}
}
