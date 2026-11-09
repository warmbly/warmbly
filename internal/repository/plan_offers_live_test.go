package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLivePlanOffers(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 328)
	ctx := context.Background()
	user, org := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, first_name, last_name, password_hash) VALUES ($1, $2, 'Plan', 'Offer', 'x')`,
		user, "po-"+user.String()[:8]+"@test.local"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Plan Offer', $2, $3)`,
		org, "po-"+org.String()[:8], user); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, org)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user)
	})

	offers := NewPlanOfferRepository(pool)
	if o, err := offers.GetPlanOffer(ctx, org, models.PartnershipPlanID); err != nil || o != nil {
		t.Fatalf("no offer yet: got %+v, %v", o, err)
	}
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	for _, reason := range []string{"partner", "renewed"} {
		if _, err := offers.UpsertPlanOffer(ctx, &models.PlanOffer{OrganizationID: org, PlanID: models.PartnershipPlanID, GrantedBy: &user, Reason: reason, ExpiresAt: &until}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := offers.ListPlanOffers(ctx, org)
	if err != nil || len(list) != 1 || list[0].Reason != "renewed" || !list[0].ExpiresAt.Equal(until) {
		t.Fatalf("one renewed offer expected, got %+v, %v", list, err)
	}
	if _, err := offers.UpsertPlanOffer(ctx, &models.PlanOffer{OrganizationID: org, PlanID: models.PartnershipPlanID, Reason: "  "}); err == nil {
		t.Fatal("a blank reason must be refused by the schema")
	}
	if ok, err := offers.DeletePlanOffer(ctx, org, models.PartnershipPlanID); err != nil || !ok {
		t.Fatalf("withdraw: %v %v", ok, err)
	}
}
