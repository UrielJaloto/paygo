package rest

import (
	"errors"
	"net/http"
	"slices"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/domain/webhook"
	apikeyservice "github.com/UrielJaloto/paygo/services/apikey"
	webhookservice "github.com/UrielJaloto/paygo/services/webhook"
	"github.com/gin-gonic/gin"
)

const internalErrorDetail = "internal error"

type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

type problemKind struct {
	status int
	slug   string
	title  string
}

type errorMapping struct {
	target error
	kind   problemKind
}

func PresentError(c *gin.Context, err error) {
	for _, mapping := range errorMappings() {
		if errors.Is(err, mapping.target) {
			presentProblem(c, mapping.kind, problemDetail(err, mapping))
			return
		}
	}
	presentProblem(c, problemKind{http.StatusInternalServerError, "internal", "Internal server error"}, internalErrorDetail)
}

func presentProblem(c *gin.Context, kind problemKind, detail string) {
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(kind.status, problem{Type: "/problems/" + kind.slug, Title: kind.title, Status: kind.status, Detail: detail})
}

func problemDetail(err error, mapping errorMapping) string {
	if mapping.kind.status == http.StatusBadRequest {
		return err.Error()
	}
	return mapping.target.Error()
}

func errorMappings() []errorMapping {
	return slices.Concat(
		mappingsFor(problemKind{http.StatusBadRequest, "malformed-request", "Malformed request"}, errMalformedRequest),
		mappingsFor(problemKind{http.StatusRequestEntityTooLarge, "payload-too-large", "Payload too large"}, errPayloadTooLarge),
		mappingsFor(problemKind{http.StatusUnsupportedMediaType, "unsupported-media-type", "Unsupported media type"}, errUnsupportedMediaType),
		mappingsFor(problemKind{http.StatusBadRequest, "invalid-request", "Invalid request"}, invalidRequestErrors()...),
		mappingsFor(problemKind{http.StatusBadRequest, "idempotency-key-required", "Idempotency key required"}, errIdempotencyKeyRequired),
		mappingsFor(problemKind{http.StatusUnauthorized, "unauthenticated", "Unauthenticated"}, apikeyservice.ErrUnauthenticated, payment.ErrWebhookUnauthorized),
		mappingsFor(problemKind{http.StatusNotFound, "not-found", "Not found"}, notFoundErrors()...),
		mappingsFor(problemKind{http.StatusConflict, "conflict", "Conflict"}, conflictErrors()...),
		mappingsFor(problemKind{http.StatusConflict, "idempotency-conflict", "Idempotency conflict"}, transfer.ErrIdempotencyConflict),
		mappingsFor(problemKind{http.StatusUnprocessableEntity, "insufficient-funds", "Insufficient funds"}, transfer.ErrInsufficientFunds),
		mappingsFor(problemKind{http.StatusUnprocessableEntity, "amount-too-large", "Amount too large"}, money.ErrAmountTooLarge),
		mappingsFor(problemKind{http.StatusUnprocessableEntity, "provider-rejected", "Provider rejected"}, payment.ErrProviderRejected),
		mappingsFor(problemKind{http.StatusBadGateway, "provider-unavailable", "Provider unavailable"}, payment.ErrProviderUnavailable),
		mappingsFor(problemKind{http.StatusTooManyRequests, "rate-limited", "Rate limited"}, errRateLimited),
	)
}

func mappingsFor(kind problemKind, targets ...error) []errorMapping {
	mappings := make([]errorMapping, 0, len(targets))
	for _, target := range targets {
		mappings = append(mappings, errorMapping{target: target, kind: kind})
	}
	return mappings
}

func invalidRequestErrors() []error {
	return []error{
		money.ErrInvalidAmount,
		money.ErrUnsupportedCurrency,
		transfer.ErrInvalidIdempotencyKey,
		page.ErrInvalidPageLimit,
		page.ErrInvalidCursor,
		webhook.ErrInvalidEndpointURL,
		webhook.ErrInvalidSecret,
		webhookservice.ErrInvalidDeliveryStatus,
		payment.ErrInvalidPixKey,
		payment.ErrInvalidDocument,
		payment.ErrUnsupportedProvider,
		payment.ErrInvalidPayerName,
		account.ErrInvalidAccountID,
		apikey.ErrInvalidOwner,
		transfer.ErrSameAccount,
	}
}

func notFoundErrors() []error {
	return []error{
		account.ErrAccountNotFound,
		transfer.ErrTransferNotFound,
		webhook.ErrEndpointNotFound,
		webhook.ErrDeliveryNotFound,
		payment.ErrChargeNotFound,
		payment.ErrPayoutNotFound,
		apikey.ErrAPIKeyNotFound,
	}
}

func conflictErrors() []error {
	return []error{
		account.ErrAccountFrozen,
		account.ErrAccountAlreadyFrozen,
		account.ErrAccountNotFrozen,
		transfer.ErrTransferAlreadyReversed,
		transfer.ErrReversalNotReversible,
		webhook.ErrDeliveryNotReplayable,
		webhook.ErrEndpointLimitReached,
		webhook.ErrEndpointAlreadyDisabled,
		apikey.ErrAPIKeyAlreadyRevoked,
		money.ErrCurrencyMismatch,
		account.ErrConcurrentModification,
	}
}
