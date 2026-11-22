package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/partner"
	"github.com/warmbly/warmbly/internal/errx"
)

// The partner API (/v1/partner) is for resellers that create and run workspaces. Its errors use
// {"error": {"code", "message", "request_id"}}, and a request that names no live partner key gets 401.

const partnerMaxBody = 1 << 20

// partnerGenericCodes renames the generic class codes to the partner API's vocabulary.
var partnerGenericCodes = map[string]string{
	"bad_request":         "invalid_request",
	"unprocessable":       "invalid_request",
	"rate_limit_exceeded": "rate_limited",
	"internal_error":      "internal",
	"conflict":            "conflict",
	"service_unavailable": "unavailable",
}

func partnerError(c *gin.Context, xerr *errx.Error) {
	status := xerr.HTTPStatus()
	code := xerr.ResponseCode()
	if alias, ok := partnerGenericCodes[code]; ok {
		code = alias
	}
	if xerr.RetryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(xerr.RetryAfter))
	}
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{
		"code":       code,
		"message":    errx.ClientMessage(c, xerr),
		"request_id": c.GetString(middleware.RequestIDContextKey),
	}})
}

func partnerCaller(c *gin.Context) *partner.Caller { return partner.CallerFrom(c.Request.Context()) }

// PartnerAuth resolves the partner key, applies the partner's rate limit, and requires Idempotency-Key on writes.
func (h *Handler) PartnerAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.Partners == nil {
			partnerError(c, partner.ErrUnauthorized)
			return
		}
		raw, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok {
			partnerError(c, partner.ErrUnauthorized)
			return
		}
		caller, xerr := h.Partners.Authenticate(c.Request.Context(), strings.TrimSpace(raw))
		if xerr != nil {
			partnerError(c, xerr)
			return
		}
		if !h.Partners.Allow(c.Request.Context(), caller) {
			partnerError(c, &errx.Error{Code: errx.TooManyRequests, Identifier: "rate_limited", Message: "Too many requests for this partner.", RetryAfter: 1})
			return
		}
		c.Request = c.Request.WithContext(partner.WithCaller(c.Request.Context(), caller))
		c.Header("Cache-Control", "no-store")
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			c.Next()
			return
		}
		h.partnerIdempotent(c, caller)
	}
}

// partnerIdempotent runs a write once per Idempotency-Key, replaying its stored answer to a retry.
func (h *Handler) partnerIdempotent(c *gin.Context, caller *partner.Caller) {
	key := strings.TrimSpace(c.GetHeader(middleware.IdempotencyKeyHeader))
	if key == "" {
		partnerError(c, partner.ErrIdempotencyRequired)
		return
	}
	if !partner.ValidIdempotencyKey(key) {
		partnerError(c, partner.ErrIdempotencyInvalid)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, partnerMaxBody+1))
	if err != nil || len(body) > partnerMaxBody {
		partnerError(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_request", "The request body is too large or unreadable."))
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	claim, replay, xerr := h.Partners.BeginIdempotent(c.Request.Context(), caller, key, partner.RequestHash(c.Request.Method, c.Request.URL.Path, body))
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	if replay != nil {
		c.Header("Idempotent-Replayed", "true")
		c.Header(middleware.IdempotencyReplayedHeader, "true")
		ct := replay.ContentType
		if ct == "" {
			ct = "application/json; charset=utf-8"
		}
		if len(replay.Body) == 0 {
			c.AbortWithStatus(replay.Status)
			return
		}
		c.Data(replay.Status, ct, replay.Body)
		c.Abort()
		return
	}
	capture := &partnerCapture{ResponseWriter: c.Writer}
	c.Writer = capture
	finished := false
	defer func() {
		if !finished {
			h.Partners.FinishIdempotent(c.Request.Context(), caller, claim, http.StatusInternalServerError, "", nil)
		}
	}()
	c.Next()
	finished = true
	h.Partners.FinishIdempotent(c.Request.Context(), caller, claim, capture.Status(), capture.Header().Get("Content-Type"), capture.body.Bytes())
}

type partnerCapture struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *partnerCapture) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *partnerCapture) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

// bindPartnerJSON decodes a body, ignoring fields it does not know.
func bindPartnerJSON(c *gin.Context, dst any) bool {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, partnerMaxBody))
	if err == nil && len(bytes.TrimSpace(raw)) > 0 {
		err = json.Unmarshal(raw, dst)
	}
	if err != nil {
		var syntax *json.SyntaxError
		var typ *json.UnmarshalTypeError
		msg := "The request body is not valid JSON."
		if errors.As(err, &typ) {
			msg = "The field " + typ.Field + " has the wrong type."
		} else if !errors.As(err, &syntax) {
			msg = "The request body could not be read."
		}
		partnerError(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_request", msg))
		return false
	}
	return true
}

// PartnerUpsertOrganization is PUT /v1/partner/organizations/:external_id.
func (h *Handler) PartnerUpsertOrganization(c *gin.Context) {
	var in partner.OrganizationInput
	if !bindPartnerJSON(c, &in) {
		return
	}
	v, created, xerr := h.Partners.Upsert(c.Request.Context(), partnerCaller(c), c.Param("external_id"), in)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, v)
}

// PartnerGetOrganization is GET /v1/partner/organizations/:external_id.
func (h *Handler) PartnerGetOrganization(c *gin.Context) {
	v, xerr := h.Partners.Get(c.Request.Context(), partnerCaller(c), c.Param("external_id"))
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusOK, v)
}

// PartnerListOrganizations is GET /v1/partner/organizations.
func (h *Handler) PartnerListOrganizations(c *gin.Context) {
	limit, xerr := partner.ParseLimit(c.Query("limit"), 100)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	page, xerr := h.Partners.List(c.Request.Context(), partnerCaller(c), c.Query("cursor"), limit)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusOK, page)
}

// PartnerCancelOrganization is DELETE /v1/partner/organizations/:external_id.
func (h *Handler) PartnerCancelOrganization(c *gin.Context) {
	v, xerr := h.Partners.Cancel(c.Request.Context(), partnerCaller(c), c.Param("external_id"))
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusOK, v)
}

// PartnerCreateLoginLink is POST /v1/partner/organizations/:external_id/login-links.
func (h *Handler) PartnerCreateLoginLink(c *gin.Context) {
	var in partner.LoginInput
	if !bindPartnerJSON(c, &in) {
		return
	}
	link, xerr := h.Partners.IssueLoginLink(c.Request.Context(), partnerCaller(c), c.Param("external_id"), in)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, link)
}

// PartnerRemovePerson is DELETE /v1/partner/organizations/:external_id/people/:person_external_id.
func (h *Handler) PartnerRemovePerson(c *gin.Context) {
	if xerr := h.Partners.RemovePerson(c.Request.Context(), partnerCaller(c), c.Param("external_id"), c.Param("person_external_id")); xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.Status(http.StatusNoContent)
}

// PartnerCreateAPIKey is POST /v1/partner/organizations/:external_id/api-keys.
func (h *Handler) PartnerCreateAPIKey(c *gin.Context) {
	var in partner.APIKeyInput
	if !bindPartnerJSON(c, &in) {
		return
	}
	k, xerr := h.Partners.CreateAPIKey(c.Request.Context(), partnerCaller(c), c.Param("external_id"), in, publicAPIBaseURL(c)+"/v1")
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, k)
}

// PartnerRevokeAPIKey is DELETE /v1/partner/organizations/:external_id/api-keys/:id.
func (h *Handler) PartnerRevokeAPIKey(c *gin.Context) {
	if xerr := h.Partners.RevokeAPIKey(c.Request.Context(), partnerCaller(c), c.Param("external_id"), c.Param("id")); xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.Status(http.StatusNoContent)
}

// PartnerListEvents is GET /v1/partner/events?after=&limit=.
func (h *Handler) PartnerListEvents(c *gin.Context) {
	limit, xerr := partner.ParseLimit(c.DefaultQuery("limit", "100"), 100)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	page, xerr := h.Partners.Events(c.Request.Context(), partnerCaller(c), c.Query("after"), limit)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusOK, page)
}

// PartnerLoginExchange is POST /v1/auth/partner/exchange: a login link spent once for a session.
func (h *Handler) PartnerLoginExchange(c *gin.Context) {
	if h.Partners == nil {
		errx.JSON(c, partner.ErrLoginInvalid)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	res, xerr := h.Partners.ExchangeLogin(c.Request.Context(), req.Code, c.ClientIP(), c.Request.UserAgent())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, res)
}

// mountPartnerOrganizationRoutes mounts the mailbox half of the partner API; every route on org has a resolved partner caller.
func (h *Handler) mountPartnerOrganizationRoutes(org *gin.RouterGroup) {
	org.GET("/mailboxes", h.PartnerListMailboxes)
	org.PUT("/mailboxes/:partner_mailbox_id", h.PartnerAttachMailbox)
	org.DELETE("/mailboxes/:partner_mailbox_id", h.PartnerDetachMailbox)
	org.PUT("/enablements/:mailbox_id", h.PartnerSetEnablement)
	org.POST("/connect-links", h.PartnerCreateConnectLink)
	org.POST("/microsoft-consent-links", h.PartnerCreateMicrosoftConsentLink)
	org.GET("/balance", h.PartnerGetBalance)
	org.POST("/balance/credits", h.PartnerCreditBalance)
}

// MountPartnerAPI registers /v1/partner on v1. A request with no live partner key gets 401, so the
// API does nothing until an operator creates a partner.
func (h *Handler) MountPartnerAPI(v1 *gin.RouterGroup) {
	g := v1.Group("/partner")
	g.Use(h.PartnerAuth())
	g.GET("/organizations", h.PartnerListOrganizations)
	g.PUT("/organizations/:external_id", h.PartnerUpsertOrganization)
	g.GET("/organizations/:external_id", h.PartnerGetOrganization)
	g.DELETE("/organizations/:external_id", h.PartnerCancelOrganization)
	org := g.Group("/organizations/:external_id")
	org.POST("/login-links", h.PartnerCreateLoginLink)
	org.DELETE("/people/:person_external_id", h.PartnerRemovePerson)
	org.POST("/api-keys", h.PartnerCreateAPIKey)
	org.DELETE("/api-keys/:id", h.PartnerRevokeAPIKey)
	h.mountPartnerOrganizationRoutes(org)
	g.GET("/events", h.PartnerListEvents)
	g.GET("/usage", h.PartnerUsage)
	g.GET("/usage/mailbox-days", h.PartnerMailboxDays)
}
