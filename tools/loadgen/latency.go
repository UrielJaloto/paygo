package main

import (
	"math"
	"slices"
	"time"
)

type latencies struct {
	P50  time.Duration `json:"p50_ns"`
	P95  time.Duration `json:"p95_ns"`
	P99  time.Duration `json:"p99_ns"`
	P100 time.Duration `json:"p100_ns"`
}

func summarize(samples []time.Duration) latencies {
	sorted := slices.Sorted(slices.Values(samples))
	return latencies{
		P50:  percentile(sorted, 50),
		P95:  percentile(sorted, 95),
		P99:  percentile(sorted, 99),
		P100: percentile(sorted, 100),
	}
}

func percentile(sorted []time.Duration, rank int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	position := int(math.Ceil(float64(rank) / 100 * float64(len(sorted))))
	return sorted[max(position, 1)-1]
}
