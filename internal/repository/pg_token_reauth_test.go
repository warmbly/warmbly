package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/warmbly/warmbly/internal/models"
)

type sessionProofTx struct {
	pgx.Tx
	query string
	args  []any
}

func (tx *sessionProofTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.query, tx.args = sql, args
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestSessionInsertPersistsOriginalProofRatherThanCreationTime(t *testing.T) {
	created := time.Now().UTC()
	proof := created.Add(-time.Minute)
	tx := &sessionProofTx{}
	r := &tokenRepository{}
	if xerr := r.GenerateSession(t.Context(), tx, &models.Session{CreatedAt: created, ReauthAt: &proof}); xerr != nil {
		t.Fatal(xerr)
	}
	if len(tx.args) != 20 || !strings.Contains(tx.query, "$18, $19, $20") {
		t.Fatal("reauth_at is not an independent SQL parameter", tx.query)
	}
	if at, ok := tx.args[18].(*time.Time); !ok || !at.Equal(proof) {
		t.Fatal("session insertion manufactured fresh auth")
	}
}

func TestSessionInsertPersistsPortalBinding(t *testing.T) {
	agency := uuid.New()
	tx := &sessionProofTx{}
	if xerr := (&tokenRepository{}).GenerateSession(t.Context(), tx, &models.Session{PortalAgencyID: &agency}); xerr != nil {
		t.Fatal(xerr)
	}
	if !strings.Contains(tx.query, "portal_agency_id") {
		t.Fatal("the insert does not name portal_agency_id")
	}
	if got, ok := tx.args[19].(*uuid.UUID); !ok || got == nil || *got != agency {
		t.Fatalf("portal binding parameter = %v, want %v", tx.args[19], agency)
	}
}
