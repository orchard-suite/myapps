package catalog

import (
	"cmp"
	"context"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/orchard-suite/platformkit/orrery"
)

// Action is a self-service action a person may run, as a link into the
// hub's form for it.
type Action struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Group       string `json:"group,omitempty"`
	// Approval is true when a run waits for someone else to approve it.
	Approval bool   `json:"approval,omitempty"`
	URL      string `json:"url"`
}

// ActionSource lists the actions one person may run: the Orrery client, or
// a fake in tests.
type ActionSource interface {
	ActionsFor(ctx context.Context, email string) ([]orrery.ActionSummary, error)
}

// Actions answers each person's actions, asking the hub at most once per
// TTL for each person and keeping their last good list through a failure.
type Actions struct {
	src    ActionSource
	ttl    time.Duration
	hub    string
	log    *slog.Logger
	onRead func(ctx context.Context, outcome string)
	now    func() time.Time

	mu     sync.Mutex
	people map[string]actionsRead
}

type actionsRead struct {
	actions []Action
	tried   time.Time
}

// NewActions returns nil without the hub's public address, and then no
// page shows actions. onRead is told each read's outcome, ok or failed.
func NewActions(src ActionSource, hubURL string, ttl time.Duration, log *slog.Logger, onRead func(ctx context.Context, outcome string)) *Actions {
	if hubURL == "" {
		return nil
	}
	if onRead == nil {
		onRead = func(context.Context, string) {}
	}
	return &Actions{src: src, ttl: ttl, hub: strings.TrimRight(hubURL, "/"), log: log, onRead: onRead, now: time.Now, people: map[string]actionsRead{}}
}

// For lists what the person with this email may run, by group and title.
func (a *Actions) For(ctx context.Context, email string) []Action {
	if a == nil || email == "" {
		return nil
	}
	key := strings.ToLower(email)
	a.mu.Lock()
	defer a.mu.Unlock()
	last, seen := a.people[key]
	if seen && a.now().Sub(last.tried) < a.ttl {
		return last.actions
	}
	last.tried = a.now()
	listed, err := a.src.ActionsFor(ctx, email)
	if err != nil {
		a.onRead(ctx, "failed")
		a.log.WarnContext(ctx, "actions read failed", "error", err, "kept", len(last.actions))
		a.people[key] = last
		return last.actions
	}
	a.onRead(ctx, "ok")
	last.actions = make([]Action, 0, len(listed))
	for _, s := range listed {
		last.actions = append(last.actions, Action{ID: s.ID, Title: s.Title, Description: s.Description, Group: s.Group, Approval: s.Approval, URL: a.hub + "/actions/" + url.PathEscape(s.ID)})
	}
	sortActions(last.actions)
	a.people[key] = last
	a.log.DebugContext(ctx, "actions read", "actions", len(last.actions))
	return last.actions
}

func sortActions(actions []Action) {
	slices.SortFunc(actions, func(x, y Action) int {
		if c := cmp.Compare(x.Group, y.Group); c != 0 {
			return c
		}
		return cmp.Compare(x.Title, y.Title)
	})
}

// ActionsOf is the actions Orrery groups under an application's name: the
// ones that act on it.
func ActionsOf(actions []Action, title string) []Action {
	var out []Action
	for _, a := range actions {
		if title != "" && strings.EqualFold(a.Group, title) {
			out = append(out, a)
		}
	}
	return out
}
