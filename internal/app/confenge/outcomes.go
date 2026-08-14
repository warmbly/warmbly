package confenge

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Outcome event types for confenge.outcome.v1.
const (
	OutcomeLeadImported    = "LEAD_IMPORTED"
	OutcomeLeadReviewed    = "LEAD_REVIEWED"
	OutcomeContactApproved = "CONTACT_APPROVED"
	OutcomeContacted       = "CONTACTED"
	OutcomeReplied         = "REPLIED"
	OutcomeMeeting         = "MEETING"
	OutcomeProposal        = "PROPOSAL"
	OutcomeWon             = "WON"
	OutcomeLost            = "LOST"
	OutcomeDoNotContact    = "DO_NOT_CONTACT"
	OutcomeBounced         = "BOUNCED"
)

// OutcomeEnvelope is the payload posted to extra-cli.
type OutcomeEnvelope struct {
	SchemaVersion  string `json:"schema_version"`
	EventID        string `json:"event_id"`
	IdempotencyKey string `json:"idempotency_key"`
	OccurredAt     string `json:"occurred_at"`
	Source         string `json:"source"`
	SourceLeadID   string `json:"source_lead_id"`
	CNPJ14         string `json:"cnpj14"`
	ContactEmail   string `json:"contact_email"`
	EventType      string `json:"event_type"`
	CampaignID     string `json:"campaign_id,omitempty"`
	MessageID      string `json:"message_id,omitempty"`
	// Commercial snapshot for Decision Memory analysis (not a second ledger).
	ServiceCode             string   `json:"service_code,omitempty"`
	MomentCode              string   `json:"moment_code,omitempty"`
	ActivationPolicyVersion string   `json:"activation_policy_version,omitempty"`
	ActivationScore         float64  `json:"activation_score,omitempty"`
	ActivationReasonCodes   []string `json:"activation_reason_codes,omitempty"`
	ActivationSourceHash    string   `json:"activation_source_hash,omitempty"`
	GeneratedContextHash    string   `json:"generated_context_hash,omitempty"`
	TouchpointOrdinal       int      `json:"touchpoint_ordinal,omitempty"`
	Channel                 string   `json:"channel,omitempty"`
	// Additive commercial-action feedback for extra-cli Decision Memory.
	ActionID                string         `json:"action_id,omitempty"`
	ActionType              string         `json:"action_type,omitempty"`
	ReachabilityClass       string         `json:"reachability_class,omitempty"`
	OutcomeCode             string         `json:"outcome_code,omitempty"`
	TargetReached           *bool          `json:"target_reached,omitempty"`
	ConversationStarted     *bool          `json:"conversation_started,omitempty"`
	InterestState           string         `json:"interest_state,omitempty"`
	PersonRelevanceFeedback string         `json:"person_relevance_feedback,omitempty"`
	RouteValidity           string         `json:"route_validity,omitempty"`
	Referral                map[string]any `json:"referral,omitempty"`
	NewPerson               string         `json:"new_person,omitempty"`
	NewRole                 string         `json:"new_role,omitempty"`
	NewRoute                string         `json:"new_route,omitempty"`
	PreferredChannel        string         `json:"preferred_channel,omitempty"`
	Metadata                map[string]any `json:"metadata,omitempty"`
}

// EnqueueOutcome records an async outbox row (idempotent by key).
// When payload is empty, enriches a commercial snapshot from account/touchpoint when available.
func (s *service) EnqueueOutcome(ctx context.Context, orgID uuid.UUID, ev models.OutreachOutcome) *errx.Error {
	if xerr := s.requireEnabled(); xerr != nil {
		return xerr
	}
	if strings.TrimSpace(ev.IdempotencyKey) == "" {
		return errx.New(errx.BadRequest, "idempotency_key is required")
	}
	if strings.TrimSpace(ev.EventType) == "" {
		return errx.New(errx.BadRequest, "event_type is required")
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	ev.OrganizationID = orgID
	s.enrichOutcomePayload(ctx, orgID, &ev)
	if err := s.repo.EnqueueOutcome(ctx, &ev); err != nil {
		// unique violation → already queued
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return nil
		}
		return errx.New(errx.Internal, "failed to enqueue outcome: "+err.Error())
	}
	return nil
}

// enrichOutcomePayload merges activation/commercial snapshot into payload JSON without wiping caller fields.
func (s *service) enrichOutcomePayload(ctx context.Context, orgID uuid.UUID, ev *models.OutreachOutcome) {
	if ev == nil {
		return
	}
	meta := map[string]any{}
	if len(ev.Payload) > 0 {
		_ = json.Unmarshal(ev.Payload, &meta)
	}
	// Prefer CNPJ lookup; never invent facts.
	var acc *models.OutreachAccount
	if cnpj := NormalizeCNPJ14(ev.CNPJ14); cnpj != "" {
		acc, _ = s.repo.GetAccountByCNPJ(ctx, orgID, cnpj)
	}
	if acc == nil && strings.TrimSpace(ev.ContactEmail) != "" {
		_, found, _ := s.repo.FindCandidateByEmail(ctx, orgID, strings.TrimSpace(strings.ToLower(ev.ContactEmail)))
		acc = found
	}
	if acc != nil {
		if ev.CNPJ14 == "" {
			ev.CNPJ14 = acc.CNPJ14
		}
		if ev.SourceLeadID == "" {
			ev.SourceLeadID = acc.SourceLeadID
		}
		putIfMissing(meta, "service_code", acc.ServiceCode)
		putIfMissing(meta, "moment_code", acc.MomentCode)
		putIfMissing(meta, "activation_policy_version", acc.ActivationPolicyVersion)
		if _, ok := meta["activation_score"]; !ok && acc.ActivationScore > 0 {
			meta["activation_score"] = acc.ActivationScore
		}
		if _, ok := meta["activation_reason_codes"]; !ok && len(acc.ActivationReasonCodes) > 0 {
			meta["activation_reason_codes"] = acc.ActivationReasonCodes
		}
		putIfMissing(meta, "activation_source_hash", acc.ActivationSourceHash)
		putIfMissing(meta, "message_context_hash", acc.MessageContextHash)
		// Latest approved/sent touchpoint context if present
		if tps, err := s.repo.ListTouchpoints(ctx, orgID, acc.ID, "", 1, 0); err == nil && len(tps) > 0 {
			tp := tps[0]
			putIfMissing(meta, "generated_context_hash", tp.GeneratedContextHash)
			if _, ok := meta["touchpoint_ordinal"]; !ok {
				meta["touchpoint_ordinal"] = tp.Ordinal
			}
			putIfMissing(meta, "channel", tp.Channel)
		}
	}
	if len(meta) > 0 {
		b, _ := json.Marshal(meta)
		if len(b) > 0 {
			ev.Payload = b
		}
	}
}

func putIfMissing(m map[string]any, k, v string) {
	if v == "" {
		return
	}
	if _, ok := m[k]; !ok {
		m[k] = v
	}
}

// SignOutcomeHMAC builds the signature header value: t=<unix>,v1=<hex>.
func SignOutcomeHMAC(secret string, ts time.Time, body []byte) string {
	msg := fmt.Sprintf("%d.", ts.Unix()) + string(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(msg))
	return "t=" + strconv.FormatInt(ts.Unix(), 10) + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyOutcomeHMAC checks timestamp skew and constant-time signature.
func VerifyOutcomeHMAC(secret, header string, body []byte, now time.Time, maxSkew time.Duration) bool {
	// header: t=...,v1=...
	var tUnix int64
	var sig string
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "t=") {
			tUnix, _ = strconv.ParseInt(strings.TrimPrefix(part, "t="), 10, 64)
		}
		if strings.HasPrefix(part, "v1=") {
			sig = strings.TrimPrefix(part, "v1=")
		}
	}
	if tUnix == 0 || sig == "" {
		return false
	}
	ts := time.Unix(tUnix, 0).UTC()
	if now.Sub(ts) > maxSkew || ts.Sub(now) > maxSkew {
		return false
	}
	expected := SignOutcomeHMAC(secret, ts, body)
	// Compare only the v1 portion for constant-time on hex.
	want := strings.TrimPrefix(strings.Split(expected, ",v1=")[1], "")
	return hmac.Equal([]byte(want), []byte(sig))
}

// BuildOutcomeEnvelope maps a stored row to the wire contract.
// Promotes commercial snapshot keys from payload metadata into top-level fields
// so Decision Memory consumers do not need to dig into metadata only.
func BuildOutcomeEnvelope(ev *models.OutreachOutcome) OutcomeEnvelope {
	meta := map[string]any{}
	if len(ev.Payload) > 0 {
		_ = json.Unmarshal(ev.Payload, &meta)
	}
	env := OutcomeEnvelope{
		SchemaVersion:  models.OutreachOutcomeSchemaV1,
		EventID:        ev.EventID.String(),
		IdempotencyKey: ev.IdempotencyKey,
		OccurredAt:     ev.OccurredAt.UTC().Format(time.RFC3339),
		Source:         "warmbly",
		SourceLeadID:   ev.SourceLeadID,
		CNPJ14:         ev.CNPJ14,
		ContactEmail:   ev.ContactEmail,
		EventType:      ev.EventType,
		Metadata:       meta,
	}
	// Promote activation / commercial snapshot from payload (set by enrichOutcomePayload).
	env.ServiceCode = metaString(meta, "service_code")
	env.MomentCode = metaString(meta, "moment_code")
	env.ActivationPolicyVersion = metaString(meta, "activation_policy_version")
	env.ActivationSourceHash = metaString(meta, "activation_source_hash")
	env.GeneratedContextHash = metaString(meta, "generated_context_hash")
	env.Channel = metaString(meta, "channel")
	if v, ok := meta["activation_score"].(float64); ok {
		env.ActivationScore = v
	}
	if v, ok := meta["touchpoint_ordinal"].(float64); ok {
		env.TouchpointOrdinal = int(v)
	} else if v, ok := meta["touchpoint_ordinal"].(int); ok {
		env.TouchpointOrdinal = v
	}
	if raw, ok := meta["activation_reason_codes"]; ok {
		switch codes := raw.(type) {
		case []string:
			env.ActivationReasonCodes = codes
		case []any:
			out := make([]string, 0, len(codes))
			for _, c := range codes {
				if s, ok := c.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			env.ActivationReasonCodes = out
		}
	}
	if camp, ok := meta["campaign_id"].(string); ok && camp != "" {
		env.CampaignID = camp
	}
	if mid, ok := meta["message_id"].(string); ok && mid != "" {
		env.MessageID = mid
	}
	env.ActionID = metaString(meta, "action_id")
	env.ActionType = metaString(meta, "action_type")
	env.ReachabilityClass = metaString(meta, "reachability_class")
	env.OutcomeCode = metaString(meta, "outcome_code")
	env.InterestState = metaString(meta, "interest_state")
	env.PersonRelevanceFeedback = metaString(meta, "person_relevance_feedback")
	env.RouteValidity = firstNonEmpty(metaString(meta, "route_validity"), metaString(meta, "route_quality_feedback"))
	env.NewPerson = metaString(meta, "new_person")
	env.NewRole = metaString(meta, "new_role")
	env.NewRoute = metaString(meta, "new_route")
	env.PreferredChannel = metaString(meta, "preferred_channel")
	if v, ok := meta["target_reached"].(bool); ok {
		env.TargetReached = &v
	}
	if v, ok := meta["conversation_started"].(bool); ok {
		env.ConversationStarted = &v
	}
	if raw, ok := meta["referral"].(map[string]any); ok {
		env.Referral = raw
	}
	return env
}

func metaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta[key].(string); ok {
		return v
	}
	return ""
}

// OutcomeBackoff grows attempts: 30s, 1m, 2m, 5m, 15m, 1h (cap).
func OutcomeBackoff(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return 30 * time.Second
	case attempt == 2:
		return time.Minute
	case attempt == 3:
		return 2 * time.Minute
	case attempt == 4:
		return 5 * time.Minute
	case attempt == 5:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}
