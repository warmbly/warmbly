package orgtransfer

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// importBatchRows is how many rows go into one INSERT.
	importBatchRows = 500
	// importBatchBytes caps a batch by payload size, because one inbox row can
	// carry a megabyte of message body and a fixed row count is no protection.
	importBatchBytes = 8 << 20
	// maxManifestBytes bounds the manifest read from an untrusted archive.
	maxManifestBytes = 64 << 20
)

// ImportFrom applies an archive to a destination workspace.
//
// The whole thing runs in one transaction. An import is a rare, deliberate,
// operator-driven event, and a half-applied workspace — campaigns without
// their sequences, contacts without their suppression list — is far worse than
// a long-running transaction. If anything fails, nothing lands.
func (s *service) ImportFrom(
	ctx context.Context,
	orgID uuid.UUID,
	archive ReaderAtSizer,
	opts ImportOptions,
	progress ProgressFunc,
) (*ImportResult, error) {
	report := func(p int, stage string) {
		if progress != nil {
			progress(p, stage)
		}
	}
	report(1, "reading archive")

	zr, err := zip.NewReader(archive, archive.Size())
	if err != nil {
		return nil, fmt.Errorf("archive is not a readable zip: %w", err)
	}
	entries := indexEntries(zr)

	manifest, err := readManifest(entries)
	if err != nil {
		return nil, err
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}

	result := &ImportResult{RowCounts: map[string]int64{}}

	archiveKey, err := s.resolveArchiveKey(manifest, opts.Passphrase)
	if err != nil {
		return nil, err
	}
	result.SecretsApplied = archiveKey != nil
	switch {
	case manifest.Secrets != nil && archiveKey == nil:
		result.Warnings = append(result.Warnings,
			"This archive carries sealed credentials but no passphrase was supplied, so mailboxes and integrations were imported without them and need reconnecting.")
	case manifest.Secrets == nil:
		result.Warnings = append(result.Warnings,
			"This archive was exported without credentials, so mailboxes and integrations need reconnecting.")
	}

	// The destination's own DEK, for re-sealing everything the archive holds
	// under the org key domain.
	var orgCipher *cipher.Cipher
	if archiveKey != nil && s.cipher != nil {
		orgCipher, err = s.cipher.Cipher(ctx, orgID)
		if err != nil {
			return nil, fmt.Errorf("load destination organization key: %w", err)
		}
	}

	report(4, "matching members")
	userMap, unknown, err := s.buildUserMap(ctx, manifest, opts.ActorUserID)
	if err != nil {
		return nil, err
	}
	for _, u := range unknown {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"%s has no account on this instance; their rows were reassigned to you.", u.Email))
	}

	groups := NormalizeGroups(opts.Groups)
	selected := map[models.OrgDataGroup]bool{}
	for _, g := range groups {
		selected[g] = true
	}

	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = 0`); err != nil {
		return nil, fmt.Errorf("lift statement timeout: %w", err)
	}

	report(6, "applying workspace settings")
	if err := s.mergeOrganization(ctx, tx, orgID, manifest); err != nil {
		return nil, err
	}

	byName := manifestTables(manifest)
	applied := 0
	for i := range Tables {
		t := &Tables[i]
		if t.ImportSkip || !selected[t.Group] {
			continue
		}
		mt, ok := byName[t.Name]
		if !ok || mt.Rows == 0 {
			continue
		}
		entry, ok := entries[dataPath(t.Name)]
		if !ok {
			continue
		}

		report(8+(applied*84)/max(1, len(byName)), "importing "+t.Name)
		applied++

		n, warn, err := s.importTable(ctx, tx, orgID, t, mt, entry, importContext{
			archiveKey: archiveKey,
			orgCipher:  orgCipher,
			userMap:    userMap,
			actor:      opts.ActorUserID,
			conflict:   opts.Conflict,
			selected:   selected,
		})
		if err != nil {
			return nil, err
		}
		if n > 0 {
			result.RowCounts[t.Name] = n
		}
		result.Warnings = append(result.Warnings, warn...)
	}

	report(93, "restoring attachments")
	if warn := s.restoreBlobs(ctx, entries, manifest); len(warn) > 0 {
		result.Warnings = append(result.Warnings, warn...)
	}

	report(97, "checking mailboxes")
	reconnect, err := s.repo.MarkMailboxesNeedingReconnect(ctx, tx, orgID)
	if err != nil {
		return nil, err
	}
	if reconnect > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"%d mailbox(es) have no usable credentials and are marked for reconnection.", reconnect))
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit import: %w", err)
	}
	report(100, "done")
	return result, nil
}

// importContext is the per-import state every table needs.
type importContext struct {
	archiveKey []byte
	orgCipher  *cipher.Cipher
	userMap    map[uuid.UUID]uuid.UUID
	// actor owns any row whose original owner has no account here.
	actor    uuid.UUID
	conflict models.OrgImportConflict
	// selected is the set of groups this run applies, used to decide which
	// references the import can actually satisfy.
	selected map[models.OrgDataGroup]bool
}

// importTable streams one table's rows out of the archive and into the
// destination, in batches.
func (s *service) importTable(
	ctx context.Context,
	tx pgx.Tx,
	orgID uuid.UUID,
	t *Table,
	mt ManifestTable,
	entry *zip.File,
	ic importContext,
) (int64, []string, error) {
	destCols, err := s.repo.TableColumns(ctx, t.Name)
	if err != nil {
		return 0, nil, fmt.Errorf("inspect %s: %w", t.Name, err)
	}
	if len(destCols) == 0 {
		return 0, []string{fmt.Sprintf(
			"This instance has no %s table, so those %d rows were skipped.", t.Name, mt.Rows)}, nil
	}

	// Only columns that exist on both sides and can actually be written. This
	// is what lets an archive from a different release still apply, and it is
	// also the guarantee that no name out of the archive reaches a query.
	writable := map[string]bool{}
	for _, c := range destCols {
		if c.Writable() {
			writable[c.Name] = true
		}
	}

	// A reset column is left out of the statement entirely rather than written
	// as NULL, so the destination's own DEFAULT fills it. Writing NULL only
	// worked for the nullable ones and aborted the whole import on a NOT NULL
	// counter such as webhook_endpoints.consecutive_failures.
	reset := make(map[string]bool, len(t.ResetOnImport))
	for _, c := range t.ResetOnImport {
		reset[c] = true
	}

	insertCols := make([]string, 0, len(mt.Columns))
	unknown := 0
	for _, c := range mt.Columns {
		switch {
		case reset[c]:
			// deliberately omitted
		case writable[c]:
			insertCols = append(insertCols, c)
		default:
			unknown++
		}
	}
	if len(insertCols) == 0 {
		return 0, []string{fmt.Sprintf("No column of %s matched this instance, so it was skipped.", t.Name)}, nil
	}

	var warnings []string
	if unknown > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%s: %d column(s) in the archive do not exist here and were ignored.", t.Name, unknown))
	}

	var pk []string
	if ic.conflict == models.OrgImportConflictOverwrite {
		if pk, err = s.repo.PrimaryKeyColumns(ctx, t.Name); err != nil {
			return 0, nil, err
		}
		if len(pk) == 0 {
			warnings = append(warnings, fmt.Sprintf(
				"%s has no primary key, so existing rows there were kept rather than overwritten.", t.Name))
		}
	}

	refs, err := s.referencePlan(ctx, t, destCols, ic.selected)
	if err != nil {
		return 0, nil, err
	}
	if len(refs.nullify) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%s: %d reference(s) point at data this import does not include and were cleared.",
			t.Name, len(refs.nullify)))
	}

	rc, err := entry.Open()
	if err != nil {
		return 0, nil, fmt.Errorf("open %s in archive: %w", t.Name, err)
	}
	defer rc.Close()

	reader := bufio.NewReaderSize(rc, 256*1024)
	batch := make([]json.RawMessage, 0, importBatchRows)
	batchBytes := 0
	var total int64

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, err := s.repo.InsertBatch(ctx, tx, t.Name, insertCols, batch, ic.conflict, pk)
		if err != nil {
			return err
		}
		total += n
		batch = batch[:0]
		batchBytes = 0
		return nil
	}

	for {
		line, err := readLine(reader)
		if len(line) > 0 {
			row, terr := s.importRow(ctx, orgID, t, line, refs, ic)
			if terr != nil {
				return 0, nil, terr
			}
			batch = append(batch, row)
			batchBytes += len(row)
			if len(batch) >= importBatchRows || batchBytes >= importBatchBytes {
				if ferr := flush(); ferr != nil {
					return 0, nil, ferr
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return 0, nil, fmt.Errorf("read %s from archive: %w", t.Name, err)
		}
	}
	if err := flush(); err != nil {
		return 0, nil, err
	}

	if skipped := mt.Rows - total; skipped > 0 && ic.conflict == models.OrgImportConflictSkip {
		warnings = append(warnings, fmt.Sprintf(
			"%s: %d row(s) already existed here and were kept as they are.", t.Name, skipped))
	}
	return total, warnings, nil
}

// importRow rewrites one archive row for this instance.
func (s *service) importRow(
	ctx context.Context,
	orgID uuid.UUID,
	t *Table,
	line []byte,
	refs *referencePlan,
	ic importContext,
) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil {
		return nil, fmt.Errorf("%s: archive row is not valid JSON: %w", t.Name, err)
	}

	// The destination workspace owns the rows now, whatever id they carried.
	destOrg, err := json.Marshal(orgID.String())
	if err != nil {
		return nil, err
	}
	for _, c := range []string{"organization_id", "org_id"} {
		if _, ok := obj[c]; ok {
			obj[c] = destOrg
		}
	}

	// People are matched by account, not by id: the same person has a
	// different user id on every instance. An id with no match here (a member
	// who left, or an account that never existed on this instance) would be a
	// dangling foreign key, so it is blanked where the column allows it and
	// redirected to the importer where it does not.
	for c, notNull := range refs.users {
		raw, ok := obj[c]
		if !ok {
			continue
		}
		id, perr := uuid.Parse(jsonString(raw))
		if perr != nil {
			continue
		}
		mapped, ok := ic.userMap[id]
		if !ok {
			if !notNull {
				obj[c] = json.RawMessage(`null`)
				continue
			}
			if ic.actor == uuid.Nil {
				continue
			}
			mapped = ic.actor
		}
		enc, merr := json.Marshal(mapped.String())
		if merr != nil {
			return nil, merr
		}
		obj[c] = enc
	}

	// References whose target is not part of this run. They are nullable by
	// construction (a NOT NULL one is covered by GroupRequires), so clearing
	// them keeps the row valid instead of failing the whole import on a
	// constraint the archive never promised to satisfy.
	for _, c := range refs.nullify {
		if _, ok := obj[c]; ok {
			obj[c] = json.RawMessage(`null`)
		}
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
		if !IsSealed(stored) {
			// Exported without secrets, so the column is already blank or was
			// never ciphertext. Nothing to re-key.
			continue
		}
		if ic.archiveKey == nil {
			// Sealed, but no passphrase. Blank it rather than storing a value
			// this instance's keys can never open.
			blankSecret(obj, sc)
			continue
		}
		plain, oerr := OpenValue(ic.archiveKey, stored)
		if oerr != nil {
			return nil, fmt.Errorf("%s.%s: %w", t.Name, sc.Column, oerr)
		}
		resealed, rerr := s.sealSecret(ctx, sc, plain, ic.orgCipher)
		if rerr != nil {
			return nil, fmt.Errorf("%s.%s: %w", t.Name, sc.Column, rerr)
		}
		enc, merr := json.Marshal(resealed)
		if merr != nil {
			return nil, merr
		}
		obj[sc.Column] = enc
	}

	return json.Marshal(obj)
}

// sealSecret re-encrypts a plaintext under this instance's key for that domain.
func (s *service) sealSecret(ctx context.Context, sc SecretColumn, plain string, orgCipher *cipher.Cipher) (string, error) {
	switch sc.Domain {
	case KeyDomainInstance:
		if s.creds == nil {
			return "", errors.New("this instance has no CREDENTIALS_ENCRYPTION_KEY, so mailbox credentials cannot be stored")
		}
		return s.creds.Encrypt(plain)
	case KeyDomainOrgDEK:
		if orgCipher == nil {
			return "", errors.New("destination organization key unavailable")
		}
		return orgCipher.Encrypt(ctx, plain)
	}
	return "", fmt.Errorf("column %s has no key domain", sc.Column)
}

// referencePlan is how one table's foreign keys are handled on the way in.
type referencePlan struct {
	// users maps a person-naming column to whether it is NOT NULL.
	users map[string]bool
	// nullify are columns pointing at data this import does not carry.
	nullify []string
}

// referencePlan works out, for one table, which columns name a person and which
// point at something this run will not have.
//
// Person columns are the ones with a declared foreign key to users, plus the
// ones named for it that never got a constraint (owner_user_id, actor_user_id,
// created_by_user_id). Both halves are needed: the catalog misses the
// unconstrained ones, and the naming convention misses created_by, assigned_to,
// actor_id and the rest.
func (s *service) referencePlan(
	ctx context.Context,
	t *Table,
	destCols []repository.ColumnInfo,
	selected map[models.OrgDataGroup]bool,
) (*referencePlan, error) {
	fks, err := s.repo.ForeignKeyRefs(ctx, t.Name)
	if err != nil {
		return nil, err
	}

	notNull := make(map[string]bool, len(destCols))
	for _, c := range destCols {
		notNull[c.Name] = c.NotNull
	}

	plan := &referencePlan{users: map[string]bool{}}

	for column, target := range fks {
		if target == "users" {
			plan.users[column] = notNull[column]
			continue
		}
		if target == "organizations" || target == t.Name {
			continue
		}
		// A target this run writes is satisfiable; anything else is not.
		if dep, known := TableByName[target]; known && !dep.ImportSkip && selected[dep.Group] {
			continue
		}
		// NOT NULL crossings are meant to be impossible (GroupRequires closes
		// over them). Leave one alone rather than write a NULL the column
		// rejects: a clear constraint error beats a silent corruption.
		if notNull[column] {
			continue
		}
		plan.nullify = append(plan.nullify, column)
	}

	for _, c := range destCols {
		if _, already := plan.users[c.Name]; already {
			continue
		}
		if c.Name == "user_id" || strings.HasSuffix(c.Name, "_user_id") {
			plan.users[c.Name] = c.NotNull
		}
	}

	sort.Strings(plan.nullify)
	return plan, nil
}

// mergeOrganization applies the archive's workspace settings onto the
// destination org. Identity, ownership, and lifecycle columns are excluded:
// an archive must not be able to hand a workspace to someone else or schedule
// it for deletion.
func (s *service) mergeOrganization(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, m *Manifest) error {
	if len(m.Organization) == 0 {
		return nil
	}
	destCols, err := s.repo.TableColumns(ctx, "organizations")
	if err != nil {
		return err
	}
	cols := make([]string, 0, len(destCols))
	for _, c := range destCols {
		if !c.Writable() || orgMergeExcluded[c.Name] {
			continue
		}
		if _, present := m.Organization[c.Name]; present {
			cols = append(cols, c.Name)
		}
	}
	if len(cols) == 0 {
		return nil
	}
	raw, err := json.Marshal(m.Organization)
	if err != nil {
		return err
	}
	return s.repo.MergeOrganization(ctx, tx, orgID, cols, raw)
}

// orgMergeExcluded are the organization columns an archive may never set.
var orgMergeExcluded = map[string]bool{
	"id":                     true,
	"owner_user_id":          true,
	"slug":                   true,
	"created_at":             true,
	"deletion_scheduled_at":  true,
	"deletion_scheduled_for": true,
	// Risk posture is one platform's verdict about a tenant on its own
	// infrastructure, reached from evidence the destination never saw. Letting
	// an archive carry it would import a suspension nobody here decided, and
	// letting it carry "trusted" would let an archive clear one.
	"risk_state":        true,
	"risk_score":        true,
	"risk_reason":       true,
	"risk_signals":      true,
	"risk_evaluated_at": true,
}

// buildUserMap resolves archive members to destination accounts by email.
// Anyone without an account here has their rows reassigned to the importer,
// which keeps every foreign key valid without an archive being able to conjure
// accounts on the destination instance.
func (s *service) buildUserMap(ctx context.Context, m *Manifest, actor uuid.UUID) (map[uuid.UUID]uuid.UUID, []models.OrgArchiveUser, error) {
	out := make(map[uuid.UUID]uuid.UUID, len(m.Members))
	if len(m.Members) == 0 {
		return out, nil, nil
	}

	emails := make([]string, 0, len(m.Members))
	for _, u := range m.Members {
		emails = append(emails, strings.ToLower(strings.TrimSpace(u.Email)))
	}
	found, err := s.repo.ResolveUsersByEmail(ctx, emails)
	if err != nil {
		return nil, nil, fmt.Errorf("match members: %w", err)
	}

	var unknown []models.OrgArchiveUser
	for _, u := range m.Members {
		if id, ok := found[strings.ToLower(strings.TrimSpace(u.Email))]; ok {
			out[u.ID] = id
			continue
		}
		if actor != uuid.Nil {
			out[u.ID] = actor
		}
		unknown = append(unknown, u)
	}
	return out, unknown, nil
}

// restoreBlobs writes the archive's objects back into this instance's storage
// under their original keys, so the rows that reference them resolve.
func (s *service) restoreBlobs(ctx context.Context, entries map[string]*zip.File, m *Manifest) []string {
	if len(m.Blobs) == 0 {
		return nil
	}
	if s.blobs == nil {
		return []string{fmt.Sprintf(
			"Object storage is not configured here, so %d attachment(s) in the archive were not restored.", len(m.Blobs))}
	}

	var failed int
	for _, b := range m.Blobs {
		entry, ok := entries[b.Path]
		if !ok {
			failed++
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			failed++
			continue
		}
		err = s.blobs.Put(ctx, b.Key, rc, "")
		_ = rc.Close()
		if err != nil {
			failed++
		}
	}
	if failed > 0 {
		return []string{fmt.Sprintf("%d attachment(s) could not be restored to object storage.", failed)}
	}
	return nil
}

// resolveArchiveKey derives the archive key when the archive has secrets and a
// passphrase was supplied.
func (s *service) resolveArchiveKey(m *Manifest, passphrase string) ([]byte, error) {
	if m.Secrets == nil || passphrase == "" {
		return nil, nil
	}
	return DeriveArchiveKey(passphrase, m.Secrets)
}

// ---------- archive reading helpers ----------

func indexEntries(zr *zip.Reader) map[string]*zip.File {
	out := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		out[f.Name] = f
	}
	return out
}

func readManifest(entries map[string]*zip.File) (*Manifest, error) {
	entry, ok := entries[manifestPath]
	if !ok {
		return nil, errors.New("archive has no manifest.json, so it is not a Warmbly organization archive")
	}
	rc, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var m Manifest
	dec := json.NewDecoder(io.LimitReader(rc, maxManifestBytes))
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("archive manifest is unreadable: %w", err)
	}
	return &m, nil
}

func manifestTables(m *Manifest) map[string]ManifestTable {
	out := make(map[string]ManifestTable, len(m.Tables))
	for _, t := range m.Tables {
		out[t.Name] = t
	}
	return out
}

// readLine reads one newline-terminated record of any length. bufio.Scanner is
// not usable here: a single inbox row can carry a megabyte of message body,
// well past the Scanner token cap.
func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	line = trimNewline(line)
	return line, err
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
