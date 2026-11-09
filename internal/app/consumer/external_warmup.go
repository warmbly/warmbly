package jobs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// ExternalWarmupStore is where the rule for other services' warmup is read and applied.
type ExternalWarmupStore interface {
	ExternalWarmupRule(ctx context.Context, orgID uuid.UUID) (models.ExternalWarmupSettings, error)
	ExternalWarmupRules(ctx context.Context) (map[uuid.UUID]models.ExternalWarmupSettings, error)
	OrganizationMailboxIDs(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error)
	DeleteExternalWarmup(ctx context.Context, orgID, emailID uuid.UUID, rule models.ExternalWarmupSettings, limit int) ([]models.JobEventNewEmail, error)
}

var _ ExternalWarmupStore = (*repository.ExternalWarmupRepository)(nil)

const (
	// externalWarmupRuleTTL bounds how long an edited rule takes to reach arrivals.
	externalWarmupRuleTTL      = 30 * time.Second
	externalWarmupSweepEvery   = 2 * time.Minute
	externalWarmupResweepAfter = 24 * time.Hour
	externalWarmupDeleteBatch  = 500
)

type cachedExternalWarmup struct {
	rule models.ExternalWarmupSettings
	at   time.Time
}

// externalWarmupState is the consumer's rule cache and the rule each organization was last swept with.
type externalWarmupState struct {
	mu    sync.Mutex
	rules map[uuid.UUID]cachedExternalWarmup
	swept map[uuid.UUID]cachedExternalWarmup
}

// isExternalWarmup reports whether the arrival is another service's warmup the workspace chose to hide.
func (s *JobsService) isExternalWarmup(ctx context.Context, e *models.JobEventNewEmail) bool {
	if s.ExternalWarmup == nil || e.Message == nil || s.EmailRepository == nil {
		return false
	}
	account, xerr := s.mailbox(ctx, e.Message.EmailID)
	if xerr != nil || account == nil || account.OrganizationID == nil {
		return false
	}
	rule, err := s.externalWarmupRule(ctx, *account.OrganizationID)
	if err != nil {
		// Showing warmup mail is recoverable; dropping a real message is not.
		log.Warn().Err(err).Str("email_account_id", e.Message.EmailID.String()).Msg("external warmup rule unavailable; arrival kept")
		return false
	}
	return rule.Matches(e.Message)
}

func (s *JobsService) externalWarmupRule(ctx context.Context, orgID uuid.UUID) (models.ExternalWarmupSettings, error) {
	st := &s.externalWarmup
	st.mu.Lock()
	cached, ok := st.rules[orgID]
	st.mu.Unlock()
	if ok && time.Since(cached.at) < externalWarmupRuleTTL {
		return cached.rule, nil
	}
	rule, err := s.ExternalWarmup.ExternalWarmupRule(ctx, orgID)
	if err != nil {
		return rule, err
	}
	st.mu.Lock()
	if st.rules == nil {
		st.rules = map[uuid.UUID]cachedExternalWarmup{}
	}
	st.rules[orgID] = cachedExternalWarmup{rule: rule, at: time.Now()}
	st.mu.Unlock()
	return rule, nil
}

// StartExternalWarmupSweep removes stored mail a workspace's rule now hides, once per rule change and daily after.
func (s *JobsService) StartExternalWarmupSweep(ctx context.Context) {
	if s.ExternalWarmup == nil {
		return
	}
	jobrun.Loop(ctx, "external_warmup_sweep", externalWarmupSweepEvery, true, s.sweepExternalWarmup)
}

func (s *JobsService) sweepExternalWarmup(ctx context.Context) error {
	rules, err := s.ExternalWarmup.ExternalWarmupRules(ctx)
	if err != nil {
		return err
	}
	// A rule turned off forgets its sweep, so turning it back on sweeps what arrived meanwhile.
	s.externalWarmup.mu.Lock()
	for orgID := range s.externalWarmup.swept {
		if _, ok := rules[orgID]; !ok {
			delete(s.externalWarmup.swept, orgID)
		}
	}
	s.externalWarmup.mu.Unlock()
	var failures []error
	for orgID, rule := range rules {
		if !s.externalWarmupSweepDue(orgID, rule) {
			continue
		}
		if err := s.sweepOrganizationExternalWarmup(ctx, orgID, rule); err != nil {
			failures = append(failures, err)
			continue
		}
		s.externalWarmup.mu.Lock()
		if s.externalWarmup.swept == nil {
			s.externalWarmup.swept = map[uuid.UUID]cachedExternalWarmup{}
		}
		s.externalWarmup.swept[orgID] = cachedExternalWarmup{rule: rule, at: time.Now()}
		s.externalWarmup.mu.Unlock()
	}
	return errors.Join(failures...)
}

// externalWarmupSweepDue is true for a rule not yet swept here, one that changed, or one swept a day ago.
func (s *JobsService) externalWarmupSweepDue(orgID uuid.UUID, rule models.ExternalWarmupSettings) bool {
	s.externalWarmup.mu.Lock()
	defer s.externalWarmup.mu.Unlock()
	last, ok := s.externalWarmup.swept[orgID]
	return !ok || time.Since(last.at) >= externalWarmupResweepAfter ||
		last.rule.HideWarmblyMarked != rule.HideWarmblyMarked ||
		!slices.EqualFunc(last.rule.Tags, rule.Tags, strings.EqualFold)
}

func (s *JobsService) sweepOrganizationExternalWarmup(ctx context.Context, orgID uuid.UUID, rule models.ExternalWarmupSettings) error {
	ctx = withMailboxMemo(ctx)
	mailboxes, err := s.ExternalWarmup.OrganizationMailboxIDs(ctx, orgID)
	if err != nil {
		return err
	}
	removed := 0
	for _, mailbox := range mailboxes {
		for {
			deleted, err := s.ExternalWarmup.DeleteExternalWarmup(ctx, orgID, mailbox, rule, externalWarmupDeleteBatch)
			if err != nil {
				return err
			}
			removed += len(deleted)
			if s.StreamingPublisher != nil {
				for i := range deleted {
					s.StreamingPublisher.PublishEmailDeleted(ctx, s.emailInboxEvent(ctx, deleted[i].UserID, deleted[i].Message))
				}
			}
			if len(deleted) < externalWarmupDeleteBatch {
				break
			}
		}
	}
	if removed > 0 {
		log.Info().Str("organization_id", orgID.String()).Int("removed", removed).Msg("external warmup removed from the inbox")
	}
	return nil
}
