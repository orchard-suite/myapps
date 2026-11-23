package catalog

import (
	"net/url"
	"slices"
	"strings"

	"github.com/orchard-suite/platformkit/orrery"
)

// BuiltOrigin is the origin of a service the organization built itself.
const BuiltOrigin = "built"

// RepositoryPath is a repository's path as its forge names it, such as
// acme/portal, read from its address: https://git.example.com/acme/portal,
// with or without .git, or git@git.example.com:acme/portal.git. An address
// with no path is no repository.
func RepositoryPath(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	path := ""
	if u, err := url.Parse(address); err == nil && u.Scheme != "" && u.Host != "" {
		path = u.Path
	} else if _, rest, ok := strings.Cut(address, ":"); ok && strings.Contains(address, "@") && !strings.Contains(address, "://") {
		path = rest
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if path == "" || strings.ContainsAny(path, " \t?#") {
		return ""
	}
	return path
}

// WithRepositories sets every application's Repository: the repository of
// the first service it runs that the organization built and that names
// one. A service someone else built is never it: its repository is theirs.
func WithRepositories(apps []Application, services []orrery.Record) []Application {
	repos := make(map[string]string, len(services))
	for _, s := range services {
		if s.Hidden || s.String("origin") != BuiltOrigin {
			continue
		}
		if path := RepositoryPath(s.String("repository_url")); path != "" {
			repos[s.Key] = path
		}
	}
	out := slices.Clone(apps)
	for i := range out {
		for _, k := range out[i].Services {
			if path := repos[k]; path != "" {
				out[i].Repository = path
				break
			}
		}
	}
	return out
}
