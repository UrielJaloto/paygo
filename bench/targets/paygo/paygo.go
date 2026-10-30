package paygo

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const (
	currency = "BRL"
	owner    = "0190a000-0000-7000-8000-000000000001"
	centsLen = 2
)

type Config struct {
	BaseURL     string
	AdminToken  string
	FundedMinor int64
	Connections int
}

type Target struct {
	config   Config
	client   *http.Client
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

func New(config Config) *Target {
	transport := &http.Transport{MaxIdleConns: config.Connections, MaxIdleConnsPerHost: config.Connections}
	return &Target{config: config, client: &http.Client{Transport: transport}}
}

func (t *Target) Setup(ctx context.Context, accounts int) ([]string, error) {
	var issued struct {
		Token string `json:"token"`
	}
	path := "/admin/v1/owners/" + owner + "/api-keys"
	if err := t.post(ctx, request{path: path, token: t.config.AdminToken}, &issued); err != nil {
		return nil, fmt.Errorf("issuing api key: %w", err)
	}
	t.token = issued.Token
	opened := make([]string, 0, accounts)
	for range accounts {
		account, err := t.openFunded(ctx)
		if err != nil {
			return nil, err
		}
		opened = append(opened, account)
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.accounts = opened
	return opened, nil
}

func (t *Target) openFunded(ctx context.Context) (string, error) {
	var account struct {
		ID string `json:"id"`
	}
	opening := request{path: "/v1/accounts", token: t.token, body: map[string]string{"currency": currency}}
	if err := t.post(ctx, opening, &account); err != nil {
		return "", fmt.Errorf("opening account: %w", err)
	}
	deposit := map[string]string{"account_id": account.ID, "amount": formatMinor(t.config.FundedMinor), "currency": currency}
	funding := request{path: "/admin/v1/deposits", token: t.config.AdminToken, key: newKey(), body: deposit}
	if err := t.post(ctx, funding, nil); err != nil {
		return "", fmt.Errorf("funding account: %w", err)
	}
	return account.ID, nil
}

func (t *Target) Transfer(ctx context.Context, from, to string, amountMinor int64, key string) error {
	body := map[string]string{"from_account_id": from, "to_account_id": to, "amount": formatMinor(amountMinor), "currency": currency}
	return t.post(ctx, request{path: "/v1/transfers", token: t.token, key: key, body: body}, nil)
}

func (t *Target) Balances(ctx context.Context) (map[string]int64, error) {
	t.mutex.Lock()
	accounts := t.accounts
	t.mutex.Unlock()
	balances := make(map[string]int64, len(accounts))
	for _, account := range accounts {
		balance, err := t.balance(ctx, account)
		if err != nil {
			return nil, err
		}
		balances[account] = balance
	}
	return balances, nil
}

func (t *Target) balance(ctx context.Context, account string) (int64, error) {
	var balance struct {
		Amount string `json:"amount"`
	}
	built, err := http.NewRequestWithContext(ctx, http.MethodGet, t.config.BaseURL+"/v1/accounts/"+account+"/balance", nil)
	if err != nil {
		return 0, fmt.Errorf("building balance request: %w", err)
	}
	built.Header.Set("Authorization", "Bearer "+t.token)
	if err := t.exchange(built, &balance); err != nil {
		return 0, fmt.Errorf("reading balance of %s: %w", account, err)
	}
	return parseMinor(balance.Amount)
}

func (t *Target) post(ctx context.Context, call request, output any) error {
	encoded, err := json.Marshal(call.body)
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}
	built, err := http.NewRequestWithContext(ctx, http.MethodPost, t.config.BaseURL+call.path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	built.Header.Set("Authorization", "Bearer "+call.token)
	built.Header.Set("Content-Type", "application/json")
	if call.key != "" {
		built.Header.Set("Idempotency-Key", call.key)
	}
	return t.exchange(built, output)
}

func (t *Target) exchange(built *http.Request, output any) error {
	response, err := t.client.Do(built)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("status %d: %s", response.StatusCode, payload)
	}
	if output == nil {
		return nil
	}
	return json.Unmarshal(payload, output)
}

func formatMinor(minor int64) string {
	return fmt.Sprintf("%d.%02d", minor/100, minor%100)
}

func parseMinor(amount string) (int64, error) {
	units, cents, found := strings.Cut(amount, ".")
	if !found || len(cents) != centsLen {
		return 0, fmt.Errorf("invalid amount %q", amount)
	}
	minor, err := strconv.ParseInt(units+cents, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q: %w", amount, err)
	}
	return minor, nil
}

func newKey() string {
	return rand.Text()
}
