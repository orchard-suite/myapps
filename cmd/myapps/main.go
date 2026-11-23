// Command myapps is the My Apps portal: one page of tiles, one per app in
// the platform, read from an Orrery catalog and shown to the people whose
// groups each app names.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/orchard-suite/platformkit/faro"
	"github.com/orchard-suite/platformkit/orrery"
	"github.com/orchard-suite/platformkit/telemetry"

	"github.com/orchard-suite/myapps/internal/catalog"
	"github.com/orchard-suite/myapps/internal/graft"
	"github.com/orchard-suite/myapps/internal/server"
)

// version is set at build time from VERSION.
var version = "dev"

func main() {
	log := telemetry.NewLogger(os.Stdout, server.Service, version, telemetry.ParseLevel(os.Getenv("LOG_LEVEL")))
	if err := run(log); err != nil {
		log.Error("myapps stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ttl, err := time.ParseDuration(env("CATALOG_TTL", "30s"))
	if err != nil || ttl <= 0 {
		return errors.New("CATALOG_TTL must be a positive duration such as 30s")
	}
	shutdownTelemetry, err := telemetry.Setup(ctx, server.Service, version)
	if err != nil {
		return err
	}
	curator, err := newCurator(log)
	if err != nil {
		return err
	}
	hub, err := catalog.NewOrrery(os.Getenv("ORRERY_URL"), os.Getenv("ORRERY_TOKEN"), log)
	if err != nil {
		return err
	}
	if err := startOffering(ctx, hub, log); err != nil {
		return err
	}
	var cache *catalog.Cache[catalog.Directory]
	metrics, err := server.NewMetrics(func() time.Duration { return cache.Age() })
	if err != nil {
		return err
	}
	// Every icon the tiles and cards name is read when they are, if new or
	// changed.
	icons := catalog.NewIcons(hub, log, metrics.IconRead)
	// The organization's logo is read with the tiles' icons.
	orgIcon := os.Getenv("ORG_ICON")
	if orgIcon != "" && !catalog.IconName(orgIcon) {
		return errors.New("ORG_ICON must be an icon name in Orrery, such as acme")
	}
	tileIcons := func(d catalog.Directory) []string {
		if orgIcon == "" {
			return d.IconNames()
		}
		return append(d.IconNames(), orgIcon)
	}
	cache = catalog.NewTiles(catalog.WithIcons(hub.Directory, icons, tileIcons), ttl, log, metrics.CatalogRead)
	hubURL := os.Getenv("ORRERY_PUBLIC_URL")
	coverage := catalog.NewCoverageCache(catalog.WithIcons(hub.Coverage, icons, catalog.Coverage.IconNames), ttl, log, metrics.LandscapeRead)
	faroURL, faroKey := os.Getenv("FARO_URL"), os.Getenv("FARO_API_KEY")
	relay, err := faro.NewRelay(faro.Config{Service: server.Service, Version: version, URL: faroURL, Key: faroKey}, log)
	if err != nil {
		return err
	}
	// Each person's self-service actions, as links into the hub's forms.
	actions := catalog.NewActions(hub, hubURL, ttl, log, metrics.ActionsRead)
	desk, err := newDesk(metrics, log)
	if err != nil {
		return err
	}
	srv, err := server.New(server.Config{
		Org:              env("ORG_NAME", "My Apps"),
		OrgIcon:          orgIcon,
		Version:          version,
		BrowserTelemetry: faroURL != "",
		PublicURL:        os.Getenv("PUBLIC_URL"),
		OrreryURL:        hubURL,
		SignOutURL:       os.Getenv("SIGNOUT_URL"),
	}, server.Parts{Tiles: cache, Coverage: coverage, Actions: actions, Icons: icons, Curator: curator, Desk: desk, Relay: relay, Metrics: metrics}, log)
	if err != nil {
		return err
	}

	listeners := []struct {
		name, addr string
		handler    http.Handler
	}{
		{"session", env("SESSION_ADDR", ":8080"), srv.Session()},
		{"open", env("OPEN_ADDR", ":8081"), srv.Open()},
	}
	errs := make(chan error, len(listeners))
	servers := make([]*http.Server, 0, len(listeners))
	for _, l := range listeners {
		hs := &http.Server{
			Addr:              l.addr,
			Handler:           l.handler,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       2 * time.Minute,
		}
		servers = append(servers, hs)
		go func() {
			if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs <- err
			}
		}()
	}
	log.Info("myapps started", "orrery", os.Getenv("ORRERY_URL"), "actions", actions != nil, "requests", desk != nil, "decisions", curator != nil && os.Getenv("PUBLIC_URL") != "", "catalog_ttl", ttl.String(), "browser_telemetry", faroURL != "", "faro_relay", faroURL != "" && faroKey != "")

	select {
	case <-ctx.Done():
	case err = <-errs:
	}
	log.Info("myapps stopping")
	done, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, hs := range servers {
		_ = hs.Shutdown(done)
	}
	return errors.Join(err, shutdownTelemetry(done))
}

// startOffering offers the blueprints My Apps owns to the hub, for as long
// as ctx lasts. It is off unless both ORRERY_URL and ORRERY_PUSH_TOKEN, a
// source token, are set; ORRERY_TOKEN only reads.
func startOffering(ctx context.Context, reader *catalog.Orrery, log *slog.Logger) error {
	raw, token := os.Getenv("ORRERY_URL"), os.Getenv("ORRERY_PUSH_TOKEN")
	if raw == "" || token == "" {
		log.Info("offering blueprints off: ORRERY_URL and ORRERY_PUSH_TOKEN are not both set")
		return nil
	}
	base, err := catalog.HubBase(raw)
	if err != nil {
		return err
	}
	go offer(ctx, orrery.New(base, token, orrery.WithLogger(log)), reader, log)
	return nil
}

// offerEvery is how long a refused offer waits before it is made again.
const offerEvery = 10 * time.Minute

// offer brings the parts of the catalog My Apps owns: the capabilities, the
// lenses, the mappings, and the applications, linked to what the mappings
// hold. The mappings are read with the read token, since the push token
// only writes. Offering adds what the hub lacks and changes nothing there.
// Each is offered alone, so one the hub refuses leaves the others; a
// refused one, or all of them while the mappings cannot be read, is
// offered again until the hub takes it.
func offer(ctx context.Context, hub *orrery.Client, reader *catalog.Orrery, log *slog.Logger) {
	waiting := map[string]bool{}
	for {
		m, err := reader.Mappings(ctx)
		if err != nil {
			log.Warn("reading the mappings failed: offering waits", "retry_in", offerEvery.String(), "error", err)
		} else {
			first := len(waiting) == 0
			for _, b := range catalog.CurationBlueprints(m) {
				id, _ := b["id"].(string)
				if !first && !waiting[id] {
					continue
				}
				delete(waiting, id)
				if _, err := hub.OfferBlueprints(ctx, []orrery.Blueprint{b}); err != nil {
					log.Warn("offering a blueprint failed: changes to it will not save until it is taken", "blueprint", id, "retry_in", offerEvery.String(), "error", err)
					waiting[id] = true
				}
			}
			if len(waiting) == 0 {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(offerEvery):
		}
	}
}

// newCurator writes the admin's decisions with the push token; without
// one, the admin page only reads.
func newCurator(log *slog.Logger) (catalog.Curator, error) {
	raw, token := os.Getenv("ORRERY_URL"), os.Getenv("ORRERY_PUSH_TOKEN")
	if raw == "" || token == "" {
		return nil, nil
	}
	return catalog.NewCurator(raw, token, log)
}

// newDesk is the Graft request desk people report problems to. It is off
// unless GRAFT_URL, the desk's token port, GRAFT_TOKEN_FILE, a file
// holding My Apps' intake token, and PUBLIC_URL, which tells the portal's
// own pages from another site's, are all set.
func newDesk(metrics *server.Metrics, log *slog.Logger) (*graft.Client, error) {
	var unset []string
	for _, name := range []string{"GRAFT_URL", "GRAFT_TOKEN_FILE", "PUBLIC_URL"} {
		if os.Getenv(name) == "" {
			unset = append(unset, name)
		}
	}
	if len(unset) > 0 {
		log.Info("requests off: " + strings.Join(unset, " and ") + " not set")
		return nil, nil
	}
	return graft.New(os.Getenv("GRAFT_URL"), os.Getenv("GRAFT_TOKEN_FILE"), metrics.GraftCall)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
