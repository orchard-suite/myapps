package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/orchard-suite/platformkit/door"
	"github.com/orchard-suite/platformkit/orrery"
)

// Orrery reads tiles and the landscape from an Orrery hub.
type Orrery struct {
	hub *orrery.Client
	log *slog.Logger
}

// NewOrrery takes ORRERY_URL, the ingest service's base such as
// https://orrery-ingest.example.com, and a bearer token that may read
// entities. A failed read is not retried: the cache keeps the last good
// tiles and asks again after its TTL.
func NewOrrery(base, token string, log *slog.Logger) (*Orrery, error) {
	base, err := HubBase(base)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, errors.New("ORRERY_TOKEN is required")
	}
	return &Orrery{hub: orrery.New(base, token, orrery.WithRetries(0), orrery.WithLogger(log)), log: log}, nil
}

// HubBase is ORRERY_URL as the kit's client takes it: absolute, without a
// trailing slash. The client adds /v1 itself.
func HubBase(raw string) (string, error) {
	base := strings.TrimRight(raw, "/")
	if u, err := url.Parse(base); err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("ORRERY_URL must be an absolute URL, such as https://orrery-ingest.example.com")
	}
	return base, nil
}

// readTimeout bounds one read of the catalog.
const readTimeout = 8 * time.Second

// Directory reads the business capabilities, the applications, the Entra
// apps, workloads, open-source services, and outside APIs they may be
// paired with, who holds what in Entra, the
// teams, the dismissed suggestions, and how many approvals wait. A
// blueprint the hub does not have reads as empty; any other failed read
// fails the whole read, so a blip never takes tiles away.
func (o *Orrery) Directory(ctx context.Context) (Directory, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	src, blueprints, err := o.mappingsAndBlueprints(ctx)
	if err != nil {
		return Directory{}, err
	}
	lists := map[string][]orrery.Record{}
	for _, name := range []string{CapabilityBlueprint, ApplicationBlueprint, IgnoredBlueprint} {
		if lists[name], err = o.list(ctx, orrery.Query{Blueprint: name}); err != nil {
			return Directory{}, err
		}
	}
	read := map[MappingKind][]orrery.Record{}
	for _, spec := range MappingSpecs {
		bp := src.Blueprint(spec.Kind)
		if bp == "" {
			continue
		}
		list, err := o.list(ctx, orrery.Query{Blueprint: bp})
		if err != nil {
			return Directory{}, err
		}
		read[spec.Kind] = src.Normalize(spec.Kind, list)
	}
	assignments := read[MapAssignments]
	if src.Blueprint(MapAssignments) == "" {
		assignments = src.AssignmentsOnApps(read[MapEntraApps])
	}
	waiting, err := o.list(ctx, orrery.Query{Blueprint: ApprovalBlueprint, Filters: []orrery.Filter{{Field: "decision", Op: "eq", Value: "pending"}}})
	if err != nil {
		return Directory{}, err
	}
	entra := EntraApps(read[MapEntraApps])
	apps := WithRepositories(WithLogos(Applications(WithoutBlueprintIcons(lists[ApplicationBlueprint], blueprints)), entra), read[MapServices])
	access := AccessOf(read[MapPeople], read[MapGroups], assignments)
	var ignored []string
	for _, r := range lists[IgnoredBlueprint] {
		ignored = append(ignored, r.Key)
	}
	return Directory{
		Capabilities: Categories(lists[CapabilityBlueprint]),
		Apps:         Tiles(apps, entra, access),
		Applications: apps,
		EntraApps:    entra,
		Workloads:    Categories(read[MapWorkloads]),
		Suggestions:  SuggestionsOf(apps, entra, Found{Workloads: read[MapWorkloads], Services: read[MapServices], APIs: read[MapAPIs]}, ignored),
		Access:       access,
		Teams:        Categories(read[MapTeams]),
		Waiting:      len(waiting),
		Mappings:     src,
	}, nil
}

// Mappings are where this hub holds what My Apps reads: the saved
// mappings over the defaults, each kept only when the hub holds its
// blueprint.
func (o *Orrery) Mappings(ctx context.Context) (Mappings, error) {
	m, _, err := o.mappingsAndBlueprints(ctx)
	return m, err
}

// mappingsAndBlueprints are the mappings and the hub's blueprints they
// were read against.
func (o *Orrery) mappingsAndBlueprints(ctx context.Context) (Mappings, []Blueprint, error) {
	saved, err := o.list(ctx, orrery.Query{Blueprint: MappingBlueprint})
	if err != nil {
		return nil, nil, err
	}
	blueprints, err := o.Blueprints(ctx)
	if err != nil {
		return nil, nil, err
	}
	return MappingsOf(saved).Within(blueprints), blueprints, nil
}

// ApprovalBlueprint is the record of a pending decision, such as a request
// for access to an app.
const ApprovalBlueprint = "it_approval"

// list reads a blueprint's entities; a blueprint the hub lacks is empty.
func (o *Orrery) list(ctx context.Context, q orrery.Query) ([]orrery.Record, error) {
	records, err := o.hub.Entities(ctx, q)
	if apiErr, ok := errors.AsType[*orrery.APIError](err); ok && apiErr.Status == http.StatusNotFound {
		o.log.DebugContext(ctx, "the hub has no such blueprint", "blueprint", q.Blueprint)
		return nil, nil
	}
	return records, err
}

// Blueprints lists the hub's blueprints, their fields and relations.
func (o *Orrery) Blueprints(ctx context.Context) ([]Blueprint, error) {
	var page struct {
		Blueprints []Blueprint `json:"blueprints"`
	}
	if err := o.hub.Call(ctx, http.MethodGet, "/v1/blueprints", nil, &page); err != nil {
		return nil, err
	}
	return page.Blueprints, nil
}

// Curator writes what admins decide to Orrery: the applications, the
// dismissed suggestions, the business capabilities, the lenses and their
// assessments, and the relations a new lens needs on applications.
type Curator interface {
	SaveApplication(ctx context.Context, key string, d AppDetails) error
	DeleteApplication(ctx context.Context, key string) error
	Ignore(ctx context.Context, ref, by string) error
	PutCapability(ctx context.Context, c Category) error
	DeleteCapability(ctx context.Context, key string) error
	PutLens(ctx context.Context, blueprint string, order int) error
	DeleteLens(ctx context.Context, blueprint string) error
	PutAssessment(ctx context.Context, a Assessment) error
	PutMapping(ctx context.Context, kind MappingKind, m Mapping, by string) error
	Offer(ctx context.Context, b orrery.Blueprint) error
}

// OrreryCurator writes with My Apps' push token. Everything it writes is
// My Apps' own: no pusher's entity is touched.
type OrreryCurator struct{ hub *orrery.Client }

// NewCurator takes the hub's base and My Apps' source token, which must be
// registered for every blueprint My Apps writes.
func NewCurator(base, token string, log *slog.Logger) (*OrreryCurator, error) {
	base, err := HubBase(base)
	if err != nil {
		return nil, err
	}
	return &OrreryCurator{hub: orrery.New(base, token, orrery.WithRetries(1), orrery.WithLogger(log))}, nil
}

func (c *OrreryCurator) put(ctx context.Context, e orrery.Entity) error {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	return c.hub.Put(ctx, e)
}

func (c *OrreryCurator) del(ctx context.Context, blueprint, key string) error {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	return c.hub.Delete(ctx, blueprint, key)
}

// SaveApplication writes one application.
func (c *OrreryCurator) SaveApplication(ctx context.Context, key string, d AppDetails) error {
	return c.put(ctx, d.Entity(key))
}

// DeleteApplication removes one application.
func (c *OrreryCurator) DeleteApplication(ctx context.Context, key string) error {
	return c.del(ctx, ApplicationBlueprint, key)
}

// Ignore dismisses one suggestion.
func (c *OrreryCurator) Ignore(ctx context.Context, ref, by string) error {
	return c.put(ctx, IgnoredEntity(ref, by, time.Now()))
}

// PutCapability writes one business capability.
func (c *OrreryCurator) PutCapability(ctx context.Context, cap Category) error {
	return c.put(ctx, CapabilityEntity(cap))
}

// DeleteCapability removes one business capability.
func (c *OrreryCurator) DeleteCapability(ctx context.Context, key string) error {
	return c.del(ctx, CapabilityBlueprint, key)
}

// PutLens writes one lens.
func (c *OrreryCurator) PutLens(ctx context.Context, blueprint string, order int) error {
	return c.put(ctx, LensEntity(blueprint, order))
}

// DeleteLens removes one lens.
func (c *OrreryCurator) DeleteLens(ctx context.Context, blueprint string) error {
	return c.del(ctx, LensBlueprint, blueprint)
}

// PutAssessment writes one coverage assessment.
func (c *OrreryCurator) PutAssessment(ctx context.Context, a Assessment) error {
	return c.put(ctx, a.Entity())
}

// PutMapping writes where My Apps reads one kind of thing.
func (c *OrreryCurator) PutMapping(ctx context.Context, kind MappingKind, m Mapping, by string) error {
	return c.put(ctx, m.Entity(kind, by))
}

// Offer brings a blueprint, or the parts of one My Apps needs.
func (c *OrreryCurator) Offer(ctx context.Context, b orrery.Blueprint) error {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	_, err := c.hub.OfferBlueprints(ctx, []orrery.Blueprint{b})
	return err
}

// Coverage is the reader of every lens: the hub's blueprints, the lenses,
// each lens's categories, and every service and API, with Orrery's sources
// for each card's integration. A failed read of the sources does not fail
// it: integration is unknown until a read succeeds.
func (o *Orrery) Coverage(ctx context.Context) (Coverage, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	blueprints, err := o.Blueprints(ctx)
	if err != nil {
		return Coverage{}, err
	}
	lensRecords, err := o.list(ctx, orrery.Query{Blueprint: LensBlueprint})
	if err != nil {
		return Coverage{}, err
	}
	c := Coverage{Blueprints: blueprints, Lenses: Lenses(lensRecords, blueprints), Categories: map[string][]orrery.Record{}}
	for _, l := range c.Lenses {
		if c.Categories[l.Blueprint], err = o.list(ctx, orrery.Query{Blueprint: l.Blueprint}); err != nil {
			return Coverage{}, err
		}
	}
	assessments, err := o.list(ctx, orrery.Query{Blueprint: AssessmentBlueprint})
	if err != nil {
		return Coverage{}, err
	}
	c.Assessments = Assessments(assessments)
	saved, err := o.list(ctx, orrery.Query{Blueprint: MappingBlueprint})
	if err != nil {
		return Coverage{}, err
	}
	m := MappingsOf(saved).Within(blueprints)
	for _, read := range []struct {
		kind MappingKind
		into *[]orrery.Record
	}{{MapServices, &c.Services}, {MapAPIs, &c.APIs}} {
		bp := m.Blueprint(read.kind)
		if bp == "" {
			continue
		}
		list, err := o.list(ctx, orrery.Query{Blueprint: bp})
		if err != nil {
			return Coverage{}, err
		}
		*read.into = m.Normalize(read.kind, list)
	}
	sources, err := o.Sources(ctx)
	if err != nil {
		o.log.WarnContext(ctx, "sources read failed: integration unknown", "error", err)
		return c, nil
	}
	if !Reports(sources) {
		o.log.WarnContext(ctx, "sources carry no writes: integration unknown", "sources", len(sources))
	}
	c.Sources, c.SourcesKnown = sources, true
	return c, nil
}

// Sources lists Orrery's sources, what each acts for and what it wrote.
func (o *Orrery) Sources(ctx context.Context) ([]Source, error) {
	var page struct {
		Sources []Source `json:"sources"`
	}
	if err := o.hub.Call(ctx, http.MethodGet, "/v1/sources", nil, &page); err != nil {
		return nil, err
	}
	return page.Sources, nil
}

// parseAudience accepts an array of group names or one comma separated
// string, since a field a person types may arrive either way. The names are
// the front door's roles, so they split the way its header does.
func parseAudience(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return door.ParseRoles(strings.Join(list, ","))
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return door.ParseRoles(one)
	}
	return nil
}

// parsePeople reads the people field, an array of emails or one comma
// separated string: lowercased, each once, anything not an email dropped.
func parsePeople(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		var one string
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil
		}
		list = strings.Split(one, ",")
	}
	var out []string
	for _, v := range list {
		if e, ok := Email(v); ok && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out
}

// Email is s as an email, trimmed and lowercased, and whether it is one:
// one @ with something on each side, no space, a dot in the domain.
func Email(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || !strings.Contains(domain, ".") || strings.ContainsAny(s, " \t<>,;") || strings.Contains(domain, "@") {
		return "", false
	}
	return s, true
}

// statusOf maps Orrery's status level to a dot. An entity Orrery has not
// seen lately, or one with no level, is grey.
func statusOf(state, level string) Status {
	if state != "" && state != "active" {
		return Grey
	}
	switch strings.ToLower(level) {
	case "green", "ok", "healthy":
		return Green
	case "amber", "yellow", "orange", "warning", "degraded":
		return Amber
	case "red", "critical", "error", "down":
		return Red
	}
	return Grey
}

// ActionsFor lists the actions the person with this email may run. The
// token's role must hold actions.list.
func (o *Orrery) ActionsFor(ctx context.Context, email string) ([]orrery.ActionSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	return o.hub.ActionsFor(ctx, email)
}
