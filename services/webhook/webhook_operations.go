package webhook

import (
	"context"
	"fmt"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/webhook"
)

func saveWithinLimit(ctx context.Context, store Store, endpoint webhook.Endpoint) error {
	active, err := store.Endpoints.CountActiveByOwner(ctx, endpoint.OwnerID())
	if err != nil {
		return fmt.Errorf("counting endpoints: %w", err)
	}
	if active >= webhook.MaxActiveEndpoints {
		return webhook.ErrEndpointLimitReached
	}
	if err := store.Endpoints.Save(ctx, endpoint); err != nil {
		return fmt.Errorf("saving endpoint: %w", err)
	}
	return nil
}

func (s *Service) disable(ctx context.Context, store Store, owner apikey.OwnerID, id webhook.EndpointID) error {
	endpoint, err := store.Endpoints.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("finding endpoint: %w", err)
	}
	if endpoint.OwnerID() != owner {
		return fmt.Errorf("%w: %s", webhook.ErrEndpointNotFound, id)
	}
	if err := endpoint.Disable(); err != nil {
		return err
	}
	if err := store.Endpoints.Update(ctx, endpoint); err != nil {
		return fmt.Errorf("updating endpoint: %w", err)
	}
	return s.killPending(ctx, store, id)
}

func (s *Service) killPending(ctx context.Context, store Store, id webhook.EndpointID) error {
	pending, err := store.Deliveries.ListPendingByEndpoint(ctx, id)
	if err != nil {
		return fmt.Errorf("listing pending deliveries: %w", err)
	}
	for _, delivery := range pending {
		delivery.Kill(disabledReason, s.clock.Now())
		if err := store.Deliveries.Update(ctx, delivery); err != nil {
			return fmt.Errorf("updating delivery: %w", err)
		}
	}
	return nil
}

func (s *Service) replay(ctx context.Context, store Store, owner apikey.OwnerID, id webhook.DeliveryID) (DeliveryOutput, error) {
	delivery, err := store.Deliveries.FindByID(ctx, id)
	if err != nil {
		return DeliveryOutput{}, fmt.Errorf("finding delivery: %w", err)
	}
	if delivery.OwnerID() != owner {
		return DeliveryOutput{}, fmt.Errorf("%w: %s", webhook.ErrDeliveryNotFound, id)
	}
	if err := delivery.Replay(s.clock.Now()); err != nil {
		return DeliveryOutput{}, err
	}
	if err := store.Deliveries.Update(ctx, delivery); err != nil {
		return DeliveryOutput{}, fmt.Errorf("updating delivery: %w", err)
	}
	return toDeliveryOutput(delivery), nil
}

func parseStatus(text string) (webhook.DeliveryStatus, error) {
	status := webhook.DeliveryStatus(text)
	switch status {
	case "", webhook.DeliveryPending, webhook.DeliverySucceeded, webhook.DeliveryDead:
		return status, nil
	}
	return "", fmt.Errorf("%w: %q", ErrInvalidDeliveryStatus, text)
}

func parsePage(cursorText, limitText string) (page.Cursor, page.Limit, error) {
	cursor, err := page.ParseCursor(cursorText)
	if err != nil {
		return page.Cursor{}, 0, err
	}
	limit, err := page.ParseLimit(limitText)
	if err != nil {
		return page.Cursor{}, 0, err
	}
	return cursor, limit, nil
}

func buildPage(found []webhook.Delivery, limit page.Limit) DeliveryPage {
	kept, next := found, ""
	if len(found) > int(limit) {
		kept = found[:limit]
		next = page.NewCursor(string(kept[len(kept)-1].ID())).Encode()
	}
	items := make([]DeliveryOutput, 0, len(kept))
	for _, delivery := range kept {
		items = append(items, toDeliveryOutput(delivery))
	}
	return DeliveryPage{Items: items, NextCursor: next}
}

func toEndpointOutput(endpoint webhook.Endpoint) EndpointOutput {
	return EndpointOutput{
		ID:        string(endpoint.ID()),
		URL:       string(endpoint.URL()),
		IsActive:  endpoint.IsActive(),
		CreatedAt: endpoint.CreatedAt(),
	}
}

func toDeliveryOutput(delivery webhook.Delivery) DeliveryOutput {
	return DeliveryOutput{
		ID:            string(delivery.ID()),
		EventID:       string(delivery.EventID()),
		EndpointID:    string(delivery.EndpointID()),
		Status:        string(delivery.Status()),
		Attempts:      delivery.Attempts(),
		NextAttemptAt: delivery.NextAttemptAt(),
		LastError:     delivery.LastError(),
	}
}
