package warmupcontent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

type controlRepo struct {
	repository.WarmupContentRepository
	settings   models.WarmupGenerationSettings
	jobs       []models.WarmupGenerationJob
	records    map[uuid.UUID]models.WarmupConversation
	reserved   int
	reserveErr error
	countErr   error
	available  int
	insertErr  error
}

func (*controlRepo) ExpireStaleScheduledJobs(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (*controlRepo) RetireRiskyConversations(context.Context, time.Time, int, int, float64) (int, error) {
	return 0, nil
}
func (*controlRepo) WarmupMailboxDemandSince(context.Context, time.Time) (int, error) { return 0, nil }
func (r *controlRepo) CountAvailableConversations(context.Context, string, string) (int, error) {
	return r.available, nil
}

func (r *controlRepo) GetGenerationSettings(context.Context) (*models.WarmupGenerationSettings, error) {
	s := r.settings
	return &s, nil
}
func (r *controlRepo) ReservedGenerationCountSince(context.Context, time.Time) (int, error) {
	return r.reserved, r.countErr
}
func (r *controlRepo) CreateGenerationJob(_ context.Context, j *models.WarmupGenerationJob) error {
	if r.reserveErr != nil {
		return r.reserveErr
	}
	r.jobs = append(r.jobs, *j)
	return nil
}
func (r *controlRepo) UpdateGenerationJob(_ context.Context, j *models.WarmupGenerationJob) error {
	for i := range r.jobs {
		if r.jobs[i].ID == j.ID {
			r.jobs[i] = *j
			return nil
		}
	}
	return errors.New("missing job")
}
func (r *controlRepo) ListActiveBatchJobs(context.Context) ([]models.WarmupGenerationJob, error) {
	return append([]models.WarmupGenerationJob(nil), r.jobs...), nil
}
func (r *controlRepo) GetGenerationJob(_ context.Context, id uuid.UUID) (*models.WarmupGenerationJob, error) {
	for _, j := range r.jobs {
		if j.ID == id {
			return &j, nil
		}
	}
	return nil, nil
}
func (r *controlRepo) MarkBatchCancelling(_ context.Context, id uuid.UUID, reason string) (bool, error) {
	for i := range r.jobs {
		if r.jobs[i].ID == id {
			r.jobs[i].BatchStatus = "cancelling"
			r.jobs[i].Error = reason
			return true, nil
		}
	}
	return false, nil
}
func (r *controlRepo) InsertConversation(_ context.Context, c *models.WarmupConversation) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	if r.records == nil {
		r.records = map[uuid.UUID]models.WarmupConversation{}
	}
	r.records[c.ID] = *c
	return nil
}

func TestBatchCountsPriorLibraryDuplicatesAsRejectedContent(t *testing.T) {
	s, r, p := newControlFixture()
	version := generation.WarmupScenarioVersion
	job := models.WarmupGenerationJob{ID: uuid.New(), Model: "configured-model", RequestedCount: 1, ContentVersion: &version}
	r.jobs = append(r.jobs, job)
	r.insertErr = repository.ErrWarmupConversationDuplicate
	p.review = &generation.WarmupReview{Coherent: true, Confidence: 0.95}
	p.results = []generation.BatchResult{diagnosticResult(job)}
	if err := s.ingestBatch(t.Context(), &r.jobs[0], "output", generation.BatchCounts{Total: 1}); err != nil {
		t.Fatal(err)
	}
	if r.jobs[0].GeneratedCount != 0 || r.jobs[0].LintRejectedCount != 1 || r.jobs[0].FailedCount != 0 {
		t.Fatalf("duplicate batch outcome: %+v", r.jobs[0])
	}
}
func (r *controlRepo) GetConversation(_ context.Context, id uuid.UUID) (*models.WarmupConversation, error) {
	c, ok := r.records[id]
	if !ok {
		return nil, nil
	}
	return &c, nil
}

type controlProvider struct {
	model     string
	modelErr  error
	requests  []generation.BatchRequest
	state     generation.BatchState
	results   []generation.BatchResult
	fetchErr  error
	polls     int
	cancels   int
	review    *generation.WarmupReview
	reviewErr error
}

func (p *controlProvider) CheckModel(_ context.Context, model string) error {
	p.model = model
	return p.modelErr
}
func (p *controlProvider) SubmitBatch(_ context.Context, req []generation.BatchRequest, _ string) (string, string, error) {
	p.requests = req
	return "existing-provider-batch", "existing-input", nil
}
func (p *controlProvider) GetBatch(context.Context, string) (generation.BatchState, error) {
	p.polls++
	return p.state, nil
}
func (p *controlProvider) FetchBatchResults(context.Context, string) ([]generation.BatchResult, error) {
	return p.results, p.fetchErr
}
func (p *controlProvider) CancelBatch(context.Context, string) error { p.cancels++; return nil }

func (p *controlProvider) ReviewConversation(context.Context, generation.Conversation, string) (*generation.WarmupReview, error) {
	return p.review, p.reviewErr
}

func newControlFixture() (*service, *controlRepo, *controlProvider) {
	r := &controlRepo{settings: models.DefaultWarmupGenerationSettings()}
	p := &controlProvider{}
	return &service{repo: r, gen: p}, r, p
}
func diagnosticResult(job models.WarmupGenerationJob) generation.BatchResult {
	return generation.BatchResult{CustomID: fmt.Sprintf("%s-0", job.ID), Conversation: &generation.Conversation{Subject: "A card count", Description: "We sorted the supply cupboard and found three boxes of index cards nobody remembers ordering. Do you still plan on cards?", Messages: []string{"I do, mostly for sketching out a week at a glance.", "Same here. Good to know I'm not the only one."}}}
}

func TestGenerationStopBlocksBeforeProviderAndReservation(t *testing.T) {
	s, r, p := newControlFixture()
	r.settings.GenerationEnabled = false
	if _, err := s.GenerateBatch(context.Background(), GenerateRequest{Count: 1}); !errors.Is(err, ErrGenerationStopped) {
		t.Fatalf("stop: %v", err)
	}
	if err := s.RunScheduled(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.model != "" || len(p.requests) != 0 || len(r.jobs) != 0 {
		t.Fatal("stop touched provider or reserved job")
	}
}

func TestScheduledGenerationReplenishesEligiblePoolCapacity(t *testing.T) {
	s, r, p := newControlFixture()
	r.settings.RefreshEnabled = false
	r.available = 200
	if err := s.RunScheduled(t.Context()); err != nil || len(p.requests) != 0 {
		t.Fatalf("full pool generated unnecessarily: %v", err)
	}
	r.available = 199
	if err := s.RunScheduled(t.Context()); err != nil || len(p.requests) != 1 {
		t.Fatalf("depleted capacity did not replenish automatically: requests=%d err=%v", len(p.requests), err)
	}
}

func TestBatchReviewsWithOpenAIWhenTypeSafeIsAbsent(t *testing.T) {
	for _, review := range []*generation.WarmupReview{
		{Coherent: true, Confidence: 0.95},
		{Coherent: true, Confidence: 0.5},
		{Coherent: true, ContainsPitch: true, Confidence: 0.95},
		{Coherent: true, Unnatural: true, Confidence: 0.95},
		nil,
	} {
		s, r, p := newControlFixture()
		version := generation.WarmupScenarioVersion
		job := models.WarmupGenerationJob{ID: uuid.New(), Model: "configured-model", RequestedCount: 1, ContentVersion: &version}
		r.jobs = append(r.jobs, job)
		p.review = review
		result := diagnosticResult(job)
		result.Conversation.Subject = "A card count"
		result.Conversation.Description = "Our team switched to index cards for planning the week. Have you tried that?"
		p.results = []generation.BatchResult{result}
		if err := s.ingestBatch(t.Context(), &r.jobs[0], "output", generation.BatchCounts{Total: 1}); err != nil {
			t.Fatal(err)
		}
		got := r.records[batchConversationID(job.ID, result.CustomID)]
		approved := review != nil && review.Coherent && !review.ContainsPitch && !review.Unnatural && review.Confidence >= judgeConfFloor
		if (got.Status == models.WarmupConversationActive) != approved || got.ReplyEligible != approved {
			t.Fatalf("semantic review bypassed: review=%+v record=%+v", review, got)
		}
	}
}

func TestBatchRejectsRepeatedSubjectsAndOpenings(t *testing.T) {
	s, r, p := newControlFixture()
	version := generation.WarmupScenarioVersion
	job := models.WarmupGenerationJob{ID: uuid.New(), Model: "configured-model", RequestedCount: 3, ContentVersion: &version}
	r.jobs = append(r.jobs, job)
	p.review = &generation.WarmupReview{Coherent: true, Confidence: 0.95}
	for i := range 3 {
		result := diagnosticResult(job)
		result.CustomID = fmt.Sprintf("%s-%d", job.ID, i)
		if i == 1 {
			result.Conversation.Subject = "  A   CARD COUNT "
			result.Conversation.Description += " A different opening."
		}
		if i == 2 {
			result.Conversation.Subject = "A different subject"
		}
		p.results = append(p.results, result)
	}
	if err := s.ingestBatch(t.Context(), &r.jobs[0], "output", generation.BatchCounts{Total: 3}); err != nil {
		t.Fatal(err)
	}
	if r.jobs[0].GeneratedCount != 1 || r.jobs[0].LintRejectedCount != 2 || len(r.records) != 1 {
		t.Fatalf("duplicate content accepted: %+v", r.jobs[0])
	}
}

func TestBatchKeepsHistoricalQueuedOutputWithoutReactivatingBoilerplate(t *testing.T) {
	s, r, p := newControlFixture()
	version := generation.DiagnosticScenarioVersion
	job := models.WarmupGenerationJob{ID: uuid.New(), Model: "configured-model", RequestedCount: 1, ContentVersion: &version}
	r.jobs = append(r.jobs, job)
	result := diagnosticResult(job)
	result.Conversation.Subject = "Simulated diagnostic: sample count"
	result.Conversation.Description = "Simulated diagnostic. A hypothetical sample has 5 cards. How many cards is that?"
	p.results = []generation.BatchResult{result}
	if err := s.ingestBatch(t.Context(), &r.jobs[0], "output", generation.BatchCounts{Total: 1}); err != nil {
		t.Fatal(err)
	}
	got := r.records[batchConversationID(job.ID, result.CustomID)]
	if got.Subject != result.Conversation.Subject || got.Description != result.Conversation.Description || got.Status != models.WarmupConversationArchived || got.SemanticReview != "rejected" || got.ReplyEligible {
		t.Fatalf("historical queued output was lost or reactivated: %+v", got)
	}
}

func TestGenerationReservationGateAndConfiguredModel(t *testing.T) {
	s, r, p := newControlFixture()
	r.settings.Model = "stored-provider-model"
	r.reserved = 999
	if _, err := s.GenerateBatch(context.Background(), GenerateRequest{Count: 20, MaxMessages: 2}); err != nil {
		t.Fatal(err)
	}
	if p.model != r.settings.Model || len(p.requests) != 1 || r.jobs[0].ContentVersion == nil {
		t.Fatal("model substituted, cap ignored or job unversioned")
	}
	if p.requests[0].MaxMessages != 2 || r.jobs[0].MaxMessagesPerThread == nil || *r.jobs[0].MaxMessagesPerThread != 2 {
		t.Fatal("requested turn count changed or not retained")
	}
	s, r, p = newControlFixture()
	r.reserveErr = ErrGenerationStopped
	if _, err := s.GenerateBatch(context.Background(), GenerateRequest{Count: 1}); !errors.Is(err, ErrGenerationStopped) || len(p.requests) != 0 {
		t.Fatalf("reservation stop bypassed: %v", err)
	}
	s, r, p = newControlFixture()
	r.countErr = errors.New("storage outage")
	if _, err := s.GenerateBatch(context.Background(), GenerateRequest{Count: 1}); err == nil || p.model != "" {
		t.Fatal("cap storage error authorized generation")
	}
	s, _, p = newControlFixture()
	p.modelErr = errors.New("model unavailable")
	if _, err := s.GenerateBatch(context.Background(), GenerateRequest{Count: 1}); err == nil || len(p.requests) != 0 {
		t.Fatal("unavailable model submitted")
	}
}

func TestStoppedGenerationDrainsAndCancelsOutstandingBatches(t *testing.T) {
	for _, review := range []string{"passed", "unavailable", "rejected", "legacy_unreviewed"} {
		t.Run(review, func(t *testing.T) {
			s, r, p := newControlFixture()
			version := generation.WarmupScenarioVersion
			j := models.WarmupGenerationJob{ID: uuid.New(), Mode: models.WarmupGenerationModeBatch, Status: "running", BatchID: "existing-provider-id", RequestedCount: 1, ContentVersion: &version}
			if review == "legacy_unreviewed" {
				j.ContentVersion = nil
			}
			r.jobs = []models.WarmupGenerationJob{j}
			r.settings.GenerationEnabled = false
			if review == "passed" {
				s.judge = &fakeAsker{resp: judgeResponse(0, 0, 2, 0.99)}
			}
			if review == "rejected" {
				s.judge = &fakeAsker{resp: judgeResponse(0, 0, 0, 0.99)}
			}
			if err := s.CancelBatch(context.Background(), j.ID); err != nil {
				t.Fatal(err)
			}
			p.state = generation.BatchState{Status: "cancelled", OutputFileID: "old-output", Counts: generation.BatchCounts{Total: 1}}
			result := diagnosticResult(j)
			if review == "legacy_unreviewed" {
				result.Conversation.Subject = "Old stored subject"
				result.Conversation.Description = "Old ordered opening"
			}
			p.results = []generation.BatchResult{result}
			if err := s.PollBatches(context.Background()); err != nil {
				t.Fatal(err)
			}
			if p.polls != 1 || p.cancels != 1 || len(r.records) != 1 || r.jobs[0].BatchID != j.BatchID {
				t.Fatal("existing stopped batch not drained or IDs changed")
			}
			c := r.records[batchConversationID(j.ID, result.CustomID)]
			if c.SemanticReview != review || c.ReplyEligible != (review == "passed") {
				t.Fatalf("false semantic approval: %+v", c)
			}
			if review != "passed" && c.Status != models.WarmupConversationArchived {
				t.Fatal("unreviewed content active")
			}
			if review == "legacy_unreviewed" && (c.Subject != "Old stored subject" || c.ScenarioVersion != nil) {
				t.Fatal("legacy output replaced or relabeled")
			}
			if review == "passed" && (c.Description != result.Conversation.Description || c.RenderingVersion == nil) {
				t.Fatal("canonical text or provenance lost")
			}
			if err := s.ingestBatch(context.Background(), &r.jobs[0], "old-output", p.state.Counts); err != nil {
				t.Fatal(err)
			}
			if len(r.records) != 1 {
				t.Fatal("re-ingestion duplicated output")
			}
		})
	}
}

func TestBatchCustomIDScopeAndReplayAreCanonical(t *testing.T) {
	s, r, p := newControlFixture()
	j := models.WarmupGenerationJob{ID: uuid.New(), PoolType: "premium", RequestedCount: 1}
	r.jobs = append(r.jobs, j)
	valid := diagnosticResult(j)
	alias, foreign := valid, valid
	alias.CustomID = j.ID.String() + "-00"
	foreign.CustomID = uuid.New().String() + "-0"
	p.results = []generation.BatchResult{alias, foreign, valid, valid}
	if err := s.ingestBatch(context.Background(), &r.jobs[0], "output", generation.BatchCounts{Total: 1}); err != nil {
		t.Fatal(err)
	}
	if len(r.records) != 1 || r.jobs[0].GeneratedCount != 1 || r.jobs[0].FailedCount != 0 {
		t.Fatal("aliased, foreign or replayed custom ID changed reservation accounting")
	}
}

func TestBatchRequestedTurnCountSurvivesSettingsChange(t *testing.T) {
	s, r, p := newControlFixture()
	version, turns := generation.WarmupScenarioVersion, 1
	j := models.WarmupGenerationJob{ID: uuid.New(), Mode: "batch", Status: "running", BatchID: "existing-provider-batch", PoolType: "premium", RequestedCount: 1, ContentVersion: &version, MaxMessagesPerThread: &turns}
	r.settings.MaxMessagesPerThread = 5
	r.jobs = append(r.jobs, j)
	p.state = generation.BatchState{Status: "completed", OutputFileID: "output", Counts: generation.BatchCounts{Total: 1}}
	p.results = []generation.BatchResult{diagnosticResult(j)}
	if err := s.PollBatches(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.records) != 0 || r.jobs[0].LintRejectedCount != 1 {
		t.Fatal("current settings reinterpreted accepted job reply count")
	}
}

func TestBatchDownloadOutageKeepsAcceptedJobRetryable(t *testing.T) {
	s, r, p := newControlFixture()
	j := models.WarmupGenerationJob{ID: uuid.New(), Status: "running", BatchStatus: "in_progress"}
	r.jobs = []models.WarmupGenerationJob{j}
	p.fetchErr = errors.New("secret provider response")
	if err := s.ingestBatch(context.Background(), &j, "file", generation.BatchCounts{}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error privacy: %v", err)
	}
	if r.jobs[0].Status != "running" || r.jobs[0].BatchStatus != "in_progress" {
		t.Fatal("transient download failure discarded accepted output")
	}
}

func TestCanonicalTransformKeepsEmptyTurnsAndFacts(t *testing.T) {
	subject, opening, turns := preserveCanonicalThread(" Date ", " Ada will not meet on 2026-10-08 at 14:00. ", []string{"", " No meeting was scheduled. "})
	if subject != "Date" || opening != "Ada will not meet on 2026-10-08 at 14:00." || len(turns) != 2 || turns[0] != "" || turns[1] != "No meeting was scheduled." {
		t.Fatal("facts or alternating positions changed")
	}
}
