package confenge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
	"github.com/warmbly/warmbly/internal/repository"
)

// OutcomeDeliveryWorker drains outreach_outcome_outbox to the configured webhook.
type OutcomeDeliveryWorker struct {
	repo      repository.OutreachRepository
	cfg       Config
	http      *http.Client
	batchSize int
	pollEvery time.Duration
	maxTries  int
}

// OutcomeDeliveryOptions configures the worker.
type OutcomeDeliveryOptions struct {
	BatchSize  int
	PollEvery  time.Duration
	HTTPClient *http.Client
	MaxTries   int
}

// NewOutcomeDeliveryWorker builds a worker. When webhook URL is empty, Run waits on ctx only.
func NewOutcomeDeliveryWorker(repo repository.OutreachRepository, cfg Config, opts OutcomeDeliveryOptions) *OutcomeDeliveryWorker {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 25
	}
	if opts.PollEvery <= 0 {
		opts.PollEvery = 3 * time.Second
	}
	if opts.MaxTries <= 0 {
		opts.MaxTries = 8
	}
	if opts.HTTPClient == nil {
		client := safehttp.Client(15 * time.Second)
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		opts.HTTPClient = client
	}
	return &OutcomeDeliveryWorker{
		repo:      repo,
		cfg:       cfg,
		http:      opts.HTTPClient,
		batchSize: opts.BatchSize,
		pollEvery: opts.PollEvery,
		maxTries:  opts.MaxTries,
	}
}

// Run blocks until ctx is cancelled.
func (w *OutcomeDeliveryWorker) Run(ctx context.Context) {
	if w == nil {
		return
	}
	if strings.TrimSpace(w.cfg.OutcomeWebhookURL) == "" || strings.TrimSpace(w.cfg.OutcomeWebhookSecret) == "" {
		log.Info().Msg("confenge outcome delivery worker idle (webhook URL/secret unset)")
		<-ctx.Done()
		return
	}
	t := time.NewTicker(w.pollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *OutcomeDeliveryWorker) tick(ctx context.Context) {
	rows, err := w.repo.ListPendingOutcomes(ctx, w.batchSize)
	if err != nil {
		log.Warn().Err(err).Msg("confenge outbox list failed")
		return
	}
	for i := range rows {
		w.deliverOne(ctx, &rows[i])
	}
}

func (w *OutcomeDeliveryWorker) deliverOne(ctx context.Context, ev *models.OutreachOutcome) {
	env := BuildOutcomeEnvelope(ev)
	body, err := json.Marshal(env)
	if err != nil {
		w.fail(ctx, ev, "marshal: "+err.Error())
		return
	}
	ts := time.Now().UTC()
	sig := SignOutcomeHMAC(w.cfg.OutcomeWebhookSecret, ts, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.OutcomeWebhookURL, bytes.NewReader(body))
	if err != nil {
		w.fail(ctx, ev, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Warmbly-Signature", sig)
	req.Header.Set("X-Warmbly-Event-Id", env.EventID)
	req.Header.Set("Idempotency-Key", env.IdempotencyKey)

	resp, err := w.http.Do(req)
	if err != nil {
		w.fail(ctx, ev, err.Error())
		return
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := w.repo.MarkOutcomeDelivered(ctx, ev.OrganizationID, ev.ID); err != nil {
			log.Warn().Err(err).Str("id", ev.ID.String()).Msg("mark delivered failed")
		}
		return
	}
	w.fail(ctx, ev, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet))))
}

func (w *OutcomeDeliveryWorker) fail(ctx context.Context, ev *models.OutreachOutcome, reason string) {
	attempts := ev.Attempts + 1
	dead := attempts >= w.maxTries
	next := time.Now().UTC().Add(OutcomeBackoff(attempts))
	if err := w.repo.MarkOutcomeAttempt(ctx, ev.OrganizationID, ev.ID, attempts, next, reason, dead); err != nil {
		log.Warn().Err(err).Str("id", ev.ID.String()).Msg("mark attempt failed")
	}
}
