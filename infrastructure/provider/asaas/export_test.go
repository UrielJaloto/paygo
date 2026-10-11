package asaas

import "context"

func SendCall(ctx context.Context, p *Provider, input any) error {
	return p.sendCall(ctx, apiCall{method: "POST", path: "/any", input: input})
}
