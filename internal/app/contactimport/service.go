// Package contactimport runs contact imports in the background: a file is
// uploaded once as a draft, analysed under a mapping, then applied in chunks
// by a leased runner, so a large list survives a closed tab, a proxy timeout
// and a restarted backend, and every teammate can watch it move.
package contactimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/spreadsheet"
	"github.com/warmbly/warmbly/internal/repository"
)

// Publisher tells the workspace an import moved.
type Publisher interface {
	PublishContactImportProgress(ctx context.Context, orgID, importID uuid.UUID, status string)
}

// EventDispatcher delivers the bulk_operation.* webhooks. Satisfied by webhook.Service.
type EventDispatcher interface {
	Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error)
}

// Deps are the service's collaborators.
type Deps struct {
	Repo      repository.ContactImportRepository
	Contacts  contact.ContactService
	Publisher Publisher
	Events    EventDispatcher
	// Kicked is called after an import is started, e.g. to verify new addresses.
	Kicked func()
}

// Service owns contact imports.
type Service struct {
	repo      repository.ContactImportRepository
	contacts  contact.ContactService
	publisher Publisher
	events    EventDispatcher
	kicked    func()
	kick      chan struct{}
}

func NewService(d Deps) *Service {
	return &Service{
		repo:      d.Repo,
		contacts:  d.Contacts,
		publisher: d.Publisher,
		events:    d.Events,
		kicked:    d.Kicked,
		kick:      make(chan struct{}, 1),
	}
}

var errNotFound = errx.New(errx.NotFound, "import not found")

// Create parses an upload and stores it as a draft, returning it with the
// preview the column mapper needs.
func (s *Service) Create(ctx context.Context, orgID, userID uuid.UUID, r io.Reader, filename string) (*models.ContactImport, *errx.Error) {
	active, err := s.repo.CountActive(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if active >= config.ContactImportMaxActive {
		return nil, errx.New(errx.Conflict, fmt.Sprintf(
			"this workspace already has %d imports waiting or running; finish or cancel one first", active))
	}

	// Two past the cap: one for a header row, one to tell a full file from a longer one.
	rows, format, perr := spreadsheet.Parse(r, filename, models.MaxContactImportRows+2)
	if perr != nil {
		return nil, errx.New(errx.BadRequest, perr.Error())
	}
	preview, xerr := s.contacts.BuildImportPreview(ctx, orgID, filename, format, rows)
	if xerr != nil {
		return nil, xerr
	}
	if preview.TotalRows > models.MaxContactImportRows {
		return nil, errx.New(errx.BadRequest,
			fmt.Sprintf("too many rows; max %d per import", models.MaxContactImportRows))
	}

	// A file with the headers of one imported before maps itself the same way.
	if preview.HasHeader {
		if saved, ok, err := s.repo.GetMapping(ctx, orgID, headerSignature(preview.Columns)); err == nil && ok && fits(saved, len(preview.Columns)) {
			preview.SuggestedMapping, preview.InferredColumns = saved, nil
			preview.MappingSource = models.ContactImportMappingSaved
		}
	}

	imp := &models.ContactImport{
		ID:             uuid.New(),
		OrganizationID: orgID,
		CreatedBy:      &userID,
		Filename:       filename,
		Format:         format,
		Status:         models.ContactImportDraft,
		HasHeader:      preview.HasHeader,
		Columns:        preview.Columns,
		Total:          preview.TotalRows,
		Notes:          []string{},
		Preview:        preview,
	}
	if err := s.repo.Create(ctx, imp, rows); err != nil {
		return nil, errx.InternalError()
	}
	return imp, nil
}

// SaveDraft keeps a draft's in-progress mapping and options, so leaving the
// page or reloading it resumes the import where it was. Checked on start, not here.
func (s *Service) SaveDraft(ctx context.Context, orgID, id uuid.UUID, opts *models.ContactImportCommit) (*models.ContactImport, *errx.Error) {
	if len(opts.Mapping) > maxDraftMapping {
		return nil, errx.New(errx.BadRequest, "too many column mappings")
	}
	raw, err := json.Marshal(opts)
	if err != nil {
		return nil, errx.InternalError()
	}
	saved, err := s.repo.SaveDraft(ctx, orgID, id, raw)
	if err != nil {
		return nil, errx.InternalError()
	}
	if !saved {
		imp, xerr := s.Get(ctx, orgID, id)
		if xerr != nil {
			return nil, xerr
		}
		if imp.Status != models.ContactImportDraft {
			return nil, errx.New(errx.Conflict, "this import has already started")
		}
	}
	return s.Get(ctx, orgID, id)
}

// maxDraftMapping bounds a saved draft mapping; a file has far fewer columns.
const maxDraftMapping = 2000

// Get returns an import; a finished one carries its first failed rows.
func (s *Service) Get(ctx context.Context, orgID, id uuid.UUID) (*models.ContactImport, *errx.Error) {
	imp, err := s.repo.Get(ctx, orgID, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	if imp == nil {
		return nil, errNotFound
	}
	if imp.Status.Terminal() && imp.Failed > 0 {
		failures, err := s.repo.Failures(ctx, orgID, id, models.MaxContactImportListedFailures)
		if err != nil {
			return nil, errx.InternalError()
		}
		imp.Failures = failures
	}
	return imp, nil
}

// List pages the workspace's imports, newest first. The cursor is opaque.
func (s *Service) List(ctx context.Context, orgID uuid.UUID, cursor string, limit int) (*models.ContactImportList, *errx.Error) {
	if limit < 1 {
		return nil, errx.New(errx.BadRequest, "limit must be at least 1")
	}
	var before *time.Time
	var beforeID *uuid.UUID
	if cursor != "" {
		t, id, ok := decodeCursor(cursor)
		if !ok {
			return nil, errx.New(errx.BadRequest, "invalid cursor")
		}
		before, beforeID = &t, &id
	}
	items, err := s.repo.List(ctx, orgID, before, beforeID, limit+1)
	if err != nil {
		return nil, errx.InternalError()
	}
	out := &models.ContactImportList{Data: items}
	if len(items) > limit {
		out.Data = items[:limit]
		last := out.Data[limit-1]
		next := encodeCursor(last.CreatedAt, last.ID)
		out.Pagination = models.Pagination{NextCursor: &next, HasMore: true}
	}
	return out, nil
}

// Analyze reports what a draft would do under a mapping, over the whole file.
func (s *Service) Analyze(ctx context.Context, orgID, userID, id uuid.UUID, req *models.ContactImportAnalyzeRequest) (*models.ContactImportAnalysis, *errx.Error) {
	imp, err := s.repo.Get(ctx, orgID, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	if imp == nil {
		return nil, errNotFound
	}
	if imp.Status != models.ContactImportDraft {
		return nil, errx.New(errx.Conflict, "this import has already started")
	}
	rows, xerr := s.dataRows(ctx, id, req.HasHeader)
	if xerr != nil {
		return nil, xerr
	}
	return s.contacts.AnalyzeImport(ctx, userID.String(), orgID, rows, req.Mapping)
}

// Start queues a draft with its options; started reports whether this call
// did. Starting one that has already started returns it unchanged, so a
// retried request is harmless.
func (s *Service) Start(ctx context.Context, orgID, userID, id uuid.UUID, opts *models.ContactImportCommit) (imp *models.ContactImport, started bool, xerr *errx.Error) {
	imp, err := s.repo.Get(ctx, orgID, id)
	if err != nil {
		return nil, false, errx.InternalError()
	}
	if imp == nil {
		return nil, false, errNotFound
	}
	switch imp.Status {
	case models.ContactImportDraft:
	case models.ContactImportCancelled:
		return nil, false, errx.New(errx.Conflict, "this import was cancelled; upload the file again")
	default:
		return imp, false, nil
	}
	if xerr := s.contacts.ValidateImportOptions(ctx, userID.String(), orgID, opts); xerr != nil {
		return nil, false, xerr
	}

	stored, err := s.repo.Rows(ctx, id, false)
	if err != nil {
		return nil, false, errx.InternalError()
	}
	columns := headerColumns(stored, opts.HasHeader)
	raw, err := json.Marshal(opts)
	if err != nil {
		return nil, false, errx.InternalError()
	}
	queued, err := s.repo.Queue(ctx, orgID, id, opts.HasHeader, columns, raw)
	if err != nil {
		return nil, false, errx.InternalError()
	}
	if queued {
		if opts.HasHeader {
			if err := s.repo.SaveMapping(ctx, orgID, headerSignature(columns), opts.Mapping); err != nil {
				log.Warn().Str("organization_id", orgID.String()).Msg("could not remember the contact import mapping")
			}
		}
		s.publish(ctx, orgID, id, models.ContactImportQueued)
		s.announce(ctx, orgID, id, imp.Filename, models.WebhookEventBulkOperationStarted, nil)
		s.Kick()
	}
	imp, xerr = s.Get(ctx, orgID, id)
	return imp, queued && xerr == nil, xerr
}

// Cancel stops an import. Rows already written stay written.
func (s *Service) Cancel(ctx context.Context, orgID, id uuid.UUID) (*models.ContactImport, *errx.Error) {
	cancelled, err := s.repo.Cancel(ctx, orgID, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	if cancelled {
		s.publish(ctx, orgID, id, models.ContactImportCancelled)
	}
	return s.Get(ctx, orgID, id)
}

// FailedCSV is every failed row as uploaded, under the file's own headers,
// with the reason last, so the file can be fixed and imported again.
func (s *Service) FailedCSV(ctx context.Context, orgID, id uuid.UUID) ([]byte, string, *errx.Error) {
	imp, xerr := s.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, "", xerr
	}
	failures, err := s.repo.Failures(ctx, orgID, id, models.MaxContactImportRows)
	if err != nil {
		return nil, "", errx.InternalError()
	}
	var buf bytes.Buffer
	buf.WriteString("\ufeff")
	w := csv.NewWriter(&buf)
	width := len(imp.Columns)
	for _, f := range failures {
		width = max(width, len(f.Values))
	}
	header := make([]string, 0, width+2)
	header = append(header, imp.Columns...)
	for i := len(imp.Columns); i < width; i++ {
		header = append(header, "Column "+strconv.Itoa(i+1))
	}
	for i := range header {
		header[i] = csvSafe(header[i])
	}
	_ = w.Write(append(header, "Line", "Error"))
	for _, f := range failures {
		row := make([]string, width, width+2)
		for i, v := range f.Values {
			row[i] = csvSafe(v)
		}
		_ = w.Write(append(row, strconv.Itoa(f.Line), csvSafe(f.Reason)))
	}
	w.Flush()
	name := strings.TrimSuffix(imp.Filename, fileExt(imp.Filename))
	if name == "" {
		name = "contacts"
	}
	return buf.Bytes(), name + "-failed.csv", nil
}

// dataRows reads a draft's rows as the import would see them.
func (s *Service) dataRows(ctx context.Context, id uuid.UUID, hasHeader bool) ([]contact.ImportRow, *errx.Error) {
	stored, err := s.repo.Rows(ctx, id, false)
	if err != nil {
		return nil, errx.InternalError()
	}
	out := make([]contact.ImportRow, 0, len(stored))
	for _, row := range stored {
		if hasHeader && row.Line == 1 {
			continue
		}
		out = append(out, contact.ImportRow{Line: row.Line, Cells: row.Cells})
	}
	return out, nil
}

func (s *Service) publish(ctx context.Context, orgID, id uuid.UUID, status models.ContactImportStatus) {
	if s.publisher != nil {
		s.publisher.PublishContactImportProgress(ctx, orgID, id, string(status))
	}
}

// announce delivers one bulk_operation.* webhook for a contact import.
func (s *Service) announce(ctx context.Context, orgID, id uuid.UUID, filename string, eventType models.WebhookEventType, fields map[string]any) {
	if s.events == nil {
		return
	}
	payload := map[string]any{
		"operation_id":   id.String(),
		"operation_type": "contact_import",
		"entity_type":    "contact",
		"filename":       filename,
	}
	maps.Copy(payload, fields)
	_, _ = s.events.Dispatch(ctx, orgID, eventType, payload)
}

// Kick wakes the runner now instead of at its next tick.
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Run works queued imports until ctx ends, plus the retention sweep. An import
// interrupted by a shutdown keeps its settled rows and resumes on the next pass.
func (s *Service) Run(ctx context.Context) {
	go jobrun.Loop(ctx, "contact_import_upkeep", time.Hour, true, func(ctx context.Context) error {
		return s.repo.PurgeExpired(ctx, config.ContactImportDraftHours, config.ContactImportRetentionDays)
	})
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		s.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.kick:
		}
	}
}

func (s *Service) pass(ctx context.Context) {
	lease := time.Duration(config.ContactImportLeaseSeconds) * time.Second
	for ctx.Err() == nil {
		job, err := s.repo.Claim(ctx, lease)
		if err != nil || job == nil {
			return
		}
		s.run(ctx, job, lease)
	}
}

// run applies one claimed import. Writes use a context the shutdown does not
// cancel, so a chunk in flight always settles.
func (s *Service) run(ctx context.Context, job *repository.ContactImportJob, lease time.Duration) {
	work := context.WithoutCancel(ctx)
	finish := func(status models.ContactImportStatus, res *models.ContactImportResult, msg string) {
		var quality *models.ContactImportQuality
		var pinned *bool
		notes := []string{}
		if res != nil {
			quality, pinned = res.Quality, res.SegmentsPinned
			for _, e := range res.Errors {
				if e.Line == 0 {
					notes = append(notes, e.Reason)
				}
			}
		}
		ended, err := s.repo.Finish(work, job.ID, job.Attempts, status, quality, pinned, notes, msg)
		if err != nil {
			return
		}
		s.publish(work, job.OrgID, job.ID, status)
		switch {
		case ended != status:
		case status == models.ContactImportCompleted && res != nil:
			s.announce(work, job.OrgID, job.ID, job.Filename, models.WebhookEventBulkOperationCompleted, map[string]any{
				"total": res.Total, "imported": res.Imported, "updated": res.Updated,
				"skipped": res.Skipped, "failed": res.Failed,
			})
		case status == models.ContactImportFailed:
			s.announce(work, job.OrgID, job.ID, job.Filename, models.WebhookEventBulkOperationFailed, map[string]any{"error": msg})
		}
	}

	if job.Attempts > config.ContactImportMaxAttempts {
		finish(models.ContactImportFailed, nil, "The import stopped unexpectedly several times. The rows it settled are in; upload the failed rows again.")
		return
	}
	if job.CreatedBy == nil {
		finish(models.ContactImportFailed, nil, "The member who started this import no longer has an account.")
		return
	}
	var opts models.ContactImportCommit
	if err := json.Unmarshal(job.Options, &opts); err != nil {
		finish(models.ContactImportFailed, nil, "The import's options could not be read.")
		return
	}
	opts.Source, opts.SourceDetail = models.ContactSourceImport, job.Filename

	stored, err := s.repo.Rows(work, job.ID, true)
	if err != nil {
		return
	}
	prior, err := s.repo.TouchedContacts(work, job.ID)
	if err != nil {
		return
	}
	rows := make([]contact.ImportRow, len(stored))
	for i, row := range stored {
		rows[i] = contact.ImportRow{Line: row.Line, Cells: row.Cells}
	}
	s.publish(work, job.OrgID, job.ID, models.ContactImportRunning)

	sink := &jobSink{s: s, job: job, parent: ctx, lease: lease}
	res, xerr := s.contacts.RunImport(work, job.CreatedBy.String(), job.OrgID, rows, &opts, sink, prior)
	if sink.interrupted {
		// Shutting down: the lease lapses and the next pass resumes the rest.
		return
	}
	if xerr != nil {
		finish(models.ContactImportFailed, nil, xerr.Message)
		return
	}
	finish(models.ContactImportCompleted, res, "")
	if s.kicked != nil && (res.Imported > 0 || res.Updated > 0) {
		s.kicked()
	}
}

// jobSink records a running import's chunks and stops it when it is cancelled.
type jobSink struct {
	s           *Service
	job         *repository.ContactImportJob
	parent      context.Context
	lease       time.Duration
	lastPublish time.Time
	interrupted bool
}

func (k *jobSink) Settle(ctx context.Context, outcomes []models.ContactImportRowOutcome) error {
	if err := k.s.repo.Settle(ctx, k.job.ID, outcomes); err != nil {
		return err
	}
	// Progress at most once a second, however small the chunks.
	if time.Since(k.lastPublish) >= time.Second {
		k.lastPublish = time.Now()
		k.s.publish(ctx, k.job.OrgID, k.job.ID, models.ContactImportRunning)
	}
	return nil
}

func (k *jobSink) Cancelled(ctx context.Context) bool {
	if k.parent.Err() != nil {
		k.interrupted = true
		return true
	}
	held, err := k.s.repo.Touch(ctx, k.job.ID, k.job.Attempts, k.lease)
	if err != nil {
		// A failed renewal is not a cancellation; the next chunk tries again.
		return false
	}
	return !held
}

// headerColumns names the file's columns: its first row when that is a header,
// otherwise "Column N", as wide as the widest row either way.
func headerColumns(rows []repository.ContactImportFileRow, hasHeader bool) []string {
	width := 0
	for _, row := range rows {
		width = max(width, len(row.Cells))
	}
	out := make([]string, width)
	for i := range out {
		out[i] = "Column " + strconv.Itoa(i+1)
	}
	if hasHeader && len(rows) > 0 && rows[0].Line == 1 {
		for i, cell := range rows[0].Cells {
			if c := strings.TrimSpace(cell); c != "" {
				out[i] = c
			}
		}
	}
	return out
}

// headerSignature identifies a header set however it is spaced or cased.
func headerSignature(columns []string) string {
	h := sha256.New()
	for _, c := range columns {
		h.Write([]byte(strings.ToLower(strings.TrimSpace(c))))
		h.Write([]byte{0x1f})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// fits reports whether a saved mapping still addresses only real columns.
func fits(mapping []models.ContactImportColumnMapping, columns int) bool {
	for _, m := range mapping {
		if m.Index < 0 || m.Index >= columns {
			return false
		}
	}
	return len(mapping) > 0
}

// csvSafe keeps a spreadsheet from reading a cell as a formula.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func fileExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[i:]
	}
	return ""
}

func encodeCursor(t time.Time, id uuid.UUID) string {
	return strconv.FormatInt(t.UnixMicro(), 36) + "." + id.String()
}

func decodeCursor(c string) (time.Time, uuid.UUID, bool) {
	ts, rawID, ok := strings.Cut(c, ".")
	if !ok {
		return time.Time{}, uuid.Nil, false
	}
	micros, err := strconv.ParseInt(ts, 36, 64)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	return time.UnixMicro(micros).UTC(), id, true
}
