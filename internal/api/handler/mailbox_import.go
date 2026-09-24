package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/mailboximport"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

// maxImportTextBytes bounds a pasted list, which arrives as a form field.
const maxImportTextBytes = 2 << 20

// importRequest is the multipart body shared by preview and create: a file or
// pasted text, plus the mapping and options as JSON fields.
type importRequest struct {
	input   mailboximport.Input
	mapping models.MailboxImportMapping
	options models.MailboxImportOptions
}

func readImportRequest(c *gin.Context) (*importRequest, *errx.Error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, config.MailboxImportMaxBytes+maxImportTextBytes)
	if err := c.Request.ParseMultipartForm(config.MailboxImportMaxBytes); err != nil {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "mailbox_import_too_large", "Send a file up to 10 MB as multipart form data.")
	}
	req := &importRequest{}
	if file, header, err := c.Request.FormFile("file"); err == nil {
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, config.MailboxImportMaxBytes+1))
		if err != nil || len(data) > config.MailboxImportMaxBytes {
			return nil, errx.NewWithIdentifier(errx.BadRequest, "mailbox_import_too_large", "The file is larger than 10 MB.")
		}
		req.input.File, req.input.Filename = data, header.Filename
	} else {
		text := c.Request.FormValue("text")
		if len(text) > maxImportTextBytes {
			return nil, errx.NewWithIdentifier(errx.BadRequest, "mailbox_import_too_large", "The pasted list is too long; upload it as a file.")
		}
		req.input.Text = text
	}
	if raw := c.Request.FormValue("mapping"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &req.mapping); err != nil {
			return nil, errx.New(errx.BadRequest, "mapping must be a JSON object of column index to field")
		}
	}
	if raw := c.Request.FormValue("options"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &req.options); err != nil {
			return nil, errx.New(errx.BadRequest, "options must be a JSON object")
		}
	}
	return req, nil
}

// PreviewMailboxImport is POST /emails/imports/preview: what an import of this
// file would do, row by row and domain by domain, without doing any of it.
func (h *Handler) PreviewMailboxImport(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	req, xerr := readImportRequest(c)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	preview, xerr := h.MailboxImportService.Preview(c.Request.Context(), *orgID, req.input, req.mapping, req.options)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, preview)
}

// CreateMailboxImport is POST /emails/imports: store the rows and connect them
// in the background. Retrying the request creates a second import, whose rows
// find the first one's mailboxes already connected and update them.
func (h *Handler) CreateMailboxImport(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	req, xerr := readImportRequest(c)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	imp, xerr := h.MailboxImportService.Create(c.Request.Context(), mailboximport.CreateInput{
		OrgID: *orgID, UserID: userID, Input: req.input, Mapping: req.mapping, Options: req.options,
	})
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, imp)
}

// ListMailboxImports is GET /emails/imports, newest first.
func (h *Handler) ListMailboxImports(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	limit, xerr := validate.Limit(c.Query("limit"))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	if limit > 100 {
		errx.Handle(c, errx.ErrLimit)
		return
	}
	list, next, xerr := h.MailboxImportService.List(c.Request.Context(), *orgID, c.Query("cursor"), int(limit))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "pagination": gin.H{"next_cursor": next, "has_more": next != ""}})
}

func importID(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return uuid.Nil, uuid.Nil, false
	}
	return *orgID, id, true
}

// GetMailboxImport is GET /emails/imports/:id.
func (h *Handler) GetMailboxImport(c *gin.Context) {
	orgID, id, ok := importID(c)
	if !ok {
		return
	}
	imp, xerr := h.MailboxImportService.Get(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, imp)
}

var importRowStatuses = map[string]bool{
	models.ImportRowQueued: true, models.ImportRowRunning: true, models.ImportRowConnected: true,
	models.ImportRowUpdated: true, models.ImportRowSkipped: true, models.ImportRowFailed: true,
	models.ImportRowNeedsSignin: true, models.ImportRowCancelled: true,
}

// ListMailboxImportRows is GET /emails/imports/:id/rows?status=failed,needs_signin&cause=.
func (h *Handler) ListMailboxImportRows(c *gin.Context) {
	orgID, id, ok := importID(c)
	if !ok {
		return
	}
	limit, xerr := validate.Limit(c.Query("limit"))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	if limit > 200 {
		errx.Handle(c, errx.ErrLimit)
		return
	}
	var statuses []string
	if raw := c.Query("status"); raw != "" {
		for _, st := range strings.Split(raw, ",") {
			if !importRowStatuses[st] {
				errx.Handle(c, errx.New(errx.BadRequest, "unknown status "+strconv.Quote(st)))
				return
			}
			statuses = append(statuses, st)
		}
	}
	rows, next, xerr := h.MailboxImportService.Rows(c.Request.Context(), orgID, id, statuses, c.Query("cause"), c.Query("cursor"), int(limit))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "pagination": gin.H{"next_cursor": next, "has_more": next != ""}})
}

// FixMailboxImportRow is PATCH /emails/imports/:id/rows/:line: correct one
// failed row and queue it again. Retrying is safe: a row that already
// connected is refused.
func (h *Handler) FixMailboxImportRow(c *gin.Context) {
	orgID, id, ok := importID(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	line, err := strconv.Atoi(c.Param("line"))
	if err != nil || line < 1 {
		errx.Handle(c, errx.New(errx.BadRequest, "line must be a row number"))
		return
	}
	var fix models.MailboxImportRowFix
	if err := c.ShouldBindJSON(&fix); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	row, xerr := h.MailboxImportService.FixRow(c.Request.Context(), orgID, userID, id, line, fix)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, row)
}

// RetryMailboxImport is POST /emails/imports/:id/retry. Only failed rows are
// requeued, so repeating the call cannot connect anything twice.
func (h *Handler) RetryMailboxImport(c *gin.Context) {
	orgID, id, ok := importID(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	var req models.MailboxImportRetry
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			errx.Handle(c, errx.ErrInvalid)
			return
		}
	}
	imp, xerr := h.MailboxImportService.Retry(c.Request.Context(), orgID, userID, id, req)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, imp)
}

// CancelMailboxImport is POST /emails/imports/:id/cancel; cancelling twice is a no-op.
func (h *Handler) CancelMailboxImport(c *gin.Context) {
	orgID, id, ok := importID(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	imp, xerr := h.MailboxImportService.Cancel(c.Request.Context(), orgID, userID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, imp)
}

// DismissMailboxImport is POST /emails/imports/:id/dismiss: hides the import
// from the recent list, stopping it first when it is still running. A repeat
// changes nothing, so it needs no Idempotency-Key.
func (h *Handler) DismissMailboxImport(c *gin.Context) {
	orgID, id, ok := importID(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	if xerr := h.MailboxImportService.Dismiss(c.Request.Context(), orgID, userID, id); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.Status(http.StatusNoContent)
}

// DownloadMailboxImportFailures is GET /emails/imports/:id/failed.csv.
func (h *Handler) DownloadMailboxImportFailures(c *gin.Context) {
	orgID, id, ok := importID(c)
	if !ok {
		return
	}
	data, name, xerr := h.MailboxImportService.FailedCSV(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}
