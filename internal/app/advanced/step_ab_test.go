package advanced

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func arm(sent, replied int) models.ABStepArmStats {
	return fillArm(models.ABStepArmStats{}, models.ABStepArmCounts{Sent: sent, Replied: replied})
}

func TestTwoProportionPMatchesTheNormalApproximation(t *testing.T) {
	// 30/200 against 12/200: pooled 0.105, z ≈ 2.94, two-sided p ≈ 0.0033.
	if p := twoProportionP(30, 200, 12, 200); math.Abs(p-0.0033) > 0.0003 {
		t.Fatalf("p = %v", p)
	}
	if p := twoProportionP(0, 100, 0, 100); p != 1 {
		t.Fatalf("no successes anywhere must not be significant, p = %v", p)
	}
}

func TestStepWinnerNeedsSampleAndSignificance(t *testing.T) {
	cases := []struct {
		name       string
		arms       []models.ABStepArmStats
		winner     int
		confidence string
	}{
		{"no sends", []models.ABStepArmStats{arm(0, 0), arm(0, 0)}, -1, "none"},
		{"one arm sent", []models.ABStepArmStats{arm(100, 5), arm(0, 0)}, -1, "low"},
		{"below the minimum sample", []models.ABStepArmStats{arm(29, 10), arm(100, 1)}, -1, "low"},
		{"lead not significant", []models.ABStepArmStats{arm(100, 6), arm(100, 4)}, -1, "low"},
		{"tie", []models.ABStepArmStats{arm(100, 5), arm(100, 5)}, -1, "low"},
		{"control wins clearly", []models.ABStepArmStats{arm(400, 60), arm(400, 20)}, 0, "high"},
		{"variant wins", []models.ABStepArmStats{arm(200, 12), arm(200, 30)}, 1, "high"},
		{"medium", []models.ABStepArmStats{arm(200, 10), arm(200, 22)}, 1, "medium"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, confidence, _ := stepWinner(tc.arms, "reply_rate", 30)
			if idx != tc.winner || confidence != tc.confidence {
				t.Fatalf("got winner %d %q, want %d %q", idx, confidence, tc.winner, tc.confidence)
			}
		})
	}
}

func TestStepWinnerCorrectsForEveryComparison(t *testing.T) {
	// 22/200 against 10/200 alone is p ≈ 0.03; against two arms it is doubled past 0.05.
	two := []models.ABStepArmStats{arm(200, 10), arm(200, 22)}
	three := []models.ABStepArmStats{arm(200, 10), arm(200, 22), arm(200, 10)}
	if idx, _, _ := stepWinner(two, "reply_rate", 30); idx != 1 {
		t.Fatalf("two arms: winner %d", idx)
	}
	if idx, confidence, _ := stepWinner(three, "reply_rate", 30); idx != -1 || confidence != "low" {
		t.Fatalf("three arms must not call a winner, got %d %q", idx, confidence)
	}
}

func TestStepWinnerRatesExcludeBounces(t *testing.T) {
	// 40 bounced of 100 leaves 60 delivered: 12/60 = 20% beats 6/100 = 6%.
	bouncy := fillArm(models.ABStepArmStats{}, models.ABStepArmCounts{Sent: 100, Bounced: 40, Replied: 12})
	if bouncy.Delivered != 60 || bouncy.ReplyRate != 20 || bouncy.BounceRate != 40 {
		t.Fatalf("arm = %+v", bouncy)
	}
	if idx, _, _ := stepWinner([]models.ABStepArmStats{arm(100, 6), bouncy}, "reply_rate", 30); idx != 1 {
		t.Fatalf("winner %d", idx)
	}
}

func TestStepSplitPool(t *testing.T) {
	step := uuid.New()
	variant := func(control, active bool, weight int) models.CampaignABVariant {
		return models.CampaignABVariant{ID: uuid.New(), SequenceID: &step, IsControl: control, IsActive: active, Weight: weight}
	}
	if _, handled := stepSplitPool(nil); handled {
		t.Fatal("a step without rows must leave campaign-level variants in charge")
	}
	if _, handled := stepSplitPool([]models.CampaignABVariant{variant(false, false, 100)}); handled {
		t.Fatal("a step with only paused variants runs no split of its own")
	}
	pool, handled := stepSplitPool([]models.CampaignABVariant{variant(true, true, 70), variant(false, true, 30)})
	if !handled || len(pool) != 2 || pool[0].ID != uuid.Nil || pool[0].Weight != 70 {
		t.Fatalf("pool = %+v", pool)
	}
	// A paused Original row takes the step's own copy out of the draw.
	winner := variant(false, true, 100)
	pool, handled = stepSplitPool([]models.CampaignABVariant{variant(true, false, 100), winner, variant(false, false, 100)})
	if !handled || len(pool) != 1 || pool[0].ID != winner.ID {
		t.Fatalf("pool = %+v", pool)
	}
}

func TestBuildStepAnalysesIncludesTheOriginalAndDropsDeletedArms(t *testing.T) {
	step, other := uuid.New(), uuid.New()
	promoted := time.Now()
	control := models.CampaignABVariant{ID: uuid.New(), SequenceID: &step, Name: "Original", IsControl: true}
	b := models.CampaignABVariant{ID: uuid.New(), SequenceID: &step, Name: "Variant B", IsActive: true, PromotedAt: &promoted}
	deleted := uuid.New()
	counts := []models.ABStepArmCounts{
		{SequenceID: step, Sent: 400, Replied: 20},
		{SequenceID: step, VariantID: &b.ID, Sent: 400, Replied: 60},
		{SequenceID: step, VariantID: &deleted, Sent: 50, Replied: 50},
		{SequenceID: other, Sent: 10},
	}
	out := buildStepAnalyses([]models.CampaignABVariant{control, b}, counts, "reply_rate", 30)
	if len(out) != 1 {
		t.Fatalf("steps = %+v", out)
	}
	a := out[0]
	if len(a.Arms) != 2 || !a.Arms[0].IsControl || a.Arms[0].IsActive || a.Arms[0].Sent != 400 || *a.Arms[1].VariantID != b.ID {
		t.Fatalf("arms = %+v", a.Arms)
	}
	if a.Winner == nil || a.Winner.IsControl || *a.Winner.VariantID != b.ID || a.Confidence != "high" || a.PromotedAt == nil {
		t.Fatalf("analysis = %+v", a)
	}
}
