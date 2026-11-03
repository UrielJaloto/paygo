package webhook

import (
	"errors"
	"fmt"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
)

const (
	DeliveryPending   DeliveryStatus = "pending"
	DeliverySucceeded DeliveryStatus = "succeeded"
	DeliveryDead      DeliveryStatus = "dead"

	jitterLimit = 0.2
)

var (
	ErrDeliveryNotFound      = errors.New("delivery not found")
	ErrDeliveryNotReplayable = errors.New("delivery not replayable")
	ErrDeliveryNotPending    = errors.New("delivery not pending")
	ErrInvalidJitter         = errors.New("invalid jitter")
)

type DeliveryID string

type DeliveryStatus string

type DeliverySnapshot struct {
	ID            DeliveryID
	EventID       event.ID
	EndpointID    EndpointID
	OwnerID       apikey.OwnerID
	Status        DeliveryStatus
	Attempts      int
	NextAttemptAt time.Time
	LastError     string
	UpdatedAt     time.Time
}

type Failure struct {
	Reason string
	At     time.Time
	Policy RetryPolicy
	Jitter float64
}

type Delivery struct {
	id            DeliveryID
	eventID       event.ID
	endpointID    EndpointID
	ownerID       apikey.OwnerID
	status        DeliveryStatus
	attempts      int
	nextAttemptAt time.Time
	lastError     string
	updatedAt     time.Time
}

func NewDelivery(id DeliveryID, eventID event.ID, endpointID EndpointID, owner apikey.OwnerID, at time.Time) Delivery {
	return RestoreDelivery(DeliverySnapshot{
		ID:            id,
		EventID:       eventID,
		EndpointID:    endpointID,
		OwnerID:       owner,
		Status:        DeliveryPending,
		NextAttemptAt: at,
		UpdatedAt:     at,
	})
}

func RestoreDelivery(snapshot DeliverySnapshot) Delivery {
	return Delivery{
		id:            snapshot.ID,
		eventID:       snapshot.EventID,
		endpointID:    snapshot.EndpointID,
		ownerID:       snapshot.OwnerID,
		status:        snapshot.Status,
		attempts:      snapshot.Attempts,
		nextAttemptAt: snapshot.NextAttemptAt,
		lastError:     snapshot.LastError,
		updatedAt:     snapshot.UpdatedAt,
	}
}

func (d Delivery) ID() DeliveryID {
	return d.id
}

func (d Delivery) EventID() event.ID {
	return d.eventID
}

func (d Delivery) EndpointID() EndpointID {
	return d.endpointID
}

func (d Delivery) OwnerID() apikey.OwnerID {
	return d.ownerID
}

func (d Delivery) Status() DeliveryStatus {
	return d.status
}

func (d Delivery) Attempts() int {
	return d.attempts
}

func (d Delivery) NextAttemptAt() time.Time {
	return d.nextAttemptAt
}

func (d Delivery) LastError() string {
	return d.lastError
}

func (d Delivery) UpdatedAt() time.Time {
	return d.updatedAt
}

func (d *Delivery) Succeed(at time.Time) error {
	if d.status != DeliveryPending {
		return fmt.Errorf("%w: %s", ErrDeliveryNotPending, d.status)
	}
	d.status = DeliverySucceeded
	d.updatedAt = at
	return nil
}

func (d *Delivery) Fail(failure Failure) error {
	if d.status != DeliveryPending {
		return fmt.Errorf("%w: %s", ErrDeliveryNotPending, d.status)
	}
	if failure.Jitter < -jitterLimit || failure.Jitter > jitterLimit {
		return fmt.Errorf("%w: %v", ErrInvalidJitter, failure.Jitter)
	}
	d.attempts++
	d.lastError = failure.Reason
	d.updatedAt = failure.At
	if d.attempts >= failure.Policy.maxAttempts {
		d.status = DeliveryDead
		return nil
	}
	d.nextAttemptAt = failure.At.Add(failure.Policy.Delay(d.attempts, failure.Jitter))
	return nil
}

func (d *Delivery) Kill(reason string, at time.Time) {
	d.status = DeliveryDead
	d.lastError = reason
	d.updatedAt = at
}

func (d *Delivery) Replay(at time.Time) error {
	if d.status != DeliveryDead {
		return fmt.Errorf("%w: %s", ErrDeliveryNotReplayable, d.status)
	}
	d.status = DeliveryPending
	d.attempts = 0
	d.nextAttemptAt = at
	d.lastError = ""
	d.updatedAt = at
	return nil
}

func (d Delivery) IsDue(now time.Time) bool {
	return d.status == DeliveryPending && !d.nextAttemptAt.After(now)
}
