package payment_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
)

var (
	createdAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	expiresAt = createdAt.Add(24 * time.Hour)
	later     = createdAt.Add(time.Hour)
)

func chargeDraft() payment.ChargeDraft {
	return payment.ChargeDraft{
		OwnerID:   "o1",
		AccountID: "a1",
		Amount:    money.NewMoney(100, money.BRL),
		Payer:     payment.Payer{Document: "52998224725", Name: "Ana"},
		Provider:  "asaas",
	}
}

func newCharge(t *testing.T) payment.Charge {
	t.Helper()
	created, err := payment.NewCharge("c1", chargeDraft(), expiresAt, createdAt)
	if err != nil {
		t.Fatalf("NewCharge: %v", err)
	}
	return created
}

func newPayout(t *testing.T) payment.Payout {
	t.Helper()
	key, err := payment.ParsePixKey("email", "a@b.co")
	if err != nil {
		t.Fatalf("ParsePixKey: %v", err)
	}
	created, err := payment.NewPayout("p1", payment.PayoutDraft{
		OwnerID:        "o1",
		AccountID:      "a1",
		Amount:         money.NewMoney(100, money.BRL),
		PixKey:         key,
		Provider:       "asaas",
		HoldTransferID: "hold",
	}, createdAt)
	if err != nil {
		t.Fatalf("NewPayout: %v", err)
	}
	return created
}

func TestParseDocument_Valid_ReturnsDigits(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{name: "cpf", text: "529.982.247-25", want: "52998224725"},
		{name: "cpf with zero check digit", text: "123.456.789-09", want: "12345678909"},
		{name: "cnpj", text: "11.222.333/0001-81", want: "11222333000181"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := payment.ParseDocument(testCase.text)
			if err != nil || string(got) != testCase.want {
				t.Fatalf("got (%q, %v), want %q", got, err, testCase.want)
			}
		})
	}
}

func TestParseDocument_Invalid_ReturnsErrInvalidDocument(t *testing.T) {
	cases := []struct{ name, text string }{
		{name: "cpf bad digit", text: "52998224724"},
		{name: "cpf bad second digit", text: "52998224726"},
		{name: "cpf all equal", text: "11111111111"},
		{name: "cnpj bad digit", text: "11222333000180"},
		{name: "wrong length", text: "123"},
		{name: "letters", text: "5299822472a"},
		{name: "empty", text: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := payment.ParseDocument(testCase.text); !errors.Is(err, payment.ErrInvalidDocument) {
				t.Fatalf("error = %v, want ErrInvalidDocument", err)
			}
		})
	}
}

func TestParsePixKey_Valid_KeepsKindAndValue(t *testing.T) {
	evp := "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	cases := []struct {
		name, kind, value, want string
	}{
		{name: "cpf", kind: "cpf", value: "529.982.247-25", want: "52998224725"},
		{name: "cnpj", kind: "cnpj", value: "11.222.333/0001-81", want: "11222333000181"},
		{name: "email", kind: "email", value: "a@b.co", want: "a@b.co"},
		{name: "email 77 chars", kind: "email", value: strings.Repeat("a", 72) + "@b.co", want: strings.Repeat("a", 72) + "@b.co"},
		{name: "phone", kind: "phone", value: "+5511987654321", want: "+5511987654321"},
		{name: "evp", kind: "evp", value: evp, want: evp},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := payment.ParsePixKey(testCase.kind, testCase.value)
			if err != nil || string(got.Kind()) != testCase.kind || got.Value() != testCase.want {
				t.Fatalf("got (%+v, %v)", got, err)
			}
		})
	}
}

func TestParsePixKey_Invalid_ReturnsErrInvalidPixKey(t *testing.T) {
	cases := []struct{ name, kind, value string }{
		{name: "cpf holding cnpj", kind: "cpf", value: "11222333000181"},
		{name: "cnpj holding cpf", kind: "cnpj", value: "52998224725"},
		{name: "cpf bad digit", kind: "cpf", value: "52998224724"},
		{name: "email no at", kind: "email", value: "ab.co"},
		{name: "email two at", kind: "email", value: "a@b@c.co"},
		{name: "email dot before at", kind: "email", value: "a.b@co"},
		{name: "email 78 chars", kind: "email", value: strings.Repeat("a", 73) + "@b.co"},
		{name: "phone no prefix", kind: "phone", value: "11987654321"},
		{name: "evp upper", kind: "evp", value: "0190A1B2-C3D4-7E5F-8A9B-0C1D2E3F4A5B"},
		{name: "unknown type", kind: "iban", value: "x"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := payment.ParsePixKey(testCase.kind, testCase.value); !errors.Is(err, payment.ErrInvalidPixKey) {
				t.Fatalf("error = %v, want ErrInvalidPixKey", err)
			}
		})
	}
}

func TestNewCharge_Valid_IsPending(t *testing.T) {
	created := newCharge(t)
	if created.ID() != "c1" || created.OwnerID() != "o1" || created.AccountID() != "a1" {
		t.Fatalf("unexpected identity: %+v", created)
	}
	if created.Amount() != money.NewMoney(100, money.BRL) || created.Payer().Name != "Ana" {
		t.Fatalf("unexpected payment: %+v", created)
	}
}

func TestNewCharge_Valid_StartsUnpaid(t *testing.T) {
	created := newCharge(t)
	if created.Status() != payment.ChargePending || created.TransferID() != "" {
		t.Fatalf("unexpected state: %+v", created)
	}
	if created.Reference().Provider != "asaas" || !created.Pix().ExpiresAt.Equal(expiresAt) {
		t.Fatalf("unexpected provider data: %+v", created)
	}
	if !created.Timeline().CreatedAt.Equal(createdAt) || !created.Timeline().UpdatedAt.Equal(createdAt) {
		t.Fatalf("unexpected timeline: %+v", created)
	}
}

func TestNewCharge_NonBRL_ReturnsErrUnsupportedCurrency(t *testing.T) {
	draft := chargeDraft()
	draft.Amount = money.NewMoney(100, money.USD)
	if _, err := payment.NewCharge("c1", draft, expiresAt, createdAt); !errors.Is(err, money.ErrUnsupportedCurrency) {
		t.Fatalf("error = %v, want ErrUnsupportedCurrency", err)
	}
}

func TestNewCharge_PayerName_ValidatesLength(t *testing.T) {
	cases := []struct {
		name    string
		payer   string
		wantErr error
	}{
		{name: "empty", payer: "", wantErr: payment.ErrInvalidPayerName},
		{name: "1 char", payer: "a"},
		{name: "100 multibyte chars", payer: strings.Repeat("é", 100)},
		{name: "101 chars", payer: strings.Repeat("a", 101), wantErr: payment.ErrInvalidPayerName},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			draft := chargeDraft()
			draft.Payer.Name = testCase.payer
			if _, err := payment.NewCharge("c1", draft, expiresAt, createdAt); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func TestAttachProvider_Charge_StoresReferences(t *testing.T) {
	target := newCharge(t)
	target.AttachProvider("prov", "code", "qr")
	if target.Reference().ProviderID != "prov" || target.Pix().CopyPaste != "code" || target.Pix().QRBase64 != "qr" {
		t.Fatalf("got %+v", target)
	}
}

func TestMarkPaid_Twice_AppliesOnce(t *testing.T) {
	target := newCharge(t)
	first, err := target.MarkPaid("t1", later)
	if err != nil || !first {
		t.Fatalf("first = (%v, %v)", first, err)
	}
	if target.Status() != payment.ChargePaid || target.TransferID() != "t1" || !target.Timeline().UpdatedAt.Equal(later) {
		t.Fatalf("unexpected state: %+v", target)
	}
	second, err := target.MarkPaid("t2", later)
	if err != nil || second || target.TransferID() != "t1" {
		t.Fatalf("second = (%v, %v), transfer %s", second, err, target.TransferID())
	}
}

func TestMarkPaid_FromFinalState_ReturnsErrInvalidTransition(t *testing.T) {
	for _, status := range []payment.ChargeStatus{payment.ChargeExpired, payment.ChargeFailed} {
		target := payment.RestoreCharge(payment.ChargeSnapshot{Status: status})
		if _, err := target.MarkPaid("t1", later); !errors.Is(err, payment.ErrInvalidTransition) {
			t.Fatalf("%s: error = %v", status, err)
		}
	}
}

func TestMarkExpiredAndFailed_FollowPending(t *testing.T) {
	expired, failed := newCharge(t), newCharge(t)
	if err := expired.MarkExpired(later); err != nil || expired.Status() != payment.ChargeExpired {
		t.Fatalf("expired: %v, %s", err, expired.Status())
	}
	if err := failed.MarkFailed(later); err != nil || failed.Status() != payment.ChargeFailed {
		t.Fatalf("failed: %v, %s", err, failed.Status())
	}
	if err := expired.MarkFailed(later); !errors.Is(err, payment.ErrInvalidTransition) {
		t.Fatalf("error = %v, want ErrInvalidTransition", err)
	}
	if err := failed.MarkExpired(later); !errors.Is(err, payment.ErrInvalidTransition) {
		t.Fatalf("error = %v, want ErrInvalidTransition", err)
	}
}

func TestNewPayout_Valid_IsPending(t *testing.T) {
	created := newPayout(t)
	if created.ID() != "p1" || created.OwnerID() != "o1" || created.AccountID() != "a1" {
		t.Fatalf("unexpected identity: %+v", created)
	}
	if created.Amount() != money.NewMoney(100, money.BRL) || created.PixKey().Value() != "a@b.co" {
		t.Fatalf("unexpected payment: %+v", created)
	}
}

func TestNewPayout_Valid_StartsUnfinished(t *testing.T) {
	created := newPayout(t)
	if created.Status() != payment.PayoutPending || created.Transfers().Hold != "hold" || created.Transfers().Final != "" {
		t.Fatalf("unexpected state: %+v", created)
	}
	if created.Reference().Provider != "asaas" || !created.Timeline().CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected provider data: %+v", created)
	}
}

func TestNewPayout_NonBRL_ReturnsErrUnsupportedCurrency(t *testing.T) {
	_, err := payment.NewPayout("p1", payment.PayoutDraft{Amount: money.NewMoney(100, money.EUR)}, createdAt)
	if !errors.Is(err, money.ErrUnsupportedCurrency) {
		t.Fatalf("error = %v, want ErrUnsupportedCurrency", err)
	}
}

func TestAttachProvider_Payout_StoresProviderID(t *testing.T) {
	target := newPayout(t)
	target.AttachProvider("prov")
	if target.Reference().ProviderID != "prov" {
		t.Fatalf("got %+v", target)
	}
}

func TestPayoutMarkFailed_Twice_AppliesOnce(t *testing.T) {
	target := newPayout(t)
	first, err := target.MarkFailed("t1", later)
	if err != nil || !first || target.Status() != payment.PayoutFailed || target.Transfers().Final != "t1" {
		t.Fatalf("first = (%v, %v), %+v", first, err, target)
	}
	second, err := target.MarkFailed("t2", later)
	if err != nil || second || target.Transfers().Final != "t1" {
		t.Fatalf("second = (%v, %v), %+v", second, err, target)
	}
}

func TestPayoutMarkCompleted_ThenMarkFailed_ReturnsErrInvalidTransition(t *testing.T) {
	target := newPayout(t)
	done, err := target.MarkCompleted("t1", later)
	if err != nil || !done || target.Status() != payment.PayoutCompleted || !target.Timeline().UpdatedAt.Equal(later) {
		t.Fatalf("complete = (%v, %v), %+v", done, err, target)
	}
	if _, err := target.MarkFailed("t2", later); !errors.Is(err, payment.ErrInvalidTransition) {
		t.Fatalf("error = %v, want ErrInvalidTransition", err)
	}
}
