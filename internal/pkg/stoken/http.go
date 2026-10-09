package stoken

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

const ProviderRequestTimeout = 45 * time.Second

// BoundedContext also bounds refresh I/O, which uses the token source's context.
func BoundedContext(ctx context.Context) context.Context {
	base := http.DefaultClient
	if client, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok && client != nil {
		base = client
	}
	client := *base
	if client.Timeout <= 0 || client.Timeout > ProviderRequestTimeout {
		client.Timeout = ProviderRequestTimeout
	}
	return context.WithValue(ctx, oauth2.HTTPClient, &client)
}

func HTTPClient(ctx context.Context, source oauth2.TokenSource) *http.Client {
	ctx = BoundedContext(ctx)
	client := oauth2.NewClient(ctx, source)
	client.Timeout = ctx.Value(oauth2.HTTPClient).(*http.Client).Timeout
	return client
}
