package orgtransfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (f *selectiveHoldReviewFixture) localRows(t *testing.T) string {
	t.Helper()
	var snapshot string
	if err := f.pool.QueryRow(t.Context(), `SELECT jsonb_build_object('task',to_jsonb(t),
		'email',(SELECT to_jsonb(e) FROM email_tasks e WHERE e.task_id=t.id),
		'warmup',(SELECT to_jsonb(w) FROM warmup_tasks w WHERE w.task_id=t.id),
		'tokens',(SELECT jsonb_agg(to_jsonb(w) ORDER BY w.token) FROM warmup_tokens w WHERE w.task_id=t.id),
		'attempts',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.nonce) FROM outbound_attempts a WHERE a.task_id=t.id),
		'resolutions',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM send_recovery_resolutions r WHERE r.recovery_task_id=t.id),
		'failures',(SELECT jsonb_agg(to_jsonb(f)) FROM task_failures f WHERE f.task_id=t.id),
		'dead_letters',(SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id) FROM task_dead_letters d WHERE d.task_id=t.id),
		'execution_keys',(SELECT jsonb_agg(to_jsonb(k)) FROM task_execution_keys k WHERE k.task_id=t.id),
		'receipts',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.internal_id) FROM warmup_received r WHERE r.email_account_id=t.email_account_id))::text
		FROM tasks t WHERE t.id=$1`, f.task).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestLiveImportLocalSendIdentityPayloadAndUnrelated(t *testing.T) {
	f := newSelectiveHoldReviewFixture(t)
	other := uuid.New()
	f.exec(t, `INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,status) VALUES($1,$2,$3,$4,'Other','','','smtp_imap','active')`, other, f.tenant.owner, f.tenant.org, other.String()+"@fixture.invalid")
	if err := f.repo.UpdateTaskEmailAccount(t.Context(), f.task, other); err != nil {
		t.Fatal(err)
	}
	groups := []models.OrgDataGroup{models.OrgDataGroupSending}
	archive := f.archive(t, groups)
	if err := f.repo.UpdateTaskEmailAccount(t.Context(), f.task, f.mailbox); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE email_tasks SET subject='Current payload',body='Current body',body_plain='Current body' WHERE task_id=$1`, f.task)
	f.exec(t, `UPDATE email_accounts SET name='Current unrelated metadata' WHERE id=$1`, other)
	f.reserve(t)
	f.begin(t)
	before := f.localRows(t)
	f.importArchive(t, archive, groups, models.OrgImportConflictOverwrite)
	if after := f.localRows(t); after != before {
		t.Error("older native archive changed current task identity, type, reservation or typed payload")
	}
	var name string
	if err := f.pool.QueryRow(t.Context(), `SELECT name FROM email_accounts WHERE id=$1`, other).Scan(&name); err != nil || name != "Other" {
		t.Fatalf("unrelated mailbox overwrite was improperly suppressed: %q %v", name, err)
	}
	f.finish(t)
	calls := 0
	if err := f.repo.(repository.SendResultRecovery).ApplySendResult(t.Context(), f.result(), func(inner context.Context) error {
		calls++
		return f.repo.UpdateTaskMessageID(inner, f.task, f.result().MessageID)
	}); err != nil || calls != 1 {
		t.Fatalf("matching original mailbox/payload result refused after reparent archive: %d %v", calls, err)
	}
}

type failLocalImportRepository struct {
	repository.OrgTransferRepository
	table string
}

var localImportFailure = errors.New("synthetic local import failure")

func (r *failLocalImportRepository) InsertBatch(ctx context.Context, tx pgx.Tx, table string, cols []string, rows []json.RawMessage, conflict models.OrgImportConflict, pk []string, owner string, org uuid.UUID, resets []string, invalidations []repository.ImportInvalidation) (int64, error) {
	if table == r.table {
		return 0, localImportFailure
	}
	return r.OrgTransferRepository.InsertBatch(ctx, tx, table, cols, rows, conflict, pk, owner, org, resets, invalidations)
}

func TestLiveImportLocalSendRollbackForeignAndRestrictiveConflicts(t *testing.T) {
	for _, kind := range []string{"rollback", "foreign", "conflicting_holds", "local_conflict", "local_authentication"} {
		t.Run(kind, func(t *testing.T) {
			f := newSelectiveHoldReviewFixture(t)
			groups := []models.OrgDataGroup{models.OrgDataGroupSending}
			archive := f.archive(t, groups)
			f.reserve(t)
			f.begin(t)
			if kind == "conflicting_holds" {
				archive = f.archive(t, groups)
			}
			result := f.result()
			if kind == "local_authentication" {
				result.Success = false
				result.Error = &models.EmailSendError{Failure: &errx.SendFailure{Disposition: errx.SendAuth, Scope: "mailbox", ObservedAt: time.Now()}}
			}
			if kind == "local_authentication" || kind == "local_conflict" || kind == "conflicting_holds" {
				if err := f.admission.FinishOutbound(t.Context(), f.task, f.mailbox, f.worker, result); err != nil {
					t.Fatal(err)
				}
				if err := f.repo.(repository.SendResultRecovery).ApplySendResult(t.Context(), result, func(context.Context) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "local_conflict" {
				contrary := result
				contrary.Success = false
				contrary.Error = &models.EmailSendError{Failure: &errx.SendFailure{Disposition: errx.SendPermanent, Scope: "recipient", ObservedAt: time.Now()}}
				if err := f.repo.(repository.SendResultRecovery).ApplySendResult(t.Context(), contrary, func(context.Context) error { t.Error("conflict repeated accounting"); return nil }); err != nil {
					t.Fatal(err)
				}
				archive = f.archive(t, groups)
			}
			if kind == "conflicting_holds" {
				newTask := uuid.New()
				f.exec(t, `INSERT INTO tasks(id,email_account_id,task_type,status,message_id) VALUES($1,$2,'email','active','')`, newTask, f.mailbox)
				f.exec(t, `INSERT INTO email_tasks(task_id,to_addrs,subject,body,body_plain,encrypted) VALUES($1,ARRAY['recipient@fixture.invalid'],'New local','Synthetic body','Synthetic body',false)`, newTask)
				if _, err := f.admission.ReserveOutbound(t.Context(), repository.OutboundReservation{TaskID: newTask, MailboxID: f.mailbox, OrganizationID: f.tenant.org, WorkerID: f.worker, Provider: models.InboxProviderSMTPIMAP, Recipients: []string{"recipient@fixture.invalid"}}); err != nil {
					t.Fatal(err)
				}
			}
			before, beforeRows := f.snapshot(t), f.localRows(t)
			var generation string
			if err := f.pool.QueryRow(t.Context(), `SELECT transport_generation::text FROM email_accounts WHERE id=$1`, f.mailbox).Scan(&generation); err != nil {
				t.Fatal(err)
			}
			org, actor := f.tenant.org, f.tenant.owner
			var want error
			if kind == "rollback" {
				service := f.svc.(*service)
				service.repo = &failLocalImportRepository{OrgTransferRepository: service.repo, table: "email_tasks"}
				want = localImportFailure
			} else if kind == "foreign" {
				other := newTenantFixture(t, f.pool)
				org, actor = other.org, other.owner
				want = ErrForeignRows
			} else if kind == "conflicting_holds" {
				want = repository.ErrImportSendRecoveryConflict
			}
			_, err := f.svc.ImportFrom(t.Context(), org, bytes.NewReader(archive), ImportOptions{Groups: groups, Passphrase: "synthetic-review-passphrase", Conflict: models.OrgImportConflictOverwrite, ActorUserID: actor}, nil)
			if !errors.Is(err, want) {
				t.Fatalf("import result %v, want %v", err, want)
			}
			if f.snapshot(t) != before || f.localRows(t) != beforeRows {
				t.Error("failed import or restrictive archive overwrite changed local recovery projection/evidence")
			}
			if want != nil {
				var after string
				if err := f.pool.QueryRow(t.Context(), `SELECT transport_generation::text FROM email_accounts WHERE id=$1`, f.mailbox).Scan(&after); err != nil || after != generation {
					t.Fatalf("failed import did not roll back mailbox generation: %v", err)
				}
			}
		})
	}
}

func nativeWarmupArchiveFixture(t *testing.T, reply bool) (*selectiveHoldReviewFixture, uuid.UUID) {
	t.Helper()
	f := newSelectiveHoldReviewFixture(t)
	recipient := uuid.New()
	f.exec(t, `UPDATE organizations SET risk_state='trusted' WHERE id=$1`, f.tenant.org)
	f.exec(t, `UPDATE email_accounts SET warmup=NOW(),warmup_days=127 WHERE id=$1`, f.mailbox)
	f.exec(t, `INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,status,worker_id,timezone,warmup,warmup_days,campaign_limit,warmup_max,shared_daily_limit,rolling_recipient_limit) VALUES($1,$2,$3,$4,'Native recipient','','','smtp_imap','active',$5,'UTC',NOW(),127,50,50,100,100)`, recipient, f.tenant.owner, f.tenant.org, recipient.String()+"@fixture.invalid", f.worker)
	f.exec(t, `INSERT INTO email_accounts_smtp_imap(email_account_id,smtp_host,smtp_port,smtp_user,smtp_password,imap_host,imap_port,imap_user,imap_password) SELECT $1,smtp_host,smtp_port,smtp_user,smtp_password,imap_host,imap_port,imap_user,imap_password FROM email_accounts_smtp_imap WHERE email_account_id=$2`, recipient, f.mailbox)
	f.exec(t, `UPDATE email_accounts SET test_mode=NULL WHERE id IN($1,$2)`, recipient, f.mailbox)
	f.exec(t, `INSERT INTO warmup_pool_participants(pool_id,email_account_id,participant_role,health_state) SELECT p.id,ea.id,'sender_receiver','healthy' FROM warmup_pools p CROSS JOIN email_accounts ea WHERE p.pool_type='free' AND ea.id IN($1,$2)`, recipient, f.mailbox)
	f.exec(t, `DELETE FROM email_tasks WHERE task_id=$1`, f.task)
	f.exec(t, `UPDATE tasks SET task_type='warmup',status='pending',scheduled_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, f.task)
	f.exec(t, `INSERT INTO warmup_tasks(task_id,subject) VALUES($1,'Archive opening')`, f.task)
	lineage := f.repo.(repository.WarmupLineageRepository)
	if reply {
		parent := uuid.New()
		f.exec(t, `INSERT INTO tasks(id,email_account_id,task_type,status,message_id) VALUES($1,$2,'warmup','active','')`, parent, recipient)
		f.exec(t, `INSERT INTO warmup_tasks(task_id,target_account_id,subject) VALUES($1,$2,'Native parent')`, parent, f.mailbox)
		if err := lineage.SaveWarmupLineage(t.Context(), parent, repository.WarmupLineage{Subject: "Native parent", ScenarioVersion: "diagnostic-v1", RenderingVersion: "canonical-v1", MaxTurns: 3}); err != nil {
			t.Fatal(err)
		}
		f.exec(t, `INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,subject,conversation_id,conversation_turn,content_source) VALUES($1,$2,$3,$4,'Native parent',$5,0,'static')`, uuid.New(), parent, recipient, f.mailbox, uuid.New())
		if _, err := f.repo.(repository.WarmupDispatchRepository).AuthorizeWarmupDispatch(t.Context(), parent, recipient, f.worker); err != nil {
			t.Fatal(err)
		}
		nonce, err := f.admission.ReserveOutbound(t.Context(), repository.OutboundReservation{TaskID: parent, MailboxID: recipient, OrganizationID: f.tenant.org, WorkerID: f.worker, Provider: models.InboxProviderSMTPIMAP, Recipients: []string{f.mailbox.String() + "@example.test"}})
		if err != nil {
			t.Fatal(err)
		}
		start, err := f.admission.BeginOutbound(t.Context(), parent, recipient, f.worker, nonce)
		if err != nil || start == nil || start.State != "execute" {
			t.Fatalf("real parent start denied: %+v %v", start, err)
		}
		result := models.SendEmailResult{TaskID: parent, Success: true, MessageID: "<parent@fixture.invalid>", SentAt: time.Now()}
		if err := f.admission.FinishOutbound(t.Context(), parent, recipient, f.worker, result); err != nil {
			t.Fatal(err)
		}
		warmup := repository.NewWarmupRepository(f.pool)
		if err := f.repo.(repository.SendResultRecovery).ApplySendResult(t.Context(), result, func(inner context.Context) error {
			return warmup.RecordWarmupTokenDelivery(inner, parent, result.MessageID)
		}); err != nil {
			t.Fatal(err)
		}
		receipt := uuid.New()
		f.exec(t, `INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id) VALUES($1,$2,$3,'parent@fixture.invalid')`, f.mailbox, receipt, recipient)
		if err := lineage.RecordVerifiedWarmupParent(t.Context(), parent, f.mailbox, receipt, "native-local-thread", repository.WarmupReceiptProof{SenderAddress: recipient.String() + "@fixture.invalid", MessageID: result.MessageID}); err != nil {
			t.Fatal(err)
		}
		f.exec(t, `UPDATE tasks SET scheduled_at=NOW()+INTERVAL '1 hour' WHERE id=$1`, f.task)
		if bound, err := lineage.BindWarmupSuccessor(t.Context(), parent, f.mailbox, receipt, time.Now().Add(time.Hour)); err != nil || !bound {
			t.Fatalf("native receipt did not bind current reply: %v %v", bound, err)
		}
		if err := lineage.RescheduleWarmupTask(t.Context(), f.task, time.Now().Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
	} else {
		f.exec(t, `UPDATE warmup_tasks SET target_account_id=$2 WHERE task_id=$1`, f.task, recipient)
	}
	if err := lineage.SaveWarmupLineage(t.Context(), f.task, repository.WarmupLineage{Subject: "Pinned local", ScenarioVersion: "diagnostic-v1", RenderingVersion: "canonical-v1", MaxTurns: 3}); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,subject,conversation_id,conversation_turn,content_source) VALUES($1,$2,$3,$4,'Pinned local',$5,$6,'static')`, uuid.New(), f.task, f.mailbox, recipient, uuid.New(), map[bool]int{false: 0, true: 1}[reply])
	f.exec(t, `INSERT INTO warmup_statistics(email_account_id,date,emails_sent,emails_replied,target_volume) VALUES($1,(NOW() AT TIME ZONE 'UTC')::date,0,0,10)`, f.mailbox)
	return f, recipient
}

func TestLiveImportLocalWarmupChargeReplyReceiptAndLateAccounting(t *testing.T) {
	for _, reply := range []bool{false, true} {
		for _, success := range []bool{false, true} {
			for _, mode := range []string{"core", "sending", "sending_warmup"} {
				label := mode + "/" + map[bool]string{false: "opening", true: "reply"}[reply] + "/" + map[bool]string{false: "failed", true: "sent"}[success]
				t.Run(label, func(t *testing.T) {
					f, recipient := nativeWarmupArchiveFixture(t, reply)
					groups := []models.OrgDataGroup{models.OrgDataGroupCore}
					if mode != "core" {
						groups = []models.OrgDataGroup{models.OrgDataGroupSending}
					}
					if mode == "sending_warmup" {
						groups = append(groups, models.OrgDataGroupWarmup)
					}
					archive := f.archive(t, groups)
					f.exec(t, `UPDATE warmup_tokens SET token=gen_random_uuid(),subject='Current pinned token' WHERE task_id=$1`, f.task)
					if claimed, err := f.repo.(repository.WarmupLineageRepository).ClaimWarmupTask(t.Context(), f.task, time.Now()); err != nil || !claimed {
						t.Fatalf("real warmup claim failed: %v %v", claimed, err)
					}
					if _, err := f.repo.(repository.WarmupDispatchRepository).AuthorizeWarmupDispatch(t.Context(), f.task, f.mailbox, f.worker); err != nil {
						t.Fatal(err)
					}
					var err error
					f.nonce, err = f.admission.ReserveOutbound(t.Context(), repository.OutboundReservation{TaskID: f.task, MailboxID: f.mailbox, OrganizationID: f.tenant.org, WorkerID: f.worker, Provider: models.InboxProviderSMTPIMAP, Recipients: []string{recipient.String() + "@fixture.invalid"}})
					if err != nil {
						t.Fatal(err)
					}
					start, err := f.admission.BeginOutbound(t.Context(), f.task, f.mailbox, f.worker, f.nonce)
					if err != nil || start == nil || start.State != "execute" {
						t.Fatalf("real current native warmup start denied: %+v %v", start, err)
					}
					before := f.localRows(t)
					booked, err := f.repo.GetBookedSendsForAccount(t.Context(), f.mailbox, time.Now())
					if err != nil {
						t.Fatal(err)
					}
					f.importArchive(t, archive, groups, models.OrgImportConflictOverwrite)
					if after := f.localRows(t); after != before {
						t.Error("native import changed warmup task/binding/payload/token/lineage/charge/attempt/receipt evidence")
					}
					bookedAfter, err := f.repo.GetBookedSendsForAccount(t.Context(), f.mailbox, time.Now())
					if err != nil || len(bookedAfter) != len(booked) {
						t.Errorf("native import changed booked sends: %d -> %d %v", len(booked), len(bookedAfter), err)
					}
					var sent, replies int
					checkCounts := func(wantSent, wantReply int) {
						t.Helper()
						if err := f.pool.QueryRow(t.Context(), `SELECT emails_sent,emails_replied FROM warmup_statistics WHERE email_account_id=$1 AND date=(NOW() AT TIME ZONE 'UTC')::date`, f.mailbox).Scan(&sent, &replies); err != nil || sent != wantSent || replies != wantReply {
							t.Errorf("current known charge/refund wrong: sent/replies=%d/%d want=%d/%d err=%v", sent, replies, wantSent, wantReply, err)
						}
					}
					checkCounts(1, map[bool]int{false: 0, true: 1}[reply])
					result := f.result()
					result.Success = success
					if !success {
						result.Error = &models.EmailSendError{Failure: &errx.SendFailure{Disposition: errx.SendPermanent, Scope: "recipient", ObservedAt: time.Now()}}
					}
					if err := f.admission.FinishOutbound(t.Context(), f.task, f.mailbox, f.worker, result); err != nil {
						t.Fatal(err)
					}
					if err := f.repo.(repository.WarmupDispatchRepository).FinishWarmupDispatch(t.Context(), f.task, f.mailbox, f.worker, result); err != nil {
						t.Fatal(err)
					}
					warmup := repository.NewWarmupRepository(f.pool)
					calls, effects := 0, 0
					apply := func(inner context.Context) error {
						calls++
						repository.AfterSendResultCommit(inner, func(context.Context) { effects++ })
						if !success {
							return warmup.FailWarmupSend(inner, f.mailbox, f.task, time.Now(), "Native refused", "Synthetic local refusal")
						}
						if err := f.repo.UpdateTaskMessageID(inner, f.task, result.MessageID); err != nil {
							return err
						}
						return warmup.RecordWarmupTokenDelivery(inner, f.task, result.MessageID)
					}
					for range 2 {
						if err := f.repo.(repository.SendResultRecovery).ApplySendResult(t.Context(), result, apply); err != nil {
							t.Fatal(err)
						}
					}
					if calls != 1 || effects != 1 {
						t.Errorf("late result accounting/postcommit effects repeated: %d/%d", calls, effects)
					}
					checkCounts(map[bool]int{false: 0, true: 1}[success], map[bool]int{false: 0, true: 1}[success && reply])
					var correct bool
					if err := f.pool.QueryRow(t.Context(), `SELECT t.send_result_applied_at IS NOT NULL AND t.send_result_state=$2 AND w.warmup_charged_date IS NOT NULL AND w.warmup_reply_charged=$3 AND (w.warmup_refunded_at IS NULL)=$4 AND (wt.sent_message_id<>'')=$4 FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id JOIN warmup_tokens wt ON wt.task_id=t.id WHERE t.id=$1`, f.task, map[bool]string{false: "failed", true: "sent"}[success], reply, success).Scan(&correct); err != nil || !correct {
						t.Errorf("late native result lost applied state, original known charge or token delivery/refund: %v %v", correct, err)
					}
				})
			}
		}
	}
}

func TestLiveImportFreshWarmupReservationDuringMailboxImport(t *testing.T) {
	f, recipient := nativeWarmupArchiveFixture(t, false)
	groups := []models.OrgDataGroup{models.OrgDataGroupSending, models.OrgDataGroupWarmup}
	archive := f.archive(t, groups)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ready, resume := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
	})
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.ImportFrom(ctx, f.tenant.org, bytes.NewReader(archive), ImportOptions{Groups: groups, Passphrase: "synthetic-review-passphrase", Conflict: models.OrgImportConflictOverwrite, ActorUserID: f.tenant.owner}, func(_ int, stage string) {
			if stage == "importing email_accounts" {
				close(ready)
				select {
				case <-resume:
				case <-ctx.Done():
				}
			}
		})
		done <- err
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("import ended before mailbox barrier: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	f.task = uuid.New()
	f.exec(t, `INSERT INTO tasks(id,email_account_id,task_type,status,scheduled_at,message_id) VALUES($1,$2,'warmup','pending',NOW()-INTERVAL '1 minute','')`, f.task, f.mailbox)
	if err := f.repo.CreateWarmupTask(ctx, &repository.WarmupTask{TaskID: f.task, TargetAccountID: &recipient}); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.(repository.WarmupLineageRepository).SaveWarmupLineage(ctx, f.task, repository.WarmupLineage{Subject: "Native late creation", ScenarioVersion: "diagnostic-v1", RenderingVersion: "canonical-v1", MaxTurns: 3}); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,subject,conversation_id,conversation_turn,content_source) VALUES($1,$2,$3,$4,'Native late creation',$5,0,'static')`, uuid.New(), f.task, f.mailbox, recipient, uuid.New())
	if claimed, err := f.repo.(repository.WarmupLineageRepository).ClaimWarmupTask(ctx, f.task, time.Now()); err != nil || !claimed {
		t.Fatalf("new warmup task not claimed while import waits: %v %v", claimed, err)
	}
	if _, err := f.repo.(repository.WarmupDispatchRepository).AuthorizeWarmupDispatch(ctx, f.task, f.mailbox, f.worker); err != nil {
		t.Fatal(err)
	}
	var err error
	f.nonce, err = f.admission.ReserveOutbound(ctx, repository.OutboundReservation{TaskID: f.task, MailboxID: f.mailbox, OrganizationID: f.tenant.org, WorkerID: f.worker, Provider: models.InboxProviderSMTPIMAP, Recipients: []string{recipient.String() + "@fixture.invalid"}})
	if err != nil {
		t.Fatal(err)
	}
	before, beforeRows := f.snapshot(t), f.localRows(t)
	close(resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if after := f.snapshot(t); after != before || f.localRows(t) != beforeRows {
		t.Error("import's mailbox table inventory lost a new local warmup reservation, charge, hold or typed state")
	}
	start, err := f.admission.BeginOutbound(ctx, f.task, f.mailbox, f.worker, f.nonce)
	if err != nil || start == nil || start.State != "execute" {
		t.Errorf("fresh new local reservation did not remain executable: %+v %v", start, err)
	}
}
