package main

import (
	"context"
	"errors"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"
)

type ledgerStub struct {
	mutex    sync.Mutex
	balances map[string]int64
	failKey  string
}

func (l *ledgerStub) CreateAccounts(context.Context, int) ([]string, error) { return nil, nil }

func (l *ledgerStub) Transfer(_ context.Context, move movement) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if move.key == l.failKey {
		return errors.New("rejected")
	}
	l.balances[move.from] -= move.amountMinor
	l.balances[move.to] += move.amountMinor
	return nil
}

func (l *ledgerStub) FetchBalances(context.Context) (map[string]int64, error) { return l.balances, nil }

func TestSummarizeLatencies_KnownSamples_ReturnsNearestRank(t *testing.T) {
	samples := make([]time.Duration, 0, 100)
	for value := 100; value >= 1; value-- {
		samples = append(samples, time.Duration(value))
	}
	want := latencies{Median: 50, Percentile95: 95, Percentile99: 99, Maximum: 100}
	if got := summarizeLatencies(samples); got != want {
		t.Errorf("summarize = %+v, want %+v", got, want)
	}
}

func TestSummarizeLatencies_EdgeSizes_ReturnsExpected(t *testing.T) {
	tests := []struct {
		name    string
		samples []time.Duration
		want    latencies
	}{
		{name: "empty", samples: nil, want: latencies{}},
		{name: "single", samples: []time.Duration{7}, want: latencies{Median: 7, Percentile95: 7, Percentile99: 7, Maximum: 7}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := summarizeLatencies(testCase.samples); got != testCase.want {
				t.Errorf("summarize = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

func TestPicker_Contention_PicksHotAccountsNinetyPercent(t *testing.T) {
	chosen, err := newPicker(scenarioContention, 1000, 1)
	if err != nil {
		t.Fatalf("newPicker: %v", err)
	}
	const draws = 100000
	hot := 0
	for range draws {
		from, to := chosen.pickPair()
		if from == to {
			t.Fatalf("from equals to: %d", from)
		}
		if from < 10 {
			hot++
		}
	}
	share := float64(hot) / draws
	if math.Abs(share-0.9) > 0.01 {
		t.Errorf("hot share = %.4f, want 0.90 within 0.01", share)
	}
}

func TestPicker_Uniform_SpreadsAcrossAccounts(t *testing.T) {
	chosen, err := newPicker(scenarioUniform, 1000, 1)
	if err != nil {
		t.Fatalf("newPicker: %v", err)
	}
	const draws = 100000
	hot := 0
	for range draws {
		if from, _ := chosen.pickPair(); from < 10 {
			hot++
		}
	}
	if share := float64(hot) / draws; math.Abs(share-0.01) > 0.01 {
		t.Errorf("hot share = %.4f, want 0.01 within 0.01", share)
	}
}

func TestNewPicker_UnknownScenario_ReturnsError(t *testing.T) {
	if _, err := newPicker("storm", 10, 1); !errors.Is(err, errUnknownScenario) {
		t.Errorf("err = %v, want errUnknownScenario", err)
	}
}

func TestDriveLoad_AllAccepted_ExpectedMatchesBalances(t *testing.T) {
	accounts := make([]string, 10)
	stub := &ledgerStub{balances: make(map[string]int64)}
	for index := range accounts {
		accounts[index] = "account-" + strconv.Itoa(index)
		stub.balances[accounts[index]] = calculateFunding(1000)
	}
	result, err := driveLoad(context.Background(), stub, workload{scenarioContention, accounts, 1000, 8, 1, "run"})
	if err != nil {
		t.Fatalf("drive: %v", err)
	}
	if len(result.samples) != 1000 || result.failures != 0 {
		t.Errorf("samples = %d, failures = %d", len(result.samples), result.failures)
	}
	if !matchesBalances(result.expected, stub.balances) {
		t.Errorf("expected %v, balances %v", result.expected, stub.balances)
	}
}

func TestDriveLoad_RejectedTransfer_IsCountedAndNotApplied(t *testing.T) {
	stub := &ledgerStub{balances: map[string]int64{"a": calculateFunding(5), "b": calculateFunding(5)}, failKey: "run-0-3"}
	result, err := driveLoad(context.Background(), stub, workload{scenarioUniform, []string{"a", "b"}, 5, 1, 1, "run"})
	if err != nil {
		t.Fatalf("drive: %v", err)
	}
	if result.failures != 1 || !matchesBalances(result.expected, stub.balances) {
		t.Errorf("failures = %d, expected %v, balances %v", result.failures, result.expected, stub.balances)
	}
}

func TestMatchesBalances_Mismatches_ReturnFalse(t *testing.T) {
	tests := []struct {
		name   string
		actual map[string]int64
		want   bool
	}{
		{name: "equal", actual: map[string]int64{"a": 1, "b": 2}, want: true},
		{name: "different balance", actual: map[string]int64{"a": 1, "b": 3}},
		{name: "missing account", actual: map[string]int64{"a": 1}},
		{name: "other account", actual: map[string]int64{"a": 1, "c": 2}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := matchesBalances(map[string]int64{"a": 1, "b": 2}, testCase.actual); got != testCase.want {
				t.Errorf("correct = %t, want %t", got, testCase.want)
			}
		})
	}
}
