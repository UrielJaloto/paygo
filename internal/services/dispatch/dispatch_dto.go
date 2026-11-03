package dispatch

import (
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/webhook"
)

type FanOutResult struct {
	Events     int
	Deliveries int
}

type DeliverInput struct {
	Limit  int
	Lease  time.Duration
	Policy webhook.RetryPolicy
}

type DeliverResult struct {
	Succeeded int
	Retried   int
	Dead      int
}

func (r *DeliverResult) count(status webhook.DeliveryStatus) {
	switch status {
	case webhook.DeliverySucceeded:
		r.Succeeded++
	case webhook.DeliveryDead:
		r.Dead++
	default:
		r.Retried++
	}
}
