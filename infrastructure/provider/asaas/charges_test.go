package asaas_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/infrastructure/provider/asaas"
	paymentservice "github.com/UrielJaloto/paygo/services/payment"
)

const (
	apiKey     = "secret-api-key"
	document   = "24971563792"
	customerID = "cus_000005401844"
	paymentID  = "pay_080225913252"
)

var errCache = errors.New("cache down")

type reply struct {
	status int
	file   string
}

type recorded struct {
	method string
	path   string
	header http.Header
	body   []byte
}

type stubServer struct {
	replies  []reply
	requests []recorded
}

type stubCache struct {
	stored  map[string]string
	findErr error
	saveErr error
}

type failingTransport struct {
	err error
}

type brokenBodyTransport struct{}

type brokenBody struct{}

func (s *stubCache) Find(_ context.Context, provider, document string) (string, error) {
	return s.stored[provider+document], s.findErr
}

func (s *stubCache) Save(_ context.Context, provider, document, customer string) error {
	s.stored[provider+document] = customer
	return s.saveErr
}

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, f.err
}

func (brokenBodyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: brokenBody{}, Header: http.Header{}}, nil
}

func (brokenBody) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func (brokenBody) Close() error {
	return nil
}

func (s *stubServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.requests = append(s.requests, recorded{r.Method, r.URL.Path, r.Header.Clone(), body})
	next := s.replies[0]
	s.replies = s.replies[1:]
	w.WriteHeader(next.status)
	_, _ = w.Write(readFixture(next.file))
}

func readFixture(name string) []byte {
	if name == "" {
		return nil
	}
	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		panic(err)
	}
	return content
}

func newStub(t *testing.T, cache *stubCache, replies ...reply) (*asaas.Provider, *stubServer) {
	t.Helper()
	stub := &stubServer{replies: replies}
	server := httptest.NewServer(stub)
	t.Cleanup(server.Close)
	return asaas.NewMoney(server.URL, apiKey, cache, server.Client()), stub
}

func emptyCache() *stubCache {
	return &stubCache{stored: map[string]string{}}
}

func cachedCustomer() *stubCache {
	return &stubCache{stored: map[string]string{"asaas" + document: customerID}}
}

func chargeRequest() paymentservice.ChargeRequest {
	return paymentservice.ChargeRequest{
		ExternalReference: "charge-1",
		PayerName:         "John Doe",
		PayerDocument:     document,
		AmountMinor:       500,
		DueDate:           time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
	}
}

func assertJSONFixture(t *testing.T, got []byte, name string) {
	t.Helper()
	if !reflect.DeepEqual(decodeJSON(t, got), decodeJSON(t, readFixture(name))) {
		t.Fatalf("body = %s, want %s", got, readFixture(name))
	}
}

func decodeJSON(t *testing.T, raw []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return value
}

func TestCreatePixCharge_NewCustomer_SendsThreeRequestsInOrder(t *testing.T) {
	cache := emptyCache()
	provider, stub := newStub(t, cache,
		reply{http.StatusOK, "customer_response.json"},
		reply{http.StatusOK, "payment_response.json"},
		reply{http.StatusOK, "pix_qr_code_response.json"},
	)
	got, err := provider.CreatePixCharge(context.Background(), chargeRequest())
	if err != nil {
		t.Fatalf("CreatePixCharge: %v", err)
	}
	want := paymentservice.ChargeResult{ProviderID: paymentID, PixCopyPaste: "00020126copy", PixQRBase64: "iVBORw0KGgo="}
	if got != want {
		t.Fatalf("result = %+v, want %+v", got, want)
	}
	paths := []string{"/customers", "/payments", "/payments/" + paymentID + "/pixQrCode"}
	for i, path := range paths {
		if stub.requests[i].path != path {
			t.Fatalf("request %d path = %q, want %q", i, stub.requests[i].path, path)
		}
		assertHeaders(t, stub.requests[i].header)
	}
	assertJSONFixture(t, stub.requests[0].body, "customer_request.json")
	assertJSONFixture(t, stub.requests[1].body, "payment_request.json")
	if cache.stored["asaas"+document] != customerID {
		t.Fatalf("cache = %v", cache.stored)
	}
}

func assertHeaders(t *testing.T, header http.Header) {
	t.Helper()
	want := map[string]string{"access_token": apiKey, "User-Agent": "paygo", "Content-Type": "application/json"}
	for name, value := range want {
		if got := header.Get(name); got != value {
			t.Fatalf("header %s = %q, want %q", name, got, value)
		}
	}
}

func TestCreatePixCharge_CachedCustomer_SkipsCustomerRequest(t *testing.T) {
	provider, stub := newStub(t, cachedCustomer(),
		reply{http.StatusOK, "payment_response.json"},
		reply{http.StatusOK, "pix_qr_code_response.json"},
	)
	if _, err := provider.CreatePixCharge(context.Background(), chargeRequest()); err != nil {
		t.Fatalf("CreatePixCharge: %v", err)
	}
	if len(stub.requests) != 2 || stub.requests[0].path != "/payments" {
		t.Fatalf("requests = %+v", stub.requests)
	}
}

func TestCreatePixCharge_StepFails_ReturnsMappedError(t *testing.T) {
	tests := []struct {
		name    string
		cache   *stubCache
		replies []reply
		want    error
	}{
		{"find fails", &stubCache{findErr: errCache}, nil, errCache},
		{"customer unavailable", emptyCache(), []reply{{http.StatusInternalServerError, ""}}, payment.ErrProviderUnavailable},
		{"save fails", &stubCache{stored: map[string]string{}, saveErr: errCache}, []reply{{http.StatusOK, "customer_response.json"}}, errCache},
		{"payment throttled", cachedCustomer(), []reply{{http.StatusTooManyRequests, ""}}, payment.ErrProviderUnavailable},
		{"payment rejected", cachedCustomer(), []reply{{http.StatusBadRequest, "error_response.json"}}, payment.ErrProviderRejected},
		{"qr code unavailable", cachedCustomer(), []reply{{http.StatusOK, "payment_response.json"}, {http.StatusBadGateway, ""}}, payment.ErrProviderUnavailable},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider, _ := newStub(t, testCase.cache, testCase.replies...)
			_, err := provider.CreatePixCharge(context.Background(), chargeRequest())
			if !errors.Is(err, testCase.want) {
				t.Fatalf("err = %v, want %v", err, testCase.want)
			}
			if err != nil && strings.Contains(err.Error(), apiKey) {
				t.Fatalf("error leaks api key: %v", err)
			}
		})
	}
}

func TestCreatePixCharge_Rejected_CarriesAsaasDescription(t *testing.T) {
	provider, _ := newStub(t, cachedCustomer(), reply{http.StatusBadRequest, "error_response.json"})
	_, err := provider.CreatePixCharge(context.Background(), chargeRequest())
	if err == nil || !strings.Contains(err.Error(), "invalid customer") {
		t.Fatalf("err = %v, want Asaas description", err)
	}
}

func TestGetCharge_Statuses_MapToKinds(t *testing.T) {
	tests := []struct {
		status string
		want   payment.EventKind
	}{
		{"RECEIVED", payment.EventChargePaid},
		{"CONFIRMED", payment.EventChargePaid},
		{"OVERDUE", payment.EventChargeExpired},
		{"PENDING", payment.EventIgnored},
	}
	for _, testCase := range tests {
		t.Run(testCase.status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"id":"pay","status":"` + testCase.status + `"}`))
			}))
			defer server.Close()
			provider := asaas.NewMoney(server.URL, apiKey, emptyCache(), server.Client())
			got, err := provider.GetCharge(context.Background(), "pay")
			if err != nil {
				t.Fatalf("GetCharge: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("kind = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestGetCharge_ServerError_ReturnsUnavailable(t *testing.T) {
	provider, _ := newStub(t, emptyCache(), reply{http.StatusServiceUnavailable, ""})
	if _, err := provider.GetCharge(context.Background(), "pay"); !errors.Is(err, payment.ErrProviderUnavailable) {
		t.Fatalf("err = %v, want ErrProviderUnavailable", err)
	}
}

func TestGetCharge_TransportFailures_ReturnUnavailable(t *testing.T) {
	tests := []struct {
		name      string
		transport http.RoundTripper
	}{
		{"timeout", failingTransport{context.DeadlineExceeded}},
		{"unreadable body", brokenBodyTransport{}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider := asaas.NewMoney("http://asaas.test", apiKey, emptyCache(), &http.Client{Transport: testCase.transport})
			_, err := provider.GetCharge(context.Background(), "pay")
			if !errors.Is(err, payment.ErrProviderUnavailable) {
				t.Fatalf("err = %v, want ErrProviderUnavailable", err)
			}
			if strings.Contains(err.Error(), apiKey) {
				t.Fatalf("error leaks api key: %v", err)
			}
		})
	}
}

func TestGetCharge_MalformedResponse_ReturnsUnavailable(t *testing.T) {
	provider, _ := newStub(t, emptyCache(), reply{http.StatusOK, "malformed.txt"})
	if _, err := provider.GetCharge(context.Background(), "pay"); !errors.Is(err, payment.ErrProviderUnavailable) {
		t.Fatalf("err = %v, want ErrProviderUnavailable", err)
	}
}

func TestGetCharge_MalformedErrorBody_ReturnsRejected(t *testing.T) {
	provider, _ := newStub(t, emptyCache(), reply{http.StatusBadRequest, "malformed.txt"})
	if _, err := provider.GetCharge(context.Background(), "pay"); !errors.Is(err, payment.ErrProviderRejected) {
		t.Fatalf("err = %v, want ErrProviderRejected", err)
	}
}

func TestGetCharge_InvalidBaseURL_ReturnsError(t *testing.T) {
	provider := asaas.NewMoney("http://[::1", apiKey, emptyCache(), &http.Client{})
	if _, err := provider.GetCharge(context.Background(), "pay"); err == nil {
		t.Fatal("err = nil, want build error")
	}
}

func TestSendCall_UnencodableInput_ReturnsError(t *testing.T) {
	provider := asaas.NewMoney("http://asaas.test", apiKey, emptyCache(), &http.Client{})
	var unsupported *json.UnsupportedTypeError
	if err := asaas.SendCall(context.Background(), provider, make(chan int)); !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want UnsupportedTypeError", err)
	}
}

func TestName_Always_ReturnsAsaas(t *testing.T) {
	if got := asaas.NewMoney("", apiKey, emptyCache(), &http.Client{}).Name(); got != "asaas" {
		t.Fatalf("name = %q, want asaas", got)
	}
}
