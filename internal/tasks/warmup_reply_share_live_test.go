package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestLiveWarmupReplyShareContinuesPastUnavailableCandidates(t *testing.T) {
	for _, reason := range []string{"pool eligibility", "routing exclusion", "missing content", "all unavailable"} {
		t.Run(reason, func(t *testing.T) {
			f := newPartnerRoutingFixture(t)
			ctx := t.Context()
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := f.pool.Exec(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				for _, query := range []string{
					`DELETE FROM warmup_received WHERE email_account_id IN(SELECT id FROM email_accounts WHERE organization_id=$1)`,
					`DELETE FROM warmup_tokens WHERE sender_account_id IN(SELECT id FROM email_accounts WHERE organization_id=$1)`,
					`DELETE FROM tasks WHERE email_account_id IN(SELECT id FROM email_accounts WHERE organization_id=$1)`,
				} {
					if _, err := f.pool.Exec(context.Background(), query, f.org); err != nil {
						t.Error(err)
					}
				}
			})
			f.svc.taskRepo = repository.NewTaskRepository(f.pool)
			f.svc.warmupHealth = warmupapp.NewService(f.svc.warmupRepo)
			share := f.svc.taskRepo.(repository.WarmupReplyShareRepository)
			lineage := f.svc.taskRepo.(repository.WarmupLineageRepository)
			sender, xerr := f.svc.emailRepo.GetByID(ctx, f.sender.ID)
			if xerr != nil {
				t.Fatal(xerr)
			}
			f.sender = *sender
			now := time.Now()
			f.sender.Warmup, f.sender.WarmupReplyRate = &now, 100
			exec(`UPDATE email_accounts SET mail_host='outlook' WHERE id=$1`, f.atWorkspace)
			conv := warmupConversations()[0]
			seed := func(sender uuid.UUID, age time.Duration) uuid.UUID {
				t.Helper()
				task, receipt := uuid.New(), uuid.New()
				messageID := task.String() + "@example.test"
				exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id) VALUES($1,'warmup',$2,'completed','')`, task, sender)
				exec(`INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns)
					VALUES($1,1,$2,$3,$4,$5)`, task, conv.Subject, conv.Version, generation.CanonicalRenderingVersion, len(conv.Messages)+1)
				exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,conversation_id,conversation_turn,sent_message_id,content_source)
					VALUES($1,$2,$3,$4,$5,0,$6,'static')`, uuid.New(), task, sender, f.sender.ID, conv.ID, "<"+messageID+">")
				exec(`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id,created_at)
					VALUES($1,$2,$3,$4,$5)`, f.sender.ID, receipt, sender, messageID, now.Add(-age))
				account, xerr := f.svc.emailRepo.GetByID(ctx, sender)
				if xerr != nil {
					t.Fatal(xerr)
				}
				if err := lineage.RecordVerifiedWarmupParent(ctx, task, f.sender.ID, receipt, "thread", repository.WarmupReceiptProof{SenderAddress: account.Email}); err != nil {
					t.Fatal(err)
				}
				return task
			}
			ordinary := seed(f.atSmallHost, 2*time.Hour)
			for range owedReplyCandidates {
				seed(f.atWorkspace, time.Hour)
			}
			pool := "free"
			switch reason {
			case "pool eligibility", "all unavailable":
				exec(`DELETE FROM warmup_pool_participants WHERE email_account_id=$1`, f.atWorkspace)
				if reason == "all unavailable" {
					exec(`DELETE FROM warmup_pool_participants WHERE email_account_id=$1`, f.atSmallHost)
				}
			case "routing exclusion":
				pool = "premium"
				exec(`UPDATE warmup_pool_participants SET pool_id=$2 WHERE email_account_id IN(SELECT id FROM email_accounts WHERE organization_id=$1)`, f.org, models.WarmupPoolPremiumID)
				f.svc.warmupRoutingRepo = ruleRepo{rules: excludeDomain("acme.test")}
			case "missing content":
				exec(`UPDATE warmup_tokens SET conversation_id=$2 WHERE sender_account_id=$1`, f.atWorkspace, uuid.New())
			}
			claimed := uuid.New()
			exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id) VALUES($1,'warmup',$2,'active','')`, claimed, f.sender.ID)
			warmupTask := &repository.WarmupTask{TaskID: claimed}
			if err := f.svc.taskRepo.CreateWarmupTask(ctx, warmupTask); err != nil {
				t.Fatal(err)
			}
			first, err := share.OwedWarmupReplies(ctx, f.sender.ID, owedReplyCandidates, repository.WarmupReplyExclusions{})
			if err != nil || len(first) != owedReplyCandidates || first[0].SenderID != f.atWorkspace || first[len(first)-1].SenderID != f.atWorkspace {
				t.Fatalf("fixture did not put unavailable recovery mail in the first batch: %+v %v", first, err)
			}
			if reason != "all unavailable" {
				next, err := share.OwedWarmupReplies(ctx, f.sender.ID, owedReplyCandidates, repository.WarmupReplyExclusions{Senders: []uuid.UUID{f.atWorkspace}})
				if err != nil || len(next) != 1 || next[0].ParentTaskID != ordinary {
					t.Fatalf("ordinary message missing after excluding unavailable sender: %+v %v", next, err)
				}
				if _, _, _, err := f.svc.exactReplyContent(ctx, &next[0].Parent, f.sender); err != nil {
					t.Fatalf("ordinary message cannot be rendered: %v", err)
				}
				if f.svc.replyPartner(ctx, f.atSmallHost, &f.sender, pool) == nil {
					t.Fatal("ordinary partner cannot receive")
				}
			}
			partner := f.svc.owedWarmupReply(ctx, claimed, &f.sender, pool)
			bound, err := f.svc.taskRepo.GetWarmupTask(ctx, claimed)
			if err != nil {
				t.Fatal(err)
			}
			if reason == "all unavailable" {
				if partner != nil || bound.ParentTaskID != nil {
					t.Fatalf("unavailable partners were answered: %+v %+v", partner, bound)
				}
			} else if partner == nil || partner.ID != f.atSmallHost || bound.ParentTaskID == nil || *bound.ParentTaskID != ordinary {
				t.Fatalf("usable ordinary mail beyond the first batch was not answered: %+v %+v", partner, bound)
			}
			var sends int
			if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE email_account_id=$1 AND task_type='warmup'`, f.sender.ID).Scan(&sends); err != nil || sends != 1 {
				t.Fatalf("reply selection changed the sender's planned volume: %d %v", sends, err)
			}
		})
	}
}
