// Package placement implements seed inbox-placement testing: send a tokenized
// copy of a real template through a real sender to a panel of Warmbly-controlled
// SEED mailboxes, then classify where each landed (Inbox / Spam / Promotions /
// other) per provider.
//
// Classification reuses the warmup landing-folder detection (the same `\Junk`
// /spam-flag logic in the consumer) and reads it out of the UNIBOX — the
// received-mail store the worker syncs every mailbox into. A seed is an
// ordinary connected + synced email_account flagged is_seed, so its received
// mail lands in the unibox like any other mailbox; the poller (ClassifyPending)
// looks up the test token in the seed's unibox entries and reads the folder
// flags. No consumer hot-path hook is added.
package placement

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks"
)

// classifyTimeout bounds how long a result stays pending before the poller
// gives up and records it as "other" (the message never showed up in the
// seed's unibox — dropped, blocked at the gateway, or sync lag past the
// window). Tests are marked completed once every result resolves or this
// elapses.
const classifyTimeout = 2 * time.Hour

// subjectTokenMarker prefixes the token embedded in the test subject. The
// worker injects only the warmup verify header (config.WarmupVerifyHeader),
// which this control-plane package can't change, so the subject is the token
// carrier that reliably survives sync into the unibox. Format:
//
//	<subject>  [wmpl:<token>]
//
// The marker is matched verbatim by the repo's FindTokenInUnibox subject LIKE.
const subjectTokenMarker = "wmpl:"

// Service is the seed inbox-placement testing service.
type Service interface {
	// CreateTest generates a unique token, persists the test plus one pending
	// result per active seed, and sends a tokenized copy of the template from
	// the sender to every active seed address.
	CreateTest(ctx context.Context, orgID *uuid.UUID, senderAccountID uuid.UUID, subject, bodyPlain, bodyHTML string) (*repository.PlacementTest, error)
	// ClassifyPending resolves pending results by looking up each test's token
	// in the seed's unibox entries and classifying the folder from flags. It
	// marks a test completed once all its results resolve or the timeout passes.
	ClassifyPending(ctx context.Context) error
}

type service struct {
	repo      repository.PlacementRepository
	emailRepo repository.EmailRepository
	sender    tasks.EmailSender
}

// NewService wires the placement service. emailRepo resolves sender/seed
// mailbox rows; sender is the existing send primitive (publishes a SendEmail
// event to the assigned worker over Kafka — no Cloud Task / task row needed
// for a one-off probe).
func NewService(repo repository.PlacementRepository, emailRepo repository.EmailRepository, sender tasks.EmailSender) Service {
	return &service{repo: repo, emailRepo: emailRepo, sender: sender}
}

func (s *service) CreateTest(ctx context.Context, orgID *uuid.UUID, senderAccountID uuid.UUID, subject, bodyPlain, bodyHTML string) (*repository.PlacementTest, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, errors.New("subject is required")
	}
	if bodyPlain == "" && bodyHTML == "" {
		return nil, errors.New("a plaintext or HTML body is required")
	}

	sender, xerr := s.emailRepo.GetByID(ctx, senderAccountID)
	if xerr != nil || sender == nil {
		return nil, fmt.Errorf("sender account not found")
	}
	if sender.WorkerID == nil {
		return nil, fmt.Errorf("sender account %s has no assigned worker", senderAccountID)
	}

	seeds, err := s.repo.ListSeedAccounts(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("list seed accounts: %w", err)
	}
	if len(seeds) == 0 {
		return nil, errors.New("no active seed mailboxes are configured")
	}

	token := uuid.NewString()
	test := &repository.PlacementTest{
		ID:              uuid.New(),
		OrganizationID:  orgID,
		SenderAccountID: senderAccountID,
		Subject:         subject,
		BodyPlain:       bodyPlain,
		BodyHTML:        bodyHTML,
		Token:           token,
		Status:          repository.PlacementStatusPending,
	}
	if err := s.repo.CreateTest(ctx, test); err != nil {
		return nil, fmt.Errorf("create test: %w", err)
	}

	// Tokenized subject: the token lives in a marker the unibox subject search
	// can match. Keeping the original subject first means the seed inbox shows
	// the real template subject for classification fidelity.
	taggedSubject := fmt.Sprintf("%s [%s%s]", subject, subjectTokenMarker, token)

	sentCount := 0
	for _, seed := range seeds {
		if err := s.repo.CreatePendingResult(ctx, test.ID, seed.ID, seed.Provider); err != nil {
			log.Warn().Err(err).
				Str("test_id", test.ID.String()).
				Str("seed_id", seed.ID.String()).
				Msg("placement: failed to create pending result")
			continue
		}

		msg := tasks.EmailMessage{
			From:      sender.Email,
			To:        []string{seed.Email},
			Subject:   taggedSubject,
			BodyPlain: bodyPlain,
			BodyHTML:  bodyHTML,
			MessageID: fmt.Sprintf("<%s@%s>", uuid.NewString(), domainOf(sender.Email)),
			UserID:    mustUserID(sender.UserID),
			// We also pass the token through the warmup verify header lane via
			// WarmupToken so that, IF a future worker change starts persisting
			// that header into unibox flags, the same token is already present.
			// Today the header is consumed by the warmup detector and not stored,
			// so the subject marker remains the authoritative carrier.
			WarmupToken: token,
		}

		if err := s.sender.Send(ctx, uuid.New(), msg, *sender); err != nil {
			log.Warn().Err(err).
				Str("test_id", test.ID.String()).
				Str("seed_id", seed.ID.String()).
				Msg("placement: failed to publish test send to worker")
			continue
		}
		sentCount++
	}

	if sentCount == 0 {
		// Nothing went out — mark the test completed immediately so it doesn't
		// dangle pending forever. Results remain "pending" for visibility.
		now := time.Now()
		_ = s.repo.SetTestStatus(ctx, test.ID, repository.PlacementStatusCompleted, &now)
		return nil, errors.New("failed to send any placement probes")
	}

	return test, nil
}

func (s *service) ClassifyPending(ctx context.Context) error {
	jobs, err := s.repo.ListPendingResults(ctx, 200)
	if err != nil {
		return fmt.Errorf("list pending results: %w", err)
	}

	touchedTests := map[uuid.UUID]struct{}{}

	for _, j := range jobs {
		touchedTests[j.TestID] = struct{}{}

		match, err := s.repo.FindTokenInUnibox(ctx, j.SeedUserID, j.SeedAccountID, j.Token, j.TestCreatedAt)
		if err != nil {
			log.Warn().Err(err).Str("result_id", j.ResultID.String()).Msg("placement: unibox lookup failed")
			continue
		}

		if match != nil {
			folder := classifyFolder(match.Flags)
			rawFlags := strings.Join(match.Flags, ",")
			if err := s.repo.RecordResult(ctx, j.ResultID, folder, rawFlags, time.Now()); err != nil {
				log.Warn().Err(err).Str("result_id", j.ResultID.String()).Msg("placement: failed to record result")
			}
			continue
		}

		// Not found yet. If the test is older than the classify timeout, the
		// probe almost certainly never landed in this seat — record "other"
		// (could be a gateway block, a hard bounce, or a sync gap) so the test
		// can complete instead of hanging pending forever.
		if time.Since(j.TestCreatedAt) > classifyTimeout {
			if err := s.repo.RecordResult(ctx, j.ResultID, repository.PlacementFolderOther, "timeout", time.Now()); err != nil {
				log.Warn().Err(err).Str("result_id", j.ResultID.String()).Msg("placement: failed to timeout result")
			}
		}
	}

	// Mark any touched test completed once it has no pending results left.
	for testID := range touchedTests {
		pending, err := s.repo.CountPendingForTest(ctx, testID)
		if err != nil {
			continue
		}
		if pending == 0 {
			now := time.Now()
			_ = s.repo.SetTestStatus(ctx, testID, repository.PlacementStatusCompleted, &now)
		}
	}

	return nil
}

// classifyFolder maps a synced message's flags/labels to a placement folder.
// It reuses the SAME spam-flag detection the warmup consumer uses
// (containsSpamFlag: \Junk / \Spam / SPAM / Junk).
//
// Promotions detection requires a Gmail CATEGORY_PROMOTIONS label to be present
// in the flags. NOTE: the current Gmail sync only maps UNREAD/STARRED/IMPORTANT
// /DRAFT into flags (internal/client/goog/message.go GmailMessageToEmailData)
// and does not capture CATEGORY_* tab labels. Capturing those needs a Gmail
// category-label sync change in the worker — a deliberate follow-up. Until then
// a Gmail-tab message reads as "inbox" here, which is the correct conservative
// default (it did reach the inbox, just a tab).
func classifyFolder(flags []string) string {
	if containsSpamFlag(flags) {
		return repository.PlacementFolderSpam
	}
	if hasPromotionsLabel(flags) {
		return repository.PlacementFolderPromotions
	}
	return repository.PlacementFolderInbox
}

// containsSpamFlag mirrors internal/app/consumer/event_new_email.go's
// containsSpamFlag so placement classification and warmup spam-placement
// detection agree on what "landed in spam" means.
func containsSpamFlag(flags []string) bool {
	spamFlags := []string{"\\Junk", "\\Spam", "SPAM", "Junk"}
	for _, f := range flags {
		if slices.Contains(spamFlags, f) {
			return true
		}
	}
	return false
}

// hasPromotionsLabel reports whether a Gmail CATEGORY_PROMOTIONS label is
// present. See classifyFolder for the sync caveat.
func hasPromotionsLabel(flags []string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, "CATEGORY_PROMOTIONS") {
			return true
		}
	}
	return false
}

func domainOf(email string) string {
	if at := strings.LastIndex(email, "@"); at >= 0 && at < len(email)-1 {
		return email[at+1:]
	}
	return "localhost"
}

// mustUserID parses the email account's string UserID, returning uuid.Nil on a
// malformed value (the send still goes out; the worker keys off the account).
func mustUserID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}
