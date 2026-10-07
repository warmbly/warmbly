package msgraph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
)

func (c *Client) DiagnosticRawMessage(ctx context.Context, id string, maxBytes int) ([]byte, error) {
	if id == "" || maxBytes <= 0 || c.hc == nil {
		return nil, errors.New("diagnostic MIME unavailable")
	}
	resp, err := c.do(ctx, http.MethodGet, c.root()+"/messages/"+url.PathEscape(id)+"/$value", "", nil)
	if err != nil {
		return nil, errors.New("diagnostic MIME unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("diagnostic MIME unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes+1)))
	if err != nil || len(data) > maxBytes {
		return nil, errors.New("diagnostic MIME unavailable")
	}
	return data, nil
}
