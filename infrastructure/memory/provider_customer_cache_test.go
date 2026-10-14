package memory_test

import (
	"context"
	"testing"

	"github.com/UrielJaloto/paygo/infrastructure/memory"
)

func TestProviderCustomerCache_SaveThenFind_ReturnsCustomerByScope(t *testing.T) {
	cache := memory.NewProviderCustomerCache()
	ctx := context.Background()
	if err := cache.Save(ctx, "asaas", "doc", "cus_1"); err != nil {
		t.Fatalf("save: %v", err)
	}
	tests := []struct {
		name     string
		provider string
		document string
		want     string
	}{
		{name: "saved", provider: "asaas", document: "doc", want: "cus_1"},
		{name: "other document", provider: "asaas", document: "other", want: ""},
		{name: "other provider", provider: "fake", document: "doc", want: ""},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := cache.Find(ctx, testCase.provider, testCase.document)
			if err != nil || got != testCase.want {
				t.Errorf("find = %q, %v, want %q", got, err, testCase.want)
			}
		})
	}
}
