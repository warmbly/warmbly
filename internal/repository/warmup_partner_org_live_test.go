package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// partnerOrgFixture supplies sibling and outside partner ownership.
type partnerOrgFixture struct {
	pool   *pgxpool.Pool
	user   uuid.UUID
	org    uuid.UUID
	other  uuid.UUID
	sender uuid.UUID
	// sibling and outside distinguish the two ownership tiers.
	sibling uuid.UUID
	outside uuid.UUID
	exec    func(sql string, args ...any)
}

func newPartnerOrgFixture(t *testing.T) *partnerOrgFixture {
	t.Helper()
	_, pool := liveContactDB(t)
	ctx := context.Background()
	f := &partnerOrgFixture{
		pool: pool, user: uuid.New(), org: uuid.New(), other: uuid.New(),
		sender: uuid.New(), sibling: uuid.New(), outside: uuid.New(),
	}
	f.exec = func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	f.exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Org', 'Pairing')`,
		f.user, "org-pair-"+f.user.String()[:8]+"@test.local")
	for _, org := range []uuid.UUID{f.org, f.other} {
		f.exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Org pairing', $2, $3)`,
			org, "org-pair-"+org.String()[:8], f.user)
	}
	for _, m := range []struct {
		id     uuid.UUID
		org    uuid.UUID
		domain string
	}{
		{f.sender, f.org, "own-a.test"},
		{f.sibling, f.org, "own-b.test"},
		{f.outside, f.other, "elsewhere.test"},
	} {
		f.exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain,
		            signature_html, provider, status, campaign_limit, min_wait_time, timezone)
		        VALUES ($1, $2, $3, $4, 'Org pairing', '', '', 'smtp_imap', 'active', 50, 600, 'UTC')`,
			m.id, f.user, m.org, "box-"+m.id.String()[:8]+"@"+m.domain)
	}
	for _, id := range []uuid.UUID{f.sender, f.sibling, f.outside} {
		f.exec(`INSERT INTO warmup_pool_participants (pool_id, email_account_id, participant_role, health_state)
		        VALUES ($1, $2, 'sender_receiver', 'healthy')`, premiumPoolID, id)
	}
	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM warmup_tokens WHERE sender_account_id = $1`, f.sender},
			{`DELETE FROM tasks WHERE email_account_id = $1`, f.sender},
			{`DELETE FROM warmup_received WHERE email_account_id IN
			    (SELECT id FROM email_accounts WHERE user_id = $1)`, f.user},
			{`DELETE FROM warmup_pool_participants WHERE email_account_id IN
			    (SELECT id FROM email_accounts WHERE user_id = $1)`, f.user},
			{`DELETE FROM email_accounts WHERE user_id = $1`, f.user},
			{`DELETE FROM organizations WHERE owner_user_id = $1`, f.user},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

// warmed records one warmup send from the sender to a recipient.
func (f *partnerOrgFixture) warmed(t *testing.T, recipient uuid.UUID) {
	t.Helper()
	f.attempted(t, recipient, "completed")
}

// attempted records a token and its task outcome.
func (f *partnerOrgFixture) attempted(t *testing.T, recipient uuid.UUID, status string) {
	t.Helper()
	taskID := uuid.New()
	f.exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id)
	        VALUES ($1, 'warmup', $2, $3, '')`, taskID, f.sender, status)
	f.exec(`INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, sent_message_id, created_at)
	        VALUES (gen_random_uuid(), $1, $2, $3, $4, NOW())`, taskID, f.sender, recipient, "<"+taskID.String()+"@test.local>")
}

// Candidates retain siblings while carrying ownership for later ranking.
func TestLiveWarmupPartnerCandidatesCarryTheirOwner(t *testing.T) {
	f := newPartnerOrgFixture(t)
	repo := &warmupRepository{db: f.pool}

	cands, err := repo.WarmupPartnerCandidates(context.Background(), "premium", f.sender)
	if err != nil {
		t.Fatalf("WarmupPartnerCandidates: %v", err)
	}
	seen := map[uuid.UUID]*uuid.UUID{}
	for _, c := range cands {
		seen[c.ID] = c.OrganizationID
	}
	if _, ok := seen[f.sender]; ok {
		t.Fatal("the sender is its own candidate")
	}
	for _, tc := range []struct {
		name string
		id   uuid.UUID
		want uuid.UUID
	}{
		{"sibling", f.sibling, f.org},
		{"outside partner", f.outside, f.other},
	} {
		got, ok := seen[tc.id]
		if !ok {
			t.Fatalf("%s is missing from the candidate set", tc.name)
		}
		if got == nil || *got != tc.want {
			t.Fatalf("%s owner = %v, want %s", tc.name, got, tc.want)
		}
	}
}

// Diversity counts distinct confirmed partner reach.
func TestLiveGetPartnerDiversityCountsDistinctPartners(t *testing.T) {
	f := newPartnerOrgFixture(t)
	ctx := context.Background()
	repo := &warmupRepository{db: f.pool}
	since := time.Now().Add(-7 * 24 * time.Hour)

	if d, err := repo.GetPartnerDiversity(ctx, f.sender, since); err != nil {
		t.Fatalf("GetPartnerDiversity on a mailbox that has sent nothing: %v", err)
	} else if d.Mailboxes != 0 || d.Domains != 0 || d.Organizations != 0 {
		t.Fatalf("a mailbox that sent nothing reads %+v, want zeros", d)
	}

	for _, status := range []string{"completed", "failed"} {
		taskID := uuid.New()
		f.exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id)
		        VALUES ($1, 'warmup', $2, $3, '')`, taskID, f.sender, status)
		f.exec(`INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id)
		        VALUES (gen_random_uuid(), $1, $2, $3)`, taskID, f.sender, f.sibling)
	}
	if d, err := repo.GetPartnerDiversity(ctx, f.sender, since); err != nil {
		t.Fatalf("GetPartnerDiversity on unconfirmed sends: %v", err)
	} else if d.Mailboxes != 0 || d.Domains != 0 || d.Organizations != 0 {
		t.Fatalf("unconfirmed sends read %+v, want zeros", d)
	}

	// Repeated sends to one sibling remain one distinct partner.
	for i := 0; i < 3; i++ {
		f.warmed(t, f.sibling)
	}
	d, err := repo.GetPartnerDiversity(ctx, f.sender, since)
	if err != nil {
		t.Fatalf("GetPartnerDiversity: %v", err)
	}
	if d.Mailboxes != 1 || d.Domains != 1 || d.Organizations != 1 {
		t.Fatalf("three sends to one sibling read %+v, want 1/1/1", d)
	}

	f.warmed(t, f.outside)
	d, err = repo.GetPartnerDiversity(ctx, f.sender, since)
	if err != nil {
		t.Fatalf("GetPartnerDiversity: %v", err)
	}
	if d.Mailboxes != 2 || d.Domains != 2 || d.Organizations != 2 {
		t.Fatalf("after an outside partner: %+v, want 2/2/2", d)
	}

	// A failed token cannot credit a partner that received no mail.
	third := uuid.New()
	f.exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain,
	            signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	        VALUES ($1, $2, $3, $4, 'Third', '', '', 'smtp_imap', 'active', 50, 600, 'UTC')`,
		third, f.user, f.other, "third-"+third.String()[:8]+"@elsewhere-two.test")
	f.attempted(t, third, "failed")
	d, err = repo.GetPartnerDiversity(ctx, f.sender, since)
	if err != nil {
		t.Fatalf("GetPartnerDiversity: %v", err)
	}
	if d.Mailboxes != 2 || d.Domains != 2 || d.Organizations != 2 {
		t.Fatalf("a refused send counted as diversity: %+v, want 2/2/2", d)
	}

	// Outside the window nothing counts, so the number is about this week.
	if d, err := repo.GetPartnerDiversity(ctx, f.sender, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("GetPartnerDiversity: %v", err)
	} else if d.Mailboxes != 0 {
		t.Fatalf("a future window counted %d partners", d.Mailboxes)
	}
}

// received records one verified warmup arrival at recipient from sender.
func (f *partnerOrgFixture) received(t *testing.T, recipient, sender uuid.UUID, at time.Time) {
	t.Helper()
	f.exec(`INSERT INTO warmup_received (email_account_id, internal_id, message_id, sender_account_id, created_at)
	        VALUES ($1, gen_random_uuid(), $2, $3, $4)`, recipient, "<"+uuid.NewString()+"@test.local>", sender, at)
}

// moveToFree re-files a fixture mailbox into the free tier, proven for the
// given number of days.
func (f *partnerOrgFixture) moveToFree(t *testing.T, id uuid.UUID, memberForDays int) {
	t.Helper()
	f.exec(`UPDATE warmup_pool_participants
	           SET pool_id = $2, joined_at = NOW() - make_interval(days => $3)
	         WHERE email_account_id = $1`, id, models.WarmupPoolFreeID, memberForDays)
}

func candidateByID(cands []models.WarmupPartnerCandidate, id uuid.UUID) (models.WarmupPartnerCandidate, bool) {
	for _, c := range cands {
		if c.ID == id {
			return c, true
		}
	}
	return models.WarmupPartnerCandidate{}, false
}

// A proven free mailbox may write back to the paying mailbox that wrote to it,
// and to nobody else in the paying tier (#633).
func TestLiveWarmupPartnerCandidatesReturnTheVisit(t *testing.T) {
	f := newPartnerOrgFixture(t)
	ctx := context.Background()
	repo := &warmupRepository{db: f.pool}

	// The sender is a free mailbox; the sibling and the outside partner stay
	// premium. Only the outside partner has written to the sender.
	f.moveToFree(t, f.sender, config.WarmupPoolFallbackMinAgeDays)
	f.received(t, f.sender, f.outside, time.Now().Add(-time.Hour))

	cands, err := repo.WarmupPartnerCandidates(ctx, "free", f.sender)
	if err != nil {
		t.Fatalf("WarmupPartnerCandidates: %v", err)
	}
	visit, ok := candidateByID(cands, f.outside)
	if !ok {
		t.Fatal("the paying mailbox that wrote to the sender is not offered back")
	}
	if visit.Origin != models.WarmupPartnerReturn || visit.PoolType != "premium" {
		t.Fatalf("return visit carries origin %q in pool %q, want return/premium", visit.Origin, visit.PoolType)
	}
	if _, ok := candidateByID(cands, f.sibling); ok {
		t.Fatal("a paying mailbox that never wrote to the sender is offered on the draw")
	}

	// The visit lapses with the window.
	f.exec(`UPDATE warmup_received SET created_at = NOW() - make_interval(days => $2)
	         WHERE email_account_id = $1`, f.sender, config.WarmupPoolReturnVisitDays+1)
	cands, err = repo.WarmupPartnerCandidates(ctx, "free", f.sender)
	if err != nil {
		t.Fatalf("WarmupPartnerCandidates: %v", err)
	}
	if _, ok := candidateByID(cands, f.outside); ok {
		t.Fatal("a visit older than the window is still returned")
	}
}

// The return is only open to a sender that is itself proven: on watch, or too
// new a member, it keeps warming in its own tier only.
func TestLiveWarmupPartnerCandidatesReturnNeedsAProvenSender(t *testing.T) {
	f := newPartnerOrgFixture(t)
	ctx := context.Background()
	repo := &warmupRepository{db: f.pool}
	f.received(t, f.sender, f.outside, time.Now().Add(-time.Hour))

	for _, tc := range []struct {
		name string
		prep func()
	}{
		{"a member for less than the minimum age", func() {
			f.moveToFree(t, f.sender, config.WarmupPoolFallbackMinAgeDays-1)
		}},
		{"on watch", func() {
			f.moveToFree(t, f.sender, config.WarmupPoolFallbackMinAgeDays)
			f.exec(`UPDATE warmup_pool_participants SET health_state = 'watch' WHERE email_account_id = $1`, f.sender)
		}},
		{"in a restricted workspace", func() {
			f.moveToFree(t, f.sender, config.WarmupPoolFallbackMinAgeDays)
			f.exec(`UPDATE warmup_pool_participants SET health_state = 'healthy' WHERE email_account_id = $1`, f.sender)
			f.exec(`UPDATE organizations SET risk_state = 'restricted' WHERE id = $1`, f.org)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.prep()
			cands, err := repo.WarmupPartnerCandidates(ctx, "free", f.sender)
			if err != nil {
				t.Fatalf("WarmupPartnerCandidates: %v", err)
			}
			if _, ok := candidateByID(cands, f.outside); ok {
				t.Fatal("an unproven free sender was offered a paying inbox")
			}
		})
	}
}

// What a candidate sent and received travels with it, and one at its inbound
// cap for the day is not offered to anyone.
func TestLiveWarmupPartnerCandidatesCarryReciprocityAndHonourTheCap(t *testing.T) {
	f := newPartnerOrgFixture(t)
	ctx := context.Background()
	repo := &warmupRepository{db: f.pool}

	// The outside partner sent three and heard back once.
	other := &partnerOrgFixture{pool: f.pool, sender: f.outside, exec: f.exec}
	for i := 0; i < 3; i++ {
		other.warmed(t, f.sibling)
	}
	f.received(t, f.outside, f.sibling, time.Now().Add(-2*time.Hour))
	t.Cleanup(func() {
		f.exec(`DELETE FROM warmup_tokens WHERE sender_account_id = $1`, f.outside)
		f.exec(`DELETE FROM tasks WHERE email_account_id = $1`, f.outside)
	})

	cands, err := repo.WarmupPartnerCandidates(ctx, "premium", f.sender)
	if err != nil {
		t.Fatalf("WarmupPartnerCandidates: %v", err)
	}
	c, ok := candidateByID(cands, f.outside)
	if !ok {
		t.Fatal("the outside partner is missing")
	}
	if c.Sent7d != 3 || c.Received7d != 1 {
		t.Fatalf("outside partner sent/received = %d/%d, want 3/1", c.Sent7d, c.Received7d)
	}
	if c.Origin != models.WarmupPartnerOwnTier || c.PoolType != "premium" {
		t.Fatalf("own-tier partner carries origin %q in pool %q", c.Origin, c.PoolType)
	}

	// Fill the sibling's inbound cap for today (it sends nothing, so the
	// floor); it drops out of every draw.
	capToday := config.WarmupInboundDailyFloor
	for i := 0; i < capToday; i++ {
		f.received(t, f.sibling, f.outside, time.Now().Add(-time.Minute))
	}
	cands, err = repo.WarmupPartnerCandidates(ctx, "premium", f.sender)
	if err != nil {
		t.Fatalf("WarmupPartnerCandidates: %v", err)
	}
	if _, ok := candidateByID(cands, f.sibling); ok {
		t.Fatalf("a mailbox that received %d today is still offered", capToday)
	}
	if _, ok := candidateByID(cands, f.outside); !ok {
		t.Fatal("the cap on one mailbox removed another")
	}

	// Yesterday's arrivals do not count against today.
	f.exec(`UPDATE warmup_received SET created_at = NOW() - interval '1 day' WHERE email_account_id = $1`, f.sibling)
	cands, err = repo.WarmupPartnerCandidates(ctx, "premium", f.sender)
	if err != nil {
		t.Fatalf("WarmupPartnerCandidates: %v", err)
	}
	if _, ok := candidateByID(cands, f.sibling); !ok {
		t.Fatal("yesterday's arrivals kept a mailbox out of today's draw")
	}

	// Mail dispatched to it today counts before it is verified, so senders
	// deciding at the same moment cannot all pick the one inbox.
	for i := 0; i < capToday; i++ {
		other.warmed(t, f.sibling)
	}
	cands, err = repo.WarmupPartnerCandidates(ctx, "premium", f.sender)
	if err != nil {
		t.Fatalf("WarmupPartnerCandidates: %v", err)
	}
	if _, ok := candidateByID(cands, f.sibling); ok {
		t.Fatalf("a mailbox with %d sends dispatched to it today is still offered", capToday)
	}
}

// The receiving side of the diversity read-out counts verified arrivals and
// the distinct partners they came from.
func TestLiveGetPartnerDiversityCountsArrivals(t *testing.T) {
	f := newPartnerOrgFixture(t)
	ctx := context.Background()
	repo := &warmupRepository{db: f.pool}
	since := time.Now().Add(-7 * 24 * time.Hour)

	f.received(t, f.sender, f.outside, time.Now().Add(-time.Hour))
	f.received(t, f.sender, f.outside, time.Now().Add(-2*time.Hour))
	f.received(t, f.sender, f.sibling, time.Now().Add(-3*time.Hour))
	f.received(t, f.sender, f.sibling, time.Now().Add(-9*24*time.Hour))

	d, err := repo.GetPartnerDiversity(ctx, f.sender, since)
	if err != nil {
		t.Fatalf("GetPartnerDiversity: %v", err)
	}
	if d.Received != 3 || d.Senders != 2 {
		t.Fatalf("received/senders = %d/%d, want 3/2", d.Received, d.Senders)
	}
}

// addMember adds an active mailbox to a pool in the given workspace, a member
// for memberForDays.
func (f *partnerOrgFixture) addMember(t *testing.T, org, poolID uuid.UUID, provider string, memberForDays int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	f.exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain,
	            signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	        VALUES ($1, $2, $3, $4, 'Pool member', '', '', $5, 'active', 50, 600, 'UTC')`,
		id, f.user, org, "m-"+id.String()[:8]+"@"+id.String()[:8]+".test", provider)
	f.exec(`INSERT INTO warmup_pool_participants (pool_id, email_account_id, participant_role, health_state, joined_at)
	        VALUES ($1, $2, 'sender_receiver', 'healthy', NOW() - make_interval(days => $3))`, poolID, id, memberForDays)
	return id
}

func borrowedCount(cands []models.WarmupPartnerCandidate) int {
	n := 0
	for _, c := range cands {
		if c.Borrowed() {
			n++
		}
	}
	return n
}

// Mail between one workspace's own mailboxes builds nothing, so a customer
// with more mailboxes than the floor still borrows outside partners.
func TestLiveWarmupPartnerCandidatesSiblingsDoNotSatisfyTheFloor(t *testing.T) {
	f := newPartnerOrgFixture(t)
	ctx := context.Background()
	repo := &warmupRepository{db: f.pool}
	for i := 0; i < config.WarmupPoolTierFallbackFloor; i++ {
		f.addMember(t, f.org, premiumPoolID, "smtp_imap", 0)
	}
	free := f.addMember(t, f.other, models.WarmupPoolFreeID, "smtp_imap", config.WarmupPoolFallbackMinAgeDays)
	f.exec(`UPDATE email_accounts SET warmup_max = $2 WHERE id = $1`, f.sender, config.WarmupPoolTierFallbackFloor)

	cands, err := repo.WarmupPartnerCandidates(ctx, "premium", f.sender)
	if err != nil {
		t.Fatalf("WarmupPartnerCandidates: %v", err)
	}
	c, ok := candidateByID(cands, free)
	if !ok || !c.Borrowed() {
		t.Fatalf("a premium sender whose partners are mostly its own siblings borrowed nothing (%d candidates)", len(cands))
	}
}

// A sender ramping above the floor needs that many partners, so its warmup
// max raises the floor.
func TestLiveWarmupPartnerCandidatesFloorFollowsTheWarmupMax(t *testing.T) {
	f := newPartnerOrgFixture(t)
	ctx := context.Background()
	repo := &warmupRepository{db: f.pool}
	for i := 0; i < config.WarmupPoolTierFallbackFloor; i++ {
		f.addMember(t, f.other, premiumPoolID, "smtp_imap", 0)
	}
	free := f.addMember(t, f.other, models.WarmupPoolFreeID, "smtp_imap", config.WarmupPoolFallbackMinAgeDays)

	f.exec(`UPDATE email_accounts SET warmup_max = $2 WHERE id = $1`, f.sender, config.WarmupPoolTierFallbackFloor)
	cands, err := repo.WarmupPartnerCandidates(ctx, "premium", f.sender)
	if err != nil {
		t.Fatalf("WarmupPartnerCandidates: %v", err)
	}
	if n := borrowedCount(cands); n != 0 {
		t.Fatalf("a sender with enough outside partners for its max borrowed %d", n)
	}

	f.exec(`UPDATE email_accounts SET warmup_max = $2 WHERE id = $1`, f.sender, config.WarmupPoolTierFallbackFloor+10)
	cands, err = repo.WarmupPartnerCandidates(ctx, "premium", f.sender)
	if err != nil {
		t.Fatalf("WarmupPartnerCandidates: %v", err)
	}
	if _, ok := candidateByID(cands, free); !ok {
		t.Fatal("a sender whose max exceeds its outside partners did not borrow")
	}
}

// When more free mailboxes qualify than are borrowed, the best go first.
func TestLiveWarmupPartnerCandidatesBorrowTheBestFirst(t *testing.T) {
	f := newPartnerOrgFixture(t)
	ctx := context.Background()
	repo := &warmupRepository{db: f.pool}
	for i := 0; i < config.WarmupPoolTierFallbackFloor+15; i++ {
		f.addMember(t, f.other, models.WarmupPoolFreeID, "smtp_imap", config.WarmupPoolFallbackMinAgeDays)
	}
	best := f.addMember(t, f.other, models.WarmupPoolFreeID, "gmail", config.WarmupPoolBorrowSeasonedDays)
	f.exec(`UPDATE email_accounts SET warmup_max = $2 WHERE id = $1`, f.sender, config.WarmupPoolTierFallbackFloor)

	for i := 0; i < 10; i++ {
		cands, err := repo.WarmupPartnerCandidates(ctx, "premium", f.sender)
		if err != nil {
			t.Fatalf("WarmupPartnerCandidates: %v", err)
		}
		if n := borrowedCount(cands); n != config.WarmupPoolTierFallbackFloor {
			t.Fatalf("borrowed %d, want %d", n, config.WarmupPoolTierFallbackFloor)
		}
		if _, ok := candidateByID(cands, best); !ok {
			t.Fatal("a seasoned Google mailbox was left out of the borrow for newer SMTP ones")
		}
	}
}

// A free sender leaves part of every inbox's day to premium senders.
func TestLiveWarmupPartnerCandidatesReserveInboxCapacityForPremium(t *testing.T) {
	f := newPartnerOrgFixture(t)
	ctx := context.Background()
	repo := &warmupRepository{db: f.pool}
	freeSender := f.addMember(t, f.org, models.WarmupPoolFreeID, "smtp_imap", config.WarmupPoolFallbackMinAgeDays)
	inbox := f.addMember(t, f.other, models.WarmupPoolFreeID, "smtp_imap", config.WarmupPoolFallbackMinAgeDays)

	freeShare := config.WarmupInboundDailyFloor * config.WarmupFreeInboundSharePercent / 100
	for i := 0; i < freeShare; i++ {
		f.received(t, inbox, freeSender, time.Now().Add(-time.Minute))
	}

	cands, err := repo.WarmupPartnerCandidates(ctx, "free", freeSender)
	if err != nil {
		t.Fatalf("WarmupPartnerCandidates: %v", err)
	}
	if _, ok := candidateByID(cands, inbox); ok {
		t.Fatalf("a free sender was offered an inbox that already received its free share of %d", freeShare)
	}
	cands, err = repo.WarmupPartnerCandidates(ctx, "premium", f.sender)
	if err != nil {
		t.Fatalf("WarmupPartnerCandidates: %v", err)
	}
	if _, ok := candidateByID(cands, inbox); !ok {
		t.Fatal("the capacity kept back from free senders is not open to a premium sender")
	}
}
