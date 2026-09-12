package cloudlink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
)

// client is the outbound-only HTTP client to the cloud's pool-link API.
type client struct {
	baseURL string
	token   string
	version string
	http    *http.Client
}

func newClient(baseURL, token, version string) *client {
	return &client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   token,
		version: version,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// remoteError is the cloud's error envelope, re-surfaced with its own code.
type remoteError struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
}

func (c *client) do(ctx context.Context, method, path string, body any, out any) *errx.Error {
	var buf io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return errx.InternalError()
		}
		buf = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/v1/pool-link"+path, buf)
	if err != nil {
		return errx.InternalError()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "warmbly-cloudlink/"+c.version)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.version != "" {
		req.Header.Set("X-Warmbly-Instance-Version", c.version)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return errx.NewWithIdentifier(errx.ServiceUnavailable, "cloud_link_unreachable", fmt.Sprintf("Warmbly Cloud could not be reached: %v", err))
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 400 {
		var re remoteError
		_ = json.Unmarshal(raw, &re)
		code := remoteCode(res.StatusCode)
		if re.Message == "" {
			re.Message = fmt.Sprintf("Warmbly Cloud answered %d", res.StatusCode)
		}
		if re.Code == "" {
			re.Code = "cloud_link_remote"
		}
		return errx.NewWithIdentifier(code, re.Code, re.Message)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return errx.NewWithIdentifier(errx.ServiceUnavailable, "cloud_link_bad_response", "Warmbly Cloud returned an unreadable response.")
		}
	}
	return nil
}

// remoteCode maps the cloud's status onto one errx can answer with. errx.JSON
// writes status 0 for a code outside its table, which gin turns into a 200, so
// an unmapped answer (a proxy's 502) would report a failed call as a success.
func remoteCode(status int) errx.Code {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusNotImplemented, http.StatusServiceUnavailable:
		return errx.Code(status)
	case http.StatusGone:
		return errx.NotFound
	}
	if status >= 500 {
		return errx.ServiceUnavailable
	}
	return errx.BadRequest
}
