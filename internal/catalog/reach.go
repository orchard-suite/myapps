package catalog

// Reach is what one person may do with an app on the Landscape: open its
// tile, or ask for it. It names apps, never who holds them.
type Reach struct {
	// Tile is the tile's key, for /open/{key}.
	Tile string
	// Open is whether this person may open it.
	Open bool
	// Request is where this person asks for it; empty when they may open
	// it or there is nowhere to ask.
	Request string
	// Team is the owning team's name, who to ask when there is no request.
	Team string
}

// ReachOf is each shown app's reach for one viewer, by the product it is
// made from (blueprint:key, a card's Ref). request is where a person asks
// for an app they may not open, or empty.
func ReachOf(apps []App, v Viewer, request func(App) string) map[string]Reach {
	out := make(map[string]Reach, len(apps))
	for _, a := range apps {
		open, _ := a.VisibleTo(v)
		r := Reach{Tile: a.Key, Open: open, Team: a.Team}
		if !open && request != nil {
			r.Request = request(a)
		}
		out[a.Ref] = r
	}
	return out
}
