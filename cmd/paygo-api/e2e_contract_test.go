//go:build integration

package main_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
)

const (
	contractPath   = moduleRoot + "/api/openapi.json"
	fakePixPrefix  = "00020126fake"
	schemaRefStart = "#/components/schemas/"
	validDocument  = "52998224725"
)

type documentedResponse struct {
	Content map[string]struct {
		Schema struct {
			Ref string `json:"$ref"`
		} `json:"schema"`
	} `json:"content"`
}

type contractDocument struct {
	Paths map[string]map[string]struct {
		Responses map[string]documentedResponse `json:"responses"`
	} `json:"paths"`
	Components struct {
		Schemas map[string]struct {
			Required []string `json:"required"`
		} `json:"schemas"`
	} `json:"components"`
}

type contractClient struct {
	document contractDocument
	running  stack
	token    string
	visited  map[string]bool
}

func loadContract(t *testing.T) contractDocument {
	t.Helper()
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("reading the contract: %v", err)
	}
	var document contractDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decoding the contract: %v", err)
	}
	return document
}

func (c *contractClient) documented(t *testing.T, route string, status int) documentedResponse {
	t.Helper()
	c.visited[route] = true
	method, path, _ := strings.Cut(route, " ")
	response, found := c.document.Paths[path][strings.ToLower(method)].Responses[strconv.Itoa(status)]
	if !found {
		t.Errorf("%s answered %d, which is not documented", route, status)
	}
	return response
}

func (c *contractClient) expectFields(t *testing.T, route string, response documentedResponse, body map[string]any) {
	t.Helper()
	reference := response.Content["application/json"].Schema.Ref
	for _, field := range c.document.Components.Schemas[strings.TrimPrefix(reference, schemaRefStart)].Required {
		if _, found := body[field]; !found {
			t.Errorf("%s lacks required field %q: %v", route, field, body)
		}
	}
}

func (c *contractClient) call(t *testing.T, route, path, token string, input any) map[string]any {
	t.Helper()
	method, _, _ := strings.Cut(route, " ")
	response, body := doJSON(t, method, c.running.api+path, token, randomHex(t), input)
	c.expectFields(t, route, c.documented(t, route, response.StatusCode), body)
	return body
}

func (c *contractClient) public(t *testing.T, route, path string) {
	t.Helper()
	response, _ := getText(t, c.running.api+path)
	c.documented(t, route, response.StatusCode)
}

func (c *contractClient) walkAccounts(t *testing.T) (string, string) {
	t.Helper()
	input := map[string]string{"currency": "BRL"}
	source := text(t, c.call(t, "POST /v1/accounts", "/v1/accounts", c.token, input), "id")
	target := text(t, c.call(t, "POST /v1/accounts", "/v1/accounts", c.token, input), "id")
	base := "/v1/accounts/" + source
	c.call(t, "GET /v1/accounts", "/v1/accounts", c.token, nil)
	c.call(t, "GET /v1/accounts/{id}", base, c.token, nil)
	c.call(t, "POST /v1/accounts/{id}/freeze", base+"/freeze", c.token, nil)
	c.call(t, "POST /v1/accounts/{id}/unfreeze", base+"/unfreeze", c.token, nil)
	movement := map[string]string{"account_id": source, "amount": "100.00", "currency": "BRL"}
	c.call(t, "POST /admin/v1/deposits", "/admin/v1/deposits", adminToken, movement)
	movement["amount"] = "10.00"
	c.call(t, "POST /admin/v1/withdrawals", "/admin/v1/withdrawals", adminToken, movement)
	c.call(t, "GET /v1/accounts/{id}/balance", base+"/balance", c.token, nil)
	c.call(t, "GET /v1/accounts/{id}/statement", base+"/statement", c.token, nil)
	return source, target
}

func (c *contractClient) walkTransfers(t *testing.T, source, target string) {
	t.Helper()
	input := transferInput([2]string{source, target}, "30.00")
	id := text(t, c.call(t, "POST /v1/transfers", "/v1/transfers", c.token, input), "id")
	c.call(t, "GET /v1/transfers", "/v1/transfers", c.token, nil)
	c.call(t, "GET /v1/transfers/{id}", "/v1/transfers/"+id, c.token, nil)
	c.call(t, "POST /v1/transfers/{id}/reversal", "/v1/transfers/"+id+"/reversal", c.token, nil)
}

func (c *contractClient) walkWebhooks(t *testing.T, receiverURL string) string {
	t.Helper()
	input := map[string]string{"url": receiverURL}
	id := text(t, c.call(t, "POST /v1/webhook-endpoints", "/v1/webhook-endpoints", c.token, input), "id")
	c.call(t, "GET /v1/webhook-endpoints", "/v1/webhook-endpoints", c.token, nil)
	return id
}

func (c *contractClient) walkDeliveries(t *testing.T, source string) {
	t.Helper()
	c.running.deposit(t, source, "1.00")
	var items []any
	eventually(t, receiveTimeout, "a delivery to be listed", func() bool {
		items, _ = c.call(t, "GET /v1/webhook-deliveries", "/v1/webhook-deliveries", c.token, nil)["items"].([]any)
		return len(items) > 0
	})
	delivery, _ := items[0].(map[string]any)
	c.call(t, "POST /v1/webhook-deliveries/{id}/replay", "/v1/webhook-deliveries/"+text(t, delivery, "id")+"/replay", c.token, nil)
}

func (c *contractClient) walkPayments(t *testing.T, source string) {
	t.Helper()
	charge := map[string]any{"account_id": source, "amount": "5.00", "currency": "BRL", "payer": map[string]string{"name": "Ana Lima", "document": validDocument}}
	created := c.call(t, "POST /v1/charges", "/v1/charges", c.token, charge)
	c.call(t, "GET /v1/charges/{id}", "/v1/charges/"+text(t, created, "id"), c.token, nil)
	providerID := strings.TrimPrefix(text(t, created, "pix_copy_paste"), fakePixPrefix)
	c.providerEvent(t, map[string]any{"event_id": randomHex(t), "type": "charge.paid", "provider_id": providerID})
	payout := map[string]string{"account_id": source, "amount": "5.00", "currency": "BRL", "pix_key": "ana@example.com", "pix_key_type": "email"}
	id := text(t, c.call(t, "POST /v1/payouts", "/v1/payouts", c.token, payout), "id")
	c.call(t, "GET /v1/payouts/{id}", "/v1/payouts/"+id, c.token, nil)
}

func (c *contractClient) providerEvent(t *testing.T, event map[string]any) {
	t.Helper()
	const route = "POST /provider/v1/webhooks/{provider}"
	response, body := postProviderEvent(t, c.running, fakeToken, event)
	c.expectFields(t, route, c.documented(t, route, response.StatusCode), body)
}

func TestE2E_OpenAPIContract(t *testing.T) {
	running := startStackWith(t, fastWorker)
	client := &contractClient{document: loadContract(t), running: running, visited: map[string]bool{}}
	client.public(t, "GET /healthz", "/healthz")
	client.public(t, "GET /readyz", "/readyz")
	client.public(t, "GET /metrics", "/metrics")
	issued := client.call(t, "POST /admin/v1/owners/{owner_id}/api-keys", "/admin/v1/owners/"+ownerOne+"/api-keys", adminToken, nil)
	client.token = text(t, issued, "token")
	endpoint := client.walkWebhooks(t, newReceiver(t).server.URL)
	source, target := client.walkAccounts(t)
	client.walkTransfers(t, source, target)
	client.walkDeliveries(t, source)
	client.walkPayments(t, source)
	client.call(t, "GET /admin/v1/ledger/check", "/admin/v1/ledger/check", adminToken, nil)
	client.call(t, "DELETE /v1/webhook-endpoints/{id}", "/v1/webhook-endpoints/"+endpoint, client.token, nil)
	client.call(t, "DELETE /admin/v1/api-keys/{id}", "/admin/v1/api-keys/"+text(t, issued, "id"), adminToken, nil)
	expectEveryOperationVisited(t, client)
}

func expectEveryOperationVisited(t *testing.T, client *contractClient) {
	t.Helper()
	var routes []string
	for path, operations := range client.document.Paths {
		for method := range operations {
			routes = append(routes, strings.ToUpper(method)+" "+path)
		}
	}
	for _, route := range routes {
		if !client.visited[route] {
			t.Errorf("%s was not exercised", route)
		}
	}
}

func postProviderEvent(t *testing.T, running stack, token string, event map[string]any) (*http.Response, map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("encoding event: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, running.api+"/provider/v1/webhooks/fake", strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Paygo-Fake-Token", token)
	response, raw := send(t, request)
	return response, decodeObject(t, raw)
}
