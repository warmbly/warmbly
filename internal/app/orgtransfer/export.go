package orgtransfer

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/mailcred"
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
		FormatVersion:    models.OrgTransferFormatVersionBase,
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
			Name:  t.Name,
			Group: t.Group,
			Columns: slices.DeleteFunc(columnNames(cols), func(column string) bool {
				return slices.Contains(t.ExcludeOnExport, column) || slices.Contains(t.derivedColumns(), column)
			}),
		}
		if archiveKey != nil {
			for _, sc := range t.Secrets {
				if !slices.Contains(t.ExcludeOnExport, sc.Column) {
					mt.SecretColumns = append(mt.SecretColumns, sc.Column)
				}
			}
		}

		// Rows only need decoding when something on them has to change. Most
		// tables have neither secrets nor blobs, so they stream through
		// untouched, which is what makes a million-row inbox export cheap.
		passthrough := len(t.Secrets) == 0 && len(t.Blobs) == 0 && len(t.ExcludeOnExport) == 0 && len(t.ExcludeJSONOnExport) == 0
		bytea := byteaColumns(cols)

		err = s.repo.StreamScoped(ctx, t.Name, t.Scope, orgID, func(row []byte) error {
			out := row
			if !passthrough {
				transformed, terr := s.exportRow(ctx, t, row, bytea, archiveKey, orgCipher, blobs)
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
		if t.Name == oauthAppsTable && mt.Rows > 0 {
			manifest.FormatVersion = models.OrgTransferFormatVersion
		}
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

// exportRow removes instance-local columns and rewrites portable secrets and blobs.
func (s *service) exportRow(
	ctx context.Context,
	t *Table,
	row []byte,
	bytea map[string]bool,
	archiveKey []byte,
	orgCipher *cipher.Cipher,
	blobs *blobCollector,
) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(row, &obj); err != nil {
		return nil, fmt.Errorf("decode %s row: %w", t.Name, err)
	}

	for _, column := range t.ExcludeOnExport {
		delete(obj, column)
	}

	for _, exclusion := range t.ExcludeJSONOnExport {
		raw, ok := obj[exclusion.Column]
		if !ok {
			continue
		}
		stripped, err := excludeExportJSONKeys(raw, exclusion.Keys)
		if err != nil {
			return nil, fmt.Errorf("exclude %s.%s JSON keys: %w", t.Name, exclusion.Column, err)
		}
		obj[exclusion.Column] = stripped
	}

	for _, sc := range t.Secrets {
		// A row not yet sealed carries its value only in the clear column, which stands in for it.
		var clear string
		if sc.Plaintext != "" {
			clear = jsonString(obj[sc.Plaintext])
		}
		raw, ok := obj[sc.Column]
		if !ok && clear == "" {
			continue
		}
		stored := jsonString(raw)
		if stored == "" && clear == "" {
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

		var plain string
		var openErr error
		if bytea[sc.Column] && stored != "" {
			if stored, openErr = decodeBytea(stored); openErr == nil && stored == "" && clear == "" {
				obj[sc.Column] = json.RawMessage(`null`)
				continue
			}
		}
		if openErr == nil && stored != "" {
			plain, openErr = s.openSecret(ctx, sc, stored, orgCipher)
		}
		if (stored == "" || openErr != nil) && clear != "" {
			plain, openErr = clear, nil
		}
		if openErr != nil {
			// One unreadable credential must not sink the export. Blank it and
			// let the mailbox or integration arrive needing a reconnect.
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

	for _, column := range t.derivedColumns() {
		delete(obj, column)
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

func excludeExportJSONKeys(raw json.RawMessage, keys []string) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return raw, nil
	}
	switch raw[0] {
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
		for key, value := range object {
			if slices.Contains(keys, key) {
				delete(object, key)
				continue
			}
			stripped, err := excludeExportJSONKeys(value, keys)
			if err != nil {
				return nil, err
			}
			object[key] = stripped
		}
		return json.Marshal(object)
	case '[':
		var array []json.RawMessage
		if err := json.Unmarshal(raw, &array); err != nil {
			return nil, err
		}
		for i, value := range array {
			stripped, err := excludeExportJSONKeys(value, keys)
			if err != nil {
				return nil, err
			}
			array[i] = stripped
		}
		return json.Marshal(array)
	default:
		return raw, nil
	}
}

// openSecret returns the plaintext behind one stored value, using whichever
// key domain sealed it.
func (s *service) openSecret(ctx context.Context, sc SecretColumn, stored string, orgCipher *cipher.Cipher) (string, error) {
	if sc.PlaintextPrefix != "" && strings.HasPrefix(stored, sc.PlaintextPrefix) {
		return stored, nil
	}
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
	case KeyDomainMailbox:
		return mailcred.OpenWith(ctx, orgCipher, s.creds, stored)
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
var publicKeyPrefixes = []string{"avatars/", "form-assets/", models.EmailImageKeyPrefix, models.BrandAssetKeyPrefix}

// isPublicBlobKey reports whether an object key belongs to one of those
// prefixes, so import can restore it public-read rather than private.
func isPublicBlobKey(key string) bool {
	for _, prefix := range publicKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

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
		// The archive is a customer download, so the operator's verdict and
		// the evidence behind it stay out of it.
		if OrgRiskColumns[k] {
			continue
		}
		var decoded any
		if err := json.Unmarshal(v, &decoded); err != nil {
			continue
		}
		out[k] = decoded
	}
	return out
}

// byteaColumns names the bytea columns among cols.
func byteaColumns(cols []repository.ColumnInfo) map[string]bool {
	out := map[string]bool{}
	for _, c := range cols {
		if c.IsBytea {
			out[c.Name] = true
		}
	}
	return out
}

// decodeBytea reads the \x hex form to_jsonb gives a bytea value.
func decodeBytea(v string) (string, error) {
	h, ok := strings.CutPrefix(v, `\x`)
	if !ok {
		return "", errors.New("bytea value is not in hex form")
	}
	b, err := hex.DecodeString(h)
	return string(b), err
}

// encodeBytea writes a value in the \x hex form a bytea column accepts.
func encodeBytea(v string) string { return `\x` + hex.EncodeToString([]byte(v)) }

func columnNames(cols []repository.ColumnInfo) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Name
	}
	return out
}

// needsOrgDEK reports whether any selected table has a column that is or may be DEK-sealed.
func needsOrgDEK(tables []*Table) bool {
	for _, t := range tables {
		for _, sc := range t.Secrets {
			if sc.Domain == KeyDomainOrgDEK || sc.Domain == KeyDomainMailbox {
				return true
			}
		}
	}
	return false
}
