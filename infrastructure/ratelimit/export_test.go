package ratelimit

func (l *TokenBucketLimiter) BucketCount() int {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return len(l.buckets)
}
