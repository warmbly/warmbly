// Package compose picks the sending mailbox for a brand-new outbound email.
// The scoring is deliberately explainable: an existing conversation with the
// recipient dominates (contacts should keep hearing from the same mailbox),
// then remaining daily budget, then domain-auth health. Every candidate
// carries human-readable reasons so the picker can show why, not just what.
package compose

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type Candidate struct {
	Account       models.Email
	SentToday     int
	DailyLimit    int
	History       int
	LastContactAt *time.Time
	Score         int
	Reasons       []string
	Recommended   bool
}

// Remaining is today's unspent send budget, floored at zero.
func (c *Candidate) Remaining() int {
	if r := c.DailyLimit - c.SentToday; r > 0 {
		return r
	}
	return 0
}

type Service interface {
	// Candidates returns the user's active mailboxes scored against the
	// recipient address, sorted best-first, with Recommended set on the
	// mailbox auto-send would use. Address may be empty (no affinity signal).
	Candidates(ctx context.Context, userID, orgID uuid.UUID, address string) ([]Candidate, *errx.Error)
	// Resolve returns the mailbox a compose send should use: the explicit
	// accountID when given (validated against the user's active mailboxes),
	// otherwise the best-scored candidate. The bool reports whether the
	// pick was automatic.
	Resolve(ctx context.Context, userID, orgID uuid.UUID, accountID *uuid.UUID, address string) (*Candidate, bool, *errx.Error)

	// Drafts: autosaved per-user working copies from the compose window.
	UpsertDraft(ctx context.Context, userID, orgID uuid.UUID, d *repository.ComposeDraft) *errx.Error
	ListDrafts(ctx context.Context, userID, orgID uuid.UUID) ([]repository.ComposeDraft, *errx.Error)
	DeleteDraft(ctx context.Context, userID, id uuid.UUID) *errx.Error
}

type service struct {
	emailRepo   repository.EmailRepository
	composeRepo repository.ComposeRepository
}

func NewService(emailRepo repository.EmailRepository, composeRepo repository.ComposeRepository) Service {
	return &service{emailRepo: emailRepo, composeRepo: composeRepo}
}

func (s *service) Candidates(ctx context.Context, userID, orgID uuid.UUID, address string) ([]Candidate, *errx.Error) {
	accounts, xerr := s.emailRepo.GetAllActiveByUser(ctx, userID.String())
	if xerr != nil {
		return nil, xerr
	}
	if len(accounts) == 0 {
		return []Candidate{}, nil
	}

	ids := make([]uuid.UUID, len(accounts))
	for i, a := range accounts {
		ids[i] = a.ID
	}

	sent, err := s.composeRepo.TodaySentCounts(ctx, ids)
	if err != nil {
		return nil, errx.InternalError()
	}

	history := map[uuid.UUID]repository.ComposeAffinity{}
	address = strings.TrimSpace(address)
	if address != "" {
		history, err = s.composeRepo.AddressHistoryByAccount(ctx, orgID, address)
		if err != nil {
			return nil, errx.InternalError()
		}
	}

	candidates := make([]Candidate, 0, len(accounts))
	for _, a := range accounts {
		c := Candidate{
			Account:    a,
			SentToday:  sent[a.ID],
			DailyLimit: a.CampaignLimit,
		}
		if aff, ok := history[a.ID]; ok {
			c.History = aff.Messages
			c.LastContactAt = aff.LastAt
		}
		score(&c)
		candidates = append(candidates, c)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	// Recommend the best mailbox that still has budget today; if every
	// mailbox is spent, the top-scored one is still marked so the UI has
	// a default (the send itself queues for tomorrow's window anyway).
	for i := range candidates {
		if candidates[i].Remaining() > 0 {
			candidates[i].Recommended = true
			break
		}
	}
	if len(candidates) > 0 && !hasRecommended(candidates) {
		candidates[0].Recommended = true
	}

	return candidates, nil
}

func (s *service) Resolve(ctx context.Context, userID, orgID uuid.UUID, accountID *uuid.UUID, address string) (*Candidate, bool, *errx.Error) {
	candidates, xerr := s.Candidates(ctx, userID, orgID, address)
	if xerr != nil {
		return nil, false, xerr
	}
	if len(candidates) == 0 {
		return nil, false, errx.New(errx.BadRequest, "no active mailboxes to send from; connect a mailbox first")
	}

	if accountID != nil {
		for i := range candidates {
			if candidates[i].Account.ID == *accountID {
				return &candidates[i], false, nil
			}
		}
		return nil, false, errx.New(errx.NotFound, "mailbox not found or not active")
	}

	for i := range candidates {
		if candidates[i].Recommended {
			return &candidates[i], true, nil
		}
	}
	return &candidates[0], true, nil
}

func (s *service) UpsertDraft(ctx context.Context, userID, orgID uuid.UUID, d *repository.ComposeDraft) *errx.Error {
	if err := s.composeRepo.UpsertDraft(ctx, userID, orgID, d); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) ListDrafts(ctx context.Context, userID, orgID uuid.UUID) ([]repository.ComposeDraft, *errx.Error) {
	drafts, err := s.composeRepo.ListDrafts(ctx, userID, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return drafts, nil
}

func (s *service) DeleteDraft(ctx context.Context, userID, id uuid.UUID) *errx.Error {
	if err := s.composeRepo.DeleteDraft(ctx, userID, id); err != nil {
		return errx.InternalError()
	}
	return nil
}

// score fills Score + Reasons. Affinity dominates (+1000 band), budget and
// auth health order the rest; a spent daily budget pushes the mailbox below
// every fresh one without hiding it from the picker.
func score(c *Candidate) {
	remaining := c.Remaining()
	c.Score = remaining

	if c.History > 0 {
		bonus := c.History
		if bonus > 50 {
			bonus = 50
		}
		c.Score += 1000 + bonus*10
		c.Reasons = append(c.Reasons, fmt.Sprintf("%d past messages with this contact", c.History))
		if c.LastContactAt != nil && time.Since(*c.LastContactAt) < 30*24*time.Hour {
			c.Score += 50
		}
	}

	switch c.Account.AuthState {
	case "passing":
		c.Score += 25
		c.Reasons = append(c.Reasons, "domain authenticated")
	case "failing":
		c.Score -= 200
		c.Reasons = append(c.Reasons, "domain auth failing")
	}

	if remaining == 0 {
		c.Score -= 5000
		c.Reasons = append(c.Reasons, "daily limit reached")
	} else {
		c.Reasons = append(c.Reasons, fmt.Sprintf("%d of %d sends left today", remaining, c.DailyLimit))
	}
}

func hasRecommended(cs []Candidate) bool {
	for i := range cs {
		if cs[i].Recommended {
			return true
		}
	}
	return false
}

// RecommendedReason is the one-line explanation shown next to the Auto pick.
func RecommendedReason(c *Candidate) string {
	if c == nil {
		return ""
	}
	if c.History > 0 {
		return "already in conversation with this contact"
	}
	if c.Remaining() > 0 {
		return "most sending budget left today"
	}
	return "least loaded mailbox (all daily budgets spent)"
}
