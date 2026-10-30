package asaas

import "context"

func DoWithInput(ctx context.Context, p *Provider, in any) error {
	return p.do(ctx, apiCall{method: "POST", path: "/any", in: in})
}
