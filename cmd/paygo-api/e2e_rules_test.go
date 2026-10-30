//go:build integration

package main_test

import (
	"net/http"
	"testing"
)

func openFundedPair(t *testing.T, running stack) (token string, accounts [2]string) {
	t.Helper()
	_, token = running.issueKey(t, ownerOne)
	accounts = [2]string{running.openAccount(t, token), running.openAccount(t, token)}
	running.deposit(t, accounts[0], "100.00")
	return token, accounts
}

func transferInput(accounts [2]string, amount string) map[string]string {
	return map[string]string{"from_account_id": accounts[0], "to_account_id": accounts[1], "amount": amount, "currency": "BRL"}
}

func TestE2E_IdempotentReplayAndConflict(t *testing.T) {
	running := startStack(t)
	token, accounts := openFundedPair(t, running)
	target := running.api + "/v1/transfers"
	first, created := doJSON(t, http.MethodPost, target, token, "same-key", transferInput(accounts, "30.00"))
	expectStatus(t, first, created, http.StatusCreated)
	second, replayed := doJSON(t, http.MethodPost, target, token, "same-key", transferInput(accounts, "30.00"))
	expectStatus(t, second, replayed, http.StatusOK)
	if second.Header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("Idempotent-Replayed = %q", second.Header.Get("Idempotent-Replayed"))
	}
	if text(t, created, "id") != text(t, replayed, "id") {
		t.Errorf("replay id = %v, want %v", replayed["id"], created["id"])
	}
	running.expectBalance(t, token, accounts[0], "70.00")
	third, conflict := doJSON(t, http.MethodPost, target, token, "same-key", transferInput(accounts, "31.00"))
	expectProblem(t, third, conflict, http.StatusConflict, "idempotency-conflict")
}

func TestE2E_InsufficientFundsAndFrozenAccount(t *testing.T) {
	running := startStack(t)
	token, accounts := openFundedPair(t, running)
	target := running.api + "/v1/transfers"
	response, body := doJSON(t, http.MethodPost, target, token, randomHex(t), transferInput(accounts, "100.01"))
	expectProblem(t, response, body, http.StatusUnprocessableEntity, "insufficient-funds")
	account := running.api + "/v1/accounts/" + accounts[0]
	response, body = doJSON(t, http.MethodPost, account+"/freeze", token, "", nil)
	expectStatus(t, response, body, http.StatusOK)
	response, body = doJSON(t, http.MethodPost, target, token, randomHex(t), transferInput(accounts, "1.00"))
	expectProblem(t, response, body, http.StatusConflict, "conflict")
	response, body = doJSON(t, http.MethodPost, account+"/unfreeze", token, "", nil)
	expectStatus(t, response, body, http.StatusOK)
	running.transfer(t, token, accounts, "1.00")
	running.expectBalance(t, token, accounts[0], "99.00")
}

func TestE2E_Reversal(t *testing.T) {
	running := startStack(t)
	token, accounts := openFundedPair(t, running)
	id := running.transfer(t, token, accounts, "30.00")
	target := running.api + "/v1/transfers/" + id + "/reversal"
	response, body := doJSON(t, http.MethodPost, target, token, randomHex(t), nil)
	expectStatus(t, response, body, http.StatusCreated)
	running.expectBalance(t, token, accounts[0], "100.00")
	running.expectBalance(t, token, accounts[1], "0.00")
	response, body = doJSON(t, http.MethodPost, target, token, randomHex(t), nil)
	expectProblem(t, response, body, http.StatusConflict, "conflict")
}

func TestE2E_OwnerIsolation(t *testing.T) {
	running := startStack(t)
	_, first := running.issueKey(t, ownerOne)
	keyID, second := running.issueKey(t, ownerTwo)
	account := running.api + "/v1/accounts/" + running.openAccount(t, first)
	response, body := doJSON(t, http.MethodGet, account, second, "", nil)
	expectProblem(t, response, body, http.StatusNotFound, "not-found")
	response, body = doJSON(t, http.MethodDelete, running.api+"/admin/v1/api-keys/"+keyID, adminToken, "", nil)
	expectStatus(t, response, body, http.StatusNoContent)
	response, body = doJSON(t, http.MethodGet, running.api+"/v1/accounts", second, "", nil)
	expectProblem(t, response, body, http.StatusUnauthorized, "unauthenticated")
}

func TestE2E_RateLimit(t *testing.T) {
	running := startStackWith(t, map[string]string{"PAYGO_RATE_LIMIT_RPS": "1", "PAYGO_RATE_LIMIT_BURST": "1"})
	_, token := running.issueKey(t, ownerOne)
	target := running.api + "/v1/accounts"
	response, body := doJSON(t, http.MethodGet, target, token, "", nil)
	expectStatus(t, response, body, http.StatusOK)
	response, body = doJSON(t, http.MethodGet, target, token, "", nil)
	expectProblem(t, response, body, http.StatusTooManyRequests, "rate-limited")
	if response.Header.Get("Retry-After") != "1" {
		t.Errorf("Retry-After = %q", response.Header.Get("Retry-After"))
	}
}
