package memory

import (
	"context"
	"sync"
)

type customerScope struct {
	provider string
	document string
}

type ProviderCustomerCache struct {
	mutex     sync.RWMutex
	customers map[customerScope]string
}

func NewProviderCustomerCache() *ProviderCustomerCache {
	return &ProviderCustomerCache{customers: map[customerScope]string{}}
}

func (c *ProviderCustomerCache) Find(_ context.Context, provider, document string) (string, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.customers[customerScope{provider: provider, document: document}], nil
}

func (c *ProviderCustomerCache) Save(_ context.Context, provider, document, customerID string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.customers[customerScope{provider: provider, document: document}] = customerID
	return nil
}
