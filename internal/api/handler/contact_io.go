package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// jsonUnmarshalString parses a JSON-encoded form field. Wrapping the
// stdlib so the call site stays readable.
func jsonUnmarshalString(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}

// maxImportUploadBytes caps an upload so a malicious or accidental
// 1GB CSV doesn't OOM the API process. 50MB is wide enough for ~500k
// rows of typical contact data; the service-level row cap (50k) then
// truncates whatever's left.
const maxImportUploadBytes = 50 * 1024 * 1024

// ExportContacts streams CSV/XLSX/JSON. We never buffer the full body
// in memory if we can help it — the service writes through to
// c.Writer so the response trickles out as rows are encoded.
func (h *Handler) ExportContacts(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	var req models.ContactExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	// Pre-set the headers BEFORE the service starts writing rows.
	// Once any byte has been sent we can't change the content type or
	// status, so any error path after this point can only abort the
	// stream — gin will surface that as a truncated response.
	//
	// Service runs in two phases: it first resolves filename + format
	// + total count by doing the DB read (no writes yet), and then
	// hands us back metadata + does the encoding pass. We use a
	// captured buffer indirection: write directly to c.Writer.
	c.Writer.Header().Set("X-Content-Type-Options", "nosniff")

	// Hand the gin ResponseWriter to the service. The service sets
	// the actual Content-Disposition + Content-Type via the returned
	// values, so we set those first by doing a "dry" precheck — but
	// our service does buffered write into an io.Writer. To keep
	// things simple, we capture into a buffer and ship at once. For
	// 50k rows × 12 columns this is a few MB; well within reason.
	pw := &bufferedResponse{c: c}
	filename, contentType, count, err := h.ContactService.Export(c.Request.Context(), userIDStr, &req, pw)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Writer.Header().Set("X-Total-Rows", fmt.Sprintf("%d", count))
	if _, werr := c.Writer.Write(pw.buf); werr != nil {
		// Best-effort: client closed.
		return
	}

	// Audit
	h.auditOrg(c, models.AuditActionExport, models.AuditEntityContact, nil, nil, map[string]string{
		"count":  fmt.Sprintf("%d", count),
		"format": string(req.Format),
		"scope":  string(req.Scope),
	})
}

// bufferedResponse is a minimal io.Writer that captures bytes for the
// export. We use it so the service can write through a stable
// io.Writer without coupling to gin internals. For very large exports
// this trades RAM for simplicity; the row cap upstream keeps it
// bounded.
type bufferedResponse struct {
	c   *gin.Context
	buf []byte
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	return len(p), nil
}

// ImportPreviewContacts accepts a multipart upload, parses it, and
// returns column metadata + a sample for the column-mapping UI.
func (h *Handler) ImportPreviewContacts(c *gin.Context) {
	if _, err := middleware.GetUserUUID(c); err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImportUploadBytes)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "missing 'file' form field"))
		return
	}
	defer file.Close()

	preview, xerr := h.ContactService.ImportPreview(c.Request.Context(), file, header.Filename)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, preview)
}

// ImportCommitContacts accepts a multipart upload + an "options"
// JSON form field, applies the mapping, and returns per-row results.
func (h *Handler) ImportCommitContacts(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImportUploadBytes)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "missing 'file' form field"))
		return
	}
	defer file.Close()

	optsStr := c.Request.FormValue("options")
	if optsStr == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "missing 'options' form field"))
		return
	}
	var opts models.ContactImportCommit
	if jerr := jsonUnmarshalString(optsStr, &opts); jerr != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid 'options' JSON: "+jerr.Error()))
		return
	}

	result, xerr := h.ContactService.ImportCommit(c.Request.Context(), userIDStr, *orgID, file, header.Filename, &opts)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionImport, models.AuditEntityContact, nil, nil, map[string]string{
		"total":    fmt.Sprintf("%d", result.Total),
		"imported": fmt.Sprintf("%d", result.Imported),
		"updated":  fmt.Sprintf("%d", result.Updated),
		"skipped":  fmt.Sprintf("%d", result.Skipped),
		"failed":   fmt.Sprintf("%d", result.Failed),
	})

	c.JSON(http.StatusOK, result)
}
