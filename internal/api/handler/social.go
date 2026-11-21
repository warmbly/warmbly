package handler

import (
	"errors"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/social"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

var errSocialOff = func() *errx.Error {
	e := errx.NewWithIdentifier(errx.ServiceUnavailable, social.CodeUnavailable, "Social accounts are not available on this instance.")
	e.Public = true
	return e
}()

// socialError answers a social failure in Warmbly's error shape; payment failures take the store's codes.
func socialError(c *gin.Context, err error) {
	rid := c.GetString("request_id")
	var (
		checkout *social.CheckoutRequiredError
		accept   *social.AcceptRequiredError
		fields   *social.FieldsError
		detail   *social.DetailError
	)
	switch {
	case errors.As(err, &checkout):
		c.JSON(http.StatusPaymentRequired, gin.H{
			"error": "Payment Required", "code": social.CodeCheckoutRequired, "request_id": rid,
			"message": "Your GraphKit plan has no free seat for this account. Add one in GraphKit, then connect again.",
			"details": gin.H{"url": checkout.URL},
		})
	case errors.As(err, &accept):
		c.JSON(http.StatusConflict, gin.H{
			"error": "Conflict", "code": social.CodeAcceptRequired, "request_id": rid,
			"message": "Connecting this account is charged. Accept the charge to continue.",
			"details": gin.H{"quote": accept.Quote},
		})
	case errors.As(err, &fields):
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Bad Request", "code": social.CodeInvalidRequest, "request_id": rid,
			"message": fields.Message, "details": gin.H{"fields": fields.Fields},
		})
	case errors.As(err, &detail):
		c.JSON(detail.Status, gin.H{
			"error": http.StatusText(detail.Status), "code": detail.Code, "request_id": rid,
			"message": detail.Message, "details": detail.Details,
		})
	case isInsufficientCredits(err), isCapExceeded(err):
		mapCreditError(c, err)
	default:
		storeError(c, err)
	}
}

// socialCaller is the workspace and member of a /v1/social call, refused when the add-on is not wired.
func (h *Handler) socialCaller(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	if h.SocialService == nil {
		errx.Handle(c, errSocialOff)
		return uuid.Nil, uuid.Nil, false
	}
	return h.storeCaller(c)
}

func socialRequester(c *gin.Context, userID uuid.UUID) social.Requester {
	ua := c.Request.UserAgent()
	if len(ua) > 512 {
		ua = ua[:512]
	}
	return social.Requester{UserID: userID, IP: c.ClientIP(), UserAgent: ua}
}

// socialQueryID reads an optional id from the query; ok is false once the bad request was answered.
func socialQueryID(c *gin.Context, name string) (*uuid.UUID, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_"+name, name+" must be a UUID."))
		return nil, false
	}
	return &id, true
}

func bindSocial(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return false
	}
	return true
}

// GetSocialStatus is GET /v1/social/status.
func (h *Handler) GetSocialStatus(c *gin.Context) {
	if h.SocialService == nil {
		c.JSON(http.StatusOK, social.Status{Mode: models.SocialModeNone, Price: social.Price{Tier: social.TierStandard}})
		return
	}
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	st, err := h.SocialService.Status(c.Request.Context(), orgID)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, st)
}

// StartSocialLink is POST /v1/social/link.
func (h *Handler) StartSocialLink(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	out, err := h.SocialService.StartLink(c.Request.Context(), orgID)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// CompleteSocialLink is POST /v1/social/link/:session/complete.
func (h *Handler) CompleteSocialLink(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	st, err := h.SocialService.CompleteLink(c.Request.Context(), orgID, c.Param("session"))
	if err != nil {
		socialError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionConnect, models.AuditEntitySocialLink, &orgID, map[string]string{"mode": st.Mode}, nil)
	c.JSON(http.StatusOK, st)
}

// UnlinkSocial is DELETE /v1/social/link.
func (h *Handler) UnlinkSocial(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	if err := h.SocialService.Unlink(c.Request.Context(), orgID); err != nil {
		socialError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionDisconnect, models.AuditEntitySocialLink, &orgID, nil, nil)
	c.Status(http.StatusNoContent)
}

// GetSocialConnectQuote is GET /v1/social/connect-quote.
func (h *Handler) GetSocialConnectQuote(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	accountID, ok := socialQueryID(c, "account_id")
	if !ok {
		return
	}
	q, err := h.SocialService.Quote(c.Request.Context(), orgID, accountID)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, q)
}

// CreateSocialConnection is POST /v1/social/connections.
func (h *Handler) CreateSocialConnection(c *gin.Context) {
	orgID, userID, ok := h.socialCaller(c)
	if !ok {
		return
	}
	var in social.ConnectRequest
	if !bindSocial(c, &in) {
		return
	}
	out, err := h.SocialService.CreateConnection(c.Request.Context(), orgID, socialRequester(c, userID), in)
	if err != nil {
		socialError(c, err)
		return
	}
	meta := map[string]string{"session_id": out.Session.ID, "kind": out.Session.Kind, "charged_micros": strconv.FormatInt(out.ChargedMicros, 10)}
	h.auditOrg(c, models.AuditActionConnect, models.AuditEntitySocialAccount, in.AccountID, nil, meta)
	c.JSON(http.StatusCreated, out)
}

// GetSocialConnection is GET /v1/social/connections/:session.
func (h *Handler) GetSocialConnection(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	out, err := h.SocialService.GetConnection(c.Request.Context(), orgID, c.Param("session"))
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// SocialConnectionSignIn is POST /v1/social/connections/:session/sign-in. The credentials go to GraphKit once and are never kept.
func (h *Handler) SocialConnectionSignIn(c *gin.Context) {
	orgID, userID, ok := h.socialCaller(c)
	if !ok {
		return
	}
	var in social.SignInRequest
	if !bindSocial(c, &in) {
		return
	}
	out, err := h.SocialService.SignIn(c.Request.Context(), orgID, c.Param("session"), socialRequester(c, userID), in)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// SocialConnectionAnswer is POST /v1/social/connections/:session/answer.
func (h *Handler) SocialConnectionAnswer(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	var in graphkit.CheckpointAnswer
	if !bindSocial(c, &in) {
		return
	}
	out, err := h.SocialService.Answer(c.Request.Context(), orgID, c.Param("session"), in)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// SocialConnectionMethod is POST /v1/social/connections/:session/method.
func (h *Handler) SocialConnectionMethod(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	var in struct {
		Challenge string `json:"challenge"`
	}
	if !bindSocial(c, &in) {
		return
	}
	out, err := h.SocialService.ChooseMethod(c.Request.Context(), orgID, c.Param("session"), in.Challenge)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// SocialConnectionResend is POST /v1/social/connections/:session/resend.
func (h *Handler) SocialConnectionResend(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	out, err := h.SocialService.Resend(c.Request.Context(), orgID, c.Param("session"))
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// CancelSocialConnection is DELETE /v1/social/connections/:session.
func (h *Handler) CancelSocialConnection(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	if err := h.SocialService.CancelConnection(c.Request.Context(), orgID, c.Param("session")); err != nil {
		socialError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListSocialAccounts is GET /v1/social/accounts.
func (h *Handler) ListSocialAccounts(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	limit, ok := storeLimit(c)
	if !ok {
		return
	}
	page, err := h.SocialService.ListAccounts(c.Request.Context(), orgID, c.Query("cursor"), limit)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// GetSocialAccount is GET /v1/social/accounts/:id.
func (h *Handler) GetSocialAccount(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	a, err := h.SocialService.GetAccount(c.Request.Context(), orgID, id)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, a)
}

// PatchSocialAccount is PATCH /v1/social/accounts/:id.
func (h *Handler) PatchSocialAccount(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	var in social.AccountPatch
	if !bindSocial(c, &in) {
		return
	}
	a, err := h.SocialService.UpdateAccount(c.Request.Context(), orgID, id, in)
	if err != nil {
		socialError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySocialAccount, &a.ID, nil, map[string]string{"name": a.DisplayName})
	c.JSON(http.StatusOK, a)
}

// SocialAccountAction is POST /v1/social/accounts/:id/{pause,resume,resync}.
func (h *Handler) SocialAccountAction(action string) gin.HandlerFunc {
	audit := map[string]models.AuditAction{social.ActionPause: models.AuditActionPause, social.ActionResume: models.AuditActionResume,
		social.ActionResync: models.AuditActionUpdate}[action]
	return func(c *gin.Context) {
		orgID, _, ok := h.socialCaller(c)
		if !ok {
			return
		}
		id, ok := storeID(c)
		if !ok {
			return
		}
		a, err := h.SocialService.AccountAction(c.Request.Context(), orgID, id, action)
		if err != nil {
			socialError(c, err)
			return
		}
		h.auditOrg(c, audit, models.AuditEntitySocialAccount, &a.ID, nil, map[string]string{"name": a.DisplayName, "action": action})
		c.JSON(http.StatusOK, a)
	}
}

// RemoveSocialAccount is DELETE /v1/social/accounts/:id.
func (h *Handler) RemoveSocialAccount(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	a, err := h.SocialService.RemoveAccount(c.Request.Context(), orgID, id)
	if err != nil {
		socialError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionRemove, models.AuditEntitySocialAccount, &a.ID, nil, map[string]string{"name": a.DisplayName})
	c.Status(http.StatusNoContent)
}

// SocialSearchCatalog is GET /v1/social/search/catalog.
func (h *Handler) SocialSearchCatalog(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	raw, err := h.SocialService.Catalog(c.Request.Context(), orgID)
	if err != nil {
		socialError(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
}

// SocialSearch is POST /v1/social/search.
func (h *Handler) SocialSearch(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	var in social.SearchRequest
	if !bindSocial(c, &in) {
		return
	}
	page, err := h.SocialService.Search(c.Request.Context(), orgID, in)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// SocialTypeahead is GET /v1/social/search/typeahead.
func (h *Handler) SocialTypeahead(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	accountID, ok := socialQueryID(c, "account_id")
	if !ok {
		return
	}
	items, err := h.SocialService.Typeahead(c.Request.Context(), orgID, c.Query("source"), c.Query("filter"), c.Query("q"), accountID)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

// SocialParseURL is POST /v1/social/search/parse-url.
func (h *Handler) SocialParseURL(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	var in struct {
		URL string `json:"url"`
	}
	if !bindSocial(c, &in) {
		return
	}
	raw, err := h.SocialService.ParseURL(c.Request.Context(), orgID, in.URL)
	if err != nil {
		socialError(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
}

// SocialSearchAI is POST /v1/social/search/ai, charged to the AI balance at GraphKit's cost.
func (h *Handler) SocialSearchAI(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	var in social.AIRequest
	if !bindSocial(c, &in) {
		return
	}
	res, err := h.SocialService.BuildQuery(c.Request.Context(), orgID, in, strings.TrimSpace(c.GetHeader("Idempotency-Key")))
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// SocialSearchImport is POST /v1/social/search/import.
func (h *Handler) SocialSearchImport(c *gin.Context) {
	orgID, userID, ok := h.socialCaller(c)
	if !ok {
		return
	}
	var in social.ImportRequest
	if !bindSocial(c, &in) {
		return
	}
	if in.FindEmails {
		im, err := h.SocialService.StartImport(c.Request.Context(), orgID, userID, in)
		if err != nil {
			socialError(c, err)
			return
		}
		h.auditOrg(c, models.AuditActionImport, models.AuditEntityContact, nil, nil, map[string]string{
			"source": "linkedin", "import_id": im.ID.String(), "find_emails": "true"})
		c.JSON(http.StatusAccepted, gin.H{"import_id": im.ID})
		return
	}
	res, err := h.SocialService.Import(c.Request.Context(), orgID, userID, in)
	if err != nil {
		socialError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionImport, models.AuditEntityContact, nil, nil, map[string]string{
		"source": "linkedin", "created": strconv.Itoa(res.Created), "updated": strconv.Itoa(res.Updated)})
	c.JSON(http.StatusOK, res)
}

// ListSocialSavedSearches is GET /v1/social/saved-searches.
func (h *Handler) ListSocialSavedSearches(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	limit, ok := storeLimit(c)
	if !ok {
		return
	}
	page, err := h.SocialService.ListSavedSearches(c.Request.Context(), orgID, c.Query("cursor"), limit)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// CreateSocialSavedSearch is POST /v1/social/saved-searches.
func (h *Handler) CreateSocialSavedSearch(c *gin.Context) {
	orgID, userID, ok := h.socialCaller(c)
	if !ok {
		return
	}
	var in social.SavedSearchInput
	if !bindSocial(c, &in) {
		return
	}
	row, err := h.SocialService.CreateSavedSearch(c.Request.Context(), orgID, userID, in)
	if err != nil {
		socialError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityLinkedInSavedSearch, &row.ID, nil, map[string]string{"name": row.Name})
	c.JSON(http.StatusCreated, row)
}

// PatchSocialSavedSearch is PATCH /v1/social/saved-searches/:id.
func (h *Handler) PatchSocialSavedSearch(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	var in social.SavedSearchInput
	if !bindSocial(c, &in) {
		return
	}
	row, err := h.SocialService.UpdateSavedSearch(c.Request.Context(), orgID, id, in)
	if err != nil {
		socialError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLinkedInSavedSearch, &row.ID, nil, map[string]string{"name": row.Name})
	c.JSON(http.StatusOK, row)
}

// DeleteSocialSavedSearch is DELETE /v1/social/saved-searches/:id.
func (h *Handler) DeleteSocialSavedSearch(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	if err := h.SocialService.DeleteSavedSearch(c.Request.Context(), orgID, id); err != nil {
		socialError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityLinkedInSavedSearch, &id, nil, nil)
	c.Status(http.StatusNoContent)
}

// GetSocialProfile is GET /v1/social/profiles.
func (h *Handler) GetSocialProfile(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	accountID, ok := socialQueryID(c, "account_id")
	if !ok {
		return
	}
	recipient := c.Query("recipient")
	if recipient == "" {
		recipient = c.Query("identifier")
	}
	p, err := h.SocialService.Profile(c.Request.Context(), orgID, recipient, accountID)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// ListSocialConversations is GET /v1/social/conversations.
func (h *Handler) ListSocialConversations(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	limit, ok := storeLimit(c)
	if !ok {
		return
	}
	accountID, ok := socialQueryID(c, "account_id")
	if !ok {
		return
	}
	contactID, ok := socialQueryID(c, "contact_id")
	if !ok {
		return
	}
	channel := strings.TrimSpace(c.Query("channel"))
	if channel != "" && !models.IsSocialChannel(channel) {
		errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_channel", "channel must be one of linkedin, instagram, whatsapp, telegram or facebook."))
		return
	}
	unread := c.Query("unread") == "true" || c.Query("unread") == "1"
	page, err := h.SocialService.ListConversations(c.Request.Context(), orgID, social.ConversationFilter{
		AccountID: accountID, Channel: channel, ContactID: contactID, UnreadOnly: unread, Cursor: c.Query("cursor"), Limit: limit})
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// GetSocialConversation is GET /v1/social/conversations/:id.
func (h *Handler) GetSocialConversation(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	conv, err := h.SocialService.GetConversation(c.Request.Context(), orgID, id)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, conv)
}

// ListSocialMessages is GET /v1/social/conversations/:id/messages.
func (h *Handler) ListSocialMessages(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	limit, ok := storeLimit(c)
	if !ok {
		return
	}
	page, err := h.SocialService.ListMessages(c.Request.Context(), orgID, id, c.Query("cursor"), limit)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// SendSocialMessage is POST /v1/social/conversations/:id/messages.
func (h *Handler) SendSocialMessage(c *gin.Context) {
	orgID, userID, ok := h.socialCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	var in social.ReplyRequest
	if !bindSocial(c, &in) {
		return
	}
	m, err := h.SocialService.Reply(c.Request.Context(), orgID, userID, id, in)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusCreated, m)
}

// MarkSocialConversationRead is POST /v1/social/conversations/:id/read.
func (h *Handler) MarkSocialConversationRead(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	if err := h.SocialService.MarkRead(c.Request.Context(), orgID, id); err != nil {
		socialError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

var socialSandboxPage = template.Must(template.New("link").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Link a GraphKit workspace</title></head>
<body><h1>Link a GraphKit workspace</h1>
<p>This is the development simulator's stand-in for GraphKit's approval page. Linking connects "Sample workspace" and the social accounts it holds.</p>
<form method="post"><button name="decision" value="link">Link</button> <button name="decision" value="cancel">Cancel</button></form>
</body></html>`))

// SocialSandboxLink is GET and POST /v1/social/sandbox/link/:session: the simulator's stand-in for GraphKit's
// link approval page. It exists only on a development instance running the simulated GraphKit.
func (h *Handler) SocialSandboxLink(c *gin.Context) {
	if h.SocialService == nil {
		c.Status(http.StatusNotFound)
		return
	}
	fake := h.SocialService.Sandbox()
	if fake == nil {
		c.Status(http.StatusNotFound)
		return
	}
	id := c.Param("session")
	if _, ok := fake.LinkSessionReturn(id); !ok {
		c.String(http.StatusGone, "This link session is no longer pending.")
		return
	}
	if c.Request.Method == http.MethodGet {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(http.StatusOK)
		_ = socialSandboxPage.Execute(c.Writer, nil)
		return
	}
	to, err := fake.DecideLink(id, c.PostForm("decision") == "link")
	if err != nil {
		c.String(http.StatusConflict, "This link session could not be decided.")
		return
	}
	c.Redirect(http.StatusSeeOther, to)
}

// GetSocialChannels is GET /v1/social/channels: what each channel can do and its limits.
func (h *Handler) GetSocialChannels(c *gin.Context) {
	if h.SocialService == nil {
		c.JSON(http.StatusOK, social.BuiltinChannels())
		return
	}
	c.JSON(http.StatusOK, h.SocialService.Channels(c.Request.Context()))
}

// UploadSocialAudio is POST /v1/social/audio: a multipart recording and its duration_seconds, for voice notes.
func (h *Handler) UploadSocialAudio(c *gin.Context) {
	// The route skips the global 10MB cap; a recording may be 16 MiB plus the multipart envelope.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, graphkit.AudioMaxBytes+1<<20)
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, social.CodeInvalidRequest, "file is required."))
		return
	}
	if fh.Size <= 0 || fh.Size > graphkit.AudioMaxBytes {
		errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, social.CodeInvalidRequest, "The recording must be between 1 byte and 16 MiB."))
		return
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(c.PostForm("duration_seconds")))
	if err != nil {
		errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, social.CodeInvalidRequest, "duration_seconds is required."))
		return
	}
	src, err := fh.Open()
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	defer src.Close()
	data, err := io.ReadAll(io.LimitReader(src, graphkit.AudioMaxBytes+1))
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	ct := fh.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		ct = http.DetectContentType(data)
	}
	a, err := h.SocialService.UploadAudio(c.Request.Context(), orgID, social.AudioUpload{
		Filename: sanitizeFilename(fh.Filename), ContentType: ct, Data: data, DurationSeconds: seconds})
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusCreated, a)
}

// DeleteSocialAudio is DELETE /v1/social/audio/:audio_id; GraphKit refuses while a queued step still uses it.
func (h *Handler) DeleteSocialAudio(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	if err := h.SocialService.DeleteAudio(c.Request.Context(), orgID, c.Param("audio_id")); err != nil {
		socialError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
