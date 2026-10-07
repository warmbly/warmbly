package repository

import (
	"testing"

	"github.com/google/uuid"
)

func TestLiveLegacyBankCannotStarveVersionedOpeningSupply(t *testing.T) {
	f, _ := lineageFixture(t)
	ctx := t.Context()
	segment := "fixture-" + uuid.NewString()
	legacy, fresh := uuid.New(), uuid.New()
	t.Cleanup(func() { _, _ = f.pool.Exec(ctx, `DELETE FROM warmup_conversations WHERE segment=$1`, segment) })
	_, err := f.pool.Exec(ctx, `INSERT INTO warmup_conversations(id,pool_type,segment,source,subject,description,messages,status) VALUES($1,'premium',$2,'ai','Legacy','Historical','["Reply","Closure"]','active')`, legacy, segment)
	if err != nil {
		t.Fatal(err)
	}
	r := NewWarmupContentRepository(f.pool)
	if n, err := r.CountActiveConversations(ctx, "premium", segment); err != nil || n != 0 {
		t.Fatalf("legacy bank falsely ready: %d, %v", n, err)
	}
	// A legacy bank remains visible but cannot consume a new-opening draw.
	_, err = f.pool.Exec(ctx, `INSERT INTO warmup_conversations(id,pool_type,segment,source,subject,description,messages,status,scenario_version,rendering_version,reply_eligible,lint_passed,semantic_review) VALUES($1,'premium',$2,'ai','[Warmbly diagnostic] Fixture','Disclosed fixture','["Reply","Closure"]','active','diagnostic-v1','canonical-v1',true,true,'passed')`, fresh, segment)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := r.CountActiveConversations(ctx, "premium", segment); err != nil || n != 1 {
		t.Fatalf("usable supply missing: %d, %v", n, err)
	}
	for range 8 {
		c, err := r.PickConversation(ctx, segment)
		if err != nil || c == nil || c.ID != fresh {
			t.Fatalf("unusable legacy source selected: %+v, %v", c, err)
		}
	}
	c, err := r.GetConversation(ctx, legacy)
	if err != nil || c == nil || c.UsageCount != 0 || c.Status != "active" || c.ScenarioVersion != nil {
		t.Fatalf("legacy state changed: %+v, %v", c, err)
	}
}
