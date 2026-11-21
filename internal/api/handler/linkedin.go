package handler

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/linkedin"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

var errLinkedInOff = func() *errx.Error {
	e := errx.NewWithIdentifier(errx.ServiceUnavailable, linkedin.CodeUnavailable, "LinkedIn is not available on this instance.")
	e.Public = true
	return e
}()

// linkedInError answers a LinkedIn failure in Warmbly's error shape; payment failures take the store's codes.
func linkedInError(c *gin.Context, err error) {
	rid := c.GetString("request_id")
	var (
		checkout *linkedin.CheckoutRequiredError
		accept   *linkedin.AcceptRequiredError
		fields   *linkedin.FieldsError
	)
	switch {
	case errors.As(err, &checkout):
		c.JSON(http.StatusPaymentRequired, gin.H{
			"error": "Payment Required", "code": linkedin.CodeCheckoutRequired, "request_id": rid,
			"message": "Your GraphKit plan has no free LinkedIn seat. Add one in GraphKit, then connect again.",
			"details": gin.H{"url": checkout.URL},
		})
	case errors.As(err, &accept):
		c.JSON(http.StatusConflict, gin.H{
			"error": "Conflict", "code": linkedin.CodeAcceptRequired, "request_id": rid,
			"message": "Connecting this account is charged. Accept the charge to continue.",
			"details": gin.H{"quote": accept.Quote},
		})
	case errors.As(err, &fields):
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Bad Request", "code": linkedin.CodeInvalidRequest, "request_id": rid,
			"message": fields.Message, "details": gin.H{"fields": fields.Fields},
		})
	case isInsufficientCredits(err), isCapExceeded(err):
		mapCreditError(c, err)
	default:
		storeError(c, err)
	}
}

// linkedInCaller is the workspace and member of a /v1/linkedin call, refused when the add-on is not wired.
func (h *Handler) linkedInCaller(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	if h.LinkedInService == nil {
		errx.Handle(c, errLinkedInOff)
		return uuid.Nil, uuid.Nil, false
	}
	return h.storeCaller(c)
}

func linkedInRequester(c *gin.Context, userID uuid.UUID) linkedin.Requester {
	ua := c.Request.UserAgent()
	if len(ua) > 512 {
		ua = ua[:512]
	}
	return linkedin.Requester{UserID: userID, IP: c.ClientIP(), UserAgent: ua}
}

// linkedInQueryID reads an optional id from the query; ok is false once the bad request was answered.
func linkedInQueryID(c *gin.Context, name string) (*uuid.UUID, bool) {
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

func bindLinkedIn(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return false
	}
	return true
}

// GetLinkedInStatus is GET /v1/linkedin/status.
func (h *Handler) GetLinkedInStatus(c *gin.Context) {
	if h.LinkedInService == nil {
		c.JSON(http.StatusOK, linkedin.Status{Mode: models.LinkedInModeNone, Price: linkedin.Price{Tier: linkedin.TierStandard}})
		return
	}
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	st, err := h.LinkedInService.Status(c.Request.Context(), orgID)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, st)
}

// StartLinkedInLink is POST /v1/linkedin/link.
func (h *Handler) StartLinkedInLink(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	out, err := h.LinkedInService.StartLink(c.Request.Context(), orgID)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// CompleteLinkedInLink is POST /v1/linkedin/link/:session/complete.
func (h *Handler) CompleteLinkedInLink(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	st, err := h.LinkedInService.CompleteLink(c.Request.Context(), orgID, c.Param("session"))
	if err != nil {
		linkedInError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionConnect, models.AuditEntityLinkedInLink, &orgID, map[string]string{"mode": st.Mode}, nil)
	c.JSON(http.StatusOK, st)
}

// UnlinkLinkedIn is DELETE /v1/linkedin/link.
func (h *Handler) UnlinkLinkedIn(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	if err := h.LinkedInService.Unlink(c.Request.Context(), orgID); err != nil {
		linkedInError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionDisconnect, models.AuditEntityLinkedInLink, &orgID, nil, nil)
	c.Status(http.StatusNoContent)
}

// GetLinkedInConnectQuote is GET /v1/linkedin/connect-quote.
func (h *Handler) GetLinkedInConnectQuote(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	accountID, ok := linkedInQueryID(c, "account_id")
	if !ok {
		return
	}
	q, err := h.LinkedInService.Quote(c.Request.Context(), orgID, accountID)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, q)
}

// CreateLinkedInConnection is POST /v1/linkedin/connections.
func (h *Handler) CreateLinkedInConnection(c *gin.Context) {
	orgID, userID, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	var in linkedin.ConnectRequest
	if !bindLinkedIn(c, &in) {
		return
	}
	out, err := h.LinkedInService.CreateConnection(c.Request.Context(), orgID, linkedInRequester(c, userID), in)
	if err != nil {
		linkedInError(c, err)
		return
	}
	meta := map[string]string{"session_id": out.Session.ID, "kind": out.Session.Kind, "charged_micros": strconv.FormatInt(out.ChargedMicros, 10)}
	h.auditOrg(c, models.AuditActionConnect, models.AuditEntityLinkedInAccount, in.AccountID, nil, meta)
	c.JSON(http.StatusCreated, out)
}

// GetLinkedInConnection is GET /v1/linkedin/connections/:session.
func (h *Handler) GetLinkedInConnection(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	out, err := h.LinkedInService.GetConnection(c.Request.Context(), orgID, c.Param("session"))
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// LinkedInConnectionSignIn is POST /v1/linkedin/connections/:session/sign-in. The credentials go to GraphKit once and are never kept.
func (h *Handler) LinkedInConnectionSignIn(c *gin.Context) {
	orgID, userID, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	var in linkedin.SignInRequest
	if !bindLinkedIn(c, &in) {
		return
	}
	out, err := h.LinkedInService.SignIn(c.Request.Context(), orgID, c.Param("session"), linkedInRequester(c, userID), in)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// LinkedInConnectionAnswer is POST /v1/linkedin/connections/:session/answer.
func (h *Handler) LinkedInConnectionAnswer(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	var in graphkit.CheckpointAnswer
	if !bindLinkedIn(c, &in) {
		return
	}
	out, err := h.LinkedInService.Answer(c.Request.Context(), orgID, c.Param("session"), in)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// LinkedInConnectionMethod is POST /v1/linkedin/connections/:session/method.
func (h *Handler) LinkedInConnectionMethod(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	var in struct {
		Challenge string `json:"challenge"`
	}
	if !bindLinkedIn(c, &in) {
		return
	}
	out, err := h.LinkedInService.ChooseMethod(c.Request.Context(), orgID, c.Param("session"), in.Challenge)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// LinkedInConnectionResend is POST /v1/linkedin/connections/:session/resend.
func (h *Handler) LinkedInConnectionResend(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	out, err := h.LinkedInService.Resend(c.Request.Context(), orgID, c.Param("session"))
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// CancelLinkedInConnection is DELETE /v1/linkedin/connections/:session.
func (h *Handler) CancelLinkedInConnection(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	if err := h.LinkedInService.CancelConnection(c.Request.Context(), orgID, c.Param("session")); err != nil {
		linkedInError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListLinkedInAccounts is GET /v1/linkedin/accounts.
func (h *Handler) ListLinkedInAccounts(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	limit, ok := storeLimit(c)
	if !ok {
		return
	}
	page, err := h.LinkedInService.ListAccounts(c.Request.Context(), orgID, c.Query("cursor"), limit)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// GetLinkedInAccount is GET /v1/linkedin/accounts/:id.
func (h *Handler) GetLinkedInAccount(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	a, err := h.LinkedInService.GetAccount(c.Request.Context(), orgID, id)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, a)
}

// PatchLinkedInAccount is PATCH /v1/linkedin/accounts/:id.
func (h *Handler) PatchLinkedInAccount(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	var in linkedin.AccountPatch
	if !bindLinkedIn(c, &in) {
		return
	}
	a, err := h.LinkedInService.UpdateAccount(c.Request.Context(), orgID, id, in)
	if err != nil {
		linkedInError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLinkedInAccount, &a.ID, nil, map[string]string{"name": a.DisplayName})
	c.JSON(http.StatusOK, a)
}

// LinkedInAccountAction is POST /v1/linkedin/accounts/:id/{pause,resume,resync}.
func (h *Handler) LinkedInAccountAction(action string) gin.HandlerFunc {
	audit := map[string]models.AuditAction{linkedin.ActionPause: models.AuditActionPause, linkedin.ActionResume: models.AuditActionResume,
		linkedin.ActionResync: models.AuditActionUpdate}[action]
	return func(c *gin.Context) {
		orgID, _, ok := h.linkedInCaller(c)
		if !ok {
			return
		}
		id, ok := storeID(c)
		if !ok {
			return
		}
		a, err := h.LinkedInService.AccountAction(c.Request.Context(), orgID, id, action)
		if err != nil {
			linkedInError(c, err)
			return
		}
		h.auditOrg(c, audit, models.AuditEntityLinkedInAccount, &a.ID, nil, map[string]string{"name": a.DisplayName, "action": action})
		c.JSON(http.StatusOK, a)
	}
}

// RemoveLinkedInAccount is DELETE /v1/linkedin/accounts/:id.
func (h *Handler) RemoveLinkedInAccount(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	a, err := h.LinkedInService.RemoveAccount(c.Request.Context(), orgID, id)
	if err != nil {
		linkedInError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionRemove, models.AuditEntityLinkedInAccount, &a.ID, nil, map[string]string{"name": a.DisplayName})
	c.Status(http.StatusNoContent)
}

// LinkedInSearchCatalog is GET /v1/linkedin/search/catalog.
func (h *Handler) LinkedInSearchCatalog(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	raw, err := h.LinkedInService.Catalog(c.Request.Context(), orgID)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
}

// LinkedInSearch is POST /v1/linkedin/search.
func (h *Handler) LinkedInSearch(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	var in linkedin.SearchRequest
	if !bindLinkedIn(c, &in) {
		return
	}
	page, err := h.LinkedInService.Search(c.Request.Context(), orgID, in)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// LinkedInTypeahead is GET /v1/linkedin/search/typeahead.
func (h *Handler) LinkedInTypeahead(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	accountID, ok := linkedInQueryID(c, "account_id")
	if !ok {
		return
	}
	items, err := h.LinkedInService.Typeahead(c.Request.Context(), orgID, c.Query("source"), c.Query("filter"), c.Query("q"), accountID)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

// LinkedInParseURL is POST /v1/linkedin/search/parse-url.
func (h *Handler) LinkedInParseURL(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	var in struct {
		URL string `json:"url"`
	}
	if !bindLinkedIn(c, &in) {
		return
	}
	raw, err := h.LinkedInService.ParseURL(c.Request.Context(), orgID, in.URL)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
}

// LinkedInSearchAI is POST /v1/linkedin/search/ai, charged to the AI balance at GraphKit's cost.
func (h *Handler) LinkedInSearchAI(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	var in linkedin.AIRequest
	if !bindLinkedIn(c, &in) {
		return
	}
	res, err := h.LinkedInService.BuildQuery(c.Request.Context(), orgID, in, strings.TrimSpace(c.GetHeader("Idempotency-Key")))
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// LinkedInSearchImport is POST /v1/linkedin/search/import.
func (h *Handler) LinkedInSearchImport(c *gin.Context) {
	orgID, userID, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	var in linkedin.ImportRequest
	if !bindLinkedIn(c, &in) {
		return
	}
	if in.FindEmails {
		im, err := h.LinkedInService.StartImport(c.Request.Context(), orgID, userID, in)
		if err != nil {
			linkedInError(c, err)
			return
		}
		h.auditOrg(c, models.AuditActionImport, models.AuditEntityContact, nil, nil, map[string]string{
			"source": "linkedin", "import_id": im.ID.String(), "find_emails": "true"})
		c.JSON(http.StatusAccepted, gin.H{"import_id": im.ID})
		return
	}
	res, err := h.LinkedInService.Import(c.Request.Context(), orgID, userID, in)
	if err != nil {
		linkedInError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionImport, models.AuditEntityContact, nil, nil, map[string]string{
		"source": "linkedin", "created": strconv.Itoa(res.Created), "updated": strconv.Itoa(res.Updated)})
	c.JSON(http.StatusOK, res)
}

// ListLinkedInSavedSearches is GET /v1/linkedin/saved-searches.
func (h *Handler) ListLinkedInSavedSearches(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	limit, ok := storeLimit(c)
	if !ok {
		return
	}
	page, err := h.LinkedInService.ListSavedSearches(c.Request.Context(), orgID, c.Query("cursor"), limit)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// CreateLinkedInSavedSearch is POST /v1/linkedin/saved-searches.
func (h *Handler) CreateLinkedInSavedSearch(c *gin.Context) {
	orgID, userID, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	var in linkedin.SavedSearchInput
	if !bindLinkedIn(c, &in) {
		return
	}
	row, err := h.LinkedInService.CreateSavedSearch(c.Request.Context(), orgID, userID, in)
	if err != nil {
		linkedInError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityLinkedInSavedSearch, &row.ID, nil, map[string]string{"name": row.Name})
	c.JSON(http.StatusCreated, row)
}

// PatchLinkedInSavedSearch is PATCH /v1/linkedin/saved-searches/:id.
func (h *Handler) PatchLinkedInSavedSearch(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	var in linkedin.SavedSearchInput
	if !bindLinkedIn(c, &in) {
		return
	}
	row, err := h.LinkedInService.UpdateSavedSearch(c.Request.Context(), orgID, id, in)
	if err != nil {
		linkedInError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLinkedInSavedSearch, &row.ID, nil, map[string]string{"name": row.Name})
	c.JSON(http.StatusOK, row)
}

// DeleteLinkedInSavedSearch is DELETE /v1/linkedin/saved-searches/:id.
func (h *Handler) DeleteLinkedInSavedSearch(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	if err := h.LinkedInService.DeleteSavedSearch(c.Request.Context(), orgID, id); err != nil {
		linkedInError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityLinkedInSavedSearch, &id, nil, nil)
	c.Status(http.StatusNoContent)
}

// GetLinkedInProfile is GET /v1/linkedin/profiles.
func (h *Handler) GetLinkedInProfile(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	accountID, ok := linkedInQueryID(c, "account_id")
	if !ok {
		return
	}
	p, err := h.LinkedInService.Profile(c.Request.Context(), orgID, c.Query("identifier"), accountID)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// ListLinkedInConversations is GET /v1/linkedin/conversations.
func (h *Handler) ListLinkedInConversations(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	limit, ok := storeLimit(c)
	if !ok {
		return
	}
	accountID, ok := linkedInQueryID(c, "account_id")
	if !ok {
		return
	}
	contactID, ok := linkedInQueryID(c, "contact_id")
	if !ok {
		return
	}
	unread := c.Query("unread") == "true" || c.Query("unread") == "1"
	page, err := h.LinkedInService.ListConversations(c.Request.Context(), orgID, linkedin.ConversationFilter{
		AccountID: accountID, ContactID: contactID, UnreadOnly: unread, Cursor: c.Query("cursor"), Limit: limit})
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// GetLinkedInConversation is GET /v1/linkedin/conversations/:id.
func (h *Handler) GetLinkedInConversation(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	conv, err := h.LinkedInService.GetConversation(c.Request.Context(), orgID, id)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, conv)
}

// ListLinkedInMessages is GET /v1/linkedin/conversations/:id/messages.
func (h *Handler) ListLinkedInMessages(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
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
	page, err := h.LinkedInService.ListMessages(c.Request.Context(), orgID, id, c.Query("cursor"), limit)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// SendLinkedInMessage is POST /v1/linkedin/conversations/:id/messages.
func (h *Handler) SendLinkedInMessage(c *gin.Context) {
	orgID, userID, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if !bindLinkedIn(c, &in) {
		return
	}
	m, err := h.LinkedInService.Reply(c.Request.Context(), orgID, userID, id, in.Body)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusCreated, m)
}

// MarkLinkedInConversationRead is POST /v1/linkedin/conversations/:id/read.
func (h *Handler) MarkLinkedInConversationRead(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	if err := h.LinkedInService.MarkRead(c.Request.Context(), orgID, id); err != nil {
		linkedInError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

var linkedInSandboxPage = template.Must(template.New("link").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Link a GraphKit workspace</title></head>
<body><h1>Link a GraphKit workspace</h1>
<p>This is the development simulator's stand-in for GraphKit's approval page. Linking connects "Sample workspace" and the LinkedIn account it holds.</p>
<form method="post"><button name="decision" value="link">Link</button> <button name="decision" value="cancel">Cancel</button></form>
</body></html>`))

// LinkedInSandboxLink is GET and POST /v1/linkedin/sandbox/link/:session: the simulator's stand-in for GraphKit's
// link approval page. It exists only on a development instance running the simulated GraphKit.
func (h *Handler) LinkedInSandboxLink(c *gin.Context) {
	if h.LinkedInService == nil {
		c.Status(http.StatusNotFound)
		return
	}
	fake := h.LinkedInService.Sandbox()
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
		_ = linkedInSandboxPage.Execute(c.Writer, nil)
		return
	}
	to, err := fake.DecideLink(id, c.PostForm("decision") == "link")
	if err != nil {
		c.String(http.StatusConflict, "This link session could not be decided.")
		return
	}
	c.Redirect(http.StatusSeeOther, to)
}
