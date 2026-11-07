package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func runDB(ctx context.Context, args []string) error {
	if len(args) == 0 {
		dbUsage(os.Stderr)
		return errors.New("`db` needs a subcommand. Pick one from the list above.")
	}
	switch args[0] {
	case "help", "-h", "--help":
		dbUsage(os.Stdout)
		return nil
	case "load":
		return runDBLoad(ctx, args[1:])
	}
	dbUsage(os.Stderr)
	return fmt.Errorf("unknown db subcommand %q", args[0])
}

func dbUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:\n  warmblyctl db load [--limit 15] [--json]")
}

// dbLoad is what the database is busy with. Read-only: it reads the statistics views and changes nothing.
type dbLoad struct {
	MaxConnections int               `json:"max_connections"`
	Connections    []dbConnections   `json:"connections"`
	Busy           []dbBusy          `json:"busy"`
	Statements     []dbStatement     `json:"statements"`
	StatementsNote string            `json:"statements_note,omitempty"`
	Tables         []dbTable         `json:"tables"`
	Since          *time.Time        `json:"statements_since,omitempty"`
	Settings       map[string]string `json:"settings"`
}

type dbConnections struct {
	Application string `json:"application"`
	State       string `json:"state"`
	Count       int    `json:"count"`
}

// dbBusy carries no query text: pg_stat_activity holds literal values, pg_stat_statements does not.
type dbBusy struct {
	PID         int     `json:"pid"`
	Application string  `json:"application"`
	State       string  `json:"state"`
	Seconds     float64 `json:"seconds"`
	Wait        string  `json:"wait"`
}

type dbStatement struct {
	QueryID int64   `json:"queryid"`
	Calls   int64   `json:"calls"`
	TotalMS float64 `json:"total_ms"`
	MeanMS  float64 `json:"mean_ms"`
	Rows    int64   `json:"rows"`
	Query   string  `json:"query"`
}

type dbTable struct {
	Name       string     `json:"name"`
	Bytes      int64      `json:"bytes"`
	Live       int64      `json:"live_rows"`
	Dead       int64      `json:"dead_rows"`
	SeqScans   int64      `json:"seq_scans"`
	LastVacuum *time.Time `json:"last_autovacuum,omitempty"`
}

func runDBLoad(ctx context.Context, args []string) error {
	fs := newFlagSet("db load")
	limit := fs.Int("limit", 15, "rows to show per section")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}
	if *limit < 1 || *limit > 200 {
		return errors.New("--limit must be between 1 and 200")
	}
	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	load, err := readDBLoad(ctx, c.db.Pool, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(load)
	}
	printDBLoad(os.Stdout, load)
	return nil
}

func readDBLoad(ctx context.Context, pool *pgxpool.Pool, limit int) (*dbLoad, error) {
	load := &dbLoad{Settings: map[string]string{}, Connections: []dbConnections{}, Busy: []dbBusy{}, Statements: []dbStatement{}, Tables: []dbTable{}}
	if err := pool.QueryRow(ctx, `SELECT current_setting('max_connections')::int`).Scan(&load.MaxConnections); err != nil {
		return nil, fmt.Errorf("read max_connections: %w", err)
	}
	for _, name := range []string{"shared_buffers", "work_mem", "statement_timeout", "idle_in_transaction_session_timeout", "autovacuum_vacuum_scale_factor"} {
		var v string
		if err := pool.QueryRow(ctx, `SELECT current_setting($1, true)`, name).Scan(&v); err == nil {
			load.Settings[name] = v
		}
	}

	// A client session another role owns shows no state to a role without pg_read_all_stats.
	rows, err := pool.Query(ctx, `SELECT COALESCE(NULLIF(application_name, ''), '(unnamed)'),
			CASE WHEN backend_type IS DISTINCT FROM 'client backend' THEN '(background)' ELSE COALESCE(state, '(hidden)') END, count(*)::int
		FROM pg_stat_activity WHERE pid <> pg_backend_pid()
		GROUP BY 1, 2 ORDER BY 3 DESC, 1, 2`)
	if err != nil {
		return nil, fmt.Errorf("read connections: %w", err)
	}
	for rows.Next() {
		var r dbConnections
		if err := rows.Scan(&r.Application, &r.State, &r.Count); err != nil {
			return nil, err
		}
		load.Connections = append(load.Connections, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = pool.Query(ctx, `SELECT pid, COALESCE(NULLIF(application_name, ''), '(unnamed)'), state,
			EXTRACT(EPOCH FROM now() - COALESCE(xact_start, query_start))::float8,
			COALESCE(wait_event_type || ':' || wait_event, '')
		FROM pg_stat_activity
		WHERE datname = current_database() AND pid <> pg_backend_pid() AND state IS NOT NULL AND state <> 'idle'
		ORDER BY COALESCE(xact_start, query_start) LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("read active sessions: %w", err)
	}
	for rows.Next() {
		var r dbBusy
		if err := rows.Scan(&r.PID, &r.Application, &r.State, &r.Seconds, &r.Wait); err != nil {
			return nil, err
		}
		load.Busy = append(load.Busy, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := readStatements(ctx, pool, limit, load); err != nil {
		return nil, err
	}

	rows, err = pool.Query(ctx, `SELECT relname, pg_total_relation_size(relid), n_live_tup, n_dead_tup, seq_scan, last_autovacuum
		FROM pg_stat_user_tables ORDER BY pg_total_relation_size(relid) DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("read tables: %w", err)
	}
	for rows.Next() {
		var r dbTable
		if err := rows.Scan(&r.Name, &r.Bytes, &r.Live, &r.Dead, &r.SeqScans, &r.LastVacuum); err != nil {
			return nil, err
		}
		load.Tables = append(load.Tables, r)
	}
	return load, rows.Err()
}

// readStatements reads pg_stat_statements when the server has it. Only a query's text has its constants replaced, so other statements show their queryid alone.
func readStatements(ctx context.Context, pool *pgxpool.Pool, limit int, load *dbLoad) error {
	var schema string
	err := pool.QueryRow(ctx, `SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'pg_stat_statements'`).Scan(&schema)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check pg_stat_statements: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		load.StatementsNote = "pg_stat_statements is not installed; run CREATE EXTENSION pg_stat_statements as a superuser to see the costliest queries"
		return nil
	}
	rows, err := pool.Query(ctx, `SELECT COALESCE(queryid, 0), calls, total_exec_time, mean_exec_time, rows,
			CASE WHEN query ~* '^\s*(select|insert|update|delete|with|merge|values|table)\M'
			     THEN left(regexp_replace(query, '\s+', ' ', 'g'), 200) ELSE '(not a query; text withheld)' END
		FROM `+pgx.Identifier{schema, "pg_stat_statements"}.Sanitize()+` WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
		ORDER BY total_exec_time DESC LIMIT $1`, limit)
	if err != nil {
		load.StatementsNote = "pg_stat_statements could not be read by this role: " + err.Error()
		return nil
	}
	for rows.Next() {
		var r dbStatement
		if err := rows.Scan(&r.QueryID, &r.Calls, &r.TotalMS, &r.MeanMS, &r.Rows, &r.Query); err != nil {
			return err
		}
		load.Statements = append(load.Statements, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var since time.Time
	if err := pool.QueryRow(ctx, `SELECT stats_reset FROM `+pgx.Identifier{schema, "pg_stat_statements_info"}.Sanitize()).Scan(&since); err == nil {
		load.Since = &since
	}
	return nil
}

func printDBLoad(w io.Writer, load *dbLoad) {
	used := 0
	for _, c := range load.Connections {
		// Background processes have their own slots, outside max_connections.
		if c.State != "(background)" {
			used += c.Count
		}
	}
	fmt.Fprintf(w, "Client connections to the server, not counting this one: %d of max_connections %d\n", used, load.MaxConnections)
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "  APPLICATION\tSTATE\tCOUNT")
	for _, c := range load.Connections {
		fmt.Fprintf(tw, "  %s\t%s\t%d\n", c.Application, c.State, c.Count)
	}
	tw.Flush()

	fmt.Fprintln(w, "\nLongest-running sessions:")
	if len(load.Busy) == 0 {
		fmt.Fprintln(w, "  none")
	} else {
		tw = tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "  PID\tAPPLICATION\tSTATE\tSECONDS\tWAITING ON")
		for _, b := range load.Busy {
			fmt.Fprintf(tw, "  %d\t%s\t%s\t%.1f\t%s\n", b.PID, b.Application, b.State, b.Seconds, b.Wait)
		}
		tw.Flush()
	}

	fmt.Fprint(w, "\nCostliest statements")
	if load.Since != nil {
		fmt.Fprintf(w, " since %s", load.Since.Format(time.RFC3339))
	}
	fmt.Fprintln(w, ":")
	if load.StatementsNote != "" {
		fmt.Fprintf(w, "  %s\n", load.StatementsNote)
	}
	tw = tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	if len(load.Statements) > 0 {
		fmt.Fprintln(tw, "  TOTAL MS\tCALLS\tMEAN MS\tROWS\tQUERYID\tQUERY")
	}
	for _, s := range load.Statements {
		fmt.Fprintf(tw, "  %.0f\t%d\t%.2f\t%d\t%d\t%s\n", s.TotalMS, s.Calls, s.MeanMS, s.Rows, s.QueryID, strings.TrimSpace(s.Query))
	}
	tw.Flush()

	fmt.Fprintln(w, "\nLargest tables:")
	tw = tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "  TABLE\tSIZE\tLIVE ROWS\tDEAD ROWS\tSEQ SCANS\tLAST AUTOVACUUM")
	for _, t := range load.Tables {
		vacuum := "never"
		if t.LastVacuum != nil {
			vacuum = t.LastVacuum.Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "  %s\t%s\t%d\t%d\t%d\t%s\n", t.Name, humanBytes(t.Bytes), t.Live, t.Dead, t.SeqScans, vacuum)
	}
	tw.Flush()
}
