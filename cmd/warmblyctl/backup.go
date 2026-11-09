package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/version"
)

// Instance-level backup and restore: one bundle holding everything a Warmbly
// install is, restorable onto a fresh host.
//
// This is deliberately not `warmblyctl org export`. That one moves ONE
// workspace between two running instances and re-seals its secrets for the
// destination's keys. This one moves THE INSTANCE: every workspace, every
// user, the platform admins, the API keys, and the keys those secrets are
// sealed with, onto a host that has nothing yet.
//
// The reason it is one command rather than a documented list of steps is that
// the parts are only a backup together. A pg_dump alone restores an instance
// whose every mailbox credential decrypts to nothing, because the ciphertext
// is in the database and the key that opens it is in .env; the blob root alone
// restores bodies nothing points at. The bundle carries all three and the
// restore refuses to run when the keys do not match, which is the failure this
// command exists to make impossible.

// bundleVersion is the archive layout. The restore refuses a version it does
// not know rather than half-applying an archive it cannot read.
const bundleVersion = 1

// Paths inside the bundle.
const (
	manifestPath = "manifest.json"
	databasePath = "database.sql"
	keysPath     = "keys.env"
	blobsPrefix  = "blobs/"
)

// backupKeys are the environment values that belong in the bundle. The first
// two are unrecoverable: every sealed mailbox credential and every per-org DEK
// is opened with them, so a database without them is not a backup. The rest
// are here because restoring without them signs out every session and breaks
// every worker and websocket connection, which reads as a broken restore.
var backupKeys = []string{
	"CREDENTIALS_ENCRYPTION_KEY",
	"CREDENTIALS_ENCRYPTION_READ_KEYS",
	"KMS_LOCAL_MASTER_KEY",
	"AUTH_SECRET",
	"TWOFA_SECRET",
	"TWOFA_READ_SECRETS",
	"UNSUBSCRIBE_SIGNING_SECRET",
	"UNSUBSCRIBE_SIGNING_READ_SECRETS",
	"UNSUBSCRIBE_SIGNING_READ_UNTIL",
	"DOMAIN_PROOF_SECRET",
	"INTERNAL_API_TOKEN",
	"INTERNAL_API_PREVIOUS_TOKENS",
	"INTERNAL_API_PREVIOUS_UNTIL",
	"NODE_BROKER_TOKEN",
	"NODE_BROKER_PREVIOUS_TOKENS",
	"NODE_BROKER_PREVIOUS_UNTIL",
	"SECRET_KEY_BASE",
}

// unrecoverableKeys are the two the restore checks. A mismatch on either is
// refused, because it produces an instance that looks restored and whose
// mailboxes authenticate against nothing.
var unrecoverableKeys = []string{"CREDENTIALS_ENCRYPTION_KEY", "KMS_LOCAL_MASTER_KEY"}

// manifest describes what a bundle holds, so the restore can say what it is
// about to do before it does it.
type manifest struct {
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	AppVersion  string    `json:"app_version"`
	AppURL      string    `json:"app_url,omitempty"`
	Database    string    `json:"database"`
	DatabaseSHA string    `json:"database_sha256"`
	// PostgresVersion and DumpClient say which server was dumped and with what.
	PostgresVersion string `json:"postgres_version,omitempty"`
	DumpClient      string `json:"pg_dump_version,omitempty"`
	// Counts are for the confirmation line, not for logic.
	Organizations int `json:"organizations"`
	Users         int `json:"users"`
	Mailboxes     int `json:"mailboxes"`
	// BlobProvider records why blobs are or are not in the bundle. An s3
	// install keeps its bodies in the bucket, and the bucket is not ours to
	// copy, so the archive says so instead of pretending to be complete.
	BlobProvider string `json:"blob_provider"`
	BlobRoot     string `json:"blob_root,omitempty"`
	BlobFiles    int    `json:"blob_files"`
	BlobBytes    int64  `json:"blob_bytes"`
	// Keys is the list of key names in keys.env, never their values.
	Keys []string `json:"keys"`
}

// ---------- backup ----------

func runBackup(ctx context.Context, args []string) error {
	fs := newFlagSet("backup")
	out := fs.String("out", "", "Path to write the bundle to. Defaults to ./warmbly-backup-<timestamp>.tar.gz")
	noKeys := fs.Bool("no-keys", false, "Leave the encryption keys out. The bundle is then not restorable on its own.")
	noBlobs := fs.Bool("no-blobs", false, "Leave message bodies, attachments and avatars out. Database only.")
	force := fs.Bool("force", false, "Overwrite the output file if it already exists")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	target := strings.TrimSpace(*out)
	if target == "" {
		target = fmt.Sprintf("warmbly-backup-%s.tar.gz", time.Now().UTC().Format("20060102-150405"))
	}
	if _, err := os.Stat(target); err == nil && !*force {
		return fmt.Errorf("%s already exists. Pass --force to overwrite it, or --out with another path.", target)
	}

	dsn, err := directDBEndpoint(ctx)
	if err != nil {
		return err
	}
	server, err := readPGServer(ctx, dsn)
	if err != nil {
		return err
	}
	pgDump, err := pgTool("pg_dump", server)
	if err != nil {
		return err
	}

	m := manifest{
		Version:         bundleVersion,
		CreatedAt:       time.Now().UTC(),
		AppVersion:      version.String(),
		AppURL:          strings.TrimSpace(os.Getenv("APP_URL")),
		Database:        databaseName(dsn),
		PostgresVersion: server.version,
		DumpClient:      pgClientVersion(pgDump),
	}

	// Counts first: they are the line that tells an operator this bundle is of
	// the instance they meant, and a database that cannot be counted cannot be
	// dumped either, so failing here fails before anything is written.
	c, err := connect(ctx)
	if err != nil {
		return err
	}
	m.Organizations, m.Users, m.Mailboxes = instanceCounts(ctx, c)
	c.close()

	fmt.Printf("Backing up %s\n", redact(dsn))

	dumpFile, dumpSize, dumpSHA, err := dumpDatabase(ctx, pgDump, dsn)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(dumpFile)
	}()
	m.DatabaseSHA = dumpSHA
	fmt.Printf("  database    %s, PostgreSQL %s dumped by %s\n", humanBytes(dumpSize), server.version, m.DumpClient)

	var blobFiles []blobEntry
	m.BlobProvider = strings.ToLower(strings.TrimSpace(os.Getenv("BLOB_PROVIDER")))
	if m.BlobProvider == "" {
		m.BlobProvider = "filesystem"
	}
	switch {
	case *noBlobs:
		fmt.Println("  blobs       skipped (--no-blobs)")
	case m.BlobProvider != "filesystem":
		fmt.Printf("  blobs       not included: this instance stores them in %s. Back that store up separately.\n", m.BlobProvider)
	default:
		root := blobRoot()
		m.BlobRoot = root
		// The documented hand-off writes the bundle into the blob root, because
		// that is the one path the container and the host both see. Archiving
		// it into itself would grow a bundle by the size of the last one on
		// every run, so the output is excluded by path.
		blobFiles, err = collectBlobs(root, target)
		if err != nil {
			return err
		}
		for _, b := range blobFiles {
			m.BlobBytes += b.size
		}
		m.BlobFiles = len(blobFiles)
		fmt.Printf("  blobs       %d files, %s from %s\n", m.BlobFiles, humanBytes(m.BlobBytes), root)
	}

	keys := map[string]string{}
	if !*noKeys {
		for _, k := range backupKeys {
			if v := strings.TrimSpace(os.Getenv(k)); v != "" {
				keys[k] = v
				m.Keys = append(m.Keys, k)
			}
		}
		sort.Strings(m.Keys)
		missing := missingUnrecoverable(keys)
		if len(missing) > 0 {
			warn("%s not set in this environment, so %s not in the bundle. Restoring it elsewhere will not open sealed mailbox credentials.",
				strings.Join(missing, " and "), plural(len(missing), "it is", "they are"))
		}
		fmt.Printf("  keys        %d (%s)\n", len(m.Keys), strings.Join(m.Keys, ", "))
	} else {
		fmt.Println("  keys        skipped (--no-keys); this bundle cannot restore a working instance on its own")
	}

	if err := writeBundle(target, m, dumpFile, blobFiles, keys); err != nil {
		return err
	}

	st, _ := os.Stat(target)
	size := int64(0)
	if st != nil {
		size = st.Size()
	}
	fmt.Printf("\nWrote %s (%s)\n", target, humanBytes(size))
	steps := []string{
		"Copy it off this host. It holds every mailbox credential and the keys that open them,",
		"so treat the file as you would the instance itself: 0600, encrypted at rest, off-site.",
		"",
		"Restore it on another host with:",
		"  " + composeExec + "restore --file /path/to/" + filepath.Base(target),
	}
	if len(m.Keys) == 0 {
		steps = append(steps, "", "This bundle carries no keys. Copy CREDENTIALS_ENCRYPTION_KEY and KMS_LOCAL_MASTER_KEY", "from this instance's .env by hand, or the restored mailboxes will not connect.")
	}
	printSteps("Next:", steps)
	return nil
}

// ---------- restore ----------

func runRestore(ctx context.Context, args []string) error {
	fs := newFlagSet("restore")
	file := fs.String("file", "", "Path to a bundle written by warmblyctl backup (required)")
	yes := fs.Bool("yes", false, "Skip the typed confirmation. For scripts.")
	noBlobs := fs.Bool("no-blobs", false, "Restore the database only, leaving the blob root alone")
	force := fs.Bool("force", false, "Restore even though this host's encryption keys differ from the bundle's")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}
	if strings.TrimSpace(*file) == "" {
		return errors.New("--file is required. Point it at a bundle written by `warmblyctl backup`.")
	}
	m, err := readManifest(*file)
	if err != nil {
		return err
	}
	if m.Version != bundleVersion {
		return fmt.Errorf("this bundle is layout version %d and this warmblyctl reads version %d. Restore it with the Warmbly it was written by (%s).", m.Version, bundleVersion, m.AppVersion)
	}

	dsn, err := directDBEndpoint(ctx)
	if err != nil {
		return err
	}
	server, err := readPGServer(ctx, dsn)
	if err != nil {
		return err
	}
	psql, err := pgTool("psql", server)
	if err != nil {
		return err
	}

	fmt.Printf("Bundle    %s\n", *file)
	fmt.Printf("  written   %s by Warmbly %s\n", m.CreatedAt.Local().Format(time.RFC1123), m.AppVersion)
	if m.AppURL != "" {
		fmt.Printf("  from      %s\n", m.AppURL)
	}
	if m.PostgresVersion != "" {
		fmt.Printf("  dumped    PostgreSQL %s with %s\n", m.PostgresVersion, m.DumpClient)
	}
	fmt.Printf("  holds     %d organizations, %d users, %d mailboxes\n", m.Organizations, m.Users, m.Mailboxes)
	if m.BlobFiles > 0 {
		fmt.Printf("            %d blob files, %s\n", m.BlobFiles, humanBytes(m.BlobBytes))
	}
	fmt.Printf("Target    %s (PostgreSQL %s)\n\n", redact(dsn), server.version)

	// The key check is the point of this command. A restore onto a host whose
	// keys differ produces an instance that looks fine and whose every mailbox
	// fails to authenticate, days later, with no error that names the cause.
	keysMatch, err := checkRestoreKeys(*file, m, *force)
	if err != nil {
		return err
	}

	if !*yes {
		fmt.Printf("This REPLACES everything in %s. Every organization, user, campaign and\nmailbox currently on this instance is dropped and replaced by the bundle's.\n\n", databaseName(dsn))
		ok, cerr := confirmPhrase("Type 'restore' to continue: ", "restore")
		if cerr != nil {
			return cerr
		}
		if !ok {
			return errors.New("nothing was changed")
		}
		fmt.Println()
	}

	// Checked before the load starts, so a truncated copy is refused in seconds
	// rather than after replaying most of a large dump.
	if m.DatabaseSHA != "" {
		if err := verifyDump(*file, m.DatabaseSHA); err != nil {
			return err
		}
		fmt.Println("  bundle      intact")
	}

	fmt.Println("Restoring the database...")
	if err := restoreDatabase(ctx, psql, dsn, *file, server); err != nil {
		return err
	}
	fmt.Println("  database    restored")
	if err := analyzeDatabase(ctx, psql, dsn); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		warn("the database restored, but ANALYZE did not finish, so queries plan without statistics until autovacuum gathers them. Run ANALYZE by hand:\n%s", err)
	} else {
		fmt.Println("  statistics  analyzed")
	}

	if !*noBlobs && m.BlobFiles > 0 {
		root := blobRoot()
		n, bytes, rerr := restoreBlobs(*file, root)
		if rerr != nil {
			return rerr
		}
		fmt.Printf("  blobs       %d files, %s into %s\n", n, humanBytes(bytes), root)
	}

	steps := []string{}
	switch {
	case len(m.Keys) == 0:
	case keysMatch:
		steps = append(steps,
			"The bundle's keys match this host, so sealed mailbox credentials open as they did.",
		)
	default:
		steps = append(steps,
			"The keys did NOT match and --force was given, so every restored mailbox",
			"credential is unreadable. Reconnect each mailbox, or put the bundle's keys",
			"in .env and restore again.",
		)
	}
	steps = append(steps,
		"Restart the stack so every service picks the restored database up:",
		"  docker compose -p warmbly restart",
		"",
		"Then check it:",
		"  "+composeExec+"status",
	)
	printSteps("Next:", steps)
	return nil
}

// checkRestoreKeys refuses a restore whose ciphertext this host cannot open.
//
// The bundle's keys are compared against the environment rather than written
// anywhere: warmblyctl runs inside a container and the .env that would have to
// change is on the host, so the honest thing is to print the two lines and
// stop, not to half-apply and report success.
// checkRestoreKeys reports whether this host can open the bundle's ciphertext,
// and refuses the restore when it cannot unless force overrides it.
func checkRestoreKeys(file string, m manifest, force bool) (bool, error) {
	if len(m.Keys) == 0 {
		warn("This bundle carries no encryption keys. If this host's CREDENTIALS_ENCRYPTION_KEY and\nKMS_LOCAL_MASTER_KEY are not the ones the bundle was written with, every restored mailbox\ncredential will fail to decrypt.")
		return false, nil
	}
	bundled, err := readKeys(file)
	if err != nil {
		return false, err
	}
	var mismatched, missing []string
	for _, k := range unrecoverableKeys {
		want, inBundle := bundled[k]
		if !inBundle {
			continue
		}
		got := strings.TrimSpace(os.Getenv(k))
		switch {
		case got == "":
			missing = append(missing, k+"="+want)
		case got != want:
			mismatched = append(mismatched, k+"="+want)
		}
	}
	if len(mismatched) == 0 && len(missing) == 0 {
		return true, nil
	}

	lines := append(append([]string{}, missing...), mismatched...)
	msg := strings.Builder{}
	msg.WriteString("This host's encryption keys are not the ones the bundle was sealed with.\n")
	msg.WriteString("Restoring anyway gives you an instance whose mailbox credentials cannot be\ndecrypted, and there is no way to recover them afterwards.\n\n")
	msg.WriteString("Put these in the install's .env on the host, recreate the containers, then run\nthe restore again:\n\n")
	for _, l := range lines {
		msg.WriteString("  " + l + "\n")
	}
	if force {
		warn("%s", msg.String())
		warn("--force was given, so the restore continues. Mailboxes will need reconnecting.")
		return false, nil
	}
	msg.WriteString("\nPass --force only if you accept losing every stored mailbox credential.")
	return false, errors.New(msg.String())
}

// ---------- database ----------

// dumpDatabase writes a plain-SQL dump to a temp file and returns its path,
// size and digest. Plain rather than custom format so the bundle can be
// inspected, and so a restore needs psql alone.
func dumpDatabase(ctx context.Context, pgDump, dsn string) (string, int64, string, error) {
	tmp, err := os.CreateTemp("", "warmbly-dump-*.sql")
	if err != nil {
		return "", 0, "", err
	}
	path := tmp.Name()

	sum := sha256.New()
	w := bufio.NewWriterSize(io.MultiWriter(tmp, sum), 1<<20)

	// No owner or ACL statements: the destination's database user is whatever
	// its own compose file created, and it is never guaranteed to carry the
	// same name as this one's.
	cmd := pgCommand(ctx, pgDump, dsn, "--no-owner", "--no-privileges", "--format=plain")
	cmd.Stdout = w
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if rerr := cmd.Run(); rerr != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return "", 0, "", fmt.Errorf("pg_dump failed: %s", strings.TrimSpace(stderr.String()))
	}
	if ferr := w.Flush(); ferr != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return "", 0, "", ferr
	}
	size, _ := tmp.Seek(0, io.SeekCurrent)
	if cerr := tmp.Close(); cerr != nil {
		_ = os.Remove(path)
		return "", 0, "", cerr
	}
	return path, size, hex.EncodeToString(sum.Sum(nil)), nil
}

// verifyDump reads the bundle's dump once and compares its digest with the one
// the manifest recorded, so a bundle truncated by a failed copy is refused
// rather than half-applied.
func verifyDump(bundle, want string) error {
	sql, closeFn, err := openBundleEntry(bundle, databasePath)
	if err != nil {
		return err
	}
	defer closeFn()

	sum := sha256.New()
	if _, cerr := io.Copy(sum, sql); cerr != nil {
		return fmt.Errorf("reading the bundle's database dump: %w", cerr)
	}
	got := hex.EncodeToString(sum.Sum(nil))
	if got != want {
		return fmt.Errorf("this bundle is damaged: its database dump hashes to %s and the manifest says %s.\nNothing was changed. Copy the bundle again from wherever it came from.", short(got), short(want))
	}
	return nil
}

// short trims a digest to something a person can compare by eye.
func short(sum string) string {
	if len(sum) > 12 {
		return sum[:12]
	}
	return sum
}

// restoreSchemaReset runs ahead of the dump. The schema is dropped rather than
// the dump carrying DROP statements, because the destination has already had
// migrations applied at boot and objects the bundle does not know about would
// otherwise survive into the restored instance.
const restoreSchemaReset = "DROP SCHEMA IF EXISTS public CASCADE;\nCREATE SCHEMA public;\n"

// restoreDatabase replaces the schema with the dump's in one transaction, so a
// load that fails anywhere leaves the database as it was.
func restoreDatabase(ctx context.Context, psql, dsn, bundle string, server pgServer) error {
	dump, closeFn, err := openBundleEntry(bundle, databasePath)
	if err != nil {
		return err
	}
	defer closeFn()

	sql, skipped, err := withoutUnknownSettings(dump, server.knows)
	if err != nil {
		return fmt.Errorf("reading the bundle's database dump: %w", err)
	}
	if len(skipped) > 0 {
		fmt.Printf("  settings    skipped %s, which PostgreSQL %d does not have\n", strings.Join(skipped, ", "), server.major)
	}

	load := pgCommand(ctx, psql, dsn, "-v", "ON_ERROR_STOP=1", "-q", "--single-transaction", "--file=-")
	load.Stdin = io.MultiReader(strings.NewReader(restoreSchemaReset), sql)
	var loadErr strings.Builder
	load.Stderr = &loadErr
	if err := load.Run(); err != nil {
		msg := strings.TrimSpace(loadErr.String())
		if msg == "" {
			msg = err.Error()
		}
		hint := "Fix the cause and run the restore again."
		if strings.Contains(msg, "max_locks_per_transaction") {
			hint = "The restore replaces every table in one transaction, which needs more locks than this server allows.\nRaise max_locks_per_transaction (for example to 1024), restart PostgreSQL, and run the restore again."
		}
		return fmt.Errorf("the dump did not load, so nothing was changed: the restore runs in one transaction and the database is as it was.\n%s\n%s", hint, msg)
	}
	return nil
}

// analyzeDatabase gathers planner statistics, which a dump does not carry.
func analyzeDatabase(ctx context.Context, psql, dsn string) error {
	cmd := pgCommand(ctx, psql, dsn, "-v", "ON_ERROR_STOP=1", "-q", "-c", "ANALYZE;")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

// pgCommand builds a libpq invocation with the password moved out of argv.
//
// A connection string on the command line is readable by anyone on the host who
// can list processes, and this one opens the database that holds every sealed
// mailbox credential. libpq reads PGPASSWORD from the environment, which is not
// world-readable on Linux, so the DSN that reaches argv carries no password.
func pgCommand(ctx context.Context, name, dsn string, args ...string) *exec.Cmd {
	clean, password := splitDSNPassword(dsn)
	cmd := exec.CommandContext(ctx, name, append([]string{"--dbname=" + clean}, args...)...) //nolint:gosec // name is a client binary resolved by pgTool
	cmd.Env = os.Environ()
	if password != "" {
		cmd.Env = append(cmd.Env, "PGPASSWORD="+password)
	}
	return cmd
}

// splitDSNPassword returns the connection string with its password removed and
// the password separately. A DSN this cannot parse (libpq also accepts
// keyword/value form) is returned unchanged, because a connection that fails is
// worse than a password in argv.
func splitDSNPassword(dsn string) (string, string) {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.User == nil {
		return dsn, ""
	}
	password, ok := parsed.User.Password()
	if !ok || password == "" {
		return dsn, ""
	}
	parsed.User = url.User(parsed.User.Username())
	return parsed.String(), password
}

func instanceCounts(ctx context.Context, c *conn) (orgs, users, mailboxes int) {
	_ = c.db.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&orgs)
	_ = c.db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users)
	_ = c.db.QueryRow(ctx, `SELECT count(*) FROM email_accounts`).Scan(&mailboxes)
	return
}

// databaseName is the database a connection string names, for the line that
// tells an operator what is about to be replaced.
func databaseName(dsn string) string {
	if i := strings.LastIndex(dsn, "/"); i >= 0 {
		name := dsn[i+1:]
		if j := strings.IndexAny(name, "?"); j >= 0 {
			name = name[:j]
		}
		if name != "" {
			return name
		}
	}
	return "the database"
}

// ---------- blobs ----------

type blobEntry struct {
	abs  string
	rel  string
	size int64
	mode os.FileMode
}

func blobRoot() string {
	if v := strings.TrimSpace(os.Getenv("BLOB_FS_ROOT")); v != "" {
		return v
	}
	return "/data/blobs"
}

// collectBlobs walks the blob root, skipping the bundle being written and
// warning about any earlier one still sitting there.
func collectBlobs(root, exclude string) ([]blobEntry, error) {
	if abs, aerr := filepath.Abs(exclude); aerr == nil {
		exclude = abs
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			warn("the blob root %s does not exist on this host, so no bodies or attachments are in the bundle.", root)
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	var out []blobEntry
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		// Symlinks are skipped rather than followed: the blob root is written
		// by the app, and a link out of it would put arbitrary host files in a
		// bundle the operator believes holds message bodies.
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if abs, aerr := filepath.Abs(path); aerr == nil && abs == exclude {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		// An earlier bundle left in the blob root is not blob data, and putting
		// it inside this one doubles the archive for nothing.
		if !strings.Contains(rel, string(os.PathSeparator)) &&
			strings.HasPrefix(rel, "warmbly-") && strings.HasSuffix(rel, ".tar.gz") {
			warn("%s looks like an earlier backup and was left out of this one. Delete it: it is sitting in the blob root, where it is backed up over and over.", filepath.Join(root, rel))
			return nil
		}
		st, serr := d.Info()
		if serr != nil {
			// Vanished between the walk and the stat. A live instance writes
			// bodies constantly, and one disappearing is not a failed backup.
			if os.IsNotExist(serr) {
				return nil
			}
			return serr
		}
		out = append(out, blobEntry{abs: path, rel: filepath.ToSlash(rel), size: st.Size(), mode: st.Mode().Perm()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}

// restoreBlobs unpacks the bundle's blob tree under root. Existing files are
// overwritten; files the bundle does not know about are left alone, so a
// restore onto a host that already holds bodies is additive rather than a
// silent deletion.
func restoreBlobs(bundle, root string) (int, int64, error) {
	f, gz, tr, err := openBundle(bundle)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	defer gz.Close()

	var n int
	var total int64
	for {
		hdr, nerr := tr.Next()
		if nerr == io.EOF {
			break
		}
		if nerr != nil {
			return n, total, nerr
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasPrefix(hdr.Name, blobsPrefix) {
			continue
		}
		rel := strings.TrimPrefix(hdr.Name, blobsPrefix)
		dest, derr := safeJoin(root, rel)
		if derr != nil {
			return n, total, derr
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			return n, total, err
		}
		out, oerr := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode).Perm()) //nolint:gosec // path is checked by safeJoin
		if oerr != nil {
			return n, total, oerr
		}
		written, cerr := io.Copy(out, tr) //nolint:gosec // sizes come from a bundle the operator wrote
		_ = out.Close()
		if cerr != nil {
			return n, total, cerr
		}
		n++
		total += written
	}
	return n, total, nil
}

// safeJoin refuses a bundle entry that would land outside the destination.
// A backup is an operator's own file, but it is also the kind of file that
// gets emailed around, and a path traversal in one is a host compromise.
func safeJoin(root, rel string) (string, error) {
	clean := filepath.Clean("/" + filepath.FromSlash(rel))
	joined := filepath.Join(root, clean)
	if !strings.HasPrefix(joined, filepath.Clean(root)+string(os.PathSeparator)) {
		return "", fmt.Errorf("the bundle holds an entry that would be written outside %s: %s", root, rel)
	}
	return joined, nil
}

// ---------- bundle io ----------

func writeBundle(target string, m manifest, dumpFile string, blobs []blobEntry, keys map[string]string) error {
	// 0600 from creation, not chmod after: the file holds every secret the
	// instance has, and a window in which it is world-readable is a window.
	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // operator-supplied output path
	if err != nil {
		return err
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	if err := tarBytes(tw, manifestPath, mustJSON(m), 0o600, m.CreatedAt); err != nil {
		return err
	}
	if len(keys) > 0 {
		if err := tarBytes(tw, keysPath, []byte(renderKeys(keys)), 0o600, m.CreatedAt); err != nil {
			return err
		}
	}
	if err := tarFile(tw, databasePath, dumpFile, 0o600, m.CreatedAt); err != nil {
		return err
	}
	for _, b := range blobs {
		if err := tarFile(tw, blobsPrefix+b.rel, b.abs, b.mode, m.CreatedAt); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Close()
}

func tarBytes(tw *tar.Writer, name string, body []byte, mode os.FileMode, when time.Time) error {
	if err := tw.WriteHeader(&tar.Header{
		Name: name, Mode: int64(mode), Size: int64(len(body)), ModTime: when, Typeflag: tar.TypeReg,
	}); err != nil {
		return err
	}
	_, err := tw.Write(body)
	return err
}

// tarFile writes one file, tolerating the instance writing to it underneath.
//
// A tar entry's size is fixed by its header, so a file that grows between the
// stat and the copy has to be truncated and one that shrinks has to be padded;
// getting either wrong corrupts the whole archive. A file that disappears is
// skipped, because a live instance is writing message bodies the entire time a
// backup runs and one of them vanishing is not a failed backup.
func tarFile(tw *tar.Writer, name, path string, mode os.FileMode, when time.Time) error {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	f, err := os.Open(path) //nolint:gosec // paths come from the instance's own blob root
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	size := st.Size()
	if err := tw.WriteHeader(&tar.Header{
		Name: name, Mode: int64(mode), Size: size, ModTime: when, Typeflag: tar.TypeReg,
	}); err != nil {
		return err
	}
	written, err := io.CopyN(tw, f, size)
	if err != nil && err != io.EOF {
		return err
	}
	if written < size {
		// It shrank. The header is already committed, so the entry is padded
		// to the length it promised.
		if _, perr := io.CopyN(tw, zeroReader{}, size-written); perr != nil {
			return perr
		}
	}
	return nil
}

// zeroReader pads a tar entry whose file shrank while it was being read.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func openBundle(path string) (*os.File, *gzip.Reader, *tar.Reader, error) {
	f, err := os.Open(path) //nolint:gosec // operator-supplied bundle path
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not read %s: %w", path, err)
	}
	gz, gerr := gzip.NewReader(bufio.NewReaderSize(f, 1<<20))
	if gerr != nil {
		_ = f.Close()
		return nil, nil, nil, fmt.Errorf("%s is not a Warmbly bundle (it is not gzip): %w", path, gerr)
	}
	return f, gz, tar.NewReader(gz), nil
}

// openBundleEntry streams one entry out of the bundle. The caller closes.
func openBundleEntry(path, want string) (io.Reader, func(), error) {
	f, gz, tr, err := openBundle(path)
	if err != nil {
		return nil, func() {}, err
	}
	closeFn := func() {
		_ = gz.Close()
		_ = f.Close()
	}
	for {
		hdr, nerr := tr.Next()
		if nerr == io.EOF {
			closeFn()
			return nil, func() {}, fmt.Errorf("%s holds no %s; it is not a Warmbly bundle", path, want)
		}
		if nerr != nil {
			closeFn()
			return nil, func() {}, nerr
		}
		if hdr.Name == want {
			return tr, closeFn, nil
		}
	}
}

func readBundleEntry(path, want string, limit int64) ([]byte, error) {
	r, closeFn, err := openBundleEntry(path, want)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	return io.ReadAll(io.LimitReader(r, limit))
}

func readManifest(path string) (manifest, error) {
	var m manifest
	raw, err := readBundleEntry(path, manifestPath, 1<<20)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("%s holds an unreadable manifest: %w", path, err)
	}
	return m, nil
}

func readKeys(path string) (map[string]string, error) {
	raw, err := readBundleEntry(path, keysPath, 1<<20)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, nil
}

func renderKeys(keys map[string]string) string {
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	b := strings.Builder{}
	b.WriteString("# Warmbly instance keys. These belong in the destination's .env BEFORE the\n")
	b.WriteString("# restore runs; warmblyctl restore refuses to run without them and says so.\n")
	for _, k := range names {
		b.WriteString(k + "=" + keys[k] + "\n")
	}
	return b.String()
}

// ---------- small helpers ----------

func missingUnrecoverable(keys map[string]string) []string {
	var out []string
	for _, k := range unrecoverableKeys {
		if keys[k] == "" {
			out = append(out, k)
		}
	}
	return out
}

func mustJSON(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return []byte("{}")
	}
	return append(b, '\n')
}

// confirmPhrase reads one line and reports whether it is the phrase. It needs
// a TTY; piped into a container without one, --yes is the documented path.
func confirmPhrase(prompt, phrase string) (bool, error) {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false, errors.New("could not read the confirmation. Pass --yes to skip it, or run this without `exec -T`.")
	}
	return strings.TrimSpace(line) == phrase, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
