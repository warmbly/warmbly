package social

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

const backfillBatch = 500

// ApplySocialEvent implements store.Social: GraphKit's social.* webhooks, and their replay from its event log. An
// event for a workspace this instance does not have, or one that can never apply, is skipped so the feed moves on.
func (s *Service) ApplySocialEvent(ctx context.Context, orgID uuid.UUID, ev graphkit.Event) error {
	exists, err := s.Repo.OrganizationExists(ctx, orgID)
	if err != nil {
		return err
	}
	if !exists {
		log.Debug().Str("event_id", ev.ID).Str("type", ev.Type).Msg("social: skipping an event for a workspace this instance does not have")
		return nil
	}
	err = s.applySocialEvent(ctx, orgID, ev)
	if err != nil && inapplicable(err) {
		log.Warn().Err(err).Str("event_id", ev.ID).Str("type", ev.Type).Str("org_id", orgID.String()).
			Msg("social: skipping an event that cannot be applied")
		return nil
	}
	return err
}

// inapplicable is an error retrying the same event can never get past: an unreadable body, or a row the database refuses.
func inapplicable(err error) bool {
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &typ) {
		return true
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		// Class 22 is a data exception, class 23 an integrity constraint violation.
		return strings.HasPrefix(pg.Code, "22") || strings.HasPrefix(pg.Code, "23")
	}
	return false
}

func (s *Service) applySocialEvent(ctx context.Context, orgID uuid.UUID, ev graphkit.Event) error {
	switch ev.Type {
	case graphkit.EventSocialUpdated:
		var ws graphkit.SocialWorkspace
		if err := json.Unmarshal(ev.Data, &ws); err != nil {
			return err
		}
		s.applyWorkspace(ctx, orgID, &ws)
	case graphkit.EventSocialConnectionUpdated:
		var gs graphkit.ConnectSession
		if err := json.Unmarshal(ev.Data, &gs); err != nil {
			return err
		}
		row, err := s.Repo.GetSession(ctx, orgID, gs.ID)
		if err != nil || row == nil {
			return err
		}
		_, err = s.observe(ctx, row, &gs)
		return err
	case graphkit.EventSocialAccountUpdated:
		var sa graphkit.SocialAccount
		if err := json.Unmarshal(ev.Data, &sa); err != nil {
			return err
		}
		_, err := s.record(ctx, orgID, &sa, nil)
		return err
	case graphkit.EventSocialMessageReceived, graphkit.EventSocialMessageSent:
		var me graphkit.MessageEvent
		if err := json.Unmarshal(ev.Data, &me); err != nil {
			return err
		}
		return s.applyMessage(ctx, orgID, &me)
	case graphkit.EventSocialActionUpdated:
		var a graphkit.Action
		if err := json.Unmarshal(ev.Data, &a); err != nil {
			return err
		}
		return s.applyAction(ctx, orgID, &a)
	case graphkit.EventSocialInvitationAccepted:
		var ia graphkit.InvitationAccepted
		if err := json.Unmarshal(ev.Data, &ia); err != nil {
			return err
		}
		return s.applyInvitationAccepted(ctx, orgID, &ia)
	}
	return nil
}

// ReconcileSocial implements store.Social: connect sessions no webhook settled, queued steps to stop, Open Profile reads,
// and the contact identity backfill.
func (s *Service) ReconcileSocial(ctx context.Context) error {
	var errs []error
	if s.available() {
		errs = append(errs, s.reconcileSessions(ctx), s.stopActions(ctx, nil), s.refundRemovedSeats(ctx), s.checkOpenProfiles(ctx))
	}
	errs = append(errs, s.backfillContacts(ctx), s.resumeImports(ctx))
	return errors.Join(errs...)
}

// reconcileSessions reads back sessions still in flight, or whose first month is unsettled, a minute after their last change.
func (s *Service) reconcileSessions(ctx context.Context) error {
	rows, err := s.Repo.OpenSessions(ctx, s.now().Add(-time.Minute), 100)
	if err != nil {
		return err
	}
	for i := range rows {
		row := &rows[i]
		gs, err := s.api().GetConnection(ctx, ext(row.OrganizationID), row.ID)
		if ge, ok := graphkit.AsError(err); ok && ge.Code == graphkit.CodeNotFound {
			gs, err = &graphkit.ConnectSession{ID: row.ID, Kind: row.Kind, Status: graphkit.ConnectExpired}, nil
		}
		if err != nil {
			log.Warn().Err(err).Str("session_id", row.ID).Msg("social: reading a connect session back failed")
			continue
		}
		if row.Status == gs.Status && !gs.Ended() {
			continue
		}
		if _, err := s.observe(ctx, row, gs); err != nil {
			log.Warn().Err(err).Str("session_id", row.ID).Msg("social: settling a connect session failed")
		}
	}
	return nil
}

// backfillContacts seeds contact_social_identities from the linkedin_url custom field, one batch per pass.
func (s *Service) backfillContacts(ctx context.Context) error {
	if s.Repo == nil {
		return nil
	}
	after, done, err := s.Repo.BackfillProgress(ctx)
	if err != nil || done {
		return err
	}
	last, n, err := s.Repo.BackfillContactSocial(ctx, after, backfillBatch)
	if err != nil {
		return err
	}
	if n == 0 {
		last = after
	}
	return s.Repo.SaveBackfillProgress(ctx, last, n < backfillBatch)
}
