package orgtransfer

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestLiveImportWarmupStatisticsCannotGrantChargeRefundAuthority(t *testing.T) {
	svc, pool := liveImportService(t)
	dest := newTenantFixture(t, pool)
	ctx := t.Context()
	mailbox, task := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider) VALUES($1,$2,$3,$4,'Imported','','','smtp_imap')`, mailbox, dest.owner, dest.org, "import-"+mailbox.String()[:8]+"@example.test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM email_accounts WHERE id=$1`, mailbox) })
	day := time.Now().UTC().Format("2006-01-02")
	archive := tenantArchive(t, map[string][]map[string]any{
		"tasks":             {{"id": task, "email_account_id": mailbox, "task_type": "warmup", "status": "completed", "message_id": "", "completed_at": time.Now().UTC()}},
		"warmup_tasks":      {{"task_id": task, "warmup_charged_date": day, "warmup_reply_charged": true, "warmup_refunded_at": time.Now().UTC()}},
		"warmup_statistics": {{"email_account_id": mailbox, "date": day, "emails_sent": 3, "emails_replied": 2, "target_volume": 10}},
	})
	if _, err := svc.ImportFrom(ctx, dest.org, archive, ImportOptions{Conflict: models.OrgImportConflictOverwrite, ActorUserID: dest.owner}, nil); err != nil {
		t.Fatal(err)
	}
	var cleared bool
	if err := pool.QueryRow(ctx, `SELECT warmup_charged_date IS NULL AND warmup_reply_charged IS NULL AND warmup_refunded_at IS NULL FROM warmup_tasks WHERE task_id=$1`, task).Scan(&cleared); err != nil || !cleared {
		t.Fatalf("import retained source charge authority: cleared=%v error=%v", cleared, err)
	}
	if err := repository.NewWarmupRepository(pool).FailWarmupSend(ctx, mailbox, task, time.Now(), "Send failed", "Imported historical result"); err != nil {
		t.Fatal(err)
	}
	var sent, replies int
	if err := pool.QueryRow(ctx, `SELECT emails_sent,emails_replied FROM warmup_statistics WHERE email_account_id=$1 AND date=$2::date`, mailbox, day).Scan(&sent, &replies); err != nil || sent != 3 || replies != 2 {
		t.Fatalf("imported task refunded historical statistics: sent=%d replies=%d error=%v", sent, replies, err)
	}
}
