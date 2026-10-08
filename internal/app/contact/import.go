package contact

import (
	"context"
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/importmap"
	"github.com/warmbly/warmbly/internal/app/orgrisk"
	"github.com/warmbly/warmbly/internal/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emailverify"
	"github.com/warmbly/warmbly/internal/pkg/listquality"
	"github.com/warmbly/warmbly/internal/pkg/spreadsheet"
	"github.com/warmbly/warmbly/internal/pkg/typesafe"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils"
)

// ImportPreview parses the uploaded file enough to drive the column
// mapping UI. It does NOT persist anything; the synchronous commit uploads the
// same file again, and the background import keeps the rows it parsed here.
func (s *contactService) ImportPreview(ctx context.Context, orgID uuid.UUID, r io.Reader, filename string) (*models.ContactImportPreview, *errx.Error) {
	rows, format, xerr := parseSpreadsheet(r, filename)
	if xerr != nil {
		return nil, xerr
	}
	return s.BuildImportPreview(ctx, orgID, filename, format, rows)
}

// BuildImportPreview describes parsed rows for the mapper: headers, a sample,
// every column's fill over the whole file, and the suggested mapping.
func (s *contactService) BuildImportPreview(ctx context.Context, orgID uuid.UUID, filename, format string, rows [][]string) (*models.ContactImportPreview, *errx.Error) {
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
	suggested, inferred := s.SuggestImportMapping(ctx, orgID, headers, sample)

	return &models.ContactImportPreview{
		Filename:         filename,
		Format:           format,
		TotalRows:        totalRows,
		Columns:          headers,
		HasHeader:        hasHeader,
		SampleRows:       sample,
		SuggestedMapping: suggested,
		InferredColumns:  inferred,
		ColumnStats:      columnStats(len(headers), rows[dataStart:]),
		MappingSource:    models.ContactImportMappingSuggested,
	}, nil
}

// columnStats measures every column over every data row: how many cells are
// filled, how many distinct values there are, and a few of them.
func columnStats(n int, rows [][]string) []models.ContactImportColumnStats {
	const samples, sampleLen = 3, 80
	out := make([]models.ContactImportColumnStats, n)
	seen := make([]map[string]struct{}, n)
	for i := range out {
		out[i].Samples = []string{}
		seen[i] = map[string]struct{}{}
	}
	for _, row := range rows {
		for i := 0; i < n && i < len(row); i++ {
			v := strings.TrimSpace(row[i])
			if v == "" {
				continue
			}
			out[i].Filled++
			if len(seen[i]) >= models.MaxContactImportDistinctTracked {
				continue
			}
			if _, dup := seen[i][v]; dup {
				continue
			}
			seen[i][v] = struct{}{}
			if len(out[i].Samples) < samples {
				if r := []rune(v); len(r) > sampleLen {
					v = string(r[:sampleLen]) + "…"
				}
				out[i].Samples = append(out[i].Samples, v)
			}
		}
	}
	for i := range out {
		out[i].Distinct = len(seen[i])
	}
	return out
}

// SuggestImportMapping is the mapping every importer's preview starts from:
// the deterministic suggestion, then the TypeSafe judgment for the columns it
// left unmapped when one is wired. inferred lists the columns the judgment
// placed, so the mapper can ask for a second look at those.
func (s *contactService) SuggestImportMapping(ctx context.Context, orgID uuid.UUID, headers []string, sample [][]string) ([]models.ContactImportColumnMapping, []int) {
	keys := s.existingCustomFieldKeys(ctx, orgID)
	shapes := importmap.Shapes(len(headers), sample)
	suggested := suggestMapping(headers, sample, shapes, keys)
	if s.columnJudge == nil {
		return suggested, nil
	}
	return importmap.Infer(typesafe.WithUsage(ctx, "contact_import", orgID.String()), s.columnJudge, suggested, headers, shapes, keys)
}

// existingCustomFieldKeys is the workspace's custom-field keys for the
// suggester. A failed read only costs the suggestion, never the preview.
func (s *contactService) existingCustomFieldKeys(ctx context.Context, orgID uuid.UUID) []string {
	keys, err := s.contactRepository.DistinctCustomFieldKeys(ctx, orgID)
	if err != nil {
		log.Warn().Str("organization_id", orgID.String()).Msg("could not read custom field keys for the import suggestion")
		return nil
	}
	return keys
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
		case models.ContactImportTargetVerificationStatus:
			if p := strings.TrimSpace(m.VerificationProvider); p != "" {
				k, ok := emailverify.KnownVocabulary(p)
				if !ok {
					return nil, errx.New(errx.BadRequest,
						"unknown verification provider "+strconv.Quote(p)+" for column "+strconv.Itoa(m.Index+1))
				}
				key = k
			}
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

// ImportRow is one data row of an uploaded file with its 1-based line there.
type ImportRow struct {
	Line  int
	Cells []string
}

// ImportSink follows an import as it runs chunk by chunk.
type ImportSink interface {
	// Settle records what became of every row of one applied chunk.
	Settle(ctx context.Context, outcomes []models.ContactImportRowOutcome) error
	// Cancelled reports whether the import should stop before its next chunk.
	Cancelled(ctx context.Context) bool
}

// importChunkSize is how many rows are written per step; every step settles.
const importChunkSize = 500

// Reasons recorded against rows that did not import.
const (
	reasonInvalidEmail   = "missing or invalid email"
	reasonElsewhere      = "you already have this address as a contact in another workspace, so it cannot be added to this one"
	reasonAlreadyContact = "already in your contacts"
	reasonDeleted        = "the contact was deleted while the import ran"
)

// ImportCommit parses the file and runs the import in the request. We don't
// share state with ImportPreview on purpose: the path stays stateless, so the
// commit is safe to retry without an opaque "session id".
func (s *contactService) ImportCommit(
	ctx context.Context,
	userID string,
	orgID uuid.UUID,
	r io.Reader,
	filename string,
	opts *models.ContactImportCommit,
) (*models.ContactImportResult, *errx.Error) {
	plan, xerr := s.prepareImport(ctx, userID, orgID, opts)
	if xerr != nil {
		return nil, xerr
	}
	// A plain file import unless the caller (the Google Sheets sync) says
	// otherwise; the file name is the detail a user recognises.
	if opts.Source == "" {
		opts.Source, opts.SourceDetail = models.ContactSourceImport, filename
	}

	rows, _, xerr := parseSpreadsheet(r, filename)
	if xerr != nil {
		return nil, xerr
	}
	dataStart := 0
	if opts.HasHeader && len(rows) > 0 {
		dataStart = 1
	}
	data := make([]ImportRow, 0, len(rows)-dataStart)
	for i := dataStart; i < len(rows); i++ {
		data = append(data, ImportRow{Line: i + 1, Cells: rows[i]})
	}
	return s.runImport(ctx, plan, data, nil, nil)
}

// RunImport applies a mapped import to rows in chunks, settling each chunk
// with sink. prior lists contacts an earlier run of the same import already
// touched, so they still join its segments.
func (s *contactService) RunImport(ctx context.Context, userID string, orgID uuid.UUID, rows []ImportRow, opts *models.ContactImportCommit, sink ImportSink, prior []uuid.UUID) (*models.ContactImportResult, *errx.Error) {
	plan, xerr := s.prepareImport(ctx, userID, orgID, opts)
	if xerr != nil {
		return nil, xerr
	}
	return s.runImport(ctx, plan, rows, sink, prior)
}

func (s *contactService) ValidateImportOptions(ctx context.Context, userID string, orgID uuid.UUID, opts *models.ContactImportCommit) *errx.Error {
	_, xerr := s.prepareImport(ctx, userID, orgID, opts)
	return xerr
}

// importPlan is an import's options, validated once before any row is read.
type importPlan struct {
	userID            string
	uid               uuid.UUID
	orgID             uuid.UUID
	opts              *models.ContactImportCommit
	dedup             models.ContactImportDedupStrategy
	subscribedDefault bool
	columns           []importColumn
	categoryIDs       []string
	campaignIDs       []string
	segmentIDs        []uuid.UUID
}

func (s *contactService) prepareImport(ctx context.Context, userID string, orgID uuid.UUID, opts *models.ContactImportCommit) (*importPlan, *errx.Error) {
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
	plan := &importPlan{userID: userID, uid: uid, orgID: orgID, opts: opts, dedup: opts.Dedup, subscribedDefault: true}
	switch plan.dedup {
	case models.ContactImportDedupSkip,
		models.ContactImportDedupUpdate,
		models.ContactImportDedupCreateDuplicate:
	case "":
		plan.dedup = models.ContactImportDedupSkip
	default:
		return nil, errx.New(errx.BadRequest, "unknown dedup strategy: "+string(plan.dedup))
	}
	if opts.SubscribedDefault != nil {
		plan.subscribedDefault = *opts.SubscribedDefault
	}

	// The mapping is validated once, up front: a mistyped custom-field name
	// is one 400 the user can act on, not the same row error 50,000 times.
	var xerr *errx.Error
	if plan.columns, xerr = resolveMapping(opts.Mapping); xerr != nil {
		return nil, xerr
	}
	// Category and campaign ids must be well-formed: a blank one reaches
	// Postgres as `'' = ANY($1::uuid[])` and fails the statement. Ownership is
	// scoped later, inside the repository's organization-scoped writes.
	if plan.categoryIDs, xerr = parseIDList(opts.CategoryIDs); xerr != nil {
		return nil, xerr
	}
	if plan.campaignIDs, xerr = parseIDList(opts.CampaignIDs); xerr != nil {
		return nil, xerr
	}
	// Segment targets are resolved up front: each must exist in the org.
	// Membership is written after the rows exist, as an include override.
	if plan.segmentIDs, xerr = s.resolveSegmentIDs(ctx, orgID, opts.SegmentIDs, opts.SkipMissingSegments); xerr != nil {
		return nil, xerr
	}
	return plan, nil
}

// pendingRow is one data row read through the mapping.
type pendingRow struct {
	line       int
	raw        []string
	contact    models.AddContact
	categories []string // category titles read from the file
	ok         bool
	// dupOf is the line of an earlier row with the same address. Not an
	// error: the row counts as skipped and its data was merged.
	dupOf int
	// rawEmail is the mapped address cell as written, for rows it failed.
	rawEmail string
	errMsg   string
}

// parseRows applies the mapping to every row and collapses repeated addresses
// onto their first row, so a file listing someone twice makes one contact.
func (p *importPlan) parseRows(rows []ImportRow) []pendingRow {
	parsed := make([]pendingRow, 0, len(rows))
	firstByEmail := make(map[string]int, len(rows))
	for _, row := range rows {
		pr := pendingRow{line: row.Line, raw: row.Cells}
		contact, cats, err := buildAddContact(row.Cells, p.columns, p.campaignIDs, p.categoryIDs)
		if err != "" {
			pr.rawEmail, pr.errMsg = contact.Email, err
			parsed = append(parsed, pr)
			continue
		}
		// Normalized rather than lowercased: a cell holding
		// `Dana Reyes <dana@acme.com>` parses as an address, and the dedupe
		// keys on the result so both spellings collapse into one contact.
		addr, ok := email.Normalize(contact.Email)
		if !ok {
			pr.rawEmail, pr.errMsg = strings.TrimSpace(contact.Email), reasonInvalidEmail
			parsed = append(parsed, pr)
			continue
		}
		contact.Email = addr

		if prev, dup := firstByEmail[addr]; dup {
			// "skip" keeps the first row; the other strategies merge the later
			// row onto it so no data from the file is silently dropped.
			if p.dedup != models.ContactImportDedupSkip {
				mergeAddContact(&parsed[prev].contact, contact)
				parsed[prev].categories = appendUnique(parsed[prev].categories, cats...)
			}
			pr.contact, pr.dupOf = contact, parsed[prev].line
			parsed = append(parsed, pr)
			continue
		}
		firstByEmail[addr] = len(parsed)
		pr.contact, pr.categories, pr.ok = contact, cats, true
		parsed = append(parsed, pr)
	}
	return parsed
}

// assessRows measures the list the customer actually uploaded, malformed rows
// included: those are exactly what this is counting. Synchronous and
// address-only, so they learn something before verification catches up.
func assessRows(parsed []pendingRow) listquality.Summary {
	all := make([]string, 0, len(parsed))
	for i := range parsed {
		if addr := strings.TrimSpace(parsed[i].contact.Email); addr != "" {
			all = append(all, addr)
			continue
		}
		// The mapped address would not parse. That is exactly what malformed
		// means, so it is recorded as such rather than hunting other columns
		// for something with an @ in it, which could pick up a notes field.
		all = append(all, unparseableAddress)
	}
	return listquality.Assess(all)
}

// importRecorder counts every row into exactly one bucket, so Total always
// equals imported + updated + skipped + failed, and collects the outcomes.
type importRecorder struct {
	res      *models.ContactImportResult
	outcomes []models.ContactImportRowOutcome
}

func (r *importRecorder) settle(line int, status, addr string, values []string, reason string, id *uuid.UUID) {
	switch status {
	case models.ContactImportRowImported:
		r.res.Imported++
	case models.ContactImportRowUpdated:
		r.res.Updated++
	case models.ContactImportRowSkipped:
		r.res.Skipped++
	case models.ContactImportRowFailed:
		r.res.Failed++
		r.note(line, addr, values, reason)
	}
	r.outcomes = append(r.outcomes, models.ContactImportRowOutcome{
		Line: line, Status: status, Email: addr, Reason: reason, ContactID: id,
	})
}

// note records a row-level message in the response without counting the row.
func (r *importRecorder) note(line int, addr string, values []string, reason string) {
	if len(r.res.Errors) >= models.MaxContactImportReportedErrors {
		r.res.ErrorsTruncated = true
		return
	}
	r.res.Errors = append(r.res.Errors, models.ContactImportRowError{
		Line: line, Email: addr, Values: values, Reason: reason,
	})
}

func (s *contactService) runImport(ctx context.Context, plan *importPlan, rows []ImportRow, sink ImportSink, prior []uuid.UUID) (*models.ContactImportResult, *errx.Error) {
	startedAt := time.Now().UTC()
	// A resumed run with nothing left still owes its segments the earlier rows.
	if len(rows) == 0 && len(prior) == 0 {
		return &models.ContactImportResult{StartedAt: startedAt, EndedAt: time.Now().UTC()}, nil
	}
	if len(rows) > models.MaxContactImportRows {
		return nil, errx.New(errx.BadRequest,
			fmt.Sprintf("too many rows; max %d per import", models.MaxContactImportRows))
	}

	parsed := plan.parseRows(rows)

	// Resolve every category title the file mentions in one round trip,
	// creating the ones the workspace doesn't have yet.
	var allTitles []string
	for i := range parsed {
		allTitles = append(allTitles, parsed[i].categories...)
	}
	if len(allTitles) > 0 {
		titleToID, xerr := s.contactRepository.ResolveCategoryNames(ctx, plan.orgID, plan.uid, allTitles)
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

	// Existing contacts are the workspace's, not the importing member's: a
	// teammate's contact is the same person and must not be created twice.
	emails := make([]string, 0, len(parsed))
	for i := range parsed {
		if parsed[i].ok {
			emails = append(emails, parsed[i].contact.Email)
		}
	}
	existing, elsewhere, xerr := s.contactRepository.ImportLookup(ctx, plan.orgID, plan.uid, emails)
	if xerr != nil {
		return nil, xerr
	}

	quality := assessRows(parsed)
	res := &models.ContactImportResult{
		Total:     len(parsed),
		StartedAt: startedAt,
		Errors:    make([]models.ContactImportRowError, 0),
		Quality:   toImportQuality(quality),
	}

	// Ask about the plan ceiling once for the whole batch. Per chunk it would
	// report the same plan problem 500 times, which is what the row-level
	// error list is explicitly not for.
	if xerr := s.checkContactLimit(ctx, plan.userID, countNew(parsed, existing, elsewhere)); xerr != nil {
		return nil, xerr
	}

	// A file or sheet is a bulk arrival, not fifty thousand "new contact"
	// moments: the per-contact event stays quiet so automations and webhooks
	// are not flooded by a single import.
	ctx = WithoutCreatedEvents(ctx)

	touched := append(make([]uuid.UUID, 0, len(prior)+len(parsed)), prior...)
	for start := 0; start < len(parsed); start += importChunkSize {
		if sink != nil && sink.Cancelled(ctx) {
			break
		}
		end := min(start+importChunkSize, len(parsed))
		rec := &importRecorder{res: res}
		touched = s.applyImportChunk(ctx, plan, parsed[start:end], existing, elsewhere, rec, touched)
		if sink != nil {
			if err := sink.Settle(ctx, rec.outcomes); err != nil {
				log.Error().Err(err).Str("organization_id", plan.orgID.String()).Msg("could not record contact import progress")
				return nil, errx.InternalError()
			}
		}
	}

	// noteImport records a message about the whole import rather than one row.
	// It goes to the front and is never dropped by the per-row cap: a file full
	// of bad addresses must not push out the one note explaining why the rows
	// that DID import are not in the segment they were imported into.
	noteImport := func(reason string) {
		res.Errors = append([]models.ContactImportRowError{{Reason: reason}}, res.Errors...)
		if len(res.Errors) > models.MaxContactImportReportedErrors {
			res.Errors = res.Errors[:models.MaxContactImportReportedErrors]
			res.ErrorsTruncated = true
		}
	}

	// Segment membership last, once every touched row exists. A failed write
	// is a note, not a failed import: the contacts themselves are in, and the
	// result says the pin did not land so the UI does not claim it did.
	if len(plan.segmentIDs) > 0 && len(touched) > 0 {
		pinned, failedPins, firstReason := true, 0, ""
		for _, segID := range plan.segmentIDs {
			if _, xerr := s.segmentLinker.SetMembers(ctx, plan.orgID, segID, touched, models.SegmentMemberInclude); xerr != nil {
				pinned, failedPins = false, failedPins+1
				if firstReason == "" {
					firstReason = xerr.Message
				}
			}
		}
		// One note for the whole pin, however many segments were targeted.
		if failedPins == 1 {
			noteImport("imported contacts could not be added to a segment: " + firstReason)
		} else if failedPins > 1 {
			noteImport(fmt.Sprintf("imported contacts could not be added to %d segments: %s", failedPins, firstReason))
		}
		res.SegmentsPinned = &pinned
	}

	res.EndedAt = time.Now().UTC()

	s.updateListQuality(ctx, plan.orgID, quality)

	if len(touched) > len(prior) {
		s.publishContactsReload(ctx, plan.userID, "contacts:import")
		// Covers the Google Sheets sync too: it commits through this path.
		s.wakeCampaigns(ctx, plan.orgID, plan.campaignIDs)
		s.syncSegmentCampaigns(ctx, plan.orgID)
	}
	return res, nil
}

// countNew is how many rows would create a contact.
func countNew(parsed []pendingRow, existing map[string]uuid.UUID, elsewhere map[string]bool) int {
	n := 0
	for i := range parsed {
		if !parsed[i].ok {
			continue
		}
		addr := parsed[i].contact.Email
		if _, have := existing[addr]; !have && !elsewhere[addr] {
			n++
		}
	}
	return n
}

// applyImportChunk writes one chunk: new contacts in one batch, updates in
// one transaction, then campaign and category links grouped. It returns
// touched with every contact the chunk created, updated or linked appended.
func (s *contactService) applyImportChunk(
	ctx context.Context,
	plan *importPlan,
	chunk []pendingRow,
	existing map[string]uuid.UUID,
	elsewhere map[string]bool,
	rec *importRecorder,
	touched []uuid.UUID,
) []uuid.UUID {
	var inserts, updates []pendingRow
	var links []linkTarget
	for _, p := range chunk {
		addr := p.contact.Email
		id, have := existing[addr]
		switch {
		case p.dupOf > 0:
			rec.settle(p.line, models.ContactImportRowSkipped, addr, nil, fmt.Sprintf("same address as line %d", p.dupOf), nil)
		case !p.ok:
			rec.settle(p.line, models.ContactImportRowFailed, p.rawEmail, p.raw, p.errMsg, nil)
		case !have && elsewhere[addr]:
			rec.settle(p.line, models.ContactImportRowFailed, addr, p.raw, reasonElsewhere, nil)
		case !have:
			// SubscribedDefault is what a NEW contact inherits. An update must
			// never touch the flag, or re-importing a list would resubscribe
			// everyone who had opted out.
			if p.contact.Subscribed == nil {
				sub := plan.subscribedDefault
				p.contact.Subscribed = &sub
			}
			p.contact.Source, p.contact.SourceDetail = plan.opts.Source, plan.opts.SourceDetail
			inserts = append(inserts, p)
		case plan.dedup == models.ContactImportDedupSkip:
			// "Skip" means "don't touch their fields", not "leave them out of
			// the list": they still join the import's campaigns and categories.
			links = append(links, linkTarget{row: p, contactID: id, status: models.ContactImportRowSkipped})
		default:
			// create_duplicate cannot create a second row under the unique
			// index, so it updates rather than losing the file's data.
			updates = append(updates, p)
		}
	}

	if len(inserts) > 0 {
		batch := make([]models.AddContact, len(inserts))
		for i := range inserts {
			batch[i] = inserts[i].contact
		}
		created, xerr := s.contactRepository.Add(ctx, plan.userID, plan.orgID, batch)
		if xerr != nil && len(batch) > 1 {
			// One bad row must not fail the rows around it: retry them singly.
			for i := range batch {
				one, oneErr := s.contactRepository.Add(ctx, plan.userID, plan.orgID, batch[i:i+1])
				touched = settleInsert(rec, inserts[i], one, oneErr, touched)
			}
		} else {
			for i := range inserts {
				var one []models.Contact
				if xerr == nil && i < len(created) {
					one = created[i : i+1]
				}
				touched = settleInsert(rec, inserts[i], one, xerr, touched)
			}
		}
	}

	if len(updates) > 0 {
		batch := make([]repository.ContactImportUpdate, len(updates))
		for i, p := range updates {
			batch[i] = repository.ContactImportUpdate{ID: existing[p.contact.Email], Contact: p.contact}
		}
		found, xerr := s.contactRepository.ImportUpdate(ctx, plan.orgID, batch)
		for i, p := range updates {
			ok, rowErr := false, xerr
			if xerr != nil && len(batch) > 1 {
				var one []bool
				one, rowErr = s.contactRepository.ImportUpdate(ctx, plan.orgID, batch[i:i+1])
				ok = rowErr == nil && one[0]
			} else if xerr == nil {
				ok = found[i]
			}
			switch {
			case rowErr != nil:
				rec.settle(p.line, models.ContactImportRowFailed, p.contact.Email, p.raw, rowErr.Message, nil)
			case !ok:
				rec.settle(p.line, models.ContactImportRowFailed, p.contact.Email, p.raw, reasonDeleted, nil)
			default:
				links = append(links, linkTarget{row: p, contactID: batch[i].ID, status: models.ContactImportRowUpdated})
			}
		}
	}

	// One BulkUpdate per distinct (campaigns, categories) set, so the common
	// case (one campaign, one category list for the whole file) is one write.
	linkErr := make([]string, len(links))
	for _, group := range groupLinks(links) {
		if len(group.campaigns) == 0 && len(group.categories) == 0 {
			continue
		}
		if _, xerr := s.contactRepository.BulkUpdate(ctx, plan.userID, plan.orgID, &models.BulkEditContactsData{
			ContactSelection: models.ContactSelection{Contacts: group.contactIDs},
			AddCampaigns:     group.campaigns,
			AddCategories:    group.categories,
		}); xerr != nil {
			for _, i := range group.members {
				linkErr[i] = xerr.Message
			}
		}
	}
	for i, t := range links {
		id := t.contactID
		addr := t.row.contact.Email
		switch {
		case t.status == models.ContactImportRowSkipped && linkErr[i] != "":
			// The rows around it are fine; only this link failed, so the row
			// moves from skipped to failed rather than discarding the chunk.
			rec.settle(t.row.line, models.ContactImportRowFailed, addr, nil,
				"contact already existed but could not be added to the campaign: "+linkErr[i], &id)
		case t.status == models.ContactImportRowSkipped:
			rec.settle(t.row.line, models.ContactImportRowSkipped, addr, nil, reasonAlreadyContact, &id)
			touched = append(touched, id)
		default:
			rec.settle(t.row.line, models.ContactImportRowUpdated, addr, nil, "", &id)
			touched = append(touched, id)
			if linkErr[i] != "" {
				// Non-fatal: the contact was updated, only the link failed.
				rec.note(t.row.line, addr, nil, "contact updated but campaign link failed: "+linkErr[i])
			}
		}
	}
	return touched
}

// settleInsert records one row of an insert batch. created holds the row's
// contact first; an upsert that met an existing row is an update, not a create.
func settleInsert(rec *importRecorder, p pendingRow, created []models.Contact, xerr *errx.Error, touched []uuid.UUID) []uuid.UUID {
	if xerr != nil || len(created) == 0 {
		reason := "the contact could not be saved"
		if xerr != nil {
			reason = xerr.Message
		}
		rec.settle(p.line, models.ContactImportRowFailed, p.contact.Email, p.raw, reason, nil)
		return touched
	}
	id := created[0].ID
	status := models.ContactImportRowImported
	if !created[0].IsNew {
		status = models.ContactImportRowUpdated
	}
	rec.settle(p.line, status, p.contact.Email, nil, "", &id)
	return append(touched, id)
}

// AnalyzeImport reads rows through a mapping and reports what an import would
// do with them, without writing anything.
func (s *contactService) AnalyzeImport(ctx context.Context, userID string, orgID uuid.UUID, rows []ImportRow, mapping []models.ContactImportColumnMapping) (*models.ContactImportAnalysis, *errx.Error) {
	if len(mapping) == 0 {
		return nil, errx.New(errx.BadRequest, "no column mapping provided")
	}
	uid, perr := uuid.Parse(userID)
	if perr != nil {
		return nil, errx.ErrUuid
	}
	columns, xerr := resolveMapping(mapping)
	if xerr != nil {
		return nil, xerr
	}
	plan := &importPlan{userID: userID, uid: uid, orgID: orgID, dedup: models.ContactImportDedupSkip, columns: columns}
	parsed := plan.parseRows(rows)

	emails := make([]string, 0, len(parsed))
	var titles []string
	for i := range parsed {
		if parsed[i].ok {
			emails = append(emails, parsed[i].contact.Email)
			titles = append(titles, parsed[i].categories...)
		}
	}
	existing, elsewhere, xerr := s.contactRepository.ImportLookup(ctx, orgID, uid, emails)
	if xerr != nil {
		return nil, xerr
	}

	out := &models.ContactImportAnalysis{
		Rows:           len(parsed),
		InvalidSamples: []models.ContactImportRowError{},
		Quality:        toImportQuality(assessRows(parsed)),
	}
	sample := func(line int, addr string, reason string) {
		if len(out.InvalidSamples) < models.MaxContactImportAnalysisSamples {
			out.InvalidSamples = append(out.InvalidSamples, models.ContactImportRowError{Line: line, Email: addr, Reason: reason})
		}
	}
	for _, p := range parsed {
		_, have := existing[p.contact.Email]
		switch {
		case p.dupOf > 0:
			out.DuplicatesInFile++
		case !p.ok:
			out.Invalid++
			sample(p.line, p.rawEmail, p.errMsg)
		case have:
			out.Existing++
		case elsewhere[p.contact.Email]:
			out.Conflicts++
			sample(p.line, p.contact.Email, reasonElsewhere)
		default:
			out.New++
		}
	}

	// What would refuse the whole import is said now, not after the upload.
	if xerr := repository.ValidateImportCategoryNames(titles); xerr != nil {
		out.Problem = xerr.Message
	} else if xerr := s.checkContactLimit(ctx, userID, out.New); xerr != nil {
		out.Problem = xerr.Message
	}
	return out, nil
}

// ValidateSegmentTargets exposes resolveSegmentIDs' verdict without running an
// import, so a saved Google Sheets source is rejected at save time.
func (s *contactService) ValidateSegmentTargets(ctx context.Context, orgID uuid.UUID, ids []string) *errx.Error {
	_, xerr := s.resolveSegmentIDs(ctx, orgID, ids, false)
	return xerr
}

// parseSegmentIDs validates the import's target segments: well-formed ids
// that exist in the org, deduplicated.
func (s *contactService) parseSegmentIDs(ctx context.Context, orgID uuid.UUID, raw []string) ([]uuid.UUID, *errx.Error) {
	return s.resolveSegmentIDs(ctx, orgID, raw, false)
}

// resolveSegmentIDs is parseSegmentIDs with the recurring-source relaxation:
// skipMissing drops an id whose segment is gone instead of failing the run.
func (s *contactService) resolveSegmentIDs(ctx context.Context, orgID uuid.UUID, raw []string, skipMissing bool) ([]uuid.UUID, *errx.Error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if s.segmentLinker == nil {
		return nil, errx.New(errx.BadRequest, "segments are not available")
	}
	out := make([]uuid.UUID, 0, len(raw))
	seen := map[uuid.UUID]bool{}
	for _, r := range raw {
		id, err := uuid.Parse(strings.TrimSpace(r))
		if err != nil {
			if skipMissing {
				continue
			}
			return nil, errx.New(errx.BadRequest, "invalid segment id")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, xerr := s.segmentLinker.Get(ctx, orgID, id); xerr != nil {
			if xerr.Code == errx.NotFound {
				if skipMissing {
					continue
				}
				return nil, errx.New(errx.BadRequest, "a selected segment does not exist")
			}
			return nil, xerr
		}
		out = append(out, id)
	}
	return out, nil
}

// linkTarget is an existing contact that must join the import's campaigns and
// categories, whether or not its own fields were changed.
type linkTarget struct {
	row       pendingRow
	contactID uuid.UUID
	status    string
}

type linkGroup struct {
	campaigns  []string
	categories []string
	contactIDs []string
	// members index the targets in the slice groupLinks was given.
	members []int
}

func groupLinks(targets []linkTarget) []linkGroup {
	byKey := map[string]*linkGroup{}
	order := make([]string, 0, 1)
	for i, t := range targets {
		campaigns, categories := t.row.contact.Campaigns, t.row.contact.Categories
		key := strings.Join(campaigns, ",") + "|" + strings.Join(categories, ",")
		g, ok := byKey[key]
		if !ok {
			g = &linkGroup{campaigns: campaigns, categories: categories}
			byKey[key] = g
			order = append(order, key)
		}
		g.contactIDs = append(g.contactIDs, t.contactID.String())
		g.members = append(g.members, i)
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

// parseSpreadsheet turns an uploaded file into rows, up to one past the row cap.
func parseSpreadsheet(r io.Reader, filename string) ([][]string, string, *errx.Error) {
	rows, kind, err := spreadsheet.Parse(r, filename, models.MaxContactImportRows+1)
	if err != nil {
		return nil, kind, errx.New(errx.BadRequest, err.Error())
	}
	return rows, kind, nil
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

// SuggestMapping picks a target for each column from its header and sample:
// a standard field, a verification verdict, or a custom field the workspace
// already has (existingKeys, most used first). Anything else becomes ignore,
// better than inventing a custom-field key the user didn't ask for. Every
// importer that shows a column mapper calls this, so they suggest alike.
func SuggestMapping(headers []string, sample [][]string, existingKeys []string) []models.ContactImportColumnMapping {
	return suggestMapping(headers, sample, importmap.Shapes(len(headers), sample), existingKeys)
}

func suggestMapping(headers []string, sample [][]string, shapes []importmap.Shape, existingKeys []string) []models.ContactImportColumnMapping {
	out := make([]models.ContactImportColumnMapping, len(headers))
	for i, h := range headers {
		out[i] = guessTarget(i, h)
		if out[i].Target != models.ContactImportTargetIgnore {
			continue
		}
		// A verdict column from another verification service: the header
		// says so, or every sample value is a word one of them writes.
		provider, headerSays := emailverify.IsStatusHeader(h)
		values := make([]string, 0, len(sample))
		for _, row := range sample {
			if i < len(row) {
				values = append(values, row[i])
			}
		}
		detected, valuesSay := emailverify.DetectVocabulary(values)
		if !headerSays && !valuesSay {
			continue
		}
		// A generic header ("status") only counts when the values agree,
		// otherwise a CRM's deal-stage column would be read as a verdict.
		if headerSays && provider == "" && !valuesSay {
			continue
		}
		if provider == "" {
			provider = detected
		}
		if provider == emailverify.ProviderBuiltin {
			provider = ""
		}
		out[i] = models.ContactImportColumnMapping{Index: i, Target: models.ContactImportTargetVerificationStatus, VerificationProvider: provider}
	}
	// Email first: a custom field must never take the one column an import
	// cannot go without.
	matchEmailByValues(out, shapes)
	matchExistingCustomFields(out, headers, existingKeys)
	return out
}

// matchEmailByValues maps the first column of addresses to Email when no
// header named it, so a file with "Work contact" or no header row at all
// still has the one column an import cannot go without.
func matchEmailByValues(out []models.ContactImportColumnMapping, shapes []importmap.Shape) {
	for _, m := range out {
		if m.Target == models.ContactImportTargetEmail {
			return
		}
	}
	for i := range out {
		if i < len(shapes) && out[i].Target == models.ContactImportTargetIgnore && shapes[i] == importmap.ShapeEmail {
			out[i] = models.ContactImportColumnMapping{Index: i, Target: models.ContactImportTargetEmail}
			return
		}
	}
}

// matchExistingCustomFields maps each still-ignored column whose header names
// an existing custom field onto that field's stored spelling: the exact name
// first, then one differing only in case or separators, so "industry" in a
// file lands on "Industry" instead of starting a second field. Each field is
// claimed by one column at most. Mirrored by matchExistingKey in the web app.
func matchExistingCustomFields(out []models.ContactImportColumnMapping, headers, existingKeys []string) {
	if len(existingKeys) == 0 {
		return
	}
	exact := make(map[string]bool, len(existingKeys))
	byFold := make(map[string]string, len(existingKeys))
	for _, k := range existingKeys {
		exact[k] = true
		f := FoldCustomFieldKey(k)
		if _, taken := byFold[f]; f != "" && !taken {
			byFold[f] = k
		}
	}
	claimed := make(map[string]bool, len(out))
	for i, h := range headers {
		if out[i].Target != models.ContactImportTargetIgnore {
			continue
		}
		key := utils.NormalizeJSONKey(h)
		if !exact[key] {
			var ok bool
			if key, ok = byFold[FoldCustomFieldKey(h)]; !ok {
				continue
			}
		}
		if claimed[key] {
			continue
		}
		claimed[key] = true
		out[i] = models.ContactImportColumnMapping{Index: i, Target: models.ContactImportTargetCustom, CustomKey: key}
	}
}

// FoldCustomFieldKey reduces a header or field name to lowercase letters and
// digits, the form two spellings of one field ("Company URL", "company_url")
// share.
func FoldCustomFieldKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
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
		case models.ContactImportTargetVerificationStatus:
			// Lenient on purpose: a stray "n/a" must not drop the lead.
			if _, ok := emailverify.NormalizeExternal(col.customKey, val); ok {
				ac.VerificationStatus = val
				ac.VerificationProvider = col.customKey
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

// updateListQuality files the finding on the running unusable share of
// everything imported, not on the newest file: a small clean upload must not
// retract a large bad list whose addresses are still stored.
func (s *contactService) updateListQuality(ctx context.Context, orgID uuid.UUID, quality listquality.Summary) {
	// A list too small to measure says nothing either way.
	if s.orgRisk == nil || quality.Total < listquality.MinSample {
		return
	}
	risk, xerr := s.orgRisk.Get(ctx, orgID)
	if xerr != nil {
		// Without the running counts this import cannot be judged, and guessing
		// would either clear a real finding or invent one.
		log.Warn().Str("organization_id", orgID.String()).Msg("could not read the posture to fold in the import")
		return
	}

	imported, unusable := quality.Total, quality.Malformed+quality.Disposable
	if orgrisk.HasSignal(risk, listQualitySignal) {
		prior, priorBad := orgrisk.EvidenceInt(risk, listQualitySignal, "imported"),
			orgrisk.EvidenceInt(risk, listQualitySignal, "unusable")
		if prior == 0 {
			// Filed before these counts travelled with it: assume the smallest
			// list that could have flagged the workspace, at its worst.
			prior, priorBad = listquality.MinSample, listquality.MinSample
		}
		imported, unusable = imported+prior, unusable+priorBad
	}

	share := float64(unusable) / float64(imported) * 100
	if share < listquality.FlagSharePct {
		if _, err := s.orgRisk.ClearSignal(ctx, orgID, listQualitySignal); err != nil {
			log.Warn().Str("organization_id", orgID.String()).Msg("could not retract the import quality signal")
		}
		return
	}
	if _, err := s.orgRisk.RecordSignal(ctx, orgID, orgrisk.Signal{
		Key:    listQualitySignal,
		Weight: importRiskWeight(share),
		Detail: fmt.Sprintf("%.0f%% of the %d addresses imported into this workspace are unusable", share, imported),
		// Substantive: this is what the workspace intends to mail.
		Class:    orgrisk.ClassSubstantive,
		Evidence: map[string]any{"imported": imported, "unusable": unusable},
	}); err != nil {
		log.Warn().Str("organization_id", orgID.String()).Msg("could not record the import quality signal")
	}
}

// listQualitySignal is the detector key the running assessment is filed under.
const listQualitySignal = "list_quality"

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
