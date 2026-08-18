// Package orgtransfer moves a whole organization between Warmbly instances.
//
// The problem it solves is concrete: someone runs a self-hosted instance, wants
// to move to the cloud (or the other way, or between two self-hosts), and needs
// their workspace to arrive intact — mailboxes, campaigns, contacts, sequences,
// suppression, CRM, inbox history, and the send state that stops a migrated
// mailbox from sending twice as much mail on the day it moves.
//
// Shape of the thing:
//
//   - Export walks every org-owned relation into a zip archive of newline
//     delimited JSON, one file per table, plus the attachment bytes.
//   - Import reads that archive back into a destination workspace, remapping
//     the people and clearing the fields that only meant something on the
//     instance the archive came from.
//   - Rows travel as jsonb in both directions, so Postgres does every type
//     conversion and no Go-side column mapping can drift from the schema.
//
// Secrets get their own treatment. Warmbly seals mailbox credentials under an
// instance key and everything else under a per-organization DEK, and neither
// key is portable. So an export opens them locally and re-seals them under a
// key derived from an operator-supplied passphrase, and an import reverses
// that against its own keys. Without the passphrase the archive still applies;
// mailboxes simply arrive needing a reconnect.
package orgtransfer

import (
	"context"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
)

// Service is the public API for organization archives.
type Service interface {
	// RequestExport queues an archive build and returns the job.
	RequestExport(ctx context.Context, orgID uuid.UUID, userID *uuid.UUID, req *models.CreateOrgExportRequest) (*models.OrgExportJob, *errx.Error)
	GetExport(ctx context.Context, orgID, id uuid.UUID) (*models.OrgExportJob, *errx.Error)
	ListExports(ctx context.Context, orgID uuid.UUID) ([]models.OrgExportJob, *errx.Error)
	// OpenExport streams a finished archive for download.
	OpenExport(ctx context.Context, orgID, id uuid.UUID) (io.ReadCloser, *models.OrgExportJob, *errx.Error)
	DeleteExport(ctx context.Context, orgID, id uuid.UUID) *errx.Error

	// Preflight reads an uploaded archive and reports what applying it would
	// do, without writing anything.
	Preflight(ctx context.Context, orgID uuid.UUID, archive ReaderAtSizer, passphrase string) (*models.OrgImportPreflight, *errx.Error)
	// RequestImport stores the archive and queues it for application.
	RequestImport(ctx context.Context, orgID uuid.UUID, userID *uuid.UUID, archive ReaderAtSizer, req *models.CreateOrgImportRequest) (*models.OrgImportJob, *errx.Error)
	GetImport(ctx context.Context, orgID, id uuid.UUID) (*models.OrgImportJob, *errx.Error)
	ListImports(ctx context.Context, orgID uuid.UUID) ([]models.OrgImportJob, *errx.Error)

	// ---- background-job entry point ----

	// PurgeExpiredExports deletes archives past their retention window and
	// closes out transfers whose process died mid-run.
	PurgeExpiredExports(ctx context.Context) (int, error)

	// ---- direct entry points for warmblyctl ----

	// ExportTo writes an archive straight to w with no job row. The CLI runs
	// on the box and streams to a file, so there is nothing to poll.
	ExportTo(ctx context.Context, orgID uuid.UUID, opts ExportOptions, w io.Writer, progress ProgressFunc) (*Manifest, error)
	// ImportFrom applies an archive with no job row, for the same reason.
	ImportFrom(ctx context.Context, orgID uuid.UUID, archive ReaderAtSizer, opts ImportOptions, progress ProgressFunc) (*ImportResult, error)
}

// ReaderAtSizer is what a zip needs: random access plus a length. Both callers
// have one (an uploaded file spooled to disk, or a file on the operator's box),
// so the engine never has to hold an archive in memory.
type ReaderAtSizer interface {
	io.ReaderAt
	Size() int64
}

// ProgressFunc reports how far along a transfer is. Percent is 0-100.
type ProgressFunc func(percent int, stage string)

// ExportOptions is the engine-level export request.
type ExportOptions struct {
	Groups []models.OrgDataGroup
	// Passphrase, when set, seals credentials into the archive. Empty means
	// secret columns are blanked and mailboxes import needing a reconnect.
	Passphrase string
}

// ImportOptions is the engine-level import request.
type ImportOptions struct {
	Groups     []models.OrgDataGroup
	Conflict   models.OrgImportConflict
	Passphrase string
	// ActorUserID owns any row whose original owner has no account here. It is
	// the person running the import.
	ActorUserID uuid.UUID
}

// ImportResult is what an import did.
type ImportResult struct {
	RowCounts map[string]int64
	Warnings  []string
	// SecretsApplied reports whether credentials were unsealed and re-keyed.
	SecretsApplied bool
}

// InstanceInfo labels an archive with where it came from.
type InstanceInfo struct {
	PublicURL  string
	AppVersion string
}

type service struct {
	repo     repository.OrgTransferRepository
	cipher   cipher.CipherService
	creds    *encrypt.Encrypter
	blobs    storage.Store
	instance InstanceInfo
}

// NewService builds the transfer service.
//
// creds may be nil on an instance with no CREDENTIALS_ENCRYPTION_KEY; mailbox
// credentials simply cannot be carried there, and the export says so rather
// than writing an archive full of unreadable ciphertext.
//
// blobs may be nil for the CLI, which streams archives to the local filesystem
// and has no reason to boot object storage; attachment bytes are then skipped.
func NewService(
	repo repository.OrgTransferRepository,
	cipherSvc cipher.CipherService,
	creds *encrypt.Encrypter,
	blobs storage.Store,
	instance InstanceInfo,
) Service {
	return &service{
		repo:     repo,
		cipher:   cipherSvc,
		creds:    creds,
		blobs:    blobs,
		instance: instance,
	}
}

// archiveObjectKey is where a finished export lives in blob storage.
func archiveObjectKey(orgID, jobID uuid.UUID) string {
	return "org-archives/" + orgID.String() + "/" + jobID.String() + ".warmbly.zip"
}

// uploadObjectKey is where an uploaded archive waits for its import job.
func uploadObjectKey(orgID, jobID uuid.UUID) string {
	return "org-archives/" + orgID.String() + "/import-" + jobID.String() + ".warmbly.zip"
}

// ArchiveFilename is the name a downloaded archive lands under.
func ArchiveFilename(orgName string, jobID uuid.UUID) string {
	slug := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r == ' ', r == '_', r == '.':
			return '-'
		}
		return -1
	}, orgName)
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "workspace"
	}
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "-")
	}
	return slug + "-" + jobID.String()[:8] + ".warmbly.zip"
}
