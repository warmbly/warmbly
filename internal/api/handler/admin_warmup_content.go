package handler

import (
	"encoding/base64"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/warmupcontent"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const warmupContentEntity models.AuditEntityType = "warmup_content"

// --- cursor helpers (opaque base64 offset) ---

func decodeOffsetCursor(s string) (int, bool) {
	if s == "" {
		return 0, true
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(string(b))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func encodeOffsetCursor(n int) string {
	return base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(n)))
}

type pageMeta struct {
	Total      int     `json:"total"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

func pageMetaFor(offset, limit, returned, total int) pageMeta {
	hasMore := offset+returned < total
	var next *string
	if hasMore {
		c := encodeOffsetCursor(offset + limit)
		next = &c
	}
	return pageMeta{Total: total, HasMore: hasMore, NextCursor: next}
}

// warmupSegmentStock is one row of the library's stock-vs-target breakdown.
type warmupSegmentStock struct {
	Segment           string `json:"segment"`
	Active            int    `json:"active"`
	Target            int    `json:"target"`
	RecentSends       int    `json:"recent_sends"`
	AverageDailySends int    `json:"average_daily_sends"`
}

// AdminWarmupContentOverview returns content-bank counts + generator status,
// including everything the UI needs to render the automatic top-up pipeline:
// whether an AI client is wired, today's spend against the daily cap, and
// per-segment stock against the scheduler's targets.
func (h *Handler) AdminWarmupContentOverview(c *gin.Context) {
	ctx := c.Request.Context()
	stats, err := h.WarmupContentRepo.ConversationStats(ctx)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	totalActive, totalArchived := 0, 0
	for _, s := range stats {
		totalActive += s.Active
		totalArchived += s.Archived
	}
	lastGen, _ := h.WarmupContentRepo.LastGeneratedAt(ctx)
	settings, _ := h.WarmupContentRepo.GetGenerationSettings(ctx)
	if settings == nil {
		def := models.DefaultWarmupGenerationSettings()
		settings = &def
	}

	aiConfigured := h.WarmupContentService != nil && h.WarmupContentService.Enabled()
	// Same daily window the scheduler uses for its cap accounting.
	generatedToday, _ := h.WarmupContentRepo.GeneratedCountSince(ctx, time.Now().Truncate(24*time.Hour))

	totalDemand, _ := h.WarmupContentRepo.WarmupSendsSince(ctx, time.Now().AddDate(0, 0, -7))

	stock := make([]warmupSegmentStock, 0, 1)
	for _, pool := range settings.Pools {
		if !pool.Enabled {
			continue
		}
		segmentSet := map[string]struct{}{"": {}}
		for _, segment := range pool.Segments {
			segmentSet[segment] = struct{}{}
		}
		for seg := range segmentSet {
			active, _ := h.WarmupContentRepo.CountActiveConversations(ctx, pool.PoolType, seg)
			recent := totalDemand
			stock = append(stock, warmupSegmentStock{
				Segment:           seg,
				Active:            active,
				Target:            warmupcontent.AdaptiveThreadTarget(recent, settings.AISelectionShare, pool.TargetActiveThreads),
				RecentSends:       recent,
				AverageDailySends: (recent + 6) / 7,
			})
		}
	}
	sort.Slice(stock, func(i, j int) bool { return stock[i].Segment < stock[j].Segment })

	c.JSON(http.StatusOK, gin.H{
		"total_active":         totalActive,
		"total_archived":       totalArchived,
		"by_pool":              stats,
		"last_generated_at":    lastGen,
		"ai_enabled":           settings.Enabled,
		"schedule_enabled":     settings.ScheduleEnabled,
		"ai_configured":        aiConfigured,
		"cadence_hours":        settings.CadenceHours,
		"refresh_enabled":      settings.RefreshEnabled,
		"refresh_per_run":      settings.RefreshPerRun,
		"ai_selection_share":   settings.AISelectionShare,
		"daily_generation_cap": settings.DailyGenerationCap,
		"generated_today":      generatedToday,
		"stock":                stock,
	})
}

type conversationListItem struct {
	ID           uuid.UUID `json:"id"`
	PoolType     string    `json:"pool_type"`
	Segment      string    `json:"segment"`
	Source       string    `json:"source"`
	Theme        string    `json:"theme"`
	Subject      string    `json:"subject"`
	Description  string    `json:"description"`
	MessageCount int       `json:"message_count"`
	Status       string    `json:"status"`
	LintPassed   bool      `json:"lint_passed"`
	UsageCount   int64     `json:"usage_count"`
	CreatedAt    time.Time `json:"created_at"`
}

// AdminListWarmupConversations lists cached conversations with filters.
func (h *Handler) AdminListWarmupConversations(c *gin.Context) {
	offset, ok := decodeOffsetCursor(c.Query("cursor"))
	if !ok {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid cursor"))
		return
	}
	limit := parseLimit(c.Query("limit"), 50)

	f := repository.ConversationFilter{
		PoolType: c.Query("pool"),
		Segment:  c.Query("segment"),
		Source:   c.Query("source"),
		Status:   c.Query("status"),
		Limit:    limit,
		Offset:   offset,
	}
	rows, total, err := h.WarmupContentRepo.ListConversations(c.Request.Context(), f)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}

	items := make([]conversationListItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, conversationListItem{
			ID: r.ID, PoolType: r.PoolType, Segment: r.Segment, Source: r.Source,
			Theme: r.Theme, Subject: r.Subject, Description: r.Description,
			MessageCount: len(r.Messages), Status: r.Status, LintPassed: r.LintPassed,
			UsageCount: r.UsageCount, CreatedAt: r.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": items, "pagination": pageMetaFor(offset, limit, len(rows), total)})
}

// AdminGetWarmupConversation returns a single conversation in full.
func (h *Handler) AdminGetWarmupConversation(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	conv, err := h.WarmupContentRepo.GetConversation(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	if conv == nil {
		errx.JSON(c, errx.New(errx.NotFound, "conversation not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": conv})
}

func (h *Handler) setConversationStatus(c *gin.Context, status string) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	if err := h.WarmupContentRepo.SetConversationStatus(c.Request.Context(), id, status); err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	h.audit(c, models.AuditActionUpdate, warmupContentEntity, &id, map[string]string{"status": status})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// AdminArchiveWarmupConversation archives a conversation (excludes it from selection).
func (h *Handler) AdminArchiveWarmupConversation(c *gin.Context) {
	h.setConversationStatus(c, "archived")
}

// AdminUnarchiveWarmupConversation re-activates a conversation.
func (h *Handler) AdminUnarchiveWarmupConversation(c *gin.Context) {
	h.setConversationStatus(c, "active")
}

// AdminDeleteWarmupConversation permanently removes a conversation.
func (h *Handler) AdminDeleteWarmupConversation(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	if err := h.WarmupContentRepo.DeleteConversation(c.Request.Context(), id); err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	h.audit(c, models.AuditActionDelete, warmupContentEntity, &id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// AdminListWarmupGenerationJobs lists generation runs (visibility).
func (h *Handler) AdminListWarmupGenerationJobs(c *gin.Context) {
	offset, ok := decodeOffsetCursor(c.Query("cursor"))
	if !ok {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid cursor"))
		return
	}
	limit := parseLimit(c.Query("limit"), 50)

	jobs, total, err := h.WarmupContentRepo.ListGenerationJobs(c.Request.Context(), limit, offset)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": jobs, "pagination": pageMetaFor(offset, limit, len(jobs), total)})
}

// AdminGetWarmupGenerationJob returns one generation run.
func (h *Handler) AdminGetWarmupGenerationJob(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	job, err := h.WarmupContentRepo.GetGenerationJob(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	if job == nil {
		errx.JSON(c, errx.New(errx.NotFound, "job not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": job})
}

type abRow struct {
	ContentSource     string  `json:"content_source"`
	Sent              int     `json:"sent"`
	SpamPlacements    int     `json:"spam_placements"`
	SpamPlacementRate float64 `json:"spam_placement_rate"`
}

// AdminWarmupContentAB returns spam-placement rate by content cohort.
func (h *Handler) AdminWarmupContentAB(c *gin.Context) {
	days := 30
	if v := c.Query("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	since := time.Now().AddDate(0, 0, -days)
	stats, err := h.WarmupContentRepo.SpamPlacementByCohort(c.Request.Context(), since)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	rows := make([]abRow, 0, len(stats))
	for _, s := range stats {
		rate := 0.0
		if s.Sent > 0 {
			rate = float64(s.SpamPlacements) / float64(s.Sent) * 100
		}
		rows = append(rows, abRow{
			ContentSource: s.ContentSource, Sent: s.Sent,
			SpamPlacements: s.SpamPlacements, SpamPlacementRate: rate,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "window_days": days})
}
