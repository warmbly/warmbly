package orgtransfer

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/mailcred"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
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

// ErrForeignRows refuses an archive naming records another workspace on this
// instance owns: an import only writes and references the destination's own.
var ErrForeignRows = errors.New("this archive holds records that already belong to another workspace on this instance, so nothing was imported. " +
	"Import it into the workspace it was exported from, or into a workspace on another instance")

// ErrOAuthAppConflict refuses an archive whose own OAuth app collides with one the destination already uses for that provider.
var ErrOAuthAppConflict = errors.New("this workspace already has its own OAuth app for a provider the archive brings one for, and mailboxes can only be refreshed by the app that signed them in, so nothing was imported. " +
	"Remove that app under Settings > Mailbox sign-in, or import into a workspace without one")

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

	result := &ImportResult{RowCounts: map[string]int64{}, Warnings: manifest.attachmentCompatibilityWarnings()}

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

	rules := &ruleEnv{orgID: orgID, heldApps: map[uuid.UUID]string{}}
	if pu, ok := s.blobs.(storage.PublicURLer); ok {
		rules.logos = pu
	}
	if rules.developerBlocked, err = s.repo.DeveloperBlocked(ctx, tx, orgID, opts.ActorUserID); err != nil {
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
			rules:      rules,
		})
		if err != nil {
			return nil, err
		}
		if t.Name == "oauth_applications" {
			if err := holdImportedApps(ctx, tx, orgID, rules.heldApps); err != nil {
				return nil, fmt.Errorf("hold imported apps: %w", err)
			}
		}
		if n > 0 {
			result.RowCounts[t.Name] = n
		}
		result.Warnings = append(result.Warnings, warn...)
	}

	report(93, "restoring attachments")
	if warn := s.restoreBlobs(ctx, tx, orgID, entries, manifest); len(warn) > 0 {
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

	if selected[models.OrgDataGroupAutomations] {
		stale, err := s.repo.MarkIntegrationsNeedingReconnect(ctx, tx, orgID, integrationReconnectDetail, inboundReconnectDetail)
		if err != nil {
			return nil, err
		}
		for _, c := range stale {
			result.Warnings = append(result.Warnings, integrationReconnectWarning(c))
		}
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
	// rules re-apply write rules to the tables in importRules.
	rules *ruleEnv
	// bytea names the current table's bytea columns on this instance.
	bytea map[string]bool
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
	var resetOnOverwrite []string
	if t.ResetOnOverwrite {
		for _, c := range t.ResetOnImport {
			if writable[c] {
				resetOnOverwrite = append(resetOnOverwrite, c)
			}
		}
	}
	for _, c := range t.ResetColumnsOnOverwrite {
		if writable[c] && reset[c] && !slices.Contains(resetOnOverwrite, c) {
			resetOnOverwrite = append(resetOnOverwrite, c)
		}
	}
	var invalidations []repository.ImportInvalidation
	for _, relation := range t.InvalidateOnImport {
		columns, err := s.repo.TableColumns(ctx, relation.Table)
		if err != nil {
			return 0, nil, fmt.Errorf("inspect checkpoint relation %s: %w", relation.Table, err)
		}
		if len(columns) > 0 {
			if len(relation.ResetColumns) > 0 {
				var available []string
				for _, c := range relation.ResetColumns {
					if slices.ContainsFunc(columns, func(info repository.ColumnInfo) bool { return info.Name == c }) {
						available = append(available, c)
					}
				}
				if len(available) == 0 {
					continue
				}
				relation.ResetColumns = available
			}
			invalidations = append(invalidations, relation)
		}
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
	// Columns rebuilt from a sealed one are written whenever that one arrives.
	for _, sc := range t.Secrets {
		if !slices.Contains(insertCols, sc.Column) {
			// Without its sealed column only a clear value may travel (a previous-release archive), never a digest.
			insertCols = slices.DeleteFunc(insertCols, func(c string) bool { return sc.Digest != "" && c == sc.Digest })
			continue
		}
		for _, c := range []string{sc.Plaintext, sc.Digest} {
			if c != "" && writable[c] && !slices.Contains(insertCols, c) {
				insertCols = append(insertCols, c)
			}
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

	pk, err := s.repo.PrimaryKeyColumns(ctx, t.Name)
	if err != nil {
		return 0, nil, err
	}
	if len(pk) == 0 && ic.conflict == models.OrgImportConflictOverwrite {
		warnings = append(warnings, fmt.Sprintf(
			"%s has no primary key, so existing rows there were kept rather than overwritten.", t.Name))
	}

	ic.bytea = byteaColumns(destCols)

	refs, err := s.referencePlan(ctx, t, destCols, ic.selected)
	if err != nil {
		return 0, nil, err
	}
	tenantRefs, err := s.tenantReferences(ctx, t.Name, insertCols)
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
		foreign, err := s.repo.CountForeignRows(ctx, tx, t.Name, t.OwnerScope(), pk, tenantRefs, orgID, batch)
		if err != nil {
			return err
		}
		if foreign > 0 {
			return fmt.Errorf("%s: %w", t.Name, ErrForeignRows)
		}
		var keptSecrets map[uuid.UUID]string
		if t.Name == oauthAppsTable {
			if err := s.refuseChangedOAuthApps(ctx, tx, orgID, batch); err != nil {
				return err
			}
			if keptSecrets, err = s.repo.OAuthAppSecrets(ctx, tx, orgID, blankSecretApps(batch)); err != nil {
				return err
			}
		}
		n, err := s.repo.InsertBatch(ctx, tx, t.Name, insertCols, batch, ic.conflict, pk, t.OwnerScope(), orgID, resetOnOverwrite, invalidations)
		if err != nil {
			if t.Name == oauthAppsTable && repository.IsUniqueViolation(err) {
				return ErrOAuthAppConflict
			}
			return err
		}
		if t.Name == oauthAppsTable {
			if err := s.requireImportedOAuthApps(ctx, tx, orgID, batch); err != nil {
				return err
			}
			// An archive without credentials never blanks the secret of an app the destination already holds.
			for id, secret := range keptSecrets {
				if err := s.repo.RestoreOAuthAppSecret(ctx, tx, orgID, id, secret); err != nil {
					return err
				}
			}
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

	if rule := importRules[t.Name]; rule != nil && ic.rules != nil {
		rule(ic.rules, obj)
	}

	for _, sc := range t.Secrets {
		raw, ok := obj[sc.Column]
		if sc.Plaintext != "" && !IsSealed(jsonString(raw)) {
			importUnsealedSecret(obj, sc, raw)
			continue
		}
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
			setDerived(obj, sc, "")
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
		if ic.bytea[sc.Column] {
			resealed = encodeBytea(resealed)
		}
		enc, merr := json.Marshal(resealed)
		if merr != nil {
			return nil, merr
		}
		obj[sc.Column] = enc
		setDerived(obj, sc, plain)
	}

	return json.Marshal(obj)
}

// integrationReconnectDetail is the health detail an integration without its credentials carries.
const integrationReconnectDetail = "Credentials did not come across with the workspace import: reconnect this integration."

// inboundReconnectDetail is the health detail of a connection whose inbound URL alone did not travel.
const inboundReconnectDetail = "The inbound URL did not come across with the workspace import: reconnect this integration for a new one."

// integrationReconnectWarning is the one warning per connection, naming its provider and label, never a credential.
func integrationReconnectWarning(c repository.IntegrationReconnect) string {
	name := c.Provider
	if c.Label != "" {
		name = fmt.Sprintf("%s (%s)", c.Provider, c.Label)
	}
	missing := "its credentials"
	switch {
	case c.CredentialsMissing && c.InboundClosed:
		missing = "its credentials or inbound URL"
	case !c.CredentialsMissing:
		missing = "its inbound URL"
	}
	return fmt.Sprintf("The %s integration arrived without %s and is marked for reconnection.", name, missing)
}

// importUnsealedSecret keeps only a clear value the archive carried (the sweep seals it here), never foreign ciphertext or a digest;
// with no clear value it blanks the secret so the reconnect step closes the URL and names the connection.
func importUnsealedSecret(obj map[string]json.RawMessage, sc SecretColumn, raw json.RawMessage) {
	clear := jsonString(obj[sc.Plaintext])
	hadSecret := jsonString(raw) != "" || string(bytes.TrimSpace(raw)) == `""` || (sc.Digest != "" && jsonString(obj[sc.Digest]) != "")
	obj[sc.Column] = json.RawMessage(`null`)
	setDerived(obj, sc, "")
	switch {
	case clear != "":
		enc, _ := json.Marshal(clear)
		obj[sc.Plaintext] = enc
	case hadSecret:
		obj[sc.Column] = json.RawMessage(`""`)
	}
}

// setDerived rebuilds the clear and digest columns of a sealed one from its plaintext; "" clears them.
func setDerived(obj map[string]json.RawMessage, sc SecretColumn, plain string) {
	set := func(column, value string) {
		if column == "" {
			return
		}
		if value == "" {
			obj[column] = json.RawMessage(`null`)
			return
		}
		enc, _ := json.Marshal(value)
		obj[column] = enc
	}
	set(sc.Plaintext, plain)
	digest := ""
	if plain != "" {
		digest = repository.InboundSecretDigest(plain)
	}
	set(sc.Digest, digest)
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
	case KeyDomainMailbox:
		if s.mailSealing == mailcred.ModeOrganization {
			return mailcred.SealOrg(ctx, orgCipher, plain)
		}
		if s.creds == nil {
			return "", errors.New("this instance has no CREDENTIALS_ENCRYPTION_KEY, so mailbox credentials cannot be stored")
		}
		return s.creds.Encrypt(plain)
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

// tenantReferences are the foreign keys a written row carries into data some
// organization owns, each with the fragment selecting the destination's rows.
func (s *service) tenantReferences(ctx context.Context, table string, insertCols []string) ([]repository.TenantReference, error) {
	fks, err := s.repo.ForeignKeys(ctx, table)
	if err != nil {
		return nil, err
	}
	written := make(map[string]bool, len(insertCols))
	for _, c := range insertCols {
		written[c] = true
	}
	if t, ok := TableByName[table]; ok {
		for _, c := range t.PartnerRefs {
			delete(written, c)
		}
	}
	var out []repository.TenantReference
	for _, fk := range fks {
		carried := true
		for _, c := range fk.Columns {
			carried = carried && written[c]
		}
		if !carried {
			continue
		}
		var owner string
		switch fk.RefTable {
		case "users":
			// People are matched to destination accounts by email.
			continue
		case "organizations":
			owner = `id = $1`
		default:
			dep, ok := TableByName[fk.RefTable]
			if !ok {
				// Instance-wide data, or a reference referencePlan clears.
				continue
			}
			owner = dep.OwnerScope()
		}
		out = append(out, repository.TenantReference{ForeignKey: fk, RefOwner: owner})
	}
	return out, nil
}

// mergeOrganization applies the archive's workspace settings onto the
// destination org. Identity, ownership, and lifecycle columns are excluded:
// an archive must not be able to hand a workspace to someone else or schedule
// it for deletion.
func (s *service) mergeOrganization(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, m *Manifest) error {
	if len(m.Organization) == 0 {
		return nil
	}
	cleanArchiveOrgName(m.Organization)
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
	if err := s.repo.MergeOrganization(ctx, tx, orgID, cols, raw); err != nil {
		return err
	}

	// The verdict columns are excluded above, which preserves whatever the
	// DESTINATION already held rather than resetting them. On a workspace that
	// had its own verified forms domain, importing a different one would
	// inherit that verified flag and start building form URLs on a name whose
	// CNAME still points at the source instance. Clear it whenever the domain
	// itself moved; the hourly sweep re-earns it once DNS points here.
	for _, c := range cols {
		if c != "forms_domain" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE organizations
			SET forms_domain_verified = false, forms_domain_verified_at = NULL
			WHERE id = $1
		`, orgID); err != nil {
			return err
		}
		break
	}
	return nil
}

// cleanArchiveOrgName keeps the destination's name unless the archive's passes
// the same rules as a rename.
func cleanArchiveOrgName(org map[string]any) {
	raw, ok := org["name"]
	if !ok {
		return
	}
	name, _ := raw.(string)
	if clean := displayname.Clean(name, displayname.Workspace); clean != "" {
		org["name"] = clean
		return
	}
	delete(org, "name")
}

// orgMergeExcluded are the organization columns an archive may never set.
var orgMergeExcluded = func() map[string]bool {
	out := map[string]bool{
		"id":                     true,
		"owner_user_id":          true,
		"category":               true,
		"slug":                   true,
		"created_at":             true,
		"deletion_scheduled_at":  true,
		"deletion_scheduled_for": true,
		// forms_domain itself travels: it is the customer's own name and they
		// will want it on the destination. The verdict must not, because it
		// was reached against the SOURCE instance's forms host. Importing it
		// verified would build every form URL on a name still pointing at the
		// old install. Left unverified, links fall back to the shared host and
		// the hourly sweep re-verifies once the customer repoints the record.
		"forms_domain_verified":    true,
		"forms_domain_verified_at": true,
	}
	for name := range OrgRiskColumns {
		out[name] = true
	}
	return out
}()

// OrgRiskColumns are the risk verdict an instance reached about a tenant. They
// never travel in either direction. Importing one would apply a suspension
// nobody here decided (or let an archive clear one), and exporting one would
// hand the customer the detector evidence and the reviewer's note, which the
// customer-facing endpoint deliberately withholds.
var OrgRiskColumns = map[string]bool{
	"risk_state":           true,
	"risk_score":           true,
	"risk_reason":          true,
	"risk_signals":         true,
	"risk_evaluated_at":    true,
	"risk_override":        true,
	"risk_override_reason": true,
	"risk_override_by":     true,
	"risk_override_at":     true,
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
func (s *service) restoreBlobs(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, entries map[string]*zip.File, m *Manifest) []string {
	if len(m.Blobs) == 0 {
		return nil
	}
	if s.blobs == nil {
		return []string{fmt.Sprintf(
			"Object storage is not configured here, so %d attachment(s) in the archive were not restored.", len(m.Blobs))}
	}

	// The keys come out of the archive, which is a file the customer uploaded,
	// so the archive does not get to choose where the bytes land. A key is only
	// written when it matches a shape this product mints, and a public one is
	// re-scoped to this workspace with its extension checked.
	scope, err := loadBlobKeyScope(ctx, tx, orgID)
	if err != nil {
		return []string{"Attachments were not restored: this instance could not confirm which objects the archive is allowed to write."}
	}

	var failed, refused, notImage int
	// Keys under the two org-scoped public prefixes are rewritten to this
	// workspace, so the rows that name them have to be repointed or the bytes
	// are orphaned at a path nothing reads.
	var imageOldKeys, imageNewKeys, imageURLs []string
	type urlRewrite struct{ table, column, oldKey, newURL string }
	var urlRewrites []urlRewrite

	for _, b := range m.Blobs {
		destKey, ok := scope.plan(b.Key)
		if !ok {
			refused++
			continue
		}
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
		// A key under a public prefix has to be written public-read again, or
		// the avatars, form assets and email-body images that reference it by
		// URL resolve to a 403 on an S3 backend after the move.
		if isPublicBlobKey(destKey) {
			body, contentType, ok := publicImage(rc, destKey)
			if !ok {
				_ = rc.Close()
				notImage++
				continue
			}
			var url string
			url, err = s.blobs.PutPublic(ctx, destKey, bytes.NewReader(body), contentType)
			if err == nil && url != "" {
				switch {
				case strings.HasPrefix(destKey, models.EmailImageKeyPrefix):
					imageOldKeys = append(imageOldKeys, b.Key)
					imageNewKeys = append(imageNewKeys, destKey)
					imageURLs = append(imageURLs, url)
				case publicURLColumn(b.Table, b.Column):
					// A form's logo, cover or background is referenced by URL,
					// and that URL still names the instance it came from. The
					// bytes are here now, so the row is repointed at this one.
					// The table and column are the archive's strings, so they
					// are only used after matching the compiled registry.
					urlRewrites = append(urlRewrites, urlRewrite{b.Table, b.Column, b.Key, url})
				}
			}
		} else {
			// Preserve portable private files independently of the new-upload policy.
			err = s.blobs.Put(ctx, destKey, rc, storage.DownloadType(""))
		}
		_ = rc.Close()
		if err != nil {
			failed++
		}
	}

	warnings := make([]string, 0, 3)
	// An imported image row still carries the key and the URL the source
	// instance used. The bytes now live here, under a key scoped to this
	// workspace, so the row is repointed at both. Matching on the OLD key is
	// what makes this work: that is still what the row holds at this point.
	if len(imageOldKeys) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE email_images SET storage_key = fresh.new_key, url = fresh.url
			FROM (
				SELECT unnest($2::text[]) AS old_key,
				       unnest($3::text[]) AS new_key,
				       unnest($4::text[]) AS url
			) AS fresh
			WHERE email_images.organization_id = $1 AND email_images.storage_key = fresh.old_key
		`, orgID, imageOldKeys, imageNewKeys, imageURLs); err != nil {
			warnings = append(warnings, "Email images were restored but still name the instance they came from; re-upload them if that host goes away.")
		}
	}

	// Form logos, covers and backgrounds are referenced by URL rather than by
	// key. The table and column were matched against the compiled registry
	// above, so they are this binary's own identifiers; the values are bound.
	for _, rw := range urlRewrites {
		stmt := fmt.Sprintf(
			`UPDATE %s SET %s = $1 WHERE organization_id = $2 AND %s LIKE $3`,
			pgx.Identifier{rw.table}.Sanitize(),
			pgx.Identifier{rw.column}.Sanitize(),
			pgx.Identifier{rw.column}.Sanitize(),
		)
		if _, err := tx.Exec(ctx, stmt, rw.newURL, orgID, "%"+rw.oldKey); err != nil {
			warnings = append(warnings, "Some form images were restored but still name the instance they came from; re-upload them if that host goes away.")
			break
		}
	}
	if failed > 0 {
		warnings = append(warnings, fmt.Sprintf("%d attachment(s) could not be restored to object storage.", failed))
	}
	if refused > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d object(s) in the archive named a storage location this workspace does not own, so they were not written.", refused))
	}
	if notImage > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d image(s) in the archive were not a PNG, JPG, GIF or WebP matching their name, so they were not written.", notImage))
	}
	return warnings
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

const oauthAppsTable = "organization_oauth_apps"

// requireImportedOAuthApps refuses a batch the destination skipped: its mailboxes' grants would point at no client.
func (s *service) requireImportedOAuthApps(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, batch []json.RawMessage) error {
	ids := make([]uuid.UUID, 0, len(batch))
	for _, raw := range batch {
		var row struct {
			ID uuid.UUID `json:"id"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		ids = append(ids, row.ID)
	}
	missing, err := s.repo.MissingOAuthApps(ctx, tx, orgID, ids)
	if err != nil {
		return err
	}
	if missing > 0 {
		return ErrOAuthAppConflict
	}
	return nil
}

// refuseChangedOAuthApps refuses an app id the destination holds under another client, in either conflict mode.
func (s *service) refuseChangedOAuthApps(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, batch []json.RawMessage) error {
	ids := make([]uuid.UUID, 0, len(batch))
	clients := make([]string, 0, len(batch))
	for _, raw := range batch {
		var row struct {
			ID       uuid.UUID `json:"id"`
			ClientID string    `json:"client_id"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		ids, clients = append(ids, row.ID), append(clients, row.ClientID)
	}
	changed, err := s.repo.ChangedOAuthAppClients(ctx, tx, orgID, ids, clients)
	if err != nil {
		return err
	}
	if changed > 0 {
		return ErrOAuthAppConflict
	}
	return nil
}

// blankSecretApps lists the archived OAuth apps that carry no client secret.
func blankSecretApps(batch []json.RawMessage) []uuid.UUID {
	var ids []uuid.UUID
	for _, raw := range batch {
		var row struct {
			ID           uuid.UUID `json:"id"`
			ClientSecret string    `json:"client_secret"`
		}
		if json.Unmarshal(raw, &row) == nil && row.ClientSecret == "" {
			ids = append(ids, row.ID)
		}
	}
	return ids
}
