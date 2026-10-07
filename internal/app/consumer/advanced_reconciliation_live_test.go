package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/app/notification"
	"github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type resultDispatcher struct{ calls atomic.Int32 }

func (d *resultDispatcher) Dispatch(context.Context, uuid.UUID, models.WebhookEventType, any) (uuid.UUID, error) {
	d.calls.Add(1)
	return uuid.New(), nil
}

func TestLiveAdvancedBounceReconciliationRollbackAndReplayWithOneConnection(t *testing.T) {
	h := liveDB(t)
	f := newSendResultFixture(t, h)
	s := liveJobsService(h)
	task := f.stampSend(t, s)
	cfg := h.Pool.Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	one := &db.DB{Pool: pool}
	r := repository.NewTaskRepository(pool).(repository.SendResultRecovery)
	a := repository.NewAdvancedOutreachRepository(pool)
	v, err := a.CreateABVariant(t.Context(), f.campaign, &models.CreateCampaignABVariantRequest{Name: "Variant", SequenceID: &f.step, Subject: "Diagnostic", BodyPlain: "Text"})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.AssignVariant(t.Context(), f.campaign, f.contact, v.ID); err != nil {
		t.Fatal(err)
	}
	svc := advanced.NewService(a, repository.NewCampaignRepostory(one), repository.NewEmailRepostory(one, nil), repository.NewTaskRepository(pool), repository.NewContactRepostory(one), repository.NewCampaignProgressRepository(pool), nil, nil, nil, nil, warmup.NewService(repository.NewWarmupRepository(pool)))
	dispatcher := &resultDispatcher{}
	svc.(interface {
		WireDispatcher(advanced.EventDispatcher)
	}).WireDispatcher(dispatcher)
	svc.WireNotifier(notification.NewService(repository.NewNotificationRepository(pool), nil))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	request := &models.IngestDeliverabilityEventRequest{CampaignID: &f.campaign, TaskID: &task, ContactID: &f.contact, EventType: models.DeliverabilityEventBounce, Provider: "smtp_reject", RecipientEmail: "lead-" + f.contact.String()[:8] + "@test.local", Reason: "unknown recipient", IdempotencyKey: "refusal-" + task.String()}
	result := models.SendEmailResult{TaskID: task, Error: &models.EmailSendError{Code: "RECIPIENT_REJECTED"}}
	fault := errors.New("fault after advanced DB hooks")
	callback := func(ctx context.Context) error {
		if err := svc.IngestDeliverabilityEvent(ctx, f.org, request); err != nil {
			return err
		}
		return nil
	}
	if err = r.ApplySendResult(ctx, result, func(ctx context.Context) error {
		if err := callback(ctx); err != nil {
			return err
		}
		if dispatcher.calls.Load() != 0 {
			return errors.New("external effects escaped transaction")
		}
		return fault
	}); !errors.Is(err, fault) {
		t.Fatalf("callback: %v", err)
	}
	var events, suppressed int
	var bounced *time.Time
	if err = h.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM deliverability_events WHERE organization_id=$1`, f.org).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err = h.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM suppressed_recipients WHERE organization_id=$1`, f.org).Scan(&suppressed); err != nil {
		t.Fatal(err)
	}
	if err = h.Pool.QueryRow(ctx, `SELECT bounced_at FROM campaign_ab_assignments WHERE campaign_id=$1 AND contact_id=$2`, f.campaign, f.contact).Scan(&bounced); err != nil {
		t.Fatal(err)
	}
	if events != 0 || suppressed != 0 || bounced != nil || dispatcher.calls.Load() != 0 {
		t.Fatal("advanced hooks escaped rollback", events, suppressed, bounced, dispatcher.calls.Load())
	}
	if err = r.ApplySendResult(ctx, result, callback); err != nil {
		t.Fatal(err)
	}
	if err = r.ApplySendResult(ctx, result, callback); err != nil {
		t.Fatal(err)
	}
	if err = h.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM deliverability_events WHERE organization_id=$1`, f.org).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err = h.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM suppressed_recipients WHERE organization_id=$1`, f.org).Scan(&suppressed); err != nil {
		t.Fatal(err)
	}
	if events != 1 || suppressed != 1 || dispatcher.calls.Load() != 0 {
		t.Fatal("advanced replay duplicated durable or external effects", events, suppressed, dispatcher.calls.Load())
	}
	var queued int
	if err = h.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM send_result_effects WHERE task_id=$1 AND delivered_at IS NULL`, task).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 3 {
		t.Fatal("replay duplicated durable outbox", queued)
	}
	if err = svc.(interface{ DeliverSendResultEffects(context.Context) error }).DeliverSendResultEffects(ctx); err != nil {
		t.Fatal(err)
	}
	if err = svc.(interface{ DeliverSendResultEffects(context.Context) error }).DeliverSendResultEffects(ctx); err != nil {
		t.Fatal(err)
	}
	if dispatcher.calls.Load() != 2 {
		t.Fatal("outbox did not deliver committed effects once", dispatcher.calls.Load())
	}
	if _, err = h.Pool.Exec(ctx, `UPDATE send_result_effects SET delivered_at=NULL,locked_until=NULL WHERE task_id=$1 AND kind='notification'`, task); err != nil {
		t.Fatal(err)
	}
	if err = svc.(interface{ DeliverSendResultEffects(context.Context) error }).DeliverSendResultEffects(ctx); err != nil {
		t.Fatal(err)
	}
	var notices int
	if err = h.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE organization_id=$1 AND category=$2`, f.org, models.NotifHealthBounce).Scan(&notices); err != nil {
		t.Fatal(err)
	}
	if notices != 1 {
		t.Fatal("notification outbox restart duplicated or lost feed", notices)
	}
}
