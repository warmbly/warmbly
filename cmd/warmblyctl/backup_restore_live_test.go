package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

// TestLiveBackupRestoreRoundTrip dumps a migrated database with the client
// backup resolves, restores it with the client restore resolves, and replays a
// bundle as a PostgreSQL 17+ pg_dump writes it. It runs in a scratch database
// on the server WARMBLY_TEST_DB names, because a restore replaces the schema.
func TestLiveBackupRestoreRoundTrip(t *testing.T) {
	base := os.Getenv("WARMBLY_TEST_DB")
	if base == "" {
		t.Skip("WARMBLY_TEST_DB is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	dsn := scratchDatabase(ctx, t, base)
	if err := db.RunMigrations(dsn); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	execSQL(ctx, t, dsn, `CREATE TABLE public.restore_probe (id int PRIMARY KEY, body text);
		CREATE INDEX restore_probe_trgm ON public.restore_probe USING gin (body public.gin_trgm_ops);
		INSERT INTO public.restore_probe VALUES (1, 'kept'), (2, 'SET transaction_timeout = 0;');`)

	server, err := readPGServer(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	pgDump, err := pgTool("pg_dump", server)
	if err != nil {
		t.Fatal(err)
	}
	psql, err := pgTool("psql", server)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("server %s, %s, %s", server.version, pgClientVersion(pgDump), pgClientVersion(psql))

	dumpFile, _, dumpSHA, err := dumpDatabase(ctx, pgDump, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(dumpFile) })
	dump, err := os.ReadFile(dumpFile)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bundle := writeTestBundle(t, dir, "current.tar.gz", dump)
	if dumpSHA != sha256Hex(dump) {
		t.Fatal("dumpDatabase reported a digest that is not the dump's")
	}

	// What the restore must replace: a row added and a table created after the backup.
	execSQL(ctx, t, dsn, `INSERT INTO public.restore_probe VALUES (3, 'after the backup');
		CREATE TABLE public.not_in_bundle (id int);`)
	if err := restoreDatabase(ctx, psql, dsn, bundle, server); err != nil {
		t.Fatalf("restoring its own backup: %v", err)
	}
	assertProbe(ctx, t, dsn, "1:kept,2:SET transaction_timeout = 0;")
	if n := scalar(ctx, t, dsn, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename = 'not_in_bundle'`); n != 0 {
		t.Fatal("a table created after the backup survived the restore")
	}
	if n := scalar(ctx, t, dsn, `SELECT count(*) FROM public.schema_migrations`); n == 0 {
		t.Fatal("the restored database has no schema_migrations row")
	}

	// A bundle written by a newer pg_dump carries transaction_timeout in its preamble.
	newer := strings.Replace(string(dump), "SET lock_timeout = 0;\n", "SET lock_timeout = 0;\nSET transaction_timeout = 0;\n", 1)
	if newer == string(dump) && server.major < 17 {
		t.Fatal("the dump has no lock_timeout line to put transaction_timeout after")
	}
	if err := restoreDatabase(ctx, psql, dsn, writeTestBundle(t, dir, "newer.tar.gz", []byte(newer)), server); err != nil {
		t.Fatalf("restoring a bundle written by a newer pg_dump: %v", err)
	}
	assertProbe(ctx, t, dsn, "1:kept,2:SET transaction_timeout = 0;")

	// A load that fails at its very end changes nothing.
	execSQL(ctx, t, dsn, `INSERT INTO public.restore_probe VALUES (4, 'before the failed restore')`)
	broken := string(dump) + "\nSELECT 1 / 0;\n"
	err = restoreDatabase(ctx, psql, dsn, writeTestBundle(t, dir, "broken.tar.gz", []byte(broken)), server)
	if err == nil {
		t.Fatal("a dump ending in an error restored without one")
	}
	if !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("the failure does not say the database is untouched: %v", err)
	}
	assertProbe(ctx, t, dsn, "1:kept,2:SET transaction_timeout = 0;,4:before the failed restore")
}

func writeTestBundle(t *testing.T, dir, name string, dump []byte) string {
	t.Helper()
	dumpFile := filepath.Join(dir, name+".sql")
	if err := os.WriteFile(dumpFile, dump, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	m := manifest{Version: bundleVersion, CreatedAt: time.Now().UTC(), Database: "test", DatabaseSHA: sha256Hex(dump)}
	if err := writeBundle(path, m, dumpFile, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := verifyDump(path, m.DatabaseSHA); err != nil {
		t.Fatal(err)
	}
	return path
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// scratchDatabase creates an empty database next to base and drops it when the test ends.
func scratchDatabase(ctx context.Context, t *testing.T, base string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("WARMBLY_TEST_DB is not a URL: %v", err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	name := "warmbly_restore_" + hex.EncodeToString(suffix)
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), base)
		if err != nil {
			return
		}
		defer func() { _ = c.Close(context.Background()) }()
		_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	u.Path = "/" + name
	return u.String()
}

func execSQL(ctx context.Context, t *testing.T, dsn, sql string) {
	t.Helper()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	if _, err := c.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
}

func scalar(ctx context.Context, t *testing.T, dsn, sql string) int {
	t.Helper()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	var n int
	if err := c.QueryRow(ctx, sql).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func assertProbe(ctx context.Context, t *testing.T, dsn, want string) {
	t.Helper()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	var got string
	if err := c.QueryRow(ctx, `SELECT string_agg(id || ':' || body, ',' ORDER BY id) FROM public.restore_probe`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("restore_probe holds %q, want %q", got, want)
	}
	var idx int
	if err := c.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE indexname = 'restore_probe_trgm'`).Scan(&idx); err != nil || idx != 1 {
		t.Fatalf("the trigram index did not come back (count %d, err %v)", idx, err)
	}
}
