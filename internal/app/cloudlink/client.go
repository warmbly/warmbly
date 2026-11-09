package cloudlink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
)

// client is the outbound-only HTTP client to the cloud's pool-link API.
type client struct {
	baseURL string
	token   string
	version string
	http    *http.Client
}

func newClient(baseURL, token, version string) *client {
	// The cloud URL is configurable, so it dials public addresses only; a loopback cloud is a dev setup.
	hc := safehttp.Client(20 * time.Second)
	if devMode() {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	return &client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   token,
		version: version,
		http:    hc,
	}
}

// remoteError is the cloud's error envelope, re-surfaced with its own code.
type remoteError struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
}

var (
	// remoteIdentifier is a machine code callers branch on; anything else is replaced.
	remoteIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	// poolLinkCode is the cloud API's own vocabulary, the only codes whose message is shown.
	poolLinkCode = regexp.MustCompile(`^pool_link_[a-z_]{1,48}$`)

	errCloudUnreachable = errx.NewWithIdentifier(errx.ServiceUnavailable, "cloud_link_unreachable", "Warmbly Cloud could not be reached. Try again in a moment.")
)

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
		log.Warn().Err(err).Str("path", path).Msg("cloudlink: request failed")
		return errCloudUnreachable
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 400 {
		return remoteFailure(res.StatusCode, path, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return errx.NewWithIdentifier(errx.ServiceUnavailable, "cloud_link_bad_response", "Warmbly Cloud returned an unreadable response.")
		}
	}
	return nil
}

// remoteFailure re-surfaces the cloud's code; its text only for the pool-link vocabulary.
func remoteFailure(status int, path string, raw []byte) *errx.Error {
	var re remoteError
	_ = json.Unmarshal(raw, &re)
	code := remoteCode(status)
	id := re.Code
	if !remoteIdentifier.MatchString(id) {
		id = "cloud_link_remote"
	}
	if poolLinkCode.MatchString(id) {
		if msg := cleanRemoteMessage(re.Message); msg != "" {
			return errx.NewWithIdentifier(code, id, msg)
		}
	}
	log.Warn().Int("status", status).Str("path", path).Str("code", re.Code).Str("remote_request_id", re.RequestID).Msg("cloudlink: cloud refused the request")
	return errx.NewWithIdentifier(code, id, fmt.Sprintf("Warmbly Cloud answered %d.", status))
}

// cleanRemoteMessage keeps a cloud message short and printable, or drops it.
func cleanRemoteMessage(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 400 {
		return ""
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return ""
		}
	}
	return s
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
	// A timeout says nothing about whether the cloud acted, so it is answered as unknown, never as a refusal.
	case http.StatusRequestTimeout:
		return errx.ServiceUnavailable
	}
	if status >= 500 {
		return errx.ServiceUnavailable
	}
	return errx.BadRequest
}

// cloudRefused reports a definite answer from the cloud that it did not act, as opposed to an unknown outcome.
func cloudRefused(xerr *errx.Error) bool {
	return xerr != nil && xerr.Code >= 400 && xerr.Code < 500 && xerr.Code != errx.Code(http.StatusTooManyRequests)
}
