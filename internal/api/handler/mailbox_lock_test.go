package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/store"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
	"github.com/warmbly/warmbly/internal/repository"
)

// lockStoreRepo answers which purchase sits behind a mailbox; nothing else is called.
type lockStoreRepo struct {
	repository.StoreRepository
	org, account uuid.UUID
	status       string
}

func (r *lockStoreRepo) GetMailboxByAccount(_ context.Context, orgID, accountID uuid.UUID) (*models.StoreMailbox, error) {
	if orgID != r.org || accountID != r.account {
		return nil, nil
	}
	id := r.account
	return &models.StoreMailbox{ID: uuid.New(), OrganizationID: r.org, Status: r.status, EmailAccountID: &id}, nil
}

// lockEmails records which mailbox operations got past the lock.
type lockEmails struct {
	email.EmailService
	mu    sync.Mutex
	calls []string
}

func (e *lockEmails) record(op string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, op)
}

func (e *lockEmails) Delete(context.Context, string, string) *errx.Error {
	e.record("delete")
	return nil
}

func (e *lockEmails) OAuthReauth(context.Context, string, *uuid.UUID, uuid.UUID, string) (*models.EmailOnboardingStartResponse, *errx.Error) {
	e.record("reauth")
	return &models.EmailOnboardingStartResponse{}, nil
}

func (e *lockEmails) UpdateSMTPIMAPCredentials(context.Context, *uuid.UUID, uuid.UUID, *models.SmtpImap) (*models.Email, *errx.Error) {
	e.record("smtp")
	return nil, errx.ErrNotFound
}

func (e *lockEmails) SwitchToAppPassword(context.Context, *uuid.UUID, uuid.UUID, string) (*models.Email, *errx.Error) {
	e.record("app_password")
	return nil, errx.ErrNotFound
}

func TestPurchasedMailboxIsLockedUntilItsSubscriptionEnds(t *testing.T) {
	org, account := uuid.New(), uuid.New()
	routes := []struct {
		name, method, path, body, op string
	}{
		{"delete", http.MethodDelete, "/emails/" + account.String(), "", "delete"},
		{"oauth reauth", http.MethodPost, "/reauth/" + account.String(), "", "reauth"},
		{"smtp credentials", http.MethodPut, "/smtp/" + account.String(), `{}`, "smtp"},
		{"app password", http.MethodPost, "/app-password/" + account.String(), `{"app_password":"abcd efgh ijkl mnop"}`, "app_password"},
	}
	for _, status := range []string{"active", "cancelling", "suspended", "cancelled", "failed"} {
		locked := status != "cancelled" && status != "failed"
		for _, rt := range routes {
			t.Run(status+"/"+rt.name, func(t *testing.T) {
				emails := &lockEmails{}
				h := &Handler{EmailService: emails, StoreService: store.New(store.Deps{Repo: &lockStoreRepo{org: org, account: account, status: status}})}
				w := serveLocked(h, org, rt.method, rt.path, rt.body)
				reached := len(emails.calls) == 1 && emails.calls[0] == rt.op
				if locked {
					if w.Code != http.StatusForbidden || responseCode(w) != "purchased_mailbox" || reached {
						t.Fatalf("status %d code %q calls %v, want 403 purchased_mailbox and nothing done", w.Code, responseCode(w), emails.calls)
					}
					return
				}
				if !reached || w.Code == http.StatusForbidden {
					t.Fatalf("status %d calls %v, want the request through", w.Code, emails.calls)
				}
			})
		}
	}
}

func TestBulkDeleteRefusesALivePurchasedMailboxInItsRow(t *testing.T) {
	org, bought, other := uuid.New(), uuid.New(), uuid.New()
	for _, tt := range []struct {
		status string
		locked bool
	}{{"active", true}, {"cancelled", false}} {
		t.Run(tt.status, func(t *testing.T) {
			emails := &lockEmails{}
			h := &Handler{EmailService: emails, StoreService: store.New(store.Deps{API: graphkit.NewFake(), Repo: &lockStoreRepo{org: org, account: bought, status: tt.status}})}
			w := serveLocked(h, org, http.MethodPost, "/bulk/delete", `{"email_ids":["`+bought.String()+`","`+other.String()+`"]}`)
			var res bulkMailboxResponse
			if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || w.Code != http.StatusOK {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
			want := 2
			if tt.locked {
				want = 1
			}
			if res.Succeeded != want || len(emails.calls) != want {
				t.Fatalf("succeeded %d deletes %d, want %d", res.Succeeded, len(emails.calls), want)
			}
			for _, r := range res.Results {
				refused := r.ID == bought && tt.locked
				if refused != !r.OK || (refused && (r.Code != "purchased_mailbox" || !strings.Contains(r.Message, "Subscription"))) {
					t.Fatalf("row %+v", r)
				}
			}
		})
	}
}

func serveLocked(h *Handler, org uuid.UUID, method, path, body string) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(middleware.RequestIDMiddleware(), func(c *gin.Context) {
		c.Set(middleware.OrganizationIDKey, org)
		c.Next()
	})
	r.DELETE("/emails/:id", h.DeleteEmail)
	r.POST("/bulk/delete", h.BulkDeleteEmails)
	r.POST("/reauth/:id", h.ReauthEmailOAuth)
	r.PUT("/smtp/:id", h.UpdateEmailSMTPIMAP)
	r.POST("/app-password/:id", h.SwitchEmailToAppPassword)
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func responseCode(w *httptest.ResponseRecorder) string {
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Code
}
