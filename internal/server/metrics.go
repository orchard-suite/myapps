package server

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics are the portal's own instruments. Their names are a contract with
// the deployment's dashboard and alerts.
type Metrics struct {
	catalogReads   metric.Int64Counter
	actionReads    metric.Int64Counter
	landscapeReads metric.Int64Counter
	tileOpens      metric.Int64Counter
	iconReads      metric.Int64Counter
	changes        metric.Int64Counter
	deskCalls      metric.Int64Counter
}

// NewMetrics registers the instruments. catalogAge is read once per export,
// never per request.
func NewMetrics(catalogAge func() time.Duration) (*Metrics, error) {
	m := otel.Meter(Service)
	catalogReads, err := m.Int64Counter(Service+".catalog.read", metric.WithDescription("Reads of the Orrery catalog, by outcome: ok or failed."))
	if err != nil {
		return nil, err
	}
	actionReads, err := m.Int64Counter(Service+".actions.read", metric.WithDescription("Reads of a person's self-service actions from Orrery, by outcome: ok or failed."))
	if err != nil {
		return nil, err
	}
	landscapeReads, err := m.Int64Counter(Service+".landscape.read", metric.WithDescription("Reads of the lenses, their categories, and the services and APIs from Orrery, by outcome: ok or failed."))
	if err != nil {
		return nil, err
	}
	tileOpens, err := m.Int64Counter(Service+".tile.open", metric.WithDescription("Tiles opened, by top business capability."))
	if err != nil {
		return nil, err
	}
	iconReads, err := m.Int64Counter(Service+".icon.read", metric.WithDescription("Reads of Orrery's icons, by outcome: for an icon ok, not_modified, not_found, or failed; for the index that says which changed, index or index_failed."))
	if err != nil {
		return nil, err
	}
	changes, err := m.Int64Counter(Service+".admin.change", metric.WithDescription("What admins saved to Orrery, by kind (application saved, suggestion attached, capability saved, and the rest) and outcome: ok or failed."))
	if err != nil {
		return nil, err
	}
	deskCalls, err := m.Int64Counter(Service+".graft.call", metric.WithDescription("Calls to the Graft request desk, by operation (send or list) and outcome: ok, refused (the desk said no to the person), or failed."))
	if err != nil {
		return nil, err
	}
	age, err := m.Float64ObservableGauge(Service+".catalog.age", metric.WithUnit("s"), metric.WithDescription("Seconds since the tiles were last read from Orrery with success."))
	if err != nil {
		return nil, err
	}
	_, err = m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveFloat64(age, catalogAge().Seconds())
		return nil
	}, age)
	if err != nil {
		return nil, err
	}
	return &Metrics{catalogReads: catalogReads, actionReads: actionReads, landscapeReads: landscapeReads, tileOpens: tileOpens, iconReads: iconReads, changes: changes, deskCalls: deskCalls}, nil
}

// CatalogRead counts one read of the catalog: ok or failed.
func (m *Metrics) CatalogRead(ctx context.Context, outcome string) {
	m.catalogReads.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

// ActionsRead counts one read of a person's actions: ok or failed.
func (m *Metrics) ActionsRead(ctx context.Context, outcome string) {
	m.actionReads.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

// LandscapeRead counts one read of the landscape: ok or failed.
func (m *Metrics) LandscapeRead(ctx context.Context, outcome string) {
	m.landscapeReads.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

// TileOpen counts a tile opened, by its top business capability's key.
func (m *Metrics) TileOpen(ctx context.Context, capability string) {
	m.tileOpens.Add(ctx, 1, metric.WithAttributes(attribute.String("capability", capability)))
}

// IconRead counts one read of an icon (ok, not_modified, not_found, or
// failed) or of the icon index (index or index_failed).
func (m *Metrics) IconRead(ctx context.Context, outcome string) {
	m.iconReads.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

// Change counts one admin change saved, or not, by what it was.
func (m *Metrics) Change(ctx context.Context, kind, outcome string) {
	m.changes.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", kind), attribute.String("outcome", outcome)))
}

// GraftCall counts one call to the request desk, by operation (send or
// list) and outcome: ok, refused, or failed.
func (m *Metrics) GraftCall(ctx context.Context, operation, outcome string) {
	m.deskCalls.Add(ctx, 1, metric.WithAttributes(attribute.String("operation", operation), attribute.String("outcome", outcome)))
}
