package storetest_test

import (
	"errors"
	"testing"

	"github.com/UrielJaloto/paygo/internal/storetest"
)

type recordingTB struct {
	testing.TB
	failed bool
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Fatalf(string, ...any) { r.failed = true }

var errSample = errors.New("sample")

func TestAssertions_Inputs_ReportMismatchOnly(t *testing.T) {
	tests := []struct {
		name  string
		check func(testing.TB)
		fails bool
	}{
		{"no error passes", func(tb testing.TB) { storetest.NoError(tb, nil) }, false},
		{"no error fails", func(tb testing.TB) { storetest.NoError(tb, errSample) }, true},
		{"error is passes", func(tb testing.TB) { storetest.ErrorIs(tb, errSample, errSample) }, false},
		{"error is fails", func(tb testing.TB) { storetest.ErrorIs(tb, nil, errSample) }, true},
		{"equal passes", func(tb testing.TB) { storetest.Equal(tb, 1, 1) }, false},
		{"equal fails", func(tb testing.TB) { storetest.Equal(tb, 1, 2) }, true},
		{"slices pass", func(tb testing.TB) { storetest.EqualSlices(tb, []int{1}, []int{1}) }, false},
		{"slices fail", func(tb testing.TB) { storetest.EqualSlices(tb, []int{1}, []int{2}) }, true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := &recordingTB{}
			testCase.check(recorder)
			if recorder.failed != testCase.fails {
				t.Fatalf("failed = %v, want %v", recorder.failed, testCase.fails)
			}
		})
	}
}
