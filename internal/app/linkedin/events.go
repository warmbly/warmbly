package linkedin

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

const backfillBatch = 500

// ApplySocialEvent implements store.Social: GraphKit's social.* webhooks, and their replay from its event log.
func (s *Service) ApplySocialEvent(ctx context.Context, orgID uuid.UUID, ev graphkit.Event) error {
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
	}
	// Actions and accepted invitations belong to campaign steps, which come later.
	return nil
}

// ReconcileSocial implements store.Social: connect sessions no webhook settled, and the contact LinkedIn backfill.
func (s *Service) ReconcileSocial(ctx context.Context) error {
	var errs []error
	if s.available() {
		errs = append(errs, s.reconcileSessions(ctx))
	}
	errs = append(errs, s.backfillContacts(ctx))
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
			log.Warn().Err(err).Str("session_id", row.ID).Msg("linkedin: reading a connect session back failed")
			continue
		}
		if row.Status == gs.Status && !gs.Ended() {
			continue
		}
		if _, err := s.observe(ctx, row, gs); err != nil {
			log.Warn().Err(err).Str("session_id", row.ID).Msg("linkedin: settling a connect session failed")
		}
	}
	return nil
}

// backfillContacts fills contacts.linkedin_url for rows written before the column, one batch per pass.
func (s *Service) backfillContacts(ctx context.Context) error {
	if s.Repo == nil {
		return nil
	}
	after, done, err := s.Repo.BackfillProgress(ctx)
	if err != nil || done {
		return err
	}
	last, n, err := s.Repo.BackfillContactLinkedIn(ctx, after, backfillBatch)
	if err != nil {
		return err
	}
	if n == 0 {
		last = after
	}
	return s.Repo.SaveBackfillProgress(ctx, last, n < backfillBatch)
}
