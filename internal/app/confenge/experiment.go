package confenge

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"
)

// Experiment dimensions — assign one at a time when possible.
const (
	ExpDimOffer   = "offer"
	ExpDimCTA     = "cta"
	ExpDimSubject = "subject"
	ExpDimOpening = "opening"
	ExpDimReframe = "reframe"
)

// Default minimum sample before a variant may be considered non-inconclusive.
const ExperimentMinSamplePerArm = 50

// ExperimentAssignment is account-level A/B metadata (not a priority score).
type ExperimentAssignment struct {
	HypothesisID    string `json:"hypothesis_id"`
	VariantID       string `json:"variant_id"` // champion | challenger
	Dimension       string `json:"dimension"`
	DoctrineVersion string `json:"doctrine_version"`
	StrategyVersion string `json:"strategy_version"`
	ServiceCode     string `json:"service_code,omitempty"`
	OfferCode       string `json:"offer_code,omitempty"`
}

// ExperimentArmStats is observed counts for one arm (no open-tracking primacy).
type ExperimentArmStats struct {
	VariantID             string  `json:"variant_id"`
	Sent                  int     `json:"sent"`
	Delivered             int     `json:"delivered"`
	HumanReply            int     `json:"human_reply"`
	PositiveReply         int     `json:"positive_reply"`
	QualifiedConversation int     `json:"qualified_conversation"`
	Meeting               int     `json:"meeting"`
	Proposal              int     `json:"proposal"`
	Won                   int     `json:"won"`
	AttributedRevenue     float64 `json:"attributed_revenue,omitempty"`
}

// ExperimentEvaluation is a statistically honest comparison result.
type ExperimentEvaluation struct {
	Status            string  `json:"status"` // INCONCLUSIVE | CHAMPION_LEADS | CHALLENGER_LEADS
	PrimaryMetric     string  `json:"primary_metric"`
	ChampionRate      float64 `json:"champion_rate"`
	ChallengerRate    float64 `json:"challenger_rate"`
	SampleChampion    int     `json:"sample_champion"`
	SampleChallenger  int     `json:"sample_challenger"`
	MinSampleRequired int     `json:"min_sample_required"`
	Reason            string  `json:"reason"`
}

// AssignExperiment assigns champion/challenger at account grain (stable hash).
// Single dimension: offer wording family (challenger uses alternate CTA framing metadata only).
func AssignExperiment(accountID, serviceCode, offerCode string) *ExperimentAssignment {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil
	}
	h := sha256.Sum256([]byte("confenge-exp-v1|" + accountID))
	n := binary.BigEndian.Uint64(h[:8])
	variant := "champion"
	if n%2 == 1 {
		variant = "challenger"
	}
	// Rotate dimension by account bucket so we don't mix all dimensions at once.
	dims := []string{ExpDimCTA, ExpDimSubject, ExpDimOpening, ExpDimOffer}
	dim := dims[int(n%uint64(len(dims)))]
	return &ExperimentAssignment{
		HypothesisID:    "h1-permission-cta-vs-interest",
		VariantID:       variant,
		Dimension:       dim,
		DoctrineVersion: OutreachDoctrineVersion,
		StrategyVersion: "strategy-v1",
		ServiceCode:     serviceCode,
		OfferCode:       offerCode,
	}
}

// PositiveReplyRate = positive_reply / delivered (0 if no delivered).
func PositiveReplyRate(arm ExperimentArmStats) float64 {
	den := arm.Delivered
	if den <= 0 {
		den = arm.Sent
	}
	if den <= 0 {
		return 0
	}
	return float64(arm.PositiveReply) / float64(den)
}

// HumanReplyRate includes all human replies (positive or not). Distinct from positive.
func HumanReplyRate(arm ExperimentArmStats) float64 {
	den := arm.Delivered
	if den <= 0 {
		den = arm.Sent
	}
	if den <= 0 {
		return 0
	}
	return float64(arm.HumanReply) / float64(den)
}

// EvaluateExperiment enforces small-n protection; never declares a global winner early.
func EvaluateExperiment(champion, challenger ExperimentArmStats, minSample int) ExperimentEvaluation {
	if minSample <= 0 {
		minSample = ExperimentMinSamplePerArm
	}
	ev := ExperimentEvaluation{
		Status:            "INCONCLUSIVE",
		PrimaryMetric:     "positive_reply_rate",
		MinSampleRequired: minSample,
		SampleChampion:    sampleSize(champion),
		SampleChallenger:  sampleSize(challenger),
	}
	ev.ChampionRate = PositiveReplyRate(champion)
	ev.ChallengerRate = PositiveReplyRate(challenger)

	if ev.SampleChampion < minSample || ev.SampleChallenger < minSample {
		ev.Reason = "sample below minimum; keep both arms"
		return ev
	}

	// Wilson-ish rough check: require absolute difference and enough events.
	diff := ev.ChallengerRate - ev.ChampionRate
	// Approximate standard error for two proportions
	se := math.Sqrt(propVar(ev.ChampionRate, ev.SampleChampion) + propVar(ev.ChallengerRate, ev.SampleChallenger))
	if se == 0 || math.Abs(diff) < 1.96*se {
		ev.Reason = "difference not statistically distinguishable"
		return ev
	}
	if diff > 0 {
		ev.Status = "CHALLENGER_LEADS"
		ev.Reason = "challenger higher positive_reply_rate with adequate sample"
	} else {
		ev.Status = "CHAMPION_LEADS"
		ev.Reason = "champion higher positive_reply_rate with adequate sample"
	}
	return ev
}

func sampleSize(a ExperimentArmStats) int {
	if a.Delivered > 0 {
		return a.Delivered
	}
	return a.Sent
}

func propVar(p float64, n int) float64 {
	if n <= 0 {
		return 1
	}
	return p * (1 - p) / float64(n)
}

// FunnelSnapshot is the operator experiment dashboard shape.
type FunnelSnapshot struct {
	Sent                  int     `json:"sent"`
	Delivered             int     `json:"delivered"`
	HumanReply            int     `json:"human_reply"`
	PositiveReply         int     `json:"positive_reply"`
	QualifiedConversation int     `json:"qualified"`
	Meeting               int     `json:"meeting"`
	Proposal              int     `json:"proposal"`
	Won                   int     `json:"won"`
	Revenue               float64 `json:"revenue"`
	HumanReplyPct         float64 `json:"human_reply_pct"`
	PositiveReplyPct      float64 `json:"positive_reply_pct"`
	QualifiedPct          float64 `json:"qualified_pct"`
}

// BuildFunnelSnapshot computes rates from counts (opens intentionally omitted as primary).
func BuildFunnelSnapshot(s ExperimentArmStats) FunnelSnapshot {
	den := s.Delivered
	if den <= 0 {
		den = s.Sent
	}
	pct := func(n int) float64 {
		if den <= 0 {
			return 0
		}
		return 100 * float64(n) / float64(den)
	}
	return FunnelSnapshot{
		Sent: s.Sent, Delivered: s.Delivered,
		HumanReply: s.HumanReply, PositiveReply: s.PositiveReply,
		QualifiedConversation: s.QualifiedConversation,
		Meeting:               s.Meeting, Proposal: s.Proposal, Won: s.Won,
		Revenue:          s.AttributedRevenue,
		HumanReplyPct:    pct(s.HumanReply),
		PositiveReplyPct: pct(s.PositiveReply),
		QualifiedPct:     pct(s.QualifiedConversation),
	}
}
