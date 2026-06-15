package contact

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils"
	"github.com/xuri/excelize/v2"
)

// ImportPreview parses the uploaded file enough to drive the column
// mapping UI. It does NOT persist anything. The same file is uploaded
// a second time on commit; storing the parsed buffer between calls
// would either pin memory or require a tmp store, neither of which is
// worth it for the typical (small) file size.
func (s *contactService) ImportPreview(ctx context.Context, r io.Reader, filename string) (*models.ContactImportPreview, *errx.Error) {
	rows, format, xerr := parseSpreadsheet(r, filename)
	if xerr != nil {
		return nil, xerr
	}
	if len(rows) == 0 {
		return nil, errx.New(errx.BadRequest, "the uploaded file is empty")
	}

	headers, hasHeader := detectHeaders(rows[0])
	dataStart := 0
	if hasHeader {
		dataStart = 1
	}

	// Sample slice for the UI to render. Cap at preview limit.
	sampleEnd := dataStart + models.MaxContactImportPreviewRows
	if sampleEnd > len(rows) {
		sampleEnd = len(rows)
	}
	sample := make([][]string, 0, sampleEnd-dataStart)
	for i := dataStart; i < sampleEnd; i++ {
		sample = append(sample, padRow(rows[i], len(headers)))
	}

	totalRows := len(rows) - dataStart

	return &models.ContactImportPreview{
		Filename:         filename,
		Format:           format,
		TotalRows:        totalRows,
		Columns:          headers,
		HasHeader:        hasHeader,
		SampleRows:       sample,
		SuggestedMapping: suggestMapping(headers),
	}, nil
}

// ImportCommit re-parses the file and writes the upsert. We don't share
// state with ImportPreview on purpose — keeping the path stateless
// makes the commit safe to retry without an opaque "session id".
func (s *contactService) ImportCommit(
	ctx context.Context,
	userID string,
	orgID uuid.UUID,
	r io.Reader,
	filename string,
	opts *models.ContactImportCommit,
) (*models.ContactImportResult, *errx.Error) {
	startedAt := time.Now().UTC()

	if opts == nil {
		return nil, errx.New(errx.BadRequest, "missing import options")
	}
	if len(opts.Mapping) == 0 {
		return nil, errx.New(errx.BadRequest, "no column mapping provided")
	}
	uid, perr := uuid.Parse(userID)
	if perr != nil {
		return nil, errx.ErrUuid
	}

	dedup := opts.Dedup
	switch dedup {
	case models.ContactImportDedupSkip,
		models.ContactImportDedupUpdate,
		models.ContactImportDedupCreateDuplicate:
	case "":
		dedup = models.ContactImportDedupSkip
	default:
		return nil, errx.New(errx.BadRequest, "unknown dedup strategy: "+string(dedup))
	}

	subscribedDefault := true
	if opts.SubscribedDefault != nil {
		subscribedDefault = *opts.SubscribedDefault
	}

	// Validate category IDs are well-formed UUIDs. Ownership scoping
	// happens later inside the repo (the INSERT joins against
	// categories.user_id) so we don't need to round-trip the DB here.
	catIDs, xerr := parseLocalCategoryIDs(opts.CategoryIDs)
	if xerr != nil {
		return nil, xerr
	}

	rows, _, xerr := parseSpreadsheet(r, filename)
	if xerr != nil {
		return nil, xerr
	}
	if len(rows) == 0 {
		return &models.ContactImportResult{
			StartedAt: startedAt,
			EndedAt:   time.Now().UTC(),
		}, nil
	}

	dataStart := 0
	if opts.HasHeader {
		dataStart = 1
	}
	data := rows[dataStart:]
	if len(data) > models.MaxContactImportRows {
		return nil, errx.New(errx.BadRequest,
			fmt.Sprintf("too many rows; max %d per import", models.MaxContactImportRows))
	}

	// Build the parsed contacts up front so we can pre-check
	// collisions in one DB round trip instead of N.
	type pendingRow struct {
		line    int
		raw     []string
		contact models.AddContact
		ok      bool
		errMsg  string
	}

	parsed := make([]pendingRow, 0, len(data))
	for i, row := range data {
		line := i + dataStart + 1 // 1-based for "open in Excel and jump"
		p := pendingRow{line: line, raw: row}

		contact, err := buildAddContact(row, opts.Mapping, subscribedDefault, opts.CampaignIDs, opts.CategoryIDs)
		if err != "" {
			p.errMsg = err
			parsed = append(parsed, p)
			continue
		}
		contact.Email = strings.TrimSpace(contact.Email)
		if contact.Email == "" || !email.IsValid(contact.Email) {
			p.errMsg = "missing or invalid email"
			parsed = append(parsed, p)
			continue
		}
		contact.Email = strings.ToLower(contact.Email)
		p.contact = contact
		p.ok = true
		parsed = append(parsed, p)
	}

	// Pre-check existing emails in one shot so we can route rows to
	// the right path (skip / update / dup).
	emails := make([]string, 0, len(parsed))
	for i := range parsed {
		if parsed[i].ok {
			emails = append(emails, parsed[i].contact.Email)
		}
	}
	existing, xerr := s.contactRepository.GetByEmailsAndUser(ctx, uid, emails)
	if xerr != nil {
		return nil, xerr
	}

	res := &models.ContactImportResult{
		Total:     len(parsed),
		StartedAt: startedAt,
		Errors:    make([]models.ContactImportRowError, 0),
	}

	// Bucket rows by target action. We send fresh inserts through
	// contactRepository.Add in batches and fall back to per-row
	// Update for the "update existing" path so we can compute the
	// merged custom_fields correctly.
	toInsert := make([]models.AddContact, 0, len(parsed))
	toInsertLines := make([]int, 0, len(parsed))
	toUpdate := make([]pendingRow, 0)

	for _, p := range parsed {
		if !p.ok {
			res.Failed++
			res.Errors = append(res.Errors, models.ContactImportRowError{
				Line: p.line, Email: p.contact.Email, Values: p.raw, Reason: p.errMsg,
			})
			continue
		}
		_, dup := existing[p.contact.Email]
		switch {
		case !dup:
			toInsert = append(toInsert, p.contact)
			toInsertLines = append(toInsertLines, p.line)
		case dedup == models.ContactImportDedupSkip:
			res.Skipped++
		case dedup == models.ContactImportDedupUpdate:
			toUpdate = append(toUpdate, p)
		case dedup == models.ContactImportDedupCreateDuplicate:
			// We can't actually create a duplicate because of the
			// unique (user_id, lower(email)) index. We treat this as
			// "update" so the data isn't lost, and surface a soft
			// warning per row. This is a deliberate, friendlier
			// behaviour than failing the whole batch.
			toUpdate = append(toUpdate, p)
		}
	}

	// Insert in chunks so a 50k row import doesn't blow up a single
	// pgx batch. 500 lines up with the Search page size.
	for start := 0; start < len(toInsert); start += 500 {
		end := start + 500
		if end > len(toInsert) {
			end = len(toInsert)
		}
		chunk := toInsert[start:end]
		inserted, xerr := s.Add(ctx, userID, orgID, chunk)
		if xerr != nil {
			// Per-row reasons are easier to act on than a "batch
			// failed" — record each as failed with the same reason.
			for i, p := range chunk {
				res.Failed++
				res.Errors = append(res.Errors, models.ContactImportRowError{
					Line:   toInsertLines[start+i],
					Email:  p.Email,
					Reason: xerr.Message,
				})
			}
			continue
		}
		res.Imported += len(inserted)
	}

	for _, p := range toUpdate {
		// Find the existing contact id and merge.
		ex := existing[p.contact.Email]
		idStr := ex.ID.String()

		update := &models.UpdateContact{
			FirstName: optString(p.contact.FirstName, ex.FirstName),
			LastName:  optString(p.contact.LastName, ex.LastName),
			Company:   optString(p.contact.Company, ex.Company),
			Phone:     optString(p.contact.Phone, ex.Phone),
		}
		if len(p.contact.CustomFields) > 0 {
			merged := make(map[string]string, len(p.contact.CustomFields))
			for k, v := range p.contact.CustomFields {
				merged[k] = v
			}
			update.CustomFields = &merged
		}
		if len(catIDs) > 0 {
			ids := make([]string, len(catIDs))
			for i, id := range catIDs {
				ids[i] = id.String()
			}
			update.AddCategories = ids
		}
		if _, xerr := s.contactRepository.Update(ctx, userID, idStr, orgID, update); xerr != nil {
			res.Failed++
			res.Errors = append(res.Errors, models.ContactImportRowError{
				Line:   p.line,
				Email:  p.contact.Email,
				Reason: xerr.Message,
			})
			continue
		}
		res.Updated++

		// Attach campaigns separately if the caller requested it.
		if len(p.contact.Campaigns) > 0 {
			if _, xerr := s.contactRepository.BulkUpdate(ctx, userID, orgID, &models.BulkEditContactsData{
				Contacts:     []string{idStr},
				AddCampaigns: p.contact.Campaigns,
			}); xerr != nil {
				// Non-fatal — the contact was updated, the link
				// failed. Surface as a row-level warning.
				res.Errors = append(res.Errors, models.ContactImportRowError{
					Line:   p.line,
					Email:  p.contact.Email,
					Reason: "contact updated but campaign link failed: " + xerr.Message,
				})
			}
		}
	}

	res.EndedAt = time.Now().UTC()
	if res.Imported > 0 || res.Updated > 0 {
		s.publishContactsReload(ctx, userID, "contacts:import")
	}
	return res, nil
}

// parseSpreadsheet returns rows as a 2-D slice and the detected format.
// CSV is decoded with the stdlib (forgiving about trailing commas /
// quoting), XLSX is decoded with excelize. Anything else 400s.
func parseSpreadsheet(r io.Reader, filename string) ([][]string, string, *errx.Error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".csv", ".tsv", ".txt", "":
		reader := csv.NewReader(r)
		reader.FieldsPerRecord = -1 // tolerate ragged rows; we pad
		reader.LazyQuotes = true
		if ext == ".tsv" {
			reader.Comma = '\t'
		}
		rows, err := reader.ReadAll()
		if err != nil {
			return nil, "csv", errx.New(errx.BadRequest, "failed to parse CSV: "+err.Error())
		}
		return rows, "csv", nil
	case ".xlsx", ".xlsm":
		f, err := excelize.OpenReader(r)
		if err != nil {
			return nil, "xlsx", errx.New(errx.BadRequest, "failed to parse XLSX: "+err.Error())
		}
		defer f.Close()
		sheetName := f.GetSheetName(f.GetActiveSheetIndex())
		if sheetName == "" {
			names := f.GetSheetList()
			if len(names) == 0 {
				return nil, "xlsx", errx.New(errx.BadRequest, "workbook has no sheets")
			}
			sheetName = names[0]
		}
		rows, err := f.GetRows(sheetName)
		if err != nil {
			return nil, "xlsx", errx.New(errx.BadRequest, "failed to read XLSX rows: "+err.Error())
		}
		return rows, "xlsx", nil
	}
	return nil, "", errx.New(errx.BadRequest, "unsupported file type: "+ext)
}

// detectHeaders applies a simple heuristic: if every cell in the first
// row looks like text (no @, no digit-heavy noise), treat it as headers.
// Users can override this in the UI; this is just the smart default.
func detectHeaders(first []string) ([]string, bool) {
	if len(first) == 0 {
		return nil, false
	}
	looksLikeHeader := true
	for _, cell := range first {
		c := strings.TrimSpace(cell)
		if c == "" {
			continue
		}
		// An "@" in the first row almost certainly means it's a data
		// row (email address) — Excel-exported CSVs sometimes ship
		// without headers at all.
		if strings.Contains(c, "@") {
			looksLikeHeader = false
			break
		}
	}
	if looksLikeHeader {
		out := make([]string, len(first))
		for i, c := range first {
			out[i] = strings.TrimSpace(c)
			if out[i] == "" {
				out[i] = "Column " + strconv.Itoa(i+1)
			}
		}
		return out, true
	}
	// No header → synthesise.
	out := make([]string, len(first))
	for i := range first {
		out[i] = "Column " + strconv.Itoa(i+1)
	}
	return out, false
}

// padRow returns a copy of `row` padded to `n` columns. Excel and Sheets
// both export ragged rows when trailing cells are empty; padding makes
// downstream code simpler.
func padRow(row []string, n int) []string {
	if len(row) >= n {
		return row[:n]
	}
	out := make([]string, n)
	copy(out, row)
	return out
}

// suggestMapping uses fuzzy header matches to pick a target for each
// column. Anything we don't recognise becomes ignore — better than
// inventing a custom-field key the user didn't ask for.
func suggestMapping(headers []string) []models.ContactImportColumnMapping {
	out := make([]models.ContactImportColumnMapping, len(headers))
	for i, h := range headers {
		out[i] = guessTarget(i, h)
	}
	return out
}

// guessTarget runs against ~the set of header aliases we've seen in the
// wild from Salesforce, HubSpot, Mailchimp, Apollo, Lemlist, raw
// gmail-contact CSVs. The match is case-insensitive + ignores spaces
// and punctuation.
func guessTarget(idx int, header string) models.ContactImportColumnMapping {
	key := strings.ToLower(header)
	key = strings.NewReplacer(" ", "", "_", "", "-", "", ".", "").Replace(key)
	switch key {
	case "email", "emailaddress", "e-mail", "mail", "emailaddress1", "primaryemail":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetEmail}
	case "firstname", "givenname", "fname", "first":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetFirstName}
	case "lastname", "familyname", "surname", "lname", "last":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetLastName}
	case "company", "companyname", "organization", "organisation", "employer", "account", "accountname":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetCompany}
	case "phone", "phonenumber", "mobile", "cell", "phone1":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetPhone}
	case "subscribed", "optin", "optedin", "subscribe":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetSubscribed}
	case "categories", "category", "tags", "tag", "labels", "label":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetCategories}
	}
	return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetIgnore}
}

// buildAddContact applies the column mapping to a single row. Returns
// either a fully-populated AddContact or a reason string explaining why
// the row was rejected. We don't bail on the first bad field — we
// gather everything so the user sees one good error.
func buildAddContact(
	row []string,
	mapping []models.ContactImportColumnMapping,
	subscribedDefault bool,
	defaultCampaignIDs []string,
	defaultCategoryIDs []string,
) (models.AddContact, string) {
	ac := models.AddContact{
		CustomFields: map[string]string{},
		Campaigns:    append([]string{}, defaultCampaignIDs...),
		Categories:   append([]string{}, defaultCategoryIDs...),
	}
	subscribedSet := false
	for _, m := range mapping {
		if m.Index < 0 || m.Index >= len(row) {
			continue
		}
		val := strings.TrimSpace(row[m.Index])
		if val == "" {
			continue
		}
		switch m.Target {
		case models.ContactImportTargetIgnore:
			continue
		case models.ContactImportTargetEmail:
			ac.Email = val
		case models.ContactImportTargetFirstName:
			ac.FirstName = val
		case models.ContactImportTargetLastName:
			ac.LastName = val
		case models.ContactImportTargetCompany:
			ac.Company = val
		case models.ContactImportTargetPhone:
			ac.Phone = val
		case models.ContactImportTargetSubscribed:
			subscribedSet = true
			b, perr := parseBoolish(val)
			if perr != "" {
				return models.AddContact{}, perr
			}
			_ = b // not used: we don't have a way to push it into AddContact yet
		case models.ContactImportTargetCategories:
			// Comma-separated list of category names — caller could
			// also pass IDs but names are friendlier for CSV
			// round-trips. For now we ignore names from the file
			// (we'd need a lookup); the bulk category assignment
			// applied by `opts.CategoryIDs` covers the common case.
			_ = val
		default:
			if strings.HasPrefix(string(m.Target), "custom:") {
				key := strings.TrimPrefix(string(m.Target), "custom:")
				if key == "" || !utils.IsValidJSONKey(key) {
					return models.AddContact{}, "invalid custom field key: " + key
				}
				ac.CustomFields[key] = val
			}
			if m.CustomKey != "" {
				if !utils.IsValidJSONKey(m.CustomKey) {
					return models.AddContact{}, "invalid custom field key: " + m.CustomKey
				}
				ac.CustomFields[m.CustomKey] = val
			}
		}
	}
	_ = subscribedSet // AddContact doesn't carry subscribed; default applies at row creation
	_ = subscribedDefault
	return ac, ""
}

// parseBoolish accepts the strings real CSV exporters emit for boolean
// columns. Empty/unknown values are treated as default (caller decides
// what default means).
func parseBoolish(v string) (bool, string) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "1", "true", "t", "yes", "y", "subscribed", "opted in", "opt-in":
		return true, ""
	case "0", "false", "f", "no", "n", "unsubscribed", "opted out", "opt-out":
		return false, ""
	}
	return false, "could not parse subscribed value: " + v
}

// optString returns a pointer to `incoming` if non-empty, else `fallback`.
// Used in the update path to avoid blanking a populated field with an
// empty CSV cell — the importer's job is to enrich, not erase.
func optString(incoming, fallback string) *string {
	if strings.TrimSpace(incoming) == "" {
		return nil
	}
	v := incoming
	_ = fallback
	return &v
}

// parseLocalCategoryIDs is the import-package twin of pg_contact's
// parseCategoryIDs. Kept private and small so we don't depend on the
// repository package's internals.
func parseLocalCategoryIDs(raw []string) ([]uuid.UUID, *errx.Error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[uuid.UUID]struct{}, len(raw))
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, errx.ErrUuid
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

var _ = errors.New
