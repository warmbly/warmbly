package repository

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type transportFixture struct {
	db                  *db.DB
	pool                *pgxpool.Pool
	repo                EmailAccountErrorRepository
	account, user, task uuid.UUID
	generation          uuid.UUID
}

func newTransportFixture(t *testing.T) transportFixture {
	t.Helper()
	d, pool := liveContactDB(t)
	f := transportFixture{db: d, pool: pool, repo: NewEmailAccountErrorRepository(d), account: uuid.New(), user: uuid.New(), task: uuid.New()}
	org := uuid.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users(id,first_name,last_name,email,password_hash) VALUES($1,'Transport','Test',$2,'x')`, []any{f.user, org.String() + "@test.local"}},
		{`INSERT INTO organizations(id,name,slug,owner_user_id) VALUES($1,'Transport',$2,$3)`, []any{org, org.String(), f.user}},
		{`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider) VALUES($1,$2,$3,$4,'Transport','','','smtp_imap')`, []any{f.account, f.user, org, f.account.String() + "@test.local"}},
		{`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,scheduled_at,send_result_state) VALUES($1,'warmup',$2,'completed','',NOW(),'sent')`, []any{f.task, f.account}},
	} {
		if _, err := pool.Exec(t.Context(), q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, q := range []struct {
			sql string
			id  uuid.UUID
		}{
			{`DELETE FROM email_accounts WHERE id=$1`, f.account},
			{`DELETE FROM organizations WHERE id=$1`, org},
			{`DELETE FROM users WHERE id=$1`, f.user},
		} {
			if _, err := pool.Exec(context.Background(), q.sql, q.id); err != nil {
				t.Error(err)
			}
		}
	})
	if err := pool.QueryRow(t.Context(), `SELECT transport_generation FROM email_accounts WHERE id=$1`, f.account).Scan(&f.generation); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f transportFixture) report(operation models.MailTransportOperation, at *time.Time) *CreateEmailAccountError {
	d := &CreateEmailAccountError{TransportGeneration: f.generation, EmailAccountID: f.account, UserID: f.user, ErrorCode: "SERVER_UNREACHABLE", Severity: "WARNING", ResolveMethod: "RETRY", Title: "Transport", Message: "Transport report", Operation: operation, ObservedAt: at}
	if operation == models.MailTransportSend {
		d.TaskID = &f.task
	}
	return d
}

func (f transportFixture) errors(t *testing.T, unresolved bool) []EmailAccountError {
	t.Helper()
	rows, err := f.repo.GetByAccountID(t.Context(), f.account, unresolved)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestLiveMailTransportOnlyCurrentWorkerObservationUpdatesConnectionReason(t *testing.T) {
	for _, op := range []models.MailTransportOperation{models.MailTransportUnknown, models.MailTransportSync} {
		for _, tc := range []struct{ name, message string }{
			{"raw_cause", errx.ErrMailServerUnreachableAt("IMAP dial", errors.New("synthetic.invalid password=synthetic")).Message},
			{"unsupported_stage", errx.ErrMailServerUnreachableAt("IMAP unsupported", errors.New("connection refused")).Message},
			{"trailing_text", errx.ErrMailServerUnreachableAt("IMAP TLS", errors.New("TLS certificate validation failed")).Message + " synthetic-private"},
		} {
			t.Run(string(op)+"/"+tc.name, func(t *testing.T) {
				f := newTransportFixture(t)
				at := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
				report := func(message string) {
					t.Helper()
					d := f.report(op, &at)
					d.Message = message
					d.UserMessage = &d.Message
					if _, err := f.repo.CreateOnce(t.Context(), d); err != nil {
						t.Fatal(err)
					}
					at = at.Add(time.Minute)
				}
				assertMessage := func(want string) {
					t.Helper()
					rows := f.errors(t, true)
					if len(rows) != 1 || rows[0].Message != want || rows[0].UserMessage == nil || *rows[0].UserMessage != want {
						t.Fatal("malformed incoming message acquired precise authority")
					}
				}
				generic := errx.ErrMailServerUnreachable.Message
				report(generic)
				report(tc.message)
				assertMessage(generic)
				precise := errx.ErrMailServerUnreachableAt("IMAP dial", errors.New("connection refused")).Message
				report(precise)
				assertMessage(precise)
				report(tc.message)
				assertMessage(generic)
			})
		}
		t.Run(string(op)+"/malformed_existing", func(t *testing.T) {
			f := newTransportFixture(t)
			at := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
			malformed := errx.ErrMailServerUnreachableAt("IMAP old", errors.New("synthetic-private")).Message
			d := f.report(op, &at)
			d.Message = malformed
			d.UserMessage = &d.Message
			if _, err := f.repo.CreateOnce(t.Context(), d); err != nil {
				t.Fatal(err)
			}
			d.Message = errx.ErrMailServerUnreachable.Message
			if _, err := f.repo.CreateOnce(t.Context(), d); err != nil {
				t.Fatal(err)
			}
			if rows := f.errors(t, true); len(rows) != 1 || rows[0].Message != malformed || rows[0].UserMessage == nil || *rows[0].UserMessage != malformed {
				t.Fatal("same-time report rewrote historical message")
			}
			later := at.Add(time.Minute)
			d.ObservedAt = &later
			if _, err := f.repo.CreateOnce(t.Context(), d); err != nil {
				t.Fatal(err)
			}
			if rows := f.errors(t, true); len(rows) != 1 || rows[0].Message != errx.ErrMailServerUnreachable.Message || rows[0].UserMessage == nil || *rows[0].UserMessage != rows[0].Message || !rows[0].CreatedAt.Equal(later) {
				t.Fatal("malformed existing prefix gained a fresh diagnostic clock")
			}
		})
		for _, clock := range []string{"nil", "zero", "epoch", "negative", "future"} {
			t.Run(string(op)+"/first_insert_clock/"+clock, func(t *testing.T) {
				f := newTransportFixture(t)
				var observed *time.Time
				switch clock {
				case "zero":
					observed = new(time.Time)
				case "epoch", "negative", "future":
					value := time.Unix(0, 0).UTC()
					if clock == "negative" {
						value = time.Unix(-1, 0).UTC()
					} else if clock == "future" {
						value = time.Now().Add(time.Hour).UTC()
					}
					observed = &value
				}
				d := f.report(op, observed)
				d.Message = errx.ErrMailServerUnreachableAt(errx.MailConnectionStageIMAPDial, errors.New(errx.MailConnectionReasonRefused)).Message
				d.UserMessage = &d.Message
				before := time.Now().UTC().Truncate(time.Second)
				row, err := f.repo.CreateOnce(t.Context(), d)
				if err != nil || row == nil || row.Message != errx.ErrMailServerUnreachable.Message || row.UserMessage == nil || *row.UserMessage != row.Message || row.ObservationSource != "receipt" || row.Operation != op || row.CreatedAt.Before(before) {
					t.Fatal("first warning gave invalid-clock evidence a precise reason", err)
				}
			})
			t.Run(string(op)+"/invalid_clock/"+clock, func(t *testing.T) {
				f := newTransportFixture(t)
				at := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
				d := f.report(op, &at)
				d.Message = errx.ErrMailServerUnreachableAt(errx.MailConnectionStageIMAPDial, errors.New(errx.MailConnectionReasonRefused)).Message
				d.UserMessage = &d.Message
				if _, err := f.repo.CreateOnce(t.Context(), d); err != nil {
					t.Fatal(err)
				}
				var invalid *time.Time
				switch clock {
				case "zero":
					invalid = new(time.Time)
				case "epoch", "negative", "future":
					value := time.Unix(0, 0).UTC()
					if clock == "negative" {
						value = time.Unix(-1, 0).UTC()
					} else if clock == "future" {
						value = time.Now().Add(time.Hour).UTC()
					}
					invalid = &value
				}
				d.ObservedAt = invalid
				d.Message = errx.ErrMailServerUnreachableAt(errx.MailConnectionStageIMAPTLS, errors.New(errx.MailConnectionReasonCertificate)).Message
				before := time.Now().UTC().Truncate(time.Second)
				if _, err := f.repo.CreateOnce(t.Context(), d); err != nil {
					t.Fatal(err)
				}
				rows := f.errors(t, true)
				if len(rows) != 1 || rows[0].Message != errx.ErrMailServerUnreachable.Message || rows[0].UserMessage == nil || *rows[0].UserMessage != rows[0].Message || rows[0].ObservationSource != "receipt" || rows[0].ReceivedAt == nil || rows[0].CreatedAt.Before(before) {
					t.Fatal("invalid clock presented a specific reason as fresh worker evidence")
				}
			})
		}
		for _, legacy := range []string{"nil_user_message", "custom_user_message", "other_error_code", "unclassified_message"} {
			t.Run(string(op)+"/legacy_compatibility/"+legacy, func(t *testing.T) {
				f := newTransportFixture(t)
				at := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
				d := f.report(op, &at)
				d.TransportGeneration = uuid.Nil
				d.Message = errx.ErrMailServerUnreachableAt(errx.MailConnectionStageIMAPDial, errors.New(errx.MailConnectionReasonRefused)).Message
				custom := "Legacy user-facing warning"
				if legacy != "nil_user_message" {
					d.UserMessage = &custom
				}
				switch legacy {
				case "other_error_code":
					d.ErrorCode = "CONNECTION_LOST"
				case "unclassified_message":
					d.Message = "Legacy transport warning"
				}
				wantMessage := errx.ErrMailServerUnreachable.Message
				if legacy == "other_error_code" || legacy == "unclassified_message" {
					wantMessage = d.Message
				}
				row, err := f.repo.CreateOnce(t.Context(), d)
				if err != nil || row == nil || row.Message != wantMessage || !reflect.DeepEqual(row.UserMessage, d.UserMessage) || row.ErrorCode != d.ErrorCode || row.Operation != models.MailTransportUnknown {
					t.Fatal("classified first-insert guard changed unrelated legacy fields", err)
				}
				later := at.Add(time.Minute)
				d.ObservedAt, d.TransportGeneration = &later, f.generation
				if _, err := f.repo.CreateOnce(t.Context(), d); err != nil {
					t.Fatal(err)
				}
				if rows := f.errors(t, true); len(rows) == 0 || !reflect.DeepEqual(rows[0].UserMessage, d.UserMessage) {
					t.Fatal("classified refresh changed custom or absent legacy user message")
				}
			})
		}
		t.Run(string(op)+"/foreign_user", func(t *testing.T) {
			f := newTransportFixture(t)
			at := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
			d := f.report(op, &at)
			d.Message = errx.ErrMailServerUnreachableAt(errx.MailConnectionStageIMAPDial, errors.New(errx.MailConnectionReasonRefused)).Message
			if _, err := f.repo.CreateOnce(t.Context(), d); err != nil {
				t.Fatal(err)
			}
			before := f.errors(t, false)
			later := at.Add(time.Minute)
			d.ObservedAt, d.UserID = &later, uuid.New()
			d.Message = errx.ErrMailServerUnreachableAt(errx.MailConnectionStageIMAPTLS, errors.New(errx.MailConnectionReasonCertificate)).Message
			if row, err := f.repo.CreateOnce(t.Context(), d); err != nil || row != nil {
				t.Fatal("foreign user acquired mailbox warning authority", err)
			}
			if !reflect.DeepEqual(before, f.errors(t, false)) {
				t.Fatal("foreign user changed mailbox warning evidence")
			}
		})
		t.Run(string(op)+"/resolved_row", func(t *testing.T) {
			f := newTransportFixture(t)
			at := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
			d := f.report(op, &at)
			old, err := f.repo.CreateOnce(t.Context(), d)
			if err != nil || old == nil {
				t.Fatal("initial warning missing", err)
			}
			if err := f.repo.Resolve(t.Context(), old.ID, "local test"); err != nil {
				t.Fatal(err)
			}
			before := f.errors(t, false)
			later := at.Add(time.Minute)
			d.ObservedAt = &later
			d.Message = errx.ErrMailServerUnreachableAt(errx.MailConnectionStageIMAPTLS, errors.New(errx.MailConnectionReasonCertificate)).Message
			fresh, err := f.repo.CreateOnce(t.Context(), d)
			if err != nil || fresh == nil || fresh.ID == old.ID || fresh.Message != d.Message || !fresh.CreatedAt.Equal(later) || fresh.ResolvedAt != nil {
				t.Fatal("fresh warning reused resolved history", err)
			}
			rows := f.errors(t, false)
			if len(rows) != 2 || !reflect.DeepEqual(rows[1], before[0]) || len(f.errors(t, true)) != 1 {
				t.Fatal("fresh warning changed resolved history")
			}
		})
		for _, generation := range []string{"missing", "mismatched"} {
			t.Run(string(op)+"/generation/"+generation, func(t *testing.T) {
				f := newTransportFixture(t)
				at := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
				d := f.report(op, &at)
				d.Message = errx.ErrMailServerUnreachableAt(errx.MailConnectionStageIMAPDial, errors.New(errx.MailConnectionReasonRefused)).Message
				if _, err := f.repo.CreateOnce(t.Context(), d); err != nil {
					t.Fatal(err)
				}
				before := f.errors(t, true)
				later := at.Add(time.Minute)
				d.ObservedAt = &later
				d.TransportGeneration = uuid.Nil
				if generation == "mismatched" {
					d.TransportGeneration = uuid.New()
				}
				d.Message = errx.ErrMailServerUnreachableAt(errx.MailConnectionStageIMAPTLS, errors.New(errx.MailConnectionReasonCertificate)).Message
				if _, err := f.repo.CreateOnce(t.Context(), d); err != nil {
					t.Fatal(err)
				}
				rows := f.errors(t, true)
				wantCount := 1
				if op == models.MailTransportSync {
					wantCount = 2
				}
				if len(rows) != wantCount || rows[0].Operation != models.MailTransportUnknown || rows[0].Message != errx.ErrMailServerUnreachable.Message || !rows[0].CreatedAt.Equal(later) {
					t.Fatal("unverified generation presented a specific current connection reason")
				}
				if op == models.MailTransportSync && !reflect.DeepEqual(rows[1], before[0]) {
					t.Fatal("ambiguous generation changed current sync warning authority")
				}
			})
		}
	}
	for _, op := range []models.MailTransportOperation{models.MailTransportUnknown, models.MailTransportSync} {
		t.Run(string(op), func(t *testing.T) {
			f := newTransportFixture(t)
			at := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
			report := func(generation uuid.UUID, observed *time.Time, message string) {
				t.Helper()
				data := f.report(op, observed)
				data.TransportGeneration = generation
				data.Message = message
				if _, err := f.repo.CreateOnce(t.Context(), data); err != nil {
					t.Fatal(err)
				}
			}
			assertCurrent := func(want string, when time.Time) {
				t.Helper()
				var count int
				var actual string
				var stored time.Time
				if err := f.pool.QueryRow(t.Context(),
					`SELECT count(*) OVER(),message,created_at FROM email_account_errors
					 WHERE email_account_id=$1 AND error_code='SERVER_UNREACHABLE'
					 AND operation=$2 AND resolved_at IS NULL`, f.account, op,
				).Scan(&count, &actual, &stored); err != nil || count != 1 || actual != want || !stored.Equal(when) {
					t.Fatalf("current warning: count=%d message=%q time=%s want=%q at=%s err=%v", count, actual, stored, want, when, err)
				}
			}

			generic := errx.ErrMailServerUnreachable.Message
			report(f.generation, &at, generic)
			fresh := at.Add(time.Minute)
			classified := errx.ErrMailServerUnreachableAt("IMAP dial", errors.New("connection refused")).Message
			report(f.generation, &fresh, classified)
			assertCurrent(classified, fresh)
			report(f.generation, &at, "stale source message")
			report(f.generation, &fresh, "same-second ambiguous message")
			assertCurrent(classified, fresh)

			later := fresh.Add(time.Minute)
			report(f.generation, &later, "provider response includes private detail")
			assertCurrent(generic, later)
			newer := later.Add(time.Minute)
			classified = errx.ErrMailServerUnreachableAt("IMAP TLS", errors.New("TLS certificate validation failed")).Message
			report(f.generation, &newer, classified)
			assertCurrent(classified, newer)
			if op == models.MailTransportUnknown {
				oldGeneration := newer.Add(time.Minute)
				report(uuid.New(), &oldGeneration, "older restored mailbox details")
				assertCurrent(generic, oldGeneration)
				rows := f.errors(t, true)
				if len(rows) != 1 || rows[0].Operation != models.MailTransportUnknown {
					t.Fatalf("ambiguous report changed warning scope: %+v", rows)
				}
			} else {
				unverified := time.Now().Add(time.Hour)
				report(f.generation, &unverified, "future timestamp is not a worker observation")
				var message, source string
				if err := f.pool.QueryRow(t.Context(),
					`SELECT message,observation_source FROM email_account_errors
					 WHERE email_account_id=$1 AND operation=$2 AND resolved_at IS NULL`, f.account, op,
				).Scan(&message, &source); err != nil || message != generic || source != "receipt" {
					t.Fatalf("unverified timestamp presented as fresh IMAP cause: %q source=%q err=%v", message, source, err)
				}
			}
		})
	}
}

func TestLiveMailTransportOperationIsolationAndUnknownHold(t *testing.T) {
	f := newTransportFixture(t)
	old := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	for _, operation := range []models.MailTransportOperation{models.MailTransportUnknown, models.MailTransportSync, models.MailTransportSend} {
		if row, err := f.repo.CreateOnce(t.Context(), f.report(operation, &old)); err != nil || row == nil {
			t.Fatalf("%s: %v %v", operation, row, err)
		}
	}
	if rows := f.errors(t, true); len(rows) != 3 {
		t.Fatalf("operation classes merged: %+v", rows)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, f.account, f.task); err != nil {
		t.Fatal(err)
	}
	at := old.Add(time.Minute)
	if err := f.repo.RecordTransportSuccess(t.Context(), f.account, models.MailTransportSync, at, nil, f.generation); err != nil {
		t.Fatal(err)
	}
	if rows := f.errors(t, true); len(rows) != 2 {
		t.Fatalf("sync did not isolate scope: %+v", rows)
	} else {
		for _, row := range rows {
			if row.Operation == models.MailTransportSync {
				t.Fatal("sync remained unresolved")
			}
		}
	}
	// A completed task is not provider confirmation.
	if _, err := f.pool.Exec(t.Context(), `UPDATE tasks SET send_result_state=NULL WHERE id=$1`, f.task); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.RecordTransportSuccess(t.Context(), f.account, models.MailTransportSend, at, &f.task, f.generation); err != nil {
		t.Fatal(err)
	}
	if len(f.errors(t, true)) != 2 {
		t.Fatal("completion alone cleared a send error")
	}
	foreign := uuid.New()
	if err := f.repo.RecordTransportSuccess(t.Context(), f.account, models.MailTransportSend, at, &foreign, f.generation); err != nil {
		t.Fatal(err)
	}
	if len(f.errors(t, true)) != 2 {
		t.Fatal("foreign task cleared a send error")
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE tasks SET send_result_state='sent' WHERE id=$1`, f.task); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.RecordTransportSuccess(t.Context(), f.account, models.MailTransportSend, at, &f.task, f.generation); err != nil {
		t.Fatal(err)
	}
	rows := f.errors(t, true)
	if len(rows) != 0 {
		t.Fatalf("success from both transports left an old error unresolved: %+v", rows)
	}
	var held bool
	if err := f.pool.QueryRow(t.Context(), `SELECT send_recovery_hold FROM email_accounts WHERE id=$1`, f.account).Scan(&held); err != nil || !held {
		t.Fatalf("report recovery cleared send hold: %v %v", held, err)
	}
	if len(f.errors(t, false)) != 3 {
		t.Fatal("recovery deleted historical rows")
	}
	batch, xerr := f.repo.GetByAccountIDs(t.Context(), []uuid.UUID{f.account}, false)
	if xerr != nil || len(batch[f.account]) != 3 {
		t.Fatal("batched report model lost history", xerr)
	}
	byUser, xerr := f.repo.GetByUserID(t.Context(), f.user, 100)
	if xerr != nil || len(byUser) != 3 {
		t.Fatal("user report model lost history", xerr)
	}
}

func TestLiveMailTransportOutOfOrderFailureStaysHistorical(t *testing.T) {
	f := newTransportFixture(t)
	old := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	success := old.Add(time.Minute)
	for _, operation := range []models.MailTransportOperation{models.MailTransportSync, models.MailTransportSend} {
		var taskID *uuid.UUID
		if operation == models.MailTransportSend {
			taskID = &f.task
		}
		if err := f.repo.RecordTransportSuccess(t.Context(), f.account, operation, success, taskID, f.generation); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if row, err := f.repo.CreateOnce(t.Context(), f.report(operation, &old)); err != nil || row != nil {
				t.Fatalf("old failure notified/reopened outage: %v %v", row, err)
			}
		}
	}
	if len(f.errors(t, true)) != 0 || len(f.errors(t, false)) != 2 {
		t.Fatal("historical failure was lost, duplicated or reopened")
	}
	newer := success.Add(time.Minute)
	row, err := f.repo.CreateOnce(t.Context(), f.report(models.MailTransportSync, &newer))
	if err != nil || row == nil {
		t.Fatal("fresh failure was swallowed", err)
	}
	if err := f.repo.RecordTransportSuccess(t.Context(), f.account, models.MailTransportSync, old, nil, f.generation); err != nil {
		t.Fatal(err)
	}
	if len(f.errors(t, true)) != 1 {
		t.Fatal("old success cleared newer failure")
	}
	if row, err := f.repo.CreateOnce(t.Context(), f.report(models.MailTransportSync, &old)); err != nil || row != nil {
		t.Fatal("old redelivery created a new warning", err)
	}
	if rows := f.errors(t, true); len(rows) != 1 || !rows[0].CreatedAt.Equal(newer) {
		t.Fatalf("older error moved latest clock: %+v", rows)
	}
	// Worker failures have second precision; this ambiguous bucket stays held.
	if err := f.repo.RecordTransportSuccess(t.Context(), f.account, models.MailTransportSync, newer.Add(500*time.Millisecond), nil, f.generation); err != nil {
		t.Fatal(err)
	}
	if len(f.errors(t, true)) != 1 {
		t.Fatal("same-second success claimed temporal authority")
	}
	if err := f.repo.RecordTransportSuccess(t.Context(), f.account, models.MailTransportSync, newer.Add(2*time.Second), nil, f.generation); err != nil {
		t.Fatal(err)
	}
	if len(f.errors(t, true)) != 0 {
		t.Fatal("newer proven success did not recover")
	}
	if row, err := f.repo.CreateOnce(t.Context(), f.report(models.MailTransportUnknown, &old)); err != nil || row != nil {
		t.Fatal("success from both transports left a delayed unclassified failure active", err)
	}
}

func TestLiveMailTransportClockFallbackAndConcurrentRecovery(t *testing.T) {
	f := newTransportFixture(t)
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			at := base.Add(time.Duration(i) * time.Minute)
			if _, err := f.repo.CreateOnce(t.Context(), f.report(models.MailTransportSync, &at)); err != nil {
				t.Error(err)
			}
			if err := f.repo.RecordTransportSuccess(t.Context(), f.account, models.MailTransportSync, base.Add(20*time.Minute), nil, f.generation); err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(f.errors(t, true)) != 0 {
		t.Fatal("concurrent error/recovery left a false outage")
	}
	var recovered time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT recovered_at FROM email_account_transport_recovery WHERE email_account_id=$1 AND operation='sync'`, f.account).Scan(&recovered); err != nil || !recovered.Equal(base.Add(20*time.Minute)) {
		t.Fatalf("success watermark regressed: %v %v", recovered, err)
	}
	zero := time.Time{}
	future := time.Now().Add(time.Hour)
	negative := time.Unix(-1, 0)
	for _, invalid := range []*time.Time{nil, &zero, &future, &negative} {
		before := time.Now()
		if _, err := f.repo.CreateOnce(t.Context(), f.report(models.MailTransportSync, invalid)); err != nil {
			t.Fatal(err)
		}
		rows := f.errors(t, true)
		if len(rows) != 1 || rows[0].CreatedAt.Before(before) || rows[0].ObservationSource != "receipt" || rows[0].ReceivedAt == nil {
			t.Fatalf("invalid worker time was not receipt-scoped: %+v", rows)
		}
	}
	current := f.errors(t, true)[0]
	if _, err := f.repo.CreateOnce(t.Context(), f.report(models.MailTransportSync, &base)); err != nil {
		t.Fatal(err)
	}
	if row := f.errors(t, true)[0]; !row.CreatedAt.Equal(current.CreatedAt) || row.ObservationSource != "receipt" {
		t.Fatalf("older delivery changed latest provenance: %+v", row)
	}
	for _, at := range []time.Time{zero, future, negative} {
		if err := f.repo.RecordTransportSuccess(t.Context(), f.account, models.MailTransportSync, at, nil, f.generation); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.errors(t, true)) != 1 {
		t.Fatal("invalid success clock cleared receipt-scoped failure")
	}
}

func TestLiveMailTransportRecoveryJoinsResultTransaction(t *testing.T) {
	f := newTransportFixture(t)
	old := time.Now().Add(-time.Hour)
	if _, err := f.repo.CreateOnce(t.Context(), f.report(models.MailTransportSend, &old)); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	ctx := context.WithValue(t.Context(), sendResultKey{}, sendResultContext{tx: tx, taskID: f.task})
	if err := f.repo.RecordTransportSuccess(ctx, f.account, models.MailTransportSend, old.Add(time.Minute), &f.task, f.generation); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.errors(t, true)) != 1 {
		t.Fatal("recovery escaped failed send-result transaction")
	}
	var n int
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM email_account_transport_recovery WHERE email_account_id=$1`, f.account).Scan(&n); err != nil || n != 0 {
		t.Fatal("recovery watermark survived rollback", err)
	}
}

func TestLiveMailTransportSyncTimestampIsMonotonic(t *testing.T) {
	f := newTransportFixture(t)
	repo := NewEmailSyncStateRepository(f.db)
	newer := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
	older := newer.Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	zero := time.Time{}
	for _, at := range []*time.Time{&newer, &older, nil, &future, &zero} {
		if err := repo.Put(t.Context(), f.user, f.account, &models.SyncState{LastSyncedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	state, err := repo.Get(t.Context(), f.account)
	if err != nil || state.LastSyncedAt == nil || !state.LastSyncedAt.Equal(newer) {
		t.Fatalf("older redelivery regressed monitoring clock: %+v %v", state, err)
	}
	var accountAt time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT last_synced_at FROM email_accounts WHERE id=$1`, f.account).Scan(&accountAt); err != nil || !accountAt.Equal(newer) {
		t.Fatalf("account/table clocks diverged: %v %v", accountAt, err)
	}
}

func TestLiveMailTransportSyncFolderCoveragePersistsAndClears(t *testing.T) {
	f := newTransportFixture(t)
	repo := NewEmailSyncStateRepository(f.db)
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO email_sync_state(email_id,user_id) VALUES($1,$2)`, f.account, f.user); err != nil {
		t.Fatal(err)
	}
	assertCoverage := func(capCount, conflicts int) {
		t.Helper()
		state, err := repo.Get(t.Context(), f.account)
		if err != nil {
			t.Fatal(err)
		}
		if state == nil || state.FoldersSkippedCap != capCount || state.FoldersSkippedConflict != conflicts || state.LastSyncedAt != nil {
			t.Fatalf("unexpected folder coverage or successful sync: %+v", state)
		}
	}
	assertCoverage(0, 0)
	if _, err := f.pool.Exec(t.Context(), `DELETE FROM email_sync_state WHERE email_id=$1`, f.account); err != nil {
		t.Fatal(err)
	}
	if err := repo.Put(t.Context(), f.user, f.account, &models.SyncState{FoldersSkippedCap: 17, FoldersSkippedConflict: 2}); err != nil {
		t.Fatal(err)
	}
	assertCoverage(17, 2)
	if err := repo.Put(t.Context(), f.user, f.account, &models.SyncState{Deferred: 1, FoldersSkippedCap: 17, FoldersSkippedConflict: 2}); err != nil {
		t.Fatal(err)
	}
	assertCoverage(17, 2)
	if err := repo.Put(t.Context(), f.user, f.account, &models.SyncState{}); err != nil {
		t.Fatal(err)
	}
	assertCoverage(0, 0)
}

func TestLiveMailTransportSyncFolderCoverageRejectsOlderCompletion(t *testing.T) {
	f := newTransportFixture(t)
	repo := NewEmailSyncStateRepository(f.db)
	completed := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	older := completed.Add(-time.Minute)
	for _, state := range []*models.SyncState{
		{LastSyncedAt: &completed, FoldersSkippedCap: 17, FoldersSkippedConflict: 2},
		{LastSyncedAt: &older},
	} {
		if err := repo.Put(t.Context(), f.user, f.account, state); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.Get(t.Context(), f.account)
	if err != nil {
		t.Fatal(err)
	}
	if got.FoldersSkippedCap != 17 || got.FoldersSkippedConflict != 2 || got.LastSyncedAt == nil || !got.LastSyncedAt.Equal(completed) {
		t.Fatalf("older completion replaced current coverage: %+v", got)
	}
	if err := repo.Put(t.Context(), f.user, f.account, &models.SyncState{FoldersSkippedCap: 4, FoldersSkippedConflict: 1}); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Get(t.Context(), f.account)
	if err != nil {
		t.Fatal(err)
	}
	if got.FoldersSkippedCap != 4 || got.FoldersSkippedConflict != 1 {
		t.Fatalf("progress counters: %+v", got)
	}
	newer := completed.Add(time.Minute)
	if err := repo.Put(t.Context(), f.user, f.account, &models.SyncState{LastSyncedAt: &newer}); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Get(t.Context(), f.account)
	if err != nil {
		t.Fatal(err)
	}
	if got.FoldersSkippedCap != 0 || got.FoldersSkippedConflict != 0 {
		t.Fatalf("new completion counters: %+v", got)
	}
}

func TestLiveMailTransportSyncFolderCoverageOrdersProgressObservations(t *testing.T) {
	f := newTransportFixture(t)
	repo := NewEmailSyncStateRepository(f.db)
	completed := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	first := completed.Add(time.Minute)
	latest := first.Add(time.Minute)
	put := func(state models.SyncState) {
		t.Helper()
		if err := repo.Put(t.Context(), f.user, f.account, &state); err != nil {
			t.Fatal(err)
		}
	}
	assertCoverage := func(capCount, conflicts int, observed *time.Time) {
		t.Helper()
		got, err := repo.Get(t.Context(), f.account)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || got.FoldersSkippedCap != capCount || got.FoldersSkippedConflict != conflicts {
			t.Fatalf("coverage: %+v", got)
		}
		if observed == nil {
			if got.FolderCoverageObservedAt != nil {
				t.Fatalf("legacy observation manufactured: %+v", got)
			}
		} else if got.FolderCoverageObservedAt == nil || !got.FolderCoverageObservedAt.Equal(*observed) {
			t.Fatalf("observation: %+v", got)
		}
	}
	put(models.SyncState{LastSyncedAt: &completed, FolderCoverageObservedAt: &first, FoldersSkippedCap: 17, FoldersSkippedConflict: 2})
	put(models.SyncState{LastSyncedAt: &completed, FolderCoverageObservedAt: &latest})
	put(models.SyncState{LastSyncedAt: &completed, FolderCoverageObservedAt: &first, FoldersSkippedCap: 17, FoldersSkippedConflict: 2})
	assertCoverage(0, 0, &latest)
	put(models.SyncState{FolderCoverageObservedAt: &first, FoldersSkippedCap: 17, FoldersSkippedConflict: 2})
	assertCoverage(0, 0, &latest)
	put(models.SyncState{LastSyncedAt: &completed, FolderCoverageObservedAt: &latest, FoldersSkippedCap: 17, FoldersSkippedConflict: 2})
	assertCoverage(0, 0, &latest)
	put(models.SyncState{LastSyncedAt: &completed, FoldersSkippedCap: 17, FoldersSkippedConflict: 2})
	put(models.SyncState{FoldersSkippedCap: 17, FoldersSkippedConflict: 2})
	assertCoverage(0, 0, &latest)
	delayed := latest.Add(time.Minute)
	completed = delayed.Add(time.Minute)
	put(models.SyncState{LastSyncedAt: &completed, FolderCoverageObservedAt: &latest})
	put(models.SyncState{LastSyncedAt: &delayed, FoldersSkippedCap: 17, FoldersSkippedConflict: 2})
	assertCoverage(0, 0, &latest)
	got, err := repo.Get(t.Context(), f.account)
	if err != nil || got.LastSyncedAt == nil || !got.LastSyncedAt.Equal(completed) {
		t.Fatalf("latest completion: %+v %v", got, err)
	}
	newer := completed.Add(time.Minute)
	put(models.SyncState{LastSyncedAt: &newer, FoldersSkippedCap: 4, FoldersSkippedConflict: 1})
	assertCoverage(4, 1, nil)
	put(models.SyncState{LastSyncedAt: &completed, FolderCoverageObservedAt: &latest})
	assertCoverage(4, 1, nil)
	put(models.SyncState{FoldersSkippedCap: 3})
	assertCoverage(3, 0, nil)
}

func TestLiveMailTransportSyncFolderCoverageRejectsInvalidObservation(t *testing.T) {
	for _, invalid := range []time.Time{time.Now().Add(time.Hour), {}, time.Unix(0, 0)} {
		t.Run(invalid.String(), func(t *testing.T) {
			f := newTransportFixture(t)
			repo := NewEmailSyncStateRepository(f.db)
			completed := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
			first := completed.Add(time.Minute)
			latest := first.Add(time.Minute)
			bad := models.SyncState{LastSyncedAt: &completed, FolderCoverageObservedAt: &invalid, FoldersSkippedCap: 17, FoldersSkippedConflict: 2}
			put := func(state models.SyncState) {
				t.Helper()
				if err := repo.Put(t.Context(), f.user, f.account, &state); err != nil {
					t.Fatal(err)
				}
			}
			assertCoverage := func(capCount int, observed *time.Time) {
				t.Helper()
				got, err := repo.Get(t.Context(), f.account)
				if err != nil {
					t.Fatal(err)
				}
				if got == nil || got.FoldersSkippedCap != capCount || got.FoldersSkippedConflict != 0 || got.LastSyncedAt == nil || !got.LastSyncedAt.Equal(completed) {
					t.Fatalf("coverage or completion: %+v", got)
				}
				if (observed == nil) != (got.FolderCoverageObservedAt == nil) || (observed != nil && !got.FolderCoverageObservedAt.Equal(*observed)) {
					t.Fatalf("observation: %+v; want %v", got, observed)
				}
			}
			put(bad)
			assertCoverage(0, nil)
			put(models.SyncState{LastSyncedAt: &completed, FolderCoverageObservedAt: &first, FoldersSkippedCap: 4})
			put(bad)
			assertCoverage(4, &first)
			put(models.SyncState{LastSyncedAt: &completed, FolderCoverageObservedAt: &latest, FoldersSkippedCap: 3})
			put(bad)
			assertCoverage(3, &latest)
		})
	}
}

func TestLiveMailTransportSyncFolderCoverageAllowsBoundedClockSkew(t *testing.T) {
	f := newTransportFixture(t)
	repo := NewEmailSyncStateRepository(f.db)
	var observed time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT now() + interval '4 seconds'`).Scan(&observed); err != nil {
		t.Fatal(err)
	}
	completed := observed.Add(-time.Hour)
	first := models.SyncState{LastSyncedAt: &completed, FolderCoverageObservedAt: &observed, FoldersSkippedCap: 4}
	if err := repo.Put(t.Context(), f.user, f.account, &first); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(t.Context(), f.account)
	if err != nil {
		t.Fatal(err)
	}
	if got.FoldersSkippedCap != 4 || got.FolderCoverageObservedAt == nil || !got.FolderCoverageObservedAt.Equal(observed) {
		t.Fatalf("bounded lead was dropped or restamped: %+v", got)
	}
	latest := observed.Add(time.Millisecond)
	if err := repo.Put(t.Context(), f.user, f.account, &models.SyncState{LastSyncedAt: &completed, FolderCoverageObservedAt: &latest, FoldersSkippedCap: 3}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Put(t.Context(), f.user, f.account, &first); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Get(t.Context(), f.account)
	if err != nil {
		t.Fatal(err)
	}
	if got.FoldersSkippedCap != 3 || got.FolderCoverageObservedAt == nil || !got.FolderCoverageObservedAt.Equal(latest) || got.LastSyncedAt == nil || !got.LastSyncedAt.Equal(completed) {
		t.Fatalf("replay ordering or successful-sync clock changed: %+v", got)
	}
}

func TestLiveMailTransportSyncFolderCoverageRequiresCurrentWorker(t *testing.T) {
	f := newTransportFixture(t)
	repo := NewEmailSyncStateRepository(f.db)
	oldWorker, currentWorker := uuid.New(), uuid.New()
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO fleet_nodes(id,role) VALUES($1,'worker'),($2,'worker')`, oldWorker, currentWorker); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id IN ($1,$2)`, oldWorker, currentWorker); err != nil {
			t.Error(err)
		}
	})
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO workers(id) VALUES($1),($2)`, oldWorker, currentWorker); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM workers WHERE id IN ($1,$2)`, oldWorker, currentWorker); err != nil {
			t.Error(err)
		}
	})
	assign := func(worker *uuid.UUID) {
		t.Helper()
		if _, err := f.pool.Exec(t.Context(), `UPDATE email_accounts SET worker_id=$2 WHERE id=$1`, f.account, worker); err != nil {
			t.Fatal(err)
		}
	}
	completed := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	first := completed.Add(time.Minute)
	latestOld := first.Add(2 * time.Minute)
	oldReport := models.SyncState{LastSyncedAt: &completed, FolderCoverageObservedAt: &first, FoldersSkippedCap: 4}
	put := func(worker uuid.UUID, state *models.SyncState, want bool) {
		t.Helper()
		accepted, _, err := repo.PutFromWorker(t.Context(), f.user, f.account, worker, uuid.Nil, state, nil)
		if err != nil || accepted != want {
			t.Fatalf("accepted=%t want=%t err=%v", accepted, want, err)
		}
	}
	put(oldWorker, &oldReport, false)
	assign(&oldWorker)
	put(oldWorker, &oldReport, true)
	seed, err := repo.Get(t.Context(), f.account)
	if err != nil {
		t.Fatal(err)
	}
	assign(&currentWorker)
	oldReport.FolderCoverageObservedAt = &latestOld
	oldReport.FoldersSkippedCap = 17
	oldReport.BackfillSynced = 99
	put(oldWorker, &oldReport, false)
	put(uuid.Nil, &oldReport, false)
	currentObservation := first.Add(time.Minute)
	seed.FolderCoverageObservedAt = &currentObservation
	seed.FoldersSkippedCap = 0
	put(currentWorker, seed, true)
	put(oldWorker, &oldReport, false)
	got, err := repo.Get(t.Context(), f.account)
	if err != nil {
		t.Fatal(err)
	}
	if got.FoldersSkippedCap != 0 || got.BackfillSynced != 0 || got.FolderCoverageObservedAt == nil || !got.FolderCoverageObservedAt.Equal(currentObservation) {
		t.Fatalf("current owner's report: %+v", got)
	}
	assign(nil)
	put(currentWorker, &oldReport, false)
}

func TestLiveMailTransportSyncReportRequiresCurrentGeneration(t *testing.T) {
	f := newTransportFixture(t)
	repo := NewEmailSyncStateRepository(f.db)
	worker := uuid.New()
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO fleet_nodes(id,role) VALUES($1,'worker')`, worker); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker); err != nil {
			t.Error(err)
		}
	})
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO workers(id) VALUES($1)`, worker); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE email_accounts SET worker_id=$2 WHERE id=$1`, f.account, worker); err != nil {
		t.Fatal(err)
	}
	put := func(report uuid.UUID, state models.SyncState, want bool) {
		t.Helper()
		accepted, _, err := repo.PutFromWorker(t.Context(), f.user, f.account, worker, report, &state, nil)
		if err != nil || accepted != want {
			t.Fatalf("report=%s pass=%s accepted=%t want=%t err=%v", report, state.TransportGeneration, accepted, want, err)
		}
	}
	put(f.generation, models.SyncState{TransportGeneration: f.generation, BackfillStatus: models.SyncBackfillRunning, BackfillSynced: 3}, true)
	var current uuid.UUID
	if err := f.pool.QueryRow(t.Context(), `UPDATE email_accounts SET transport_generation=gen_random_uuid() WHERE id=$1 RETURNING transport_generation`, f.account).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE email_sync_state SET backfill_status='complete', backfill_synced=0 WHERE email_id=$1`, f.account); err != nil {
		t.Fatal(err)
	}
	put(uuid.Nil, models.SyncState{TransportGeneration: f.generation, BackfillStatus: models.SyncBackfillRunning, BackfillSynced: 9}, false)
	put(f.generation, models.SyncState{BackfillStatus: models.SyncBackfillRunning, BackfillSynced: 9}, false)
	got, err := repo.Get(t.Context(), f.account)
	if err != nil || got.BackfillStatus != models.SyncBackfillComplete || got.BackfillSynced != 0 {
		t.Fatalf("superseded generation wrote: %+v err=%v", got, err)
	}
	put(current, models.SyncState{BackfillStatus: models.SyncBackfillComplete, BackfillSynced: 1}, true)
	put(uuid.Nil, models.SyncState{BackfillStatus: models.SyncBackfillComplete, BackfillSynced: 2}, true)
	put(current, models.SyncState{TransportGeneration: current, BackfillStatus: models.SyncBackfillComplete, BackfillSynced: 3}, true)
}

func TestLiveMailTransportSyncReportCommitsRecoveryAndBothClocks(t *testing.T) {
	f := newTransportFixture(t)
	repo := NewEmailSyncStateRepository(f.db)
	worker := uuid.New()
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO fleet_nodes(id,role) VALUES($1,'worker')`, worker); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker); err != nil {
			t.Error(err)
		}
	})
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO workers(id) VALUES($1)`, worker); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE email_accounts SET worker_id=$2 WHERE id=$1`, f.account, worker); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	if _, err := f.repo.CreateOnce(t.Context(), f.report(models.MailTransportSync, &old)); err != nil {
		t.Fatal(err)
	}
	synced := old.Add(time.Minute)
	state := &models.SyncState{LastSyncedAt: &synced, FolderCoverageObservedAt: &synced, FoldersSkippedCap: -1}
	calls := 0
	recover := func(ctx context.Context, _ *SyncStatePrior) error {
		calls++
		if xerr := f.repo.RecordTransportSuccess(ctx, f.account, models.MailTransportSync, synced, nil, f.generation); xerr != nil {
			return xerr
		}
		return nil
	}
	if accepted, _, err := repo.PutFromWorker(t.Context(), f.user, f.account, uuid.New(), uuid.Nil, state, recover); err != nil || accepted || calls != 0 {
		t.Fatalf("declined report recovered: accepted=%t calls=%d err=%v", accepted, calls, err)
	}
	if accepted, _, err := repo.PutFromWorker(t.Context(), f.user, f.account, worker, uuid.Nil, state, recover); err == nil || accepted {
		t.Fatalf("invalid state committed: accepted=%t err=%v", accepted, err)
	}
	var accountAt *time.Time
	var recoveries int
	if err := f.pool.QueryRow(t.Context(), `SELECT last_synced_at FROM email_accounts WHERE id=$1`, f.account).Scan(&accountAt); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM email_account_transport_recovery WHERE email_account_id=$1`, f.account).Scan(&recoveries); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(t.Context(), f.account)
	if err != nil || got != nil || accountAt != nil || recoveries != 0 || len(f.errors(t, true)) != 1 || calls != 1 {
		t.Fatalf("failed state escaped rollback: state=%+v account=%v recoveries=%d calls=%d err=%v", got, accountAt, recoveries, calls, err)
	}
	state.FoldersSkippedCap = 4
	if accepted, _, err := repo.PutFromWorker(t.Context(), f.user, f.account, worker, uuid.Nil, state, recover); err != nil || !accepted {
		t.Fatalf("accepted=%t err=%v", accepted, err)
	}
	got, err = repo.Get(t.Context(), f.account)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(t.Context(), `SELECT last_synced_at FROM email_accounts WHERE id=$1`, f.account).Scan(&accountAt); err != nil {
		t.Fatal(err)
	}
	if got.LastSyncedAt == nil || accountAt == nil || !got.LastSyncedAt.Equal(synced) || !accountAt.Equal(synced) || len(f.errors(t, true)) != 0 || calls != 2 {
		t.Fatalf("accepted clocks/recovery: state=%+v account=%v calls=%d", got, accountAt, calls)
	}
}
