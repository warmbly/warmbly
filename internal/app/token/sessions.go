package token

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// SessionView is the safe, customer-facing shape of a session. It deliberately
// omits the access/refresh nonces and any internal-only fields.
type SessionView struct {
	ID           string    `json:"id"`
	Current      bool      `json:"current"`
	Browser      string    `json:"browser"`
	OS           string    `json:"os"`
	City         string    `json:"location_city"`
	Region       string    `json:"location_region"`
	Country      string    `json:"location_country"`
	CountryCode  string    `json:"country_code"`
	AuthProvider string    `json:"auth_provider"`
	MFAVerified  bool      `json:"mfa_verified"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
}

func toSessionView(sess *models.Session, currentID uuid.UUID) SessionView {
	lastActive := sess.LastRefreshedAt
	if lastActive.IsZero() {
		lastActive = sess.CreatedAt
	}
	return SessionView{
		ID:           sess.ID.String(),
		Current:      sess.ID == currentID,
		Browser:      sess.BrowserName,
		OS:           sess.OSName,
		City:         sess.LocationCity,
		Region:       sess.LocationRegion,
		Country:      sess.LocationCountry,
		CountryCode:  sess.LocationCountryCode,
		AuthProvider: sess.AuthProvider,
		MFAVerified:  sess.MFAVerified,
		CreatedAt:    sess.CreatedAt,
		LastActiveAt: lastActive,
	}
}

// ListSessions returns the user's active sessions, with the caller's current
// session flagged and floated to the top.
func (s *tokenService) ListSessions(ctx context.Context, userID, currentSessionID uuid.UUID) ([]SessionView, *errx.Error) {
	sessions, err := s.tokenRepository.ListSessionsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	views := make([]SessionView, 0, len(sessions))
	for _, sess := range sessions {
		view := toSessionView(sess, currentSessionID)
		if view.Current {
			// Current session first; the rest keep their recency order.
			views = append([]SessionView{view}, views...)
		} else {
			views = append(views, view)
		}
	}

	return views, nil
}

// RevokeSessionByID ends one of the user's other sessions. Revoking the current
// session is refused — that's what sign out is for.
func (s *tokenService) RevokeSessionByID(ctx context.Context, userID, sessionID, currentSessionID uuid.UUID) *errx.Error {
	if sessionID == currentSessionID {
		return errx.ErrSessionCurrent
	}

	ok, err := s.tokenRepository.RevokeSessionByID(ctx, userID, sessionID, time.Now())
	if err != nil {
		return err
	}
	if !ok {
		return errx.ErrSessionNotFound
	}

	err = s.evictRevoked(ctx, sessionID, userID, time.Now())
	s.notifyRevoked(ctx, userID)
	return err
}

// RevokeOtherSessions ends every active session except the caller's current
// one ("sign out everywhere else"). uuid.Nil ends all of them.
func (s *tokenService) RevokeOtherSessions(ctx context.Context, userID, currentSessionID uuid.UUID) *errx.Error {
	err := s.revokeOthers(ctx, userID, currentSessionID)
	s.notifyRevoked(ctx, userID)
	return err
}

// notifyRevoked closes the user's open websockets; each reconnects only with a
// ticket minted from a session that is still live.
func (s *tokenService) notifyRevoked(ctx context.Context, userID uuid.UUID) {
	if s.revocations != nil {
		s.revocations.PublishSessionsRevoked(ctx, userID)
	}
}

func (s *tokenService) revokeOthers(ctx context.Context, userID, currentSessionID uuid.UUID) *errx.Error {
	ids, err := s.tokenRepository.ListOtherActiveSessionIDs(ctx, userID, currentSessionID)
	if err != nil {
		return err
	}

	now := time.Now()
	if err := s.tokenRepository.RevokeOtherSessions(ctx, userID, currentSessionID); err != nil {
		return err
	}

	// Every id is evicted even when one fails, and a failed eviction is
	// overwritten with a revoked tombstone, so no cached copy outlives the row.
	var first *errx.Error
	for _, id := range ids {
		if err := s.evictRevoked(ctx, id, userID, now); err != nil && first == nil {
			first = err
		}
	}

	return first
}

// evictRevoked overwrites a revoked session's cache entry with a tombstone that
// outlives any cached copy; only when that write is refused does it delete.
func (s *tokenService) evictRevoked(ctx context.Context, id, userID uuid.UUID, revokedAt time.Time) *errx.Error {
	tomb := &models.Session{ID: id, UserID: userID, RevokedAt: &revokedAt}
	if err := s.saveSession(ctx, tomb, revokedTombstoneTTL); err == nil {
		return nil
	}
	return s.deleteSession(ctx, id)
}
