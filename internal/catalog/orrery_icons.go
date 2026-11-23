package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/orchard-suite/platformkit/orrery"
)

// Index reads the hub's release, which fixes every built-in brand file,
// and each custom icon's digest: two calls, however many icons there are.
func (o *Orrery) Index(ctx context.Context) (IconIndex, error) {
	var who struct {
		Version string `json:"version"`
	}
	if err := o.hub.Call(ctx, http.MethodGet, "/v1/whoami", nil, &who); err != nil {
		return IconIndex{}, fmt.Errorf("hub release: %w", err)
	}
	if who.Version == "" {
		return IconIndex{}, errors.New("hub release: whoami named no version")
	}
	var list struct {
		Icons []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"icons"`
	}
	if err := o.hub.Call(ctx, http.MethodGet, "/v1/icons", nil, &list); err != nil {
		return IconIndex{}, fmt.Errorf("custom icons: %w", err)
	}
	custom := make(map[string]string, len(list.Icons))
	for _, i := range list.Icons {
		custom[i.Name] = i.SHA256
	}
	return IconIndex{Release: who.Version, Custom: custom}, nil
}

// Icon reads one icon drawn for theme from the hub through the kit's
// client, revalidating with etag when it is set.
func (o *Orrery) Icon(ctx context.Context, name string, theme Theme, etag string) (Image, error) {
	ink := orrery.ThemeDark
	if theme == OnLight {
		ink = orrery.ThemeLight
	}
	img, err := o.hub.IconFor(ctx, name, ink, etag)
	if errors.Is(err, orrery.ErrIconNotFound) {
		return Image{}, ErrIconNotFound
	}
	if err != nil {
		return Image{}, err
	}
	return Image{Type: img.Type, Data: img.Data, ETag: img.ETag, NotModified: img.NotModified}, nil
}
