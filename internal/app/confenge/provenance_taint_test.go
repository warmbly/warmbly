package confenge

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestDemoObraEmailsAreTainted(t *testing.T) {
	for i := 0; i < 10; i++ {
		email := fmt.Sprintf("licitacoes@demo%03dobra.com.br", i)
		if !IsDemoOrFixtureEmail(email) {
			t.Fatalf("expected demo email tainted: %s", email)
		}
		src := fmt.Sprintf("https://demo%03dobra.com.br/contato", i)
		tainted, reason := ContactProvenanceTainted(email, src, "REAL_OFFICIAL_SITE", "VERIFIED", false)
		if !tainted {
			t.Fatalf("demo must taint even with REAL_OFFICIAL_SITE label: %s reason=%s", email, reason)
		}
	}
}

func TestFixtureDerivedBlocksSendReadyOnImport(t *testing.T) {
	ready := true
	derived := true
	fc := FeedContact{
		Email:              "contato@construtora-alpha.com.br",
		SourceURL:          "https://construtora-alpha.com.br/contato",
		VerificationStatus: "VERIFIED",
		OwnershipStatus:    "COMPANY_OWNED",
		EmailSendReady:     &ready,
		DerivedFromFixture: &derived,
		RootSourceType:     "TEST_FIXTURE",
	}
	cand := leadToCandidate(uuid.Nil, uuid.Nil, uuid.Nil, fc)
	if cand.EmailSendReady {
		t.Fatal("fixture-derived contact must not remain EmailSendReady after import mapping")
	}
	if !cand.Blocked {
		t.Fatal("fixture-derived contact must be blocked")
	}
	if cand.CanEnroll() {
		t.Fatal("blocked tainted contact CanEnroll must be false")
	}
}

func TestSyntheticCompanyOwnedNotSendReady(t *testing.T) {
	ready := true
	fc := FeedContact{
		Email:                          "diretor@acme.com.br",
		SourceURL:                      "https://fixtures.local/acme",
		VerificationStatus:             "HUMAN_CONFIRMED",
		OwnershipStatus:                "COMPANY_OWNED",
		EmailSendReady:                 &ready,
		RecipientCommercialSuitability: "UNSUITABLE_PROVENANCE",
		RootSourceType:                 "SYNTHETIC",
	}
	cand := leadToCandidate(uuid.Nil, uuid.Nil, uuid.Nil, fc)
	if cand.EmailSendReady {
		t.Fatal("synthetic unsuitable provenance must clear EmailSendReady")
	}
}

func TestRealCompanyEmailNotTainted(t *testing.T) {
	if IsDemoOrFixtureEmail("contato@empresa-target.com.br") {
		t.Fatal("real email must not match demo detector")
	}
	tainted, _ := ContactProvenanceTainted(
		"contato@empresa-target.com.br",
		"https://empresa-target.com.br/contato",
		"REAL_OFFICIAL_SITE",
		"OBSERVED",
		false,
	)
	if tainted {
		t.Fatal("real provenance must not taint")
	}
}

func TestCanEnrollBlocksDemoDomain(t *testing.T) {
	c := &models.OutreachContactCandidate{
		Email:              "licitacoes@demo003obra.com.br",
		VerificationStatus: models.OutreachVerifyOfficialSource,
		OwnershipStatus:    "COMPANY_OWNED",
		EmailSendReady:     true,
	}
	if c.CanEnroll() {
		t.Fatal("demo00Xobra must not CanEnroll")
	}
}

func TestStickyVerifiedDemoImportClearsSendReady(t *testing.T) {
	ready := true
	fc := FeedContact{
		Email:              "licitacoes@demo000obra.com.br",
		SourceURL:          "https://demo000obra.com.br/contato",
		VerificationStatus: "VERIFIED",
		OwnershipStatus:    "COMPANY_OWNED",
		EmailSendReady:     &ready,
		Recommended:        true,
	}
	cand := leadToCandidate(uuid.Nil, uuid.Nil, uuid.Nil, fc)
	if cand.EmailSendReady {
		t.Fatal("demo000obra must clear EmailSendReady on import")
	}
	if cand.Recommended {
		t.Fatal("demo000obra must clear Recommended on import")
	}
	if cand.CanEnroll() {
		t.Fatal("demo000obra must not CanEnroll")
	}
}
