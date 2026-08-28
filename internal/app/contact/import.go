package contact

import (
	"context"
	"encoding/csv"
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/orgrisk"
	"github.com/warmbly/warmbly/internal/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/listquality"
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

// importColumn is one validated mapping entry: exactly one destination
// for one column index. Building these up front means a bad mapping is a
// single actionable 400 instead of the same message repeated once per row.
type importColumn struct {
	index     int
	target    models.ContactImportColumnTarget
	customKey string
}

// resolveMapping validates the client's column mapping once, before any
// row is touched. It returns the columns that actually go somewhere.
func resolveMapping(mapping []models.ContactImportColumnMapping) ([]importColumn, *errx.Error) {
	out := make([]importColumn, 0, len(mapping))
	hasEmail := false
	for _, m := range mapping {
		if m.Index < 0 {
			return nil, errx.New(errx.BadRequest,
				fmt.Sprintf("column index %d is out of range", m.Index))
		}
		target := m.Target
		key := strings.TrimSpace(m.CustomKey)
		// "custom:<key>" is the legacy spelling of {target:"custom",
		// custom_key:"<key>"}; an explicit custom_key still wins.
		if rest, ok := strings.CutPrefix(string(target), string(models.ContactImportTargetCustom)+":"); ok {
			target = models.ContactImportTargetCustom
			if key == "" {
				key = strings.TrimSpace(rest)
			}
		}

		switch target {
		case models.ContactImportTargetIgnore, "":
			continue
		case models.ContactImportTargetEmail:
			hasEmail = true
		case models.ContactImportTargetFirstName,
			models.ContactImportTargetLastName,
			models.ContactImportTargetCompany,
			models.ContactImportTargetPhone,
			models.ContactImportTargetSubscribed,
			models.ContactImportTargetCategories:
		case models.ContactImportTargetCustom:
		default:
			// An unrecognised target with a custom key is how older clients
			// spelled a custom field; anything else is a client bug.
			if key == "" {
				return nil, errx.New(errx.BadRequest,
					"unknown target "+strconv.Quote(string(m.Target))+" for column "+strconv.Itoa(m.Index+1))
			}
			target = models.ContactImportTargetCustom
		}

		if target == models.ContactImportTargetCustom {
			key = utils.NormalizeJSONKey(key)
			if key == "" {
				return nil, errx.New(errx.BadRequest,
					fmt.Sprintf("column %d is mapped to a custom field but has no name", m.Index+1))
			}
			if !utils.IsValidJSONKey(key) {
				return nil, errx.New(errx.BadRequest,
					"invalid custom field name "+strconv.Quote(key)+": "+utils.JSONKeyRules)
			}
		}

		out = append(out, importColumn{index: m.Index, target: target, customKey: key})
	}
	if !hasEmail {
		return nil, errx.New(errx.BadRequest, "map one column to Email before importing")
	}
	return out, nil
}

// ValidateImportMapping exposes resolveMapping's verdict without running an
// import, so a saved mapping can be rejected at save time.
func (s *contactService) ValidateImportMapping(mapping []models.ContactImportColumnMapping) *errx.Error {
	if len(mapping) == 0 {
		return errx.New(errx.BadRequest, "no column mapping provided")
	}
	_, xerr := resolveMapping(mapping)
	return xerr
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

	// The mapping is validated once, up front: a mistyped custom-field name
	// is one 400 the user can act on, not the same row error 50,000 times.
	columns, xerr := resolveMapping(opts.Mapping)
	if xerr != nil {
		return nil, xerr
	}

	// Validate the category and campaign IDs up front. Ownership scoping
	// happens later inside the repo (the INSERTs join against
	// categories.user_id / campaigns.organization_id) so we don't need to
	// round-trip the DB here, but they do have to be well-formed UUIDs: a
	// blank one reaches Postgres as `'' = ANY($1::uuid[])` and fails the
	// statement, which used to surface as every row failing to link.
	globalCatIDs, xerr := parseIDList(opts.CategoryIDs)
	if xerr != nil {
		return nil, xerr
	}
	globalCampaignIDs, xerr := parseIDList(opts.CampaignIDs)
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
		line       int
		raw        []string
		contact    models.AddContact
		categories []string // category titles read from the file
		ok         bool
		// dupInFile marks a row whose address an earlier row already claimed.
		// Not an error: it counts as skipped and its data was merged.
		dupInFile bool
		errMsg    string
	}

	parsed := make([]pendingRow, 0, len(data))
	// firstByEmail points at the first pending row that claimed an address, so
	// a file that lists the same person twice produces one contact instead of
	// two upserts of the same row counted as two imports.
	firstByEmail := make(map[string]int, len(data))
	for i, row := range data {
		line := i + dataStart + 1 // 1-based for "open in Excel and jump"
		p := pendingRow{line: line, raw: row}

		contact, cats, err := buildAddContact(row, columns, globalCampaignIDs, globalCatIDs)
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

		if prev, dup := firstByEmail[contact.Email]; dup {
			// Same address twice in one file. "skip" keeps the first row;
			// the other strategies merge the later row onto it so no data
			// from the file is silently dropped.
			if dedup != models.ContactImportDedupSkip {
				mergeAddContact(&parsed[prev].contact, contact)
				parsed[prev].categories = appendUnique(parsed[prev].categories, cats...)
			}
			p.contact = contact
			p.dupInFile = true
			parsed = append(parsed, p)
			continue
		}
		firstByEmail[contact.Email] = len(parsed)

		p.contact = contact
		p.categories = cats
		p.ok = true
		parsed = append(parsed, p)
	}

	// Resolve every category title the file mentions in one round trip,
	// creating the ones the user doesn't have yet.
	titleToID := map[string]uuid.UUID{}
	var allTitles []string
	for i := range parsed {
		allTitles = append(allTitles, parsed[i].categories...)
	}
	if len(allTitles) > 0 {
		titleToID, xerr = s.contactRepository.ResolveCategoryNames(ctx, uid, allTitles)
		if xerr != nil {
			return nil, xerr
		}
		for i := range parsed {
			for _, title := range parsed[i].categories {
				if id, ok := titleToID[strings.ToLower(strings.TrimSpace(title))]; ok {
					parsed[i].contact.Categories = appendUnique(parsed[i].contact.Categories, id.String())
				}
			}
		}
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

	// Measure the list the customer actually uploaded, malformed rows included:
	// those are exactly what this is counting. Synchronous and address-only, so
	// they learn something now rather than when verification catches up.
	allAddresses := make([]string, 0, len(parsed))
	for i := range parsed {
		if addr := strings.TrimSpace(parsed[i].contact.Email); addr != "" {
			allAddresses = append(allAddresses, addr)
			continue
		}
		// The mapped address would not parse. That is exactly what malformed
		// means, so it is recorded as such rather than hunting other columns
		// for something with an @ in it, which could pick up a notes field.
		allAddresses = append(allAddresses, unparseableAddress)
	}
	quality := listquality.Assess(allAddresses)

	res := &models.ContactImportResult{
		Total:     len(parsed),
		StartedAt: startedAt,
		Errors:    make([]models.ContactImportRowError, 0),
		Quality:   toImportQuality(quality),
	}
	// warn records a row-level note without counting the row as failed, so
	// Total always equals imported + updated + skipped + failed.
	warn := func(line int, addr string, values []string, reason string) {
		if len(res.Errors) >= models.MaxContactImportReportedErrors {
			res.ErrorsTruncated = true
			return
		}
		res.Errors = append(res.Errors, models.ContactImportRowError{
			Line: line, Email: addr, Values: values, Reason: reason,
		})
	}
	fail := func(line int, addr string, values []string, reason string) {
		res.Failed++
		warn(line, addr, values, reason)
	}

	// Bucket rows by target action. We send fresh inserts through
	// contactRepository.Add in batches and fall back to per-row
	// Update for the "update existing" path so we can compute the
	// merged custom_fields correctly.
	toInsert := make([]models.AddContact, 0, len(parsed))
	toInsertLines := make([]int, 0, len(parsed))
	toUpdate := make([]pendingRow, 0)
	// Existing contacts the file listed but did not change. They still have
	// to join the campaign / categories this import targets: "skip" means
	// "don't touch their fields", not "leave them out of the list".
	skippedLinks := make([]linkTarget, 0)

	for _, p := range parsed {
		if !p.ok {
			if p.dupInFile {
				res.Skipped++
				continue
			}
			fail(p.line, p.contact.Email, p.raw, p.errMsg)
			continue
		}
		ex, dup := existing[p.contact.Email]
		switch {
		case !dup:
			// SubscribedDefault is what a NEW contact inherits. An update must
			// never touch the flag, or re-importing a list would resubscribe
			// everyone who had opted out.
			if p.contact.Subscribed == nil {
				sub := subscribedDefault
				p.contact.Subscribed = &sub
			}
			toInsert = append(toInsert, p.contact)
			toInsertLines = append(toInsertLines, p.line)
		case dedup == models.ContactImportDedupSkip:
			res.Skipped++
			skippedLinks = append(skippedLinks, linkTarget{
				line:       p.line,
				email:      p.contact.Email,
				contactID:  ex.ID.String(),
				campaigns:  p.contact.Campaigns,
				categories: p.contact.Categories,
			})
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

	// Ask about the plan ceiling once for the whole batch. Per chunk it would
	// report the same plan problem 500 times, which is what the row-level
	// error list is explicitly not for.
	if xerr := s.checkContactLimit(ctx, userID, len(toInsert)); xerr != nil {
		return nil, xerr
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
				fail(toInsertLines[start+i], p.Email, nil, xerr.Message)
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
			FirstName:  optString(p.contact.FirstName),
			LastName:   optString(p.contact.LastName),
			Company:    optString(p.contact.Company),
			Phone:      optString(p.contact.Phone),
			Subscribed: p.contact.Subscribed,
		}
		if len(p.contact.CustomFields) > 0 {
			merged := make(map[string]string, len(p.contact.CustomFields))
			for k, v := range p.contact.CustomFields {
				merged[k] = v
			}
			update.CustomFields = &merged
		}
		if len(p.contact.Categories) > 0 {
			update.AddCategories = p.contact.Categories
		}
		if _, xerr := s.contactRepository.Update(ctx, userID, idStr, orgID, update); xerr != nil {
			fail(p.line, p.contact.Email, nil, xerr.Message)
			continue
		}
		res.Updated++

		// Attach campaigns separately if the caller requested it.
		if len(p.contact.Campaigns) > 0 {
			if _, xerr := s.contactRepository.BulkUpdate(ctx, userID, orgID, &models.BulkEditContactsData{
				Contacts:     []string{idStr},
				AddCampaigns: p.contact.Campaigns,
			}); xerr != nil {
				// Non-fatal: the contact was updated, only the link failed.
				// Surface it as a row note, not as a failed row.
				warn(p.line, p.contact.Email, nil, "contact updated but campaign link failed: "+xerr.Message)
			}
		}
	}

	// One BulkUpdate per distinct (campaigns, categories) set covers every
	// skipped contact that shares it, so the common case (one campaign, one
	// category list for the whole file) is a single statement.
	for _, group := range groupLinks(skippedLinks) {
		if len(group.campaigns) == 0 && len(group.categories) == 0 {
			continue
		}
		if _, xerr := s.contactRepository.BulkUpdate(ctx, userID, orgID, &models.BulkEditContactsData{
			Contacts:      group.contactIDs,
			AddCampaigns:  group.campaigns,
			AddCategories: group.categories,
		}); xerr != nil {
			// The rows that were imported are fine; only these links failed.
			// Move the affected rows from skipped to failed rather than
			// discarding the whole result.
			for _, m := range group.members {
				res.Skipped--
				fail(m.line, m.email, nil,
					"contact already existed but could not be added to the campaign: "+xerr.Message)
			}
		}
	}

	res.EndedAt = time.Now().UTC()

	// File the finding on the workspace's posture. Import quality alone can
	// only reach `watch`, which changes nothing a customer can feel; it takes
	// several detectors agreeing to restrict anything.
	if quality.Flagged && s.orgRisk != nil {
		if _, err := s.orgRisk.RecordSignal(ctx, orgID, orgrisk.Signal{
			Key:    "list_quality",
			Weight: importRiskWeight(quality.BadSharePct),
			Detail: quality.Summary,
		}); err != nil {
			log.Warn().Str("organization_id", orgID.String()).Msg("could not record the import quality signal")
		}
	}

	if res.Imported > 0 || res.Updated > 0 || len(skippedLinks) > 0 {
		s.publishContactsReload(ctx, userID, "contacts:import")
		// Covers the Google Sheets sync too: it commits through this path.
		s.wakeCampaigns(ctx, orgID, globalCampaignIDs)
	}
	return res, nil
}

// linkTarget is an existing contact that must join the import's campaigns and
// categories even though its own fields were left alone.
type linkTarget struct {
	line       int
	email      string
	contactID  string
	campaigns  []string
	categories []string
}

type linkGroup struct {
	campaigns  []string
	categories []string
	contactIDs []string
	members    []linkTarget
}

func groupLinks(targets []linkTarget) []linkGroup {
	byKey := map[string]*linkGroup{}
	order := make([]string, 0, 1)
	for _, t := range targets {
		key := strings.Join(t.campaigns, ",") + "|" + strings.Join(t.categories, ",")
		g, ok := byKey[key]
		if !ok {
			g = &linkGroup{campaigns: t.campaigns, categories: t.categories}
			byKey[key] = g
			order = append(order, key)
		}
		g.contactIDs = append(g.contactIDs, t.contactID)
		g.members = append(g.members, t)
	}
	out := make([]linkGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out
}

// mergeAddContact folds a later row for the same address onto the first one.
// Non-empty incoming values win; blanks never erase what an earlier row set.
func mergeAddContact(dst *models.AddContact, src models.AddContact) {
	if strings.TrimSpace(src.FirstName) != "" {
		dst.FirstName = src.FirstName
	}
	if strings.TrimSpace(src.LastName) != "" {
		dst.LastName = src.LastName
	}
	if strings.TrimSpace(src.Company) != "" {
		dst.Company = src.Company
	}
	if strings.TrimSpace(src.Phone) != "" {
		dst.Phone = src.Phone
	}
	if src.Subscribed != nil {
		dst.Subscribed = src.Subscribed
	}
	if len(src.CustomFields) > 0 {
		if dst.CustomFields == nil {
			dst.CustomFields = map[string]string{}
		}
		for k, v := range src.CustomFields {
			dst.CustomFields[k] = v
		}
	}
	dst.Campaigns = appendUnique(dst.Campaigns, src.Campaigns...)
	dst.Categories = appendUnique(dst.Categories, src.Categories...)
}

func appendUnique(dst []string, add ...string) []string {
	for _, v := range add {
		found := false
		for _, have := range dst {
			if have == v {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, v)
		}
	}
	return dst
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

// buildAddContact applies the resolved column mapping to a single row.
// It returns the contact, the category titles the row named, and a reason
// string when the row itself is unusable.
func buildAddContact(
	row []string,
	columns []importColumn,
	defaultCampaignIDs []string,
	defaultCategoryIDs []string,
) (models.AddContact, []string, string) {
	ac := models.AddContact{
		CustomFields: map[string]string{},
		Campaigns:    append([]string{}, defaultCampaignIDs...),
		Categories:   append([]string{}, defaultCategoryIDs...),
	}
	var categories []string
	for _, col := range columns {
		if col.index >= len(row) {
			continue
		}
		val := strings.TrimSpace(row[col.index])
		if val == "" {
			continue
		}
		switch col.target {
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
			b, perr := parseBoolish(val)
			if perr != "" {
				return models.AddContact{}, nil, perr
			}
			sub := b
			ac.Subscribed = &sub
		case models.ContactImportTargetCategories:
			// Comma- or semicolon-separated category titles. Resolved to ids
			// (creating what's missing) once for the whole file by the caller.
			for _, name := range strings.FieldsFunc(val, func(r rune) bool { return r == ',' || r == ';' }) {
				if name = strings.TrimSpace(name); name != "" {
					categories = appendUnique(categories, name)
				}
			}
		case models.ContactImportTargetCustom:
			ac.CustomFields[col.customKey] = val
		}
	}
	return ac, categories, ""
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

// optString returns a pointer to `incoming` when it has content, else nil.
// Used in the update path to avoid blanking a populated field with an
// empty CSV cell — the importer's job is to enrich, not erase.
func optString(incoming string) *string {
	if strings.TrimSpace(incoming) == "" {
		return nil
	}
	v := incoming
	return &v
}

// parseIDList canonicalises a list of UUID strings from the request: blanks
// dropped, duplicates removed, malformed rejected. It is the import-package
// twin of pg_contact's parseUUIDList, kept private and small so we don't
// depend on the repository package's internals.
func parseIDList(raw []string) ([]string, *errx.Error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[uuid.UUID]struct{}, len(raw))
	out := make([]string, 0, len(raw))
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
		out = append(out, id.String())
	}
	return out, nil
}

// unparseableAddress stands in for a row whose mapped email would not parse.
// It is deliberately not an address, so the assessment counts it as malformed.
const unparseableAddress = "(unparseable)"

// toImportQuality maps the assessment onto the API shape.
func toImportQuality(q listquality.Summary) *models.ContactImportQuality {
	if q.Total == 0 {
		return nil
	}
	return &models.ContactImportQuality{
		Malformed:   q.Malformed,
		Disposable:  q.Disposable,
		Role:        q.Role,
		BadSharePct: q.BadSharePct,
		Flagged:     q.Flagged,
		Summary:     q.Summary,
	}
}

// importRiskWeight scales the org-risk contribution with how bad the list is,
// capped so one import can never restrict a workspace on its own.
func importRiskWeight(badPct float64) int {
	w := int(badPct / 2)
	if w > 30 {
		w = 30
	}
	if w < 1 {
		w = 1
	}
	return w
}
