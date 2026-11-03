//go:build integration

package e2e_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const syncDeadline = 5 * time.Second

func (r *receiver) awaitType(t *testing.T, kind string) {
	t.Helper()
	deadline := time.After(receiveTimeout)
	for r.next(t, deadline).header.Get("Paygo-Event-Type") != kind {
	}
}

func (s stack) createCharge(t *testing.T, token, account string) (string, string) {
	t.Helper()
	input := map[string]any{"account_id": account, "amount": "100.00", "currency": "BRL", "payer": map[string]string{"name": "Ana Lima", "document": validDocument}}
	response, body := doJSON(t, http.MethodPost, s.api+"/v1/charges", token, randomHex(t), input)
	expectStatus(t, response, body, http.StatusCreated)
	if text(t, body, "pix_qr_png_base64") == "" {
		t.Error("the charge has no pix qr code")
	}
	return text(t, body, "id"), strings.TrimPrefix(text(t, body, "pix_copy_paste"), fakePixPrefix)
}

func (s stack) createPayout(t *testing.T, token, account, amount string) (string, string) {
	t.Helper()
	input := map[string]string{"account_id": account, "amount": amount, "currency": "BRL", "pix_key": "ana@example.com", "pix_key_type": "email"}
	response, body := doJSON(t, http.MethodPost, s.api+"/v1/payouts", token, randomHex(t), input)
	expectStatus(t, response, body, http.StatusCreated)
	id := text(t, body, "id")
	return id, queryText(t, s.databaseURL, fmt.Sprintf("SELECT provider_id FROM payouts WHERE id = '%s'", id))
}

func (s stack) providerEvent(t *testing.T, eventID, kind, providerID string) {
	t.Helper()
	event := map[string]any{"event_id": eventID, "type": kind, "provider_id": providerID}
	response, body := postProviderEvent(t, s.api, fakeToken, event)
	expectStatus(t, response, body, http.StatusOK)
}

func (s stack) expectPaymentStatus(t *testing.T, token, path, want string) {
	t.Helper()
	response, body := doJSON(t, http.MethodGet, s.api+path, token, "", nil)
	expectStatus(t, response, body, http.StatusOK)
	if got := text(t, body, "status"); got != want {
		t.Errorf("%s status = %q, want %q", path, got, want)
	}
}

func (s stack) expectLedgerConsistent(t *testing.T) {
	t.Helper()
	response, body := doJSON(t, http.MethodGet, s.api+"/admin/v1/ledger/check", adminToken, "", nil)
	expectStatus(t, response, body, http.StatusOK)
}

func TestE2E_PixChargePaid(t *testing.T) {
	running := startStackWith(t, fastWorker)
	_, token := running.issueKey(t, ownerOne)
	account := running.openAccount(t, token)
	target := newReceiver(t)
	running.registerEndpoint(t, token, target.server.URL)
	id, providerID := running.createCharge(t, token, account)
	running.providerEvent(t, "paid-once", "charge.paid", providerID)
	running.expectBalance(t, token, account, "100.00")
	running.expectPaymentStatus(t, token, "/v1/charges/"+id, "paid")
	target.awaitType(t, "charge.paid")
	running.providerEvent(t, "paid-once", "charge.paid", providerID)
	running.expectBalance(t, token, account, "100.00")
	running.expectLedgerConsistent(t)
}

func TestE2E_PixPayoutCompletedAndFailed(t *testing.T) {
	running := startStack(t)
	_, token := running.issueKey(t, ownerOne)
	account := running.openAccount(t, token)
	running.deposit(t, account, "100.00")
	done, doneProvider := running.createPayout(t, token, account, "30.00")
	running.expectBalance(t, token, account, "70.00")
	running.providerEvent(t, "payout-done", "payout.done", doneProvider)
	running.expectPaymentStatus(t, token, "/v1/payouts/"+done, "completed")
	failed, failedProvider := running.createPayout(t, token, account, "20.00")
	running.expectBalance(t, token, account, "50.00")
	running.providerEvent(t, "payout-failed", "payout.failed", failedProvider)
	running.expectBalance(t, token, account, "70.00")
	running.expectPaymentStatus(t, token, "/v1/payouts/"+failed, "failed")
	running.expectLedgerConsistent(t)
}

func TestE2E_PaymentSyncRecovers(t *testing.T) {
	running := startStackWith(t, map[string]string{"PAYGO_PAYMENT_SYNC_INTERVAL": "1s"})
	_, token := running.issueKey(t, ownerOne)
	account := running.openAccount(t, token)
	id, providerID := running.createCharge(t, token, account)
	mark := map[string]any{"event_id": "marked", "type": "charge.paid", "provider_id": providerID, "sync_only": true}
	response, _ := postProviderEvent(t, running.worker, fakeToken, mark)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("marking status = %d", response.StatusCode)
	}
	execute(t, running.databaseURL, fmt.Sprintf("UPDATE charges SET created_at = now() - interval '2 minutes' WHERE id = '%s'", id))
	eventually(t, syncDeadline, "the charge to be paid", func() bool {
		_, body := doJSON(t, http.MethodGet, running.api+"/v1/charges/"+id, token, "", nil)
		return body["status"] == "paid"
	})
	running.expectBalance(t, token, account, "100.00")
	running.expectLedgerConsistent(t)
}

func TestE2E_ProviderWebhook_BadToken_Unauthorized(t *testing.T) {
	running := startStack(t)
	event := map[string]any{"event_id": "forged", "type": "charge.paid", "provider_id": "fake_unknown"}
	response, _ := postProviderEvent(t, running.api, "wrong-token", event)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
}
