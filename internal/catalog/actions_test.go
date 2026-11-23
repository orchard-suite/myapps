package catalog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/orchard-suite/platformkit/orrery"
)

type countedActions struct {
	calls int
	err   error
}

func (c *countedActions) ActionsFor(_ context.Context, _ string) ([]orrery.ActionSummary, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return []orrery.ActionSummary{{ID: "b", Title: "Bee", Group: "Ops"}, {ID: "a space", Title: "Ant", Group: "Ops"}, {ID: "c", Title: "Cat", Group: "Build"}}, nil
}

func TestActionsAreCachedPerPersonAndKeptThroughFailure(t *testing.T) {
	src := &countedActions{}
	now := time.Unix(0, 0)
	a := NewActions(src, "https://hub.example.org/", time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	a.now = func() time.Time { return now }
	got := a.For(context.Background(), "A@example.com")
	if len(got) != 3 || got[0].Title != "Cat" || got[1].Title != "Ant" || got[1].URL != "https://hub.example.org/actions/a%20space" {
		t.Fatalf("%+v", got)
	}
	a.For(context.Background(), "a@example.com")
	if src.calls != 1 {
		t.Fatalf("asked the hub %d times within the TTL", src.calls)
	}
	now = now.Add(2 * time.Minute)
	src.err = errors.New("down")
	if kept := a.For(context.Background(), "a@example.com"); len(kept) != 3 || src.calls != 2 {
		t.Fatalf("a failed read keeps the last list: %d after %d calls", len(kept), src.calls)
	}
}

func TestNoActionsWithoutTheHubAddress(t *testing.T) {
	var a *Actions = NewActions(&countedActions{}, "", time.Minute, nil, nil)
	if a != nil || a.For(context.Background(), "a@example.com") != nil {
		t.Fatal("without the hub's address no page shows actions")
	}
}

func TestActionsOfAnApplication(t *testing.T) {
	all := []Action{{Title: "Deploy Orrery", Group: "orrery"}, {Title: "Build an app", Group: "Graft"}, {Title: "Upgrade", Group: ""}}
	if got := ActionsOf(all, "Orrery"); len(got) != 1 || got[0].Title != "Deploy Orrery" {
		t.Fatalf("an app's actions are those grouped under its name, any case: %+v", got)
	}
	if got := ActionsOf(all, ""); got != nil {
		t.Fatalf("a nameless app has no actions: %+v", got)
	}
	if ApplicationPage("", "x") != "" || ApplicationPage("https://hub.example.org", "a/b") == "" {
		t.Fatal("an application's page needs the hub's address")
	}
}
