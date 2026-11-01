package repository

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLiveContentPoolRejectsDuplicateBatchContentUnderConcurrency(t *testing.T) {
	_, r, _, original := contentPoolFixture(t)
	base, err := r.GetConversation(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	subject := "A shared topic " + uuid.NewString()
	accepted := make(chan uuid.UUID, 12)
	var workers sync.WaitGroup
	for i := range 12 {
		workers.Go(func() {
			c := *base
			c.ID, c.Subject, c.Description = uuid.New(), subject, "A distinct opening "+uuid.NewString()
			if i%2 == 1 {
				c.Subject = "  " + strings.ToUpper(subject) + "  "
			}
			if err := r.InsertConversation(t.Context(), &c); err == nil {
				accepted <- c.ID
			} else if !errors.Is(err, ErrWarmupConversationDuplicate) {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	close(accepted)
	if len(accepted) != 1 {
		t.Fatalf("identical subjects admitted from concurrent batches: %d", len(accepted))
	}
	winner, err := r.GetConversation(t.Context(), <-accepted)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.InsertConversation(t.Context(), winner); err != nil {
		t.Fatalf("same-record retry lost idempotence: %v", err)
	}
	winner.ID, winner.Subject = uuid.New(), "Another topic "+uuid.NewString()
	winner.Description = "\n" + strings.ToUpper(winner.Description) + "\n"
	if err := r.InsertConversation(t.Context(), winner); !errors.Is(err, ErrWarmupConversationDuplicate) {
		t.Fatalf("reused opening admitted under another subject: %v", err)
	}
	if c, err := r.GetConversation(t.Context(), winner.ID); err != nil || c != nil {
		t.Fatalf("rejected content entered pool: %+v %v", c, err)
	}
}

func contentPoolFixture(t *testing.T) (*poolLinkFixture, *warmupContentRepository, string, uuid.UUID) {
	t.Helper()
	f, _ := lineageFixture(t)
	segment, conversation := "fixture-"+uuid.NewString(), uuid.New()
	t.Cleanup(func() { _, _ = f.pool.Exec(t.Context(), `DELETE FROM warmup_conversations WHERE segment=$1`, segment) })
	_, err := f.pool.Exec(t.Context(), `INSERT INTO warmup_conversations(id,pool_type,segment,source,subject,description,messages,status,scenario_version,rendering_version,reply_eligible,lint_passed,semantic_review) VALUES($1,'premium',$2,'ai','Paper notes again','I have gone back to paper notes for meetings.','["Reply","Closure"]','active','natural-v1','canonical-v1',true,true,'passed')`, conversation, segment)
	if err != nil {
		t.Fatal(err)
	}
	return f, NewWarmupContentRepository(f.pool).(*warmupContentRepository), segment, conversation
}

func contentPoolMailbox(t *testing.T, f *poolLinkFixture) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.pool.Exec(t.Context(), `INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,status,campaign_limit,min_wait_time,timezone,test_mode,warmup,warmup_days)
 VALUES($1,$2,$3,$4,'Pool','','','smtp_imap','active',50,600,'UTC',NULL,NOW(),127)`, id, f.user, f.org, id.String()+"@test.local")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func contentPoolPick(t *testing.T, f *poolLinkFixture, segment string, sender, recipient uuid.UUID) WarmupContentPick {
	t.Helper()
	task := uuid.New()
	_, err := f.pool.Exec(t.Context(), `INSERT INTO tasks(id,task_type,email_account_id,status,message_id) VALUES($1,'warmup',$2,'pending','')`, task, sender)
	if err != nil {
		t.Fatal(err)
	}
	return WarmupContentPick{Segment: segment, TaskID: task, SenderID: sender, RecipientID: recipient, IncludeAI: true}
}

func TestLiveContentPoolRetryReservesOnceAndReplenishesForExposedMailboxes(t *testing.T) {
	f, r, segment, conversation := contentPoolFixture(t)
	ctx := t.Context()
	if n, err := r.CountAvailableConversations(ctx, "premium", segment); err != nil || n != 1 {
		t.Fatalf("initial supply=%d err=%v", n, err)
	}
	pick := contentPoolPick(t, f, segment, f.sender, f.recipient)
	var successes atomic.Int32
	var workers sync.WaitGroup
	for range 12 {
		workers.Go(func() {
			c, err := r.PickConversation(ctx, pick)
			if err != nil || c == nil || c.ID != conversation {
				t.Errorf("retry=%+v err=%v", c, err)
				return
			}
			successes.Add(1)
		})
	}
	workers.Wait()
	if successes.Load() != 12 {
		t.Fatalf("successful retries=%d", successes.Load())
	}
	c, err := r.GetConversation(ctx, conversation)
	if err != nil || c == nil || c.UsageCount != 1 || !c.ReplyEligible {
		t.Fatalf("retry changed budget or replies: %+v %v", c, err)
	}
	if n, err := r.CountAvailableConversations(ctx, "premium", segment); err != nil || n != 0 {
		t.Fatalf("mailbox exhaustion hidden: %d %v", n, err)
	}
	if n, err := r.RefreshUnusedConversations(ctx, "premium", segment, 10); err != nil || n != 0 {
		t.Fatalf("ongoing thread retired: %d %v", n, err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM tasks WHERE id=$1`, pick.TaskID); err != nil {
		t.Fatal(err)
	}
	other := contentPoolMailbox(t, f)
	for _, pair := range [][2]uuid.UUID{{f.sender, other}, {other, f.recipient}, {f.recipient, f.sender}} {
		c, err := r.PickConversation(ctx, contentPoolPick(t, f, segment, pair[0], pair[1]))
		if err != nil || c == nil {
			t.Fatalf("an exhausted pair stopped receiving content: %+v %v", c, err)
		}
	}
}

func TestLiveContentPoolPreservesPlansForFutureDistinctMailboxes(t *testing.T) {
	f, r, segment, conversation := contentPoolFixture(t)
	for use := 1; use <= 12; use++ {
		sender, recipient := contentPoolMailbox(t, f), contentPoolMailbox(t, f)
		c, err := r.PickConversation(t.Context(), contentPoolPick(t, f, segment, sender, recipient))
		if err != nil || c == nil || c.ID != conversation || c.UsageCount != int64(use) {
			t.Fatalf("use %d: %+v %v", use, c, err)
		}
	}
	c, err := r.PickConversation(t.Context(), contentPoolPick(t, f, segment, f.sender, f.recipient))
	if err != nil || c == nil || c.ID != conversation || c.UsageCount != 13 {
		t.Fatalf("future distinct pair could not reuse saved plan: %+v %v", c, err)
	}
	if n, err := r.CountAvailableConversations(t.Context(), "premium", segment); err != nil || n != 0 {
		t.Fatalf("mailbox exhaustion hidden: %d %v", n, err)
	}
}

func TestLiveContentPoolConcurrentPairsAlwaysReceiveContent(t *testing.T) {
	f, r, segment, first := contentPoolFixture(t)
	for i := range 11 {
		contentPoolCopy(t, f, first, fmt.Sprintf("Topic %d", i))
	}
	picks := make([]WarmupContentPick, 12)
	for i := range picks {
		picks[i] = contentPoolPick(t, f, segment, f.sender, contentPoolMailbox(t, f))
	}
	var successes atomic.Int32
	var workers sync.WaitGroup
	for _, pick := range picks {
		workers.Go(func() {
			c, err := r.PickConversation(t.Context(), pick)
			if err != nil {
				t.Error(err)
			} else if c != nil {
				successes.Add(1)
			}
		})
	}
	workers.Wait()
	var reservations int
	if err := f.pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM warmup_conversation_mailboxes WHERE email_account_id=$1`, f.sender).Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if successes.Load() != 12 || reservations == 0 {
		t.Fatalf("concurrent sends went without content: successes=%d reservations=%d", successes.Load(), reservations)
	}
}

func TestLiveContentPoolPrefersLessUsedEligiblePlans(t *testing.T) {
	f, r, segment, first := contentPoolFixture(t)
	if c, err := r.PickConversation(t.Context(), contentPoolPick(t, f, segment, f.sender, f.recipient)); err != nil || c == nil {
		t.Fatalf("first reservation: %+v %v", c, err)
	}
	second := uuid.New()
	_, err := f.pool.Exec(t.Context(), `INSERT INTO warmup_conversations(id,pool_type,segment,source,subject,description,messages,status,scenario_version,rendering_version,reply_eligible,lint_passed,semantic_review)
 SELECT $2,pool_type,segment,source,'A different example',description,messages,status,scenario_version,rendering_version,reply_eligible,lint_passed,semantic_review FROM warmup_conversations WHERE id=$1`, first, second)
	if err != nil {
		t.Fatal(err)
	}
	sender, recipient := contentPoolMailbox(t, f), contentPoolMailbox(t, f)
	c, err := r.PickConversation(t.Context(), contentPoolPick(t, f, segment, sender, recipient))
	if err != nil || c == nil || c.ID != second {
		t.Fatalf("less-used plan was not preferred: %+v %v", c, err)
	}
}

func contentPoolCopy(t *testing.T, f *poolLinkFixture, from uuid.UUID, subject string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.pool.Exec(t.Context(), `INSERT INTO warmup_conversations(id,pool_type,segment,source,subject,description,messages,status,scenario_version,rendering_version,reply_eligible,lint_passed,semantic_review)
 SELECT $2,pool_type,segment,source,$3,description||' '||$3,messages,status,scenario_version,rendering_version,reply_eligible,lint_passed,semantic_review FROM warmup_conversations WHERE id=$1`, from, id, subject)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestLiveContentPoolPrefersWhatNeitherMailboxSawRecently(t *testing.T) {
	f, r, segment, first := contentPoolFixture(t)
	second := contentPoolCopy(t, f, first, "Desk plants")
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO warmup_conversation_mailboxes(conversation_id,email_account_id,created_at) VALUES($1,$2,NOW()-INTERVAL '1 day')`, first, f.sender); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]uuid.UUID{{f.sender, contentPoolMailbox(t, f)}, {contentPoolMailbox(t, f), f.sender}} {
		pick := contentPoolPick(t, f, segment, pair[0], pair[1])
		c, err := r.PickConversation(t.Context(), pick)
		if err != nil || c == nil || c.ID == first {
			t.Fatalf("a conversation seen yesterday was repeated while another was unseen: %+v %v", c, err)
		}
		if _, err := f.pool.Exec(t.Context(), `DELETE FROM warmup_conversation_mailboxes WHERE task_id=$1`, pick.TaskID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE warmup_conversation_mailboxes SET created_at=NOW()-INTERVAL '31 days' WHERE conversation_id=$1 AND email_account_id=$2`, first, f.sender); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO warmup_conversation_mailboxes(conversation_id,email_account_id) VALUES($1,$2)`, second, f.sender); err != nil {
		t.Fatal(err)
	}
	if c, err := r.PickConversation(t.Context(), contentPoolPick(t, f, segment, f.sender, contentPoolMailbox(t, f))); err != nil || c == nil || c.ID != first {
		t.Fatalf("a conversation outside the repeat window lost to one seen today: %+v %v", c, err)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE warmup_conversations SET scenario_version='diagnostic-v1' WHERE segment=$1`, segment); err != nil {
		t.Fatal(err)
	}
	if c, err := r.PickConversation(t.Context(), contentPoolPick(t, f, segment, contentPoolMailbox(t, f), contentPoolMailbox(t, f))); err != nil || (c != nil && c.Segment == segment) {
		t.Fatalf("retired content selected: %+v %v", c, err)
	}
}

func TestLiveContentPoolExhaustedPairRepeatsTheLeastRecentlySeen(t *testing.T) {
	f, r, segment, first := contentPoolFixture(t)
	second := contentPoolCopy(t, f, first, "Desk plants")
	_, err := f.pool.Exec(t.Context(), `INSERT INTO warmup_conversation_mailboxes(conversation_id,email_account_id,created_at) VALUES($1,$2,NOW()-INTERVAL '10 days'),($3,$4,NOW()-INTERVAL '1 day')`, first, f.sender, second, f.recipient)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := r.CountAvailableConversations(t.Context(), "premium", segment); err != nil || n != 0 {
		t.Fatalf("disjoint participant histories hid exhaustion: n=%d err=%v", n, err)
	}
	c, err := r.PickConversation(t.Context(), contentPoolPick(t, f, segment, f.sender, f.recipient))
	if err != nil || c == nil || (c.Segment == segment && c.ID != first) {
		t.Fatalf("exhausted pair did not repeat the least recently seen conversation: %+v %v", c, err)
	}
}

func TestLiveContentPoolTokenCleanupKeepsExposure(t *testing.T) {
	f, r, segment, first := contentPoolFixture(t)
	second := contentPoolCopy(t, f, first, "Desk plants")
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,subject,content_source,conversation_id) VALUES($1,$2,$3,$4,'Paper notes again','ai',$5)`, uuid.New(), f.task, f.sender, f.recipient, first); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(t.Context(), `DELETE FROM warmup_tokens WHERE task_id=$1`, f.task); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]uuid.UUID{{f.sender, contentPoolMailbox(t, f)}, {contentPoolMailbox(t, f), f.recipient}} {
		pick := contentPoolPick(t, f, segment, pair[0], pair[1])
		if c, err := r.PickConversation(t.Context(), pick); err != nil || c == nil || c.ID != second {
			t.Fatalf("exposure recorded through a deleted token was forgotten: %+v %v", c, err)
		}
		if _, err := f.pool.Exec(t.Context(), `DELETE FROM warmup_conversation_mailboxes WHERE task_id=$1`, pick.TaskID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLiveContentPoolStaticRowsOnlyFromTheCurrentLibrary(t *testing.T) {
	f, r, segment, first := contentPoolFixture(t)
	if _, err := f.pool.Exec(t.Context(), `UPDATE warmup_conversations SET source='static' WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	pick := contentPoolPick(t, f, segment, contentPoolMailbox(t, f), contentPoolMailbox(t, f))
	pick.IncludeAI = false
	if c, err := r.PickConversation(t.Context(), pick); err != nil || (c != nil && c.ID == first) {
		t.Fatalf("static row outside the library selected: %+v %v", c, err)
	}
	pick = contentPoolPick(t, f, segment, contentPoolMailbox(t, f), contentPoolMailbox(t, f))
	pick.IncludeAI, pick.StaticIDs = false, []uuid.UUID{first}
	if c, err := r.PickConversation(t.Context(), pick); err != nil || c == nil || c.ID != first {
		t.Fatalf("listed static row not selected: %+v %v", c, err)
	}
	if err := r.SeedStaticConversations(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestLiveContentPoolDemandCountsPlansRatherThanReplies(t *testing.T) {
	f, r, _, conversation := contentPoolFixture(t)
	// A separate time window isolates this fleet-wide aggregate from other fixtures.
	cutoff := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, id := range []any{conversation, conversation, nil} {
		_, err := f.pool.Exec(t.Context(), `INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,conversation_id,created_at) VALUES($1,$2,$3,$4,$5,$6)`, uuid.New(), f.task, f.sender, f.recipient, id, cutoff.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
	}
	if n, err := r.WarmupMailboxDemandSince(t.Context(), cutoff); err != nil || n != 1 {
		t.Fatalf("reply or unversioned token inflated plan demand: n=%d err=%v", n, err)
	}
}
