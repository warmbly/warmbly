package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger is gin's standard access log with every credential removed.
//
// gin.Logger() prints the full request URI, and several routes carry a
// credential there because the provider or the mail client puts it there:
// OAuth `code` and `state` on the callback bouncers, the team invitation token
// on the preview lookup, the socket ticket on /v1/getaway's reply, the form
// prefill ticket. Others carry one in the path: inbound webhook secrets,
// unsubscribe tokens, device and warmup tokens, and the pairing codes of the
// Slack, CLI and pool links. None of them may reach stdout,
// the container log, or whatever aggregates it.
//
// So the query is dropped and a path parameter named in credentialParams is
// logged as its name. A request that has to be traced has X-Request-Id.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if gin.Mode() == gin.ReleaseMode && routineInternalLookup(c) {
			return
		}
		end := time.Now()
		fmt.Fprintf(gin.DefaultWriter, "[GIN] %v | %3d | %13v | %15s | %-7s %#v\n",
			end.Format("2006/01/02 - 15:04:05"),
			c.Writer.Status(),
			end.Sub(start),
			c.ClientIP(),
			c.Request.Method,
			loggedPath(c),
		)
	}
}

func routineInternalLookup(c *gin.Context) bool {
	if len(c.Errors) != 0 {
		return false
	}
	path, status := c.FullPath(), c.Writer.Status()
	// The batched map lookup is a read sent as POST to carry its id list.
	if c.Request.Method == http.MethodPost && path == "/api/v1/internal/email-message-map/lookup" {
		return status < http.StatusBadRequest
	}
	if c.Request.Method != http.MethodGet {
		return false
	}
	if path == "/api/v1/internal/email-message-map" && status == http.StatusNotFound {
		return true
	}
	return status < http.StatusBadRequest && (path == "/health" || strings.HasPrefix(path, "/api/v1/internal/"))
}

// credentialParams are the route parameter names whose value is a credential.
var credentialParams = map[string]bool{"secret": true, "token": true, "code": true}

// loggedPath is the request path with every credential parameter replaced by
// its name, rebuilt from the matched route so no value is guessed at.
func loggedPath(c *gin.Context) string {
	path := c.Request.URL.Path
	route := c.FullPath()
	if route == "" {
		return path
	}
	redact := false
	for _, p := range c.Params {
		if credentialParams[p.Key] {
			redact = true
			break
		}
	}
	if !redact {
		return path
	}

	segs := strings.Split(route, "/")
	for i, seg := range segs {
		switch {
		case strings.HasPrefix(seg, ":"):
			if name := seg[1:]; !credentialParams[name] {
				segs[i] = c.Param(name)
			}
		case strings.HasPrefix(seg, "*"):
			segs[i] = strings.TrimPrefix(c.Param(seg[1:]), "/")
		}
	}
	return strings.Join(segs, "/")
}
