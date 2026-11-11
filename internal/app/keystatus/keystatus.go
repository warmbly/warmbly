// Package keystatus reports, and optionally reseals, the values the instance-wide keys seal.
package keystatus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/warmbly/warmbly/internal/app/mailcred"
	"github.com/warmbly/warmbly/internal/app/orgtransfer"
	"github.com/warmbly/warmbly/internal/app/twofa"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/pkg/secretset"
)

// A rotation ships the new primary with the old key as a read key, reseals, and only then retires the old key.

// Domain names which configured key seals a column.
type Domain string

const (
	DomainInstance Domain = "instance"
	DomainMFA      Domain = "mfa"
)

// SealedColumn is one column holding values sealed under an instance-wide key.
type SealedColumn struct {
	Table  string `json:"table"`
	Column string `json:"column"`
	Domain Domain `json:"domain"`
	guard  string
}

// instanceOnlyColumns are sealed under an instance-wide key on tables that never travel in a workspace archive.
var instanceOnlyColumns = []SealedColumn{
	{Table: "agency_vendor_connections", Column: "credentials", Domain: DomainInstance},
	{Table: "cloud_link", Column: "token", Domain: DomainInstance},
	{Table: "mailbox_erasures", Column: "refresh_token", Domain: DomainInstance},
	{Table: "user_totp_settings", Column: "totp_secret_sealed", Domain: DomainMFA},
}

// Columns is every column the instance keys seal: the archive registry's instance-key and mailbox secrets plus the instance-only ones.
func Columns() []SealedColumn {
	var out []SealedColumn
	for _, t := range orgtransfer.Tables {
		for _, s := range t.Secrets {
			if s.Domain == orgtransfer.KeyDomainInstance || s.Domain == orgtransfer.KeyDomainMailbox {
				out = append(out, SealedColumn{Table: t.Name, Column: s.Column, Domain: DomainInstance, guard: s.Guard})
			}
		}
	}
	return append(out, instanceOnlyColumns...)
}

// Count is where one column's values stand against the configured keys.
type Count struct {
	SealedColumn
	Primary    int  `json:"primary"`
	ReadKey    int  `json:"read_key"`
	Workspace  int  `json:"workspace"`
	Unsealed   int  `json:"unsealed"`
	Unreadable int  `json:"unreadable"`
	Resealed   int  `json:"resealed,omitempty"`
	Raced      int  `json:"changed_meanwhile,omitempty"`
	Missing    bool `json:"missing,omitempty"`
}

// Report is the whole answer, shaped for JSON as much as for a terminal.
type Report struct {
	Applied          bool      `json:"applied"`
	InstanceReadKeys int       `json:"instance_read_keys"`
	MFAReadKeys      int       `json:"mfa_read_keys"`
	MFAConfigured    bool      `json:"mfa_configured"`
	Columns          []Count   `json:"columns"`
	Windows          []Window  `json:"grace_windows"`
	CheckedAt        time.Time `json:"checked_at"`
}

// OnReadKey is how many values still open only under a read key.
func (r Report) OnReadKey() int {
	n := 0
	for _, c := range r.Columns {
		n += c.ReadKey
	}
	return n
}

// Unreadable is how many values open under no configured key.
func (r Report) Unreadable() int {
	n := 0
	for _, c := range r.Columns {
		n += c.Unreadable
	}
	return n
}

// Window is one previous-credential acceptance window and whether it is still open.
type Window struct {
	Name    string    `json:"name"`
	Until   time.Time `json:"until"`
	Open    bool      `json:"open"`
	Entries int       `json:"entries"`
}

// Keyring is the configured keys the backend opens and seals with.
type Keyring struct {
	instance     *encrypt.Encrypter
	instanceRead int
	mfa          *[32]byte
	mfaRead      [][32]byte
}

// ErrUnreadable means some stored values open under no configured key.
var ErrUnreadable = errors.New("some stored values do not open under any configured key")

// ErrOnReadKey means a read key is still needed; run keys reseal --apply before retiring it.
var ErrOnReadKey = errors.New("some stored values still open only under a read key")

// DB is the subset of a pgx pool a scan needs.
type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Options controls one scan.
type Options struct {
	Batch int
	Write bool
}

// LoadKeyring resolves the keys exactly as the backend does at boot.
func LoadKeyring(ctx context.Context) (*Keyring, error) {
	instance, err := encrypt.FromEnv()
	if err != nil {
		return nil, errors.New("CREDENTIALS_ENCRYPTION_KEY or CREDENTIALS_ENCRYPTION_READ_KEYS is invalid: each must be 64 hex characters")
	}
	if instance == nil {
		return nil, errors.New("CREDENTIALS_ENCRYPTION_KEY is not set. Run this inside the backend container, where the keys are configured.")
	}
	reads, _ := secretset.Parse(os.Getenv("CREDENTIALS_ENCRYPTION_READ_KEYS"))
	k := &Keyring{instance: instance}
	for _, r := range reads {
		if r != os.Getenv("CREDENTIALS_ENCRYPTION_KEY") {
			k.instanceRead++
		}
	}

	var primary string
	var mfaReads []string
	if cfg, err := config.NewConfig(ctx); err == nil {
		if auth, aerr := cfg.LoadAuthConfig(ctx); aerr == nil {
			primary, mfaReads = auth.TwoFASecret, auth.TwoFAReadSecrets
		}
	}
	if primary == "" {
		primary = strings.TrimSpace(os.Getenv("TWOFA_SECRET"))
		if primary == "" {
			primary = strings.TrimSpace(os.Getenv("AUTH_SECRET"))
		}
		if mfaReads, err = secretset.Parse(os.Getenv("TWOFA_READ_SECRETS")); err != nil {
			return nil, fmt.Errorf("TWOFA_READ_SECRETS: %w", err)
		}
	}
	if primary != "" {
		key := twofa.DeriveKey(primary)
		k.mfa = &key
		for _, s := range mfaReads {
			if s != primary {
				k.mfaRead = append(k.mfaRead, twofa.DeriveKey(s))
			}
		}
	}
	return k, nil
}

// Scan classifies every sealed value and, with Write, reseals the read-key ones.
func Scan(ctx context.Context, db DB, keys *Keyring, opts Options) (Report, error) {
	size := opts.Batch
	if size < 1 {
		size = 500
	}
	now := time.Now()
	report := Report{Applied: opts.Write, InstanceReadKeys: keys.instanceRead, MFAReadKeys: len(keys.mfaRead), MFAConfigured: keys.mfa != nil, Windows: Windows(now), CheckedAt: now.UTC()}
	for _, col := range Columns() {
		if col.Domain == DomainMFA && keys.mfa == nil {
			continue
		}
		count, err := scanColumn(ctx, db, keys, col, size, opts.Write)
		if err != nil {
			return report, err
		}
		report.Columns = append(report.Columns, count)
	}
	return report, nil
}

// scanColumn classifies every value in one column and, with write, reseals the read-key ones by compare-and-swap.
func scanColumn(ctx context.Context, db DB, keys *Keyring, col SealedColumn, size int, write bool) (Count, error) {
	out := Count{SealedColumn: col}
	var exists bool
	if err := db.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, col.Table).Scan(&exists); err != nil {
		return out, fmt.Errorf("reading %s: %w", col.Table, err)
	}
	if !exists {
		out.Missing = true
		return out, nil
	}
	table, column := pgx.Identifier{col.Table}.Sanitize(), pgx.Identifier{col.Column}.Sanitize()
	filter := ""
	if col.guard != "" {
		filter = " AND " + pgx.Identifier{col.guard}.Sanitize()
	}
	query := fmt.Sprintf(`SELECT ctid::text AS row_tid, %[2]s::text FROM %[1]s WHERE %[2]s IS NOT NULL AND %[2]s::text <> ''%[3]s AND ctid > $1::tid ORDER BY ctid LIMIT $2`, table, column, filter)
	// The ctid and the old value together are the compare: a row rewritten meanwhile matches neither.
	update := fmt.Sprintf(`UPDATE %[1]s SET %[2]s = $1 WHERE ctid = $2::tid AND %[2]s::text = $3`, table, column)

	// One snapshot for the reads, so a tuple this run rewrites is never read back.
	snap, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, fmt.Errorf("reading %s.%s: %w", col.Table, col.Column, err)
	}
	defer func() { _ = snap.Rollback(ctx) }()

	after := "(0,0)"
	for {
		rows, err := snap.Query(ctx, query, after, size)
		if err != nil {
			return out, fmt.Errorf("reading %s.%s: %w", col.Table, col.Column, err)
		}
		type row struct{ ctid, value string }
		var page []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.ctid, &r.value); err != nil {
				rows.Close()
				return out, fmt.Errorf("reading %s.%s: %w", col.Table, col.Column, err)
			}
			page = append(page, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return out, fmt.Errorf("reading %s.%s: %w", col.Table, col.Column, err)
		}
		for _, r := range page {
			resealed, changed, err := resealValue(keys, col, r.value)
			switch {
			case errors.Is(err, errUnsealed):
				out.Unsealed++
				continue
			case errors.Is(err, errWorkspaceSealed):
				out.Workspace++
				continue
			case err != nil:
				out.Unreadable++
				continue
			case !changed:
				out.Primary++
				continue
			}
			out.ReadKey++
			if !write {
				continue
			}
			tag, err := db.Exec(ctx, update, resealed, r.ctid, r.value)
			if err != nil {
				return out, fmt.Errorf("resealing %s.%s: %w", col.Table, col.Column, err)
			}
			if tag.RowsAffected() == 1 {
				out.Resealed++
			} else {
				out.Raced++
			}
		}
		if len(page) < size {
			return out, nil
		}
		after = page[len(page)-1].ctid
	}
}

var (
	errUnsealed        = errors.New("value is stored outside the sealed format")
	errWorkspaceSealed = errors.New("value is sealed under its workspace key")
)

func resealValue(keys *Keyring, col SealedColumn, value string) (string, bool, error) {
	if col.Domain == DomainMFA {
		return twofa.Reseal(*keys.mfa, keys.mfaRead, value)
	}
	// A workspace-sealed value needs no instance key, so a rotation leaves it alone.
	if mailcred.IsOrgSealed(value) {
		return "", false, errWorkspaceSealed
	}
	if !encrypt.LooksSealed(value) {
		return "", false, errUnsealed
	}
	return keys.instance.Reseal(value)
}

// Windows reports every previous-credential window the instance is configured with.
func Windows(now time.Time) []Window {
	var out []Window
	for _, w := range []struct{ name, list string }{
		{"INTERNAL_API_PREVIOUS_UNTIL", "INTERNAL_API_PREVIOUS_TOKENS"},
		{"NODE_BROKER_PREVIOUS_UNTIL", "NODE_BROKER_PREVIOUS_TOKENS"},
		{"UNSUBSCRIBE_SIGNING_READ_UNTIL", "UNSUBSCRIBE_SIGNING_READ_SECRETS"},
	} {
		raw := strings.TrimSpace(os.Getenv(w.name))
		entries, _ := secretset.Parse(os.Getenv(w.list))
		if raw == "" && len(entries) == 0 {
			continue
		}
		until, _ := time.Parse(time.RFC3339, raw)
		out = append(out, Window{Name: w.name, Until: until, Open: now.Before(until), Entries: len(entries)})
	}
	return out
}
