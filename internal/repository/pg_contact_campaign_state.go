package repository

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// ListCampaignStates reads every campaign the contact is a lead of, with the
// flow's steps, this contact's progress on each, and the lead status the
// Leads list derives. Next is left nil: it is scheduler-derived and filled by
// the service.
func (r *contactRepository) ListCampaignStates(ctx context.Context, orgID, contactID uuid.UUID) ([]models.ContactCampaignState, *errx.Error) {
	campQuery := `
		SELECT cam.id, cam.name, cam.status, c.subscribed, ` + undeliverableClause("cam.id") + `,
		       cl.email_account_id, COALESCE(sender.email, ''),
		       cl.paused_at, cl.paused_until, COALESCE(cl.pause_reason, ''), COALESCE(cl.pause_source, '')
		FROM campaign_leads cl
		JOIN campaigns cam ON cam.id = cl.campaign_id AND cam.organization_id = $2
		JOIN contacts c ON c.id = cl.contact_id AND c.organization_id = $2
		LEFT JOIN email_accounts sender ON sender.id = cl.email_account_id
		WHERE cl.contact_id = $1
		ORDER BY cam.created_at DESC
	`
	rows, err := r.DB.Query(ctx, campQuery, contactID, orgID)
	if err != nil {
		db.CaptureError(err, campQuery, []any{contactID, orgID}, "ListCampaignStates campaigns")
		return nil, errx.InternalError()
	}
	type campRow struct {
		state         models.ContactCampaignState
		subscribed    bool
		undeliverable bool
		held          bool
	}
	var camps []campRow
	now := time.Now()
	for rows.Next() {
		var cr campRow
		var pausedAt, pausedUntil *time.Time
		var pauseReason, pauseSource string
		if err := rows.Scan(&cr.state.CampaignID, &cr.state.CampaignName, &cr.state.CampaignStatus, &cr.subscribed, &cr.undeliverable,
			&cr.state.SenderID, &cr.state.SenderEmail, &pausedAt, &pausedUntil, &pauseReason, &pauseSource); err != nil {
			rows.Close()
			db.CaptureError(err, "", nil, "ListCampaignStates campaigns scan")
			return nil, errx.InternalError()
		}
		// A dated hold stops counting the moment it passes, with nothing having
		// to write. models.LeadHold.Live is the one Go definition of that, and
		// the router reads the same one.
		hold := &models.LeadHold{Since: pausedAtOrZero(pausedAt), Until: pausedUntil, Reason: pauseReason, Source: pauseSource}
		if pausedAt != nil && hold.Live(now) {
			cr.held, cr.state.Hold = true, hold
		}
		camps = append(camps, cr)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "ListCampaignStates campaigns rows")
		return nil, errx.InternalError()
	}

	out := make([]models.ContactCampaignState, 0, len(camps))
	for _, cr := range camps {
		st := cr.state
		steps, failureReason, xerr := r.campaignStepsForContact(ctx, st.CampaignID, contactID)
		if xerr != nil {
			return nil, xerr
		}
		st.Steps = steps
		st.TotalSteps = len(steps)

		var sent, replied, bounced, failed bool
		var emailSteps, emailSent int
		var lastAt time.Time
		for i := range steps {
			s := &steps[i]
			if s.Kind == "email" {
				emailSteps++
			}
			if s.SentAt != nil {
				sent = true
				st.CompletedSteps++
				if s.Kind == "email" {
					emailSent++
				}
				if s.SentAt.After(lastAt) {
					lastAt = *s.SentAt
					st.LastAction, st.LastActionAt = "Sent", s.SentAt
					if s.Kind != "email" {
						st.LastAction = "Ran"
					}
					cur := *s
					st.CurrentStep = &cur
				}
			}
			for _, ev := range []struct {
				at    *time.Time
				label string
			}{
				{s.OpenedAt, "Opened"}, {s.ClickedAt, "Clicked"}, {s.RepliedAt, "Replied"},
				{s.BouncedAt, "Bounced"}, {s.FailedAt, "Send failed"},
			} {
				if ev.at != nil && ev.at.After(lastAt) {
					lastAt = *ev.at
					st.LastAction, st.LastActionAt = ev.label, ev.at
				}
			}
			replied = replied || s.RepliedAt != nil
			bounced = bounced || s.BouncedAt != nil
			if s.SentAt == nil && s.FailedAt != nil && s.Attempts >= config.CampaignSendMaxAttempts {
				failed = true
				st.FailureReason = failureReason
			}
		}
		// Same priority as leadStatusClause: unsubscribed > bounced > replied >
		// failed > completed > paused > active > undeliverable > pending.
		switch {
		case !cr.subscribed:
			st.LeadStatus = models.LeadStatusUnsubscribed
		case bounced:
			st.LeadStatus = models.LeadStatusBounced
		case replied:
			st.LeadStatus = models.LeadStatusReplied
		case failed:
			st.LeadStatus = models.LeadStatusFailed
		case sent && emailSteps > 0 && emailSent >= emailSteps:
			st.LeadStatus = models.LeadStatusCompleted
		case cr.held:
			st.LeadStatus = models.LeadStatusPaused
		case sent:
			st.LeadStatus = models.LeadStatusActive
		case cr.undeliverable:
			st.LeadStatus = models.LeadStatusUndeliverable
		default:
			st.LeadStatus = models.LeadStatusPending
		}
		out = append(out, st)
	}
	return out, nil
}

// campaignStepsForContact returns the campaign's steps in canvas order with
// the contact's progress folded in, plus the worker's reason for the latest
// failed send. Labels follow the Leads list: custom name, else "Email N",
// else the action's label.
func (r *contactRepository) campaignStepsForContact(ctx context.Context, campaignID, contactID uuid.UUID) ([]models.ContactCampaignStep, string, *errx.Error) {
	stepQuery := `
		SELECT s.id, s.name, s.subject, s.kind, s.action, s.position,
		       p.sent_at,
		       -- A person's open, as in the Leads list this mirrors: a client
		       -- prefetch or a gateway scan must not read as the recipient
		       -- having opened the step.
		       CASE WHEN p.opened_machine THEN NULL ELSE p.opened_at END,
		       p.clicked_at, p.replied_at, p.bounced_at, p.failed_at,
		       COALESCE(p.send_attempts, 0), p.dispatched_at, COALESCE(p.failure_reason, '')
		FROM sequences s
		LEFT JOIN campaign_contact_progress p
		       ON p.campaign_id = s.campaign_id AND p.sequence_id = s.id AND p.contact_id = $2
		WHERE s.campaign_id = $1
		ORDER BY s.position ASC, s.created_at ASC
	`
	rows, err := r.DB.Query(ctx, stepQuery, campaignID, contactID)
	if err != nil {
		db.CaptureError(err, stepQuery, []any{campaignID, contactID}, "ListCampaignStates steps")
		return nil, "", errx.InternalError()
	}
	defer rows.Close()
	var steps []models.ContactCampaignStep
	var failureReason string
	var failedAt time.Time
	emails := 0
	for rows.Next() {
		var s models.ContactCampaignStep
		var name string
		var action []byte
		var dispatchedAt *time.Time
		var reason string
		if err := rows.Scan(&s.ID, &name, &s.Subject, &s.Kind, &action, &s.Position,
			&s.SentAt, &s.OpenedAt, &s.ClickedAt, &s.RepliedAt, &s.BouncedAt, &s.FailedAt,
			&s.Attempts, &dispatchedAt, &reason); err != nil {
			db.CaptureError(err, "", nil, "ListCampaignStates steps scan")
			return nil, "", errx.InternalError()
		}
		if s.Kind == "email" {
			emails++
		}
		s.Label = stepLabel(name, s.Kind, action, emails)
		s.InFlight = s.SentAt == nil && dispatchedAt != nil
		if s.Kind != "email" {
			s.Subject = ""
		}
		if reason != "" && s.FailedAt != nil && s.FailedAt.After(failedAt) {
			failedAt, failureReason = *s.FailedAt, reason
		}
		steps = append(steps, s)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "ListCampaignStates steps rows")
		return nil, "", errx.InternalError()
	}
	return steps, failureReason, nil
}

// stepLabel mirrors the SQL label in the Leads search: custom name, else
// "Email N" (Nth email step in canvas order), else the node's action.
func stepLabel(name, kind string, action []byte, emailOrdinal int) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	switch kind {
	case "email":
		return "Email " + itoa(emailOrdinal)
	case "wait":
		return "Wait"
	case "action":
		var cfg models.ActionConfig
		if len(action) > 0 {
			_ = json.Unmarshal(action, &cfg)
		}
		switch cfg.Type {
		case "add_tag":
			return "Add tag"
		case "remove_tag":
			return "Remove tag"
		case "add_to_segment":
			return "Add to segment"
		case "remove_from_segment":
			return "Remove from segment"
		case "unsubscribe":
			return "Unsubscribe"
		case "notify":
			return "Notify"
		case "wait":
			return "Wait"
		}
		return "Action"
	}
	return "Step"
}

// pausedAtOrZero reads a nullable hold start; the zero value is only ever seen
// by a LeadHold that is immediately discarded as not live.
func pausedAtOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
