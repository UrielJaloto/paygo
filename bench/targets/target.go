package targets

import "context"

type Target interface {
	Setup(ctx context.Context, accounts int) ([]string, error)
	Transfer(ctx context.Context, from, to string, amountMinor int64, key string) error
	Balances(ctx context.Context) (map[string]int64, error)
}
