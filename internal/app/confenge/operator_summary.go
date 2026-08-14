package confenge

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

// OperatorImportSummary is the post-import cockpit briefing.
type OperatorImportSummary struct {
	ActionableAccounts int            `json:"actionable_accounts"`
	RouteDistribution  map[string]int `json:"route_distribution"`
	ManualCall         int            `json:"manual_call"`
	EmailSafe          int            `json:"email_safe"`
	UnresolvedBlockers int            `json:"unresolved_blockers"`
	NextHumanActions   []string       `json:"next_human_actions"`
}

// SummarizeOperatorProjection plans every imported lead and counts the six
// operator fields. It never invents identity and never marks email sendable
// unless PlanCommercialAction already would.
func SummarizeOperatorProjection(feed *Feed) OperatorImportSummary {
	out := OperatorImportSummary{RouteDistribution: map[string]int{}}
	if feed == nil {
		return out
	}
	now := parseFeedNow(feed)
	for _, lead := range feed.Leads {
		acc, cands, ev := feedLeadToModels(lead)
		var cand *models.OutreachContactCandidate
		if len(cands) > 0 {
			cand = &cands[0]
		}
		p := PlanCommercialAction(PlanInput{
			Account: acc, Candidate: cand, Candidates: cands, Evidence: ev,
			Now: now, Snapshot: firstNonEmpty(feed.Source.SnapshotHash, "operator-projection"),
		})
		key := firstNonEmpty(p.Action.ActionType, p.Action.ReachabilityClass, p.Action.Lane, "NONE")
		if p.NoAction || !p.Action.Actionable {
			key = firstNonEmpty(p.Action.ReachabilityClass, p.Action.Lane, models.ReachabilityR0None)
			out.UnresolvedBlockers++
		}
		out.RouteDistribution[key]++
		if p.Action.Actionable && !p.NoAction {
			out.ActionableAccounts++
		}
		if p.Action.ActionType == models.ActionRoutedCall || p.Action.ActionType == models.ActionDirectCall {
			out.ManualCall++
		}
		if p.Action.EmailSendable && p.RecipientState == RecipientValidated {
			out.EmailSafe++
		}
		if p.Action.Actionable && strings.TrimSpace(p.Action.RecommendedAction) != "" {
			label := firstNonEmpty(accName(acc), lead.Company.RazaoSocial, lead.SourceLeadID)
			out.NextHumanActions = append(out.NextHumanActions, label+": "+p.Action.RecommendedAction)
		} else if p.NoAction || !p.Action.Actionable {
			reason := firstNonEmpty(firstWarning(p.Action.Warnings), p.Action.RecommendedAction, "sem rota acionavel")
			label := firstNonEmpty(accName(acc), lead.Company.RazaoSocial, lead.SourceLeadID)
			out.NextHumanActions = append(out.NextHumanActions, label+": BLOQUEIO "+reason)
		}
	}
	if len(out.NextHumanActions) > 20 {
		out.NextHumanActions = out.NextHumanActions[:20]
	}
	return out
}

func applyOperatorSummary(counts *models.OutreachImportCounts, sum OperatorImportSummary) {
	if counts == nil {
		return
	}
	counts.Actionable = sum.ActionableAccounts
	counts.ManualCall = sum.ManualCall
	counts.EmailSafe = sum.EmailSafe
	counts.UnresolvedBlockers = sum.UnresolvedBlockers
	counts.RouteDistribution = sum.RouteDistribution
	counts.NextHumanActions = append([]string{}, sum.NextHumanActions...)
}

func FormatOperatorSummary(sum OperatorImportSummary) string {
	var b strings.Builder
	b.WriteString("CONFENGE operator summary\n")
	fmt.Fprintf(&b, "  actionable_accounts=%d\n", sum.ActionableAccounts)
	fmt.Fprintf(&b, "  manual_call=%d\n", sum.ManualCall)
	fmt.Fprintf(&b, "  email_safe=%d\n", sum.EmailSafe)
	fmt.Fprintf(&b, "  unresolved_blockers=%d\n", sum.UnresolvedBlockers)
	b.WriteString("  route_distribution:\n")
	keys := make([]string, 0, len(sum.RouteDistribution))
	for k := range sum.RouteDistribution {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		b.WriteString("    (none)\n")
	}
	for _, k := range keys {
		fmt.Fprintf(&b, "    %s=%d\n", k, sum.RouteDistribution[k])
	}
	b.WriteString("  next_human_actions:\n")
	if len(sum.NextHumanActions) == 0 {
		b.WriteString("    (none)\n")
	}
	for _, line := range sum.NextHumanActions {
		fmt.Fprintf(&b, "    - %s\n", line)
	}
	return b.String()
}

func parseFeedNow(feed *Feed) time.Time {
	if feed == nil {
		return time.Now().UTC()
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(feed.GeneratedAt)); err == nil {
		return t
	}
	return time.Now().UTC()
}

func firstWarning(vals []string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
