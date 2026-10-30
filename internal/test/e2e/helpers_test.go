//go:build integration

package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/infrastructure/postgres"
)

const (
	defaultAdminURL = "postgres://paygo:paygo@localhost:5432/postgres?sslmode=disable"
	adminToken      = "e2e-admin-token-0123456789abcdef0123"
	fakeToken       = "e2e-fake-token-0123456789abcdef01234"
	ownerOne        = "01890000-0000-7000-8000-000000000001"
	ownerTwo        = "01890000-0000-7000-8000-000000000002"
	moduleRoot      = "../../.."
	migrationsDirectory   = moduleRoot + "/env/migrations"
	pollInterval    = 50 * time.Millisecond
	bootTimeout     = 15 * time.Second
	requestTimeout  = 10 * time.Second
	databaseTimeout = 30 * time.Second
	stopTimeout     = 20 * time.Second
)

type binaries struct {
	api     string
	worker  string
	migrate string
}

type stack struct {
	api         string
	worker      string
	databaseURL string
	coverageDirectory    string
	process     *process
}

type process struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

func buildBinaries(directory string) (binaries, error) {
	built := binaries{
		api:     binaryPath(directory, "paygo-api"),
		worker:  binaryPath(directory, "paygo-worker"),
		migrate: binaryPath(directory, "paygo-migrate"),
	}
	for _, output := range []string{built.api, built.worker, built.migrate} {
		name := strings.TrimSuffix(filepath.Base(output), ".exe")
		command := exec.Command("go", "build", "-cover", "-covermode=atomic", "-coverpkg=./...", "-o", output, "./internal/cmd/"+name)
		command.Dir = moduleRoot
		if combined, err := command.CombinedOutput(); err != nil {
			return binaries{}, fmt.Errorf("building %s: %w: %s", name, err, combined)
		}
	}
	return built, nil
}

func binaryPath(directory, name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(directory, name)
}

func adminURL() string {
	if configured := os.Getenv("PAYGO_TEST_DATABASE_URL"); configured != "" {
		return configured
	}
	return defaultAdminURL
}

func newDatabaseURL(t *testing.T) string {
	t.Helper()
	name := "paygo_test_" + randomHex(t)
	execute(t, adminURL(), "CREATE DATABASE "+name)
	t.Cleanup(func() { execute(t, adminURL(), "DROP DATABASE "+name+" WITH (FORCE)") })
	parsed, err := url.Parse(adminURL())
	if err != nil {
		t.Fatalf("parsing the admin url: %v", err)
	}
	parsed.Path = "/" + name
	return parsed.String()
}

func execute(t *testing.T, databaseURL, statement string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	pool, err := postgres.NewPool(ctx, databaseURL, 2)
	if err != nil {
		t.Fatalf("connecting to the database: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatalf("executing %q: %v", statement, err)
	}
}

func queryText(t *testing.T, databaseURL, statement string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	pool, err := postgres.NewPool(ctx, databaseURL, 2)
	if err != nil {
		t.Fatalf("connecting to the database: %v", err)
	}
	defer pool.Close()
	var value string
	if err := pool.QueryRow(ctx, statement).Scan(&value); err != nil {
		t.Fatalf("querying %q: %v", statement, err)
	}
	return value
}

func randomHex(t *testing.T) string {
	t.Helper()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("reading random bytes: %v", err)
	}
	return hex.EncodeToString(random)
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

func coverageDirectory(t *testing.T) string {
	t.Helper()
	if configured := os.Getenv("PAYGO_E2E_COVERDIR"); configured != "" {
		return configured
	}
	return t.TempDir()
}

func environment(overrides map[string]string) []string {
	merged := os.Environ()
	for name, value := range overrides {
		merged = append(merged, name+"="+value)
	}
	return merged
}

func startStack(t *testing.T) stack {
	t.Helper()
	return startStackWith(t, nil)
}

func startStackWith(t *testing.T, overrides map[string]string) stack {
	t.Helper()
	running := stack{databaseURL: newDatabaseURL(t), coverageDirectory: coverageDirectory(t)}
	shared := map[string]string{
		"GOCOVERDIR":               running.coverageDirectory,
		"PAYGO_DATABASE_URL":       running.databaseURL,
		"PAYGO_ADMIN_TOKEN":        adminToken,
		"PAYGO_FAKE_WEBHOOK_TOKEN": fakeToken,
		"PAYGO_MIGRATIONS_DIR":     migrationsDirectory,
	}
	for name, value := range overrides {
		shared[name] = value
	}
	migrate := exec.Command(built.migrate)
	migrate.Env = environment(shared)
	if combined, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("migrating: %v: %s", err, combined)
	}
	apiAddress, workerAddress := freeAddress(t), freeAddress(t)
	running.api, running.worker = "http://"+apiAddress, "http://"+workerAddress
	running.process = startProcess(t, built.api, withAddress(shared, apiAddress))
	startProcess(t, built.worker, withAddress(shared, workerAddress))
	waitForStatus(t, running.api+"/readyz", http.StatusOK)
	waitForStatus(t, running.worker+"/healthz", http.StatusOK)
	return running
}

func withAddress(shared map[string]string, address string) map[string]string {
	env := map[string]string{"PAYGO_HTTP_ADDR": address}
	for name, value := range shared {
		env[name] = value
	}
	return env
}

func startProcess(t *testing.T, path string, env map[string]string) *process {
	t.Helper()
	var output bytes.Buffer
	command := exec.Command(path)
	command.Env = environment(env)
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatalf("starting %s: %v", path, err)
	}
	started := &process{command: command, done: make(chan struct{})}
	go func() {
		started.err = command.Wait()
		close(started.done)
	}()
	t.Cleanup(func() {
		started.stop(t)
		if t.Failed() {
			t.Logf("%s output:\n%s", filepath.Base(path), output.String())
		}
	})
	return started
}

func (p *process) stop(t *testing.T) {
	t.Helper()
	if err := p.command.Process.Signal(os.Interrupt); err != nil {
		_ = p.command.Process.Kill()
	}
	select {
	case <-p.done:
	case <-time.After(stopTimeout):
		_ = p.command.Process.Kill()
		t.Errorf("%s did not stop within %s", p.command.Path, stopTimeout)
	}
}

func eventually(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	deadline := time.After(timeout)
	for !condition() {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("timed out after %s waiting for %s", timeout, description)
		}
	}
}

func waitForStatus(t *testing.T, target string, want int) {
	t.Helper()
	eventually(t, bootTimeout, target+" to answer", func() bool {
		response, err := http.Get(target)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == want
	})
}

func send(t *testing.T, request *http.Request) (*http.Response, []byte) {
	t.Helper()
	client := http.Client{Timeout: requestTimeout}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", request.Method, request.URL, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", request.URL, err)
	}
	return response, raw
}

func readText(t *testing.T, target string) (*http.Response, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	response, raw := send(t, request)
	return response, string(raw)
}

func sendJSON(t *testing.T, method, target, token, idempotencyKey string, body any) (*http.Response, map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding body: %v", err)
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, target, reader)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response, raw := send(t, request)
	return response, decodeObject(t, raw)
}

func decodeObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	if len(raw) == 0 {
		return nil
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding %q: %v", raw, err)
	}
	return decoded
}

func expectStatus(t *testing.T, response *http.Response, body map[string]any, want int) {
	t.Helper()
	if response.StatusCode != want {
		t.Fatalf("%s %s status = %d, want %d, body %v", response.Request.Method, response.Request.URL, response.StatusCode, want, body)
	}
}

func expectProblem(t *testing.T, response *http.Response, body map[string]any, status int, problem string) {
	t.Helper()
	expectStatus(t, response, body, status)
	kind, _ := body["type"].(string)
	if !strings.HasSuffix(kind, problem) {
		t.Fatalf("problem type = %q, want suffix %q", kind, problem)
	}
}

func text(t *testing.T, body map[string]any, field string) string {
	t.Helper()
	value, found := body[field].(string)
	if !found {
		t.Fatalf("field %q missing from %v", field, body)
	}
	return value
}

func (s stack) issueKey(t *testing.T, owner string) (id, token string) {
	t.Helper()
	response, body := sendJSON(t, http.MethodPost, s.api+"/admin/v1/owners/"+owner+"/api-keys", adminToken, "", nil)
	expectStatus(t, response, body, http.StatusCreated)
	return text(t, body, "id"), text(t, body, "token")
}

func (s stack) openAccount(t *testing.T, token string) string {
	t.Helper()
	response, body := sendJSON(t, http.MethodPost, s.api+"/v1/accounts", token, "", map[string]string{"currency": "BRL"})
	expectStatus(t, response, body, http.StatusCreated)
	return text(t, body, "id")
}

func (s stack) deposit(t *testing.T, account, amount string) string {
	t.Helper()
	input := map[string]string{"account_id": account, "amount": amount, "currency": "BRL"}
	response, body := sendJSON(t, http.MethodPost, s.api+"/admin/v1/deposits", adminToken, randomHex(t), input)
	expectStatus(t, response, body, http.StatusCreated)
	return text(t, body, "id")
}

func (s stack) transfer(t *testing.T, token string, accounts [2]string, amount string) string {
	t.Helper()
	input := map[string]string{"from_account_id": accounts[0], "to_account_id": accounts[1], "amount": amount, "currency": "BRL"}
	response, body := sendJSON(t, http.MethodPost, s.api+"/v1/transfers", token, randomHex(t), input)
	expectStatus(t, response, body, http.StatusCreated)
	return text(t, body, "id")
}

func (s stack) expectBalance(t *testing.T, token, account, want string) {
	t.Helper()
	response, body := sendJSON(t, http.MethodGet, s.api+"/v1/accounts/"+account+"/balance", token, "", nil)
	expectStatus(t, response, body, http.StatusOK)
	if got := text(t, body, "amount"); got != want {
		t.Fatalf("balance of %s = %s, want %s", account, got, want)
	}
}
