package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/unsublink"
	"github.com/warmbly/warmbly/internal/repository"
)

type stubUnsubTickets struct {
	ticket  *repository.UnsubscribeTicket
	err     error
	lookups int
}

func (s *stubUnsubTickets) Mint(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) (string, error) {
	return "", errors.New("not used")
}

func (s *stubUnsubTickets) Resolve(_ context.Context, token string) (*repository.UnsubscribeTicket, error) {
	s.lookups++
	if s.err != nil {
		return nil, s.err
	}
	if s.ticket == nil || s.ticket.Token != token {
		return nil, nil
	}
	return s.ticket, nil
}

func newUnsubRouter(h *Handler) *gin.Engine {
	r := gin.New()
	r.GET("/unsubscribe/:token", h.UnsubscribePage)
	return r
}

func getUnsub(h *Handler, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	newUnsubRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/unsubscribe/"+token, nil))
	return w
}

// The two token generations share one route, and shape alone decides which
// way each is resolved: a signed link already in an inbox must not cost a
// lookup, and a short ticket must not be handed to the verifier.
func TestUnsubscribeHonoursBothTokenShapes(t *testing.T) {
	signer := unsublink.New("secret", "https://api.example.com")
	tickets := &stubUnsubTickets{ticket: &repository.UnsubscribeTicket{
		Token:          "tickettoken00000000000",
		OrganizationID: uuid.New(),
		CampaignID:     uuid.New(),
		ContactID:      uuid.New(),
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}}
	h := &Handler{UnsubscribeLinks: signer, UnsubscribeTickets: tickets}

	if w := getUnsub(h, "tickettoken00000000000"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Unsubscribe from these emails?") {
		t.Errorf("a stored ticket should open the confirm page: %d %s", w.Code, w.Body.String())
	}

	signed := signer.Token(uuid.New(), uuid.New(), uuid.New(), time.Now())
	before := tickets.lookups
	if w := getUnsub(h, signed); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Unsubscribe from these emails?") {
		t.Errorf("a signed link should still open the confirm page: %d %s", w.Code, w.Body.String())
	}
	if tickets.lookups != before {
		t.Errorf("a signed token cost %d ticket lookups", tickets.lookups-before)
	}

	// Junk that is neither shape is refused without touching the store.
	before = tickets.lookups
	if w := getUnsub(h, "nope"); w.Code != http.StatusBadRequest {
		t.Errorf("junk token: %d", w.Code)
	}
	if tickets.lookups != before {
		t.Error("junk reached the store")
	}
}

func TestUnsubscribeTicketThatIsUnknownOrExpired(t *testing.T) {
	h := &Handler{
		UnsubscribeLinks:   unsublink.New("secret", "https://api.example.com"),
		UnsubscribeTickets: &stubUnsubTickets{},
	}
	if w := getUnsub(h, "unknowntoken0000000000"); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid") {
		t.Errorf("unknown ticket: %d %s", w.Code, w.Body.String())
	}

	h.UnsubscribeTickets = &stubUnsubTickets{ticket: &repository.UnsubscribeTicket{
		Token:     "expiredtok000000000000",
		ContactID: uuid.New(),
		ExpiresAt: time.Now().Add(-time.Hour),
	}}
	w := getUnsub(h, "expiredtok000000000000")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "expired") {
		t.Errorf("expired ticket should say so, not 'invalid': %d %s", w.Code, w.Body.String())
	}
}

// A lookup that could not run is not a link that is not real. Telling the
// recipient their opt-out is invalid when the database blinked is the one
// wrong answer here.
func TestUnsubscribeStoreFailureIsNotAnInvalidLink(t *testing.T) {
	h := &Handler{
		UnsubscribeLinks:   unsublink.New("secret", "https://api.example.com"),
		UnsubscribeTickets: &stubUnsubTickets{err: errors.New("postgres is down")},
	}

	w := getUnsub(h, "tickettoken00000000000")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", w.Code)
	}
	if body := w.Body.String(); strings.Contains(body, "invalid") || !strings.Contains(body, "Try again shortly") {
		t.Errorf("wrong page for a failed lookup: %s", body)
	}
}

// RFC 8058: the provider retries a 5xx and gives up on a 2xx, so a failed
// lookup must not be answered like a dead link or the click is lost.
func TestOneClickRetriesAFailedLookupButNotADeadLink(t *testing.T) {
	post := func(h *Handler, token string) int {
		r := gin.New()
		r.POST("/unsubscribe/:token", h.UnsubscribeSubmit)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/unsubscribe/"+token, strings.NewReader("List-Unsubscribe=One-Click"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.ServeHTTP(w, req)
		return w.Code
	}
	signer := unsublink.New("secret", "https://api.example.com")

	h := &Handler{UnsubscribeLinks: signer, UnsubscribeTickets: &stubUnsubTickets{err: errors.New("postgres is down")}}
	if code := post(h, "tickettoken00000000000"); code != http.StatusBadGateway {
		t.Errorf("failed lookup: %d, want 502 so the provider retries", code)
	}

	h = &Handler{UnsubscribeLinks: signer, UnsubscribeTickets: &stubUnsubTickets{}}
	if code := post(h, "unknowntoken0000000000"); code != http.StatusOK {
		t.Errorf("unknown ticket: %d, want 200 so the provider stops", code)
	}
}
