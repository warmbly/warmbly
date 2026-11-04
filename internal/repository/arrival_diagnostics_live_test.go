package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/arrivaldiag"
)

type countedArrivalCipher struct {
	cipher.CipherService
	calls int
}

func (c *countedArrivalCipher) Cipher(ctx context.Context, org uuid.UUID) (*cipher.Cipher, error) {
	c.calls++
	return c.CipherService.Cipher(ctx, org)
}

func TestLiveArrivalDiagnosticsPreserveStageRetryAndKeyCalls(t *testing.T) {
	for _, observed := range []bool{false, true} {
		d := liveUniboxFolderDB(t)
		f := newUniboxFolderFixture(t, d.Pool)
		keys := &countedArrivalCipher{CipherService: arrivalLiveCipher(t, d)}
		r := NewDurableEmailMessageMapRepository(d, keys)
		data, pending := arrivalLivePayload(f, "PRIVATE_PROVIDER_MESSAGE_ID")
		pending.Arrival.Message.Subject = "PRIVATE_CUSTOMER_SUBJECT_BODY_AUTH"
		pending.Bounce = &models.JobEventInboundBounce{UserID: f.user, EmailID: f.mailbox}
		pending.Complaint = &models.JobEventInboundComplaint{UserID: f.user, EmailID: f.mailbox}
		if err := r.AdmitArrival(t.Context(), data, pending); err != nil {
			t.Fatal(err)
		}
		keys.calls = 0
		ctx := t.Context()
		var measurements *arrivaldiag.Pass
		if observed {
			ctx, measurements = arrivaldiag.WithPass(ctx)
		}
		first := errors.New("PRIVATE_RAW_PROVIDER_ERROR_AUTH")
		var seen []models.JobEventType
		handle := func(_ context.Context, kind models.JobEventType, body any) error {
			seen = append(seen, kind)
			if kind == models.JobEventTypeNewEmail {
				event := body.(*models.JobEventNewEmail)
				if event.UserID != pending.Arrival.UserID || event.Message.ID != pending.Arrival.Message.ID || event.Message.Subject != pending.Arrival.Message.Subject {
					t.Fatal("instrumentation changed payload identity")
				}
			}
			if kind == models.JobEventTypeInboundBounce && len(seen) == 2 {
				return first
			}
			return nil
		}
		if err := r.DeliverArrivals(ctx, handle); err != first {
			t.Fatal("first error identity changed", err)
		}
		var stage int
		var unlocked, retryFuture, retryBounded bool
		if err := d.QueryRow(t.Context(), `SELECT stage,locked_until IS NULL,retry_at>now(),retry_at<=now()+interval '15 seconds' FROM sync_arrival_outbox WHERE email_id=$1`, f.mailbox).Scan(&stage, &unlocked, &retryFuture, &retryBounded); err != nil || stage != 1 || !unlocked || !retryFuture || !retryBounded {
			t.Fatalf("retry/checkpoint semantics changed: stage=%d unlocked=%v future=%v bounded=%v error=%v", stage, unlocked, retryFuture, retryBounded, err)
		}
		dueArrival(t, d, f)
		if err := r.DeliverArrivals(ctx, handle); err != nil {
			t.Fatal(err)
		}
		want := []models.JobEventType{models.JobEventTypeNewEmail, models.JobEventTypeInboundBounce, models.JobEventTypeInboundBounce, models.JobEventTypeInboundComplaint}
		if len(seen) != len(want) {
			t.Fatal("unexpected stage attempts", seen)
		}
		for i, kind := range want {
			if seen[i] != kind {
				t.Fatal("stage order/retry changed", seen)
			}
		}
		if keys.calls != 2 {
			t.Fatal("diagnostics fetched/decrypted extra keys", keys.calls)
		}
		if observed {
			var output bytes.Buffer
			previous := log.Logger
			log.Logger = zerolog.New(&output)
			var window arrivaldiag.Window
			window.Record(measurements.Snapshot(), time.Second, true, false, false, false, time.Now()).Log()
			log.Logger = previous
			if strings.Contains(output.String(), "PRIVATE") || strings.Contains(output.String(), f.user.String()) || strings.Contains(output.String(), f.mailbox.String()) {
				t.Fatal("sensitive delivery information entered diagnostics")
			}
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			stages := record["stages"].(map[string]any)
			// One batched claim per pass; a checkpoint only where a later stage still has to run.
			for name, want := range map[string]float64{"claim": 2, "key_lookup": 2, "decrypt": 2, "decode": 2, "new_email": 1, "bounce": 2, "complaint": 1, "checkpoint": 2, "retry": 1, "retire": 1} {
				if stages[name].(map[string]any)["attempts"] != want {
					t.Fatalf("%s count was not an attempt count: %v", name, stages[name])
				}
			}
		}
	}
}
