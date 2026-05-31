// Package seed populates a development database with a rich, self-consistent
// fixture set so every feature of the platform can be exercised end-to-end.
//
// All entities use deterministic UUIDs so the seed is idempotent and safe to
// re-run. Passwords use argon2id hashing (the same as production registration)
// and the plaintexts are surfaced in Result so a human can log in.
package seed

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPassword is the plaintext password every seeded user is given. It is
// hashed with argon2id on insert; only kept in plaintext here so the developer
// running the seed can log in.
const TestPassword = "Test1234!"

// Run executes every seed step in dependency order against the provided
// connection pool and returns a Result describing what was created.
func Run(ctx context.Context, pool *pgxpool.Pool) (*Result, error) {
	r := &Result{Password: TestPassword}

	steps := []struct {
		name string
		fn   func(context.Context, *pgxpool.Pool, *Result) error
	}{
		{"durations", seedDurations},
		{"plans", seedPlans},
		{"workers", seedWorkers},
		{"users", seedUsers},
		{"organizations", seedOrganizations},
		{"subscriptions", seedSubscriptions},
		{"discount-codes", seedDiscountCodes},
		{"worker-assignments", seedWorkerAssignments},
		{"api-keys", seedAPIKeys},
		{"folders-tags-categories", seedGroups},
		{"email-accounts", seedEmailAccounts},
		{"email-tag-bindings", seedEmailTagBindings},
		{"unibox", seedUnibox},
		{"warmup-participants", seedWarmupParticipants},
		{"reply-templates", seedReplyTemplates},
		{"campaigns", seedCampaigns},
		{"sequences", seedSequences},
		{"contacts", seedContacts},
		{"campaign-progress", seedCampaignProgress},
		{"campaign-logs", seedCampaignLogs},
		{"crm-pipelines", seedCRMPipelines},
		{"crm-deals", seedCRMDeals},
		{"crm-tasks", seedCRMTasks},
		{"contact-activity", seedContactActivity},
		{"admin-audit", seedAdminAudit},
		{"enterprise-inquiries", seedEnterpriseInquiries},
	}

	for _, step := range steps {
		if err := step.fn(ctx, pool, r); err != nil {
			return nil, fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return r, nil
}
