package tracing

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/UrielJaloto/paygo/services/apikey"
)

type apikeyService interface {
	Issue(ctx context.Context, input apikey.IssueInput) (apikey.IssuedKey, error)
	Authenticate(ctx context.Context, token string) (apikey.Principal, error)
	Revoke(ctx context.Context, input apikey.RevokeInput) error
}

type APIKeyServiceTracer struct {
	next   apikeyService
	tracer trace.Tracer
}

func NewAPIKeyServiceTracer(next apikeyService, tracer trace.Tracer) *APIKeyServiceTracer {
	return &APIKeyServiceTracer{next: next, tracer: tracer}
}

func (t *APIKeyServiceTracer) Issue(ctx context.Context, input apikey.IssueInput) (apikey.IssuedKey, error) {
	ctx, span := t.tracer.Start(ctx, "apikey.Issue")
	output, err := t.next.Issue(ctx, input)
	endSpan(span, err)
	return output, err
}

func (t *APIKeyServiceTracer) Authenticate(ctx context.Context, token string) (apikey.Principal, error) {
	ctx, span := t.tracer.Start(ctx, "apikey.Authenticate")
	output, err := t.next.Authenticate(ctx, token)
	endSpan(span, err)
	return output, err
}

func (t *APIKeyServiceTracer) Revoke(ctx context.Context, input apikey.RevokeInput) error {
	ctx, span := t.tracer.Start(ctx, "apikey.Revoke")
	err := t.next.Revoke(ctx, input)
	endSpan(span, err)
	return err
}
