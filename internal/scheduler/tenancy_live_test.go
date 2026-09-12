package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
)

// Regression cover for issue #167: campaign sender resolution used to filter on
// the campaign OWNER's user_id, so a user who belongs to two organizations
// could have organization A's campaign pick up an organization B mailbox and
// burn B's reputation, daily caps and warmup state.
//
// Run against the dev stack:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/scheduler/ -run LiveSender -v

// foreignOrg adds a SECOND organization owned by the same user, with its own
// active mailbox — the neighbouring tenant that must never be reachable.
type foreignOrg struct {
	org     uuid.UUID
	mailbox uuid.UUID
}

func newForeignOrg(t *testing.T, pool *pgxpool.Pool, f *liveFixture) *foreignOrg {
	t.Helper()
	ctx := context.Background()
	o := &foreignOrg{org: uuid.New(), mailbox: uuid.New()}

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("foreign org fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}

	exec(`INSERT INTO organizations (id, name, slug, owner_user_id)
	      VALUES ($1, 'Foreign Tenant', $2, $3)`, o.org, "foreign-"+o.org.String()[:8], f.user)
	// The condition the bug needs: one login, membership in both workspaces.
	exec(`INSERT INTO organization_members (organization_id, user_id, role, accepted_at)
	      VALUES ($1, $2, 'owner', NOW()), ($3, $2, 'owner', NOW())`, f.org, f.user, o.org)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Foreign', '', '', 'smtp_imap', 'active', 50, 600, 'UTC')`,
		o.mailbox, f.user, o.org, "foreign-"+o.mailbox.String()[:8]+"@test.local")

	t.Cleanup(func() {
		c := context.Background()
		steps := []struct {
			sql string
			arg any
		}{
			{`DELETE FROM campaign_senders WHERE email_account_id = $1`, o.mailbox},
			{`DELETE FROM email_tags WHERE email_id = $1`, o.mailbox},
			{`DELETE FROM email_accounts WHERE id = $1`, o.mailbox},
			{`DELETE FROM organization_members WHERE user_id = $1`, f.user},
			{`DELETE FROM organizations WHERE id = $1`, o.org},
		}
		for _, step := range steps {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return o
}

// tagMailboxes creates ONE user-owned tag, attaches it to the given mailboxes
// and puts it on the campaign. Tags carry no organization of their own, which is
// exactly why the tag path needed the organization predicate: one user's tag
// legitimately spans both workspaces.
func tagMailboxes(t *testing.T, pool *pgxpool.Pool, f *liveFixture, mailboxes ...uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	tag := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO tags (id, organization_id, user_id, title, color, "position") VALUES ($1, $2, $3, 'senders', '#aabbcc', 0)`,
		tag, f.org, f.user); err != nil {
		t.Fatalf("insert tag: %v", err)
	}
	for _, mailbox := range mailboxes {
		if _, err := pool.Exec(ctx,
			`INSERT INTO email_tags (email_id, tag_id) VALUES ($1, $2)`, mailbox, tag); err != nil {
			t.Fatalf("tag mailbox %s: %v", mailbox, err)
		}
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO campaign_email_tags (campaign_id, tag_id) VALUES ($1, $2)`, f.campaign, tag); err != nil {
		t.Fatalf("attach tag to campaign: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		for _, sql := range []string{
			`DELETE FROM campaign_email_tags WHERE tag_id = $1`,
			`DELETE FROM email_tags WHERE tag_id = $1`,
			`DELETE FROM tags WHERE id = $1`,
		} {
			if _, err := pool.Exec(c, sql, tag); err != nil {
				t.Errorf("cleanup %q: %v", sql, err)
			}
		}
	})
	return tag
}

func liveEmailRepo(t *testing.T, handle *db.DB) repository.EmailRepository {
	t.Helper()
	enc, err := encrypt.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("encrypter: %v", err)
	}
	return repository.NewEmailRepostory(handle, enc)
}

// mailboxIDs renders a result set as ids, so a failure names the mailboxes that
// leaked rather than just their count.
func mailboxIDs(accounts []models.Email) string {
	ids := make([]uuid.UUID, len(accounts))
	for i, a := range accounts {
		ids[i] = a.ID
	}
	return idsOf(ids)
}

func idsOf(ids []uuid.UUID) string {
	if len(ids) == 0 {
		return "no mailboxes"
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return strings.Join(out, ", ")
}

// TestLiveSenderResolutionStaysInsideTheCampaignOrg walks all three resolution
// paths — explicit senders, tags, and the "all active mailboxes" fallback —
// and asserts each one returns only the campaign organization's mailbox.
func TestLiveSenderResolutionStaysInsideTheCampaignOrg(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	o := newForeignOrg(t, pool, f)
	ctx := context.Background()

	repo := liveEmailRepo(t, handle)
	scope := repository.NewAccountScope(&f.org)

	// 1. The "all" fallback: no tags, no explicit senders.
	all, xerr := repo.GetAllActiveInScope(ctx, scope)
	if xerr != nil {
		t.Fatalf("all-active lookup: %v", xerr)
	}
	if len(all) != 1 || all[0].ID != f.mailbox {
		t.Fatalf("all-active fallback returned %s, want only the campaign org's mailbox %s",
			mailboxIDs(all), f.mailbox)
	}

	// 2. The tag path, with ONE tag on both mailboxes.
	tag := tagMailboxes(t, pool, f, f.mailbox, o.mailbox)
	tagged, xerr := repo.GetByTags(ctx, scope, []string{tag.String()})
	if xerr != nil {
		t.Fatalf("tag lookup: %v", xerr)
	}
	if len(tagged) != 1 || tagged[0].ID != f.mailbox {
		t.Fatalf("tag resolution returned %s, want only the campaign org's mailbox %s",
			mailboxIDs(tagged), f.mailbox)
	}

	// 3. The explicit sender pool, with the FOREIGN mailbox pinned to the
	// campaign (a row that predates the ownership check, or one written while
	// the campaign had no organization).
	if _, err := pool.Exec(ctx,
		`INSERT INTO campaign_senders (campaign_id, email_account_id, weight, enabled)
		 VALUES ($1, $2, 1, true), ($1, $3, 1, true)`, f.campaign, f.mailbox, o.mailbox); err != nil {
		t.Fatalf("pin senders: %v", err)
	}
	senders, xerr := repo.GetByCampaignSenders(ctx, scope, f.campaign)
	if xerr != nil {
		t.Fatalf("sender lookup: %v", xerr)
	}
	if len(senders) != 1 || senders[0].Account.ID != f.mailbox {
		ids := make([]uuid.UUID, len(senders))
		for i, s := range senders {
			ids[i] = s.Account.ID
		}
		t.Fatalf("explicit sender pool returned %s, want only the campaign org's mailbox %s",
			idsOf(ids), f.mailbox)
	}
}

// TestLiveSenderSchedulerNeverPicksAnotherOrgMailbox is the end-to-end shape of
// the same bug, arranged so only the WRONG answer is reachable: the campaign's
// tag sits on the other organization's mailbox alone. Before the fix the
// scheduler resolved it by owner and handed back that foreign mailbox; now the
// campaign correctly has nothing to send from.
func TestLiveSenderSchedulerNeverPicksAnotherOrgMailbox(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	o := newForeignOrg(t, pool, f)

	tagMailboxes(t, pool, f, o.mailbox)

	_, _, accountID, err := liveScheduler(t, handle, pool).
		CalculateNextCampaignTime(context.Background(), f.campaign)
	if accountID == o.mailbox {
		t.Fatalf("scheduler picked the OTHER organization's mailbox %s", o.mailbox)
	}
	if !errors.Is(err, ErrNoEmailAccounts) {
		t.Fatalf("want ErrNoEmailAccounts when the only tagged mailbox belongs to another organization, got %v (account %s)",
			err, accountID)
	}
}

// TestLiveSenderSchedulerPicksTheCampaignOrgMailbox is the other half: when both
// workspaces' mailboxes carry the campaign's tag, the campaign's own mailbox is
// the one that sends.
func TestLiveSenderSchedulerPicksTheCampaignOrgMailbox(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	o := newForeignOrg(t, pool, f)

	tagMailboxes(t, pool, f, f.mailbox, o.mailbox)

	_, accountID := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)
	if accountID != f.mailbox {
		t.Fatalf("scheduler picked %s, want the campaign org's mailbox %s", accountID, f.mailbox)
	}
}

// TestLiveSenderScopeWithoutAnOrganizationReachesNothing pins the fail-closed
// half of the rule. organization_id is NOT NULL since migration 000092, so this
// scope should be unreachable — but if one ever appears, it must resolve to no
// mailboxes rather than widening to the owner, which is how a campaign reached
// another workspace in the first place.
func TestLiveSenderScopeWithoutAnOrganizationReachesNothing(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	repo := liveEmailRepo(t, handle)
	empty := repository.NewAccountScope(nil)

	all, xerr := repo.GetAllActiveInScope(ctx, empty)
	if xerr != nil {
		t.Fatalf("all-active lookup: %v", xerr)
	}
	if len(all) != 0 {
		t.Fatalf("a scope with no organization reached %s", mailboxIDs(all))
	}

	tag := tagMailboxes(t, pool, f, f.mailbox)
	tagged, xerr := repo.GetByTags(ctx, empty, []string{tag.String()})
	if xerr != nil {
		t.Fatalf("tag lookup: %v", xerr)
	}
	if len(tagged) != 0 {
		t.Fatalf("a scope with no organization reached %s through tags", mailboxIDs(tagged))
	}

	senders, xerr := repo.GetByCampaignSenders(ctx, empty, f.campaign)
	if xerr != nil {
		t.Fatalf("sender lookup: %v", xerr)
	}
	if len(senders) != 0 {
		t.Fatalf("a scope with no organization reached %d explicit senders", len(senders))
	}
}

// TestLiveActiveCampaignLookupIsOrgScoped covers the same tenancy rule on the
// other side of the join: the warmup floor asks "does this mailbox back an
// active campaign?", and an "all" campaign (no tags, no explicit senders) used
// to answer yes for every mailbox its OWNER had, in any workspace.
func TestLiveActiveCampaignLookupIsOrgScoped(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	o := newForeignOrg(t, pool, f)
	ctx := context.Background()

	repo := repository.NewCampaignRepostory(handle)

	// The fixture campaign is active, in org A, with neither tags nor explicit
	// senders — so it backs org A's mailbox and nothing else.
	backs, err := repo.AccountHasActiveCampaign(ctx, f.mailbox)
	if err != nil {
		t.Fatalf("own-org lookup: %v", err)
	}
	if !backs {
		t.Fatal("the campaign's own organization mailbox should back its active campaign")
	}

	backs, err = repo.AccountHasActiveCampaign(ctx, o.mailbox)
	if err != nil {
		t.Fatalf("foreign-org lookup: %v", err)
	}
	if backs {
		t.Fatalf("mailbox %s in another organization counts as backing the campaign", o.mailbox)
	}

	count, err := repo.CountActiveCampaignsForAccount(ctx, o.mailbox)
	if err != nil {
		t.Fatalf("foreign-org count: %v", err)
	}
	if count != 0 {
		t.Fatalf("mailbox %s in another organization counts %d active campaigns, want 0", o.mailbox, count)
	}
}
