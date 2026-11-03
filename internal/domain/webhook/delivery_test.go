package webhook_test

import (
	"errors"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/webhook"
)

var at = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func policy(t *testing.T, attempts int) webhook.RetryPolicy {
	t.Helper()
	created, err := webhook.NewRetryPolicy(attempts)
	if err != nil {
		t.Fatalf("NewRetryPolicy: %v", err)
	}
	return created
}

func pending() webhook.Delivery {
	return webhook.NewDelivery("d1", "ev1", "e1", "o1", at)
}

func withStatus(status webhook.DeliveryStatus, attempts int) webhook.Delivery {
	return webhook.RestoreDelivery(webhook.DeliverySnapshot{ID: "d1", Status: status, Attempts: attempts, NextAttemptAt: at})
}

func TestNewRetryPolicy_ValidatesAttempts(t *testing.T) {
	cases := []struct {
		name     string
		attempts int
		wantErr  error
	}{
		{name: "zero", attempts: 0, wantErr: webhook.ErrInvalidRetryPolicy},
		{name: "one", attempts: 1},
		{name: "twenty", attempts: 20},
		{name: "twenty one", attempts: 21, wantErr: webhook.ErrInvalidRetryPolicy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := webhook.NewRetryPolicy(tc.attempts); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDelay_FollowsBackoff(t *testing.T) {
	cases := []struct {
		name    string
		attempt int
		jitter  float64
		want    time.Duration
	}{
		{name: "attempt 1", attempt: 1, want: time.Second},
		{name: "attempt 4", attempt: 4, want: 8 * time.Second},
		{name: "attempt 13 caps", attempt: 13, want: time.Hour},
		{name: "attempt 1000 caps", attempt: 1000, want: time.Hour},
		{name: "positive jitter", attempt: 1, jitter: 0.2, want: 1200 * time.Millisecond},
		{name: "negative jitter", attempt: 1, jitter: -0.2, want: 800 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy(t, 8).Delay(tc.attempt, tc.jitter); got != tc.want {
				t.Fatalf("delay = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewDelivery_IsPendingAndDueNow(t *testing.T) {
	created := pending()
	if created.ID() != "d1" || created.EventID() != "ev1" || created.EndpointID() != "e1" || created.OwnerID() != "o1" {
		t.Fatalf("unexpected identity: %+v", created)
	}
	if created.Status() != webhook.DeliveryPending || created.Attempts() != 0 || created.LastError() != "" {
		t.Fatalf("unexpected state: %+v", created)
	}
	if !created.NextAttemptAt().Equal(at) || !created.UpdatedAt().Equal(at) {
		t.Fatalf("unexpected times: %+v", created)
	}
}

func TestFail_BelowMax_SchedulesRetry(t *testing.T) {
	target := pending()
	later := at.Add(time.Minute)
	if err := target.Fail(webhook.Failure{Reason: "boom", At: later, Policy: policy(t, 8)}); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if target.Attempts() != 1 || target.Status() != webhook.DeliveryPending || target.LastError() != "boom" {
		t.Fatalf("unexpected state: %+v", target)
	}
	if !target.NextAttemptAt().Equal(later.Add(time.Second)) || !target.UpdatedAt().Equal(later) {
		t.Fatalf("unexpected times: %+v", target)
	}
}

func TestFail_AtMax_IsDead(t *testing.T) {
	target := withStatus(webhook.DeliveryPending, 7)
	if err := target.Fail(webhook.Failure{Reason: "boom", At: at, Policy: policy(t, 8)}); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if target.Attempts() != 8 || target.Status() != webhook.DeliveryDead {
		t.Fatalf("unexpected state: %+v", target)
	}
}

func TestFail_InvalidJitter_ReturnsErrInvalidJitter(t *testing.T) {
	for _, jitter := range []float64{0.21, -0.21} {
		target := pending()
		err := target.Fail(webhook.Failure{Reason: "boom", At: at, Policy: policy(t, 8), Jitter: jitter})
		if !errors.Is(err, webhook.ErrInvalidJitter) {
			t.Fatalf("jitter %v: error = %v", jitter, err)
		}
	}
}

func TestFail_Succeeded_ReturnsErrDeliveryNotPending(t *testing.T) {
	target := withStatus(webhook.DeliverySucceeded, 1)
	err := target.Fail(webhook.Failure{Reason: "boom", At: at, Policy: policy(t, 8)})
	if !errors.Is(err, webhook.ErrDeliveryNotPending) {
		t.Fatalf("error = %v, want ErrDeliveryNotPending", err)
	}
}

func TestSucceed_Pending_IsSucceeded(t *testing.T) {
	target := pending()
	later := at.Add(time.Minute)
	if err := target.Succeed(later); err != nil {
		t.Fatalf("Succeed: %v", err)
	}
	if target.Status() != webhook.DeliverySucceeded || !target.UpdatedAt().Equal(later) {
		t.Fatalf("unexpected state: %+v", target)
	}
}

func TestSucceed_Dead_ReturnsErrDeliveryNotPending(t *testing.T) {
	target := withStatus(webhook.DeliveryDead, 8)
	if err := target.Succeed(at); !errors.Is(err, webhook.ErrDeliveryNotPending) {
		t.Fatalf("error = %v, want ErrDeliveryNotPending", err)
	}
}

func TestReplay_Dead_ResetsToPending(t *testing.T) {
	target := withStatus(webhook.DeliveryDead, 8)
	later := at.Add(time.Hour)
	if err := target.Replay(later); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if target.Status() != webhook.DeliveryPending || target.Attempts() != 0 || target.LastError() != "" {
		t.Fatalf("unexpected state: %+v", target)
	}
	if !target.NextAttemptAt().Equal(later) || !target.UpdatedAt().Equal(later) {
		t.Fatalf("unexpected times: %+v", target)
	}
}

func TestReplay_NotDead_ReturnsErrDeliveryNotReplayable(t *testing.T) {
	for _, status := range []webhook.DeliveryStatus{webhook.DeliveryPending, webhook.DeliverySucceeded} {
		target := withStatus(status, 1)
		if err := target.Replay(at); !errors.Is(err, webhook.ErrDeliveryNotReplayable) {
			t.Fatalf("%s: error = %v", status, err)
		}
	}
}

func TestKill_Pending_IsDeadWithReason(t *testing.T) {
	target := pending()
	target.Kill("endpoint disabled", at)
	if target.Status() != webhook.DeliveryDead || target.LastError() != "endpoint disabled" {
		t.Fatalf("unexpected state: %+v", target)
	}
}

func TestIsDue_ComparesNextAttempt(t *testing.T) {
	cases := []struct {
		name   string
		status webhook.DeliveryStatus
		now    time.Time
		want   bool
	}{
		{name: "next before now", status: webhook.DeliveryPending, now: at.Add(time.Second), want: true},
		{name: "next equal now", status: webhook.DeliveryPending, now: at, want: true},
		{name: "next after now", status: webhook.DeliveryPending, now: at.Add(-time.Second), want: false},
		{name: "not pending", status: webhook.DeliveryDead, now: at.Add(time.Second), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := withStatus(tc.status, 0).IsDue(tc.now); got != tc.want {
				t.Fatalf("IsDue = %v, want %v", got, tc.want)
			}
		})
	}
}
