package repository

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// Saved Apollo and Salesforce imports keep their segment targets, scoped to the
// workspace, and a segment deleted since never blocks another edit.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveImportSourceSegments -v

type sourceSegmentFixture struct {
	org, conn, mine, gone uuid.UUID
}

func newSourceSegmentFixture(t *testing.T, pool *pgxpool.Pool, provider string) sourceSegmentFixture {
	t.Helper()
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	s := sourceSegmentFixture{org: f.org, conn: uuid.New(), mine: uuid.New(), gone: uuid.New()}
	if _, err := pool.Exec(ctx, `INSERT INTO integration_connections (id, organization_id, provider, label, status, auth_method, config_encrypted, health) VALUES ($1, $2, $3, 'Live', 'connected', 'api_key', 'x', 'healthy')`, s.conn, s.org, provider); err != nil {
		t.Fatalf("fixture connection: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO segments (id, organization_id, name) VALUES ($1, $3, 'Audience'), ($2, $3, 'Deleted later')`, s.mine, s.gone, s.org); err != nil {
		t.Fatalf("fixture segments: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM integration_connections WHERE id = $1`, s.conn)
		_, _ = pool.Exec(c, `DELETE FROM segments WHERE id = ANY($1)`, []uuid.UUID{s.mine, s.gone})
	})
	return s
}

// checkSourceSegments drives one source type through create, a stale segment and a foreign one.
func checkSourceSegments(t *testing.T, pool *pgxpool.Pool, s sourceSegmentFixture, refsErr error,
	create func(segs []uuid.UUID) (uuid.UUID, error), update func(id uuid.UUID, segs []uuid.UUID) error, get func(id uuid.UUID) ([]uuid.UUID, error)) {
	t.Helper()
	if _, err := create([]uuid.UUID{uuid.New()}); !errors.Is(err, refsErr) {
		t.Fatalf("a segment outside the workspace must be refused, got %v", err)
	}
	id, err := create([]uuid.UUID{s.mine, s.gone})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `DELETE FROM segments WHERE id = $1`, s.gone); err != nil {
		t.Fatalf("delete segment: %v", err)
	}
	if err := update(id, []uuid.UUID{s.mine, s.gone}); err != nil {
		t.Fatalf("update with a stale segment: %v", err)
	}
	if err := update(id, []uuid.UUID{s.mine, uuid.New()}); !errors.Is(err, refsErr) {
		t.Fatalf("a new foreign segment must be refused, got %v", err)
	}
	got, err := get(id)
	if err != nil || !slices.Equal(got, []uuid.UUID{s.mine, s.gone}) {
		t.Fatalf("get: %+v %v", got, err)
	}
}

func TestLiveImportSourceSegmentsApollo(t *testing.T) {
	_, pool := liveContactDB(t)
	s := newSourceSegmentFixture(t, pool, "apollo")
	repo := NewApolloRepository(pool)
	ctx := context.Background()
	checkSourceSegments(t, pool, s, ErrApolloSourceRefs,
		func(segs []uuid.UUID) (uuid.UUID, error) {
			src := &models.ApolloImportSource{OrganizationID: s.org, ConnectionID: s.conn, Name: "List", ListID: "l1", SegmentIDs: segs, Enabled: true}
			err := repo.CreateSource(ctx, src)
			return src.ID, err
		},
		func(id uuid.UUID, segs []uuid.UUID) error {
			src, err := repo.GetSource(ctx, s.org, id)
			if err != nil {
				return err
			}
			src.SegmentIDs = segs
			return repo.UpdateSource(ctx, src)
		},
		func(id uuid.UUID) ([]uuid.UUID, error) {
			src, err := repo.GetSource(ctx, s.org, id)
			if err != nil {
				return nil, err
			}
			return src.SegmentIDs, nil
		})
}

func TestLiveImportSourceSegmentsSalesforce(t *testing.T) {
	_, pool := liveContactDB(t)
	s := newSourceSegmentFixture(t, pool, "salesforce")
	repo := NewSalesforceRepository(pool)
	ctx := context.Background()
	checkSourceSegments(t, pool, s, ErrSalesforceSourceRefs,
		func(segs []uuid.UUID) (uuid.UUID, error) {
			src := &models.SalesforceImportSource{OrganizationID: s.org, ConnectionID: s.conn, Name: "View", SourceKind: "list_view", SObject: "Lead", SourceID: "00B000000000001", SegmentIDs: segs, Enabled: true}
			err := repo.CreateSource(ctx, src)
			return src.ID, err
		},
		func(id uuid.UUID, segs []uuid.UUID) error {
			src, err := repo.GetSource(ctx, s.org, id)
			if err != nil {
				return err
			}
			src.SegmentIDs = segs
			return repo.UpdateSource(ctx, src)
		},
		func(id uuid.UUID) ([]uuid.UUID, error) {
			src, err := repo.GetSource(ctx, s.org, id)
			if err != nil {
				return nil, err
			}
			return src.SegmentIDs, nil
		})
}
