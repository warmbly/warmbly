package token

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// A cache that cannot answer must not be able to sign everyone out.
//
// It could. The session read returned InternalError on any Redis error that was
// not a plain miss, and the write that followed a database read returned its
// error too. So when Redis began refusing writes on a quota, every
// authenticated request failed with a 500 and nobody could log in, while the
// sessions themselves sat intact in Postgres the whole time.
//
// The cache here points at a closed port, which is the real failure rather than
// a mocked one.
type sessionRepo struct {
	repository.TokenRepository
	session *models.Session
	calls   int
}

func (r *sessionRepo) GetSession(_ context.Context, _ uuid.UUID) (*models.Session, *errx.Error) {
	r.calls++
	return r.session, nil
}

func TestASessionSurvivesACacheThatIsDown(t *testing.T) {
	// Built directly rather than through cache.New, which pings on construction
	// and would fail before the test could start.
	down := &cache.Cache{Client: redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 200 * time.Millisecond,
	})}
	id := uuid.New()
	repo := &sessionRepo{session: &models.Session{ID: id, UserID: uuid.New()}}
	s := &tokenService{cache: down, tokenRepository: repo}

	got, xerr := s.GetSession(context.Background(), id)
	if xerr != nil {
		t.Fatalf("a cache outage failed the request: %v", xerr)
	}
	if got == nil || got.ID != id {
		t.Fatalf("session = %+v, want id %s", got, id)
	}
	if repo.calls != 1 {
		t.Fatalf("read the database %d times, want 1", repo.calls)
	}
}
