package catalog

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Image is one icon as Orrery answers it. NotModified is true when the
// ETag asked with still holds, and then Data is empty.
type Image struct {
	Type        string
	Data        []byte
	ETag        string
	NotModified bool
}

// ErrIconNotFound is an icon Orrery does not hold: no custom icon and no
// built-in brand by that name.
var ErrIconNotFound = errors.New("icon not found")

// Theme is the background a logo is drawn on. A brand mark drawn in black
// has a copy in each theme's ink; every other icon is the same in both.
type Theme string

const (
	// OnDark is the copy for a dark background: a black mark in light ink.
	OnDark Theme = "dark"
	// OnLight is the copy for a light background: a black mark stays dark.
	OnLight Theme = "light"
)

// Themes are the copies held of every icon.
var Themes = []Theme{OnDark, OnLight}

// ThemeOf is the theme a query value names: light, or dark for anything
// else that is empty or dark. ok is false for any other value.
func ThemeOf(v string) (Theme, bool) {
	switch v {
	case "", string(OnDark):
		return OnDark, true
	case string(OnLight):
		return OnLight, true
	}
	return "", false
}

// IconIndex tells the hub's icons apart without reading them. A built-in
// brand file ships with the hub, so it changes only with Release; a custom
// icon changes only with its digest.
type IconIndex struct {
	Release string
	// Custom is each custom icon's digest, by name.
	Custom map[string]string
}

// stamp is what one name's icon is as the index says: a custom icon's
// digest, or the hub's release for anything else. The same stamp means
// the same bytes, so the icon is not asked for again.
func (x IconIndex) stamp(name string) string {
	if digest, ok := x.Custom[name]; ok {
		return "custom " + digest
	}
	return "release " + x.Release
}

// IconSource is where icons come from: the Orrery client, or a fake in
// tests. Index is one read that says which icons changed; Icon reads one
// icon drawn for a theme, revalidating with the ETag last read when etag
// is set.
type IconSource interface {
	Index(ctx context.Context) (IconIndex, error)
	Icon(ctx context.Context, name string, theme Theme, etag string) (Image, error)
}

var iconName = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,62}$`)

// IconName says whether a name may be an icon in Orrery. Anything else,
// such as a URL or a name with a dash, is never asked for.
func IconName(name string) bool { return iconName.MatchString(name) }

// iconTypes are the only types an icon is served as.
var iconTypes = map[string]bool{"image/svg+xml": true, "image/png": true, "image/webp": true}

// maxIconBytes is the largest icon Orrery stores.
const maxIconBytes = 256 << 10

// iconFetchers bounds how many icons are asked for at once.
const iconFetchers = 8

// iconTimeout bounds one refresh of every icon a read names.
const iconTimeout = 8 * time.Second

// Icons keeps every icon the tiles and cards name in memory. Each refresh
// reads the hub's index once and asks only for the icons that are new or
// that the index says changed. A failed read keeps the last good copy and
// is asked again next refresh; an icon Orrery no longer holds is dropped.
// Pages and the icon route only read what is held, never Orrery.
type Icons struct {
	src    IconSource
	log    *slog.Logger
	onRead func(ctx context.Context, outcome string)

	// refreshing lets one refresh run at a time, so the tiles and the
	// landscape refreshing together ask for each icon once.
	refreshing sync.Mutex

	mu   sync.RWMutex
	held map[iconKey]heldIcon
}

// iconKey is one copy of an icon: its name and the theme it is drawn for.
type iconKey struct {
	name  string
	theme Theme
}

type heldIcon struct {
	img     Image
	missing bool
	// stamp is the index's stamp when this copy was read; empty until a
	// read is settled, so a failed read is asked again.
	stamp string
}

// NewIcons wraps a source. onRead is told each read's outcome: for an
// icon ok, not_modified, not_found, or failed; for the index, index or
// index_failed.
func NewIcons(src IconSource, log *slog.Logger, onRead func(ctx context.Context, outcome string)) *Icons {
	if onRead == nil {
		onRead = func(context.Context, string) {}
	}
	return &Icons{src: src, log: log, onRead: onRead, held: map[iconKey]heldIcon{}}
}

// Get is the copy of the icon named held for theme, if any.
func (i *Icons) Get(name string, theme Theme) (Image, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	h, ok := i.held[iconKey{name, theme}]
	if !ok || h.missing || len(h.img.Data) == 0 {
		return Image{}, false
	}
	return h.img, true
}

// URL is where the page loads the copy of the icon named for theme, with
// its version so a new copy is not hidden by the browser's cache, or empty
// when none is held and the page draws a monogram.
func (i *Icons) URL(name string, theme Theme) string {
	img, ok := i.Get(name, theme)
	if !ok {
		return ""
	}
	q := url.Values{}
	if theme == OnLight {
		q.Set("theme", string(OnLight))
	}
	if v := strings.Trim(img.ETag, `"`); v != "" {
		q.Set("v", v[:min(len(v), 12)])
	}
	if len(q) == 0 {
		return "/icons/" + name
	}
	return "/icons/" + name + "?" + q.Encode()
}

// Refresh reads the index, then both copies of every named icon that is new
// or changed, a few at a time. Names that cannot be icons are skipped. A
// failed index read asks for nothing: what is held stays.
func (i *Icons) Refresh(ctx context.Context, names []string) {
	i.refreshing.Lock()
	defer i.refreshing.Unlock()
	ctx, cancel := context.WithTimeout(ctx, iconTimeout)
	defer cancel()
	index, err := i.src.Index(ctx)
	if err != nil {
		i.onRead(ctx, "index_failed")
		i.log.WarnContext(ctx, "icon index read failed", "error", err)
		return
	}
	i.onRead(ctx, "index")
	due := i.due(names, index)
	if len(due) == 0 {
		return
	}
	work := make(chan dueIcon)
	var wg sync.WaitGroup
	for range min(iconFetchers, len(due)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range work {
				i.read(ctx, d.key, d.stamp)
			}
		}()
	}
	for _, key := range due {
		work <- key
	}
	close(work)
	wg.Wait()
	i.log.DebugContext(ctx, "icons refreshed", "asked", len(due))
}

// dueIcon is one copy to ask for and the stamp it is read under.
type dueIcon struct {
	key   iconKey
	stamp string
}

// due is each copy of the valid names, once each, whose held stamp is not
// the index's: never read, changed, or last read failed.
func (i *Icons) due(names []string, index IconIndex) []dueIcon {
	i.mu.RLock()
	defer i.mu.RUnlock()
	seen := map[string]bool{}
	var out []dueIcon
	for _, n := range names {
		if seen[n] || !IconName(n) {
			continue
		}
		seen[n] = true
		stamp := index.stamp(n)
		for _, theme := range Themes {
			key := iconKey{n, theme}
			if h, ok := i.held[key]; ok && h.stamp == stamp {
				continue
			}
			out = append(out, dueIcon{key, stamp})
		}
	}
	return out
}

func (i *Icons) read(ctx context.Context, key iconKey, stamp string) {
	name := key.name
	i.mu.RLock()
	last := i.held[key]
	i.mu.RUnlock()
	etag := ""
	if !last.missing {
		etag = last.img.ETag
	}
	img, err := i.src.Icon(ctx, name, key.theme, etag)
	img.Type = mediaType(img.Type)
	outcome := "ok"
	settled := stamp
	switch {
	case errors.Is(err, ErrIconNotFound):
		outcome = "not_found"
		last = heldIcon{missing: true}
	case err != nil:
		outcome = "failed"
		settled = ""
		i.log.WarnContext(ctx, "icon read failed", "icon", name, "theme", key.theme, "error", err, "kept", len(last.img.Data) > 0)
	case img.NotModified && len(last.img.Data) > 0:
		outcome = "not_modified"
	case !iconTypes[img.Type] || len(img.Data) == 0 || len(img.Data) > maxIconBytes:
		outcome = "failed"
		i.log.WarnContext(ctx, "icon refused: not an svg, png, or webp within 256 KiB", "icon", name, "theme", key.theme, "type", img.Type, "bytes", len(img.Data))
	default:
		last = heldIcon{img: img}
	}
	last.stamp = settled
	i.mu.Lock()
	i.held[key] = last
	i.mu.Unlock()
	i.onRead(ctx, outcome)
}

// mediaType is a Content-Type without its parameters, in lower case.
func mediaType(t string) string {
	t, _, _ = strings.Cut(t, ";")
	return strings.ToLower(strings.TrimSpace(t))
}

// WithIcons wraps a reader so every good read also refreshes the icons it
// names, before the cache hands the value out.
func WithIcons[T any](read Reader[T], icons *Icons, names func(T) []string) Reader[T] {
	return func(ctx context.Context) (T, error) {
		v, err := read(ctx)
		if err == nil {
			icons.Refresh(ctx, names(v))
		}
		return v, err
	}
}

// IconNames is every icon the tiles, applications, and suggestions name, so the admin
// page draws the same logos the tiles will.
func (d Directory) IconNames() []string {
	out := make([]string, 0, len(d.Apps)+len(d.Applications)+len(d.Suggestions))
	for _, a := range d.Apps {
		out = append(out, a.Icon)
	}
	for _, a := range d.Applications {
		out = append(out, a.Logo)
	}
	for _, s := range d.Suggestions {
		out = append(out, s.Icon)
	}
	return out
}
