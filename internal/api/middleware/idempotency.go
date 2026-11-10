package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/idempotency"
	"github.com/warmbly/warmbly/internal/errx"
)

const (
	IdempotencyKeyHeader      = "Idempotency-Key"
	IdempotencyReplayedHeader = "X-Idempotent-Replayed"
)

// IdempotencyMiddleware implements Stripe-style retry safety for mutating API
// requests. It is opt-in per request via Idempotency-Key and scoped by
// organization, so the same key cannot collide across tenants.
func (h *Handler) IdempotencyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := strings.TrimSpace(c.GetHeader(IdempotencyKeyHeader))
		if key == "" || !isIdempotentMethod(c.Request.Method) {
			c.Next()
			return
		}
		if h.IdempotencyService == nil {
			errx.Handle(c, errx.New(errx.ServiceUnavailable, "idempotency service is not available"))
			c.Abort()
			return
		}
		if !validIdempotencyKey(key) {
			errx.Handle(c, errx.New(errx.BadRequest, "Idempotency-Key must be 1-255 visible ASCII characters"))
			c.Abort()
			return
		}

		var scope idempotency.Scope
		switch orgID, agencyID := GetOrganizationID(c), GetAgencyKeyAgencyID(c); {
		case orgID != nil:
			scope = idempotency.OrgScope(*orgID)
		case agencyID != nil:
			scope = idempotency.AgencyScope(*agencyID)
		default:
			errx.Handle(c, errx.New(errx.BadRequest, "Idempotency-Key requires an organization context"))
			c.Abort()
			return
		}

		body, err := readAndRestoreBody(c)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "failed to read request body"))
			c.Abort()
			return
		}

		// A key belongs to one concrete resource, so identity uses the request path.
		path := c.Request.URL.Path
		requestHash := hashRequest(idempotencyPrincipal(c), c.Request.Method, path, c.Request.URL.RawQuery, body)
		record, state, xerr := h.IdempotencyService.Begin(c.Request.Context(), scope, key, c.Request.Method, path, requestHash)
		if xerr != nil {
			errx.Handle(c, xerr)
			c.Abort()
			return
		}

		switch state {
		case idempotency.StateReplay:
			c.Header(IdempotencyReplayedHeader, "true")
			if record.ContentType != nil && *record.ContentType != "" {
				c.Header("Content-Type", *record.ContentType)
			}
			c.Data(record.StatusCode, c.Writer.Header().Get("Content-Type"), record.ResponseBody)
			c.Abort()
			return
		case idempotency.StateProcessing:
			errx.Handle(c, errx.New(errx.Conflict, "an identical request is still processing"))
			c.Abort()
			return
		case idempotency.StateConflict:
			errx.Handle(c, errx.New(errx.Conflict, "Idempotency-Key was already used with a different request"))
			c.Abort()
			return
		case idempotency.StateUncertain:
			errx.Handle(c, errx.NewWithIdentifier(errx.Conflict, "idempotency_outcome_unknown",
				"The request with this Idempotency-Key ended without a definite outcome. Retry with a new key only once you know the change did not happen."))
			c.Abort()
			return
		}

		capture := &captureResponseWriter{ResponseWriter: c.Writer}
		c.Writer = capture
		finished := false
		defer func() {
			if finished {
				return
			}
			// A panic unwinds past the handler with its side effect unknown.
			p := recover()
			h.finishIdempotent(c, record.ID, outcomeUncertain, http.StatusInternalServerError, capture)
			if p != nil {
				panic(p)
			}
		}()
		c.Next()
		finished = true

		status := capture.Status()
		h.finishIdempotent(c, record.ID, classifyOutcome(status), status, capture)
	}
}

type idempotentOutcome int

const (
	outcomeDefinite idempotentOutcome = iota
	outcomeRefused
	outcomeUncertain
)

// classifyOutcome sorts a finished request: refused before it ran frees the key,
// a server error is uncertain, anything else is replayed.
func classifyOutcome(status int) idempotentOutcome {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden, status == http.StatusTooManyRequests:
		return outcomeRefused
	case status >= http.StatusInternalServerError:
		return outcomeUncertain
	default:
		return outcomeDefinite
	}
}

// idempotencyFinishTimeout bounds the write that records how a request ended.
const idempotencyFinishTimeout = 5 * time.Second

// finishIdempotent records the outcome on a context detached from the request,
// so a client that hangs up after the side effect still leaves it recorded.
func (h *Handler) finishIdempotent(c *gin.Context, recordID uuid.UUID, outcome idempotentOutcome, status int, capture *captureResponseWriter) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), idempotencyFinishTimeout)
	defer cancel()
	var xerr *errx.Error
	switch outcome {
	case outcomeRefused:
		xerr = h.IdempotencyService.Release(ctx, recordID)
	case outcomeUncertain:
		xerr = h.IdempotencyService.MarkUncertain(ctx, recordID, status)
	default:
		xerr = h.IdempotencyService.Complete(ctx, recordID, status, capture.body.Bytes(), capture.Header().Get("Content-Type"))
	}
	if xerr != nil {
		log.Error().Str("request_id", c.GetString(RequestIDContextKey)).Str("idempotency_record", recordID.String()).
			Msg("idempotency: could not record how the request ended; a retry with the same key reports it as still processing")
	}
}

// idempotencyPrincipal names the credential a key belongs to, so one caller
// never receives another's stored answer.
func idempotencyPrincipal(c *gin.Context) string {
	switch GetAuthType(c) {
	case AuthTypeAPIKey:
		if id := GetAPIKeyID(c); id != nil {
			return "api_key:" + id.String()
		}
	case AuthTypeOAuth:
		// A grant keeps its id across refreshes, and each grant holds its own scopes.
		if grant := GetOAuthGrantID(c); grant != nil {
			return "oauth_grant:" + grant.String()
		}
		if app := GetOAuthApplicationID(c); app != nil {
			return "oauth:" + app.String() + ":" + GetUserID(c)
		}
	}
	return "user:" + GetUserID(c)
}

func isIdempotentMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func validIdempotencyKey(key string) bool {
	if key == "" || len(key) > 255 {
		return false
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}

func readAndRestoreBody(c *gin.Context) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// idempotencyHashVersion keeps every hash distinct from those written without a
// principal, so an older record can only ever read as a different request.
const idempotencyHashVersion = "v2"

func hashRequest(principal, method, path, rawQuery string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(idempotencyHashVersion))
	h.Write([]byte{0})
	h.Write([]byte(principal))
	h.Write([]byte{0})
	h.Write([]byte(method))
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write([]byte(rawQuery))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

type captureResponseWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *captureResponseWriter) Write(data []byte) (int, error) {
	w.body.Write(data)
	return w.ResponseWriter.Write(data)
}

func (w *captureResponseWriter) WriteString(data string) (int, error) {
	w.body.WriteString(data)
	return w.ResponseWriter.WriteString(data)
}
