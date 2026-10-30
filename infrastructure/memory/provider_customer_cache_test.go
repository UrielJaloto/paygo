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
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cache.Find(ctx, tc.provider, tc.document)
			if err != nil || got != tc.want {
				t.Errorf("find = %q, %v, want %q", got, err, tc.want)
			}
		})
	}
}
