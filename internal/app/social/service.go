// Package social is the social add-on: LinkedIn, Instagram, WhatsApp, Telegram
// and Facebook accounts run through GraphKit's social API on the store's
// connection, their seats billed with the store's mailboxes, their steps in
// campaigns, LinkedIn lead search, and the conversations those accounts hold.
package social

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/app/store"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
	"github.com/warmbly/warmbly/internal/repository"
)

// Store is the slice of the store the add-on runs on.
type Store interface {
	Enabled() bool
	API() graphkit.API
	EnsureAccount(ctx context.Context, orgID uuid.UUID) error
	Metered() bool
	StoreRetail(ctx context.Context) instancesettings.StoreRetail
	PartnerManaged(ctx context.Context, orgID uuid.UUID) bool
	PlanActive(ctx context.Context, orgID uuid.UUID) (bool, error)
	PaymentFor(ctx context.Context, orgID uuid.UUID, due, addMonthly int64) store.Payment
	Collect(ctx context.Context, orgID uuid.UUID, due int64, key, description string, offSession bool) error
	NotifyBilling(ctx context.Context, orgID uuid.UUID, title, body, link, group string)
	Upstream(ctx context.Context, op string, err error) *errx.Error
}

// Credits is the workspace balance: seats are store charges, the search builder an AI cost.
type Credits interface {
	ChargeAmount(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason, idempotencyKey string) (models.Micro, error)
	ChargeOf(ctx context.Context, orgID uuid.UUID, key string) (*models.KeyedCharge, error)
	RefundStore(ctx context.Context, orgID uuid.UUID, capPrefix, spendKey, refundKey string, amount models.Micro, reason string) (models.Micro, error)
	Reserve(ctx context.Context, orgID uuid.UUID, estimate models.Micro) error
	ChargeAICost(ctx context.Context, orgID uuid.UUID, cost models.Micro, reason, idempotencyKey string) (models.Micro, error)
}

// Subscriptions resolves the plan a seat is priced at.
type Subscriptions interface {
	GetByOrganizationID(ctx context.Context, orgID uuid.UUID) (*models.Subscription, error)
}

// Publisher fans social changes out to the workspace's dashboards.
type Publisher interface {
	PublishSocialEvent(ctx context.Context, orgID uuid.UUID, ev pubsub.SocialEvent)
}

// Deps is what the service is built from.
type Deps struct {
	Store         Store
	Repo          repository.SocialRepository
	Credits       Credits
	Subscriptions Subscriptions
	Publisher     Publisher
	Contacts      contact.ContactService
	Cipher        cipher.CipherService
	// Replies, Campaigns and CampaignLogs carry social steps and replies into campaigns; each is optional.
	Replies      Replies
	Campaigns    Campaigns
	CampaignLogs CampaignLogs
	// Finder looks up work emails for a LinkedIn import; optional.
	Finder EmailFinder
	Now    func() time.Time
}

// Service is the social add-on.
type Service struct {
	Deps

	mu sync.Mutex
	// ackVersion is GraphKit's acknowledgement version as last read.
	ackVersion string
	// hits keeps recent search hits per workspace, so an import needs no second search.
	hits map[uuid.UUID]*hitCache
	// pulled spaces out conversation pulls per workspace.
	pulled map[uuid.UUID]time.Time
	// finderRef is the finder WireFinder attached; it wins over Deps.Finder.
	finderRef atomic.Pointer[finderBox]
	// channels is GraphKit's channel catalog as last read, at channelsAt.
	channels   []graphkit.SocialChannel
	channelsAt time.Time
}

// New builds the service.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{Deps: d, hits: map[uuid.UUID]*hitCache{}, pulled: map[uuid.UUID]time.Time{}}
}

var _ store.Social = (*Service)(nil)

func (s *Service) now() time.Time { return s.Now().UTC() }

func ext(orgID uuid.UUID) string { return orgID.String() }

// opKey is a fresh GraphKit Idempotency-Key for one action; the route's own key replays a retried request.
func opKey(op string) string { return "social:" + op + ":" + uuid.NewString() }

func (s *Service) available() bool {
	return s != nil && s.Store != nil && s.Store.Enabled() && s.Store.API() != nil
}

func (s *Service) api() graphkit.API { return s.Store.API() }

// ready refuses when the instance has no GraphKit connection and makes sure the workspace has a GraphKit account.
func (s *Service) ready(ctx context.Context, orgID uuid.UUID) error {
	if !s.available() {
		return errUnavailable
	}
	if err := s.Store.EnsureAccount(ctx, orgID); err != nil {
		return s.upstream(ctx, "account.ensure", err)
	}
	return nil
}

func (s *Service) publish(ctx context.Context, orgID uuid.UUID, ev pubsub.SocialEvent) {
	if s.Publisher != nil {
		s.Publisher.PublishSocialEvent(context.WithoutCancel(ctx), orgID, ev)
	}
}

func (s *Service) publishAccount(ctx context.Context, a *models.SocialAccount) {
	if a == nil {
		return
	}
	s.publish(ctx, a.OrganizationID, pubsub.SocialEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventSocialAccountUpdated},
		Channel: a.Channel, AccountID: a.ID.String(), Status: a.Status})
}

// ---- pricing --------------------------------------------------------------

// Price tiers.
const (
	TierStandard  = "standard"
	TierAgency    = "agency"
	TierAgencyPro = "agency_pro"
)

// Price is a seat's monthly price for the workspace.
type Price struct {
	MonthlyMicros int64  `json:"monthly_micros"`
	Tier          string `json:"tier"`
}

// seatPrice resolves the seat price by the plan the workspace pays at; an agency's client pays its agency's rate.
// A plan that cannot be read is an error, never the standard price.
func (s *Service) seatPrice(ctx context.Context, orgID uuid.UUID) (Price, error) {
	r := s.Store.StoreRetail(ctx)
	var sub *models.Subscription
	if s.Subscriptions != nil {
		got, err := s.Subscriptions.GetByOrganizationID(ctx, orgID)
		if err != nil {
			return Price{}, err
		}
		sub = got
	}
	switch sub.EffectivePlanID() {
	case models.AgencyProPlanID:
		return Price{MonthlyMicros: r.SocialSeatAgencyPro(), Tier: TierAgencyPro}, nil
	case models.AgencyPlanID:
		return Price{MonthlyMicros: r.SocialSeatAgency(), Tier: TierAgency}, nil
	}
	return Price{MonthlyMicros: r.SocialSeatMonthly(), Tier: TierStandard}, nil
}

// buyBlocked is why the workspace cannot take a managed seat, "" when it can.
func (s *Service) buyBlocked(ctx context.Context, orgID uuid.UUID) (string, error) {
	if s.Store.PartnerManaged(ctx, orgID) {
		return "partner_managed", nil
	}
	ok, err := s.Store.PlanActive(ctx, orgID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "plan_required", nil
	}
	return "", nil
}

func (s *Service) requireBuyer(ctx context.Context, orgID uuid.UUID) error {
	why, err := s.buyBlocked(ctx, orgID)
	switch {
	case err != nil:
		return err
	case why == "partner_managed":
		return errPartnerManaged
	case why == "plan_required":
		return errPlanRequired
	}
	return nil
}

// ---- status ---------------------------------------------------------------

// Status is GET /v1/social/status.
type Status struct {
	Available              bool                    `json:"available"`
	Mode                   string                  `json:"mode"`
	LinkedWorkspace        *graphkit.WorkspaceName `json:"linked_workspace"`
	LinkedAt               *time.Time              `json:"linked_at"`
	CanBuy                 bool                    `json:"can_buy"`
	BuyBlockedReason       *string                 `json:"buy_blocked_reason"`
	Price                  Price                   `json:"price"`
	AcknowledgementVersion string                  `json:"acknowledgement_version"`
	Accounts               int                     `json:"accounts"`
	// Metered is false on an instance that charges nothing for seats.
	Metered bool `json:"metered"`
}

// Status is the workspace's social state, refreshed from GraphKit when it answers in time.
func (s *Service) Status(ctx context.Context, orgID uuid.UUID) (*Status, error) {
	st := &Status{Mode: models.SocialModeNone, Price: Price{Tier: TierStandard}}
	if !s.available() {
		return st, nil
	}
	st.Available = true
	price, err := s.seatPrice(ctx, orgID)
	if err != nil {
		return nil, err
	}
	st.Price, st.Metered = price, s.Store.Metered()
	if ws, err := s.refreshWorkspace(ctx, orgID, 3*time.Second); err != nil {
		if ge, ok := graphkit.AsError(err); ok && ge.Code == graphkit.CodeSocialUnavailable {
			st.Available = false
			return st, nil
		}
		log.Warn().Err(err).Str("org_id", orgID.String()).Msg("social: status refresh failed; answering from the last copy")
	} else if ws != nil {
		st.AcknowledgementVersion = ws.AcknowledgementVersion
	}
	row, err := s.Repo.GetWorkspace(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if row != nil {
		st.Mode = row.Mode
		if row.Mode == models.SocialModeLinked {
			st.LinkedAt = row.LinkedAt
			if row.WorkspaceName != nil {
				st.LinkedWorkspace = &graphkit.WorkspaceName{Name: *row.WorkspaceName}
			}
		}
	}
	if st.AcknowledgementVersion == "" {
		s.mu.Lock()
		st.AcknowledgementVersion = s.ackVersion
		s.mu.Unlock()
	}
	if st.Mode == models.SocialModeLinked {
		st.CanBuy = true
	} else {
		why, err := s.buyBlocked(ctx, orgID)
		if err != nil {
			return nil, err
		}
		st.CanBuy = why == ""
		if why != "" {
			st.BuyBlockedReason = &why
		}
	}
	if st.Accounts, err = s.Repo.CountAccounts(ctx, orgID); err != nil {
		return nil, err
	}
	return st, nil
}

// refreshWorkspace reads the workspace's social mode from GraphKit and mirrors it.
func (s *Service) refreshWorkspace(ctx context.Context, orgID uuid.UUID, timeout time.Duration) (*graphkit.SocialWorkspace, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	ws, err := s.api().SocialWorkspace(ctx, ext(orgID))
	if ge, ok := graphkit.AsError(err); ok && ge.Code == graphkit.CodeAccountNotFound {
		if err := s.Store.EnsureAccount(ctx, orgID); err != nil {
			return nil, err
		}
		ws, err = s.api().SocialWorkspace(ctx, ext(orgID))
	}
	if err != nil {
		return nil, err
	}
	s.applyWorkspace(ctx, orgID, ws)
	return ws, nil
}

// applyWorkspace mirrors GraphKit's mode; a link that ended takes its accounts with it.
func (s *Service) applyWorkspace(ctx context.Context, orgID uuid.UUID, ws *graphkit.SocialWorkspace) {
	if ws == nil {
		return
	}
	if ws.AcknowledgementVersion != "" {
		s.mu.Lock()
		s.ackVersion = ws.AcknowledgementVersion
		s.mu.Unlock()
	}
	prev, err := s.Repo.GetWorkspace(ctx, orgID)
	if err != nil {
		log.Warn().Err(err).Str("org_id", orgID.String()).Msg("social: reading the workspace mode failed")
		return
	}
	mode := ws.Mode
	switch mode {
	case models.SocialModeManaged, models.SocialModeLinked:
	default:
		mode = models.SocialModeNone
	}
	var name *string
	if ws.Workspace != nil && ws.Workspace.Name != "" {
		name = &ws.Workspace.Name
	}
	if prev != nil && prev.Mode == mode && strPtrEq(prev.WorkspaceName, name) {
		return
	}
	if err := s.Repo.SetWorkspace(ctx, orgID, mode, name, ws.LinkedAt); err != nil {
		log.Warn().Err(err).Str("org_id", orgID.String()).Msg("social: saving the workspace mode failed")
		return
	}
	if prev != nil && prev.Mode == models.SocialModeLinked && mode != models.SocialModeLinked {
		if _, err := s.Repo.ForgetLinkedAccounts(ctx, orgID, s.now()); err != nil {
			log.Warn().Err(err).Str("org_id", orgID.String()).Msg("social: retiring linked accounts failed")
		}
	}
	s.publish(ctx, orgID, pubsub.SocialEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventSocialLinkUpdated}, Status: mode})
}

func (s *Service) mode(ctx context.Context, orgID uuid.UUID) (string, error) {
	row, err := s.Repo.GetWorkspace(ctx, orgID)
	if err != nil || row == nil {
		return models.SocialModeNone, err
	}
	return row.Mode, nil
}

func strPtrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ---- pages ----------------------------------------------------------------

const (
	defaultLimit = 25
	maxLimit     = 100
)

func invalidCursor() *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, "invalid_cursor", "The cursor is not valid.")
}

// pageArgs reads a keyset cursor: a time and an id.
func pageArgs(cursor string, limit int) (*models.StorePage, int, error) {
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit {
		return nil, 0, errx.NewWithIdentifier(errx.BadRequest, "invalid_limit", "limit must be between 1 and 100.")
	}
	if cursor == "" {
		return nil, limit, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, 0, invalidCursor()
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	nanos, err := strconv.ParseInt(ts, 10, 64)
	if !ok || err != nil {
		return nil, 0, invalidCursor()
	}
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, 0, invalidCursor()
	}
	return &models.StorePage{CreatedAt: time.Unix(0, nanos).UTC(), ID: uid}, limit, nil
}

func nextPage(more bool, at time.Time, id uuid.UUID) store.Pagination {
	if !more {
		return store.Pagination{}
	}
	c := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixNano(), 10) + "|" + id.String()))
	return store.Pagination{NextCursor: &c, HasMore: true}
}

// List is a page of a /v1/social list.
type List[T any] struct {
	Data       []T              `json:"data"`
	Pagination store.Pagination `json:"pagination"`
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
