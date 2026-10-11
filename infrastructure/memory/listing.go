package memory

import (
	"slices"
	"strings"

	"github.com/UrielJaloto/paygo/domain/page"
)

type listing[T any] struct {
	id     func(T) string
	cursor page.Cursor
	limit  page.Limit
}

func (l listing[T]) oldestFirst(items []T) []T {
	sorted := slices.SortedFunc(slices.Values(items), func(a, b T) int {
		return strings.Compare(l.id(a), l.id(b))
	})
	after := slices.DeleteFunc(sorted, func(item T) bool {
		return !l.cursor.IsStart() && l.id(item) <= l.cursor.AfterID()
	})
	return after[:min(len(after), int(l.limit))]
}

func (l listing[T]) newestFirst(items []T) []T {
	sorted := slices.SortedFunc(slices.Values(items), func(a, b T) int {
		return strings.Compare(l.id(b), l.id(a))
	})
	before := slices.DeleteFunc(sorted, func(item T) bool {
		return !l.cursor.IsStart() && l.id(item) >= l.cursor.AfterID()
	})
	return before[:min(len(before), int(l.limit))]
}
