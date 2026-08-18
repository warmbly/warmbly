package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/orgtransfer"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Organization archives: export a whole workspace to a file, and import one
// back. Owner-only, and JWT-only at the route.
//
// Owner-only is not conservatism for its own sake. An export with credentials
// is the single most sensitive artifact this product can produce — every
// mailbox password and OAuth refresh token in the workspace, in one file — and
// an import rewrites the workspace wholesale. Both sit at the same level as
// deleting the organization, so both are gated the same way.

// orgOwnerContext resolves the caller's organization and confirms they own it,
// answering the request itself when they do not.
func (h *Handler) orgOwnerContext(c *gin.Context) (orgID, userID uuid.UUID, ok bool) {
	uid, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return uuid.Nil, uuid.Nil, false
	}
	oid := middleware.GetOrganizationID(c)
	if oid == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return uuid.Nil, uuid.Nil, false
	}
	if xerr := h.requireOrgOwner(c, *oid, uid); xerr != nil {
		errx.JSON(c, errx.New(errx.Forbidden, "Only the workspace owner can export or import workspace data."))
		return uuid.Nil, uuid.Nil, false
	}
	return *oid, uid, true
}

// GetOrgTransferGroups lists the data groups an archive can carry, so the
// settings page renders the toggles from the server's own catalog rather than
// a copy that drifts.
//
// GET /organization/current/transfer/groups
func (h *Handler) GetOrgTransferGroups(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"groups":         models.OrgDataGroupCatalog,
		"format_version": models.OrgTransferFormatVersion,
		"min_passphrase": models.MinOrgExportPassphraseLength,
		"retention_days": int(models.OrgExportRetention.Hours() / 24),
	})
}

// ---------- export ----------

// CreateOrgExport starts an archive build.
//
// POST /organization/current/export
func (h *Handler) CreateOrgExport(c *gin.Context) {
	orgID, userID, ok := h.orgOwnerContext(c)
	if !ok {
		return
	}
	if h.OrgTransferService == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "Workspace export is not available on this instance."))
		return
	}

	var req models.CreateOrgExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	job, xerr := h.OrgTransferService.RequestExport(c.Request.Context(), orgID, &userID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionExport, models.AuditEntityOrgArchive, &job.ID, nil, map[string]string{
		"include_secrets": fmt.Sprintf("%t", job.IncludeSecrets),
		"groups":          fmt.Sprintf("%d", len(job.Groups)),
	})
	c.JSON(http.StatusAccepted, job)
}

// ListOrgExports returns recent archive builds.
//
// GET /organization/current/export
func (h *Handler) ListOrgExports(c *gin.Context) {
	orgID, _, ok := h.orgOwnerContext(c)
	if !ok {
		return
	}
	if h.OrgTransferService == nil {
		c.JSON(http.StatusOK, gin.H{"data": []models.OrgExportJob{}})
		return
	}

	jobs, xerr := h.OrgTransferService.ListExports(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": jobs})
}

// GetOrgExport returns one archive build, for progress polling.
//
// GET /organization/current/export/:id
func (h *Handler) GetOrgExport(c *gin.Context) {
	orgID, _, ok := h.orgOwnerContext(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	if h.OrgTransferService == nil {
		errx.JSON(c, errx.ErrNotFound)
		return
	}

	job, xerr := h.OrgTransferService.GetExport(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, job)
}

// DownloadOrgExport streams a finished archive.
//
// GET /organization/current/export/:id/download
func (h *Handler) DownloadOrgExport(c *gin.Context) {
	orgID, _, ok := h.orgOwnerContext(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	if h.OrgTransferService == nil {
		errx.JSON(c, errx.ErrNotFound)
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

	h.auditOrg(c, models.AuditActionExport, models.AuditEntityOrgArchive, &job.ID, nil, map[string]string{
		"downloaded": "true",
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
		// The client hung up mid-download. Nothing useful left to say: the
		// status line is already sent.
		return
	}
}

// DeleteOrgExport removes an archive and its stored object.
//
// DELETE /organization/current/export/:id
func (h *Handler) DeleteOrgExport(c *gin.Context) {
	orgID, _, ok := h.orgOwnerContext(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	if h.OrgTransferService == nil {
		errx.JSON(c, errx.ErrNotFound)
		return
	}

	if xerr := h.OrgTransferService.DeleteExport(c.Request.Context(), orgID, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityOrgArchive, &id, nil, nil)
	c.Status(http.StatusNoContent)
}

// ---------- import ----------

// PreflightOrgImport reports what an uploaded archive would do, writing
// nothing.
//
// POST /organization/current/import/preflight
func (h *Handler) PreflightOrgImport(c *gin.Context) {
	orgID, _, ok := h.orgOwnerContext(c)
	if !ok {
		return
	}
	if h.OrgTransferService == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "Workspace import is not available on this instance."))
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

// CreateOrgImport applies an uploaded archive to the current workspace.
//
// POST /organization/current/import
func (h *Handler) CreateOrgImport(c *gin.Context) {
	orgID, userID, ok := h.orgOwnerContext(c)
	if !ok {
		return
	}
	if h.OrgTransferService == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "Workspace import is not available on this instance."))
		return
	}

	// The import keeps reading this file after the request returns, so
	// ownership passes to the service on success and it closes it when the
	// job ends. Every failure path before that hands it back here.
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

	job, xerr := h.OrgTransferService.RequestImport(c.Request.Context(), orgID, &userID, spooled, &req)
	if xerr != nil {
		_ = spooled.Close()
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionImport, models.AuditEntityOrgArchive, &job.ID, nil, map[string]string{
		"conflict_strategy": string(job.ConflictStrategy),
	})
	c.JSON(http.StatusAccepted, job)
}

// ListOrgImports returns recent imports.
//
// GET /organization/current/import
func (h *Handler) ListOrgImports(c *gin.Context) {
	orgID, _, ok := h.orgOwnerContext(c)
	if !ok {
		return
	}
	if h.OrgTransferService == nil {
		c.JSON(http.StatusOK, gin.H{"data": []models.OrgImportJob{}})
		return
	}

	jobs, xerr := h.OrgTransferService.ListImports(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": jobs})
}

// GetOrgImport returns one import, for progress polling.
//
// GET /organization/current/import/:id
func (h *Handler) GetOrgImport(c *gin.Context) {
	orgID, _, ok := h.orgOwnerContext(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	if h.OrgTransferService == nil {
		errx.JSON(c, errx.ErrNotFound)
		return
	}

	job, xerr := h.OrgTransferService.GetImport(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, job)
}

// ---------- upload spooling ----------

// spooledArchive is an uploaded archive on local disk. A zip needs random
// access and a length, and a workspace archive is far too large to hold in
// memory, so the upload lands in a temporary file first.
//
// It is an io.Closer so ownership can be handed to a background import: the
// service closes it when the job ends, which is the only point at which the
// bytes are certainly no longer needed.
type spooledArchive struct {
	f    *os.File
	size int64
}

func (s *spooledArchive) ReadAt(p []byte, off int64) (int, error) { return s.f.ReadAt(p, off) }
func (s *spooledArchive) Size() int64                             { return s.size }

func (s *spooledArchive) Close() error {
	err := s.f.Close()
	_ = os.Remove(s.f.Name())
	return err
}

// spoolArchiveUpload writes the multipart upload to a temporary file.
func spoolArchiveUpload(c *gin.Context) (*spooledArchive, *errx.Error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, models.MaxOrgArchiveUploadBytes)

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		return nil, errx.New(errx.BadRequest, "missing 'file' form field")
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "warmbly-import-*.zip")
	if err != nil {
		return nil, errx.InternalError()
	}
	spooled := &spooledArchive{f: tmp}

	size, err := io.Copy(tmp, file)
	if err != nil {
		_ = spooled.Close()
		return nil, errx.New(errx.BadRequest, "the upload could not be read")
	}
	if size == 0 {
		_ = spooled.Close()
		return nil, errx.New(errx.BadRequest, "the uploaded file is empty")
	}
	spooled.size = size

	return spooled, nil
}

// parseUUIDParam reads a UUID path parameter, answering 400 on a malformed one.
func parseUUIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid "+name))
		return uuid.Nil, false
	}
	return id, true
}
