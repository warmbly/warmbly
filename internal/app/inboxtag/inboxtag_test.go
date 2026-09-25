package inboxtag

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// The fixtures are twenty real-shaped messages with the API's actual answers
// recorded next to them. The suite runs entirely offline against those cached
// responses, so the policy can be retuned and re-verified for free: re-running
// the model over the set costs money, re-running the arithmetic does not.
//
// A fixture's expectation is what the model actually returned, not what seemed
// likely when it was written. One of them ("sounds promising, can we jump on a
// call") was recorded as `agreed` rather than the `wants_info` first guessed,
// and the fixture records the model, because the point of pinning is to notice
// when the answer MOVES, not to encode an opinion about what it should be.

type fixture struct {
	Name     string `json:"name"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Previous string `json:"previous"`
	// Language is the workspace mail language the fixture was recorded with,
	// as a models.MailLanguageNames code. Empty for the English set.
	Language     string `json:"language,omitempty"`
	ExpectKind   string `json:"expect_kind"`
	ExpectIntent string `json:"expect_intent"`
}

// loadFixtures reads every testdata/fixtures*.json and responses*.json, so a
// set in another language is its own file next to the English one.
func loadFixtures(t *testing.T) ([]fixture, map[string]Response) {
	t.Helper()

	var fx []fixture
	files, _ := filepath.Glob(filepath.Join("testdata", "fixtures*.json"))
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var set []fixture
		if err := json.Unmarshal(raw, &set); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		fx = append(fx, set...)
	}

	responses := map[string]Response{}
	files, _ = filepath.Glob(filepath.Join("testdata", "responses*.json"))
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var set map[string]Response
		if err := json.Unmarshal(raw, &set); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for k, v := range set {
			responses[k] = v
		}
	}
	return fx, responses
}

func TestFixturesClassify(t *testing.T) {
	fx, responses := loadFixtures(t)
	if len(fx) < 20 {
		t.Fatalf("expected at least 20 fixtures, got %d", len(fx))
	}

	for _, f := range fx {
		t.Run(f.Name, func(t *testing.T) {
			resp, ok := responses[f.Name]
			if !ok {
				t.Fatalf("no cached response for %q", f.Name)
			}
			d := Decide(resp.Answers, Facts{})

			if d.Kind != f.ExpectKind {
				t.Errorf("kind = %q, want %q (confidence %.2f)", d.Kind, f.ExpectKind, d.KindConfidence)
			}
			if d.Intent != f.ExpectIntent {
				t.Errorf("intent = %q, want %q", d.Intent, f.ExpectIntent)
			}
			if d.Relevance < 0 || d.Relevance > 100 {
				t.Errorf("relevance %d out of range", d.Relevance)
			}
			if d.Priority == "" {
				t.Error("priority not set")
			}
		})
	}
}

// Intent is asked on every message and read on none but a human reply. On the
// recorded set, an out-of-office came back with intent "unclear" at 0.55 and a
// forwarded internal message with "wrong_person" at 0.50: both well under the
// floor, and both correctly ignored because the kind was not a human reply.
// Reading intent on those would have produced a confident-looking label from an
// answer to a question that had no subject.
func TestIntentIgnoredForNonHumanKinds(t *testing.T) {
	fx, responses := loadFixtures(t)
	checked := 0
	for _, f := range fx {
		if f.ExpectKind == KindHumanReply {
			continue
		}
		d := Decide(responses[f.Name].Answers, Facts{})
		if d.Intent != "" {
			t.Errorf("%s: intent %q read for kind %q", f.Name, d.Intent, d.Kind)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no non-human fixtures to check")
	}
}

// Relevance is ordered the way a person would order the inbox: someone agreeing
// beats someone asking questions, which beats a bounce.
func TestRelevanceOrdering(t *testing.T) {
	_, responses := loadFixtures(t)
	rel := func(name string) int { return Decide(responses[name].Answers, Facts{}).Relevance }

	agreed := rel("agreed_partnership")
	pricing := rel("wants_pricing")
	notInterested := rel("not_interested")
	bounce := rel("bounce_hard_body")

	if !(agreed > pricing) {
		t.Errorf("agreed (%d) should outrank wants_pricing (%d)", agreed, pricing)
	}
	if !(pricing > notInterested) {
		t.Errorf("wants_pricing (%d) should outrank not_interested (%d)", pricing, notInterested)
	}
	if bounce != 0 {
		t.Errorf("a hard bounce should floor at 0 relevance, got %d", bounce)
	}
	if p := Decide(responses["agreed_partnership"].Answers, Facts{}).Priority; p != PriorityNow {
		t.Errorf("an agreement should be priority now, got %q", p)
	}
}

// A Choice below the floor is not a weak signal, it is a non-reproducible one.
// Nothing downstream may read it.
func TestLowConfidenceStopsAtNeedsReview(t *testing.T) {
	answers := map[string]Answer{
		"kind":   {Type: QuestionChoice, Choice: KindHumanReply, Confidence: 0.21},
		"intent": {Type: QuestionChoice, Choice: IntentAgreed, Confidence: 0.99},
		// A signal that would otherwise add 50 points.
		SigAsksForCall: {Type: QuestionNoul, Noul: 0.99},
	}
	d := Decide(answers, Facts{})

	if !d.NeedsReview {
		t.Fatal("expected needs-review")
	}
	if d.ReviewReason != "kind" {
		t.Errorf("review reason = %q, want kind", d.ReviewReason)
	}
	if len(d.Labels) != 1 || d.Labels[0] != LabelNeedsReview {
		t.Errorf("labels = %v, want only %q", d.Labels, LabelNeedsReview)
	}
	if d.Intent != "" {
		t.Errorf("intent %q read despite an untrusted kind", d.Intent)
	}
	if d.Relevance != 0 {
		t.Errorf("relevance %d scored on an untrusted answer", d.Relevance)
	}
}

// The header layer is authoritative where it speaks: it reads what the sending
// system declared, while the model infers from prose. A body that reads like an
// enthusiastic human reply must still be filed as an auto-reply when the
// headers say it is one.
func TestDeterministicKindOverridesModel(t *testing.T) {
	answers := map[string]Answer{
		"kind":   {Type: QuestionChoice, Choice: KindHumanReply, Confidence: 0.99},
		"intent": {Type: QuestionChoice, Choice: IntentAgreed, Confidence: 0.99},
	}
	d := Decide(answers, Facts{DeterministicKind: KindAutoReplyOOO})

	if d.Kind != KindAutoReplyOOO {
		t.Errorf("kind = %q, want the header verdict %q", d.Kind, KindAutoReplyOOO)
	}
	if d.KindSource != "header" {
		t.Errorf("source = %q, want header", d.KindSource)
	}
	if d.Intent != "" {
		t.Errorf("intent %q read for an auto-reply", d.Intent)
	}
	if d.Relevance != 0 {
		t.Errorf("an auto-reply scored %d", d.Relevance)
	}
}

// ── Contract assertions ────────────────────────────────────────────────────

type countingAsker struct {
	calls     int
	questions int
	resp      Response
}

func (c *countingAsker) Ask(_ context.Context, _ any, q map[string]Question) (*Response, error) {
	c.calls++
	c.questions = len(q)
	r := c.resp
	return &r, nil
}

type fakeRepo struct {
	tagged    map[string]bool
	saved     []*repository.InboxTagResult
	untagged  []repository.BackfillCandidate
	cold      []repository.BackfillCandidate
	previous  string
	campaign  string
	inReplyTo []string
	reopened  []string
	states    []repository.ThreadFollowUpState
}

func (f *fakeRepo) Claim(_ context.Context, _, _ uuid.UUID, id, _ string) (bool, error) {
	if f.tagged == nil {
		f.tagged = map[string]bool{}
	}
	if f.tagged[id] {
		return false, nil
	}
	f.tagged[id] = true
	return true, nil
}
func (f *fakeRepo) ReleaseClaim(_ context.Context, _ uuid.UUID, id string) error {
	delete(f.tagged, id)
	return nil
}
func (f *fakeRepo) Save(_ context.Context, r *repository.InboxTagResult) error {
	if f.tagged == nil {
		f.tagged = map[string]bool{}
	}
	f.tagged[r.MessageID] = true
	f.saved = append(f.saved, r)
	return nil
}
func (f *fakeRepo) ListForReview(context.Context, uuid.UUID, int, int, bool) ([]repository.InboxTagResult, int, error) {
	return nil, 0, nil
}
func (f *fakeRepo) ReviewSummary(context.Context, uuid.UUID) (repository.InboxTagReviewSummary, error) {
	return repository.InboxTagReviewSummary{}, nil
}

func (f *fakeRepo) ListUntagged(context.Context, uuid.UUID, time.Time, int) ([]repository.BackfillCandidate, error) {
	return f.untagged, nil
}
func (f *fakeRepo) PreviousOutbound(_ context.Context, _ uuid.UUID, _ string, inReplyTo []string, _ time.Time) (string, string, error) {
	f.inReplyTo = inReplyTo
	return f.previous, f.campaign, nil
}
func (f *fakeRepo) ListColdInboundInCampaignThreads(context.Context, uuid.UUID, time.Time, int) ([]repository.BackfillCandidate, error) {
	return f.cold, nil
}
func (f *fakeRepo) Reopen(_ context.Context, _ uuid.UUID, id, _ string) ([]string, error) {
	delete(f.tagged, id)
	f.reopened = append(f.reopened, id)
	return []string{"cold-inbound", "needs-review"}, nil
}

func (f *fakeRepo) ThreadStates(context.Context, uuid.UUID, time.Time, int) ([]repository.ThreadFollowUpState, error) {
	return f.states, nil
}

func (f *fakeRepo) GetByMessageID(_ context.Context, _ uuid.UUID, id string) (*repository.InboxTagResult, error) {
	for _, r := range f.saved {
		if r.MessageID == id {
			return r, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) RecordActions(_ context.Context, _ uuid.UUID, id string, actions []string) error {
	for _, r := range f.saved {
		if r.MessageID == id {
			r.Actions = actions
		}
	}
	return nil
}

func newService(t *testing.T, asker Asker, repo repository.InboxTagRepository) *Service {
	t.Helper()
	return NewService(asker, repo, nil, nil, true)
}

func inboundMessage() Message {
	return Message{
		OrganizationID: uuid.New(),
		EmailAccountID: uuid.New(),
		MessageID:      "<abc@example.com>",
		ThreadID:       "t-1",
		Subject:        "Re: Collaboration idea",
		BodyText:       "Yes let's do it, happy to go ahead with the partnership this week.",
		FromAddr:       "Jane <jane@example.com>",
	}
}

// ONE call per email, with every question in it. A loop per tag is the bug this
// asserts against: the questions do not see each other, so twelve cost what one
// costs, and splitting them multiplies latency and spend for nothing.
func TestOneCallPerEmailWithAllQuestions(t *testing.T) {
	_, responses := loadFixtures(t)
	asker := &countingAsker{resp: responses["agreed_partnership"]}
	svc := newService(t, asker, &fakeRepo{})

	if _, err := svc.Classify(context.Background(), inboundMessage()); err != nil {
		t.Fatalf("classify: %v", err)
	}

	if asker.calls != 1 {
		t.Fatalf("made %d calls, want exactly 1", asker.calls)
	}
	if want := len(Questions()); asker.questions != want {
		t.Fatalf("sent %d questions, want all %d in the one call", asker.questions, want)
	}
}

// ZERO calls for our own outbound. The model, given only a body, called our own
// send a human reply at 0.94 confidence: confidently wrong, and no confidence
// threshold can catch it, because the answer is right for the question and the
// question should never have been asked.
func TestZeroCallsForOutbound(t *testing.T) {
	_, responses := loadFixtures(t)
	asker := &countingAsker{resp: responses["agreed_partnership"]}
	svc := newService(t, asker, &fakeRepo{})

	m := inboundMessage()
	m.Outbound = true

	d, err := svc.Classify(context.Background(), m)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if asker.calls != 0 {
		t.Fatalf("made %d calls for our own outbound, want 0", asker.calls)
	}
	if !d.Skipped() {
		t.Error("expected the decision to report itself skipped")
	}
}

// Idempotent per message id: webhooks retry and a folder re-sync replays the
// same message. The second delivery must cost nothing.
func TestIdempotentPerMessageID(t *testing.T) {
	_, responses := loadFixtures(t)
	asker := &countingAsker{resp: responses["agreed_partnership"]}
	repo := &fakeRepo{}
	svc := newService(t, asker, repo)

	m := inboundMessage()
	for i := 0; i < 3; i++ {
		if _, err := svc.Classify(context.Background(), m); err != nil {
			t.Fatalf("classify %d: %v", i, err)
		}
	}
	if asker.calls != 1 {
		t.Fatalf("made %d calls for the same message id, want 1", asker.calls)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("saved %d rows for the same message id, want 1", len(repo.saved))
	}
}

// The raw probabilities are persisted, not just the conclusions. Retuning the
// weights against stored answers is free; re-running the model is not.
func TestRawProbabilitiesPersisted(t *testing.T) {
	_, responses := loadFixtures(t)
	asker := &countingAsker{resp: responses["wants_pricing"]}
	repo := &fakeRepo{}
	svc := newService(t, asker, repo)

	if _, err := svc.Classify(context.Background(), inboundMessage()); err != nil {
		t.Fatalf("classify: %v", err)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("saved %d rows, want 1", len(repo.saved))
	}

	var stored map[string]Answer
	if err := json.Unmarshal(repo.saved[0].Answers, &stored); err != nil {
		t.Fatalf("stored answers are not readable: %v", err)
	}
	kind, ok := stored["kind"]
	if !ok {
		t.Fatal("kind answer not stored")
	}
	if len(kind.Probabilities) == 0 {
		t.Error("stored the winning label but not the distribution behind it")
	}
	for _, id := range ScoreIDs() {
		if _, ok := stored[id]; !ok {
			t.Errorf("score %q not stored", id)
		}
	}
}

// Disabled is the default, and a disabled service reaches nothing.
func TestDisabledMakesNoCalls(t *testing.T) {
	asker := &countingAsker{}
	svc := NewService(asker, &fakeRepo{}, nil, nil, false)

	if svc.Enabled() {
		t.Fatal("service reports enabled when it was constructed off")
	}
	if _, err := svc.Classify(context.Background(), inboundMessage()); err != nil {
		t.Fatalf("classify: %v", err)
	}
	if asker.calls != 0 {
		t.Fatalf("made %d calls while disabled", asker.calls)
	}
}

// A failure notice is a bounce, not a helpdesk receipt, and a delay notice is
// a soft one. Decided from the headers, with no model call.
func TestDeterministicKindReadsBounces(t *testing.T) {
	daemon := map[string][]string{"From": {"Mail Delivery Subsystem <mailer-daemon@googlemail.com>"}}
	hard := deterministicKind(Message{Headers: daemon, Subject: "Delivery Status Notification (Failure)", BodyText: "The group may not exist."}, nil)
	if hard != KindBounceHard {
		t.Errorf("failure notice kind = %q, want %q", hard, KindBounceHard)
	}
	soft := deterministicKind(Message{Headers: daemon, Subject: "Delivery Status Notification (Delay)", BodyText: "Delivery is delayed; we will retry."}, nil)
	if soft != KindBounceSoft {
		t.Errorf("delay notice kind = %q, want %q", soft, KindBounceSoft)
	}
	if got := deterministicKind(Message{Headers: map[string][]string{"From": {"Jane <jane@example.org>"}}, Subject: "Re: Hi", BodyText: "Sounds good"}, nil); got != "" {
		t.Errorf("a person's reply kind = %q, want it left to the model", got)
	}
}

// The tagger sees the headers the sync carried as pseudo-flags, so a failure
// notice is a bounce before any model is asked.
func TestMessageFromReadsSyncedHeaders(t *testing.T) {
	m := MessageFrom(uuid.New(), uuid.New(), &models.EmailMessageStoreData{
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Notifier <alerts@example.org>"},
		Subject:   "Your message",
		Flags:     []string{"\\Seen", "X-Failed-Recipients:info@example.org"},
		InReplyTo: []string{"<a@example.test>"},
	}, nil, "", "")
	if deterministicKind(m, nil) != KindBounceHard {
		t.Errorf("kind = %q, want %q from the synced header", deterministicKind(m, nil), KindBounceHard)
	}
	if len(m.InReplyTo) != 1 {
		t.Errorf("InReplyTo not carried: %v", m.InReplyTo)
	}
}

// A no-reply security alert is a notification, and only machine mail that
// answers something is an auto-reply. The sender alone decides it, which is
// all the backfill has.
func TestDeterministicKindTellsNoticesFromAutoReplies(t *testing.T) {
	alert := deterministicKind(Message{FromAddr: "Google <no-reply@accounts.google.com>", Subject: "Security alert", BodyText: "2-Step Verification turned on"}, nil)
	if alert != KindNotification {
		t.Errorf("security alert kind = %q, want %q", alert, KindNotification)
	}
	ack := deterministicKind(Message{
		FromAddr:  "Support <noreply@helpdesk.example>",
		Subject:   "Re: Partnership",
		BodyText:  "We received your request.",
		InReplyTo: []string{"<ours@example.test>"},
	}, nil)
	if ack != KindAutoReplyTicket {
		t.Errorf("ticket receipt kind = %q, want %q", ack, KindAutoReplyTicket)
	}
}
