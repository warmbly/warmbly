package inboxtag

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/dsn"
	"github.com/warmbly/warmbly/internal/repository"
)

// PHASE 1. This service writes labels and a relevance score. It does not
// snooze, does not hold a lead, does not create a task, and does not suppress
// anything. Those are phases 2 and 3 and they are separate changes on purpose:
// a wrongly suppressed address is silent and permanent, so nothing earns that
// power until a human has watched the labels be right for a week.
//
// There is a test asserting this service reaches none of those primitives. If
// you are adding one, you are starting phase 2 and the test should be the first
// thing you change.

// Asker is the model call, narrowed to what the service uses so tests can
// supply a cached response instead of a network.
type Asker interface {
	Ask(ctx context.Context, state any, questions map[string]Question) (*Response, error)
}

// Categories maps label slugs to the workspace category rows they file under.
// set_thread_labels takes category UUIDs, not strings, so the slug -> uuid map
// is resolved once and cached rather than rebuilt per message.
type Categories interface {
	EnsureCategory(ctx context.Context, orgID uuid.UUID, slug string) (uuid.UUID, error)
	// EnsureAll creates the whole taxonomy up front, so every label is
	// filterable from the moment the feature is on rather than appearing one
	// at a time as each first fires.
	EnsureAll(ctx context.Context, orgID uuid.UUID, slugs []string) error
	// AddThreadLabels is additive on purpose. Automatic tagging must never
	// remove a label a teammate applied by hand, and a "set" would do exactly
	// that on every re-classification.
	AddThreadLabels(ctx context.Context, orgID uuid.UUID, threadID string, categoryIDs []uuid.UUID) error
	// SyncExclusiveLabels makes one label from a family the only one on a
	// thread. Follow-up states change with the calendar, so they are replaced
	// rather than accumulated; nothing outside the named family is touched.
	SyncExclusiveLabels(ctx context.Context, orgID uuid.UUID, threadID string, family []string, want string) error
}

// MailboxAddresses answers "is this one of ours", which is a fact and must
// never be a question.
type MailboxAddresses interface {
	IsOwnAddress(ctx context.Context, orgID uuid.UUID, addr string) (bool, error)
}

type Service struct {
	asker      Asker
	repo       repository.InboxTagRepository
	categories Categories
	mailboxes  MailboxAddresses
	// enabled gates the whole feature. Off by default, and off whenever no API
	// key is configured, so an instance that never heard of TypeSafe behaves
	// exactly as it did before.
	enabled bool

	// seeded remembers which workspaces already have the full taxonomy, so the
	// label set is created once per process rather than per message.
	seededMu sync.Mutex
	seeded   map[uuid.UUID]bool
}

func NewService(asker Asker, repo repository.InboxTagRepository, categories Categories, mailboxes MailboxAddresses, enabled bool) *Service {
	return &Service{asker: asker, repo: repo, categories: categories, mailboxes: mailboxes, enabled: enabled}
}

func (s *Service) Enabled() bool {
	return s != nil && s.enabled && s.asker != nil && s.repo != nil
}

// Message is the projection the service needs. Built by the caller from the
// stored message; the service never reads the database for it.
type Message struct {
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	EmailAccountID uuid.UUID
	MessageID      string
	ThreadID       string
	Subject        string
	BodyText       string
	FromAddr       string
	Headers        map[string][]string
	// InReplyTo names the message this one answers.
	InReplyTo []string
	// PreviousMessage is our last outbound in this thread, so a bare "yes" has
	// something to be an answer to.
	PreviousMessage string
	Campaign        string
	// Outbound is set by the caller from the folder, not guessed from content.
	Outbound bool
}

// Classify runs the whole pipeline for one inbound message. Safe to call on
// anything: it decides for itself whether there is a call to make.
//
// Returns the decision for logging and tests. Errors are returned but callers
// treat them as best-effort: tagging must never block inbox ingest.
func (s *Service) Classify(ctx context.Context, m Message) (Decision, error) {
	if !s.Enabled() {
		return Decision{}, nil
	}

	// 1. Our own mail. Checked before anything else and before any call: given
	// only a body, the model calls our own outbound a human reply at 0.94
	// confidence. This is the filter that makes that impossible.
	if m.Outbound || s.isOwn(ctx, m) {
		return DecideOutbound(), nil
	}

	// 2. Claim before the network call so concurrent deliveries spend once.
	claimed, err := s.repo.Claim(ctx, m.OrganizationID, m.EmailAccountID, m.MessageID, m.ThreadID)
	if err != nil {
		return Decision{}, err
	}
	if !claimed {
		return Decision{}, nil
	}
	release := func() { _ = s.repo.ReleaseClaim(context.Background(), m.OrganizationID, m.MessageID) }

	// 3. Check deterministic subject, sender, and any supplied header signals
	// before spending a model call.
	facts := Facts{DeterministicKind: deterministicKind(m)}

	state := BuildState(m.Subject, m.BodyText, m.PreviousMessage, m.Campaign)

	var resp *Response
	if facts.DeterministicKind == "" {
		if !HasContent(state) {
			release()
			return Decision{}, nil
		}
		// 4. Send every question in one request to avoid repeated state ingest.
		resp, err = s.asker.Ask(ctx, state, Questions())
		if err != nil {
			release()
			return Decision{}, err
		}
	}

	answers := map[string]Answer{}
	model := ""
	tokens := 0
	if resp != nil {
		answers = resp.Answers
		model = resp.Model
		tokens = resp.Usage.InputTokens
	}

	// 5. Code decides. Nothing above this line chose a label.
	decision := Decide(answers, facts)

	if err := s.persist(ctx, m, decision, answers, model, tokens); err != nil {
		release()
		return decision, err
	}

	// 6. The only side effect in this phase.
	if err := s.applyLabels(ctx, m, decision); err != nil {
		// The verdict is already stored, so a label that failed to apply is
		// visible on the review page rather than lost.
		log.Warn().Err(err).Str("thread_id", m.ThreadID).Msg("inbox tagging: labels not applied")
	}

	return decision, nil
}

func (s *Service) isOwn(ctx context.Context, m Message) bool {
	if s.mailboxes == nil || m.FromAddr == "" {
		return false
	}
	own, err := s.mailboxes.IsOwnAddress(ctx, m.OrganizationID, m.FromAddr)
	if err != nil {
		// Fail closed: an unknown answer here means we might be about to
		// classify our own send, which is the one wrong answer that looks
		// entirely plausible.
		log.Warn().Err(err).Msg("inbox tagging: could not resolve sender ownership, skipping")
		return true
	}
	return own
}

// deterministicKind maps the offline classifier's verdict onto this taxonomy.
// Only the classes headers decide definitively are mapped; everything else
// falls through to the model.
func deterministicKind(m Message) string {
	headers := m.Headers
	if len(headers["From"]) == 0 && m.FromAddr != "" {
		headers = make(map[string][]string, len(m.Headers)+1)
		for k, v := range m.Headers {
			headers[k] = v
		}
		headers["From"] = []string{m.FromAddr}
	}
	in := replyclassify.Input{
		Headers:  headers,
		Subject:  m.Subject,
		BodyText: m.BodyText,
	}
	if replyclassify.IsDeliveryFailure(in) {
		if dsn.IsTransientNotice(m.Subject, m.BodyText) {
			return KindBounceSoft
		}
		return KindBounceHard
	}
	res := replyclassify.ClassifyOffline(in)
	switch res.Class {
	case replyclassify.ClassOutOfOffice:
		return KindAutoReplyOOO
	case replyclassify.ClassAutoReply:
		// The header layer's auto-reply is any machine mail. Only an answer to
		// something is an auto-reply; a no-reply alert or a list mailing is a
		// notification.
		if isAnswer(m) {
			return KindAutoReplyTicket
		}
		return KindNotification
	default:
		// Positive, negative and neutral are lexicon guesses about a human
		// reply, not statements about what the message is. They are exactly
		// the judgment the model is better at, so they are not mapped.
		return ""
	}
}

// isAnswer reports a message that replies to an earlier one.
func isAnswer(m Message) bool {
	if len(m.InReplyTo) > 0 {
		return true
	}
	subject := strings.ToLower(strings.TrimSpace(m.Subject))
	return strings.HasPrefix(subject, "re:") || strings.HasPrefix(subject, "aw:") || strings.HasPrefix(subject, "sv:")
}

func (s *Service) persist(ctx context.Context, m Message, d Decision, answers map[string]Answer, model string, tokens int) error {
	raw, err := json.Marshal(answers)
	if err != nil {
		raw = json.RawMessage(`{}`)
	}
	return s.repo.Save(ctx, &repository.InboxTagResult{
		OrganizationID:   m.OrganizationID,
		EmailAccountID:   m.EmailAccountID,
		MessageID:        m.MessageID,
		ThreadID:         m.ThreadID,
		Kind:             d.Kind,
		KindConfidence:   d.KindConfidence,
		KindSource:       d.KindSource,
		Intent:           d.Intent,
		IntentConfidence: d.IntentConfidence,
		Relevance:        d.Relevance,
		Priority:         d.Priority,
		NeedsReview:      d.NeedsReview,
		ReviewReason:     d.ReviewReason,
		Automated:        d.Automated(),
		Answers:          raw,
		Labels:           d.Labels,
		Model:            model,
		InputTokens:      tokens,
	})
}

// seedTaxonomy creates every label once per workspace per process, so the
// filter offers the full set rather than only what has fired so far.
func (s *Service) seedTaxonomy(ctx context.Context, orgID uuid.UUID) {
	if s.categories == nil {
		return
	}
	s.seededMu.Lock()
	seeded := s.seeded[orgID]
	s.seededMu.Unlock()
	if seeded {
		return
	}
	if err := s.categories.EnsureAll(ctx, orgID, AllLabels()); err != nil {
		// Not fatal: the per-label EnsureCategory below still creates whatever
		// this decision needs. Only the "all labels visible" guarantee is lost
		// until the next attempt.
		log.Warn().Err(err).Msg("inbox tagging: could not seed the label taxonomy")
		return
	}
	s.seededMu.Lock()
	if s.seeded == nil {
		s.seeded = map[uuid.UUID]bool{}
	}
	s.seeded[orgID] = true
	s.seededMu.Unlock()
}

func (s *Service) applyLabels(ctx context.Context, m Message, d Decision) error {
	if s.categories == nil || m.ThreadID == "" || len(d.Labels) == 0 {
		return nil
	}
	s.seedTaxonomy(ctx, m.OrganizationID)
	ids := make([]uuid.UUID, 0, len(d.Labels))
	for _, label := range d.Labels {
		id, err := s.categories.EnsureCategory(ctx, m.OrganizationID, label)
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}
	return s.categories.AddThreadLabels(ctx, m.OrganizationID, m.ThreadID, ids)
}

// MessageFrom projects a stored inbound message into the service's input.
// Direction comes from the folder the message is in, never from its content.
func MessageFrom(orgID, userID uuid.UUID, msg *models.EmailMessageStoreData, headers map[string][]string, previous, campaign string) Message {
	from := ""
	if len(msg.FromAddr) > 0 {
		from = strings.TrimSpace(msg.FromAddr[0])
	}
	if headers == nil {
		// The sync carries the classification headers as pseudo-flags.
		headers = replyclassify.FlagHeaders(msg.Flags)
	}
	if from != "" && len(headers["From"]) == 0 {
		headers["From"] = []string{from}
	}
	return Message{
		OrganizationID:  orgID,
		UserID:          userID,
		EmailAccountID:  msg.EmailID,
		MessageID:       msg.MessageID,
		ThreadID:        msg.ThreadID,
		Subject:         msg.Subject,
		BodyText:        msg.BodyText,
		FromAddr:        from,
		Headers:         headers,
		InReplyTo:       msg.InReplyTo,
		PreviousMessage: previous,
		Campaign:        campaign,
		Outbound:        !msg.MayBeInbound(),
	}
}

// ── Backfill ───────────────────────────────────────────────────────────────

// BackfillProgress is reported after each message so a long run says what it
// is doing rather than going quiet for an hour.
type BackfillProgress struct {
	Considered int
	Classified int
	Skipped    int
	Failed     int
	Tokens     int
}

// BackfillOptions bound one run.
type BackfillOptions struct {
	// Since is the oldest message to consider.
	Since time.Time
	// Limit caps how many messages this run classifies. A backfill over a
	// large mailbox is real money and real time, so it is always bounded and
	// always resumable rather than being one unbounded job.
	Limit int
	// DryRun lists what would be classified and calls nothing. This is how you
	// find out the size and cost of a run before paying for it.
	DryRun bool
	// OnProgress is called after each message. Optional.
	OnProgress func(BackfillProgress, string)
}

// Backfill classifies historical inbound mail that has never been tagged.
//
// It exists because a classifier that only sees new arrivals is useless on the
// day you turn it on: the inbox you want sorted is the one already sitting
// there. Phase 1 of the plan says to run it over the existing inbox for exactly
// that reason, and then watch it for a week.
//
// Safe to re-run. Every message is idempotent on its Message-ID, so an
// interrupted run resumes where it stopped and a repeated run costs nothing.
// Cancelling the context stops it cleanly: each message is saved as it goes, so
// the work already done is kept.
func (s *Service) Backfill(ctx context.Context, orgID uuid.UUID, opts BackfillOptions) (BackfillProgress, error) {
	var p BackfillProgress
	if !s.Enabled() {
		return p, errors.New("inbox tagging is not enabled on this instance")
	}
	if opts.Limit <= 0 {
		opts.Limit = 200
	}

	candidates, err := s.repo.ListUntagged(ctx, orgID, opts.Since, opts.Limit)
	if err != nil {
		return p, err
	}

	// The taxonomy is created up front even on a dry run, so the labels are
	// filterable in the inbox before the first message is classified.
	if !opts.DryRun {
		s.seedTaxonomy(ctx, orgID)
	}

	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		p.Considered++

		if opts.DryRun {
			p.Classified++
			if opts.OnProgress != nil {
				opts.OnProgress(p, c.Subject)
			}
			continue
		}

		// Our previous message in the thread, looked up rather than asked:
		// a reply cannot be read without the thing it replies to.
		previous, campaign, _ := s.repo.PreviousOutbound(ctx, c.EmailAccountID, c.ThreadID, c.InternalDate)

		d, err := s.Classify(ctx, Message{
			OrganizationID:  orgID,
			UserID:          c.UserID,
			EmailAccountID:  c.EmailAccountID,
			MessageID:       c.MessageID,
			ThreadID:        c.ThreadID,
			Subject:         c.Subject,
			BodyText:        c.BodyText,
			FromAddr:        c.FromAddr,
			Headers:         replyclassify.FlagHeaders(c.Flags),
			InReplyTo:       c.InReplyTo,
			PreviousMessage: previous,
			Campaign:        campaign,
		})
		switch {
		case err != nil:
			p.Failed++
			log.Warn().Err(err).Str("message_id", c.MessageID).Msg("inbox tagging backfill: message failed")
		case d.Skipped() || d.KindSource == "":
			p.Skipped++
		default:
			p.Classified++
		}

		if opts.OnProgress != nil {
			opts.OnProgress(p, c.Subject)
		}
	}

	return p, nil
}

// PreviousOutbound exposes the thread lookup to callers that build a Message
// themselves, so the live ingest path and the backfill give the model the same
// context rather than one of them sending a reply with nothing to answer.
// Nil-safe and never fatal: no previous message is the normal case for the
// first inbound of a thread.
// RecordActions stores what a verdict was allowed to do, for the review page.
func (s *Service) RecordActions(ctx context.Context, orgID uuid.UUID, messageID string, actions []string) error {
	if s == nil || s.repo == nil {
		return nil
	}
	return s.repo.RecordActions(ctx, orgID, messageID, actions)
}

func (s *Service) PreviousContext(ctx context.Context, accountID uuid.UUID, threadID string, before time.Time) (string, string) {
	if !s.Enabled() || threadID == "" {
		return "", ""
	}
	body, campaign, err := s.repo.PreviousOutbound(ctx, accountID, threadID, before)
	if err != nil {
		return "", ""
	}
	return body, campaign
}

// ── Follow-up sweep ────────────────────────────────────────────────────────

// FollowUpProgress reports what one sweep changed.
type FollowUpProgress struct {
	Threads  int
	Labelled map[string]int
	Cleared  int
}

// SweepFollowUps recomputes the follow-up label on every recently active thread.
//
// The sweep makes no model calls. It runs when tagging is enabled and reuses
// trusted classifications so automated mail is not treated as a human reply.
func (s *Service) SweepFollowUps(ctx context.Context, orgID uuid.UUID, since time.Time, limit int) (FollowUpProgress, error) {
	p := FollowUpProgress{Labelled: map[string]int{}}
	if s == nil || s.repo == nil || s.categories == nil {
		return p, nil
	}
	if limit <= 0 {
		limit = 2000
	}

	states, err := s.repo.ThreadStates(ctx, orgID, since, limit)
	if err != nil {
		return p, err
	}
	// The hourly sweep is also how a workspace that predates the feature
	// gets its labels: the whole taxonomy when classification is on, the
	// follow-up labels otherwise. Idempotent and cached, so it costs nothing
	// after the first pass.
	if err := s.categories.EnsureAll(ctx, orgID, SeedSet()); err != nil {
		log.Warn().Err(err).Msg("inbox tagging: could not seed labels")
	}

	now := time.Now()
	for _, st := range states {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		want := FollowUp(ThreadState{
			ThreadID:       st.ThreadID,
			LastInboundAt:  st.LastInboundAt,
			LastOutboundAt: st.LastOutboundAt,
			BestIntent:     st.BestIntent,
			LastKind:       st.LastKind,
		}, now)

		if err := s.categories.SyncExclusiveLabels(ctx, orgID, st.ThreadID, FollowUpLabels, want); err != nil {
			log.Warn().Err(err).Str("thread_id", st.ThreadID).Msg("inbox tagging: follow-up label not applied")
			continue
		}

		p.Threads++
		if want == "" {
			p.Cleared++
		} else {
			p.Labelled[want]++
		}
	}
	return p, nil
}
