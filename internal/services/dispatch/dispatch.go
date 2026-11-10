package dispatch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/webhook"
)

const maximumReasonLength = 500

type OutboxReader interface {
	ClaimUnpublished(ctx context.Context, limit int) ([]event.Event, error)
	MarkPublished(ctx context.Context, ids []string) error
}

type EndpointFinder interface {
	ListActiveByOwners(ctx context.Context, owners []apikey.OwnerID) ([]webhook.Endpoint, error)
}

type DeliveryWriter interface {
	SaveAll(ctx context.Context, deliveries []webhook.Delivery) error
}

type IDGenerator interface {
	NewID() string
}

type Store struct {
	Outbox     OutboxReader
	Endpoints  EndpointFinder
	Deliveries DeliveryWriter
	IDs        IDGenerator
}

type UnitOfWork interface {
	Run(ctx context.Context, work func(ctx context.Context, store Store) error) error
}

type Claim struct {
	Delivery webhook.Delivery
	Endpoint webhook.Endpoint
	Event    event.Event
}

type DeliveryQueue interface {
	ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Claim, error)
	Update(ctx context.Context, delivery webhook.Delivery) error
}

type Sender interface {
	Send(ctx context.Context, endpoint webhook.Endpoint, occurred event.Event) error
}

type Jitter interface {
	Next() float64
}

type Service struct {
	unitOfWork UnitOfWork
	queue      DeliveryQueue
	sender     Sender
	now        func() time.Time
	jitter     Jitter
}

type sendOutcome struct {
	delivery webhook.Delivery
	err      error
}

func NewService(unitOfWork UnitOfWork, queue DeliveryQueue, sender Sender, now func() time.Time, jitter Jitter) *Service {
	return &Service{unitOfWork: unitOfWork, queue: queue, sender: sender, now: now, jitter: jitter}
}

func (s *Service) FanOut(ctx context.Context, limit int) (FanOutResult, error) {
	var result FanOutResult
	err := s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		var err error
		result, err = s.fanOutBatch(ctx, store, limit)
		return err
	})
	if err != nil {
		return FanOutResult{}, err
	}
	return result, nil
}

func (s *Service) DeliverDue(ctx context.Context, input DeliverInput) (DeliverResult, error) {
	claims, err := s.queue.ClaimDue(ctx, s.now(), input.Lease, input.Limit)
	if err != nil {
		return DeliverResult{}, fmt.Errorf("claiming deliveries: %w", err)
	}
	var result DeliverResult
	var failures []error
	for outcome := range s.sendDeliveries(ctx, claims) {
		status, err := s.settleDelivery(ctx, outcome, input.Policy)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		result.countStatus(status)
	}
	return result, errors.Join(failures...)
}

func (s *Service) fanOutBatch(ctx context.Context, store Store, limit int) (FanOutResult, error) {
	events, err := store.Outbox.ClaimUnpublished(ctx, limit)
	if err != nil {
		return FanOutResult{}, fmt.Errorf("claiming events: %w", err)
	}
	if len(events) == 0 {
		return FanOutResult{}, nil
	}
	endpoints, err := store.Endpoints.ListActiveByOwners(ctx, collectUniqueOwners(events))
	if err != nil {
		return FanOutResult{}, fmt.Errorf("listing endpoints: %w", err)
	}
	deliveries := s.buildDeliveries(store.IDs, events, endpoints)
	if err := store.Deliveries.SaveAll(ctx, deliveries); err != nil {
		return FanOutResult{}, fmt.Errorf("saving deliveries: %w", err)
	}
	if err := store.Outbox.MarkPublished(ctx, collectEventIDs(events)); err != nil {
		return FanOutResult{}, fmt.Errorf("marking events published: %w", err)
	}
	return FanOutResult{Events: len(events), Deliveries: len(deliveries)}, nil
}

func (s *Service) buildDeliveries(ids IDGenerator, events []event.Event, endpoints []webhook.Endpoint) []webhook.Delivery {
	byOwner := groupByOwner(endpoints)
	var subscriptions []subscription
	for _, occurred := range events {
		subscriptions = append(subscriptions, buildSubscriptions(occurred, selectSubscribers(occurred, byOwner))...)
	}
	now := s.now()
	deliveries := make([]webhook.Delivery, 0, len(subscriptions))
	for _, pair := range subscriptions {
		deliveries = append(deliveries, webhook.NewDelivery(ids.NewID(), pair.event.ID(), pair.endpoint.ID(), pair.endpoint.OwnerID(), now))
	}
	return deliveries
}

type subscription struct {
	event    event.Event
	endpoint webhook.Endpoint
}

func groupByOwner(endpoints []webhook.Endpoint) map[apikey.OwnerID][]webhook.Endpoint {
	byOwner := make(map[apikey.OwnerID][]webhook.Endpoint)
	for _, endpoint := range endpoints {
		byOwner[endpoint.OwnerID()] = append(byOwner[endpoint.OwnerID()], endpoint)
	}
	return byOwner
}

func buildSubscriptions(occurred event.Event, endpoints []webhook.Endpoint) []subscription {
	pairs := make([]subscription, 0, len(endpoints))
	for _, endpoint := range endpoints {
		pairs = append(pairs, subscription{event: occurred, endpoint: endpoint})
	}
	return pairs
}

func selectSubscribers(occurred event.Event, byOwner map[apikey.OwnerID][]webhook.Endpoint) []webhook.Endpoint {
	var endpoints []webhook.Endpoint
	for _, owner := range occurred.OwnerIDs() {
		endpoints = append(endpoints, byOwner[owner]...)
	}
	return endpoints
}

func (s *Service) sendDeliveries(ctx context.Context, claims []Claim) <-chan sendOutcome {
	outcomes := make(chan sendOutcome, len(claims))
	var running sync.WaitGroup
	for _, claim := range claims {
		running.Go(func() {
			outcomes <- sendOutcome{delivery: claim.Delivery, err: s.sender.Send(ctx, claim.Endpoint, claim.Event)}
		})
	}
	running.Wait()
	close(outcomes)
	return outcomes
}

func (s *Service) settleDelivery(ctx context.Context, outcome sendOutcome, policy webhook.RetryPolicy) (string, error) {
	delivery := outcome.delivery
	if err := s.applySendOutcome(&delivery, outcome.err, policy); err != nil {
		return "", fmt.Errorf("settling delivery %s: %w", delivery.ID(), err)
	}
	if err := s.queue.Update(ctx, delivery); err != nil {
		return "", fmt.Errorf("updating delivery %s: %w", delivery.ID(), err)
	}
	return delivery.Status(), nil
}

func (s *Service) applySendOutcome(delivery *webhook.Delivery, sendErr error, policy webhook.RetryPolicy) error {
	now := s.now()
	if sendErr == nil {
		return delivery.MarkSucceeded(now)
	}
	return delivery.MarkFailed(webhook.Failure{Reason: truncateReason(sendErr.Error()), At: now, Policy: policy, Jitter: s.jitter.Next()})
}

func collectUniqueOwners(events []event.Event) []apikey.OwnerID {
	var owners []apikey.OwnerID
	for _, occurred := range events {
		owners = append(owners, occurred.OwnerIDs()...)
	}
	slices.Sort(owners)
	return slices.Compact(owners)
}

func collectEventIDs(events []event.Event) []string {
	ids := make([]string, 0, len(events))
	for _, occurred := range events {
		ids = append(ids, occurred.ID())
	}
	return ids
}

func truncateReason(reason string) string {
	runes := []rune(reason)
	if len(runes) <= maximumReasonLength {
		return reason
	}
	return string(runes[:maximumReasonLength])
}
