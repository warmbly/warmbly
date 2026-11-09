package handler

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/leadfinder"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// leadsReady resolves the organization and actor, or answers the request itself.
func (h *Handler) leadsReady(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	if h.LeadFinderService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "The lead finder is not available on this instance"))
		return uuid.Nil, uuid.Nil, false
	}
	orgID, ok := requireOrgID(c)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	userID, _ := middleware.GetUserUUID(c)
	return orgID, userID, true
}

// leadsFail answers a lead finder error: the service's own refusals as they
// are, anything else as an internal error with the cause logged.
func leadsFail(c *gin.Context, err error, what string) {
	var xe *errx.Error
	if errors.As(err, &xe) {
		errx.JSON(c, xe)
		return
	}
	log.Error().Err(err).Str("op", what).Msg("lead finder request failed")
	errx.JSON(c, errx.InternalError())
}

func leadsParamID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return uuid.Nil, false
	}
	return id, true
}

// LeadFinderConfig lists the providers, categories and map style.
func (h *Handler) LeadFinderConfig(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, h.LeadFinderService.FinderConfig(c.Request.Context(), orgID))
}

// LeadFinderGeocode looks up a typed location for the map.
func (h *Handler) LeadFinderGeocode(c *gin.Context) {
	if _, _, ok := h.leadsReady(c); !ok {
		return
	}
	var near *models.LatLng
	lat, lng := c.Query("lat"), c.Query("lng")
	if (lat == "") != (lng == "") {
		errx.JSON(c, errx.New(errx.BadRequest, "lat and lng must be sent together"))
		return
	}
	if lat != "" {
		la, err1 := strconv.ParseFloat(lat, 64)
		lo, err2 := strconv.ParseFloat(lng, 64)
		if err1 != nil || err2 != nil || la < -90 || la > 90 || lo < -180 || lo > 180 {
			errx.JSON(c, errx.New(errx.BadRequest, "lat and lng must be a point on the map"))
			return
		}
		near = &models.LatLng{Lat: la, Lng: lo}
	}
	out, err := h.LeadFinderService.Geocode(c.Request.Context(), c.Query("q"), near, c.Query("lang"))
	if err != nil {
		leadsFail(c, err, "geocode")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// SearchLeads runs one page of a provider search. A retry with the same
// Idempotency-Key replays the stored answer instead of searching again.
func (h *Handler) SearchLeads(c *gin.Context) {
	orgID, userID, ok := h.leadsReady(c)
	if !ok {
		return
	}
	var req leadfinder.SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	out, err := h.LeadFinderService.Search(c.Request.Context(), orgID, userID, req)
	if err != nil {
		leadsFail(c, err, "search")
		return
	}
	if req.SearchID == nil {
		h.auditOrg(c, models.AuditActionCreate, models.AuditEntityLeadSearch, &out.Search.ID, nil,
			map[string]string{"provider": string(out.Search.Provider), "found": strconv.Itoa(out.Found)})
	} else {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLeadSearch, &out.Search.ID, nil,
			map[string]string{"page": strconv.Itoa(out.Search.PagesFetched), "found": strconv.Itoa(out.Found)})
	}
	c.JSON(http.StatusOK, out)
}

type leadQuery struct {
	Filter *models.LeadFilter `json:"filter"`
	Sort   models.LeadSort    `json:"sort"`
	Limit  *int               `json:"limit"`
	Cursor string             `json:"cursor"`
}

// QueryLeads lists leads with a filter, a page at a time. A read with a body.
func (h *Handler) QueryLeads(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	var q leadQuery
	if err := c.ShouldBindJSON(&q); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	limit := 50
	if q.Limit != nil {
		if *q.Limit < 1 || *q.Limit > 200 {
			errx.JSON(c, errx.New(errx.BadRequest, "limit must be between 1 and 200"))
			return
		}
		limit = *q.Limit
	}
	if q.Sort == "" {
		q.Sort = models.LeadSortRecent
	}
	if !q.Sort.Valid() {
		errx.JSON(c, errx.New(errx.BadRequest, "sort must be recent, score, rating, reviews or name"))
		return
	}
	offset, xerr := paging.DecodeOffsetCursor(q.Cursor)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	leads, total, err := h.LeadFinderService.List(c.Request.Context(), orgID, q.Filter, q.Sort, limit, offset)
	if err != nil {
		leadsFail(c, err, "list")
		return
	}
	var next *string
	if int64(offset+len(leads)) < total {
		next = paging.EncodeOffset(offset + len(leads))
	}
	c.JSON(http.StatusOK, gin.H{
		"data":       leads,
		"pagination": gin.H{"next_cursor": next, "has_more": next != nil, "total": total},
	})
}

// LeadPoints returns the pins for the map. A read with a body.
func (h *Handler) LeadPoints(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	var q leadQuery
	if err := c.ShouldBindJSON(&q); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	pts, err := h.LeadFinderService.Points(c.Request.Context(), orgID, q.Filter)
	if err != nil {
		leadsFail(c, err, "points")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": pts, "truncated": len(pts) >= leadfinder.PointsCap})
}

// LeadFacets returns the filter values for the scope. A read with a body.
func (h *Handler) LeadFacets(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	var q leadQuery
	if err := c.ShouldBindJSON(&q); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	out, err := h.LeadFinderService.Facets(c.Request.Context(), orgID, q.Filter)
	if err != nil {
		leadsFail(c, err, "facets")
		return
	}
	c.JSON(http.StatusOK, out)
}

// GetLead returns one lead.
func (h *Handler) GetLead(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	id, ok := leadsParamID(c, "id")
	if !ok {
		return
	}
	l, err := h.LeadFinderService.Get(c.Request.Context(), orgID, id)
	if err != nil {
		leadsFail(c, err, "get")
		return
	}
	c.JSON(http.StatusOK, l)
}

// UpdateLead edits one lead's status, notes or tags. A PATCH that sets
// values, so a retry lands on the same state.
func (h *Handler) UpdateLead(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	id, ok := leadsParamID(c, "id")
	if !ok {
		return
	}
	var p models.LeadPatch
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	n, err := h.LeadFinderService.Patch(c.Request.Context(), orgID, []uuid.UUID{id}, &p)
	if err != nil {
		leadsFail(c, err, "update")
		return
	}
	if n == 0 {
		errx.JSON(c, errx.New(errx.NotFound, "lead not found"))
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLead, &id, nil, nil)
	l, err := h.LeadFinderService.Get(c.Request.Context(), orgID, id)
	if err != nil {
		leadsFail(c, err, "get")
		return
	}
	c.JSON(http.StatusOK, l)
}

type leadBulkPatch struct {
	models.LeadSelection
	models.LeadPatch
}

// UpdateLeads edits many leads at once. Setting values and adding or removing
// tags converge, so a retry is safe.
func (h *Handler) UpdateLeads(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	var in leadBulkPatch
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	ids, err := h.LeadFinderService.Resolve(c.Request.Context(), orgID, &in.LeadSelection)
	if err != nil {
		leadsFail(c, err, "bulk update")
		return
	}
	n, err := h.LeadFinderService.Patch(c.Request.Context(), orgID, ids, &in.LeadPatch)
	if err != nil {
		leadsFail(c, err, "bulk update")
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLead, nil, nil, map[string]string{"count": strconv.FormatInt(n, 10)})
	c.JSON(http.StatusOK, gin.H{"updated": n})
}

// DeleteLeads removes leads. Deleting what is already gone is a no-op, so a
// retry is safe.
func (h *Handler) DeleteLeads(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	var sel models.LeadSelection
	if err := c.ShouldBindJSON(&sel); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	ids, err := h.LeadFinderService.Resolve(c.Request.Context(), orgID, &sel)
	if err != nil {
		leadsFail(c, err, "delete")
		return
	}
	n, err := h.LeadFinderService.Delete(c.Request.Context(), orgID, ids)
	if err != nil {
		leadsFail(c, err, "delete")
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityLead, nil, nil, map[string]string{"count": strconv.FormatInt(n, 10)})
	c.JSON(http.StatusOK, gin.H{"deleted": n})
}

type leadEnrichRequest struct {
	models.LeadSelection
	Verify *bool `json:"verify"`
	// People also finds the people each site names and works out their addresses.
	People *bool `json:"people"`
	// WebSearch lets the run search the web for a missing site or a blocked
	// site's addresses, when a search is set up; on unless false.
	WebSearch *bool `json:"web_search"`
}

// EnrichLeads starts a background run reading the leads' websites. Send an
// Idempotency-Key to make a retry return the same run.
func (h *Handler) EnrichLeads(c *gin.Context) {
	orgID, userID, ok := h.leadsReady(c)
	if !ok {
		return
	}
	var in leadEnrichRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	ids, err := h.LeadFinderService.Resolve(c.Request.Context(), orgID, &in.LeadSelection)
	if err != nil {
		leadsFail(c, err, "enrich")
		return
	}
	run, err := h.LeadFinderService.StartEnrichment(c.Request.Context(), orgID, userID, ids, leadfinder.EnrichOptions{
		Verify:    in.Verify == nil || *in.Verify,
		People:    in.People == nil || *in.People,
		WebSearch: in.WebSearch == nil || *in.WebSearch,
	})
	if err != nil {
		leadsFail(c, err, "enrich")
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLead, nil, nil,
		map[string]string{"enrichment_run": run.ID.String(), "count": strconv.Itoa(run.Total)})
	c.JSON(http.StatusAccepted, run)
}

// LeadEnrichmentRuns lists the runs still going.
func (h *Handler) LeadEnrichmentRuns(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	runs, err := h.LeadFinderService.ActiveEnrichmentRuns(c.Request.Context(), orgID)
	if err != nil {
		leadsFail(c, err, "enrichment runs")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": runs})
}

// LeadEnrichmentRun returns one run's progress.
func (h *Handler) LeadEnrichmentRun(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	id, ok := leadsParamID(c, "id")
	if !ok {
		return
	}
	run, err := h.LeadFinderService.EnrichmentRun(c.Request.Context(), orgID, id)
	if err != nil {
		leadsFail(c, err, "enrichment run")
		return
	}
	c.JSON(http.StatusOK, run)
}

// ExportLeads downloads the selected leads as CSV, XLSX or JSON. A read.
func (h *Handler) ExportLeads(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	var req leadfinder.ExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	var buf bytes.Buffer
	contentType, filename, err := h.LeadFinderService.Export(c.Request.Context(), orgID, &req, &buf)
	if err != nil {
		leadsFail(c, err, "export")
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, contentType, buf.Bytes())
}

// AddLeadsToContacts imports the selected leads as contacts. The import
// updates a contact that already exists, so a retry adds nobody twice.
func (h *Handler) AddLeadsToContacts(c *gin.Context) {
	orgID, userID, ok := h.leadsReady(c)
	if !ok {
		return
	}
	var req leadfinder.AddToContactsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	for _, list := range [][]string{req.CampaignIDs, req.CategoryIDs, req.SegmentIDs} {
		for _, id := range list {
			if _, err := uuid.Parse(strings.TrimSpace(id)); err != nil {
				errx.JSON(c, errx.New(errx.BadRequest, "campaign, category and segment ids must be UUIDs"))
				return
			}
		}
	}
	out, err := h.LeadFinderService.AddToContacts(c.Request.Context(), orgID, userID, &req)
	if err != nil {
		leadsFail(c, err, "add to contacts")
		return
	}
	meta := map[string]string{"source": "lead_finder", "imported": strconv.Itoa(out.Imported), "updated": strconv.Itoa(out.Updated)}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityContact, nil, nil, meta)
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLead, nil, nil, meta)
	c.JSON(http.StatusOK, out)
}

// LeadSearches lists recent and saved searches.
func (h *Handler) LeadSearches(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	saved := false
	if v := c.Query("saved"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "saved must be true or false"))
			return
		}
		saved = b
	}
	out, err := h.LeadFinderService.Searches(c.Request.Context(), orgID, saved)
	if err != nil {
		leadsFail(c, err, "searches")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// UpdateLeadSearch renames or saves a search. A PATCH that sets values.
func (h *Handler) UpdateLeadSearch(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	id, ok := leadsParamID(c, "id")
	if !ok {
		return
	}
	var in struct {
		Name  *string `json:"name"`
		Saved *bool   `json:"saved"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	out, err := h.LeadFinderService.UpdateSearch(c.Request.Context(), orgID, id, in.Name, in.Saved)
	if err != nil {
		leadsFail(c, err, "update search")
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLeadSearch, &id, nil, nil)
	c.JSON(http.StatusOK, out)
}

// DeleteLeadSearch removes a search; its leads stay.
func (h *Handler) DeleteLeadSearch(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	id, ok := leadsParamID(c, "id")
	if !ok {
		return
	}
	if err := h.LeadFinderService.DeleteSearch(c.Request.Context(), orgID, id); err != nil {
		leadsFail(c, err, "delete search")
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityLeadSearch, &id, nil, nil)
	c.Status(http.StatusNoContent)
}

// AddLeadPerson adds someone to a lead and works out their address when none
// is given. People on a lead are matched by name, so a retry lands on the same
// person.
func (h *Handler) AddLeadPerson(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	id, ok := leadsParamID(c, "id")
	if !ok {
		return
	}
	var in leadfinder.PersonInput
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	p, err := h.LeadFinderService.AddPerson(c.Request.Context(), orgID, id, in)
	if err != nil {
		leadsFail(c, err, "add person")
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLead, &id, nil, map[string]string{"person": p.ID.String()})
	c.JSON(http.StatusOK, p)
}

// UpdateLeadPerson edits someone on a lead. A PATCH that sets values.
func (h *Handler) UpdateLeadPerson(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	id, ok := leadsParamID(c, "id")
	if !ok {
		return
	}
	personID, ok := leadsParamID(c, "personId")
	if !ok {
		return
	}
	var patch leadfinder.PersonPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	p, err := h.LeadFinderService.UpdatePerson(c.Request.Context(), orgID, id, personID, patch)
	if err != nil {
		leadsFail(c, err, "update person")
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLead, &id, nil, map[string]string{"person": p.ID.String()})
	c.JSON(http.StatusOK, p)
}

// DeleteLeadPerson removes someone from a lead.
func (h *Handler) DeleteLeadPerson(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	id, ok := leadsParamID(c, "id")
	if !ok {
		return
	}
	personID, ok := leadsParamID(c, "personId")
	if !ok {
		return
	}
	if err := h.LeadFinderService.DeletePerson(c.Request.Context(), orgID, id, personID); err != nil {
		leadsFail(c, err, "delete person")
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLead, &id, nil, map[string]string{"person": personID.String(), "removed": "true"})
	c.Status(http.StatusNoContent)
}

// FindLeadPersonEmail works out a person's address at the lead's domain again.
// Searching again converges on the same address, so a retry is safe.
func (h *Handler) FindLeadPersonEmail(c *gin.Context) {
	orgID, _, ok := h.leadsReady(c)
	if !ok {
		return
	}
	id, ok := leadsParamID(c, "id")
	if !ok {
		return
	}
	personID, ok := leadsParamID(c, "personId")
	if !ok {
		return
	}
	p, err := h.LeadFinderService.FindPersonEmail(c.Request.Context(), orgID, id, personID)
	if err != nil {
		leadsFail(c, err, "find person email")
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityLead, &id, nil, map[string]string{"person": p.ID.String()})
	c.JSON(http.StatusOK, p)
}
