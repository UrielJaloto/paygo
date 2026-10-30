package memory

import (
	"context"
	"slices"
	"strings"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/webhook"
	webhookservice "github.com/UrielJaloto/paygo/services/webhook"
)

type webhookTables struct {
	endpoints  map[webhook.EndpointID]webhook.Endpoint
	deliveries map[webhook.DeliveryID]webhook.Delivery
}

func newWebhookTables() webhookTables {
	return webhookTables{
		endpoints:  map[webhook.EndpointID]webhook.Endpoint{},
		deliveries: map[webhook.DeliveryID]webhook.Delivery{},
	}
}

type EndpointRepository struct {
	store *Store
	tx    *transaction
}

func NewEndpointRepository(store *Store) *EndpointRepository {
	return &EndpointRepository{store: store}
}

func (r *EndpointRepository) Save(_ context.Context, endpoint webhook.Endpoint) error {
	return r.store.write(r.tx, func(tx *transaction) error {
		tx.checks = append(tx.checks, func() error { return tx.store.checkEndpoint(endpoint.ID()) })
		tx.writes = append(tx.writes, func() { tx.store.webhooks.endpoints[endpoint.ID()] = endpoint })
		return nil
	})
}

func (r *EndpointRepository) FindByID(_ context.Context, id webhook.EndpointID) (webhook.Endpoint, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	found, ok := r.store.webhooks.endpoints[id]
	if !ok {
		return webhook.Endpoint{}, webhook.ErrEndpointNotFound
	}
	return found, nil
}

func (r *EndpointRepository) ListByOwner(_ context.Context, owner apikey.OwnerID) ([]webhook.Endpoint, error) {
	return r.selectEndpoints(func(endpoint webhook.Endpoint) bool {
		return endpoint.OwnerID() == owner
	}), nil
}

func (r *EndpointRepository) CountActiveByOwner(_ context.Context, owner apikey.OwnerID) (int, error) {
	active := r.selectEndpoints(func(endpoint webhook.Endpoint) bool {
		return endpoint.OwnerID() == owner && endpoint.IsActive()
	})
	return len(active), nil
}

func (r *EndpointRepository) ListActiveByOwners(_ context.Context, owners []apikey.OwnerID) ([]webhook.Endpoint, error) {
	return r.selectEndpoints(func(endpoint webhook.Endpoint) bool {
		return endpoint.IsActive() && slices.Contains(owners, endpoint.OwnerID())
	}), nil
}

func (r *EndpointRepository) Update(_ context.Context, endpoint webhook.Endpoint) error {
	return r.store.write(r.tx, func(tx *transaction) error {
		tx.writes = append(tx.writes, func() {
			if _, found := tx.store.webhooks.endpoints[endpoint.ID()]; found {
				tx.store.webhooks.endpoints[endpoint.ID()] = endpoint
			}
		})
		return nil
	})
}

func (r *EndpointRepository) selectEndpoints(keep func(webhook.Endpoint) bool) []webhook.Endpoint {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	selected := make([]webhook.Endpoint, 0, len(r.store.webhooks.endpoints))
	for _, endpoint := range r.store.webhooks.endpoints {
		if keep(endpoint) {
			selected = append(selected, endpoint)
		}
	}
	slices.SortFunc(selected, func(a, b webhook.Endpoint) int {
		return strings.Compare(string(a.ID()), string(b.ID()))
	})
	return selected
}

func (s *Store) checkEndpoint(id webhook.EndpointID) error {
	if _, taken := s.webhooks.endpoints[id]; taken {
		return ErrDuplicateID
	}
	return nil
}

type DeliveryRepository struct {
	store *Store
	tx    *transaction
}

func NewDeliveryRepository(store *Store) *DeliveryRepository {
	return &DeliveryRepository{store: store}
}

func (r *DeliveryRepository) FindByID(_ context.Context, id webhook.DeliveryID) (webhook.Delivery, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	found, ok := r.store.webhooks.deliveries[id]
	if !ok {
		return webhook.Delivery{}, webhook.ErrDeliveryNotFound
	}
	return found, nil
}

func (r *DeliveryRepository) ListByOwner(_ context.Context, filter webhookservice.DeliveryFilter, cursor page.Cursor, limit page.Limit) ([]webhook.Delivery, error) {
	matching := r.selectDeliveries(func(delivery webhook.Delivery) bool {
		return delivery.OwnerID() == filter.Owner && (filter.Status == "" || delivery.Status() == filter.Status)
	})
	return listing[webhook.Delivery]{id: deliveryIDText, cursor: cursor, limit: limit}.newestFirst(matching), nil
}

func (r *DeliveryRepository) ListPendingByEndpoint(_ context.Context, id webhook.EndpointID) ([]webhook.Delivery, error) {
	return r.selectDeliveries(func(delivery webhook.Delivery) bool {
		return delivery.EndpointID() == id && delivery.Status() == webhook.DeliveryPending
	}), nil
}

func (r *DeliveryRepository) Update(_ context.Context, delivery webhook.Delivery) error {
	return r.store.write(r.tx, func(tx *transaction) error {
		tx.writes = append(tx.writes, func() {
			if _, found := tx.store.webhooks.deliveries[delivery.ID()]; found {
				tx.store.webhooks.deliveries[delivery.ID()] = delivery
			}
		})
		return nil
	})
}

func (r *DeliveryRepository) SaveAll(_ context.Context, deliveries []webhook.Delivery) error {
	return r.store.write(r.tx, func(tx *transaction) error {
		tx.checks = append(tx.checks, func() error { return tx.store.checkDeliveries(deliveries) })
		tx.writes = append(tx.writes, func() { tx.store.insertDeliveries(deliveries) })
		return nil
	})
}

func (r *DeliveryRepository) selectDeliveries(keep func(webhook.Delivery) bool) []webhook.Delivery {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	selected := make([]webhook.Delivery, 0, len(r.store.webhooks.deliveries))
	for _, delivery := range r.store.webhooks.deliveries {
		if keep(delivery) {
			selected = append(selected, delivery)
		}
	}
	slices.SortFunc(selected, func(a, b webhook.Delivery) int {
		return strings.Compare(deliveryIDText(a), deliveryIDText(b))
	})
	return selected
}

func (s *Store) checkDeliveries(deliveries []webhook.Delivery) error {
	for _, delivery := range deliveries {
		if _, taken := s.webhooks.deliveries[delivery.ID()]; taken {
			return ErrDuplicateID
		}
	}
	return nil
}

func (s *Store) insertDeliveries(deliveries []webhook.Delivery) {
	for _, delivery := range deliveries {
		s.webhooks.deliveries[delivery.ID()] = delivery
	}
}

func deliveryIDText(delivery webhook.Delivery) string {
	return string(delivery.ID())
}
