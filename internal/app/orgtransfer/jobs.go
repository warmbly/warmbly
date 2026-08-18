package orgtransfer

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Transfers run in the process that accepted the request rather than through a
// queue, for one reason: the passphrase must never be written down. It lives in
// this process's memory for the length of the run and nowhere else — not in the
// job row, not in Redis, not in an environment variable.
//
// The cost of that is an orphaned job if the process restarts mid-run, which
// staleTransferDeadline cleans up.
const (
	staleTransferDeadline = 6 * time.Hour
	jobHistoryLimit       = 25
)

// ---------- export ----------

func (s *service) RequestExport(
	ctx context.Context,
	orgID uuid.UUID,
	userID *uuid.UUID,
	req *models.CreateOrgExportRequest,
) (*models.OrgExportJob, *errx.Error) {
	if req == nil {
		req = &models.CreateOrgExportRequest{}
	}
	if req.IncludeSecrets {
		if err := ValidatePassphrase(req.Passphrase); err != nil {
			return nil, errx.New(errx.BadRequest, err.Error())
		}
		if s.creds == nil {
			return nil, errx.New(errx.BadRequest,
				"This instance has no CREDENTIALS_ENCRYPTION_KEY, so there are no mailbox credentials to carry. Export without credentials instead.")
		}
	} else {
		// A passphrase without the flag is a mistake worth surfacing rather
		// than quietly producing an archive with no credentials in it.
		req.Passphrase = ""
	}
	if s.blobs == nil {
		return nil, errx.New(errx.BadRequest, "Object storage is not configured on this instance, so an archive cannot be stored.")
	}

	active, err := s.repo.HasActiveExport(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	if active {
		return nil, errx.New(errx.Conflict, "An export is already running for this workspace. Wait for it to finish.")
	}

	job := &models.OrgExportJob{
		OrganizationID: orgID,
		RequestedBy:    userID,
		Groups:         NormalizeGroups(req.Groups),
		IncludeSecrets: req.IncludeSecrets,
	}
	if err := s.repo.CreateExportJob(ctx, job); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	// Detached context: the archive keeps building after the request returns.
	passphrase := req.Passphrase
	go func() {
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), staleTransferDeadline)
		defer cancel()
		if err := s.runExport(runCtx, job, passphrase); err != nil {
			sentry.CaptureException(err)
			_ = s.repo.FailExportJob(context.WithoutCancel(ctx), job.ID, err.Error())
		}
	}()

	return job, nil
}

// runExport builds the archive and records the outcome on the job.
func (s *service) runExport(ctx context.Context, job *models.OrgExportJob, passphrase string) error {
	if err := s.repo.UpdateExportProgress(ctx, job.ID, 1, "starting"); err != nil {
		return err
	}

	// Spool to disk rather than memory: a full workspace archive is routinely
	// larger than anything worth holding in RAM, and blob storage wants a
	// length up front anyway.
	tmp, err := os.CreateTemp("", "warmbly-export-*.zip")
	if err != nil {
		return fmt.Errorf("create temporary archive: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	digest := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(tmp, digest)}

	manifest, err := s.ExportTo(ctx, job.OrganizationID, ExportOptions{
		Groups:     job.Groups,
		Passphrase: passphrase,
	}, counter, func(percent int, stage string) {
		_ = s.repo.UpdateExportProgress(ctx, job.ID, percent, stage)
	})
	if err != nil {
		return err
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	key := archiveObjectKey(job.OrganizationID, job.ID)
	if err := s.blobs.Put(ctx, key, tmp, "application/zip"); err != nil {
		return fmt.Errorf("store archive: %w", err)
	}

	counts := make(map[string]int64, len(manifest.Tables))
	for _, t := range manifest.Tables {
		counts[t.Name] = t.Rows
	}
	return s.repo.CompleteExportJob(ctx, job.ID, key, counter.n,
		hex.EncodeToString(digest.Sum(nil)), counts, time.Now().Add(models.OrgExportRetention))
}

func (s *service) GetExport(ctx context.Context, orgID, id uuid.UUID) (*models.OrgExportJob, *errx.Error) {
	job, err := s.repo.GetExportJob(ctx, orgID, id)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	if job == nil {
		return nil, errx.ErrNotFound
	}
	return job, nil
}

func (s *service) ListExports(ctx context.Context, orgID uuid.UUID) ([]models.OrgExportJob, *errx.Error) {
	jobs, err := s.repo.ListExportJobs(ctx, orgID, jobHistoryLimit)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	return jobs, nil
}

func (s *service) OpenExport(ctx context.Context, orgID, id uuid.UUID) (io.ReadCloser, *models.OrgExportJob, *errx.Error) {
	job, xerr := s.GetExport(ctx, orgID, id)
	if xerr != nil {
		return nil, nil, xerr
	}
	switch job.Status {
	case models.OrgTransferStatusCompleted:
	case models.OrgTransferStatusExpired:
		return nil, nil, errx.New(errx.NotFound, "This archive has expired. Run a new export.")
	default:
		return nil, nil, errx.New(errx.Conflict, "This export is not finished yet.")
	}
	if job.ArchiveKey == nil || s.blobs == nil {
		return nil, nil, errx.New(errx.NotFound, "This archive is no longer stored.")
	}

	body, err := s.blobs.Get(ctx, *job.ArchiveKey)
	if err != nil {
		sentry.CaptureException(err)
		return nil, nil, errx.New(errx.NotFound, "This archive could not be read from storage.")
	}
	return body, job, nil
}

func (s *service) DeleteExport(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	key, err := s.repo.DeleteExportJob(ctx, orgID, id)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}
	if key != "" && s.blobs != nil {
		if err := s.blobs.Delete(ctx, key); err != nil {
			sentry.CaptureException(err)
		}
	}
	return nil
}

// ---------- import ----------

func (s *service) Preflight(
	ctx context.Context,
	orgID uuid.UUID,
	archive ReaderAtSizer,
	passphrase string,
) (*models.OrgImportPreflight, *errx.Error) {
	zr, err := zip.NewReader(archive, archive.Size())
	if err != nil {
		return nil, errx.New(errx.BadRequest, "That file is not a readable archive.")
	}
	entries := indexEntries(zr)

	manifest, err := readManifest(entries)
	if err != nil {
		return nil, errx.New(errx.BadRequest, err.Error())
	}
	if err := manifest.Validate(); err != nil {
		return nil, errx.New(errx.BadRequest, err.Error())
	}

	out := &models.OrgImportPreflight{
		Archive:   manifest.Info(),
		Conflicts: map[string]int64{},
		Warnings:  []string{},
	}

	if manifest.Secrets != nil && passphrase != "" {
		if _, err := DeriveArchiveKey(passphrase, manifest.Secrets); err != nil {
			if errors.Is(err, ErrWrongPassphrase) {
				return nil, errx.New(errx.BadRequest, "That passphrase does not open this archive.")
			}
			return nil, errx.New(errx.BadRequest, err.Error())
		}
		out.SecretsUnsealed = true
	}
	switch {
	case manifest.Secrets == nil:
		out.Warnings = append(out.Warnings, "This archive was exported without credentials, so every mailbox and integration will need reconnecting.")
	case !out.SecretsUnsealed:
		out.Warnings = append(out.Warnings, "Enter the export passphrase to bring mailbox credentials across; without it every mailbox will need reconnecting.")
	case s.creds == nil:
		out.Warnings = append(out.Warnings, "This instance has no CREDENTIALS_ENCRYPTION_KEY, so mailbox credentials cannot be stored here.")
	}

	// Who lands where.
	emails := make([]string, 0, len(manifest.Members))
	for _, m := range manifest.Members {
		emails = append(emails, lowerTrim(m.Email))
	}
	known, rerr := s.repo.ResolveUsersByEmail(ctx, emails)
	if rerr != nil {
		sentry.CaptureException(rerr)
		return nil, errx.InternalError()
	}
	for _, m := range manifest.Members {
		if _, ok := known[lowerTrim(m.Email)]; !ok {
			out.UnknownMembers = append(out.UnknownMembers, m)
		}
	}

	// What already exists here, and what this instance cannot take.
	for _, mt := range manifest.Tables {
		t, known := TableByName[mt.Name]
		if !known || t.ImportSkip {
			continue
		}
		destCols, err := s.repo.TableColumns(ctx, mt.Name)
		if err != nil {
			sentry.CaptureException(err)
			return nil, errx.InternalError()
		}
		if len(destCols) == 0 {
			out.SkippedTables = append(out.SkippedTables, mt.Name)
			continue
		}
		if mt.Rows == 0 {
			continue
		}
		pk, err := s.repo.PrimaryKeyColumns(ctx, mt.Name)
		if err != nil || len(pk) == 0 {
			continue
		}
		entry, ok := entries[dataPath(mt.Name)]
		if !ok {
			continue
		}
		n, err := s.countConflicts(ctx, mt.Name, pk, entry)
		if err != nil {
			// A conflict count is advisory; failing the whole preflight over
			// one unreadable table helps nobody.
			continue
		}
		if n > 0 {
			out.Conflicts[mt.Name] = n
		}
	}

	if len(out.SkippedTables) > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"%d table(s) in this archive do not exist on this instance and will be skipped. It was probably exported from a newer release.",
			len(out.SkippedTables)))
	}
	return out, nil
}

// countConflicts samples a table's keys to report how many rows already exist.
// It reads a bounded prefix: the number is there to warn, not to be exact, and
// scanning a million-row inbox table twice for a preview is not worth it.
const conflictSampleRows = 2000

func (s *service) countConflicts(ctx context.Context, table string, pk []string, entry *zip.File) (int64, error) {
	rc, err := entry.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	rows, err := readRows(rc, conflictSampleRows)
	if err != nil {
		return 0, err
	}
	return s.repo.CountExisting(ctx, table, pk, rows)
}

func (s *service) RequestImport(
	ctx context.Context,
	orgID uuid.UUID,
	userID *uuid.UUID,
	archive ReaderAtSizer,
	req *models.CreateOrgImportRequest,
) (*models.OrgImportJob, *errx.Error) {
	if req == nil {
		req = &models.CreateOrgImportRequest{}
	}
	if req.ConflictStrategy == "" {
		req.ConflictStrategy = models.OrgImportConflictSkip
	}
	if req.ConflictStrategy != models.OrgImportConflictSkip && req.ConflictStrategy != models.OrgImportConflictOverwrite {
		return nil, errx.New(errx.BadRequest, "conflict_strategy must be skip or overwrite")
	}

	active, err := s.repo.HasActiveImport(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	if active {
		return nil, errx.New(errx.Conflict, "An import is already running for this workspace. Wait for it to finish.")
	}

	zr, err := zip.NewReader(archive, archive.Size())
	if err != nil {
		return nil, errx.New(errx.BadRequest, "That file is not a readable archive.")
	}
	manifest, err := readManifest(indexEntries(zr))
	if err != nil {
		return nil, errx.New(errx.BadRequest, err.Error())
	}
	if err := manifest.Validate(); err != nil {
		return nil, errx.New(errx.BadRequest, err.Error())
	}
	// Fail on a bad passphrase now, not two hours into applying rows.
	if manifest.Secrets != nil && req.Passphrase != "" {
		if _, err := DeriveArchiveKey(req.Passphrase, manifest.Secrets); err != nil {
			return nil, errx.New(errx.BadRequest, "That passphrase does not open this archive.")
		}
	}

	size := archive.Size()
	job := &models.OrgImportJob{
		OrganizationID:   orgID,
		RequestedBy:      userID,
		ArchiveBytes:     &size,
		SourceManifest:   manifest.Info(),
		Groups:           NormalizeGroups(req.Groups),
		ConflictStrategy: req.ConflictStrategy,
	}

	// Keep the upload until the import succeeds, so a failure can be retried
	// without asking someone to re-upload several gigabytes.
	if s.blobs != nil {
		key := uploadObjectKey(orgID, uuid.New())
		if err := s.blobs.Put(ctx, key, io.NewSectionReader(archive, 0, size), "application/zip"); err != nil {
			sentry.CaptureException(err)
			return nil, errx.New(errx.Internal, "The archive could not be stored for import.")
		}
		job.ArchiveKey = &key
	}

	if err := s.repo.CreateImportJob(ctx, job); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	actor := uuid.Nil
	if userID != nil {
		actor = *userID
	}
	passphrase := req.Passphrase
	groups := job.Groups
	conflict := job.ConflictStrategy

	go func() {
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), staleTransferDeadline)
		defer cancel()

		// The caller spooled the upload somewhere and handed us ownership; it
		// is only certainly finished with once the import is.
		if closer, ok := archive.(io.Closer); ok {
			defer func() { _ = closer.Close() }()
		}

		_ = s.repo.UpdateImportProgress(runCtx, job.ID, 1, "starting")
		result, err := s.ImportFrom(runCtx, orgID, archive, ImportOptions{
			Groups:      groups,
			Conflict:    conflict,
			Passphrase:  passphrase,
			ActorUserID: actor,
		}, func(percent int, stage string) {
			_ = s.repo.UpdateImportProgress(runCtx, job.ID, percent, stage)
		})
		if err != nil {
			sentry.CaptureException(err)
			_ = s.repo.FailImportJob(runCtx, job.ID, err.Error())
			return
		}
		_ = s.repo.CompleteImportJob(runCtx, job.ID, result.RowCounts, result.Warnings)

		// The upload has done its job; it holds a whole workspace, so it does
		// not linger past the point it could still be useful.
		if job.ArchiveKey != nil && s.blobs != nil {
			if derr := s.blobs.Delete(runCtx, *job.ArchiveKey); derr != nil {
				sentry.CaptureException(derr)
			}
		}
	}()

	return job, nil
}

func (s *service) GetImport(ctx context.Context, orgID, id uuid.UUID) (*models.OrgImportJob, *errx.Error) {
	job, err := s.repo.GetImportJob(ctx, orgID, id)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	if job == nil {
		return nil, errx.ErrNotFound
	}
	return job, nil
}

func (s *service) ListImports(ctx context.Context, orgID uuid.UUID) ([]models.OrgImportJob, *errx.Error) {
	jobs, err := s.repo.ListImportJobs(ctx, orgID, jobHistoryLimit)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	return jobs, nil
}

// ---------- maintenance ----------

// PurgeExpiredExports deletes archives past their retention window and fails
// any transfer whose process died mid-run.
func (s *service) PurgeExpiredExports(ctx context.Context) (int, error) {
	expired, err := s.repo.ListExpiredExports(ctx, time.Now(), 200)
	if err != nil {
		return 0, err
	}
	var purged int
	for i := range expired {
		job := &expired[i]
		if job.ArchiveKey != nil && s.blobs != nil {
			if derr := s.blobs.Delete(ctx, *job.ArchiveKey); derr != nil {
				sentry.CaptureException(derr)
			}
		}
		if err := s.repo.MarkExportExpired(ctx, job.ID); err != nil {
			sentry.CaptureException(err)
			continue
		}
		purged++
	}

	if err := s.repo.FailStaleTransfers(ctx, time.Now().Add(-staleTransferDeadline)); err != nil {
		sentry.CaptureException(err)
	}
	return purged, nil
}

// ---------- helpers ----------

// countingWriter records how many bytes went through, so the job can report
// the archive's size without stat-ing the temporary file.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func lowerTrim(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// readRows reads up to limit newline-delimited records from an archive entry.
func readRows(r io.Reader, limit int) ([]json.RawMessage, error) {
	br := bufio.NewReaderSize(r, 256*1024)
	out := make([]json.RawMessage, 0, limit)
	for len(out) < limit {
		line, err := readLine(br)
		if len(line) > 0 {
			out = append(out, append(json.RawMessage(nil), line...))
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
	}
	return out, nil
}
