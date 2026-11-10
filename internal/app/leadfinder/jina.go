package leadfinder

import (
	"context"
	"io"
	"net/http"
)

// jinaCheckURL is one small read the key check makes; tests replace it.
var jinaCheckURL = "https://r.jina.ai/https://example.com/"

// CheckJinaKey proves a Jina AI key works with one read of a tiny page.
func CheckJinaKey(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jinaCheckURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, err := providerHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return statusErr(resp.StatusCode)
	}
	return nil
}
