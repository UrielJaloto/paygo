package webhook

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	minimumAttempts = 1
	maximumAttempts = 20
	baseDelay       = time.Second
	maximumDelay    = time.Hour
)

var ErrInvalidRetryPolicy = errors.New("invalid retry policy")

type RetryPolicy struct {
	base            time.Duration
	delayLimit      time.Duration
	maximumAttempts int
}

func NewRetryPolicy(attempts int) (RetryPolicy, error) {
	if attempts < minimumAttempts || attempts > maximumAttempts {
		return RetryPolicy{}, fmt.Errorf("%w: %d attempts", ErrInvalidRetryPolicy, attempts)
	}
	return RetryPolicy{base: baseDelay, delayLimit: maximumDelay, maximumAttempts: attempts}, nil
}

func (p RetryPolicy) CalculateDelay(attempt int, jitter float64) time.Duration {
	delay := p.base
	for step := 1; step < attempt && delay < p.delayLimit; step++ {
		delay *= 2
	}
	return time.Duration(math.Round(float64(min(delay, p.delayLimit)) * (1 + jitter)))
}
