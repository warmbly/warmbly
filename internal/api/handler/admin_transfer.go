package handler

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/orgtransfer"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Operator-side workspace archives: the same export and import flow as
// org_transfer.go, keyed by the :id path parameter instead of the caller's own
// organization, and audited to the admin log rather than the org's.

// adminTransferOrg resolves :id to an existing organization and confirms the
// transfer service is wired, answering the request itself otherwise.
func (h *Handler) adminTransferOrg(c *gin.Context) (uuid.UUID, bool) {
	orgID, ok := parseUUIDParam(c, "id")
	if !ok {
		return uuid.Nil, false
	}
	org, err := h.OrgRepo.GetByID(c.Request.Context(), orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return uuid.Nil, false
	}
	if org == nil {
		errx.JSON(c, errx.New(errx.NotFound, "organization not found"))
		return uuid.Nil, false
	}
	if h.OrgTransferService == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "Workspace transfer is not available on this instance."))
		return uuid.Nil, false
	}
	return orgID, true
}

// ---------- export ----------

// AdminListOrgExports returns a workspace's recent archive builds.
//
// GET /admin/organizations/:id/exports
func (h *Handler) AdminListOrgExports(c *gin.Context) {
	orgID, ok := h.adminTransferOrg(c)
	if !ok {
		return
	}
	jobs, xerr := h.OrgTransferService.ListExports(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": jobs})
}

// AdminCreateOrgExport starts an archive build on the operator's behalf.
//
// POST /admin/organizations/:id/exports
func (h *Handler) AdminCreateOrgExport(c *gin.Context) {
	orgID, ok := h.adminTransferOrg(c)
	if !ok {
		return
	}
	var req models.CreateOrgExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	job, xerr := h.OrgTransferService.RequestExport(c.Request.Context(), orgID, middleware.GetAdminUserID(c), &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.audit(c, models.AuditActionExport, models.AuditEntityOrgArchive, &job.ID, map[string]string{
		"organization_id": orgID.String(),
		"include_secrets": fmt.Sprintf("%t", job.IncludeSecrets),
		"groups":          fmt.Sprintf("%d", len(job.Groups)),
	})
	c.JSON(http.StatusAccepted, job)
}

// AdminGetOrgExport returns one archive build, for progress polling.
//
// GET /admin/organizations/:id/exports/:exportId
func (h *Handler) AdminGetOrgExport(c *gin.Context) {
	orgID, ok := h.adminTransferOrg(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "exportId")
	if !ok {
		return
	}
	job, xerr := h.OrgTransferService.GetExport(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, job)
}

// AdminDownloadOrgExport streams a finished archive.
//
// GET /admin/organizations/:id/exports/:exportId/download
func (h *Handler) AdminDownloadOrgExport(c *gin.Context) {
	orgID, ok := h.adminTransferOrg(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "exportId")
	if !ok {
		return
	}

	body, job, xerr := h.OrgTransferService.OpenExport(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	defer body.Close()

	name := "workspace-" + job.ID.String()[:8] + ".warmbly.zip"
	if org, err := h.OrgRepo.GetByID(c.Request.Context(), orgID); err == nil && org != nil {
		name = orgtransfer.ArchiveFilename(org.Name, job.ID)
	}

	h.audit(c, models.AuditActionExport, models.AuditEntityOrgArchive, &job.ID, map[string]string{
		"organization_id": orgID.String(),
		"downloaded":      "true",
	})

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	c.Header("X-Content-Type-Options", "nosniff")
	if job.ArchiveBytes != nil {
		c.Header("Content-Length", fmt.Sprintf("%d", *job.ArchiveBytes))
	}
	if job.ArchiveSHA256 != nil {
		c.Header("X-Archive-SHA256", *job.ArchiveSHA256)
	}

	if _, err := io.Copy(c.Writer, body); err != nil {
		// The client hung up mid-download; the status line is already sent.
		return
	}
}

// AdminDeleteOrgExport removes an archive and its stored object.
//
// DELETE /admin/organizations/:id/exports/:exportId
func (h *Handler) AdminDeleteOrgExport(c *gin.Context) {
	orgID, ok := h.adminTransferOrg(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "exportId")
	if !ok {
		return
	}
	if xerr := h.OrgTransferService.DeleteExport(c.Request.Context(), orgID, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.audit(c, models.AuditActionDelete, models.AuditEntityOrgArchive, &id, map[string]string{
		"organization_id": orgID.String(),
	})
	c.Status(http.StatusNoContent)
}

// ---------- import ----------

// AdminListOrgImports returns a workspace's recent imports.
//
// GET /admin/organizations/:id/imports
func (h *Handler) AdminListOrgImports(c *gin.Context) {
	orgID, ok := h.adminTransferOrg(c)
	if !ok {
		return
	}
	jobs, xerr := h.OrgTransferService.ListImports(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": jobs})
}

// AdminPreflightOrgImport reports what an uploaded archive would do to the
// workspace, writing nothing.
//
// POST /admin/organizations/:id/imports/preflight
func (h *Handler) AdminPreflightOrgImport(c *gin.Context) {
	orgID, ok := h.adminTransferOrg(c)
	if !ok {
		return
	}
	spooled, xerr := spoolArchiveUpload(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	defer spooled.Close()

	report, xerr := h.OrgTransferService.Preflight(
		c.Request.Context(), orgID, spooled, c.Request.FormValue("passphrase"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, report)
}

// AdminCreateOrgImport applies an uploaded archive to the workspace.
//
// POST /admin/organizations/:id/imports
func (h *Handler) AdminCreateOrgImport(c *gin.Context) {
	orgID, ok := h.adminTransferOrg(c)
	if !ok {
		return
	}

	// Ownership of the spooled file passes to the service on success; every
	// failure path before that closes it here.
	spooled, xerr := spoolArchiveUpload(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	var req models.CreateOrgImportRequest
	if opts := c.Request.FormValue("options"); opts != "" {
		if err := jsonUnmarshalString(opts, &req); err != nil {
			_ = spooled.Close()
			errx.JSON(c, errx.New(errx.BadRequest, "invalid 'options' JSON: "+err.Error()))
			return
		}
	}
	req.Passphrase = c.Request.FormValue("passphrase")

	job, xerr := h.OrgTransferService.RequestImport(c.Request.Context(), orgID, middleware.GetAdminUserID(c), spooled, &req)
	if xerr != nil {
		_ = spooled.Close()
		errx.JSON(c, xerr)
		return
	}

	h.audit(c, models.AuditActionImport, models.AuditEntityOrgArchive, &job.ID, map[string]string{
		"organization_id":   orgID.String(),
		"conflict_strategy": string(job.ConflictStrategy),
	})
	c.JSON(http.StatusAccepted, job)
}
