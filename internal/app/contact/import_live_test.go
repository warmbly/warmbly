package contact

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Issue #207: a 1,000-row CSV with 13 columns failed every single row with
// "invalid custom field key: Company Mobile". Custom-field keys were held to
// ^[A-Za-z0-9_]+$ even though the campaign template engine has always been
// able to resolve spaced keys, and a mapping mistake was re-reported once per
// row instead of once per import.
//
// Run against the dev stack:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/contact/ -run LiveImport -v

type importFixture struct {
	org      uuid.UUID
	user     uuid.UUID
	campaign uuid.UUID
	svc      ContactService
	repo     repository.ContactRepository
	pool     *pgxpool.Pool
}

func newImportFixture(t *testing.T) *importFixture {
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

	f := &importFixture{
		org:      uuid.New(),
		user:     uuid.New(),
		campaign: uuid.New(),
		pool:     handle.Pool,
	}
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(70, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash)
	      VALUES ($1, 'Import', 'Live', $2, 'x')`, f.user, "i207-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id)
	      VALUES ($1, 'Issue 207', $2, $3)`, f.org, "i207-"+f.org.String()[:8], f.user)
	exec(`INSERT INTO organization_members (organization_id, user_id, role, accepted_at)
	      VALUES ($1, $2, 'owner', NOW())`, f.org, f.user)
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Iqonic Agency Mix', '', 62, NOW(), NOW())`, f.campaign, f.user, f.org)

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM campaign_leads WHERE campaign_id IN (SELECT id FROM campaigns WHERE organization_id = $1)`, f.org},
			{`DELETE FROM campaigns WHERE organization_id = $1`, f.org},
			{`DELETE FROM contact_categories WHERE contact_id IN (SELECT id FROM contacts WHERE organization_id = $1)`, f.org},
			{`DELETE FROM contacts WHERE organization_id = $1`, f.org},
			{`DELETE FROM categories WHERE user_id = $1`, f.user},
			{`DELETE FROM organization_members WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := f.pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})

	f.repo = repository.NewContactRepostory(handle)
	// nil sub/plan repos: the plan cap is skipped, which is what we want here.
	f.svc = NewService(f.repo, nil, nil)
	return f
}

func (f *importFixture) newCategory(t *testing.T, title string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO categories (id, user_id, title, color, position) VALUES ($1, $2, $3, '#38bdf8', 0)`,
		id, f.user, title); err != nil {
		t.Fatalf("create category: %v", err)
	}
	return id
}

func (f *importFixture) commit(t *testing.T, csv string, opts *models.ContactImportCommit) (*models.ContactImportResult, string) {
	t.Helper()
	res, xerr := f.svc.ImportCommit(context.Background(), f.user.String(), f.org,
		strings.NewReader(csv), "leads.csv", opts)
	if xerr != nil {
		return nil, xerr.Message
	}
	// Every row lands in exactly one bucket. A count that stops adding up is
	// how "1,000 failed" hid a mapping bug behind per-row noise.
	if sum := res.Imported + res.Updated + res.Skipped + res.Failed; sum != res.Total {
		t.Fatalf("counts do not add up: total=%d imported=%d updated=%d skipped=%d failed=%d",
			res.Total, res.Imported, res.Updated, res.Skipped, res.Failed)
	}
	return res, ""
}

func col(idx int, target models.ContactImportColumnTarget) models.ContactImportColumnMapping {
	return models.ContactImportColumnMapping{Index: idx, Target: target}
}

func customCol(idx int, key string) models.ContactImportColumnMapping {
	return models.ContactImportColumnMapping{
		Index: idx, Target: models.ContactImportTargetCustom, CustomKey: key,
	}
}

// thirteenColumnCSV mirrors the file in the report: 13 columns, several of
// them custom fields whose names contain spaces.
func thirteenColumnCSV(rows int) string {
	var b strings.Builder
	b.WriteString("Email,First Name,Last Name,Company,Phone,Company Mobile,Job Title,Seniority Level,Employee Count,Annual Revenue,LinkedIn URL,City,Country\n")
	for i := 0; i < rows; i++ {
		fmt.Fprintf(&b,
			"lead%04d@iqonic.test,Ada%04d,Lovelace,Iqonic %04d,+15550000%03d,+15551110%03d,Head of Growth,VP,250,10M,linkedin-%04d,Berlin,DE\n",
			i, i, i, i%1000, i%1000, i)
	}
	return b.String()
}

func thirteenColumnMapping() []models.ContactImportColumnMapping {
	return []models.ContactImportColumnMapping{
		col(0, models.ContactImportTargetEmail),
		col(1, models.ContactImportTargetFirstName),
		col(2, models.ContactImportTargetLastName),
		col(3, models.ContactImportTargetCompany),
		col(4, models.ContactImportTargetPhone),
		customCol(5, "Company Mobile"),
		customCol(6, "Job Title"),
		customCol(7, "Seniority Level"),
		customCol(8, "Employee Count"),
		customCol(9, "Annual Revenue"),
		customCol(10, "LinkedIn URL"),
		customCol(11, "City"),
		customCol(12, "Country"),
	}
}

func TestLiveImportThousandRowsWithSpacedCustomFields(t *testing.T) {
	f := newImportFixture(t)

	res, msg := f.commit(t, thirteenColumnCSV(1000), &models.ContactImportCommit{
		Mapping:     thirteenColumnMapping(),
		Dedup:       models.ContactImportDedupSkip,
		HasHeader:   true,
		CampaignIDs: []string{f.campaign.String()},
	})
	if msg != "" {
		t.Fatalf("import rejected: %s", msg)
	}
	if res.Failed != 0 {
		t.Fatalf("failed=%d (want 0); first errors: %+v", res.Failed, firstN(res.Errors, 3))
	}
	if res.Imported != 1000 {
		t.Fatalf("imported=%d updated=%d skipped=%d (want imported 1000)", res.Imported, res.Updated, res.Skipped)
	}

	// The spaced key is stored verbatim, so {{.Company Mobile}} resolves.
	var mobile, title string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT custom_fields ->> 'Company Mobile', custom_fields ->> 'Job Title'
		 FROM contacts WHERE organization_id = $1 AND email = 'lead0007@iqonic.test'`,
		f.org).Scan(&mobile, &title); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if mobile == "" || title != "Head of Growth" {
		t.Fatalf("custom fields not stored: mobile=%q title=%q", mobile, title)
	}

	// Every row joined the campaign it was imported into.
	var leads int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM campaign_leads WHERE campaign_id = $1`, f.campaign).Scan(&leads); err != nil {
		t.Fatalf("count leads: %v", err)
	}
	if leads != 1000 {
		t.Fatalf("campaign_leads=%d (want 1000)", leads)
	}

	// A custom field with a space is filterable through the same UI path.
	found, xerr := f.repo.Search(context.Background(), f.org.String(), nil, nil, models.SearchContacts{
		CustomFieldFilters: []models.SearchContactsFilter{
			{Name: "Job Title", Value: "Head of Growth", Type: models.SearchContactsFilterTypeEqual},
		},
	}, 5)
	if xerr != nil {
		t.Fatalf("search by custom field: %s", xerr.Message)
	}
	if len(found.Data) == 0 {
		t.Fatalf("custom-field filter on a spaced key returned nothing")
	}
}

func TestLiveImportRejectsBadMappingOnceNotPerRow(t *testing.T) {
	f := newImportFixture(t)

	mapping := thirteenColumnMapping()
	mapping[5] = customCol(5, "Company/Mobile") // a slash is not addressable
	res, msg := f.commit(t, thirteenColumnCSV(50), &models.ContactImportCommit{
		Mapping: mapping, Dedup: models.ContactImportDedupSkip, HasHeader: true,
	})
	if res != nil {
		t.Fatalf("expected a 400, got a result with %d errors", len(res.Errors))
	}
	if !strings.Contains(msg, "Company/Mobile") || !strings.Contains(msg, "letters") {
		t.Fatalf("unhelpful message: %q", msg)
	}

	// An unnamed custom column is rejected the same way.
	mapping[5] = customCol(5, "   ")
	if res, msg = f.commit(t, thirteenColumnCSV(5), &models.ContactImportCommit{
		Mapping: mapping, Dedup: models.ContactImportDedupSkip, HasHeader: true,
	}); res != nil || !strings.Contains(msg, "no name") {
		t.Fatalf("unnamed custom column: res=%v msg=%q", res, msg)
	}

	// So is a mapping with no email column at all.
	if res, msg = f.commit(t, thirteenColumnCSV(5), &models.ContactImportCommit{
		Mapping:   []models.ContactImportColumnMapping{col(1, models.ContactImportTargetFirstName)},
		HasHeader: true,
	}); res != nil || !strings.Contains(msg, "Email") {
		t.Fatalf("missing email mapping: res=%v msg=%q", res, msg)
	}
}

func TestLiveImportHonoursSubscribedAndCategoriesColumns(t *testing.T) {
	f := newImportFixture(t)
	ctx := context.Background()

	csv := "email,subscribed,tags\n" +
		"a@iqonic.test,yes,Agency; Enterprise\n" +
		"b@iqonic.test,unsubscribed,Agency\n" +
		"c@iqonic.test,,Startup\n"
	res, msg := f.commit(t, csv, &models.ContactImportCommit{
		Mapping: []models.ContactImportColumnMapping{
			col(0, models.ContactImportTargetEmail),
			col(1, models.ContactImportTargetSubscribed),
			col(2, models.ContactImportTargetCategories),
		},
		Dedup:     models.ContactImportDedupSkip,
		HasHeader: true,
	})
	if msg != "" {
		t.Fatalf("import rejected: %s", msg)
	}
	if res.Imported != 3 || res.Failed != 0 {
		t.Fatalf("imported=%d failed=%d %+v", res.Imported, res.Failed, res.Errors)
	}

	subs := map[string]bool{}
	rows, err := f.pool.Query(ctx, `SELECT email, subscribed FROM contacts WHERE organization_id = $1`, f.org)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for rows.Next() {
		var e string
		var sub bool
		if err := rows.Scan(&e, &sub); err != nil {
			t.Fatalf("scan: %v", err)
		}
		subs[e] = sub
	}
	rows.Close()
	if subs["a@iqonic.test"] != true || subs["b@iqonic.test"] != false || subs["c@iqonic.test"] != true {
		t.Fatalf("subscribed column ignored: %+v", subs)
	}

	// Categories named in the file exist and are attached.
	var linked int
	if err := f.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM contact_categories cc
		JOIN categories cat ON cat.id = cc.category_id
		JOIN contacts c ON c.id = cc.contact_id
		WHERE c.organization_id = $1 AND cat.title = 'Agency'`, f.org).Scan(&linked); err != nil {
		t.Fatalf("count categories: %v", err)
	}
	if linked != 2 {
		t.Fatalf("Agency category attached to %d contacts (want 2)", linked)
	}
}

func TestLiveImportDeduplicatesWithinTheFileAndLinksSkippedRows(t *testing.T) {
	f := newImportFixture(t)
	ctx := context.Background()

	csv := "email,first_name\n" +
		"dup@iqonic.test,Ada\n" +
		"dup@iqonic.test,Ada\n" +
		"other@iqonic.test,Grace\n"
	mapping := []models.ContactImportColumnMapping{
		col(0, models.ContactImportTargetEmail),
		col(1, models.ContactImportTargetFirstName),
	}
	res, msg := f.commit(t, csv, &models.ContactImportCommit{
		Mapping: mapping, Dedup: models.ContactImportDedupSkip, HasHeader: true,
	})
	if msg != "" {
		t.Fatalf("import rejected: %s", msg)
	}
	if res.Imported != 2 || res.Skipped != 1 {
		t.Fatalf("imported=%d skipped=%d (want 2/1)", res.Imported, res.Skipped)
	}

	// Re-importing the same people into a campaign must add them as leads
	// even though "skip existing" leaves their fields alone, and a blank
	// name column must not erase the name already stored.
	category := f.newCategory(t, "Re-import")
	res, msg = f.commit(t, "email,first_name\ndup@iqonic.test,\nother@iqonic.test,\n", &models.ContactImportCommit{
		Mapping: mapping, Dedup: models.ContactImportDedupSkip, HasHeader: true,
		CampaignIDs: []string{f.campaign.String()},
		CategoryIDs: []string{category.String()},
	})
	if msg != "" {
		t.Fatalf("second import rejected: %s", msg)
	}
	if res.Skipped != 2 || res.Imported != 0 {
		t.Fatalf("second import: imported=%d skipped=%d (want 0/2)", res.Imported, res.Skipped)
	}
	var leads int
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_leads WHERE campaign_id = $1`, f.campaign).Scan(&leads); err != nil {
		t.Fatalf("count leads: %v", err)
	}
	if leads != 2 {
		t.Fatalf("skipped rows were not added to the campaign: campaign_leads=%d (want 2)", leads)
	}
	var name string
	if err := f.pool.QueryRow(ctx,
		`SELECT first_name FROM contacts WHERE organization_id = $1 AND email = 'dup@iqonic.test'`,
		f.org).Scan(&name); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if name != "Ada" {
		t.Fatalf("blank cell erased the stored name: %q", name)
	}
	var tagged int
	if err := f.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM contact_categories cc
		JOIN contacts c ON c.id = cc.contact_id
		WHERE c.organization_id = $1 AND cc.category_id = $2`, f.org, category).Scan(&tagged); err != nil {
		t.Fatalf("count categories: %v", err)
	}
	if tagged != 2 {
		t.Fatalf("skipped rows did not get the import's categories: %d (want 2)", tagged)
	}
}

func TestLiveImportMixedFileCountsEveryRowOnce(t *testing.T) {
	f := newImportFixture(t)

	mapping := []models.ContactImportColumnMapping{
		col(0, models.ContactImportTargetEmail),
		col(1, models.ContactImportTargetFirstName),
	}
	if _, msg := f.commit(t, "email,first_name\nseed@iqonic.test,Seed\n", &models.ContactImportCommit{
		Mapping: mapping, Dedup: models.ContactImportDedupSkip, HasHeader: true,
	}); msg != "" {
		t.Fatalf("seed import rejected: %s", msg)
	}

	// new + already-there + unusable address + the same address twice.
	res, msg := f.commit(t, "email,first_name\n"+
		"fresh@iqonic.test,Fresh\n"+
		"seed@iqonic.test,Seed\n"+
		"not-an-email,Broken\n"+
		"twice@iqonic.test,Twice\n"+
		"twice@iqonic.test,Twice\n", &models.ContactImportCommit{
		Mapping: mapping, Dedup: models.ContactImportDedupSkip, HasHeader: true,
	})
	if msg != "" {
		t.Fatalf("import rejected: %s", msg)
	}
	if res.Total != 5 || res.Imported != 2 || res.Skipped != 2 || res.Failed != 1 {
		t.Fatalf("total=%d imported=%d updated=%d skipped=%d failed=%d (want 5/2/0/2/1)",
			res.Total, res.Imported, res.Updated, res.Skipped, res.Failed)
	}
	if len(res.Errors) != 1 || res.Errors[0].Line != 4 {
		t.Fatalf("expected one error on line 4, got %+v", res.Errors)
	}
}

// A blank campaign or category id reaches Postgres as `” = ANY($1::uuid[])`
// and fails the statement, which showed up as every row failing to link. The
// request is canonicalised before anything is written.
func TestLiveImportIgnoresBlankCampaignAndCategoryIDs(t *testing.T) {
	f := newImportFixture(t)

	mapping := []models.ContactImportColumnMapping{col(0, models.ContactImportTargetEmail)}
	csv := "email\nblank-ids@iqonic.test\n"
	if _, msg := f.commit(t, csv, &models.ContactImportCommit{
		Mapping: mapping, Dedup: models.ContactImportDedupSkip, HasHeader: true,
	}); msg != "" {
		t.Fatalf("seed import rejected: %s", msg)
	}

	// Second pass: the contact exists, so it goes down the skipped-link path.
	res, msg := f.commit(t, csv, &models.ContactImportCommit{
		Mapping: mapping, Dedup: models.ContactImportDedupSkip, HasHeader: true,
		CampaignIDs: []string{""}, CategoryIDs: []string{"", " "},
	})
	if msg != "" {
		t.Fatalf("import rejected: %s", msg)
	}
	if res.Failed != 0 || res.Skipped != 1 {
		t.Fatalf("blank ids broke the import: skipped=%d failed=%d %+v", res.Skipped, res.Failed, res.Errors)
	}

	// A malformed id is a request error, not a per-row one.
	if res, msg = f.commit(t, csv, &models.ContactImportCommit{
		Mapping: mapping, Dedup: models.ContactImportDedupSkip, HasHeader: true,
		CampaignIDs: []string{"not-a-uuid"},
	}); res != nil || msg == "" {
		t.Fatalf("malformed campaign id: res=%v msg=%q", res, msg)
	}
}

func TestLiveImportUpdateDoesNotResubscribeOrErase(t *testing.T) {
	f := newImportFixture(t)
	ctx := context.Background()

	// Someone who already opted out, with a name and a custom field.
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, subscribed)
		VALUES (gen_random_uuid(), $1, $2, 'opted-out@iqonic.test', 'Grace', 'Hopper', 'Navy', '', '{"Job Title":"Rear Admiral"}'::jsonb, FALSE)`,
		f.user, f.org); err != nil {
		t.Fatalf("seed contact: %v", err)
	}

	yes := true
	res, msg := f.commit(t, "email,first_name,company\nopted-out@iqonic.test,,Iqonic\n", &models.ContactImportCommit{
		Mapping: []models.ContactImportColumnMapping{
			col(0, models.ContactImportTargetEmail),
			col(1, models.ContactImportTargetFirstName),
			col(2, models.ContactImportTargetCompany),
		},
		Dedup:             models.ContactImportDedupUpdate,
		HasHeader:         true,
		SubscribedDefault: &yes,
	})
	if msg != "" {
		t.Fatalf("import rejected: %s", msg)
	}
	if res.Updated != 1 {
		t.Fatalf("updated=%d (want 1)", res.Updated)
	}

	var sub bool
	var first, company, title string
	if err := f.pool.QueryRow(ctx, `
		SELECT subscribed, first_name, company, COALESCE(custom_fields ->> 'Job Title', '')
		FROM contacts WHERE organization_id = $1 AND email = 'opted-out@iqonic.test'`,
		f.org).Scan(&sub, &first, &company, &title); err != nil {
		t.Fatalf("read back: %v", err)
	}
	// subscribed_default applies to NEW contacts only.
	if sub {
		t.Fatalf("an update resubscribed a contact who had opted out")
	}
	if first != "Grace" {
		t.Fatalf("blank cell erased first_name: %q", first)
	}
	if company != "Iqonic" {
		t.Fatalf("update did not apply: company=%q", company)
	}
	if title != "Rear Admiral" {
		t.Fatalf("update dropped an existing custom field: %q", title)
	}
}

func firstN[T any](in []T, n int) []T {
	if len(in) < n {
		return in
	}
	return in[:n]
}

// simpleCSV builds an email-only file from the given addresses.
func simpleCSV(emails []string) string {
	var b strings.Builder
	b.WriteString("email\n")
	for _, e := range emails {
		b.WriteString(e)
		b.WriteString("\n")
	}
	return b.String()
}

func emailOnlyMapping() []models.ContactImportColumnMapping {
	return []models.ContactImportColumnMapping{{Index: 0, Target: "email"}}
}

func repeatEmails(pattern string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf(pattern, i))
	}
	return out
}

// Issue #145: the assessment has to reach the RESULT, not just exist as a
// package. Testing listquality directly could never catch a call site that
// never runs, which is the failure this codebase keeps producing.
func TestLiveImportReportsListQuality(t *testing.T) {
	f := newImportFixture(t)

	emails := append(repeatEmails("junk%d-not-an-email", 30), repeatEmails("throwaway%d@mailinator.com", 30)...)
	emails = append(emails, repeatEmails("real%d@acme.test", 40)...)

	res, msg := f.commit(t, simpleCSV(emails), &models.ContactImportCommit{
		Mapping:   emailOnlyMapping(),
		Dedup:     models.ContactImportDedupSkip,
		HasHeader: true,
	})
	if msg != "" {
		t.Fatalf("import rejected: %s", msg)
	}
	if res.Quality == nil {
		t.Fatal("no quality assessment on the result; the import path never ran it")
	}
	if !res.Quality.Flagged {
		t.Errorf("a 60%% unusable list was not flagged: %+v", res.Quality)
	}
	if res.Quality.Disposable != 30 {
		t.Errorf("disposable = %d, want the 30 throwaway addresses", res.Quality.Disposable)
	}
	if res.Quality.Summary == "" {
		t.Error("a flagged list must say what is wrong with it")
	}
	// The import still happened: these are the customer's own records, and the
	// launch gate is where a bad list is actually stopped.
	if res.Imported == 0 {
		t.Error("a flagged import stored nothing; it should report, not refuse")
	}
}

// An ordinary list must come back with nothing to say.
func TestLiveImportOfAnOrdinaryListIsQuiet(t *testing.T) {
	f := newImportFixture(t)

	res, msg := f.commit(t, simpleCSV(repeatEmails("person%d@acme.test", 60)), &models.ContactImportCommit{
		Mapping:   emailOnlyMapping(),
		Dedup:     models.ContactImportDedupSkip,
		HasHeader: true,
	})
	if msg != "" {
		t.Fatalf("import rejected: %s", msg)
	}
	if res.Quality == nil {
		t.Fatal("no quality assessment on the result")
	}
	if res.Quality.Flagged {
		t.Errorf("an ordinary list was flagged: %+v", res.Quality)
	}
	if res.Imported != 60 {
		t.Errorf("imported = %d, want 60", res.Imported)
	}
}
