package orgtransfer

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// ExportTo writes one organization's archive to w.
//
// The archive is a zip, so the manifest can be written last (after the row
// counts are known) and still be the first thing an importer reads: zip has a
// central directory, so entry order on disk is not read order.
func (s *service) ExportTo(
	ctx context.Context,
	orgID uuid.UUID,
	opts ExportOptions,
	w io.Writer,
	progress ProgressFunc,
) (*Manifest, error) {
	report := func(p int, stage string) {
		if progress != nil {
			progress(p, stage)
		}
	}
	report(1, "reading workspace")

	orgRaw, err := s.repo.OrganizationRow(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("read organization: %w", err)
	}
	if orgRaw == nil {
		return nil, errors.New("organization not found")
	}
	var orgRow map[string]json.RawMessage
	if err := json.Unmarshal(orgRaw, &orgRow); err != nil {
		return nil, fmt.Errorf("decode organization: %w", err)
	}

	members, err := s.repo.ArchiveMembers(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("read members: %w", err)
	}

	groups := NormalizeGroups(opts.Groups)
	tables := GroupTables(groups)

	manifest := &Manifest{
		Kind:             ArchiveKind,
		FormatVersion:    models.OrgTransferFormatVersion,
		SourceInstance:   s.instance.PublicURL,
		SourceAppVersion: s.instance.AppVersion,
		ExportedAt:       time.Now().UTC(),
		Organization:     toAnyMap(orgRow),
		OrganizationID:   orgID,
		OrganizationName: jsonString(orgRow["name"]),
		Groups:           groups,
		Members:          members,
		Tables:           make([]ManifestTable, 0, len(tables)),
	}

	// Derive the archive key up front: a passphrase that fails validation
	// should stop the export before it has read a single row.
	var archiveKey []byte
	if opts.Passphrase != "" {
		hdr, key, err := NewSecretsHeader(opts.Passphrase)
		if err != nil {
			return nil, err
		}
		manifest.Secrets = hdr
		archiveKey = key
	}

	// The org DEK is only needed when a selected table has DEK-sealed columns
	// and we are actually carrying secrets.
	var orgCipher *cipher.Cipher
	if archiveKey != nil && s.cipher != nil && needsOrgDEK(tables) {
		orgCipher, err = s.cipher.Cipher(ctx, orgID)
		if err != nil {
			return nil, fmt.Errorf("load organization key: %w", err)
		}
	}

	zw := zip.NewWriter(w)
	blobs := newBlobCollector()

	// The org's own avatar is an object too, and the org row lives in the
	// manifest rather than in a table, so it is collected here.
	if url := jsonString(orgRow["avatar_url"]); url != "" {
		blobs.addURL(url, "organizations", "avatar_url")
	}

	for i, t := range tables {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Table coverage is compiled in, but an instance mid-migration may not
		// have it yet. Skipping beats failing the whole export.
		cols, err := s.repo.TableColumns(ctx, t.Name)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", t.Name, err)
		}
		if len(cols) == 0 {
			continue
		}

		report(5+(i*80)/len(tables), "exporting "+t.Name)

		entry, err := zw.Create(dataPath(t.Name))
		if err != nil {
			return nil, err
		}
		buf := bufio.NewWriterSize(entry, 256*1024)

		mt := ManifestTable{
			Name:    t.Name,
			Group:   t.Group,
			Columns: columnNames(cols),
		}
		if archiveKey != nil {
			for _, sc := range t.Secrets {
				mt.SecretColumns = append(mt.SecretColumns, sc.Column)
			}
		}

		// Rows only need decoding when something on them has to change. Most
		// tables have neither secrets nor blobs, so they stream through
		// untouched, which is what makes a million-row inbox export cheap.
		passthrough := len(t.Secrets) == 0 && len(t.Blobs) == 0

		err = s.repo.StreamScoped(ctx, t.Name, t.Scope, orgID, func(row []byte) error {
			out := row
			if !passthrough {
				transformed, terr := s.exportRow(ctx, t, row, archiveKey, orgCipher, blobs)
				if terr != nil {
					return terr
				}
				out = transformed
			}
			if _, werr := buf.Write(out); werr != nil {
				return werr
			}
			if werr := buf.WriteByte('\n'); werr != nil {
				return werr
			}
			mt.Rows++
			return nil
		})
		if err != nil {
			return nil, err
		}
		if err := buf.Flush(); err != nil {
			return nil, err
		}
		manifest.Tables = append(manifest.Tables, mt)
	}

	report(86, "collecting attachments")
	if err := s.writeBlobs(ctx, zw, blobs, manifest); err != nil {
		return nil, err
	}

	report(96, "writing manifest")
	if err := writeJSONEntry(zw, manifestPath, manifest); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	report(100, "done")
	return manifest, nil
}

// exportRow rewrites one row's secret and blob columns.
func (s *service) exportRow(
	ctx context.Context,
	t *Table,
	row []byte,
	archiveKey []byte,
	orgCipher *cipher.Cipher,
	blobs *blobCollector,
) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(row, &obj); err != nil {
		return nil, fmt.Errorf("decode %s row: %w", t.Name, err)
	}

	for _, sc := range t.Secrets {
		raw, ok := obj[sc.Column]
		if !ok {
			continue
		}
		stored := jsonString(raw)
		if stored == "" {
			continue
		}
		// A guarded column only holds ciphertext when its flag is set.
		if sc.Guard != "" && !jsonBool(obj[sc.Guard]) {
			continue
		}

		if archiveKey == nil {
			// Not carrying secrets: blank the column rather than shipping
			// ciphertext no destination could ever open.
			blankSecret(obj, sc)
			continue
		}

		plain, err := s.openSecret(ctx, sc, stored, orgCipher)
		if err != nil {
			// One unreadable credential must not sink the export. Blank it and
			// let the mailbox arrive needing a reconnect.
			blankSecret(obj, sc)
			continue
		}
		sealed, err := SealValue(archiveKey, plain)
		if err != nil {
			return nil, err
		}
		enc, err := json.Marshal(sealed)
		if err != nil {
			return nil, err
		}
		obj[sc.Column] = enc
	}

	for _, bc := range t.Blobs {
		v := jsonString(obj[bc.Column])
		if v == "" {
			continue
		}
		switch bc.Kind {
		case BlobKindKey:
			blobs.addKey(v, t.Name, bc.Column)
		case BlobKindPublicURL:
			blobs.addURL(v, t.Name, bc.Column)
		}
	}

	return json.Marshal(obj)
}

// openSecret returns the plaintext behind one stored value, using whichever
// key domain sealed it.
func (s *service) openSecret(ctx context.Context, sc SecretColumn, stored string, orgCipher *cipher.Cipher) (string, error) {
	switch sc.Domain {
	case KeyDomainInstance:
		if s.creds == nil {
			return "", errors.New("credential key not configured")
		}
		return s.creds.Decrypt(stored)
	case KeyDomainOrgDEK:
		if orgCipher == nil {
			return "", errors.New("organization key unavailable")
		}
		return orgCipher.Decrypt(ctx, stored)
	}
	return "", fmt.Errorf("column %s has no key domain", sc.Column)
}

// writeBlobs copies every collected object into the archive.
func (s *service) writeBlobs(ctx context.Context, zw *zip.Writer, blobs *blobCollector, manifest *Manifest) error {
	if s.blobs == nil || len(blobs.order) == 0 {
		return nil
	}
	for i, key := range blobs.order {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		ref := blobs.refs[key]
		body, err := s.blobs.Get(ctx, key)
		if err != nil {
			// A missing object is a gap in the archive, not a reason to lose
			// the other ten thousand rows. The row keeps its key, and the
			// import reports the object as absent.
			continue
		}

		archivePath := blobArchivePath(i, key)
		entry, cerr := zw.Create(archivePath)
		if cerr != nil {
			_ = body.Close()
			return cerr
		}
		n, cerr := io.Copy(entry, body)
		_ = body.Close()
		if cerr != nil {
			return cerr
		}
		manifest.Blobs = append(manifest.Blobs, ManifestBlob{
			Key:    key,
			Path:   archivePath,
			Size:   n,
			Table:  ref.table,
			Column: ref.column,
		})
	}
	return nil
}

// blobCollector dedupes object references across rows.
type blobCollector struct {
	order []string
	refs  map[string]blobRef
}

type blobRef struct{ table, column string }

func newBlobCollector() *blobCollector {
	return &blobCollector{refs: map[string]blobRef{}}
}

func (b *blobCollector) addKey(key, table, column string) {
	if key == "" {
		return
	}
	if _, seen := b.refs[key]; seen {
		return
	}
	b.refs[key] = blobRef{table, column}
	b.order = append(b.order, key)
}

// addURL recovers an object key from a public URL. Both storage backends build
// that URL differently, but the key always begins with a known prefix, so the
// prefix is what we look for. A URL that matches nothing is left alone: the row
// keeps its absolute URL and the image simply resolves against the old host.
func (b *blobCollector) addURL(url, table, column string) {
	for _, prefix := range publicKeyPrefixes {
		if idx := strings.Index(url, prefix); idx >= 0 {
			key := url[idx:]
			if q := strings.IndexAny(key, "?#"); q >= 0 {
				key = key[:q]
			}
			b.addKey(key, table, column)
			return
		}
	}
}

// publicKeyPrefixes are the object-key prefixes reachable through a public URL.
var publicKeyPrefixes = []string{"avatars/"}

// ---------- small JSON helpers ----------

func writeJSONEntry(zw *zip.Writer, name string, v any) error {
	entry, err := zw.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(entry)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// blankSecret empties a secret column, and clears its guard when it has one.
// Leaving the guard set would leave the row claiming to hold ciphertext it no
// longer has, which the reader would then try to decrypt.
func blankSecret(obj map[string]json.RawMessage, sc SecretColumn) {
	obj[sc.Column] = json.RawMessage(`""`)
	if sc.Guard != "" {
		obj[sc.Guard] = json.RawMessage(`false`)
	}
}

// jsonString reads a JSON value as a string, returning "" for null, absent, or
// a non-string value.
func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func jsonBool(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false
	}
	return b
}

func toAnyMap(in map[string]json.RawMessage) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		var decoded any
		if err := json.Unmarshal(v, &decoded); err != nil {
			continue
		}
		out[k] = decoded
	}
	return out
}

func columnNames(cols []repository.ColumnInfo) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Name
	}
	return out
}

// needsOrgDEK reports whether any selected table has a DEK-sealed column.
func needsOrgDEK(tables []*Table) bool {
	for _, t := range tables {
		for _, sc := range t.Secrets {
			if sc.Domain == KeyDomainOrgDEK {
				return true
			}
		}
	}
	return false
}
