package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
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

		orgID := GetOrganizationID(c)
		if orgID == nil {
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

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		requestHash := hashRequest(c.Request.Method, path, c.Request.URL.RawQuery, body)
		record, state, xerr := h.IdempotencyService.Begin(c.Request.Context(), *orgID, key, c.Request.Method, path, requestHash)
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
		}

		capture := &captureResponseWriter{ResponseWriter: c.Writer}
		c.Writer = capture
		c.Next()

		status := capture.Status()
		if status == 0 {
			status = http.StatusOK
		}
		contentType := capture.Header().Get("Content-Type")
		_ = h.IdempotencyService.Complete(c.Request.Context(), record.ID, status, capture.body.Bytes(), contentType)
	}
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

func hashRequest(method, path, rawQuery string, body []byte) string {
	h := sha256.New()
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
