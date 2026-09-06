package webhook

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/webhook"
)

const disabledReason = "endpoint disabled"

var ErrInvalidDeliveryStatus = errors.New("invalid delivery status")

type EndpointRepository interface {
	Save(ctx context.Context, endpoint webhook.Endpoint) error
	FindByID(ctx context.Context, id webhook.EndpointID) (webhook.Endpoint, error)
	ListByOwner(ctx context.Context, owner apikey.OwnerID) ([]webhook.Endpoint, error)
	CountActiveByOwner(ctx context.Context, owner apikey.OwnerID) (int, error)
	Update(ctx context.Context, endpoint webhook.Endpoint) error
}

type DeliveryFilter struct {
	Owner  apikey.OwnerID
	Status webhook.DeliveryStatus
}

type DeliveryRepository interface {
	FindByID(ctx context.Context, id webhook.DeliveryID) (webhook.Delivery, error)
	ListByOwner(ctx context.Context, filter DeliveryFilter, cursor page.Cursor, limit page.Limit) ([]webhook.Delivery, error)
	ListPendingByEndpoint(ctx context.Context, id webhook.EndpointID) ([]webhook.Delivery, error)
	Update(ctx context.Context, delivery webhook.Delivery) error
}

type Store struct {
	Endpoints  EndpointRepository
	Deliveries DeliveryRepository
}

type UnitOfWork interface {
	Run(ctx context.Context, work func(ctx context.Context, store Store) error) error
}

type SecretGenerator interface {
	NewSecret() (string, error)
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewID() string
}

type Service struct {
	unitOfWork     UnitOfWork
	secrets SecretGenerator
	clock   Clock
	ids     IDGenerator
}

func NewMoney(unitOfWork UnitOfWork, secrets SecretGenerator, clock Clock, ids IDGenerator) *Service {
	return &Service{unitOfWork: unitOfWork, secrets: secrets, clock: clock, ids: ids}
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (RegisteredEndpoint, error) {
	owner, err := apikey.ParseOwnerID(input.OwnerID)
	if err != nil {
		return RegisteredEndpoint{}, err
	}
	target, err := webhook.ParseEndpointURL(input.URL)
	if err != nil {
		return RegisteredEndpoint{}, err
	}
	secret, err := s.secrets.NewSecret()
	if err != nil {
		return RegisteredEndpoint{}, fmt.Errorf("generating secret: %w", err)
	}
	endpoint, err := webhook.NewEndpoint(webhook.EndpointID(s.ids.NewID()), owner, target, secret, s.clock.Now())
	if err != nil {
		return RegisteredEndpoint{}, err
	}
	err = s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		return saveWithinLimit(ctx, store, endpoint)
	})
	if err != nil {
		return RegisteredEndpoint{}, err
	}
	return RegisteredEndpoint{EndpointOutput: toEndpointOutput(endpoint), Secret: secret}, nil
}

func (s *Service) ListEndpoints(ctx context.Context, ownerID string) ([]EndpointOutput, error) {
	owner, err := apikey.ParseOwnerID(ownerID)
	if err != nil {
		return nil, err
	}
	var found []webhook.Endpoint
	err = s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		found, err = store.Endpoints.ListByOwner(ctx, owner)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("listing endpoints: %w", err)
	}
	outputs := make([]EndpointOutput, 0, len(found))
	for _, endpoint := range found {
		outputs = append(outputs, toEndpointOutput(endpoint))
	}
	return outputs, nil
}

func (s *Service) Disable(ctx context.Context, query EndpointQuery) error {
	owner, err := apikey.ParseOwnerID(query.OwnerID)
	if err != nil {
		return err
	}
	return s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		return s.disableEndpoint(ctx, store, owner, webhook.EndpointID(query.EndpointID))
	})
}

func (s *Service) ListDeliveries(ctx context.Context, input DeliveryListInput) (DeliveryPage, error) {
	owner, err := apikey.ParseOwnerID(input.OwnerID)
	if err != nil {
		return DeliveryPage{}, err
	}
	status, err := parseStatus(input.Status)
	if err != nil {
		return DeliveryPage{}, err
	}
	cursor, limit, err := parsePage(input.Cursor, input.Limit)
	if err != nil {
		return DeliveryPage{}, err
	}
	var found []webhook.Delivery
	err = s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		found, err = store.Deliveries.ListByOwner(ctx, DeliveryFilter{Owner: owner, Status: status}, cursor, limit+1)
		return err
	})
	if err != nil {
		return DeliveryPage{}, fmt.Errorf("listing deliveries: %w", err)
	}
	return buildPage(found, limit), nil
}

func (s *Service) Replay(ctx context.Context, query DeliveryQuery) (DeliveryOutput, error) {
	owner, err := apikey.ParseOwnerID(query.OwnerID)
	if err != nil {
		return DeliveryOutput{}, err
	}
	var output DeliveryOutput
	err = s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		output, err = s.replayDelivery(ctx, store, owner, webhook.DeliveryID(query.DeliveryID))
		return err
	})
	if err != nil {
		return DeliveryOutput{}, err
	}
	return output, nil
}
