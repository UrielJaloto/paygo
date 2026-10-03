package rest_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/domain/webhook"
	"github.com/UrielJaloto/paygo/presentation/rest"
	apikeyservice "github.com/UrielJaloto/paygo/services/apikey"
	webhookservice "github.com/UrielJaloto/paygo/services/webhook"
	"github.com/gin-gonic/gin"
)

func failing(err error) http.Handler {
	engine := gin.New()
	engine.GET("/fail", func(c *gin.Context) { rest.PresentError(c, err) })
	return engine
}

func TestPresentError_EachMapping_ReturnsStatusAndType(t *testing.T) {
	cases := []struct {
		err    error
		status int
		slug   string
	}{
		{rest.ErrMalformedRequest, http.StatusBadRequest, "malformed-request"},
		{rest.ErrIdempotencyKeyRequired, http.StatusBadRequest, "idempotency-key-required"},
		{rest.ErrPayloadTooLarge, http.StatusRequestEntityTooLarge, "payload-too-large"},
		{rest.ErrUnsupportedMediaType, http.StatusUnsupportedMediaType, "unsupported-media-type"},
		{money.ErrInvalidAmount, http.StatusBadRequest, "invalid-request"},
		{money.ErrUnsupportedCurrency, http.StatusBadRequest, "invalid-request"},
		{transfer.ErrInvalidIdempotencyKey, http.StatusBadRequest, "invalid-request"},
		{page.ErrInvalidPageLimit, http.StatusBadRequest, "invalid-request"},
		{page.ErrInvalidCursor, http.StatusBadRequest, "invalid-request"},
		{webhook.ErrInvalidEndpointURL, http.StatusBadRequest, "invalid-request"},
		{webhook.ErrInvalidSecret, http.StatusBadRequest, "invalid-request"},
		{webhookservice.ErrInvalidDeliveryStatus, http.StatusBadRequest, "invalid-request"},
		{account.ErrInvalidAccountID, http.StatusBadRequest, "invalid-request"},
		{apikey.ErrInvalidOwner, http.StatusBadRequest, "invalid-request"},
		{transfer.ErrSameAccount, http.StatusBadRequest, "invalid-request"},
		{apikeyservice.ErrUnauthenticated, http.StatusUnauthorized, "unauthenticated"},
		{account.ErrAccountNotFound, http.StatusNotFound, "not-found"},
		{transfer.ErrTransferNotFound, http.StatusNotFound, "not-found"},
		{webhook.ErrEndpointNotFound, http.StatusNotFound, "not-found"},
		{webhook.ErrDeliveryNotFound, http.StatusNotFound, "not-found"},
		{apikey.ErrAPIKeyNotFound, http.StatusNotFound, "not-found"},
		{account.ErrAccountFrozen, http.StatusConflict, "conflict"},
		{account.ErrAccountAlreadyFrozen, http.StatusConflict, "conflict"},
		{account.ErrAccountNotFrozen, http.StatusConflict, "conflict"},
		{transfer.ErrTransferAlreadyReversed, http.StatusConflict, "conflict"},
		{transfer.ErrReversalNotReversible, http.StatusConflict, "conflict"},
		{webhook.ErrDeliveryNotReplayable, http.StatusConflict, "conflict"},
		{webhook.ErrEndpointLimitReached, http.StatusConflict, "conflict"},
		{webhook.ErrEndpointAlreadyDisabled, http.StatusConflict, "conflict"},
		{apikey.ErrAPIKeyAlreadyRevoked, http.StatusConflict, "conflict"},
		{money.ErrCurrencyMismatch, http.StatusConflict, "conflict"},
		{account.ErrConcurrentModification, http.StatusConflict, "conflict"},
		{transfer.ErrIdempotencyConflict, http.StatusConflict, "idempotency-conflict"},
		{transfer.ErrInsufficientFunds, http.StatusUnprocessableEntity, "insufficient-funds"},
		{money.ErrAmountTooLarge, http.StatusUnprocessableEntity, "amount-too-large"},
		{rest.ErrRateLimited, http.StatusTooManyRequests, "rate-limited"},
	}
	for _, testCase := range cases {
		t.Run(testCase.err.Error(), func(t *testing.T) {
			recorder := get(failing(testCase.err), "/fail")
			body := decodeProblem(t, recorder)
			if recorder.Code != testCase.status || body.Status != testCase.status || body.Type != "/problems/"+testCase.slug || body.Title == "" {
				t.Fatalf("code = %d, body = %+v", recorder.Code, body)
			}
		})
	}
}

func TestPresentError_WrappedBusinessError_DetailOmitsWrapping(t *testing.T) {
	recorder := get(failing(fmt.Errorf("locking row: %w", account.ErrAccountNotFound)), "/fail")
	if body := decodeProblem(t, recorder); recorder.Code != http.StatusNotFound || body.Detail != "account not found" {
		t.Fatalf("code = %d, body = %+v", recorder.Code, body)
	}
}

func TestPresentError_WrappedValidationError_DetailKeepsContext(t *testing.T) {
	recorder := get(failing(fmt.Errorf("amount: %w", money.ErrInvalidAmount)), "/fail")
	if body := decodeProblem(t, recorder); recorder.Code != http.StatusBadRequest || body.Detail != "amount: invalid amount" {
		t.Fatalf("code = %d, body = %+v", recorder.Code, body)
	}
}

func TestPresentError_UnknownError_HidesInternals(t *testing.T) {
	recorder := get(failing(errBoom), "/fail")
	body := decodeProblem(t, recorder)
	if recorder.Code != http.StatusInternalServerError || body.Type != "/problems/internal" || body.Detail != "internal error" {
		t.Fatalf("code = %d, body = %+v", recorder.Code, body)
	}
}
