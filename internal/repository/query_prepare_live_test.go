package repository

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Every parameterised statement in this package must be preparable by Postgres.
// Skipped unless WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run TestLiveEveryQueryPrepares -v
//
// Issue #195: UpdateParticipantHealth used a bare $1 both as a health_state
// assignment and in equality tests. Postgres deduced `character varying` from
// one and `text` from the other, refused the statement with 42P08, and the
// query never ran — for any account, in any pool, since it was written. Nothing
// caught it because the error surfaced as a generic internal error at runtime
// and only on a code path nobody watched.
//
// A statement that cannot be prepared can never execute, so this is decidable
// ahead of time: ask the server. The same sweep found the identical defect in
// the contacts bulk custom-field writes and in the worker install-state update.
//
// Two classes fail the test. A parameter Postgres cannot type (42P08) is the
// original #195 defect. A reference to schema that does not exist (issue #209 —
// a renamed column, a table that was dropped, an enum value the type never had,
// a comparison between types with no operator) is the same kind of bug found
// the same way: six admin queries referenced a `plans.duration` column, a
// `user_rate_limits.daily_email_limit` column and a `campaign_status` value
// called 'stopped', none of which exist, and each failed 100% of the time.
//
// Any other prepare error fails too: the extractor folds only complete constant
// expressions, so a statement assembled at runtime is skipped, never half-read.
const (
	indeterminateDatatype = "42P08" // a $n Postgres cannot assign a type to
	undefinedColumn       = "42703"
	undefinedTable        = "42P01"
	undefinedFunction     = "42883" // includes "operator does not exist"
	undefinedObject       = "42704"
	invalidTextRepr       = "22P02" // e.g. a literal that is not a value of an enum
)

// unrunnable is the set of SQLSTATEs that mean the statement can never execute,
// whatever the calling code looks like.
var unrunnable = map[string]string{
	indeterminateDatatype: "a parameter Postgres cannot type",
	undefinedColumn:       "a column that does not exist",
	undefinedTable:        "a table that does not exist",
	undefinedFunction:     "a function or operator that does not exist",
	undefinedObject:       "an object that does not exist",
	invalidTextRepr:       "a literal that is not a valid value of its type",
}

var (
	// Something that starts like a complete statement.
	statementStart = regexp.MustCompile(`(?is)^\s*(SELECT|INSERT|UPDATE|DELETE|WITH)\b`)
	// Placeholders. No placeholders means no parameter to be ambiguous about.
	hasParam = regexp.MustCompile(`\$\d`)
	// Assembled with fmt: not a statement, just a template.
	isTemplate = regexp.MustCompile(`%(\[\d+\])?[sdvqt]`)
	// Session setup the package runs before statements that read pg_temp tables.
	tempTable = regexp.MustCompile(`(?is)^\s*CREATE\s+TEMP(ORARY)?\s+TABLE\b`)
)

type extractedQuery struct {
	file string
	line int
	sql  string
}

// harvest is every complete statement in the package's source and the temp tables it creates.
type harvest struct {
	queries     []extractedQuery
	tempTables  []string
	unassembled int
}

// extractQueries type-checks the package and takes every maximal string constant expression as a statement.
func extractQueries(t *testing.T, dir string) harvest {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}

	// Imports are not resolved; constant folding of the package's own strings does not need them.
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: noImporter{}, Error: func(error) {}}
	_, _ = conf.Check("repository", fset, files, info)

	stringConst := func(e ast.Expr) (string, bool) {
		tv, ok := info.Types[e]
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
			return "", false
		}
		return constant.StringVal(tv.Value), true
	}

	// A constant spliced into a + chain is a fragment; the chain is checked where it is assembled.
	fragments := map[types.Object]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if isConcat(n) {
				for _, op := range concatOperands(n.(ast.Expr)) {
					if id, ok := op.(*ast.Ident); ok && info.Uses[id] != nil {
						fragments[info.Uses[id]] = true
					}
				}
			}
			return true
		})
	}

	var h harvest
	for _, f := range files {
		skip := map[ast.Node]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil || skip[n] {
				return false
			}
			switch v := n.(type) {
			case *ast.ValueSpec:
				for i, name := range v.Names {
					if i < len(v.Values) && fragments[info.Defs[name]] {
						skip[v.Values[i]] = true
					}
				}
				return true
			case *ast.CompositeLit:
				// monitoringStatement assembles these; the test prepares the assembled form.
				if at, ok := v.Type.(*ast.ArrayType); ok && isIdent(at.Elt, "monitoringQuery") {
					for _, el := range v.Elts {
						if q, ok := el.(*ast.CompositeLit); ok {
							skipMonitoringQuery(q, skip)
						}
					}
				}
				if isIdent(v.Type, "monitoringQuery") {
					skipMonitoringQuery(v, skip)
				}
				return true
			}
			expr, ok := n.(ast.Expr)
			if !ok {
				return true
			}
			sql, ok := stringConst(expr)
			if !ok {
				if isConcat(n) {
					for _, op := range concatOperands(expr) {
						if _, isString := stringConst(op); isString {
							// A chain with a runtime operand cannot be assembled from source.
							h.unassembled++
							return false
						}
					}
				}
				return true
			}
			pos := fset.Position(expr.Pos())
			switch {
			case tempTable.MatchString(sql):
				h.tempTables = append(h.tempTables, sql)
			case hasParam.MatchString(sql) && statementStart.MatchString(sql) && !isTemplate.MatchString(sql):
				h.queries = append(h.queries, extractedQuery{file: filepath.Base(pos.Filename), line: pos.Line, sql: sql})
			}
			return false
		})
	}
	sort.Slice(h.queries, func(i, j int) bool {
		if h.queries[i].file != h.queries[j].file {
			return h.queries[i].file < h.queries[j].file
		}
		return h.queries[i].line < h.queries[j].line
	})
	return h
}

type noImporter struct{}

func (noImporter) Import(path string) (*types.Package, error) {
	return nil, fmt.Errorf("not resolved: %s", path)
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

func skipMonitoringQuery(cl *ast.CompositeLit, skip map[ast.Node]bool) {
	for _, el := range cl.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok && isIdent(kv.Key, "query") {
			skip[kv.Value] = true
		}
	}
}

func isConcat(n ast.Node) bool {
	b, ok := n.(*ast.BinaryExpr)
	return ok && b.Op == token.ADD
}

func concatOperands(e ast.Expr) []ast.Expr {
	if p, ok := e.(*ast.ParenExpr); ok {
		return concatOperands(p.X)
	}
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.ADD {
		return append(concatOperands(b.X), concatOperands(b.Y)...)
	}
	return []ast.Expr{e}
}

func TestLiveEveryQueryPrepares(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	h := extractQueries(t, ".")
	queries := h.queries
	if len(queries) < 100 {
		t.Fatalf("only found %d statements to check; the extractor is broken", len(queries))
	}
	for _, id := range monitoringSectionOrder {
		queries = append(queries, extractedQuery{file: "monitoringStatement(" + id + ")", sql: monitoringStatement(monitoringSections[id])})
	}

	// Temp tables are ON COMMIT DROP, so the sweep runs in one transaction with a savepoint per statement.
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, ddl := range h.tempTables {
		if _, err := tx.Exec(ctx, ddl); err != nil {
			t.Fatalf("create temp table: %v\n\t%s", err, oneLine(ddl))
		}
	}

	var broken, other []string
	for i, q := range queries {
		if _, err := tx.Exec(ctx, "SAVEPOINT prepare_check"); err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		_, err := tx.Prepare(ctx, fmt.Sprintf("check_%d", i), strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(q.sql), ";")))
		if _, rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT prepare_check"); rbErr != nil {
			t.Fatalf("rollback to savepoint: %v", rbErr)
		}
		if err == nil {
			continue
		}

		where := fmt.Sprintf("%s:%d", q.file, q.line)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if why, fatal := unrunnable[pgErr.Code]; fatal {
				broken = append(broken, fmt.Sprintf("%s: %s -- %s (%s)\n\t\t%s",
					where, why, pgErr.Message, pgErr.Code, oneLine(q.sql)))
				continue
			}
		}
		other = append(other, fmt.Sprintf("%s: %v", where, err))
	}

	if len(other) > 0 {
		t.Errorf("%d statement(s) could not be prepared:\n%s", len(other), strings.Join(other, "\n"))
	}

	if len(broken) > 0 {
		t.Fatalf("%d statement(s) can never execute:\n%s",
			len(broken), strings.Join(broken, "\n"))
	}

	t.Logf("checked %d parameterised statements; %d runtime-assembled chains not checked", len(queries), h.unassembled)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
