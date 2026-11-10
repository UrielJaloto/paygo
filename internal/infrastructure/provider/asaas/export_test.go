package asaas

import "context"

func SendCall(ctx context.Context, provider *Provider, input any) error {
	return provider.sendCall(ctx, apiCall{method: "POST", path: "/any", input: input})
}
