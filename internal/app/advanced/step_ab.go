package advanced

import (
	"context"
	"math"
	"strconv"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// abAutoPromoteBatch bounds how many campaigns one promotion pass reads.
const abAutoPromoteBatch = 200

// abSignificance is the family-wise two-sided significance level a winner must reach;
// abHighSignificance upgrades its confidence to high.
const (
	abSignificance     = 0.05
	abHighSignificance = 0.01
)

// stepSplitPool is the draw for one step: the Original (zero id, unless its row is
// paused) plus every active variant. handled is false when the step runs no split of
// its own and campaign-level variants still apply.
func stepSplitPool(stepRows []models.CampaignABVariant) (pool []models.CampaignABVariant, handled bool) {
	controlWeight, controlActive := abControlWeight, true
	var arms []models.CampaignABVariant
	for _, v := range stepRows {
		switch {
		case v.IsControl:
			controlWeight, controlActive = v.Weight, v.IsActive
			handled = true
		case v.IsActive:
			arms = append(arms, v)
			handled = true
		}
	}
	if controlActive {
		pool = append(pool, models.CampaignABVariant{Weight: controlWeight})
	}
	return append(pool, arms...), handled
}

// twoProportionP is the two-sided p-value of a pooled two-proportion z-test.
func twoProportionP(x1, n1, x2, n2 int) float64 {
	if n1 <= 0 || n2 <= 0 {
		return 1
	}
	pooled := float64(x1+x2) / float64(n1+n2)
	se := math.Sqrt(pooled * (1 - pooled) * (1/float64(n1) + 1/float64(n2)))
	if se == 0 {
		return 1
	}
	z := (float64(x1)/float64(n1) - float64(x2)/float64(n2)) / se
	return math.Erfc(math.Abs(z) / math.Sqrt2)
}

// ruleSuccesses is the arm's count on the winning rule's metric.
func ruleSuccesses(a models.ABStepArmStats, rule string) int {
	switch rule {
	case "open_rate":
		return a.Opened
	case "click_rate":
		return a.Clicked
	default:
		return a.Replied
	}
}

// stepWinner picks the arm with the best rate on rule once every arm that sent has
// minSample delivered contacts and the leader beats each other arm in a two-proportion
// z-test, Bonferroni-adjusted for the comparisons made. It returns -1 without a winner.
func stepWinner(arms []models.ABStepArmStats, rule string, minSample int) (int, string, *float64) {
	if minSample <= 0 {
		minSample = 30
	}
	var compared []int
	for i, a := range arms {
		if a.Sent > 0 {
			compared = append(compared, i)
		}
	}
	switch {
	case len(compared) == 0:
		return -1, "none", nil
	case len(compared) < 2:
		return -1, "low", nil
	}
	rate := func(i int) float64 {
		if arms[i].Delivered == 0 {
			return 0
		}
		return float64(ruleSuccesses(arms[i], rule)) / float64(arms[i].Delivered)
	}
	best := compared[0]
	for _, i := range compared {
		if arms[i].Delivered < minSample {
			return -1, "low", nil
		}
		if rate(i) > rate(best) {
			best = i
		}
	}
	worst := 0.0
	for _, i := range compared {
		if i == best {
			continue
		}
		if rate(i) == rate(best) {
			worst = 1
			break
		}
		p := twoProportionP(ruleSuccesses(arms[best], rule), arms[best].Delivered, ruleSuccesses(arms[i], rule), arms[i].Delivered)
		worst = math.Max(worst, p)
	}
	adjusted := math.Min(1, worst*float64(len(compared)-1))
	switch {
	case adjusted < abHighSignificance:
		return best, "high", &adjusted
	case adjusted < abSignificance:
		return best, "medium", &adjusted
	default:
		return -1, "low", &adjusted
	}
}

func armKey(step uuid.UUID, variant *uuid.UUID) [2]uuid.UUID {
	if variant == nil {
		return [2]uuid.UUID{step, uuid.Nil}
	}
	return [2]uuid.UUID{step, *variant}
}

// buildStepAnalyses lays every step split out as the Original plus its variants, filled
// from the contact counts. A step whose variants were all deleted is not reported.
func buildStepAnalyses(rows []models.CampaignABVariant, counts []models.ABStepArmCounts, rule string, minSample int) []models.ABStepAnalysis {
	byArm := make(map[[2]uuid.UUID]models.ABStepArmCounts, len(counts))
	for _, c := range counts {
		byArm[armKey(c.SequenceID, c.VariantID)] = c
	}
	type step struct {
		control  *models.CampaignABVariant
		variants []models.CampaignABVariant
	}
	steps := map[uuid.UUID]*step{}
	var order []uuid.UUID
	for i := range rows {
		v := rows[i]
		if v.SequenceID == nil {
			continue
		}
		st, ok := steps[*v.SequenceID]
		if !ok {
			st = &step{}
			steps[*v.SequenceID] = st
			order = append(order, *v.SequenceID)
		}
		if v.IsControl {
			st.control = &rows[i]
		} else {
			st.variants = append(st.variants, v)
		}
	}

	out := make([]models.ABStepAnalysis, 0, len(order))
	for _, stepID := range order {
		st := steps[stepID]
		if len(st.variants) == 0 {
			continue
		}
		a := models.ABStepAnalysis{StepID: stepID}
		original := models.ABStepArmStats{Name: "Original", IsControl: true, IsActive: true}
		if st.control != nil {
			original.IsActive = st.control.IsActive
			a.PromotedAt = st.control.PromotedAt
		}
		a.Arms = append(a.Arms, fillArm(original, byArm[armKey(stepID, nil)]))
		for _, v := range st.variants {
			id := v.ID
			arm := models.ABStepArmStats{VariantID: &id, Name: v.Name, IsActive: v.IsActive}
			a.Arms = append(a.Arms, fillArm(arm, byArm[armKey(stepID, &id)]))
			if v.PromotedAt != nil {
				a.PromotedAt = v.PromotedAt
			}
		}
		idx, confidence, p := stepWinner(a.Arms, rule, minSample)
		a.Confidence, a.PValue = confidence, p
		if idx >= 0 {
			w := a.Arms[idx]
			a.Winner = &models.ABStepWinner{VariantID: w.VariantID, IsControl: w.IsControl, Name: w.Name}
		}
		out = append(out, a)
	}
	return out
}

func fillArm(a models.ABStepArmStats, c models.ABStepArmCounts) models.ABStepArmStats {
	a.Sent, a.Bounced = c.Sent, c.Bounced
	a.Opened, a.Clicked, a.Replied = c.Opened, c.Clicked, c.Replied
	a.Delivered = max(c.Sent-c.Bounced, 0)
	if a.Delivered > 0 {
		a.OpenRate = float64(a.Opened) / float64(a.Delivered) * 100
		a.ClickRate = float64(a.Clicked) / float64(a.Delivered) * 100
		a.ReplyRate = float64(a.Replied) / float64(a.Delivered) * 100
	}
	if a.Sent > 0 {
		a.BounceRate = float64(a.Bounced) / float64(a.Sent) * 100
	}
	return a
}

func (s *service) stepAnalyses(ctx context.Context, campaignID uuid.UUID, settings *models.AdvancedOutreachSettings) ([]models.ABStepAnalysis, error) {
	rows, err := s.repo.ListABVariants(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.GetStepArmCounts(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	return buildStepAnalyses(rows, counts, settings.ABTesting.DefaultWinningRule, settings.ABTesting.MinSampleSize), nil
}

// PromoteStepWinners hands each running step split whose winner is significant to that
// winner, for campaigns with auto_promote_winner on. A step is promoted at most once.
func (s *service) PromoteStepWinners(ctx context.Context) (int, *errx.Error) {
	candidates, err := s.repo.ListABAutoPromoteCandidates(ctx, abAutoPromoteBatch)
	if err != nil {
		return 0, toErrx(err)
	}
	promoted := 0
	for _, c := range candidates {
		settings, xerr := s.effectiveSettings(ctx, c.OrganizationID, c.CampaignID)
		if xerr != nil {
			log.Warn().Str("campaign_id", c.CampaignID.String()).Msg("ab auto-promote: could not read the campaign's settings")
			continue
		}
		if !settings.ABTesting.Enabled || !settings.ABTesting.AutoPromoteWinner {
			continue
		}
		analyses, err := s.stepAnalyses(ctx, c.CampaignID, settings)
		if err != nil {
			log.Warn().Err(err).Str("campaign_id", c.CampaignID.String()).Msg("ab auto-promote: could not read the step results")
			continue
		}
		for _, a := range analyses {
			if a.PromotedAt != nil || a.Winner == nil {
				continue
			}
			ok, err := s.repo.PromoteStepWinner(ctx, c.CampaignID, a.StepID, a.Winner.VariantID)
			if err != nil {
				log.Warn().Err(err).Str("campaign_id", c.CampaignID.String()).Str("step_id", a.StepID.String()).Msg("ab auto-promote: promotion failed")
				continue
			}
			if !ok {
				continue
			}
			promoted++
			if s.audit != nil {
				meta := map[string]string{
					"scope":        "ab_winner_promoted",
					"step_id":      a.StepID.String(),
					"winner":       a.Winner.Name,
					"winning_rule": settings.ABTesting.DefaultWinningRule,
					"confidence":   a.Confidence,
				}
				if a.PValue != nil {
					meta["p_value"] = strconv.FormatFloat(*a.PValue, 'g', 4, 64)
				}
				campaignID := c.CampaignID
				s.audit.LogAction(ctx, c.OrganizationID, uuid.Nil, models.AuditActionUpdate, models.AuditEntityCampaign, &campaignID, "", "", nil, meta)
			}
		}
	}
	return promoted, nil
}
