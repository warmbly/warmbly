package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/idempotency"
	"github.com/warmbly/warmbly/internal/errx"
)

type idempotencyBegin struct {
	path, hash string
}

type fakeIdempotencyService struct {
	record *idempotency.Record
	state  idempotency.State
	body   []byte

	begins []idempotencyBegin
	// finish is "complete", "release" or "uncertain", with the context error
	// seen when it was recorded.
	finish    string
	finishErr error
	status    int
}

func (s *fakeIdempotencyService) Begin(ctx context.Context, scope idempotency.Scope, key, method, path, requestHash string) (*idempotency.Record, idempotency.State, *errx.Error) {
	s.begins = append(s.begins, idempotencyBegin{path: path, hash: requestHash})
	if s.record == nil {
		s.record = &idempotency.Record{ID: uuid.New(), StatusCode: http.StatusCreated}
	}
	return s.record, s.state, nil
}

func (s *fakeIdempotencyService) Complete(ctx context.Context, recordID uuid.UUID, statusCode int, responseBody []byte, contentType string) *errx.Error {
	s.finish, s.finishErr, s.status = "complete", ctx.Err(), statusCode
	s.record.StatusCode = statusCode
	s.body = append([]byte(nil), responseBody...)
	return nil
}

func (s *fakeIdempotencyService) Release(ctx context.Context, recordID uuid.UUID) *errx.Error {
	s.finish, s.finishErr = "release", ctx.Err()
	return nil
}

func (s *fakeIdempotencyService) MarkUncertain(ctx context.Context, recordID uuid.UUID, statusCode int) *errx.Error {
	s.finish, s.finishErr, s.status = "uncertain", ctx.Err(), statusCode
	return nil
}

// idempotentRouter mounts handler behind the middleware for one org and the
// given credential setup.
func idempotentRouter(svc idempotency.Service, orgID uuid.UUID, auth func(*gin.Context), handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &Handler{IdempotencyService: svc}
	r := gin.New()
	r.Use(RequestIDMiddleware())
	setup := func(c *gin.Context) {
		c.Set(OrganizationIDKey, orgID)
		if auth != nil {
			auth(c)
		}
		c.Next()
	}
	r.POST("/contacts", setup, h.IdempotencyMiddleware(), handler)
	r.PATCH("/contacts/:id", setup, h.IdempotencyMiddleware(), handler)
	return r
}

func asUser(id string) func(*gin.Context) {
	return func(c *gin.Context) {
		c.Set(AuthTypeKey, AuthTypeJWT)
		c.Set(UserIDKey, id)
	}
}

func asAPIKey(keyID, userID string) func(*gin.Context) {
	return func(c *gin.Context) {
		c.Set(AuthTypeKey, AuthTypeAPIKey)
		c.Set(APIKeyIDKey, keyID)
		c.Set(UserIDKey, userID)
	}
}

func asOAuth(grantID, appID uuid.UUID, userID string) func(*gin.Context) {
	return func(c *gin.Context) {
		c.Set(AuthTypeKey, AuthTypeOAuth)
		c.Set(OAuthGrantIDKey, grantID)
		c.Set(OAuthApplicationIDKey, appID)
		c.Set(UserIDKey, userID)
	}
}

func sendIdempotent(r http.Handler, method, path, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Header.Set(IdempotencyKeyHeader, key)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestIdempotencyMiddlewareStoresResponse(t *testing.T) {
	svc := &fakeIdempotencyService{state: idempotency.StateStarted}
	r := idempotentRouter(svc, uuid.New(), asUser(uuid.NewString()), func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"id": "contact_123"})
	})

	rec := sendIdempotent(r, http.MethodPost, "/contacts", "idem_123", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if svc.finish != "complete" || string(svc.body) == "" {
		t.Fatalf("finish = %q body = %q, want the response stored", svc.finish, svc.body)
	}
}

func TestIdempotencyMiddlewareReplaysResponse(t *testing.T) {
	contentType := "application/json; charset=utf-8"
	svc := &fakeIdempotencyService{
		state: idempotency.StateReplay,
		record: &idempotency.Record{
			ID:           uuid.New(),
			StatusCode:   http.StatusCreated,
			ResponseBody: []byte(`{"id":"contact_123"}`),
			ContentType:  &contentType,
		},
	}
	ran := false
	r := idempotentRouter(svc, uuid.New(), asUser(uuid.NewString()), func(c *gin.Context) {
		ran = true
		c.JSON(http.StatusTeapot, gin.H{"unexpected": true})
	})

	rec := sendIdempotent(r, http.MethodPost, "/contacts", "idem_123", "")
	if ran {
		t.Fatal("a replay ran the handler again")
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if rec.Header().Get(IdempotencyReplayedHeader) != "true" {
		t.Fatalf("missing replay header")
	}
	if got := rec.Body.String(); got != `{"id":"contact_123"}` {
		t.Fatalf("body = %q", got)
	}
}

func TestIdempotencyIdentityNamesTheConcreteResourceAndCredential(t *testing.T) {
	svc := &fakeIdempotencyService{state: idempotency.StateStarted}
	user := uuid.NewString()
	ok := func(c *gin.Context) { c.Status(http.StatusNoContent) }
	orgID := uuid.New()
	asMember := idempotentRouter(svc, orgID, asUser(user), ok)
	asKeyA := idempotentRouter(svc, orgID, asAPIKey(uuid.NewString(), user), ok)
	asKeyB := idempotentRouter(svc, orgID, asAPIKey(uuid.NewString(), user), ok)
	app := uuid.New()
	asGrantA := idempotentRouter(svc, orgID, asOAuth(uuid.New(), app, user), ok)
	asGrantB := idempotentRouter(svc, orgID, asOAuth(uuid.New(), app, user), ok)

	sendIdempotent(asMember, http.MethodPatch, "/contacts/aaa", "k", `{"name":"x"}`)
	sendIdempotent(asMember, http.MethodPatch, "/contacts/bbb", "k", `{"name":"x"}`)
	sendIdempotent(asKeyA, http.MethodPatch, "/contacts/aaa", "k", `{"name":"x"}`)
	sendIdempotent(asKeyB, http.MethodPatch, "/contacts/aaa", "k", `{"name":"x"}`)
	sendIdempotent(asGrantA, http.MethodPatch, "/contacts/aaa", "k", `{"name":"x"}`)
	sendIdempotent(asGrantB, http.MethodPatch, "/contacts/aaa", "k", `{"name":"x"}`)
	sendIdempotent(asMember, http.MethodPatch, "/contacts/aaa", "k", `{"name":"x"}`)

	if got := svc.begins[0].path; got != "/contacts/aaa" {
		t.Fatalf("stored path = %q, want the concrete path", got)
	}
	seen := map[string]int{}
	for i, b := range svc.begins[:6] {
		if prev, dup := seen[b.hash]; dup {
			t.Fatalf("requests %d and %d share an identity hash", prev, i)
		}
		seen[b.hash] = i
	}
	if svc.begins[6].hash != svc.begins[0].hash {
		t.Fatal("the same request from the same credential produced a different identity")
	}
}

func TestIdempotencyHashNeverEqualsThePreviousReleaseHash(t *testing.T) {
	// The previous release hashed method, path, query and body only; its records
	// must read as a different request, never as a match.
	previous := sha256.New()
	for _, part := range [][]byte{[]byte(http.MethodPost), []byte("/contacts"), nil} {
		previous.Write(part)
		previous.Write([]byte{0})
	}
	if hex.EncodeToString(previous.Sum(nil)) == hashRequest("user:1", http.MethodPost, "/contacts", "", nil) {
		t.Fatal("a record written by the previous release matches the new identity")
	}
}

func TestIdempotencyOutcomeIsRecordedAfterTheClientHangsUp(t *testing.T) {
	svc := &fakeIdempotencyService{state: idempotency.StateStarted}
	ctx, cancel := context.WithCancel(context.Background())
	r := idempotentRouter(svc, uuid.New(), asUser(uuid.NewString()), func(c *gin.Context) {
		// The side effect commits, then the client goes away.
		cancel()
		c.JSON(http.StatusCreated, gin.H{"id": "contact_123"})
	})

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/contacts", nil)
	req.Header.Set(IdempotencyKeyHeader, "idem_123")
	r.ServeHTTP(httptest.NewRecorder(), req)

	if svc.finish != "complete" || svc.status != http.StatusCreated {
		t.Fatalf("finish = %q status = %d, want the 201 completed", svc.finish, svc.status)
	}
	if svc.finishErr != nil {
		t.Fatalf("completion was written on a cancelled context: %v", svc.finishErr)
	}
}

func TestIdempotencyOutcomeFollowsTheResponse(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusCreated, "complete"},
		{http.StatusNoContent, "complete"},
		{http.StatusBadRequest, "complete"},
		{http.StatusConflict, "complete"},
		{http.StatusUnauthorized, "release"},
		{http.StatusForbidden, "release"},
		{http.StatusTooManyRequests, "release"},
		{http.StatusInternalServerError, "uncertain"},
		{http.StatusBadGateway, "uncertain"},
		{http.StatusServiceUnavailable, "uncertain"},
	}
	for _, tc := range cases {
		svc := &fakeIdempotencyService{state: idempotency.StateStarted}
		r := idempotentRouter(svc, uuid.New(), asUser(uuid.NewString()), func(c *gin.Context) {
			c.JSON(tc.status, gin.H{})
		})
		sendIdempotent(r, http.MethodPost, "/contacts", "k", "")
		if svc.finish != tc.want {
			t.Errorf("%d: finish = %q, want %q", tc.status, svc.finish, tc.want)
		}
	}
}

func TestIdempotencyPanicLeavesTheOutcomeUncertain(t *testing.T) {
	svc := &fakeIdempotencyService{state: idempotency.StateStarted}
	r := idempotentRouter(svc, uuid.New(), asUser(uuid.NewString()), func(c *gin.Context) {
		panic("boom")
	})
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the panic was swallowed instead of reaching the recovery middleware")
			}
		}()
		sendIdempotent(r, http.MethodPost, "/contacts", "k", "")
	}()
	if svc.finish != "uncertain" {
		t.Fatalf("finish = %q, want uncertain", svc.finish)
	}
}

func TestIdempotencyStateAnswers(t *testing.T) {
	cases := []struct {
		state  idempotency.State
		status int
		code   string
	}{
		{idempotency.StateProcessing, http.StatusConflict, "conflict"},
		{idempotency.StateConflict, http.StatusConflict, "conflict"},
		{idempotency.StateUncertain, http.StatusConflict, "idempotency_outcome_unknown"},
	}
	for _, tc := range cases {
		svc := &fakeIdempotencyService{state: tc.state}
		ran := false
		r := idempotentRouter(svc, uuid.New(), asUser(uuid.NewString()), func(c *gin.Context) {
			ran = true
		})
		rec := sendIdempotent(r, http.MethodPost, "/contacts", "k", "")
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if ran || rec.Code != tc.status || body.Code != tc.code {
			t.Errorf("%s: ran=%v status=%d code=%q, want %d %q without running", tc.state, ran, rec.Code, body.Code, tc.status, tc.code)
		}
	}
}
