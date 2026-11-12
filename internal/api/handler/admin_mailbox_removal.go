package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	adminRemovalMaxDomains = 20
	// One pass stays well inside a request timeout; the caller repeats until nothing remains.
	adminRemovalBatch   = 100
	adminRemovalMaxSkip = 1000
	// A preview deletes nothing, so it lists far more of the exact targets than a removing pass takes.
	adminRemovalPreview = 1000
)

// Shared mailbox providers: nobody owns these domains, so a domain-owner removal never names one.
var adminRemovalSharedDomains = map[string]struct{}{
	"gmail.com": {}, "googlemail.com": {}, "outlook.com": {}, "hotmail.com": {}, "live.com": {},
	"msn.com": {}, "yahoo.com": {}, "ymail.com": {}, "icloud.com": {}, "me.com": {}, "mac.com": {},
	"aol.com": {}, "proton.me": {}, "protonmail.com": {}, "gmx.com": {}, "gmx.net": {}, "zoho.com": {},
	"yandex.com": {}, "mail.com": {},
}

var errAdminRemovalDomains = errx.New(errx.BadRequest, "domains must name 1 to 20 domains such as example.com, and none may be a shared mailbox provider")

// AdminRemoveMailboxes is POST /admin/mailboxes/remove: deletes every mailbox on the named domains in any workspace,
// through the same path a workspace's own delete takes, so each worker drops the mailbox before its row goes.
func (h *Handler) AdminRemoveMailboxes(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var req models.AdminRemoveMailboxesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	domains, ok := adminRemovalDomains(req.Domains)
	if !ok {
		errx.JSON(c, errAdminRemovalDomains)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "a reason is required to remove mailboxes"))
		return
	}

	if len(req.Skip) > adminRemovalMaxSkip {
		errx.JSON(c, errx.New(errx.BadRequest, "skip may name at most 1000 mailboxes"))
		return
	}

	ctx := c.Request.Context()
	skip := req.Skip
	if skip == nil {
		skip = []uuid.UUID{}
	}
	limit := adminRemovalBatch
	if req.DryRun {
		limit = adminRemovalPreview
	}
	targets, total, xerr := h.AdminService.MailboxesOnDomains(ctx, domains, skip, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	result := models.AdminRemoveMailboxesResult{DryRun: req.DryRun, Matched: total, Mailboxes: targets}
	if result.Mailboxes == nil {
		result.Mailboxes = []models.AdminMailboxTarget{}
	}
	if req.DryRun {
		result.Remaining = total
		c.JSON(http.StatusOK, result)
		return
	}

	for i := range result.Mailboxes {
		t := &result.Mailboxes[i]
		if derr := h.EmailService.Delete(ctx, t.OrganizationID.String(), t.ID.String()); derr != nil {
			t.Error = derr.Message
			result.Failed++
			log.Warn().Str("email_account_id", t.ID.String()).Str("error", derr.Message).Msg("admin mailbox removal: delete failed")
			continue
		}
		t.Removed = true
		result.Removed++
		h.AdminService.LogAdminAction(ctx, *adminID, "remove_mailbox", "email_account", &t.ID, map[string]any{
			"organization_id": t.OrganizationID,
			"email":           t.Email,
			"reason":          reason,
		}, c.ClientIP(), c.GetHeader("User-Agent"))
	}
	result.Remaining = total - result.Removed
	c.JSON(http.StatusOK, result)
}

// adminRemovalDomains normalises the requested domains and refuses an empty, malformed or shared one.
func adminRemovalDomains(raw []string) ([]string, bool) {
	if len(raw) == 0 || len(raw) > adminRemovalMaxDomains {
		return nil, false
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, d := range raw {
		d = strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(d)), "@"), ".")
		if d == "" || strings.ContainsAny(d, "@%_ /\\,") || !strings.Contains(d, ".") ||
			strings.HasPrefix(d, ".") || strings.Contains(d, "..") {
			return nil, false
		}
		if _, shared := adminRemovalSharedDomains[d]; shared {
			return nil, false
		}
		if _, dup := seen[d]; dup {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	return out, true
}
