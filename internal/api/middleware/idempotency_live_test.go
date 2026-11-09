package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/app/idempotency"
)

// End-to-end checks of Idempotency-Key through the real store. Skipped unless
// WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/api/middleware/ -run LiveIdempotency -v
func TestLiveIdempotencyKeepsIdentityAndCompletion(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	user, org := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Idem', '')`, user, user.String()+"@example.test"); err != nil {
		t.Fatalf("user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Idem', $2)`, org, user); err != nil {
		t.Fatalf("org: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, org)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user)
		pool.Close()
	})

	var effects atomic.Int32
	hold := make(chan struct{})
	entered := make(chan struct{}, 1)
	r := idempotentRouter(idempotency.NewService(pool), org, asUser(user.String()), func(c *gin.Context) {
		effects.Add(1)
		switch c.Query("mode") {
		case "hangup":
			// The side effect commits, then the client goes away.
			c.Request.Context().Value(cancelKey{}).(context.CancelFunc)()
		case "hold":
			entered <- struct{}{}
			<-hold
		case "fail":
			c.JSON(http.StatusInternalServerError, gin.H{"error": "boom"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
	})
	send := func(reqCtx context.Context, path, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(reqCtx, http.MethodPatch, path, strings.NewReader(`{"name":"x"}`))
		req.Header.Set(IdempotencyKeyHeader, key)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	code := func(rec *httptest.ResponseRecorder) string {
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return body.Code
	}

	hangup, cancel := context.WithCancel(ctx)
	defer cancel()
	send(context.WithValue(hangup, cancelKey{}, cancel), "/contacts/aaa?mode=hangup", "k1")
	retry := send(ctx, "/contacts/aaa?mode=hangup", "k1")
	if retry.Code != http.StatusOK || retry.Header().Get(IdempotencyReplayedHeader) != "true" || effects.Load() != 1 {
		t.Fatalf("retry after a hang-up = %d replayed=%q effects=%d, want the stored 200 and one effect", retry.Code, retry.Header().Get(IdempotencyReplayedHeader), effects.Load())
	}
	if other := send(ctx, "/contacts/bbb?mode=hangup", "k1"); other.Code != http.StatusConflict || strings.Contains(other.Body.String(), "aaa") || effects.Load() != 1 {
		t.Fatalf("same key on another contact = %d %s, want 409 without the first answer", other.Code, other.Body.String())
	}

	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- send(ctx, "/contacts/aaa?mode=hold", "k2") }()
	select {
	case <-entered:
	case early := <-done:
		t.Fatalf("held request returned %d %s before reaching the handler", early.Code, early.Body.String())
	case <-time.After(10 * time.Second):
		t.Fatal("held request neither reached the handler nor returned")
	}
	if second := send(ctx, "/contacts/aaa?mode=hold", "k2"); second.Code != http.StatusConflict || code(second) != "conflict" {
		t.Fatalf("concurrent duplicate = %d %q, want 409 while the first runs", second.Code, code(second))
	}
	close(hold)
	if first := <-done; first.Code != http.StatusOK {
		t.Fatalf("first of the concurrent pair = %d, want 200", first.Code)
	}
	if replay := send(ctx, "/contacts/aaa?mode=hold", "k2"); replay.Header().Get(IdempotencyReplayedHeader) != "true" || effects.Load() != 2 {
		t.Fatalf("after the pair, effects = %d, want exactly one more", effects.Load())
	}

	send(ctx, "/contacts/aaa?mode=fail", "k3")
	if again := send(ctx, "/contacts/aaa?mode=fail", "k3"); again.Code != http.StatusConflict || code(again) != "idempotency_outcome_unknown" || effects.Load() != 3 {
		t.Fatalf("retry after a server error = %d %q effects=%d, want idempotency_outcome_unknown without re-running", again.Code, code(again), effects.Load())
	}
}

type cancelKey struct{}
