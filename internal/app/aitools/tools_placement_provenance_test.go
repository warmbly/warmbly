package aitools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/placement"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// stubPlacement answers the two send-class placement tools; everything else on
// the embedded interface panics if accidentally called.
type stubPlacement struct {
	PlacementTests
	tests []placement.TestView
	batch *placement.BatchView
}

func (s stubPlacement) CreateTests(ctx context.Context, in placement.CreateInput) ([]placement.TestView, *errx.Error) {
	return s.tests, nil
}

func (s stubPlacement) CreateBatch(ctx context.Context, in placement.BatchInput) (*placement.BatchView, *errx.Error) {
	return s.batch, nil
}

// TestPlacementToolsProvenance pins that the placement send tools' audit rows
// (they audit through their own service handle, outside Deps.logAudit) carry
// the AI provenance of the invocation that started them, and that a
// provenance-less invocation keeps the old metadata shape.
func TestPlacementToolsProvenance(t *testing.T) {
	org, user := uuid.New(), uuid.New()
	sender := uuid.New()

	p := placementTools{
		svc: stubPlacement{
			tests: []placement.TestView{{PlacementTest: models.PlacementTest{ID: uuid.New(), SenderEmail: "seed-a@example.com", Panel: "instance"}}},
			batch: &placement.BatchView{PlacementBatch: models.PlacementBatch{ID: uuid.New(), SenderCount: 2, Panel: "instance"}},
		},
	}

	t.Run("run_placement_test carries invocation provenance", func(t *testing.T) {
		rec := &recordingAudit{}
		p.audit = rec
		inv := Invocation{OrgID: org, UserID: user, AISurface: models.AISurfaceAgent, AIDecision: models.AIDecisionHumanApproved}
		if _, err := p.run(context.Background(), inv, json.RawMessage(`{"sender_account_id":"`+sender.String()+`"}`)); err != nil {
			t.Fatal(err)
		}
		if len(rec.rows) != 1 {
			t.Fatalf("expected one audit row, got %d", len(rec.rows))
		}
		row := rec.rows[0]
		if row.entity != models.AuditEntityPlacementTest || row.action != models.AuditActionCreate {
			t.Fatalf("unexpected audit action/entity: %s %s", row.action, row.entity)
		}
		if row.metadata[models.MetaKeyAISurface] != "agent" || row.metadata[models.MetaKeyAIDecision] != "human_approved" {
			t.Fatalf("placement test audit row missing provenance: %v", row.metadata)
		}
		if row.metadata["sender"] != "seed-a@example.com" {
			t.Fatalf("call-site metadata lost: %v", row.metadata)
		}
	})

	t.Run("run_placement_batch carries invocation provenance", func(t *testing.T) {
		rec := &recordingAudit{}
		p.audit = rec
		inv := Invocation{OrgID: org, UserID: user, AISurface: models.AISurfaceMCP, AIDecision: models.AIDecisionPermissionBit}
		if _, err := p.runBatch(context.Background(), inv, json.RawMessage(`{"scope":"workspace"}`)); err != nil {
			t.Fatal(err)
		}
		if len(rec.rows) != 1 {
			t.Fatalf("expected one audit row, got %d", len(rec.rows))
		}
		row := rec.rows[0]
		if row.metadata[models.MetaKeyAISurface] != "mcp" || row.metadata[models.MetaKeyAIDecision] != "permission_bit" {
			t.Fatalf("placement batch audit row missing provenance: %v", row.metadata)
		}
		if row.metadata["senders"] != "2" {
			t.Fatalf("call-site metadata lost: %v", row.metadata)
		}
	})

	t.Run("invocation without provenance keeps the plain metadata", func(t *testing.T) {
		rec := &recordingAudit{}
		p.audit = rec
		inv := Invocation{OrgID: org, UserID: user}
		if _, err := p.run(context.Background(), inv, json.RawMessage(`{"sender_account_id":"`+sender.String()+`"}`)); err != nil {
			t.Fatal(err)
		}
		row := rec.rows[0]
		if _, ok := row.metadata[models.MetaKeyAISurface]; ok {
			t.Fatalf("provenance invented without an AI invocation: %v", row.metadata)
		}
		if row.metadata["sender"] != "seed-a@example.com" {
			t.Fatalf("plain metadata lost: %v", row.metadata)
		}
	})
}
