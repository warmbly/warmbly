package idempotency

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Live checks of the idempotency store against a real Postgres. Skipped unless
// WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/app/idempotency/ -run Live -v

// liveTestOrg opens WARMBLY_TEST_DB and creates an organization that is removed
// with its idempotency records when the test ends.
func liveTestOrg(t *testing.T) (*pgxpool.Pool, uuid.UUID) {
	t.Helper()
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
	return pool, org
}

func TestLiveIdempotencyConcurrentBeginsClaimOnce(t *testing.T) {
	pool, org := liveTestOrg(t)
	svc := NewService(pool)
	ctx := context.Background()

	const racers = 16
	states := make([]State, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, state, xerr := svc.Begin(ctx, OrgScope(org), "race", "POST", "/contacts", "h")
			if xerr != nil {
				t.Errorf("begin %d: %v", i, xerr)
			}
			states[i] = state
		}(i)
	}
	wg.Wait()
	started := 0
	for _, s := range states {
		switch s {
		case StateStarted:
			started++
		case StateProcessing:
		default:
			t.Fatalf("a racer answered %q, want started or processing", s)
		}
	}
	if started != 1 {
		t.Fatalf("%d racers claimed the key, want exactly 1", started)
	}
}

func TestLiveIdempotencyOutcomes(t *testing.T) {
	pool, org := liveTestOrg(t)
	svc := NewService(pool)
	ctx := context.Background()
	begin := func(key, path, hash string) (*Record, State) {
		t.Helper()
		rec, state, xerr := svc.Begin(ctx, OrgScope(org), key, "PATCH", path, hash)
		if xerr != nil {
			t.Fatalf("begin %s: %v", key, xerr)
		}
		return rec, state
	}

	rec, _ := begin("done", "/contacts/a", "h-a")
	if xerr := svc.Complete(ctx, rec.ID, 200, []byte(`{"id":"a"}`), "application/json"); xerr != nil {
		t.Fatal(xerr)
	}
	if got, state := begin("done", "/contacts/a", "h-a"); state != StateReplay || string(got.ResponseBody) != `{"id":"a"}` {
		t.Fatalf("retry of a completed request = %q %q, want its replay", state, got.ResponseBody)
	}
	if _, state := begin("done", "/contacts/b", "h-b"); state != StateConflict {
		t.Fatalf("same key on another resource = %q, want conflict", state)
	}

	rec, _ = begin("refused", "/contacts/a", "h-a")
	if xerr := svc.Release(ctx, rec.ID); xerr != nil {
		t.Fatal(xerr)
	}
	if _, state := begin("refused", "/contacts/a", "h-a"); state != StateStarted {
		t.Fatalf("retry of a refused request = %q, want it to run", state)
	}

	rec, _ = begin("failed", "/contacts/a", "h-a")
	if xerr := svc.MarkUncertain(ctx, rec.ID, 500); xerr != nil {
		t.Fatal(xerr)
	}
	if xerr := svc.Complete(ctx, rec.ID, 200, nil, ""); xerr != nil {
		t.Fatal(xerr)
	}
	if _, state := begin("failed", "/contacts/a", "h-a"); state != StateUncertain {
		t.Fatalf("retry after a server error = %q, want uncertain (and never completed)", state)
	}

	rec, _ = begin("abandoned", "/contacts/a", "h-a")
	if _, err := pool.Exec(ctx, `UPDATE api_idempotency_keys SET updated_at = now() - interval '1 hour' WHERE id = $1`, rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, state := begin("abandoned", "/contacts/a", "h-a"); state != StateUncertain {
		t.Fatalf("retry of a request nobody finished = %q, want uncertain", state)
	}

	rec, _ = begin("expired", "/contacts/a", "h-a")
	if _, err := pool.Exec(ctx, `UPDATE api_idempotency_keys SET status = 'completed', status_code = 201, expires_at = now() - interval '1 second' WHERE id = $1`, rec.ID); err != nil {
		t.Fatal(err)
	}
	if again, state := begin("expired", "/contacts/b", "h-b"); state != StateStarted || again.ID == rec.ID {
		t.Fatalf("an expired key = %q (same id %v), want a fresh claim", state, again.ID == rec.ID)
	}
}

// A record written by the previous release stored the route pattern and an
// unversioned hash; it must never read as a match for a new request.
func TestLiveIdempotencyPreviousReleaseRecordsNeverMatch(t *testing.T) {
	pool, org := liveTestOrg(t)
	svc := NewService(pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO api_idempotency_keys (organization_id, key, method, path, request_hash, status, status_code, response_body, expires_at)
		VALUES ($1, 'old', 'PATCH', '/v1/contacts/:id', 'legacyhash', 'completed', 200, '{"id":"a"}', now() + interval '1 day'),
		       ($1, 'old-running', 'PATCH', '/v1/contacts/:id', 'legacyhash', 'processing', NULL, NULL, now() + interval '1 day')
	`, org); err != nil {
		t.Fatal(err)
	}
	if _, state, xerr := svc.Begin(ctx, OrgScope(org), "old", "PATCH", "/v1/contacts/b", "v2hash"); xerr != nil || state != StateConflict {
		t.Fatalf("previous-release record = %q %v, want conflict", state, xerr)
	}
	if _, state, xerr := svc.Begin(ctx, OrgScope(org), "old-running", "PATCH", "/v1/contacts/b", "v2hash"); xerr != nil || state != StateConflict {
		t.Fatalf("previous-release in-flight record = %q %v, want conflict", state, xerr)
	}
}

// An agency-scoped key replays within its agency and never collides with a workspace or another agency.
func TestLiveAgencyScopedKeys(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	owner, agency, other, org := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Idem', 'Live', $2, 'x')`, []any{owner, "idem-" + owner.String()[:8] + "@test.local"}},
		{`INSERT INTO agencies (id, name, owner_user_id) VALUES ($1, 'A', $3), ($2, 'B', $3)`, []any{agency, other, owner}},
		{`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Idem', $2, $3)`, []any{org, "idem-" + org.String()[:8], owner}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, org)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agencies WHERE id IN ($1, $2)`, agency, other)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, owner)
	})

	s := NewService(pool)
	rec, state, xerr := s.Begin(ctx, AgencyScope(agency), "k1", "POST", "/v1/agency/:agencyId/clients", "h")
	if xerr != nil || state != StateStarted {
		t.Fatalf("first agency begin = %v, %v", state, xerr)
	}
	if xerr := s.Complete(ctx, rec.ID, 201, []byte(`{}`), "application/json"); xerr != nil {
		t.Fatal(xerr)
	}
	if _, state, _ := s.Begin(ctx, AgencyScope(agency), "k1", "POST", "/v1/agency/:agencyId/clients", "h"); state != StateReplay {
		t.Errorf("retry in the same agency = %v, want replay", state)
	}
	if _, state, _ := s.Begin(ctx, AgencyScope(agency), "k1", "POST", "/v1/agency/:agencyId/clients", "other"); state != StateConflict {
		t.Errorf("reuse with another body = %v, want conflict", state)
	}
	for name, scope := range map[string]Scope{"another agency": AgencyScope(other), "a workspace": OrgScope(org)} {
		if _, state, xerr := s.Begin(ctx, scope, "k1", "POST", "/v1/agency/:agencyId/clients", "h"); xerr != nil || state != StateStarted {
			t.Errorf("%s shares the key: %v, %v", name, state, xerr)
		}
	}
	if _, _, xerr := s.Begin(ctx, Scope{}, "k2", "POST", "/x", "h"); xerr == nil {
		t.Error("a request with no scope was accepted")
	}
}
