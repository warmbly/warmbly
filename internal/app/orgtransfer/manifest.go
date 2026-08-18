package orgtransfer

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// ArchiveKind identifies the file so a wrong upload fails on the manifest
// rather than halfway through writing rows.
const ArchiveKind = "warmbly.organization-archive"

// Archive layout. A plain zip, so an operator can unzip it and read the data
// with nothing but a text editor:
//
//	manifest.json          what this archive is, and what is in it
//	data/<table>.ndjson    one JSON object per row, column-name keyed
//	blobs/<n>-<name>       attachment and avatar bytes
//
// Rows are newline-delimited rather than one big array so a table with a
// million rows never has to be held in memory on either side.
const (
	manifestPath = "manifest.json"
	dataPrefix   = "data/"
	blobPrefix   = "blobs/"
)

// dataPath is where one table's rows live inside the archive.
func dataPath(table string) string { return dataPrefix + table + ".ndjson" }

// Manifest is the archive's header. Everything the importer needs to decide
// what it is looking at before it reads a single row.
type Manifest struct {
	Kind          string `json:"kind"`
	FormatVersion int    `json:"format_version"`

	// SourceInstance is the exporting instance's public URL host, and
	// SourceAppVersion its release. Both are informational: the importer keys
	// off FormatVersion and the per-table column lists, never off these.
	SourceInstance   string    `json:"source_instance"`
	SourceAppVersion string    `json:"source_app_version"`
	ExportedAt       time.Time `json:"exported_at"`

	// Organization is the workspace row itself, verbatim. It is merged onto
	// the destination workspace rather than inserted, so it lives here instead
	// of in a one-row data file.
	Organization map[string]any `json:"organization"`

	OrganizationID   uuid.UUID `json:"organization_id"`
	OrganizationName string    `json:"organization_name"`

	Groups []models.OrgDataGroup `json:"groups"`
	Tables []ManifestTable       `json:"tables"`
	Blobs  []ManifestBlob        `json:"blobs"`

	// Members lets the importer resolve people to destination accounts without
	// reading the members table first. No password material: an archive can
	// never mint a working login.
	Members []models.OrgArchiveUser `json:"members"`

	// Secrets is present only when credentials were sealed into the archive.
	Secrets *SecretsHeader `json:"secrets,omitempty"`
}

// ManifestTable records what was written for one relation. Columns are stored
// because the destination schema may differ: the importer intersects this list
// with the columns it actually has, so an archive from an older or newer
// release still applies.
type ManifestTable struct {
	Name    string              `json:"name"`
	Group   models.OrgDataGroup `json:"group"`
	Rows    int64               `json:"rows"`
	Columns []string            `json:"columns"`
	// SecretColumns are the columns whose values were sealed. Empty when the
	// export ran without secrets, in which case those columns are blank.
	SecretColumns []string `json:"secret_columns,omitempty"`
	// Truncated marks a table whose export hit the row ceiling.
	Truncated bool `json:"truncated,omitempty"`
}

// ManifestBlob is one object-storage object carried by the archive.
type ManifestBlob struct {
	// Key is the object key as the source instance stored it.
	Key string `json:"key"`
	// Path is where the bytes live inside this archive.
	Path string `json:"path"`
	Size int64  `json:"size"`
	// Table and Column say which row referenced it, so a failed blob can be
	// reported against something the user recognises.
	Table  string `json:"table"`
	Column string `json:"column"`
}

// SecretsHeader describes how the archive's secret values were sealed. The
// passphrase is never stored; Verifier exists so a wrong one is caught before
// anything is written rather than surfacing as a mailbox that cannot log in.
type SecretsHeader struct {
	Algorithm string `json:"algorithm"`
	Salt      string `json:"salt"`
	Time      uint32 `json:"time"`
	Memory    uint32 `json:"memory"`
	Threads   uint8  `json:"threads"`
	Verifier  string `json:"verifier"`
}

// Info reduces a manifest to the summary the dashboard shows.
func (m *Manifest) Info() *models.OrgArchiveInfo {
	counts := make(map[string]int64, len(m.Tables))
	for _, t := range m.Tables {
		counts[t.Name] = t.Rows
	}
	return &models.OrgArchiveInfo{
		FormatVersion:    m.FormatVersion,
		SourceInstance:   m.SourceInstance,
		SourceAppVersion: m.SourceAppVersion,
		OrganizationID:   m.OrganizationID,
		OrganizationName: m.OrganizationName,
		ExportedAt:       m.ExportedAt,
		Groups:           m.Groups,
		HasSecrets:       m.Secrets != nil,
		RowCounts:        counts,
		BlobCount:        len(m.Blobs),
		Members:          m.Members,
	}
}

// Validate rejects a file that is not a Warmbly archive, or one this build
// cannot read, before any of it is applied.
func (m *Manifest) Validate() error {
	if m.Kind != ArchiveKind {
		return fmt.Errorf("not a Warmbly organization archive (found kind %q)", m.Kind)
	}
	if m.FormatVersion <= 0 {
		return fmt.Errorf("archive has no format version")
	}
	if m.FormatVersion > models.OrgTransferFormatVersion {
		return fmt.Errorf(
			"archive was written in format version %d, but this instance reads up to version %d. Upgrade this instance, then import again.",
			m.FormatVersion, models.OrgTransferFormatVersion)
	}
	if m.OrganizationID == uuid.Nil {
		return fmt.Errorf("archive names no organization")
	}
	return nil
}

// blobArchivePath builds a collision-free, traversal-free path for one blob.
// Object keys contain slashes and arbitrary characters; the index prefix keeps
// two objects with the same basename apart.
func blobArchivePath(index int, key string) string {
	base := path.Base(strings.TrimSuffix(key, "/"))
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		}
		return '_'
	}, base)
	if base == "" || base == "." || base == ".." {
		base = "object"
	}
	if len(base) > 80 {
		base = base[len(base)-80:]
	}
	return fmt.Sprintf("%s%06d-%s", blobPrefix, index, base)
}
