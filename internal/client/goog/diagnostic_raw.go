package goog

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) DiagnosticRawMessage(ctx context.Context, id string, maxBytes int) ([]byte, error) {
	if c.srv == nil || c.rawClient == nil || id == "" || maxBytes <= 0 {
		return nil, errors.New("diagnostic MIME unavailable")
	}
	endpoint := strings.TrimRight(c.srv.BasePath, "/") + "/gmail/v1/users/me/messages/" + url.PathEscape(id) + "?format=raw&fields=raw"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("diagnostic MIME unavailable")
	}
	resp, err := c.rawClient.Do(req)
	if err != nil {
		return nil, errors.New("diagnostic MIME unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("diagnostic MIME unavailable")
	}
	limit := maxBytes*4/3 + 65536
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit+1)))
	if err != nil || len(data) > limit {
		return nil, errors.New("diagnostic MIME unavailable")
	}
	var encoded struct {
		Raw string `json:"raw"`
	}
	if json.Unmarshal(data, &encoded) != nil {
		return nil, errors.New("diagnostic MIME unavailable")
	}
	if len(encoded.Raw) > ((maxBytes+2)/3)*4 {
		return nil, errors.New("diagnostic MIME unavailable")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(encoded.Raw, "="))
	if err != nil || len(raw) > maxBytes {
		return nil, errors.New("diagnostic MIME unavailable")
	}
	return raw, nil
}
