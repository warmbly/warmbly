package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/models"
)

const outreachActionSelect = `
	SELECT id, organization_id, account_id, contact_candidate_id, parent_action_id, followup_action_id,
		COALESCE(source_lead_id,''), COALESCE(person_name,''), COALESCE(observed_role,''), COALESCE(target_role,''),
		action_type, COALESCE(reachability_class,''), COALESCE(mapping_version,''),
		COALESCE(route_type,''), COALESCE(route_relation,''), COALESCE(channel_value,''), COALESCE(channel_display,''),
		COALESCE(why_now,''), COALESCE(factual_hook,''), COALESCE(recommended_action,''),
		COALESCE(service_code,''), COALESCE(service_context,''), COALESCE(confidence,''),
		COALESCE(evidence_ids,'[]'::jsonb), COALESCE(warnings,'[]'::jsonb),
		state, COALESCE(lane,''), priority_rank, priority_score,
		actionable, email_sendable, dispatchable,
		COALESCE(person_fingerprint,''), COALESCE(route_fingerprint,''), COALESCE(content_hash,''),
		COALESCE(snapshot_hash,''), COALESCE(idempotency_key,''),
		COALESCE(human_actor,''), COALESCE(human_notes,''),
		COALESCE(outcome_code,''), COALESCE(outcome_notes,''), target_reached, conversation_started,
		COALESCE(interest_state,''), COALESCE(next_action_type,''), next_action_at,
		COALESCE(route_quality_feedback,''), COALESCE(person_relevance_feedback,''), COALESCE(message_feedback,''),
		COALESCE(content_json,'{}'::jsonb), COALESCE(correction_json,'[]'::jsonb),
		blocked_person, blocked_route, COALESCE(stale_warning,''), requires_fresh, COALESCE(company_name,''),
		created_at, updated_at, started_at, completed_at `

func (r *outreachRepository) UpsertCommercialAction(ctx context.Context, a *models.OutreachCommercialAction) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	now := time.Now().UTC()
	a.UpdatedAt = now
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	ev, _ := json.Marshal(a.EvidenceIDs)
	if ev == nil {
		ev = []byte("[]")
	}
	warn, _ := json.Marshal(a.Warnings)
	if warn == nil {
		warn = []byte("[]")
	}
	content := a.ContentJSON
	if len(content) == 0 {
		content = []byte("{}")
	}
	corr := a.CorrectionJSON
	if len(corr) == 0 {
		corr = []byte("[]")
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO outreach_commercial_actions (
			id, organization_id, account_id, contact_candidate_id, parent_action_id, followup_action_id,
			source_lead_id, person_name, observed_role, target_role,
			action_type, reachability_class, mapping_version, route_type, route_relation,
			channel_value, channel_display, why_now, factual_hook, recommended_action,
			service_code, service_context, confidence, evidence_ids, warnings,
			state, lane, priority_rank, priority_score, actionable, email_sendable, dispatchable,
			person_fingerprint, route_fingerprint, content_hash, snapshot_hash, idempotency_key,
			human_actor, human_notes, outcome_code, outcome_notes, target_reached, conversation_started,
			interest_state, next_action_type, next_action_at, route_quality_feedback,
			person_relevance_feedback, message_feedback, content_json, correction_json,
			blocked_person, blocked_route, stale_warning, requires_fresh, company_name,
			created_at, updated_at, started_at, completed_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,
			$7,$8,$9,$10,
			$11,$12,$13,$14,$15,
			$16,$17,$18,$19,$20,
			$21,$22,$23,$24,$25,
			$26,$27,$28,$29,$30,$31,$32,
			$33,$34,$35,$36,$37,
			$38,$39,$40,$41,$42,$43,
			$44,$45,$46,$47,
			$48,$49,$50,$51,
			$52,$53,$54,$55,$56,
			$57,$58,$59,$60
		)
		ON CONFLICT (id) DO UPDATE SET
			person_name = EXCLUDED.person_name,
			observed_role = EXCLUDED.observed_role,
			target_role = EXCLUDED.target_role,
			action_type = EXCLUDED.action_type,
			reachability_class = EXCLUDED.reachability_class,
			route_type = EXCLUDED.route_type,
			route_relation = EXCLUDED.route_relation,
			channel_value = EXCLUDED.channel_value,
			channel_display = EXCLUDED.channel_display,
			why_now = EXCLUDED.why_now,
			factual_hook = EXCLUDED.factual_hook,
			recommended_action = EXCLUDED.recommended_action,
			service_code = EXCLUDED.service_code,
			service_context = EXCLUDED.service_context,
			confidence = EXCLUDED.confidence,
			evidence_ids = EXCLUDED.evidence_ids,
			warnings = EXCLUDED.warnings,
			state = EXCLUDED.state,
			lane = EXCLUDED.lane,
			priority_rank = EXCLUDED.priority_rank,
			priority_score = EXCLUDED.priority_score,
			actionable = EXCLUDED.actionable,
			email_sendable = EXCLUDED.email_sendable,
			dispatchable = EXCLUDED.dispatchable,
			person_fingerprint = EXCLUDED.person_fingerprint,
			route_fingerprint = EXCLUDED.route_fingerprint,
			content_hash = EXCLUDED.content_hash,
			snapshot_hash = EXCLUDED.snapshot_hash,
			human_actor = EXCLUDED.human_actor,
			human_notes = EXCLUDED.human_notes,
			outcome_code = EXCLUDED.outcome_code,
			outcome_notes = EXCLUDED.outcome_notes,
			target_reached = EXCLUDED.target_reached,
			conversation_started = EXCLUDED.conversation_started,
			interest_state = EXCLUDED.interest_state,
			next_action_type = EXCLUDED.next_action_type,
			next_action_at = EXCLUDED.next_action_at,
			route_quality_feedback = EXCLUDED.route_quality_feedback,
			person_relevance_feedback = EXCLUDED.person_relevance_feedback,
			message_feedback = EXCLUDED.message_feedback,
			content_json = EXCLUDED.content_json,
			correction_json = EXCLUDED.correction_json,
			blocked_person = EXCLUDED.blocked_person,
			blocked_route = EXCLUDED.blocked_route,
			stale_warning = EXCLUDED.stale_warning,
			requires_fresh = EXCLUDED.requires_fresh,
			company_name = EXCLUDED.company_name,
			parent_action_id = EXCLUDED.parent_action_id,
			followup_action_id = EXCLUDED.followup_action_id,
			started_at = EXCLUDED.started_at,
			completed_at = EXCLUDED.completed_at,
			updated_at = EXCLUDED.updated_at`,
		a.ID, a.OrganizationID, a.AccountID, a.CandidateID, a.ParentActionID, a.FollowupActionID,
		a.SourceLeadID, a.PersonName, a.ObservedRole, a.TargetRole,
		a.ActionType, a.ReachabilityClass, a.MappingVersion, a.RouteType, a.RouteRelation,
		a.ChannelValue, a.ChannelDisplay, a.WhyNow, a.FactualHook, a.RecommendedAction,
		a.ServiceCode, a.ServiceContext, a.Confidence, ev, warn,
		a.State, a.Lane, a.PriorityRank, a.PriorityScore, a.Actionable, a.EmailSendable, a.Dispatchable,
		a.PersonFingerprint, a.RouteFingerprint, a.ContentHash, a.SnapshotHash, a.IdempotencyKey,
		a.HumanActor, a.HumanNotes, a.OutcomeCode, a.OutcomeNotes, a.TargetReached, a.ConversationStarted,
		a.InterestState, a.NextActionType, a.NextActionAt, a.RouteQualityFeedback,
		a.PersonRelevanceFeedback, a.MessageFeedback, content, corr,
		a.BlockedPerson, a.BlockedRoute, a.StaleWarning, a.RequiresFresh, a.CompanyName,
		a.CreatedAt, a.UpdatedAt, a.StartedAt, a.CompletedAt,
	)
	return err
}

func (r *outreachRepository) GetCommercialAction(ctx context.Context, orgID, id uuid.UUID) (*models.OutreachCommercialAction, error) {
	row := r.db.QueryRow(ctx, outreachActionSelect+` FROM outreach_commercial_actions WHERE organization_id=$1 AND id=$2`, orgID, id)
	return scanCommercialAction(row)
}

func (r *outreachRepository) GetCommercialActionByIdempotency(ctx context.Context, orgID uuid.UUID, key string) (*models.OutreachCommercialAction, error) {
	row := r.db.QueryRow(ctx, outreachActionSelect+` FROM outreach_commercial_actions WHERE organization_id=$1 AND idempotency_key=$2`, orgID, key)
	return scanCommercialAction(row)
}

func (r *outreachRepository) ListCommercialActions(ctx context.Context, orgID, accountID uuid.UUID, openOnly bool, limit int) ([]models.OutreachCommercialAction, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := outreachActionSelect + ` FROM outreach_commercial_actions WHERE organization_id=$1`
	args := []any{orgID}
	n := 2
	if accountID != uuid.Nil {
		q += ` AND account_id=$2`
		args = append(args, accountID)
		n = 3
	}
	if openOnly {
		q += ` AND state IN ('PLANNED','READY','IN_PROGRESS','NEEDS_FOLLOWUP')`
	}
	q += ` ORDER BY priority_score DESC, created_at ASC LIMIT $` + strconv.Itoa(n)
	args = append(args, limit)
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.OutreachCommercialAction
	for rows.Next() {
		a, err := scanCommercialAction(rows)
		if err != nil {
			return nil, err
		}
		if a != nil {
			out = append(out, *a)
		}
	}
	return out, rows.Err()
}

func scanCommercialAction(row scannable) (*models.OutreachCommercialAction, error) {
	var a models.OutreachCommercialAction
	var ev, warn, content, corr []byte
	err := row.Scan(
		&a.ID, &a.OrganizationID, &a.AccountID, &a.CandidateID, &a.ParentActionID, &a.FollowupActionID,
		&a.SourceLeadID, &a.PersonName, &a.ObservedRole, &a.TargetRole,
		&a.ActionType, &a.ReachabilityClass, &a.MappingVersion, &a.RouteType, &a.RouteRelation,
		&a.ChannelValue, &a.ChannelDisplay, &a.WhyNow, &a.FactualHook, &a.RecommendedAction,
		&a.ServiceCode, &a.ServiceContext, &a.Confidence, &ev, &warn,
		&a.State, &a.Lane, &a.PriorityRank, &a.PriorityScore, &a.Actionable, &a.EmailSendable, &a.Dispatchable,
		&a.PersonFingerprint, &a.RouteFingerprint, &a.ContentHash, &a.SnapshotHash, &a.IdempotencyKey,
		&a.HumanActor, &a.HumanNotes, &a.OutcomeCode, &a.OutcomeNotes, &a.TargetReached, &a.ConversationStarted,
		&a.InterestState, &a.NextActionType, &a.NextActionAt, &a.RouteQualityFeedback,
		&a.PersonRelevanceFeedback, &a.MessageFeedback, &content, &corr,
		&a.BlockedPerson, &a.BlockedRoute, &a.StaleWarning, &a.RequiresFresh, &a.CompanyName,
		&a.CreatedAt, &a.UpdatedAt, &a.StartedAt, &a.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(ev, &a.EvidenceIDs)
	_ = json.Unmarshal(warn, &a.Warnings)
	a.ContentJSON = content
	a.CorrectionJSON = corr
	return &a, nil
}
