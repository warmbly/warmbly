package social

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// Account is a social account as /v1/social shows it: GraphKit's view plus Warmbly's billing.
type Account struct {
	ID                       uuid.UUID                            `json:"id"`
	Channel                  string                               `json:"channel"`
	Status                   string                               `json:"status"`
	StatusReason             *string                              `json:"status_reason"`
	DisplayName              string                               `json:"display_name"`
	Headline                 string                               `json:"headline"`
	AvatarURL                string                               `json:"avatar_url"`
	ProfileURL               string                               `json:"profile_url"`
	PublicID                 string                               `json:"public_id"`
	Licences                 []string                             `json:"licences"`
	Limits                   graphkit.SocialLimits                `json:"limits"`
	WeeklyLimits             graphkit.SocialLimits                `json:"weekly_limits"`
	LimitCeilings            graphkit.SocialLimits                `json:"limit_ceilings"`
	WeeklyLimitCeilings      graphkit.SocialLimits                `json:"weekly_limit_ceilings"`
	Capabilities             map[string]graphkit.SocialCapability `json:"capabilities"`
	Voice                    graphkit.SocialVoiceState            `json:"voice"`
	WorkingHours             *graphkit.WorkingHours               `json:"working_hours"`
	Timezone                 string                               `json:"timezone"`
	Warmup                   *graphkit.SocialWarmup               `json:"warmup"`
	Usage                    []graphkit.SocialUsage               `json:"usage"`
	InmailCredits            *graphkit.InmailCredits              `json:"inmail_credits"`
	PendingInvites           *int                                 `json:"pending_invites"`
	AcceptanceRate           *float64                             `json:"acceptance_rate"`
	WithdrawInvitesAfterDays *int                                 `json:"withdraw_invites_after_days"`
	Billing                  Billing                              `json:"billing"`
	ConnectedBy              *uuid.UUID                           `json:"connected_by"`
	ConnectedAt              *time.Time                           `json:"connected_at"`
	UpdatedAt                time.Time                            `json:"updated_at"`
}

// Billing is how a seat is paid: by Warmbly's balance ("store") or the customer's GraphKit plan ("linked").
type Billing struct {
	Mode          string     `json:"mode"`
	MonthlyMicros *int64     `json:"monthly_micros"`
	PaidThrough   *time.Time `json:"paid_through"`
	UnpaidSince   *time.Time `json:"unpaid_since"`
}

func toAccount(a *models.SocialAccount) *Account {
	if a == nil {
		return nil
	}
	var snap graphkit.SocialAccount
	if len(a.Snapshot) > 0 {
		_ = json.Unmarshal(a.Snapshot, &snap)
	}
	out := &Account{
		ID: a.ID, Channel: a.Channel, Status: a.Status, StatusReason: a.StatusReason, DisplayName: a.DisplayName, Headline: a.Headline,
		AvatarURL: a.AvatarURL, ProfileURL: a.ProfileURL, PublicID: a.PublicID, Licences: a.Licences,
		Limits: nonNilLimits(snap.Limits), WeeklyLimits: nonNilLimits(snap.WeeklyLimits), LimitCeilings: nonNilLimits(snap.LimitCeilings),
		WeeklyLimitCeilings: nonNilLimits(snap.WeeklyLimitCeilings), Capabilities: snap.Capabilities, Voice: snap.Voice, WorkingHours: snap.WorkingHours,
		Timezone: snap.Timezone, Warmup: snap.Warmup, Usage: snap.Usage, InmailCredits: snap.InmailCredits,
		PendingInvites: snap.PendingInvites, AcceptanceRate: snap.AcceptanceRate, WithdrawInvitesAfterDays: snap.WithdrawInvitesAfterDays,
		Billing:     Billing{Mode: a.BillingMode, PaidThrough: a.PaidThrough, UnpaidSince: a.UnpaidSince},
		ConnectedBy: a.UserID, ConnectedAt: a.ConnectedAt, UpdatedAt: a.UpdatedAt,
	}
	if a.BillingMode == models.SocialBillingStore {
		m := int64(a.MonthlyRetailMicros)
		out.Billing.MonthlyMicros = &m
	}
	if out.Licences == nil {
		out.Licences = []string{}
	}
	if out.Usage == nil {
		out.Usage = []graphkit.SocialUsage{}
	}
	if out.Capabilities == nil {
		out.Capabilities = map[string]graphkit.SocialCapability{}
	}
	return out
}

func nonNilLimits(l graphkit.SocialLimits) graphkit.SocialLimits {
	if l == nil {
		return graphkit.SocialLimits{}
	}
	return l
}

// accountChannel is the channel GraphKit names for an account, LinkedIn when it names none.
func accountChannel(sa *graphkit.SocialAccount) string {
	if models.IsSocialChannel(sa.Channel) {
		return sa.Channel
	}
	return models.SocialLinkedIn
}

// record mirrors GraphKit's view of an account; userID attributes a new one and is otherwise kept. It never brings
// back a removed seat: only a connect does (recordConnected).
func (s *Service) record(ctx context.Context, orgID uuid.UUID, sa *graphkit.SocialAccount, userID *uuid.UUID) (*models.SocialAccount, error) {
	return s.recordAccount(ctx, orgID, sa, userID, false)
}

// recordConnected records the account a connect just succeeded with, reviving it if it had been removed.
func (s *Service) recordConnected(ctx context.Context, orgID uuid.UUID, sa *graphkit.SocialAccount, userID *uuid.UUID) (*models.SocialAccount, error) {
	return s.recordAccount(ctx, orgID, sa, userID, true)
}

func (s *Service) recordAccount(ctx context.Context, orgID uuid.UUID, sa *graphkit.SocialAccount, userID *uuid.UUID, revive bool) (*models.SocialAccount, error) {
	if sa == nil || sa.ID == "" {
		return nil, nil
	}
	billing := models.SocialBillingStore
	if sa.Billing.Mode == graphkit.SocialModeLinked {
		billing = models.SocialBillingLinked
	}
	row := &models.SocialAccount{
		OrganizationID: orgID, UserID: userID, Channel: accountChannel(sa), GraphKitID: sa.ID, Status: sa.Status, StatusReason: sa.StatusReason,
		DisplayName: sa.DisplayName, Headline: sa.Headline, AvatarURL: sa.AvatarURL, ProfileURL: sa.ProfileURL,
		PublicID: sa.PublicID, Licences: sa.Licences, Snapshot: mustJSON(sa), BillingMode: billing, ConnectedAt: sa.ConnectedAt,
	}
	if sa.Status == graphkit.SocialRemoved {
		t := s.now()
		row.RemovedAt = &t
	}
	var snapshotAt *time.Time
	if !sa.UpdatedAt.IsZero() {
		t := sa.UpdatedAt.UTC()
		snapshotAt = &t
	}
	got, err := s.Repo.UpsertAccount(ctx, row, revive, snapshotAt)
	if err != nil {
		return nil, err
	}
	if got != nil {
		s.publishAccount(ctx, got)
	}
	return got, nil
}

// owned is the workspace's account by Warmbly id; nil with errNotFound otherwise.
func (s *Service) owned(ctx context.Context, orgID, id uuid.UUID) (*models.SocialAccount, error) {
	a, err := s.Repo.GetAccount(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errNotFound
	}
	return a, nil
}

// live is an owned account that was not removed.
func (s *Service) live(ctx context.Context, orgID, id uuid.UUID) (*models.SocialAccount, error) {
	a, err := s.owned(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if a.RemovedAt != nil {
		return nil, errRemoved
	}
	return a, nil
}

// ListAccounts is the workspace's accounts, newest first.
func (s *Service) ListAccounts(ctx context.Context, orgID uuid.UUID, cursor string, limit int) (*List[*Account], error) {
	after, limit, err := pageArgs(cursor, limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.Repo.ListAccounts(ctx, orgID, after, limit+1, false)
	if err != nil {
		return nil, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	out := &List[*Account]{Data: make([]*Account, 0, len(rows))}
	for i := range rows {
		out.Data = append(out.Data, toAccount(&rows[i]))
	}
	if more {
		last := rows[len(rows)-1]
		out.Pagination = nextPage(true, last.CreatedAt, last.ID)
	}
	return out, nil
}

// GetAccount is one account, refreshed from GraphKit when it answers in time.
func (s *Service) GetAccount(ctx context.Context, orgID, id uuid.UUID) (*Account, error) {
	a, err := s.owned(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if a.RemovedAt == nil && s.available() {
		rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		sa, gerr := s.api().GetSocialAccount(rctx, ext(orgID), a.GraphKitID)
		cancel()
		if gerr == nil {
			if got, err := s.record(ctx, orgID, sa, nil); err == nil && got != nil {
				a = got
			}
		} else {
			log.Debug().Err(gerr).Str("account_id", a.ID.String()).Msg("social: account refresh failed; answering from the last copy")
		}
	}
	return toAccount(a), nil
}

// AccountPatch is PATCH /v1/social/accounts/:id.
type AccountPatch = graphkit.SocialAccountPatch

// UpdateAccount changes an account's limits, hours, time zone, warm-up or invite withdrawal at GraphKit.
func (s *Service) UpdateAccount(ctx context.Context, orgID, id uuid.UUID, patch AccountPatch) (*Account, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	a, err := s.live(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if patch.Timezone != nil {
		if _, err := time.LoadLocation(*patch.Timezone); err != nil || *patch.Timezone == "" {
			return nil, invalid("timezone must be an IANA time zone such as Europe/London.")
		}
	}
	sa, err := s.api().UpdateSocialAccount(ctx, ext(orgID), a.GraphKitID, patch, opKey("account.update"))
	if err != nil {
		return nil, s.upstream(ctx, "social.account.update", err)
	}
	got, err := s.record(ctx, orgID, sa, nil)
	if err != nil {
		return nil, err
	}
	return toAccount(got), nil
}

// Account actions.
const (
	ActionPause  = "pause"
	ActionResume = "resume"
	ActionResync = "resync"
)

// AccountAction pauses, resumes or resyncs an account at GraphKit.
func (s *Service) AccountAction(ctx context.Context, orgID, id uuid.UUID, action string) (*Account, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	a, err := s.live(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	var sa *graphkit.SocialAccount
	switch action {
	case ActionPause:
		sa, err = s.api().PauseSocialAccount(ctx, ext(orgID), a.GraphKitID, opKey("account.pause"))
	case ActionResume:
		if a.UnpaidPaused {
			return nil, errUnpaid
		}
		sa, err = s.api().ResumeSocialAccount(ctx, ext(orgID), a.GraphKitID, opKey("account.resume"))
	case ActionResync:
		sa, err = s.api().ResyncSocialAccount(ctx, ext(orgID), a.GraphKitID, opKey("account.resync"))
	default:
		return nil, errNotFound
	}
	if err != nil {
		return nil, s.upstream(ctx, "social.account."+action, err)
	}
	got, err := s.record(ctx, orgID, sa, nil)
	if err != nil {
		return nil, err
	}
	return toAccount(got), nil
}

// RemoveAccount removes an account at GraphKit. Its seat stops there; a month paid ahead that had not started is
// refunded, the month in progress is not.
func (s *Service) RemoveAccount(ctx context.Context, orgID, id uuid.UUID) (*models.SocialAccount, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	a, err := s.owned(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if a.RemovedAt != nil {
		return a, nil
	}
	sa, err := s.api().RemoveSocialAccount(ctx, ext(orgID), a.GraphKitID, opKey("account.remove"))
	if ge, ok := graphkit.AsError(err); ok && ge.Code == graphkit.CodeNotFound {
		err = nil
		sa = nil
	}
	if err != nil {
		return nil, s.upstream(ctx, "social.account.remove", err)
	}
	if sa != nil {
		if got, err := s.record(ctx, orgID, sa, nil); err == nil && got != nil {
			a = got
		}
	}
	if a.RemovedAt == nil {
		if err := s.Repo.MarkRemoved(ctx, orgID, a.ID, s.now()); err != nil {
			return nil, err
		}
		a.Status = graphkit.SocialRemoved
		s.publishAccount(ctx, a)
	}
	if got, err := s.Repo.GetAccount(ctx, orgID, a.ID); err == nil && got != nil {
		a = got
	}
	if a.RemovedAt != nil && a.PaidThrough != nil && a.PaidThrough.AddDate(0, -1, 0).After(*a.RemovedAt) {
		s.refundUnstartedMonth(ctx, a)
	}
	return a, nil
}

// graphkitAccount resolves an optional Warmbly account id to the GraphKit one, checking it is the workspace's.
func (s *Service) graphkitAccount(ctx context.Context, orgID uuid.UUID, id *uuid.UUID) (*string, error) {
	if id == nil {
		return nil, nil
	}
	a, err := s.live(ctx, orgID, *id)
	if err != nil {
		return nil, err
	}
	return &a.GraphKitID, nil
}
