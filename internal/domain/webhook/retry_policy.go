package webhook

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	minAttempts  = 1
	maxAttempts  = 20
	baseDelay    = time.Second
	maximumDelay = time.Hour
)

var ErrInvalidRetryPolicy = errors.New("invalid retry policy")

type RetryPolicy struct {
	base        time.Duration
	cap         time.Duration
	maxAttempts int
}

func NewRetryPolicy(attempts int) (RetryPolicy, error) {
	if attempts < minAttempts || attempts > maxAttempts {
		return RetryPolicy{}, fmt.Errorf("%w: %d attempts", ErrInvalidRetryPolicy, attempts)
	}
	return RetryPolicy{base: baseDelay, cap: maximumDelay, maxAttempts: attempts}, nil
}

func (p RetryPolicy) Delay(attempt int, jitter float64) time.Duration {
	delay := p.base
	for step := 1; step < attempt && delay < p.cap; step++ {
		delay *= 2
	}
	return time.Duration(math.Round(float64(min(delay, p.cap)) * (1 + jitter)))
}
