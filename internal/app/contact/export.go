package contact

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/xuri/excelize/v2"
)

// Export streams contacts in the requested format directly to w. The
// caller is responsible for setting response headers based on the
// returned content type + filename.
func (s *contactService) Export(
	ctx context.Context,
	userID string,
	req *models.ContactExportRequest,
	w io.Writer,
) (string, string, int, *errx.Error) {
	if req == nil {
		return "", "", 0, errx.New(errx.BadRequest, "missing export request body")
	}

	format, xerr := normalizeExportFormat(req.Format)
	if xerr != nil {
		return "", "", 0, xerr
	}
	scope, xerr := normalizeExportScope(req.Scope)
	if xerr != nil {
		return "", "", 0, xerr
	}

	// Resolve the column set up front so we can render the header row
	// (CSV/XLSX) before walking contacts.
	fields := req.Fields
	if len(fields) == 0 {
		fields = models.DefaultExportFields
	}
	if err := validateFieldList(fields); err != nil {
		return "", "", 0, err
	}

	var (
		searchFilters *models.SearchContacts
		contactIDs    []string
	)
	switch scope {
	case models.ContactExportScopeAll:
		// nothing to do; ExportAll handles "no filters" as "everyone".
	case models.ContactExportScopeFiltered:
		if req.Filters == nil {
			return "", "", 0, errx.New(errx.BadRequest, "filters required when scope=filtered")
		}
		searchFilters = req.Filters
	case models.ContactExportScopeSelected:
		if len(req.ContactIDs) == 0 {
			return "", "", 0, errx.New(errx.BadRequest, "contact_ids required when scope=selected")
		}
		contactIDs = req.ContactIDs
	}

	rows, xerr := s.contactRepository.ExportAll(ctx, userID, searchFilters, contactIDs, models.MaxContactExportRows)
	if xerr != nil {
		return "", "", 0, xerr
	}

	filename := sanitizeFilename(req.Filename)
	if filename == "" {
		filename = "contacts-" + time.Now().UTC().Format("2006-01-02")
	}

	switch format {
	case models.ContactExportFormatCSV:
		if err := writeCSV(w, rows, fields); err != nil {
			return "", "", 0, errx.InternalError()
		}
		return filename + ".csv", "text/csv; charset=utf-8", len(rows), nil
	case models.ContactExportFormatJSON:
		if err := writeJSON(w, rows, fields); err != nil {
			return "", "", 0, errx.InternalError()
		}
		return filename + ".json", "application/json", len(rows), nil
	case models.ContactExportFormatXLSX:
		if err := writeXLSX(w, rows, fields); err != nil {
			return "", "", 0, errx.InternalError()
		}
		return filename + ".xlsx",
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			len(rows), nil
	}
	return "", "", 0, errx.New(errx.BadRequest, "unsupported export format")
}

func normalizeExportFormat(f models.ContactExportFormat) (models.ContactExportFormat, *errx.Error) {
	switch strings.ToLower(string(f)) {
	case "", "csv":
		return models.ContactExportFormatCSV, nil
	case "xlsx":
		return models.ContactExportFormatXLSX, nil
	case "json":
		return models.ContactExportFormatJSON, nil
	}
	return "", errx.New(errx.BadRequest, "unsupported format: "+string(f))
}

func normalizeExportScope(s models.ContactExportScope) (models.ContactExportScope, *errx.Error) {
	switch strings.ToLower(string(s)) {
	case "", "all":
		return models.ContactExportScopeAll, nil
	case "filtered":
		return models.ContactExportScopeFiltered, nil
	case "selected":
		return models.ContactExportScopeSelected, nil
	}
	return "", errx.New(errx.BadRequest, "unsupported scope: "+string(s))
}

// validateFieldList catches typo'd field identifiers early so the user
// sees an explicit error instead of a column full of blanks.
func validateFieldList(fields []string) *errx.Error {
	for _, f := range fields {
		if strings.HasPrefix(f, "custom:") {
			key := strings.TrimPrefix(f, "custom:")
			if key == "" {
				return errx.New(errx.BadRequest, "custom: prefix without a key")
			}
			continue
		}
		switch f {
		case models.ContactExportFieldID,
			models.ContactExportFieldEmail,
			models.ContactExportFieldFirstName,
			models.ContactExportFieldLastName,
			models.ContactExportFieldCompany,
			models.ContactExportFieldPhone,
			models.ContactExportFieldSubscribed,
			models.ContactExportFieldCategories,
			models.ContactExportFieldCampaigns,
			models.ContactExportFieldCreatedAt,
			models.ContactExportFieldUpdatedAt:
		default:
			return errx.New(errx.BadRequest, "unknown export field: "+f)
		}
	}
	return nil
}

// fieldHeader is the user-visible column label. We keep the export
// header human-readable (capitalised, no underscores) since the file
// is meant to be opened in Excel/Sheets and stared at.
func fieldHeader(f string) string {
	if strings.HasPrefix(f, "custom:") {
		return strings.TrimPrefix(f, "custom:")
	}
	switch f {
	case models.ContactExportFieldID:
		return "ID"
	case models.ContactExportFieldEmail:
		return "Email"
	case models.ContactExportFieldFirstName:
		return "First Name"
	case models.ContactExportFieldLastName:
		return "Last Name"
	case models.ContactExportFieldCompany:
		return "Company"
	case models.ContactExportFieldPhone:
		return "Phone"
	case models.ContactExportFieldSubscribed:
		return "Subscribed"
	case models.ContactExportFieldCategories:
		return "Categories"
	case models.ContactExportFieldCampaigns:
		return "Campaigns"
	case models.ContactExportFieldCreatedAt:
		return "Created At"
	case models.ContactExportFieldUpdatedAt:
		return "Updated At"
	}
	return f
}

// fieldValue is the cell text for a given contact + field. Concrete
// types are stringified consistently so the same exported file produces
// the same hash if exported twice with no data changes (helps QA).
func fieldValue(c *models.Contact, f string) string {
	if strings.HasPrefix(f, "custom:") {
		key := strings.TrimPrefix(f, "custom:")
		if c.CustomFields != nil {
			return c.CustomFields[key]
		}
		return ""
	}
	switch f {
	case models.ContactExportFieldID:
		return c.ID.String()
	case models.ContactExportFieldEmail:
		return c.Email
	case models.ContactExportFieldFirstName:
		return c.FirstName
	case models.ContactExportFieldLastName:
		return c.LastName
	case models.ContactExportFieldCompany:
		return c.Company
	case models.ContactExportFieldPhone:
		return c.Phone
	case models.ContactExportFieldSubscribed:
		return strconv.FormatBool(c.Subscribed)
	case models.ContactExportFieldCategories:
		titles := make([]string, len(c.Categories))
		for i, cat := range c.Categories {
			titles[i] = cat.Title
		}
		return strings.Join(titles, ", ")
	case models.ContactExportFieldCampaigns:
		names := make([]string, len(c.Campaigns))
		for i, cam := range c.Campaigns {
			names[i] = cam.Name
		}
		return strings.Join(names, ", ")
	case models.ContactExportFieldCreatedAt:
		if c.CreatedAt.IsZero() {
			return ""
		}
		return c.CreatedAt.UTC().Format(time.RFC3339)
	case models.ContactExportFieldUpdatedAt:
		if c.UpdatedAt.IsZero() {
			return ""
		}
		return c.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return ""
}

// writeCSV uses encoding/csv from the stdlib. The leading UTF-8 BOM is
// what Excel-on-Windows looks for to render non-ASCII characters
// correctly when opening a CSV directly — without it, names like "Söre"
// become "SÃ¶re". Annoying but real.
func writeCSV(w io.Writer, rows []models.Contact, fields []string) error {
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	headers := make([]string, len(fields))
	for i, f := range fields {
		headers[i] = fieldHeader(f)
	}
	if err := cw.Write(headers); err != nil {
		return err
	}
	rec := make([]string, len(fields))
	for i := range rows {
		c := &rows[i]
		for j, f := range fields {
			rec[j] = fieldValue(c, f)
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// writeJSON emits a flat array of objects keyed by field identifier so
// the file round-trips through the import endpoint unmodified.
func writeJSON(w io.Writer, rows []models.Contact, fields []string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	out := make([]map[string]any, len(rows))
	for i := range rows {
		c := &rows[i]
		obj := make(map[string]any, len(fields))
		for _, f := range fields {
			if f == models.ContactExportFieldSubscribed {
				obj[f] = c.Subscribed
			} else if f == models.ContactExportFieldCategories {
				obj[f] = c.Categories
			} else if f == models.ContactExportFieldCampaigns {
				obj[f] = c.Campaigns
			} else {
				obj[f] = fieldValue(c, f)
			}
		}
		out[i] = obj
	}
	return enc.Encode(out)
}

// writeXLSX writes a single-sheet xlsx file using excelize. We freeze
// the header row + bold it so the file feels native to anyone who's
// ever opened an Excel template.
func writeXLSX(w io.Writer, rows []models.Contact, fields []string) error {
	f := excelize.NewFile()
	defer f.Close()

	sheet := "Contacts"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		return err
	}
	f.SetActiveSheet(idx)
	if err := f.DeleteSheet("Sheet1"); err != nil {
		// Best-effort: this is the default sheet excelize creates;
		// if it's already gone we don't care.
		var notFoundErr *excelize.ErrSheetNotExist
		if !errors.As(err, &notFoundErr) {
			return err
		}
	}

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#F1F5F9"}},
	})

	for i, fld := range fields {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, fieldHeader(fld)); err != nil {
			return err
		}
	}
	if headerStyle != 0 && len(fields) > 0 {
		end, _ := excelize.CoordinatesToCellName(len(fields), 1)
		_ = f.SetCellStyle(sheet, "A1", end, headerStyle)
	}
	if err := f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	}); err != nil {
		// Freeze pane is decorative; don't fail the whole export over it.
		_ = err
	}

	for r := 0; r < len(rows); r++ {
		c := &rows[r]
		for i, fld := range fields {
			cell, _ := excelize.CoordinatesToCellName(i+1, r+2)
			var v any
			if fld == models.ContactExportFieldSubscribed {
				v = c.Subscribed
			} else {
				v = fieldValue(c, fld)
			}
			if err := f.SetCellValue(sheet, cell, v); err != nil {
				return err
			}
		}
	}

	// Reasonable column widths so the file looks usable on first open.
	if len(fields) > 0 {
		startCol, _ := excelize.ColumnNumberToName(1)
		endCol, _ := excelize.ColumnNumberToName(len(fields))
		_ = f.SetColWidth(sheet, startCol, endCol, 20)
	}

	return f.Write(w)
}

var filenameSafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// sanitizeFilename strips path separators and weird unicode so the
// Content-Disposition header is well-formed without quoting tricks.
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = filenameSafe.ReplaceAllString(name, "_")
	name = strings.Trim(name, "._-")
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}
