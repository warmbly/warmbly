package fleetmetrics

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestLiveReadTakesMemoryFromHeartbeats(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	handle, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Pool.Close()
	nodes := repository.NewFleetNodeRepository(handle)
	hot, cool := uuid.New(), uuid.New()
	defer handle.Pool.Exec(ctx, `DELETE FROM fleet_nodes WHERE id = ANY($1)`, []uuid.UUID{hot, cool})
	for id, used := range map[uuid.UUID]int{hot: 3600, cool: 400} {
		limit := 4000
		if err := nodes.UpsertOnHeartbeat(ctx, models.NodeHeartbeat{NodeID: id, Role: models.NodeRoleWorker,
			Usage: models.NodeUsage{MemoryUsedMB: &used, MemoryLimitMB: &limit, MemoryScope: "host"}}); err != nil {
			t.Fatal(err)
		}
	}
	u, err := Read(ctx, handle)
	if err != nil {
		t.Fatal(err)
	}
	if u.MemoryMaxPercent < 90 {
		t.Fatalf("the 90%% worker did not set the hottest reading: %+v", u)
	}
	if u.MemoryPercent <= 0 || u.MemoryPercent >= 100 {
		t.Fatalf("fleet memory %v is not a share of the reported machines", u.MemoryPercent)
	}
}
