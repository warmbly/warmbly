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
	"hash/crc32"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/attachmenttype"
	"github.com/warmbly/warmbly/internal/version"
)

const maxInventoryTableBytes = 64 << 20

// AttachmentInventoryOptions selects a page in an immutable local archive.
type AttachmentInventoryOptions struct {
	After  int
	Limit  int
	SHA256 string
}

// AttachmentInventory reports observations only; it grants no send eligibility.
type AttachmentInventory struct {
	ArchiveSHA256     string                    `json:"archive_sha256"`
	ClassifierBuild   version.Info              `json:"classifier_build"`
	OrganizationID    uuid.UUID                 `json:"organization_id"`
	SourceVersion     string                    `json:"source_version"`
	TotalRows         int                       `json:"total_rows"`
	UnreferencedBlobs int                       `json:"unreferenced_blobs"`
	After             int                       `json:"after"`
	NextAfter         int                       `json:"next_after"`
	Done              bool                      `json:"done"`
	Results           []AttachmentInventoryItem `json:"results"`
}

// AttachmentInventoryItem compares one archived row and blob with the upload profile.
type AttachmentInventoryItem struct {
	Row           int       `json:"row"`
	ID            uuid.UUID `json:"id"`
	CampaignID    uuid.UUID `json:"campaign_id"`
	Filename      string    `json:"filename"`
	StoredMIME    string    `json:"stored_mime"`
	StoredBytes   int64     `json:"stored_bytes"`
	Status        string    `json:"status"`
	ContentSHA256 string    `json:"content_sha256,omitempty"`
	CanonicalMIME string    `json:"canonical_mime,omitempty"`
}

// InventoryAttachments reads archive bytes without a database, storage client or importer.
func InventoryAttachments(ctx context.Context, archive ReaderAtSizer, opts AttachmentInventoryOptions) (result *AttachmentInventory, resultErr error) {
	defer func() {
		if err := ctx.Err(); err != nil {
			result, resultErr = nil, err
		}
	}()
	if opts.After < 0 || opts.Limit < 1 || opts.Limit > 100 {
		return nil, errors.New("after must be nonnegative and limit must be between 1 and 100")
	}
	if opts.After > 0 && opts.SHA256 == "" {
		return nil, errors.New("resuming requires the previous report's archive SHA-256")
	}
	if opts.SHA256 != "" {
		b, err := hex.DecodeString(opts.SHA256)
		if err != nil || len(b) != sha256.Size {
			return nil, errors.New("expected archive SHA-256 must contain 64 hexadecimal characters")
		}
	}
	if archive.Size() <= 0 || archive.Size() > models.MaxOrgArchiveUploadBytes {
		return nil, errors.New("archive must be between 1 byte and 8 GiB")
	}
	digest, err := inventoryDigest(ctx, archive)
	if err != nil {
		return nil, err
	}
	if opts.SHA256 != "" && !strings.EqualFold(opts.SHA256, digest) {
		return nil, errors.New("archive SHA-256 differs from the requested inventory")
	}
	zr, err := zip.NewReader(inventoryContextReaderAt{ctx, archive}, archive.Size())
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, errors.New("archive is not a readable ZIP")
	}
	entries := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		if entries[f.Name] != nil {
			return nil, errors.New("archive contains duplicate ZIP paths")
		}
		entries[f.Name] = f
	}
	manifestEntry := entries[manifestPath]
	if manifestEntry == nil || manifestEntry.UncompressedSize64 > maxManifestBytes {
		return nil, errors.New("inventory requires manifest.json within the 64 MiB limit")
	}
	mr, err := manifestEntry.Open()
	if err != nil {
		return nil, errors.New("cannot open archive manifest")
	}
	manifestBody, readErr := io.ReadAll(io.LimitReader(inventoryContextReader{ctx, mr}, maxManifestBytes+1))
	_ = mr.Close()
	if readErr != nil || len(manifestBody) > maxManifestBytes || crc32.ChecksumIEEE(manifestBody) != manifestEntry.CRC32 {
		return nil, errors.New("cannot read archive manifest within the 64 MiB limit")
	}
	var m Manifest
	if err := json.Unmarshal(manifestBody, &m); err != nil {
		return nil, errors.New("archive manifest is not readable JSON")
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	campaigns := map[uuid.UUID]bool{}
	err = inventoryRows(ctx, entries, &m, "campaigns", func(b []byte) error {
		var c struct {
			ID             uuid.UUID `json:"id"`
			OrganizationID uuid.UUID `json:"organization_id"`
		}
		if err := json.Unmarshal(b, &c); err != nil || c.ID == uuid.Nil || c.OrganizationID != m.OrganizationID || campaigns[c.ID] {
			return errors.New("campaign rows must uniquely belong to the archive organization")
		}
		campaigns[c.ID] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	blobs := map[string]ManifestBlob{}
	for _, b := range m.Blobs {
		if b.Table != "campaign_attachments" && !strings.HasPrefix(b.Key, "attachments/") {
			continue
		}
		if _, exists := blobs[b.Key]; exists {
			return nil, errors.New("archive contains duplicate attachment blob keys")
		}
		blobs[b.Key] = b
	}
	report := &AttachmentInventory{ArchiveSHA256: digest, ClassifierBuild: version.Current(), OrganizationID: m.OrganizationID,
		SourceVersion: m.SourceAppVersion, After: opts.After, NextAfter: opts.After, Results: []AttachmentInventoryItem{}}
	ids := map[uuid.UUID]bool{}
	keys := map[string]bool{}
	scope := blobKeyScope{orgID: m.OrganizationID, campaigns: campaigns}
	err = inventoryRows(ctx, entries, &m, "campaign_attachments", func(b []byte) error {
		var a models.CampaignAttachment
		if err := json.Unmarshal(b, &a); err != nil || a.ID == uuid.Nil || ids[a.ID] || !campaigns[a.CampaignID] {
			return errors.New("attachment rows must uniquely reference an archived campaign")
		}
		ids[a.ID] = true
		keys[a.S3Key] = true
		report.TotalRows++
		if report.TotalRows <= opts.After || len(report.Results) == opts.Limit {
			return nil
		}
		item := AttachmentInventoryItem{Row: report.TotalRows, ID: a.ID, CampaignID: a.CampaignID,
			Filename: a.Filename, StoredMIME: a.MimeType, StoredBytes: a.Size, Status: "metadata_mismatch"}
		ref, exists := blobs[a.S3Key]
		switch {
		case !scope.allows(a.S3Key) || !strings.HasPrefix(a.S3Key, "attachments/"+a.CampaignID.String()+"/"):
			item.Status = "metadata_mismatch"
		case !exists || entries[ref.Path] == nil:
			item.Status = "missing_blob"
		case ref.Table != "campaign_attachments" || ref.Column != "s3_key" || !strings.HasPrefix(ref.Path, blobPrefix) || ref.Size != a.Size || a.Size < 0 || uint64(a.Size) != entries[ref.Path].UncompressedSize64:
			item.Status = "metadata_mismatch"
		case a.Size == 0 || a.Size > attachmenttype.MaxBytes:
			item.Status = "outside_upload_profile"
		default:
			item = inventoryContent(ctx, entries[ref.Path], item)
		}
		report.Results = append(report.Results, item)
		report.NextAfter = report.TotalRows
		return nil
	})
	if err != nil {
		return nil, err
	}
	if opts.After > report.TotalRows {
		return nil, errors.New("after is beyond the attachment row count")
	}
	for key := range blobs {
		if !keys[key] {
			report.UnreferencedBlobs++
		}
	}
	report.Done = report.NextAfter == report.TotalRows
	finalDigest, err := inventoryDigest(ctx, archive)
	if err != nil {
		return nil, err
	}
	if finalDigest != digest {
		return nil, errors.New("archive changed during inventory; discard this page")
	}
	return report, ctx.Err()
}

func inventoryContent(ctx context.Context, entry *zip.File, item AttachmentInventoryItem) AttachmentInventoryItem {
	item.Status = "unreadable_blob"
	r, err := entry.Open()
	if err != nil {
		return item
	}
	defer r.Close()
	body, err := io.ReadAll(io.LimitReader(inventoryContextReader{ctx, r}, attachmenttype.MaxBytes+1))
	if err != nil || crc32.ChecksumIEEE(body) != entry.CRC32 {
		return item
	}
	if int64(len(body)) != item.StoredBytes {
		item.Status = "metadata_mismatch"
		return item
	}
	digest := sha256.Sum256(body)
	item.ContentSHA256 = hex.EncodeToString(digest[:])
	t, err := attachmenttype.Validate(item.Filename, body)
	item.Status = "outside_upload_profile"
	if err == nil {
		item.Status = "matches_upload_profile"
		item.CanonicalMIME = t.MIME
	}
	return item
}

func inventoryRows(ctx context.Context, entries map[string]*zip.File, m *Manifest, table string, visit func([]byte) error) error {
	var mt *ManifestTable
	for i := range m.Tables {
		if m.Tables[i].Name == table {
			if mt != nil {
				return fmt.Errorf("duplicate %s table in manifest", table)
			}
			mt = &m.Tables[i]
		}
	}
	if mt == nil || mt.Truncated || mt.Rows < 0 {
		return fmt.Errorf("inventory requires a complete %s table in the archive", table)
	}
	entry := entries[dataPath(table)]
	if entry == nil {
		return fmt.Errorf("archive has no %s data", table)
	}
	if entry.UncompressedSize64 > maxInventoryTableBytes {
		return fmt.Errorf("%s metadata exceeds the 64 MiB inventory limit", table)
	}
	r, err := entry.Open()
	if err != nil {
		return fmt.Errorf("cannot open %s metadata", table)
	}
	defer r.Close()
	checksum := crc32.NewIEEE()
	limited := &io.LimitedReader{R: io.TeeReader(inventoryContextReader{ctx, r}, checksum), N: maxInventoryTableBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	var rows int64
	for scanner.Scan() {
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue
		}
		if err := visit(scanner.Bytes()); err != nil {
			return err
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("cannot read %s metadata (1 MiB row limit): %w", table, err)
	}
	if limited.N == 0 || rows != mt.Rows {
		return fmt.Errorf("%s metadata size or row count differs from the manifest", table)
	}
	if checksum.Sum32() != entry.CRC32 {
		return fmt.Errorf("cannot verify %s metadata integrity", table)
	}
	return nil
}

func inventoryDigest(ctx context.Context, archive ReaderAtSizer) (string, error) {
	h := sha256.New()
	size := archive.Size()
	n, err := io.Copy(h, inventoryContextReader{ctx, io.NewSectionReader(archive, 0, size)})
	if err != nil {
		return "", err
	}
	if n != size {
		return "", errors.New("archive size changed during fingerprinting")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type inventoryContextReader struct {
	ctx context.Context
	r   io.Reader
}

type inventoryContextReaderAt struct {
	ctx context.Context
	r   io.ReaderAt
}

func (r inventoryContextReaderAt) ReadAt(b []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.ReadAt(b, off)
}

func (r inventoryContextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
