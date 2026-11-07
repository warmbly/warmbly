package nodescope

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// The data-key check on a cache hit is a map lookup under a per-node mutex.
func BenchmarkDataKeyCached(b *testing.B) {
	ctx := context.Background()
	worker, org := uuid.New(), uuid.New()
	boxes := make([]repository.NodeMailbox, 0, 500)
	keys := map[uuid.UUID]string{}
	for i := range 500 {
		o := org
		if i%5 != 0 {
			o = uuid.New()
		}
		boxes = append(boxes, repository.NodeMailbox{ID: uuid.New(), UserID: uuid.New(), OrganizationID: &o})
		keys[o] = "sealed-" + o.String()
	}
	repo := &fakeAssignments{roles: map[uuid.UUID]models.NodeRole{worker: models.NodeRoleWorker}, mailboxes: map[uuid.UUID][]repository.NodeMailbox{worker: boxes}, keys: keys}
	s := New(repo, nil)
	sealed := keys[org]
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if ok, _ := s.DataKey(ctx, worker, sealed); !ok {
				b.Fatal("refused")
			}
		}
	})
}
