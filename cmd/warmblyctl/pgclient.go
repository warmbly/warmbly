package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// pgServer is what backup and restore need to know about the database they talk to.
type pgServer struct {
	major   int
	version string
	// settings is every configuration parameter name the server recognises.
	settings map[string]bool
}

func readPGServer(ctx context.Context, dsn string) (pgServer, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return pgServer{}, fmt.Errorf("could not reach postgres at %s: %w", redact(dsn), err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	var s pgServer
	var num int
	if err := conn.QueryRow(ctx, `SELECT current_setting('server_version_num')::int, current_setting('server_version')`).Scan(&num, &s.version); err != nil {
		return pgServer{}, fmt.Errorf("could not read the server's version: %w", err)
	}
	s.major = num / 10000
	rows, err := conn.Query(ctx, `SELECT name FROM pg_settings`)
	if err != nil {
		return pgServer{}, fmt.Errorf("could not read the server's settings: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return pgServer{}, fmt.Errorf("could not read the server's settings: %w", err)
	}
	s.settings = make(map[string]bool, len(names))
	for _, n := range names {
		s.settings[strings.ToLower(n)] = true
	}
	return s, nil
}

// knows reports whether a SET of this parameter succeeds on the server. Dotted
// names are extension placeholders, which any server accepts.
func (s pgServer) knows(name string) bool {
	name = strings.ToLower(name)
	return s.settings[name] || strings.Contains(name, ".")
}

// pgClientLayouts are where packaged client majors install: Alpine, then Debian and Ubuntu.
var pgClientLayouts = []string{"/usr/libexec/postgresql%s", "/usr/lib/postgresql/%s/bin"}

var pgVersionRe = regexp.MustCompile(`\(PostgreSQL\) (\d+)`)

// installedPGClients maps each client major found on this host to the path of name.
func installedPGClients(name string) map[int]string {
	found := map[int]string{}
	for _, layout := range pgClientLayouts {
		dirs, _ := filepath.Glob(fmt.Sprintf(layout, "[0-9]*"))
		for _, dir := range dirs {
			if major := pgVersionMajor(filepath.Join(dir, name)); major > 0 {
				if _, ok := found[major]; !ok {
					found[major] = filepath.Join(dir, name)
				}
			}
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		if major := pgVersionMajor(path); major > 0 {
			if _, ok := found[major]; !ok {
				found[major] = path
			}
		}
	}
	return found
}

func pgVersionMajor(path string) int {
	out, err := exec.Command(path, "--version").Output() //nolint:gosec // path is a client binary found by layout or PATH
	if err != nil {
		return 0
	}
	m := pgVersionRe.FindSubmatch(out)
	if m == nil {
		return 0
	}
	major, _ := strconv.Atoi(string(m[1]))
	return major
}

// pickPGMajor chooses the client major for a server: its own, else the
// nearest newer one. An older pg_dump refuses a newer server, so none older.
func pickPGMajor(server int, installed []int) (int, bool) {
	sort.Ints(installed)
	for _, m := range installed {
		if m >= server {
			return m, true
		}
	}
	return 0, false
}

// pgTool resolves name (pg_dump or psql) for the server it will talk to.
func pgTool(name string, server pgServer) (string, error) {
	clients := installedPGClients(name)
	majors := make([]int, 0, len(clients))
	for m := range clients {
		majors = append(majors, m)
	}
	major, ok := pickPGMajor(server.major, majors)
	if !ok {
		have := "none is installed"
		if len(majors) > 0 {
			sort.Ints(majors)
			parts := make([]string, len(majors))
			for i, m := range majors {
				parts[i] = strconv.Itoa(m)
			}
			have = "this host has " + strings.Join(parts, ", ")
		}
		return "", fmt.Errorf("the database is PostgreSQL %d and %s needs a client of that version or newer, but %s.\nInstall the PostgreSQL %d client (postgresql%d-client on Alpine, postgresql-client-%d on Debian and Ubuntu), or run this inside the backend container, which ships 16, 17 and 18.",
			server.major, name, have, server.major, server.major, server.major)
	}
	if major != server.major {
		warn("no PostgreSQL %d %s is installed, so the %d one is used.", server.major, name, major)
	}
	return clients[major], nil
}

// withoutUnknownSettings comments out SET lines in a plain dump's preamble that
// name a parameter the target does not have. A newer pg_dump writes settings an
// older server rejects (17 added transaction_timeout); they only reset defaults.
// Only the preamble is read: it ends at the first statement that is not a SET,
// a set_config call, a psql meta-command or a comment, and the rest is passed
// through untouched.
func withoutUnknownSettings(dump io.Reader, knows func(string) bool) (io.Reader, []string, error) {
	br := bufio.NewReaderSize(dump, 64<<10)
	var head strings.Builder
	var skipped []string
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			name, isSet := preambleSetting(line)
			switch {
			case isSet && !knows(name):
				skipped = append(skipped, name)
				head.WriteString("-- skipped, not a setting on this server: " + strings.TrimRight(line, "\r\n") + "\n")
			case isSet || isPreambleLine(line):
				head.WriteString(line)
			default:
				head.WriteString(line)
				return io.MultiReader(strings.NewReader(head.String()), br), skipped, nil
			}
		}
		if err == io.EOF {
			return strings.NewReader(head.String()), skipped, nil
		}
		if err != nil {
			return nil, nil, err
		}
	}
}

// preambleSetting parses `SET name = value;` and `SET name TO value;`.
func preambleSetting(line string) (string, bool) {
	f := strings.Fields(line)
	if len(f) < 2 || !strings.EqualFold(f[0], "SET") {
		return "", false
	}
	name := f[1]
	if (strings.EqualFold(name, "SESSION") || strings.EqualFold(name, "LOCAL")) && len(f) > 2 {
		name = f[2]
	}
	name, _, _ = strings.Cut(name, "=")
	name = strings.Trim(name, `"`)
	return name, name != ""
}

func isPreambleLine(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "--") || strings.HasPrefix(t, `\`) ||
		strings.HasPrefix(t, "SELECT pg_catalog.set_config(")
}

// pgClientVersion is the client's own version line, for the manifest.
func pgClientVersion(path string) string {
	out, err := exec.Command(path, "--version").Output() //nolint:gosec // path is resolved by pgTool
	if err != nil {
		return filepath.Base(path)
	}
	return strings.Replace(strings.TrimSpace(string(out)), " (PostgreSQL)", "", 1)
}
