package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestAgencyHomeQueriesSelectByOrganizationList(t *testing.T) {
	for name, query := range map[string]string{
		"campaigns": agencyCampaignStatsSQL,
		"mailboxes": agencyMailboxStatsSQL,
		"replies":   agencyRepliesWaitingSQL,
		"advisor":   agencyAdvisorCountsSQL,
	} {
		if !strings.Contains(query, "ANY($1::uuid[])") && !strings.Contains(query, "unnest($1::uuid[])") {
			t.Errorf("%s does not select by the organization list", name)
		}
		if strings.Contains(query, "organization_id = $1 ") || strings.Contains(query, "organization_id = $1\n") {
			t.Errorf("%s compares against a single organization", name)
		}
	}

	q, args := agencyInboxQuery(models.AgencyInboxQuery{OrgIDs: []uuid.UUID{uuid.New()}, Limit: 10})
	if len(args) != 4 || !strings.Contains(q, "ea.organization_id = ANY($1::uuid[])") ||
		!strings.Contains(q, "ea.organization_id = ANY($2::uuid[]) AND ea.id = ANY($3::uuid[])") {
		t.Fatalf("inbox tenant predicate missing: %d args", len(args))
	}
	if strings.Contains(q, "(b.internal_date, b.id) <") {
		t.Fatal("first page carries a keyset clause")
	}
	q, args = agencyInboxQuery(models.AgencyInboxQuery{OrgIDs: []uuid.UUID{uuid.New()}, BeforeAt: time.Now(), BeforeID: uuid.New(), Limit: 10, Unread: true, NeedsReply: true, Positive: true, Search: "hello there"})
	if !strings.Contains(q, "(b.internal_date, b.id) < ($") || len(args) < 7 {
		t.Fatalf("keyset clause or filter args missing: %d args", len(args))
	}
}
