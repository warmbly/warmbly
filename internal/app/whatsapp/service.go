package whatsapp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// TemplateCatalog looks up official template approval state.
type TemplateCatalog interface {
	// Status returns APPROVED | PAUSED | REJECTED | PENDING | MISSING.
	Status(ctx context.Context, orgID uuid.UUID, name, language string) (string, error)
}

// StaticTemplateCatalog is an in-memory catalog for tests and manual sync ops.
type StaticTemplateCatalog struct {
	mu    sync.RWMutex
	items map[string]string // name|lang -> status
}

// NewStaticTemplateCatalog builds an empty catalog.
func NewStaticTemplateCatalog() *StaticTemplateCatalog {
	return &StaticTemplateCatalog{items: map[string]string{}}
}

// Set records a template status.
func (c *StaticTemplateCatalog) Set(name, language, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[name+"|"+language] = status
}

// Status implements TemplateCatalog.
func (c *StaticTemplateCatalog) Status(_ context.Context, _ uuid.UUID, name, language string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if s, ok := c.items[name+"|"+language]; ok {
		return s, nil
	}
	return TemplateMissing, nil
}

// Service is the domain send/inbound gate. All automated sends go through Send.
type Service struct {
	cfg      Config
	provider Provider
	catalog  TemplateCatalog

	// seenEventIDs is an in-process idempotency set (tests / single-node).
	// Production persists via repository when wired.
	mu   sync.Mutex
	seen map[string]struct{}
}

// NewService constructs the domain service. provider may be nil when disabled.
func NewService(cfg Config, provider Provider, catalog TemplateCatalog) *Service {
	if catalog == nil {
		catalog = NewStaticTemplateCatalog()
	}
	return &Service{
		cfg:      cfg,
		provider: provider,
		catalog:  catalog,
		seen:     map[string]struct{}{},
	}
}

// Config returns a copy of runtime config.
func (s *Service) Config() Config { return s.cfg }

// ProviderName returns the active provider id.
func (s *Service) ProviderName() string {
	if s.provider == nil {
		return ""
	}
	return s.provider.Name()
}

// SendResultExt wraps provider result with policy decision.
type SendResultExt struct {
	Decision Decision
	Result   *SendResult
	Skipped  bool
}

// Send runs eligibility then, if allowed, the provider. Never bypasses policy.
func (s *Service) Send(ctx context.Context, state ContactChannelState, intent SendIntent, textReq *SendTextRequest, tmplReq *SendTemplateRequest) (*SendResultExt, error) {
	if s == nil {
		return nil, fmt.Errorf("whatsapp service is nil")
	}
	intent.FeatureEnabled = s.cfg.Enabled
	if intent.Now.IsZero() {
		intent.Now = time.Now().UTC()
	}
	if intent.CrossChannelMin == 0 {
		intent.CrossChannelMin = s.cfg.CrossChannelInterval
	}
	if intent.ServiceWindow == 0 {
		intent.ServiceWindow = s.cfg.ServiceWindow
	}
	if intent.Mode == ModeTemplate && tmplReq != nil && s.catalog != nil {
		st, err := s.catalog.Status(ctx, state.OrganizationID, tmplReq.TemplateName, tmplReq.Language)
		if err != nil {
			return nil, err
		}
		intent.TemplateApproved = st == TemplateApproved
		if st != TemplateApproved {
			d := Decision{Allowed: false, Eligibility: EligBlocked, Reason: "template_status_" + st}
			return &SendResultExt{Decision: d, Skipped: true}, nil
		}
	}

	d := EvaluateEligibility(state, intent)
	if !d.Allowed {
		return &SendResultExt{Decision: d, Skipped: true}, nil
	}
	if s.provider == nil {
		return nil, fmt.Errorf("whatsapp provider not configured")
	}

	switch intent.Mode {
	case ModeTemplate:
		if tmplReq == nil {
			return nil, fmt.Errorf("template request required")
		}
		res, err := s.provider.SendTemplate(ctx, *tmplReq)
		return &SendResultExt{Decision: d, Result: res}, err
	default:
		if textReq == nil {
			return nil, fmt.Errorf("text request required")
		}
		res, err := s.provider.SendText(ctx, *textReq)
		return &SendResultExt{Decision: d, Result: res}, err
	}
}

// ProcessInbound applies opt-out detection, service window, and idempotency.
// Persist/CRM hooks are left to the caller (W2 / confenge orchestration).
func (s *Service) ProcessInbound(ctx context.Context, state *ContactChannelState, ev ChannelEvent) (InboundResult, error) {
	_ = ctx
	res := InboundResult{Event: ev}
	if s == nil {
		return res, fmt.Errorf("whatsapp service is nil")
	}
	key := ev.IdempotencyKey()
	s.mu.Lock()
	if _, ok := s.seen[key]; ok {
		s.mu.Unlock()
		res.Duplicate = true
		return res, nil
	}
	s.seen[key] = struct{}{}
	s.mu.Unlock()

	if ev.EventType != EventMessageReceived {
		res.Ignored = true
		return res, nil
	}

	if state != nil {
		OpenServiceWindowFromInbound(state, ev.OccurredAt, s.cfg.ServiceWindow)
		if ev.Content.Type == ContentText && ev.Content.Text != "" {
			match := DetectOptOut(ev.Content.Text)
			res.OptOut = match
			if match.Matched && match.Confident {
				ApplyOptOut(state, ev.OccurredAt, "inbound_phrase:"+match.Phrase)
				res.StopSequences = true
			} else if match.Matched && !match.Confident {
				res.NeedsHumanReview = true
			} else {
				// Any human inbound should stop automated follow-ups for the lead.
				res.StopSequences = true
			}
		} else {
			res.StopSequences = true
		}
	}
	return res, nil
}

// InboundResult is the domain outcome of one inbound message.
type InboundResult struct {
	Event            ChannelEvent
	Duplicate        bool
	Ignored          bool
	StopSequences    bool
	NeedsHumanReview bool
	OptOut           OptOutMatch
}

// SeenReports whether an event key was already processed (tests).
func (s *Service) Seen(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.seen[key]
	return ok
}

// LogBaileysWarning emits the lab-only warning when configured.
func LogBaileysWarning(cfg Config) {
	if w := cfg.BaileysWarning(); w != "" {
		log.Warn().Str("component", "whatsapp").Msg(w)
	}
}
