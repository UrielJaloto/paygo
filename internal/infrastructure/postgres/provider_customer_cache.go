package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	selectProviderCustomer = `SELECT provider_customer_id FROM provider_customers WHERE provider = $1 AND document = $2`
	insertProviderCustomer = `INSERT INTO provider_customers (provider, document, provider_customer_id) VALUES ($1, $2, $3)
		ON CONFLICT (provider, document) DO UPDATE SET provider_customer_id = EXCLUDED.provider_customer_id`
)

type ProviderCustomerCache struct {
	database querier
}

func NewProviderCustomerCache(pool *pgxpool.Pool) *ProviderCustomerCache {
	return &ProviderCustomerCache{database: pool}
}

func (c *ProviderCustomerCache) Find(ctx context.Context, provider, document string) (string, error) {
	var customerID string
	err := c.database.QueryRow(ctx, selectProviderCustomer, provider, document).Scan(&customerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return customerID, err
}

func (c *ProviderCustomerCache) Save(ctx context.Context, provider, document, customerID string) error {
	_, err := c.database.Exec(ctx, insertProviderCustomer, provider, document, customerID)
	return err
}
