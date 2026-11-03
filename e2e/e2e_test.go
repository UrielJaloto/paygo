//go:build integration

package e2e_test

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

var (
	built        binaries
	tokenPattern = regexp.MustCompile(`^pgk_[0-9a-f]{8}_[A-Za-z0-9_-]{43}$`)
)

func TestMain(m *testing.M) {
	os.Exit(runSuite(m))
}

func runSuite(m *testing.M) int {
	directory, err := os.MkdirTemp("", "paygo-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(directory)
	if built, err = buildBinaries(directory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

func TestE2E_HealthAndReadiness(t *testing.T) {
	running := startStack(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		response, body := getText(t, running.api+path)
		if response.StatusCode != http.StatusOK {
			t.Errorf("%s status = %d, body %s", path, response.StatusCode, body)
		}
		if response.Header.Get("X-Request-Id") == "" {
			t.Errorf("%s has no X-Request-Id", path)
		}
	}
	response, body := getText(t, running.api+"/metrics")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "paygo_http_requests_total") {
		t.Errorf("/metrics status = %d, body lacks the request counter:\n%s", response.StatusCode, body)
	}
}

func TestE2E_AdminIssuesKey_OwnerOpensAccount(t *testing.T) {
	running := startStack(t)
	_, token := running.issueKey(t, ownerOne)
	if !tokenPattern.MatchString(token) {
		t.Fatalf("token = %q does not match the documented format", token)
	}
	response, body := doJSON(t, http.MethodPost, running.api+"/v1/accounts", token, "", map[string]string{"currency": "BRL"})
	expectStatus(t, response, body, http.StatusCreated)
	if text(t, body, "status") != "active" || text(t, body, "owner_id") != ownerOne {
		t.Fatalf("account = %v", body)
	}
}

func TestE2E_DepositTransferWithdraw_BalancesAndStatement(t *testing.T) {
	running := startStack(t)
	_, first := running.issueKey(t, ownerOne)
	_, second := running.issueKey(t, ownerTwo)
	source, target := running.openAccount(t, first), running.openAccount(t, second)
	running.deposit(t, source, "100.00")
	running.transfer(t, first, [2]string{source, target}, "30.00")
	withdrawal := map[string]string{"account_id": target, "amount": "10.00", "currency": "BRL"}
	response, body := doJSON(t, http.MethodPost, running.api+"/admin/v1/withdrawals", adminToken, randomHex(t), withdrawal)
	expectStatus(t, response, body, http.StatusCreated)
	running.expectBalance(t, first, source, "70.00")
	running.expectBalance(t, second, target, "20.00")
	expectStatement(t, running, first, source, []string{"debit 30.00", "credit 100.00"})
}

func expectStatement(t *testing.T, running stack, token, account string, want []string) {
	t.Helper()
	response, body := doJSON(t, http.MethodGet, running.api+"/v1/accounts/"+account+"/statement", token, "", nil)
	expectStatus(t, response, body, http.StatusOK)
	items, _ := body["items"].([]any)
	got := make([]string, 0, len(items))
	for _, item := range items {
		line, _ := item.(map[string]any)
		got = append(got, fmt.Sprintf("%v %v", line["direction"], line["amount"]))
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("statement = %v, want %v", got, want)
	}
}
