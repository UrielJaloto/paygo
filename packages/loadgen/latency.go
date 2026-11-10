package main

import (
	"math"
	"slices"
	"time"
)

type latencies struct {
	Median       time.Duration `json:"p50_ns"`
	Percentile95 time.Duration `json:"p95_ns"`
	Percentile99 time.Duration `json:"p99_ns"`
	Maximum      time.Duration `json:"p100_ns"`
}

const (
	median       = 50
	percentile95 = 95
	percentile99 = 99
	maximum      = 100
)

func summarizeLatencies(samples []time.Duration) latencies {
	sorted := slices.Sorted(slices.Values(samples))
	return latencies{
		Median:       findPercentile(sorted, median),
		Percentile95: findPercentile(sorted, percentile95),
		Percentile99: findPercentile(sorted, percentile99),
		Maximum:      findPercentile(sorted, maximum),
	}
}

func findPercentile(sorted []time.Duration, rank int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	position := int(math.Ceil(float64(rank) / percentScale * float64(len(sorted))))
	return sorted[max(position, 1)-1]
}
