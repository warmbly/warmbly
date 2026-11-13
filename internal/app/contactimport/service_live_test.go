package contactimport

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Run against a migrated database:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/app/contactimport/ -run Live -v

type fixture struct {
	org  uuid.UUID
	user uuid.UUID
	svc  *Service
	pool *pgxpool.Pool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	handle, err := db.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })
	f := &fixture{org: uuid.New(), user: uuid.New(), pool: handle.Pool}
	ctx := context.Background()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Import', 'Job', $2, 'x')`,
			[]any{f.user, "job-" + f.user.String()[:8] + "@test.local"}},
		{`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Import job', $2, $3)`,
			[]any{f.org, "job-" + f.org.String()[:8], f.user}},
		{`INSERT INTO organization_members (organization_id, user_id, role, accepted_at) VALUES ($1, $2, 'owner', NOW())`,
			[]any{f.org, f.user}},
	} {
		if _, err := f.pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	t.Cleanup(func() {
		c := context.Background()
		for _, q := range []string{
			`DELETE FROM contact_imports WHERE organization_id = $1`,
			`DELETE FROM contact_import_mappings WHERE organization_id = $1`,
			`DELETE FROM contacts WHERE organization_id = $1`,
			`DELETE FROM organization_members WHERE organization_id = $1`,
			`DELETE FROM organizations WHERE id = $1`,
		} {
			if _, err := f.pool.Exec(c, q, f.org); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
		_, _ = f.pool.Exec(c, `DELETE FROM users WHERE id = $1`, f.user)
	})
	f.svc = NewService(Deps{
		Repo:     repository.NewContactImportRepository(handle),
		Contacts: contact.NewService(repository.NewContactRepostory(handle), nil),
	})
	return f
}

// runAll works the queue until this fixture's import settles.
func (f *fixture) runAll(t *testing.T, id uuid.UUID) *models.ContactImport {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		f.svc.pass(context.Background())
		imp, xerr := f.svc.Get(context.Background(), f.org, id)
		if xerr != nil {
			t.Fatalf("get: %s", xerr.Message)
		}
		if imp.Status.Terminal() {
			return imp
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("import %s did not finish", id)
	return nil
}

func leadsCSV(n int) string {
	var b strings.Builder
	b.WriteString("Email,First Name,Company\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "lead%05d@job.test,Ada%d,Acme %d\n", i, i, i)
	}
	// A repeat of the first address and a row with no usable address.
	b.WriteString("LEAD00000@job.test,Again,Acme\nnot-an-address,Bad,Row\n")
	return b.String()
}

func mapping() []models.ContactImportColumnMapping {
	return []models.ContactImportColumnMapping{
		{Index: 0, Target: models.ContactImportTargetEmail},
		{Index: 1, Target: models.ContactImportTargetFirstName},
		{Index: 2, Target: models.ContactImportTargetCompany},
	}
}

func TestLiveContactImportRunsInTheBackground(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	draft, xerr := f.svc.Create(ctx, f.org, f.user, strings.NewReader(leadsCSV(1200)), "leads.csv")
	if xerr != nil {
		t.Fatalf("create: %s", xerr.Message)
	}
	if draft.Status != models.ContactImportDraft || draft.Total != 1202 || draft.Preview == nil || !draft.Preview.HasHeader {
		t.Fatalf("draft=%+v", draft)
	}
	if got := draft.Preview.ColumnStats[0].Filled; got != 1202 {
		t.Fatalf("email column filled=%d (want 1202)", got)
	}

	// A reload finds the draft as it was left: its preview, and the choices autosaved so far.
	if _, xerr := f.svc.SaveDraft(ctx, f.org, draft.ID, &models.ContactImportCommit{Mapping: mapping()[:1], Dedup: models.ContactImportDedupUpdate, HasHeader: true}); xerr != nil {
		t.Fatalf("save draft: %s", xerr.Message)
	}
	resumed, xerr := f.svc.Get(ctx, f.org, draft.ID)
	if xerr != nil {
		t.Fatalf("get draft: %s", xerr.Message)
	}
	if resumed.Preview == nil || len(resumed.Preview.Columns) != 3 || resumed.Options == nil ||
		len(resumed.Options.Mapping) != 1 || resumed.Options.Dedup != models.ContactImportDedupUpdate {
		t.Fatalf("resumed draft lost its state: preview=%v options=%+v", resumed.Preview != nil, resumed.Options)
	}

	analysis, xerr := f.svc.Analyze(ctx, f.org, f.user, draft.ID, &models.ContactImportAnalyzeRequest{Mapping: mapping(), HasHeader: true})
	if xerr != nil {
		t.Fatalf("analyze: %s", xerr.Message)
	}
	if analysis.New != 1200 || analysis.DuplicatesInFile != 1 || analysis.Invalid != 1 || analysis.Existing != 0 {
		t.Fatalf("analysis=%+v", analysis)
	}

	started, ok, xerr := f.svc.Start(ctx, f.org, f.user, draft.ID, &models.ContactImportCommit{Mapping: mapping(), HasHeader: true})
	if xerr != nil || !ok || started.Status != models.ContactImportQueued {
		t.Fatalf("start: ok=%v err=%v imp=%+v", ok, xerr, started)
	}
	// A retried start is a no-op, not a second import.
	if _, again, xerr := f.svc.Start(ctx, f.org, f.user, draft.ID, &models.ContactImportCommit{Mapping: mapping(), HasHeader: true}); xerr != nil || again {
		t.Fatalf("second start: started=%v err=%v", again, xerr)
	}

	if _, xerr := f.svc.SaveDraft(ctx, f.org, draft.ID, &models.ContactImportCommit{Mapping: mapping()}); xerr == nil {
		t.Fatalf("a started import accepted a draft save")
	}

	done := f.runAll(t, draft.ID)
	if done.Preview != nil {
		t.Fatalf("a started import still carries its draft preview")
	}
	if done.Status != models.ContactImportCompleted {
		t.Fatalf("status=%s error=%q", done.Status, done.Error)
	}
	if done.Imported != 1200 || done.Skipped != 1 || done.Failed != 1 || done.Processed != 1202 || done.Total != 1202 {
		t.Fatalf("counts: %+v", done)
	}
	if len(done.Failures) != 1 || done.Failures[0].Line != 1203 || done.Failures[0].Values[0] != "not-an-address" {
		t.Fatalf("failures=%+v", done.Failures)
	}

	var n int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM contacts WHERE organization_id = $1 AND source = 'import' AND source_detail = 'leads.csv'`, f.org).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1200 {
		t.Fatalf("contacts=%d (want 1200)", n)
	}

	csv, name, xerr := f.svc.FailedCSV(ctx, f.org, draft.ID)
	if xerr != nil {
		t.Fatalf("failed csv: %s", xerr.Message)
	}
	body := strings.TrimPrefix(string(csv), "\ufeff")
	if name != "leads-failed.csv" || !strings.HasPrefix(body, "Email,First Name,Company,Line,Error\n") || !strings.Contains(body, "not-an-address,Bad,Row,1203,") {
		t.Fatalf("failed csv %q:\n%s", name, body)
	}

	// The next file with the same headers maps itself.
	again, xerr := f.svc.Create(ctx, f.org, f.user, strings.NewReader("email,first name,company\nx@job.test,X,Y\n"), "again.csv")
	if xerr != nil {
		t.Fatalf("second create: %s", xerr.Message)
	}
	if again.Preview.MappingSource != models.ContactImportMappingSaved {
		t.Fatalf("mapping source=%q (want saved)", again.Preview.MappingSource)
	}
	cancelled, xerr := f.svc.Cancel(ctx, f.org, again.ID)
	if xerr != nil || cancelled.Status != models.ContactImportCancelled {
		t.Fatalf("cancel: %v %+v", xerr, cancelled)
	}

	list, xerr := f.svc.List(ctx, f.org, "", 1)
	if xerr != nil || len(list.Data) != 1 || !list.Pagination.HasMore || list.Data[0].ID != again.ID {
		t.Fatalf("list page one: %v %+v", xerr, list)
	}
	next, xerr := f.svc.List(ctx, f.org, *list.Pagination.NextCursor, 1)
	if xerr != nil || len(next.Data) != 1 || next.Data[0].ID != draft.ID || next.Pagination.HasMore {
		t.Fatalf("list page two: %v %+v", xerr, next)
	}
	if _, xerr := f.svc.List(ctx, f.org, "garbage", 1); xerr == nil {
		t.Fatalf("an invalid cursor was accepted")
	}
}

// Another workspace cannot read, start or cancel an import.
func TestLiveContactImportIsScopedToItsWorkspace(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	draft, xerr := f.svc.Create(ctx, f.org, f.user, strings.NewReader(leadsCSV(3)), "leads.csv")
	if xerr != nil {
		t.Fatalf("create: %s", xerr.Message)
	}
	stranger := uuid.New()
	if _, xerr := f.svc.Get(ctx, stranger, draft.ID); xerr == nil {
		t.Fatalf("another workspace read the import")
	}
	if _, _, xerr := f.svc.Start(ctx, stranger, f.user, draft.ID, &models.ContactImportCommit{Mapping: mapping(), HasHeader: true}); xerr == nil {
		t.Fatalf("another workspace started the import")
	}
	if _, xerr := f.svc.Cancel(ctx, stranger, draft.ID); xerr == nil {
		t.Fatalf("another workspace cancelled the import")
	}
	if _, _, xerr := f.svc.FailedCSV(ctx, stranger, draft.ID); xerr == nil {
		t.Fatalf("another workspace downloaded the import's rows")
	}
}
