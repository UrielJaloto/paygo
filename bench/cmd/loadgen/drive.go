package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/UrielJaloto/paygo/bench/targets"
)

const transferMinor = 100

type workload struct {
	scenario  string
	accounts  []string
	transfers int
	clients   int
	seed      uint64
}

type outcome struct {
	samples  []time.Duration
	failures int
	expected map[string]int64
	elapsed  time.Duration
}

type clientResult struct {
	samples  []time.Duration
	failures int
	deltas   map[string]int64
}

func drive(ctx context.Context, target targets.Target, load workload) (outcome, error) {
	pickers := make([]*picker, load.clients)
	for client := range pickers {
		chosen, err := newPicker(load.scenario, len(load.accounts), load.seed+uint64(client))
		if err != nil {
			return outcome{}, fmt.Errorf("scenario %q: %w", load.scenario, err)
		}
		pickers[client] = chosen
	}
	results := make([]clientResult, load.clients)
	var issued atomic.Int64
	var clients sync.WaitGroup
	started := time.Now()
	for client := range pickers {
		clients.Go(func() {
			results[client] = runClient(ctx, target, load, clientState{client, pickers[client], &issued})
		})
	}
	clients.Wait()
	return merge(results, load, time.Since(started)), nil
}

type clientState struct {
	index  int
	picker *picker
	issued *atomic.Int64
}

func runClient(ctx context.Context, target targets.Target, load workload, state clientState) clientResult {
	result := clientResult{deltas: make(map[string]int64)}
	for sequence := state.issued.Add(1); sequence <= int64(load.transfers); sequence = state.issued.Add(1) {
		from, to := state.picker.next()
		key := fmt.Sprintf("bench-%d-%d-%d", load.seed, state.index, sequence)
		started := time.Now()
		err := target.Transfer(ctx, load.accounts[from], load.accounts[to], transferMinor, key)
		result.samples = append(result.samples, time.Since(started))
		if err != nil {
			result.failures++
			continue
		}
		result.deltas[load.accounts[from]] -= transferMinor
		result.deltas[load.accounts[to]] += transferMinor
	}
	return result
}

func merge(results []clientResult, load workload, elapsed time.Duration) outcome {
	merged := outcome{expected: make(map[string]int64, len(load.accounts)), elapsed: elapsed}
	for _, account := range load.accounts {
		merged.expected[account] = fundingFor(load.transfers)
	}
	for _, result := range results {
		merged.samples = append(merged.samples, result.samples...)
		merged.failures += result.failures
		for account, delta := range result.deltas {
			merged.expected[account] += delta
		}
	}
	return merged
}

func fundingFor(transfers int) int64 {
	return int64(transfers) * transferMinor
}

func correct(expected, actual map[string]int64) bool {
	if len(expected) != len(actual) {
		return false
	}
	for account, balance := range expected {
		if got, found := actual[account]; !found || got != balance {
			return false
		}
	}
	return true
}
