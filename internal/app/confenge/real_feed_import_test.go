package confenge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// TestRealExtraCLISmokeChunkParsesAndImports validates a feed produced by the
// real extra-cli pipeline (datalake sample → universe → intelligence → feed),
// not a hand-written demo fixture.
func TestRealExtraCLISmokeChunkParsesAndImports(t *testing.T) {
	path := filepath.Join("testdata", "real_smoke_chunk.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("real smoke chunk missing (%v); produce via confenge_outreach_pipeline", err)
	}
	feed, err := DetectAndNormalize(raw)
	if err != nil {
		t.Fatalf("DetectAndNormalize: %v", err)
	}
	if err := ValidateFeed(feed); err != nil {
		t.Fatalf("ValidateFeed: %v", err)
	}
	if len(feed.Leads) < 30 {
		t.Fatalf("want >=30 real leads, got %d", len(feed.Leads))
	}

	// Diversity: not a single service for the whole chunk
	services := map[string]int{}
	cnpjs := map[string]struct{}{}
	dnc := 0
	for i, lead := range feed.Leads {
		if lv := ValidateLead(i, lead); lv != nil {
			t.Fatalf("lead %d invalid: %s", i, lv.Message)
		}
		if lead.Company.CNPJ14 == "" {
			t.Fatalf("lead %d missing cnpj", i)
		}
		if _, dup := cnpjs[lead.Company.CNPJ14]; dup {
			t.Fatalf("duplicate cnpj in feed: %s", lead.Company.CNPJ14)
		}
		cnpjs[lead.Company.CNPJ14] = struct{}{}
		if lead.Offer.ServiceCode != "" {
			services[lead.Offer.ServiceCode]++
		}
		if lead.CommercialState == "DO_NOT_CONTACT" {
			dnc++
		}
		// Messaging context must remain intact when intelligence produced it
		if lead.MessagingContext.FactToMention == "" && lead.Offer.ServiceCode != "" {
			t.Fatalf("lead %s has service but empty fact_to_mention", lead.Company.CNPJ14)
		}
	}
	if len(services) < 2 {
		t.Fatalf("expected multi-service distribution from real intelligence, got %v", services)
	}

	// Import into memory repo (dry-run + apply)
	r := newMemRepo()
	svc := NewService(Config{
		Enabled:              true,
		DefaultDailyLimit:    DefaultCampaignDailyLimit,
		MaxInitialEmailWords: 120,
		RequireHumanApproval: true,
	}, r, nil)

	orgID := uuid.New()
	userID := uuid.New()
	dry, xerr := svc.ImportFromBytes(context.Background(), orgID, &userID, raw, ImportOptions{DryRun: true})
	if xerr != nil {
		t.Fatalf("dry-run import: %v", xerr)
	}
	if dry == nil || dry.Counts.LeadsProcessed < 30 {
		t.Fatalf("dry-run counts: %+v", dry)
	}

	run, xerr := svc.ImportFromBytes(context.Background(), orgID, &userID, raw, ImportOptions{})
	if xerr != nil {
		t.Fatalf("import: %v", xerr)
	}
	if run.Counts.Creates+run.Counts.Updates < 1 {
		t.Fatalf("expected accounts created/updated, got %+v", run.Counts)
	}
	// Reimport must not explode duplicates
	run2, xerr := svc.ImportFromBytes(context.Background(), orgID, &userID, raw, ImportOptions{
		IdempotencyKey: "real-smoke-reimport",
	})
	if xerr != nil {
		t.Fatalf("reimport: %v", xerr)
	}
	_ = run2
	_ = dnc
}
