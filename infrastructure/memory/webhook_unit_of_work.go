package memory

import (
	"context"

	"github.com/UrielJaloto/paygo/services/dispatch"
	webhookservice "github.com/UrielJaloto/paygo/services/webhook"
)

type WebhookUnitOfWork struct {
	store *Store
}

func NewWebhookUnitOfWork(store *Store) *WebhookUnitOfWork {
	return &WebhookUnitOfWork{store: store}
}

func (u *WebhookUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store webhookservice.Store) error) error {
	tx := u.store.begin()
	defer tx.release()
	err := work(ctx, webhookservice.Store{
		Endpoints:  &EndpointRepository{store: u.store, tx: tx},
		Deliveries: &DeliveryRepository{store: u.store, tx: tx},
	})
	if err != nil {
		return err
	}
	return tx.commit()
}

type DispatchUnitOfWork struct {
	store *Store
	ids   dispatch.IDGenerator
}

func NewDispatchUnitOfWork(store *Store, ids dispatch.IDGenerator) *DispatchUnitOfWork {
	return &DispatchUnitOfWork{store: store, ids: ids}
}

func (u *DispatchUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store dispatch.Store) error) error {
	tx := u.store.begin()
	defer tx.release()
	err := work(ctx, dispatch.Store{
		Outbox:     &OutboxRepository{store: u.store, tx: tx},
		Endpoints:  &EndpointRepository{store: u.store, tx: tx},
		Deliveries: &DeliveryRepository{store: u.store, tx: tx},
		IDs:        u.ids,
	})
	if err != nil {
		return err
	}
	return tx.commit()
}
