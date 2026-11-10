package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const (
	currency     = "BRL"
	owner        = "0190a000-0000-7000-8000-000000000001"
	centsLength  = 2
	minorPerUnit = 100
)

var (
	errUnexpectedStatus = errors.NewMoney("unexpected status")
	errInvalidAmount    = errors.NewMoney("invalid amount")
)

type clientConfig struct {
	baseURL     string
	adminToken  string
	fundedMinor int64
	connections int
}

type apiClient struct {
	config   clientConfig
	http     *http.Client
	token    string
	mutex    sync.Mutex
	accounts []string
}

type request struct {
	path  string
	token string
	key   string
	body  any
}

func newAPIClient(config clientConfig) *apiClient {
	transport := &http.Transport{MaxIdleConns: config.connections, MaxIdleConnsPerHost: config.connections}
	return &apiClient{config: config, http: &http.Client{Transport: transport}}
}

func (c *apiClient) CreateAccounts(ctx context.Context, accounts int) ([]string, error) {
	var issued struct {
		Token string `json:"token"`
	}
	path := "/admin/v1/owners/" + owner + "/api-keys"
	if err := c.postJSON(ctx, request{path: path, token: c.config.adminToken}, &issued); err != nil {
		return nil, fmt.Errorf("issuing api key: %w", err)
	}
	c.token = issued.Token
	opened := make([]string, 0, accounts)
	for range accounts {
		account, err := c.openFundedAccount(ctx)
		if err != nil {
			return nil, err
		}
		opened = append(opened, account)
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.accounts = opened
	return opened, nil
}

func (c *apiClient) openFundedAccount(ctx context.Context) (string, error) {
	var account struct {
		ID string `json:"id"`
	}
	opening := request{path: "/v1/accounts", token: c.token, body: map[string]string{"currency": currency}}
	if err := c.postJSON(ctx, opening, &account); err != nil {
		return "", fmt.Errorf("opening account: %w", err)
	}
	deposit := map[string]string{"account_id": account.ID, "amount": formatMinor(c.config.fundedMinor), "currency": currency}
	funding := request{path: "/admin/v1/deposits", token: c.config.adminToken, key: generateIdempotencyKey(), body: deposit}
	if err := c.postJSON(ctx, funding, nil); err != nil {
		return "", fmt.Errorf("funding account: %w", err)
	}
	return account.ID, nil
}

func (c *apiClient) Transfer(ctx context.Context, move movement) error {
	body := map[string]string{"from_account_id": move.from, "to_account_id": move.to, "amount": formatMinor(move.amountMinor), "currency": currency}
	return c.postJSON(ctx, request{path: "/v1/transfers", token: c.token, key: move.key, body: body}, nil)
}

func (c *apiClient) FetchBalances(ctx context.Context) (map[string]int64, error) {
	c.mutex.Lock()
	accounts := c.accounts
	c.mutex.Unlock()
	balances := make(map[string]int64, len(accounts))
	for _, account := range accounts {
		balance, err := c.fetchBalance(ctx, account)
		if err != nil {
			return nil, err
		}
		balances[account] = balance
	}
	return balances, nil
}

func (c *apiClient) fetchBalance(ctx context.Context, account string) (int64, error) {
	var balance struct {
		Amount string `json:"amount"`
	}
	built, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.baseURL+"/v1/accounts/"+account+"/balance", nil)
	if err != nil {
		return 0, fmt.Errorf("building balance request: %w", err)
	}
	built.Header.Set("Authorization", "Bearer "+c.token)
	if err := c.sendRequest(built, &balance); err != nil {
		return 0, fmt.Errorf("reading balance of %s: %w", account, err)
	}
	return parseMinor(balance.Amount)
}

func (c *apiClient) postJSON(ctx context.Context, call request, output any) error {
	encoded, err := json.Marshal(call.body)
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}
	built, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.baseURL+call.path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	built.Header.Set("Authorization", "Bearer "+call.token)
	built.Header.Set("Content-Type", "application/json")
	if call.key != "" {
		built.Header.Set("Idempotency-Key", call.key)
	}
	return c.sendRequest(built, output)
}

func (c *apiClient) sendRequest(built *http.Request, output any) error {
	response, err := c.http.Do(built)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("%w %d: %s", errUnexpectedStatus, response.StatusCode, payload)
	}
	if output == nil {
		return nil
	}
	return json.Unmarshal(payload, output)
}

func formatMinor(minor int64) string {
	return fmt.Sprintf("%d.%02d", minor/minorPerUnit, minor%minorPerUnit)
}

func parseMinor(amount string) (int64, error) {
	units, cents, found := strings.Cut(amount, ".")
	if !found || len(cents) != centsLength {
		return 0, fmt.Errorf("%w %q", errInvalidAmount, amount)
	}
	minor, err := strconv.ParseInt(units+cents, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w %q: %w", errInvalidAmount, amount, err)
	}
	return minor, nil
}

func generateIdempotencyKey() string {
	return rand.Text()
}
