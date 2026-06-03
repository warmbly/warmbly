package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// placementEntity tags placement actions in the admin audit trail.
const placementEntity models.AuditEntityType = "placement_test"

// --- DTOs --------------------------------------------------------------

type placementTestRow struct {
	ID              uuid.UUID  `json:"id"`
	OrganizationID  *uuid.UUID `json:"organization_id"`
	SenderAccountID uuid.UUID  `json:"sender_account_id"`
	Subject         string     `json:"subject"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	FinishedAt      *time.Time `json:"finished_at"`
}

func toPlacementTestRow(t repository.PlacementTest) placementTestRow {
	return placementTestRow{
		ID:              t.ID,
		OrganizationID:  t.OrganizationID,
		SenderAccountID: t.SenderAccountID,
		Subject:         t.Subject,
		Status:          t.Status,
		CreatedAt:       t.CreatedAt,
		FinishedAt:      t.FinishedAt,
	}
}

type placementResultRow struct {
	SeedAccountID uuid.UUID  `json:"seed_account_id"`
	Provider      string     `json:"provider"`
	Folder        string     `json:"folder"`
	DetectedAt    *time.Time `json:"detected_at"`
	RawFlags      string     `json:"raw_flags"`
}

// providerRollup aggregates per-provider folder counts for a test.
type providerRollup struct {
	Provider   string `json:"provider"`
	Inbox      int    `json:"inbox"`
	Promotions int    `json:"promotions"`
	Spam       int    `json:"spam"`
	Other      int    `json:"other"`
	Pending    int    `json:"pending"`
	Total      int    `json:"total"`
}

type seedAccountRow struct {
	ID       uuid.UUID  `json:"id"`
	Email    string     `json:"email"`
	Name     string     `json:"name"`
	Provider string     `json:"provider"`
	Status   string     `json:"status"`
	WorkerID *uuid.UUID `json:"worker_id"`
	IsSeed   bool       `json:"is_seed"`
}

func toSeedRow(s repository.SeedAccount) seedAccountRow {
	return seedAccountRow{
		ID:       s.ID,
		Email:    s.Email,
		Name:     s.Name,
		Provider: s.Provider,
		Status:   s.Status,
		WorkerID: s.WorkerID,
		IsSeed:   s.IsSeed,
	}
}

// --- Tests -------------------------------------------------------------

type createPlacementTestRequest struct {
	SenderAccountID string `json:"sender_account_id"`
	Subject         string `json:"subject"`
	BodyPlain       string `json:"body_plain"`
	BodyHTML        string `json:"body_html"`
}

// AdminCreatePlacementTest sends a tokenized copy of a template through a chosen
// sender to every active seed mailbox, recording one pending result per seed.
func (h *Handler) AdminCreatePlacementTest(c *gin.Context) {
	if h.PlacementService == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "placement testing is not configured"))
		return
	}

	var req createPlacementTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	senderID, err := uuid.Parse(req.SenderAccountID)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid sender_account_id"))
		return
	}

	test, serr := h.PlacementService.CreateTest(c.Request.Context(), nil, senderID, req.Subject, req.BodyPlain, req.BodyHTML)
	if serr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, serr.Error()))
		return
	}

	h.audit(c, models.AuditActionCreate, placementEntity, &test.ID, map[string]string{
		"sender_account_id": senderID.String(),
		"subject":           test.Subject,
	})
	c.JSON(http.StatusOK, gin.H{"data": toPlacementTestRow(*test)})
}

// AdminListPlacementTests lists placement tests, newest first.
func (h *Handler) AdminListPlacementTests(c *gin.Context) {
	if h.PlacementRepo == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "placement testing is not configured"))
		return
	}
	offset, ok := decodeOffsetCursor(c.Query("cursor"))
	if !ok {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid cursor"))
		return
	}
	limit := parseLimit(c.Query("limit"), 25)

	tests, total, err := h.PlacementRepo.ListTests(c.Request.Context(), nil, limit, offset)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	rows := make([]placementTestRow, 0, len(tests))
	for _, t := range tests {
		rows = append(rows, toPlacementTestRow(t))
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "pagination": pageMetaFor(offset, limit, len(tests), total)})
}

// AdminGetPlacementTest returns a test with its per-provider rollup and the
// per-seed detail rows.
func (h *Handler) AdminGetPlacementTest(c *gin.Context) {
	if h.PlacementRepo == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "placement testing is not configured"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}

	test, results, err := h.PlacementRepo.GetTestWithResults(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	if test == nil {
		errx.JSON(c, errx.New(errx.NotFound, "placement test not found"))
		return
	}

	rollups := map[string]*providerRollup{}
	resultRows := make([]placementResultRow, 0, len(results))
	for _, r := range results {
		provider := r.Provider
		if provider == "" {
			provider = "unknown"
		}
		ru, ok := rollups[provider]
		if !ok {
			ru = &providerRollup{Provider: provider}
			rollups[provider] = ru
		}
		ru.Total++
		switch r.Folder {
		case repository.PlacementFolderInbox:
			ru.Inbox++
		case repository.PlacementFolderPromotions:
			ru.Promotions++
		case repository.PlacementFolderSpam:
			ru.Spam++
		case repository.PlacementFolderOther:
			ru.Other++
		default:
			ru.Pending++
		}
		resultRows = append(resultRows, placementResultRow{
			SeedAccountID: r.SeedAccountID,
			Provider:      r.Provider,
			Folder:        r.Folder,
			DetectedAt:    r.DetectedAt,
			RawFlags:      r.RawFlags,
		})
	}

	rollup := make([]providerRollup, 0, len(rollups))
	for _, ru := range rollups {
		rollup = append(rollup, *ru)
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"test":    toPlacementTestRow(*test),
			"rollup":  rollup,
			"results": resultRows,
		},
	})
}

// --- Seeds -------------------------------------------------------------

// AdminListSeedMailboxes lists the configured seed panel.
func (h *Handler) AdminListSeedMailboxes(c *gin.Context) {
	if h.PlacementRepo == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "placement testing is not configured"))
		return
	}
	seeds, err := h.PlacementRepo.ListSeedAccounts(c.Request.Context(), false)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	rows := make([]seedAccountRow, 0, len(seeds))
	for _, s := range seeds {
		rows = append(rows, toSeedRow(s))
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// AdminListSeedCandidates lists connected mailboxes an admin can flag as seeds,
// optionally filtered by an email substring.
func (h *Handler) AdminListSeedCandidates(c *gin.Context) {
	if h.PlacementRepo == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "placement testing is not configured"))
		return
	}
	limit := parseLimit(c.Query("limit"), 50)
	candidates, err := h.PlacementRepo.ListSeedCandidates(c.Request.Context(), c.Query("search"), limit)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	rows := make([]seedAccountRow, 0, len(candidates))
	for _, s := range candidates {
		rows = append(rows, toSeedRow(s))
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

type setSeedRequest struct {
	IsSeed bool `json:"is_seed"`
}

// AdminSetSeedMailbox toggles is_seed on a mailbox (register/unregister a seed).
func (h *Handler) AdminSetSeedMailbox(c *gin.Context) {
	if h.PlacementRepo == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "placement testing is not configured"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	var req setSeedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	seed, err := h.PlacementRepo.GetSeedAccount(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	if seed == nil {
		errx.JSON(c, errx.New(errx.NotFound, "mailbox not found"))
		return
	}

	if err := h.PlacementRepo.SetIsSeed(c.Request.Context(), id, req.IsSeed); err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}

	action := models.AuditActionUpdate
	h.audit(c, action, placementEntity, &id, map[string]string{
		"is_seed": map[bool]string{true: "true", false: "false"}[req.IsSeed],
		"email":   seed.Email,
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
