package repository

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
)

const unreachableSilence = 72 * time.Hour

// silentMailbox is a transport fixture on a worker whose other mailbox synced recently, connected four days ago and
// failing to open its IMAP connection ever since.
func silentMailbox(t *testing.T) (transportFixture, uuid.UUID) {
	t.Helper()
	f := newTransportFixture(t)
	worker, peer := uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(t.Context(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO fleet_nodes(id,role,active,last_seen_at) VALUES($1,'worker',true,NOW())`, worker)
	exec(`INSERT INTO workers(id) VALUES($1)`, worker)
	exec(`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,worker_id)
	    SELECT $1,user_id,organization_id,$2,'Peer','','','smtp_imap',$3 FROM email_accounts WHERE id=$4`, peer, peer.String()+"@test.local", worker, f.account)
	exec(`INSERT INTO email_account_transport_recovery(email_account_id,operation,recovered_at) VALUES($1,'sync',NOW()-INTERVAL '10 minutes')`, peer)
	exec(`UPDATE email_accounts SET worker_id=$2, activated_at=NOW()-INTERVAL '4 days' WHERE id=$1`, f.account, worker)
	failure := errx.ErrMailServerUnreachableAt(errx.MailConnectionStageIMAPTLS, errors.New(errx.MailConnectionReasonTimeout))
	exec(`INSERT INTO email_account_errors(email_account_id,user_id,error_code,severity,title,message,operation,observation_source,created_at)
	    VALUES($1,$2,'SERVER_UNREACHABLE','WARNING','Connection Error',$3,'sync','worker',NOW()-INTERVAL '10 minutes')`, f.account, f.user, failure.Message)
	t.Cleanup(func() {
		for _, q := range []struct {
			sql string
			id  uuid.UUID
		}{
			{`UPDATE email_accounts SET worker_id=NULL WHERE id=$1`, f.account},
			{`DELETE FROM email_accounts WHERE id=$1`, peer},
			{`DELETE FROM workers WHERE id=$1`, worker},
			{`DELETE FROM fleet_nodes WHERE id=$1`, worker},
		} {
			if _, err := f.pool.Exec(context.Background(), q.sql, q.id); err != nil {
				t.Error(err)
			}
		}
	})
	return f, worker
}

func switchedOff(t *testing.T, f transportFixture) []UnreachableMailbox {
	t.Helper()
	off, xerr := f.repo.SwitchOffUnreachable(t.Context(), unreachableSilence, 10000)
	if xerr != nil {
		t.Fatal(xerr.Message)
	}
	return slices.DeleteFunc(off, func(m UnreachableMailbox) bool { return m.ID != f.account })
}

func (f transportFixture) status(t *testing.T) string {
	t.Helper()
	var status string
	if err := f.pool.QueryRow(t.Context(), `SELECT status FROM email_accounts WHERE id=$1`, f.account).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func openNotices(t *testing.T, f transportFixture) int {
	t.Helper()
	n := 0
	for _, e := range f.errors(t, true) {
		if e.ErrorCode == string(errx.MailErrorCodeMailboxUnreachable) {
			n++
		}
	}
	return n
}

func TestLiveMailTransportUnreachableMailboxIsSwitchedOffUntilSwitchedBackOn(t *testing.T) {
	f, worker := silentMailbox(t)

	off := switchedOff(t, f)
	if len(off) != 1 || off[0].WorkerID == nil || *off[0].WorkerID != worker || off[0].UserID != f.user {
		t.Fatalf("switched off %+v, want the silent mailbox on its worker", off)
	}
	if got := f.status(t); got != "inactive" {
		t.Fatalf("status %q, want inactive", got)
	}
	if n := openNotices(t, f); n != 1 {
		t.Fatalf("%d open switch-off notices, want 1", n)
	}
	if again := switchedOff(t, f); len(again) != 0 {
		t.Fatalf("second sweep switched off %+v again", again)
	}

	if _, err := f.pool.Exec(t.Context(), `UPDATE email_accounts SET status='active' WHERE id=$1`, f.account); err != nil {
		t.Fatal(err)
	}
	if n := openNotices(t, f); n != 0 {
		t.Fatalf("%d switch-off notices still open after switching the mailbox on", n)
	}
	if again := switchedOff(t, f); len(again) != 0 {
		t.Fatalf("a mailbox switched back on was switched off again at once: %+v", again)
	}
}

func TestLiveMailTransportUnreachableMailboxGuards(t *testing.T) {
	failingWith := func(stage, reason string) (string, []any) {
		return `UPDATE email_account_errors SET message=$2 WHERE email_account_id=$1`, []any{errx.ErrMailServerUnreachableAt(stage, errors.New(reason)).Message}
	}
	unreadable, unreadableArgs := `UPDATE email_account_errors SET message=$2 WHERE email_account_id=$1`, []any{errx.ErrMailServerUnreachable.Message}
	certificate, certificateArgs := failingWith(errx.MailConnectionStageIMAPTLS, errx.MailConnectionReasonCertificate)
	workerNetwork, workerNetworkArgs := failingWith(errx.MailConnectionStageIMAPDial, errx.MailConnectionReasonWorkerAddress)
	for _, tc := range []struct {
		name  string
		setup string
		args  []any
	}{
		{"a send succeeded inside the window", `INSERT INTO email_account_transport_recovery(email_account_id,operation,recovered_at) VALUES($1,'send',NOW()-INTERVAL '1 day')`, nil},
		{"switched on inside the window", `UPDATE email_accounts SET activated_at=NOW()-INTERVAL '1 day' WHERE id=$1`, nil},
		{"the server answered but its reply was unreadable", unreadable, unreadableArgs},
		{"a certificate the owner has to confirm", certificate, certificateArgs},
		{"the worker's own address is unavailable", workerNetwork, workerNetworkArgs},
		{"the failure stopped being observed", `UPDATE email_account_errors SET created_at=NOW()-INTERVAL '2 days' WHERE email_account_id=$1`, nil},
		{"no other mailbox on the worker is syncing", `DELETE FROM email_account_transport_recovery WHERE email_account_id IN (SELECT id FROM email_accounts WHERE worker_id=(SELECT worker_id FROM email_accounts WHERE id=$1) AND id<>$1)`, nil},
		{"an OAuth provider", `UPDATE email_accounts SET provider='outlook' WHERE id=$1`, nil},
		{"already off", `UPDATE email_accounts SET status='inactive' WHERE id=$1`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := silentMailbox(t)
			if _, err := f.pool.Exec(t.Context(), tc.setup, append([]any{f.account}, tc.args...)...); err != nil {
				t.Fatal(err)
			}
			before := f.status(t)
			if off := switchedOff(t, f); len(off) != 0 {
				t.Fatalf("switched off %+v", off)
			}
			if got := f.status(t); got != before {
				t.Fatalf("status moved from %q to %q", before, got)
			}
			if n := openNotices(t, f); n != 0 {
				t.Fatalf("%d switch-off notices recorded", n)
			}
		})
	}
}
