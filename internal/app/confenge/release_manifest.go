package confenge

import (
	"strings"
	"time"
)

const (
	ReleaseGO   = "GO_FOR_CONTROLLED_PILOT"
	ReleaseNOGO = "NO_GO"
)

// ReleaseManifest binds the exact release a canary may use.
type ReleaseManifest struct {
	RepositorySHA          string    `json:"repository_sha"`
	ImageDigests           []string  `json:"image_digests,omitempty"`
	Schema                 string    `json:"schema"`
	FeedHash               string    `json:"feed_hash"`
	CohortHash             string    `json:"cohort_hash"`
	PolicyVersion          string    `json:"policy_version"`
	ComposerVersion        string    `json:"composer_version"`
	DoctrineVersion        string    `json:"doctrine_version"`
	RecipientPolicyVersion string    `json:"recipient_policy_version"`
	ApprovalsHash          string    `json:"approvals_hash"`
	CIResults              string    `json:"ci_results"`
	RuntimeResults         string    `json:"runtime_results"`
	HumanApprovals         int       `json:"human_approvals"`
	ReadyCount             int       `json:"ready_count"`
	KillSwitch             bool      `json:"kill_switch"`
	AutoSend               bool      `json:"auto_send"`
	RequireHumanApproval   bool      `json:"require_human_approval"`
	EvaluatedAt            time.Time `json:"evaluated_at"`
}

// ReleaseVerdict is exactly GO or NO_GO. Never "quase GO".
type ReleaseVerdict struct {
	Verdict string   `json:"verdict"`
	Reasons []string `json:"reasons,omitempty"`
}

// EvaluateRelease is fail-closed. Missing human approvals or any drift is NO_GO.
func EvaluateRelease(want, got ReleaseManifest) ReleaseVerdict {
	var reasons []string
	check := func(ok bool, code string) {
		if !ok {
			reasons = append(reasons, code)
		}
	}
	check(got.RepositorySHA != "" && got.RepositorySHA == want.RepositorySHA, "sha_drift")
	check(sameStringSet(want.ImageDigests, got.ImageDigests), "image_drift")
	check(got.Schema != "" && got.Schema == want.Schema, "schema_drift")
	check(got.FeedHash != "" && got.FeedHash == want.FeedHash, "feed_drift")
	check(got.CohortHash != "" && got.CohortHash == want.CohortHash, "cohort_drift")
	check(got.ComposerVersion == ComposerVersion, "composer_drift")
	check(got.DoctrineVersion == OutreachDoctrineVersion, "doctrine_drift")
	check(got.RecipientPolicyVersion == RecipientPolicyVersion, "recipient_policy_drift")
	check(got.ApprovalsHash != "" && got.ApprovalsHash == want.ApprovalsHash, "approvals_drift")
	check(strings.EqualFold(got.CIResults, "pass"), "ci_not_green")
	check(strings.EqualFold(got.RuntimeResults, "pass"), "runtime_not_green")
	check(got.KillSwitch, "kill_switch_off")
	check(!got.AutoSend, "auto_send_enabled")
	check(got.RequireHumanApproval, "human_approval_disabled")
	check(got.HumanApprovals > 0 && got.HumanApprovals == got.ReadyCount && got.ReadyCount > 0, "insufficient_human_approvals")
	if len(reasons) > 0 {
		return ReleaseVerdict{Verdict: ReleaseNOGO, Reasons: reasons}
	}
	return ReleaseVerdict{Verdict: ReleaseGO}
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	idx := map[string]int{}
	for _, s := range a {
		idx[s]++
	}
	for _, s := range b {
		idx[s]--
		if idx[s] < 0 {
			return false
		}
	}
	return true
}

// InvalidateOnDrift returns true when any bound hash moved after review.
func InvalidateOnDrift(approved, current ReleaseManifest) bool {
	v := EvaluateRelease(approved, current)
	return v.Verdict != ReleaseGO
}
