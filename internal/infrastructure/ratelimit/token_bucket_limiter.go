package ratelimit

import (
	"maps"
	"sync"
	"time"
)

const (
	idleTTL       = 10 * time.Minute
	sweepInterval = time.Minute
)

type Clock interface {
	Now() time.Time
}

type bucket struct {
	tokens  float64
	updated time.Time
}

type TokenBucketLimiter struct {
	clock     Clock
	rate      float64
	burst     float64
	mutex     sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

func New(clock Clock, ratePerSecond, burst int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		clock:     clock,
		rate:      float64(ratePerSecond),
		burst:     float64(burst),
		buckets:   make(map[string]*bucket),
		lastSweep: clock.Now(),
	}
}

func (l *TokenBucketLimiter) Allow(key string) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	now := l.clock.Now()
	l.sweep(now)
	current := l.refilled(key, now)
	if current.tokens < 1 {
		return false
	}
	current.tokens--
	return true
}

func (l *TokenBucketLimiter) refilled(key string, now time.Time) *bucket {
	current, found := l.buckets[key]
	if !found {
		current = &bucket{tokens: l.burst, updated: now}
		l.buckets[key] = current
		return current
	}
	elapsed := max(now.Sub(current.updated).Seconds(), 0)
	current.tokens = min(current.tokens+elapsed*l.rate, l.burst)
	current.updated = now
	return current
}

func (l *TokenBucketLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now
	maps.DeleteFunc(l.buckets, func(_ string, candidate *bucket) bool {
		return now.Sub(candidate.updated) > idleTTL
	})
}
