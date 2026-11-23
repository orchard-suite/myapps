package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/orchard-suite/platformkit/orrery"
)

// fakeIcons answers from a map, counting each ask, its theme, and the
// ETag it carried. light holds the copies drawn for a light background;
// a name it lacks answers the same copy in both themes.
type fakeIcons struct {
	mu    sync.Mutex
	icons map[string]Image
	light map[string]Image
	err   error
	asked []string
	// index is what Index answers; indexErr fails it. indexed counts asks.
	index    IconIndex
	indexErr error
	indexed  int
}

func (f *fakeIcons) Index(context.Context) (IconIndex, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.indexed++
	return f.index, f.indexErr
}

// set changes what the index says, as the hub would after a change.
func (f *fakeIcons) set(index IconIndex) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.index = index
}

func (f *fakeIcons) Icon(_ context.Context, name string, theme Theme, etag string) (Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, name+"/"+string(theme)+"@"+etag)
	if f.err != nil {
		return Image{}, f.err
	}
	img, ok := f.icons[name]
	if l, has := f.light[name]; has && theme == OnLight {
		img, ok = l, true
	}
	if !ok {
		return Image{}, ErrIconNotFound
	}
	if etag != "" && etag == img.ETag {
		return Image{ETag: etag, NotModified: true}, nil
	}
	return img, nil
}

// seen is every ask, sorted: icons are read a few at a time.
func (f *fakeIcons) seen() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(slices.Sorted(slices.Values(f.asked)), ",")
}

// outcomes collects what a refresh counted, sorted.
type outcomes struct {
	mu   sync.Mutex
	list []string
}

func (o *outcomes) add(_ context.Context, outcome string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.list = append(o.list, outcome)
}

func (o *outcomes) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.Join(slices.Sorted(slices.Values(o.list)), ",")
}

var svg = Image{Type: "image/svg+xml; charset=utf-8", Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), ETag: `"abc123def456789"`}

func TestIconsAreAskedForOnlyWhenTheIndexSaysTheyChanged(t *testing.T) {
	src := &fakeIcons{icons: map[string]Image{"grafana": svg}, index: IconIndex{Release: "1.0.0", Custom: map[string]string{"grafana": "aaa"}}}
	var counted outcomes
	icons := NewIcons(src, quiet(), counted.add)

	icons.Refresh(context.Background(), []string{"grafana", "grafana", "stack", "https://x.example.com/a.svg", "Bad-Name", ""})
	if got := src.seen(); got != "grafana/dark@,grafana/light@,stack/dark@,stack/light@" {
		t.Fatalf("asked %s: each valid name once in each theme, nothing that cannot be a name", got)
	}
	img, ok := icons.Get("grafana", OnDark)
	if !ok || img.Type != "image/svg+xml" || string(img.Data) != string(svg.Data) {
		t.Fatalf("held %+v %v: the type without its parameters", img, ok)
	}
	if got := icons.URL("grafana", OnDark); got != "/icons/grafana?v=abc123def456" {
		t.Fatalf("url %q", got)
	}
	if got := icons.URL("grafana", OnLight); got != "/icons/grafana?theme=light&v=abc123def456" {
		t.Fatalf("light url %q", got)
	}
	if icons.URL("stack", OnDark) != "" || icons.URL("nothing", OnLight) != "" {
		t.Fatal("an icon not held has no url, so the page draws a monogram")
	}

	for range 5 {
		icons.Refresh(context.Background(), []string{"grafana", "stack"})
	}
	if got := src.seen(); got != "grafana/dark@,grafana/light@,stack/dark@,stack/light@" || src.indexed != 6 {
		t.Fatalf("an unchanged index should ask for no icon, asked %s after %d index reads", got, src.indexed)
	}

	src.set(IconIndex{Release: "1.0.0", Custom: map[string]string{"grafana": "bbb"}})
	icons.Refresh(context.Background(), []string{"grafana", "stack"})
	if got := src.seen(); got != `grafana/dark@,grafana/dark@"abc123def456789",grafana/light@,grafana/light@"abc123def456789",stack/dark@,stack/light@` {
		t.Fatalf("a new digest should revalidate that custom icon alone, asked %s", got)
	}
	if _, ok := icons.Get("grafana", OnLight); !ok {
		t.Fatal("a 304 keeps the icon")
	}

	src.set(IconIndex{Release: "1.1.0", Custom: map[string]string{"grafana": "bbb"}})
	icons.Refresh(context.Background(), []string{"grafana", "stack"})
	if got := src.seen(); strings.Count(got, "stack/") != 4 || strings.Count(got, "grafana/") != 4 {
		t.Fatalf("a new release should ask again for the built-in names alone, asked %s", got)
	}
	if got := counted.String(); got != "index,index,index,index,index,index,index,index,not_found,not_found,not_found,not_found,not_modified,not_modified,ok,ok" {
		t.Fatalf("outcomes %s", got)
	}
}

func TestIconsKeepTheLastGoodCopy(t *testing.T) {
	src := &fakeIcons{icons: map[string]Image{"grafana": svg}, index: IconIndex{Release: "1.0.0", Custom: map[string]string{"grafana": "aaa"}}}
	var counted outcomes
	icons := NewIcons(src, quiet(), counted.add)
	icons.Refresh(context.Background(), []string{"grafana"})

	src.err = errors.New("orrery answered 503")
	src.set(IconIndex{Release: "1.0.0", Custom: map[string]string{"grafana": "bbb"}})
	icons.Refresh(context.Background(), []string{"grafana"})
	if _, ok := icons.Get("grafana", OnDark); !ok {
		t.Fatal("a failed read should keep the last good icon")
	}

	src.err = nil
	icons.Refresh(context.Background(), []string{"grafana"})
	if got := src.seen(); strings.Count(got, "grafana/dark@") != 3 {
		t.Fatalf("a failed read should be asked again on the next refresh, asked %s", got)
	}

	delete(src.icons, "grafana")
	src.set(IconIndex{Release: "1.0.0"})
	icons.Refresh(context.Background(), []string{"grafana"})
	if _, ok := icons.Get("grafana", OnDark); ok {
		t.Fatal("an icon Orrery no longer holds should be dropped")
	}
	if got := counted.String(); got != "failed,failed,index,index,index,index,not_found,not_found,not_modified,not_modified,ok,ok" {
		t.Fatalf("outcomes %s", got)
	}
}

func TestAFailedIndexAsksForNoIcon(t *testing.T) {
	src := &fakeIcons{icons: map[string]Image{"grafana": svg}, index: IconIndex{Release: "1.0.0"}}
	var counted outcomes
	icons := NewIcons(src, quiet(), counted.add)
	icons.Refresh(context.Background(), []string{"grafana"})

	src.indexErr = errors.New(`orrery: 429 "myapps" made 10,001 API calls this hour`)
	icons.Refresh(context.Background(), []string{"grafana", "loki"})
	if got := src.seen(); got != "grafana/dark@,grafana/light@" {
		t.Fatalf("a failed index should ask for nothing, asked %s", got)
	}
	if _, ok := icons.Get("grafana", OnDark); !ok {
		t.Fatal("a failed index keeps what is held")
	}
	if got := counted.String(); got != "index,index_failed,ok,ok" {
		t.Fatalf("outcomes %s", got)
	}
}

func TestRefreshesTogetherAskForEachIconOnce(t *testing.T) {
	src := &fakeIcons{icons: map[string]Image{"grafana": svg}, index: IconIndex{Release: "1.0.0"}}
	icons := NewIcons(src, quiet(), nil)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { icons.Refresh(context.Background(), []string{"grafana"}) })
	}
	wg.Wait()
	if got := src.seen(); got != "grafana/dark@,grafana/light@" {
		t.Fatalf("the tiles and the landscape refreshing together should ask once, asked %s", got)
	}
}

func TestIconsRefuseWhatIsNotAnImage(t *testing.T) {
	src := &fakeIcons{icons: map[string]Image{
		"page":  {Type: "text/html", Data: []byte("<html>"), ETag: `"1"`},
		"empty": {Type: "image/png", ETag: `"2"`},
		"huge":  {Type: "image/png", Data: make([]byte, maxIconBytes+1), ETag: `"3"`},
		"webp":  {Type: "image/webp", Data: []byte("RIFF....WEBP"), ETag: `"4"`},
	}}
	icons := NewIcons(src, quiet(), nil)
	icons.Refresh(context.Background(), []string{"page", "empty", "huge", "webp"})
	for _, name := range []string{"page", "empty", "huge"} {
		if _, ok := icons.Get(name, OnDark); ok {
			t.Errorf("%s should be refused", name)
		}
	}
	if _, ok := icons.Get("webp", OnLight); !ok {
		t.Error("a webp should be held")
	}
}

func TestWithIconsFetchesWhatAGoodReadNames(t *testing.T) {
	src := &fakeIcons{icons: map[string]Image{"grafana": svg}}
	icons := NewIcons(src, quiet(), nil)
	fail := errors.New("down")
	var err error
	read := WithIcons(func(context.Context) (Directory, error) {
		return Directory{Apps: []App{{Key: "a", Icon: "grafana"}, {Key: "b", Icon: "loki"}}}, err
	}, icons, Directory.IconNames)

	err = fail
	if _, got := read(context.Background()); got != fail || src.seen() != "" {
		t.Fatalf("a failed read asks for no icons, asked %q", src.seen())
	}
	err = nil
	if _, got := read(context.Background()); got != nil || src.seen() != "grafana/dark@,grafana/light@,loki/dark@,loki/light@" {
		t.Fatalf("a good read asks for each icon it names, asked %q", src.seen())
	}
	var svc []orrery.Record
	_ = json.Unmarshal([]byte(`[{"key":"a","icon":"argocd"}]`), &svc)
	if names := (Coverage{Services: svc}).IconNames(); len(names) != 1 || names[0] != "argocd" {
		t.Fatalf("coverage names %v", names)
	}
}

func TestOrreryReadsTheIconIndex(t *testing.T) {
	var asked []string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/whoami":
			_, _ = w.Write([]byte(`{"identity":{"kind":"token"},"version":"1.125.2"}`))
		case "/v1/icons":
			_, _ = w.Write([]byte(`{"icons":[{"name":"tally","type":"image/svg+xml","sha256":"abc","size":10}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()
	o, err := NewOrrery(hub.URL, "secret", quiet())
	if err != nil {
		t.Fatal(err)
	}
	index, err := o.Index(context.Background())
	if err != nil || index.Release != "1.125.2" || index.Custom["tally"] != "abc" || len(index.Custom) != 1 {
		t.Fatalf("got %+v, %v", index, err)
	}
	if strings.Join(asked, ",") != "/v1/whoami,/v1/icons" {
		t.Fatalf("asked %v: two calls, however many icons", asked)
	}
	if index.stamp("tally") == index.stamp("grafana") || index.stamp("grafana") != (IconIndex{Release: "1.125.2"}).stamp("github") {
		t.Fatal("a custom icon is stamped by its digest, a built-in one by the release")
	}
}

func TestOrreryReadsAnIcon(t *testing.T) {
	var auth, match, path, query string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, match, path, query = r.Header.Get("Authorization"), r.Header.Get("If-None-Match"), r.URL.Path, r.URL.RawQuery
		switch {
		case strings.HasSuffix(r.URL.Path, "/missing"):
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/broken"):
			w.WriteHeader(http.StatusBadGateway)
		case match == `"v1"`:
			w.WriteHeader(http.StatusNotModified)
		default:
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Header().Set("ETag", `"v1"`)
			_, _ = w.Write(svg.Data)
		}
	}))
	defer hub.Close()
	o, err := NewOrrery(hub.URL, "secret", quiet())
	if err != nil {
		t.Fatal(err)
	}
	img, err := o.Icon(context.Background(), "grafana", OnDark, "")
	if err != nil || img.Type != "image/svg+xml" || img.ETag != `"v1"` || string(img.Data) != string(svg.Data) || img.NotModified {
		t.Fatalf("got %+v, %v", img, err)
	}
	if auth != "Bearer secret" || path != "/v1/icons/grafana" || match != "" {
		t.Fatalf("asked %s with %q and If-None-Match %q", path, auth, match)
	}
	if img, err := o.Icon(context.Background(), "grafana", OnDark, `"v1"`); err != nil || !img.NotModified || match != `"v1"` {
		t.Fatalf("revalidating got %+v, %v, If-None-Match %q", img, err, match)
	}
	if _, err := o.Icon(context.Background(), "grafana", OnLight, ""); err != nil || query != "theme=light" {
		t.Fatalf("the light copy asks theme=light, asked %q, %v", query, err)
	}
	if _, err := o.Icon(context.Background(), "missing", OnDark, ""); !errors.Is(err, ErrIconNotFound) {
		t.Fatalf("a 404 should be ErrIconNotFound, got %v", err)
	}
	if _, err := o.Icon(context.Background(), "broken", OnDark, ""); err == nil || errors.Is(err, ErrIconNotFound) {
		t.Fatalf("a 502 should be a failure, got %v", err)
	}
}

func TestIconsHoldACopyPerTheme(t *testing.T) {
	dark := Image{Type: "image/svg+xml", Data: []byte(`<svg fill="#eee"/>`), ETag: `"d"`}
	light := Image{Type: "image/svg+xml", Data: []byte(`<svg fill="#111"/>`), ETag: `"l"`}
	src := &fakeIcons{icons: map[string]Image{"github": dark}, light: map[string]Image{"github": light}}
	icons := NewIcons(src, quiet(), nil)
	icons.Refresh(context.Background(), []string{"github"})
	if img, ok := icons.Get("github", OnDark); !ok || string(img.Data) != string(dark.Data) {
		t.Fatalf("dark copy %+v %v", img, ok)
	}
	if img, ok := icons.Get("github", OnLight); !ok || string(img.Data) != string(light.Data) {
		t.Fatalf("light copy %+v %v", img, ok)
	}
	for v, want := range map[string]Theme{"": OnDark, "dark": OnDark, "light": OnLight} {
		if got, ok := ThemeOf(v); !ok || got != want {
			t.Errorf("ThemeOf(%q) = %q, %v", v, got, ok)
		}
	}
	if _, ok := ThemeOf("sepia"); ok {
		t.Error("an unknown theme is refused")
	}
}
