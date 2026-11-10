package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/notification"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type boundTokens struct {
	token.TokenService
	session *models.Session
}

func (f boundTokens) ValidateAccessToken(context.Context, string) (*models.Session, *errx.Error) {
	return f.session, nil
}

type clientsOf map[uuid.UUID]uuid.UUID

func (f clientsOf) ClientAgency(_ context.Context, orgID uuid.UUID) (*uuid.UUID, error) {
	if a, ok := f[orgID]; ok {
		return &a, nil
	}
	return nil, nil
}

type twoWorkspaces struct {
	organization.OrganizationService
	orgs []uuid.UUID
}

func (f twoWorkspaces) GetUserOrganizations(context.Context, uuid.UUID) ([]models.OrganizationMember, *errx.Error) {
	out := make([]models.OrganizationMember, 0, len(f.orgs))
	for _, id := range f.orgs {
		out = append(out, models.OrganizationMember{OrganizationID: id})
	}
	return out, nil
}

type markRecorder struct {
	notification.Service
	all    int
	single int
	in     [][]uuid.UUID
	ids    []*uuid.UUID
}

func (m *markRecorder) MarkAllRead(context.Context, uuid.UUID) *errx.Error { m.all++; return nil }
func (m *markRecorder) MarkRead(context.Context, uuid.UUID, uuid.UUID) *errx.Error {
	m.single++
	return nil
}
func (m *markRecorder) MarkReadIn(_ context.Context, _ uuid.UUID, id *uuid.UUID, orgs []uuid.UUID) *errx.Error {
	m.in = append(m.in, orgs)
	m.ids = append(m.ids, id)
	return nil
}

// Marking notifications read from a portal session touches only the agency's client workspaces.
func TestPortalSessionMarksReadOnlyItsAgencysNotifications(t *testing.T) {
	agency, client, other := uuid.New(), uuid.New(), uuid.New()
	session := &models.Session{ID: uuid.New(), UserID: uuid.New(), PortalAgencyID: &agency}
	m := &middleware.Handler{TokenService: boundTokens{session: session}, PortalClients: clientsOf{client: agency, other: uuid.New()}}
	rec := &markRecorder{}
	h := &Handler{NotificationService: rec, OrganizationService: twoWorkspaces{orgs: []uuid.UUID{client, other}}}

	r := gin.New()
	r.PUT("/v1/auth/me/notifications", m.AuthMiddleware(), h.MarkAllNotificationsRead)
	r.POST("/v1/auth/me/notifications/:id/read", m.AuthMiddleware(), h.MarkNotificationRead)
	one := uuid.New()
	for _, req := range []*http.Request{
		httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/v1/auth/me/notifications", nil),
		httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/auth/me/notifications/"+one.String()+"/read", nil),
	} {
		req.Header.Set("Authorization", "Bearer t")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", req.URL.Path, w.Code, w.Body.String())
		}
	}
	if rec.all != 0 || rec.single != 0 {
		t.Fatalf("a portal session used the unscoped marks: all=%d single=%d", rec.all, rec.single)
	}
	if len(rec.in) != 2 || len(rec.in[0]) != 1 || rec.in[0][0] != client || rec.ids[0] != nil || rec.ids[1] == nil || *rec.ids[1] != one {
		t.Fatalf("scoped marks = %v ids %v, want only the client workspace", rec.in, rec.ids)
	}
}
