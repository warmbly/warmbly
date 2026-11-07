package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/api/middleware"
)

// Access logging for every internal endpoint that hands a node something worth
// more than a record: the data-key decrypt broker, the blob presigner, the
// node env and the managed-mailbox access token. Each access, allowed or
// refused, leaves one line naming the operation, the subject, the node that
// asked, the caller's address and the outcome, so an operator can see a
// misconfigured node or someone holding a node credential and looking around.
//
// Never logged: ciphertext, plaintext keys, signed URLs, env contents or
// tokens. The record is who asked for what shape of thing and whether it was
// allowed, which makes a pattern visible without the log becoming the leak.
func logBrokerAccess(c *gin.Context, operation, subject string, allowed bool, reason string) {
	ev := log.Info()
	if !allowed {
		ev = log.Warn()
	}
	ev.
		Str("event", "broker_access").
		Str("operation", operation).
		Str("subject", subject).
		Str("node_id", brokerCaller(c)).
		Bool("allowed", allowed).
		Str("client_ip", c.ClientIP()).
		Str("request_id", c.GetHeader("X-Request-Id")).
		Str("reason", reason).
		Msg("internal broker access")
}

// brokerCaller names the node whose own credential admitted the call, or "fleet_token".
func brokerCaller(c *gin.Context) string {
	if id, ok := middleware.AuthenticatedNodeID(c); ok {
		return id.String()
	}
	return "fleet_token"
}
