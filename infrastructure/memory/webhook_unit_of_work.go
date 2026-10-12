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
	transaction := u.store.begin()
	defer transaction.release()
	err := work(ctx, webhookservice.Store{
		Endpoints:  &EndpointRepository{store: u.store, transaction: transaction},
		Deliveries: &DeliveryRepository{store: u.store, transaction: transaction},
	})
	if err != nil {
		return err
	}
	return transaction.commit()
}

type DispatchUnitOfWork struct {
	store *Store
	ids   dispatch.IDGenerator
}

func NewDispatchUnitOfWork(store *Store, ids dispatch.IDGenerator) *DispatchUnitOfWork {
	return &DispatchUnitOfWork{store: store, ids: ids}
}

func (u *DispatchUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store dispatch.Store) error) error {
	transaction := u.store.begin()
	defer transaction.release()
	err := work(ctx, dispatch.Store{
		Outbox:     &OutboxRepository{store: u.store, transaction: transaction},
		Endpoints:  &EndpointRepository{store: u.store, transaction: transaction},
		Deliveries: &DeliveryRepository{store: u.store, transaction: transaction},
		IDs:        u.ids,
	})
	if err != nil {
		return err
	}
	return transaction.commit()
}
