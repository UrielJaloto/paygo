//go:build integration

package e2e_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	receiveTimeout = 10 * time.Second
	receiverBuffer = 32
)

var fastWorker = map[string]string{"PAYGO_WORKER_POLL_INTERVAL": "100ms", "PAYGO_WEBHOOK_MAX_ATTEMPTS": "2"}

type receipt struct {
	header http.Header
	body   []byte
	status int
}

type receiver struct {
	server   *httptest.Server
	statuses chan int
	receipts chan receipt
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	target := &receiver{statuses: make(chan int, receiverBuffer), receipts: make(chan receipt, receiverBuffer)}
	target.server = httptest.NewServer(http.HandlerFunc(target.handle))
	t.Cleanup(target.server.Close)
	return target
}

func (r *receiver) handle(w http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	status := http.StatusOK
	select {
	case status = <-r.statuses:
	default:
	}
	w.WriteHeader(status)
	r.receipts <- receipt{header: request.Header.Clone(), body: body, status: status}
}

func (r *receiver) await(t *testing.T, transferID string) receipt {
	t.Helper()
	deadline := time.After(receiveTimeout)
	for {
		if got := r.next(t, deadline); eventTransferID(t, got.body) == transferID {
			return got
		}
	}
}

func (r *receiver) next(t *testing.T, deadline <-chan time.Time) receipt {
	t.Helper()
	select {
	case got := <-r.receipts:
		return got
	case <-deadline:
		t.Fatalf("no expected webhook within %s", receiveTimeout)
		return receipt{}
	}
}

func eventTransferID(t *testing.T, body []byte) string {
	t.Helper()
	var decoded struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding webhook %q: %v", body, err)
	}
	return decoded.Data.ID
}

func (s stack) registerEndpoint(t *testing.T, token, target string) string {
	t.Helper()
	response, body := doJSON(t, http.MethodPost, s.api+"/v1/webhook-endpoints", token, "", map[string]string{"url": target})
	expectStatus(t, response, body, http.StatusCreated)
	return text(t, body, "secret")
}

func expectSigned(t *testing.T, got receipt, secret string) {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(got.header.Get("Paygo-Timestamp") + "."))
	mac.Write(got.body)
	want := "v1=" + hex.EncodeToString(mac.Sum(nil))
	if signature := got.header.Get("Paygo-Signature"); signature != want {
		t.Errorf("Paygo-Signature = %q, want %q", signature, want)
	}
}

func (s stack) deliveries(t *testing.T, token, status string) []any {
	t.Helper()
	response, body := doJSON(t, http.MethodGet, s.api+"/v1/webhook-deliveries?status="+status, token, "", nil)
	expectStatus(t, response, body, http.StatusOK)
	items, _ := body["items"].([]any)
	return items
}

func TestE2E_WebhookSignedDeliveryToBothOwners(t *testing.T) {
	running := startStackWith(t, fastWorker)
	_, first := running.issueKey(t, ownerOne)
	_, second := running.issueKey(t, ownerTwo)
	accounts := [2]string{running.openAccount(t, first), running.openAccount(t, second)}
	running.deposit(t, accounts[0], "100.00")
	sender, payee := newReceiver(t), newReceiver(t)
	senderSecret := running.registerEndpoint(t, first, sender.server.URL)
	payeeSecret := running.registerEndpoint(t, second, payee.server.URL)
	id := running.transfer(t, first, accounts, "30.00")
	for target, secret := range map[*receiver]string{sender: senderSecret, payee: payeeSecret} {
		got := target.await(t, id)
		expectSigned(t, got, secret)
		if kind := got.header.Get("Paygo-Event-Type"); kind != "transfer.completed" {
			t.Errorf("Paygo-Event-Type = %q", kind)
		}
	}
}

func prepareFailingTransfer(t *testing.T, running stack, statuses ...int) (string, *receiver, func() string) {
	t.Helper()
	_, token := running.issueKey(t, ownerOne)
	accounts := [2]string{running.openAccount(t, token), running.openAccount(t, token)}
	target := newReceiver(t)
	running.registerEndpoint(t, token, target.server.URL)
	target.await(t, running.deposit(t, accounts[0], "100.00"))
	return token, target, func() string {
		for _, status := range statuses {
			target.statuses <- status
		}
		return running.transfer(t, token, accounts, "30.00")
	}
}

func TestE2E_WebhookRetry_DeliveredOnSecondAttempt(t *testing.T) {
	running := startStackWith(t, fastWorker)
	_, target, send := prepareFailingTransfer(t, running, http.StatusInternalServerError, http.StatusOK)
	id := send()
	if got := target.await(t, id); got.status != http.StatusInternalServerError {
		t.Fatalf("first attempt status = %d", got.status)
	}
	if got := target.await(t, id); got.status != http.StatusOK {
		t.Fatalf("second attempt status = %d", got.status)
	}
}

func TestE2E_DeadDeliveryReplay(t *testing.T) {
	running := startStackWith(t, fastWorker)
	token, target, send := prepareFailingTransfer(t, running, http.StatusInternalServerError, http.StatusInternalServerError)
	id := send()
	target.await(t, id)
	target.await(t, id)
	var dead []any
	eventually(t, receiveTimeout, "the delivery to be dead", func() bool {
		dead = running.deliveries(t, token, "dead")
		return len(dead) == 1
	})
	delivery, _ := dead[0].(map[string]any)
	replay := running.api + "/v1/webhook-deliveries/" + text(t, delivery, "id") + "/replay"
	response, body := doJSON(t, http.MethodPost, replay, token, "", nil)
	expectStatus(t, response, body, http.StatusOK)
	if got := target.await(t, id); got.status != http.StatusOK {
		t.Fatalf("replayed status = %d", got.status)
	}
}
