package ratelimit

import (
	"maps"
	"sync"
	"time"
)

const (
	idleLifetime  = 10 * time.Minute
	sweepInterval = time.Minute
)

type bucket struct {
	tokens  float64
	updated time.Time
}

type TokenBucketLimiter struct {
	now       func() time.Time
	rate      float64
	burst     float64
	mutex     sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

func NewTokenBucketLimiter(now func() time.Time, ratePerSecond, burst int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		now:       now,
		rate:      float64(ratePerSecond),
		burst:     float64(burst),
		buckets:   make(map[string]*bucket),
		lastSweep: now(),
	}
}

func (l *TokenBucketLimiter) Allow(key string) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	now := l.now()
	l.sweepIdleBuckets(now)
	current := l.refillBucket(key, now)
	if current.tokens < 1 {
		return false
	}
	current.tokens--
	return true
}

func (l *TokenBucketLimiter) refillBucket(key string, now time.Time) *bucket {
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

func (l *TokenBucketLimiter) sweepIdleBuckets(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now
	maps.DeleteFunc(l.buckets, func(_ string, candidate *bucket) bool {
		return now.Sub(candidate.updated) > idleLifetime
	})
}
