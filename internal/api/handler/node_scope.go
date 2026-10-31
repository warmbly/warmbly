package handler

import (
	"context"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/api/middleware"
)

// nodeScopeRefusals remembers which node and route pairs were already logged, so each is said once per process.
var nodeScopeRefusals sync.Map

// nodeInScope answers whether the node that authenticated this call may act on what it names.
// A fleet-token caller is not bound. A false has already answered the caller with refusal.
func (h *Handler) nodeInScope(c *gin.Context, route string, refusal int, allowed func(ctx context.Context, node uuid.UUID) (bool, error)) bool {
	node, ok := middleware.AuthenticatedNodeID(c)
	if !ok || h.NodeScope == nil {
		return true
	}
	ok, err := allowed(c.Request.Context(), node)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "node assignment could not be checked"})
		return false
	}
	if ok {
		return true
	}
	if _, seen := nodeScopeRefusals.LoadOrStore(node.String()+" "+route, struct{}{}); !seen {
		log.Warn().Str("event", "node_scope_refused").Str("node_id", node.String()).Str("route", route).
			Msg("fleet: a worker node named something it is not assigned; refused")
	}
	if refusal == http.StatusNotFound {
		c.AbortWithStatusJSON(refusal, gin.H{"error": "not found"})
	} else {
		c.AbortWithStatusJSON(refusal, gin.H{"error": "not assigned to this node"})
	}
	return false
}

// nodeMailbox binds a call naming a mailbox (and the user it belongs to, when given) to the node's assignment.
func (h *Handler) nodeMailbox(c *gin.Context, route string, refusal int, user, mailbox uuid.UUID) bool {
	return h.nodeInScope(c, route, refusal, func(ctx context.Context, node uuid.UUID) (bool, error) {
		return h.NodeScope.Mailbox(ctx, node, user, mailbox)
	})
}

// nodeOrganization binds a call naming an organization to the node's assignment.
func (h *Handler) nodeOrganization(c *gin.Context, route string, org uuid.UUID) bool {
	return h.nodeInScope(c, route, http.StatusNotFound, func(ctx context.Context, node uuid.UUID) (bool, error) {
		return h.NodeScope.Organization(ctx, node, org)
	})
}

// parsedOrNil reads an id from a request field; anything unreadable is uuid.Nil, which no node is assigned.
func parsedOrNil(raw string) uuid.UUID {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// mapScope reads the user and mailbox a message-map call names; an unreadable user names no mailbox.
func mapScope(rawUser, rawMailbox string) (uuid.UUID, uuid.UUID) {
	user := parsedOrNil(rawUser)
	if user == uuid.Nil {
		return uuid.Nil, uuid.Nil
	}
	return user, parsedOrNil(rawMailbox)
}

// nodeWorkerMailbox binds a call naming both a worker and one of its mailboxes.
func (h *Handler) nodeWorkerMailbox(c *gin.Context, route string, refusal int, worker, mailbox uuid.UUID) bool {
	return h.nodeInScope(c, route, refusal, func(ctx context.Context, node uuid.UUID) (bool, error) {
		ok, err := h.NodeScope.Worker(ctx, node, worker)
		if err != nil || !ok {
			return ok, err
		}
		return h.NodeScope.Mailbox(ctx, node, uuid.Nil, mailbox)
	})
}
