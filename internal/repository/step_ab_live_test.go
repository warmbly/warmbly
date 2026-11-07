package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// Step A/B results count contacts, and credit an open, click or reply to the
// step the contact had most recently been sent when it happened.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveStepAB -v
func TestLiveStepABCountsAttributeToTheLatestStep(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 338)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	repo := NewAdvancedOutreachRepository(pool)

	intro, follow := uuid.New(), uuid.New()
	for i, step := range []uuid.UUID{intro, follow} {
		if _, err := pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position)
		      VALUES ($1, $2, $3, 'Step', 'Hi', 'Hello', '', 0, $4)`, step, f.campaign, f.org, i+1); err != nil {
			t.Fatal(err)
		}
	}
	b := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO campaign_ab_variants (id, campaign_id, sequence_id, name) VALUES ($1, $2, $3, 'Variant B')`, b, f.campaign, intro); err != nil {
		t.Fatal(err)
	}
	row := func(contact, step uuid.UUID, arm, variant any, sentHoursAgo any, extra string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, ab_arm, ab_variant_id, dispatched_at, sent_at)
		      VALUES ($1, $2, $3, $4, $5, NOW() - make_interval(hours => $6::int), NOW() - make_interval(hours => $6::int))`,
			f.campaign, contact, step, arm, variant, sentHoursAgo); err != nil {
			t.Fatal(err)
		}
		if extra != "" {
			if _, err := pool.Exec(ctx, `UPDATE campaign_contact_progress SET `+extra+` WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`,
				f.campaign, contact, step); err != nil {
				t.Fatal(err)
			}
		}
	}
	lead := func() uuid.UUID { return addLead(t, f, "ab-"+uuid.New().String()[:8]+"@test.local", "valid", true) }

	// Original: opened the intro, then replied on its thread after the follow-up went
	// out, so the reply is the follow-up's.
	c1 := lead()
	row(c1, intro, "control", nil, 10, `opened_at = NOW() - INTERVAL '9 hours', replied_at = NOW() - INTERVAL '4 hours'`)
	row(c1, follow, nil, nil, 5, "")
	// Variant B: replied before any follow-up; a machine open does not count.
	c2 := lead()
	row(c2, intro, "variant", b, 10, `replied_at = NOW() - INTERVAL '9 hours', opened_at = NOW() - INTERVAL '9 hours', opened_machine = true`)
	// Variant B: bounced, counted as sent and bounced, nothing else.
	c3 := lead()
	row(c3, intro, "variant", b, 10, `bounced_at = NOW() - INTERVAL '9 hours', replied_at = NOW() - INTERVAL '8 hours'`)
	// Sent before the split existed: no arm, never counted.
	c4 := lead()
	row(c4, intro, nil, nil, 30, `replied_at = NOW() - INTERVAL '29 hours'`)
	// Reserved, not sent.
	c5 := lead()
	if _, err := pool.Exec(ctx, `INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, ab_arm, dispatched_at) VALUES ($1, $2, $3, 'control', NOW())`,
		f.campaign, c5, intro); err != nil {
		t.Fatal(err)
	}

	counts, err := repo.GetStepArmCounts(ctx, f.campaign)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]models.ABStepArmCounts{}
	for _, c := range counts {
		if c.SequenceID != intro {
			t.Fatalf("an unsplit step was counted: %+v", c)
		}
		key := "control"
		if c.VariantID != nil {
			key = c.VariantID.String()
		}
		got[key] = c
	}
	if c := got["control"]; c.Sent != 1 || c.Opened != 1 || c.Replied != 0 || c.Bounced != 0 {
		t.Fatalf("control = %+v", c)
	}
	if c := got[b.String()]; c.Sent != 2 || c.Bounced != 1 || c.Replied != 1 || c.Opened != 0 {
		t.Fatalf("variant B = %+v", c)
	}

	// The reservation records the arm it dispatches, so a send carries it from the start.
	c6 := lead()
	if ok, err := NewCampaignProgressRepository(pool).ReserveSend(ctx, f.campaign, c6, intro, uuid.New(), uuid.Nil, true, &models.StepABArm{VariantID: &b}); err != nil || !ok {
		t.Fatalf("reserve: %v %v", ok, err)
	}
	var arm string
	var variant uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT ab_arm, ab_variant_id FROM campaign_contact_progress WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, c6).Scan(&arm, &variant); err != nil || arm != "variant" || variant != b {
		t.Fatalf("reserved arm = %q %v (%v)", arm, variant, err)
	}
}

func TestLiveStepABPromotionHappensOnce(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 338)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	repo := NewAdvancedOutreachRepository(pool)

	step := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position)
	      VALUES ($1, $2, $3, 'Step', 'Hi', 'Hello', '', 0, 1)`, step, f.campaign, f.org); err != nil {
		t.Fatal(err)
	}
	b, c := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO campaign_ab_variants (id, campaign_id, sequence_id, name) VALUES ($1, $3, $4, 'Variant B'), ($2, $3, $4, 'Variant C')`,
		b, c, f.campaign, step); err != nil {
		t.Fatal(err)
	}

	ok, err := repo.PromoteStepWinner(ctx, f.campaign, step, &b)
	if err != nil || !ok {
		t.Fatalf("promote: %v %v", ok, err)
	}
	variants, err := repo.ListABVariants(ctx, f.campaign)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range variants {
		switch {
		case v.IsControl:
			if v.IsActive || v.PromotedAt != nil {
				t.Fatalf("the Original must leave the split: %+v", v)
			}
		case v.ID == b:
			if !v.IsActive || v.PromotedAt == nil {
				t.Fatalf("winner = %+v", v)
			}
		case v.IsActive:
			t.Fatalf("a losing variant still sends: %+v", v)
		}
	}
	if len(variants) != 3 {
		t.Fatalf("variants = %d, want the Original's row added", len(variants))
	}
	if ok, err := repo.PromoteStepWinner(ctx, f.campaign, step, nil); err != nil || ok {
		t.Fatalf("a promoted step was promoted again: %v %v", ok, err)
	}
}
