package confenge

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestFulfillmentDeliversBeforeMeeting(t *testing.T) {
	pb := MustPlaybook()
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149 aniversário")
	st := PlanOutreachStrategy(pb, acc, testCand("Sócio"), []models.OutreachEvidence{{
		SourceEvidenceID: "ev-1",
	}}, 1)
	fd, err := BuildFulfillmentDraft(pb, st, acc, nil)
	if err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(fd.BodyText)
	if strings.Contains(low, "marcar uma call") || strings.Contains(low, "calendly") {
		t.Fatal("must not pivot to meeting")
	}
	if !strings.Contains(low, "confer") && !strings.Contains(low, "checklist") && !strings.Contains(low, "1)") {
		t.Fatalf("expected deliverable content: %s", fd.BodyText)
	}
	if strings.Contains(low, "reajuste a receber") {
		t.Fatal("prohibited claim in fulfillment")
	}
	if fd.DoctrineVersion != OutreachDoctrineVersion {
		t.Fatal(fd.DoctrineVersion)
	}
}

func TestOperatorEditAndReject(t *testing.T) {
	pb := MustPlaybook()
	if !ValidRejectionReason(pb, "too_salesy") {
		t.Fatal("rejection reason")
	}
	if ValidRejectionReason(pb, "not_a_real_reason_xyz") {
		t.Fatal("invalid reason")
	}
	orig := "Olá, temos sinergia e vocês têm R$ a receber. Podemos marcar uma call?"
	edited := "Olá, pelo contrato publicado há um ponto a conferir. Posso mandar o checklist?"
	codes := ClassifyEditSignals(orig, edited)
	if len(codes) == 0 {
		t.Fatal("expected edit codes")
	}
	sig := NewOperatorEditSignal("d1", orig, edited)
	if sig.DoctrineVersion != OutreachDoctrineVersion {
		t.Fatal()
	}
	rej := NewOperatorRejection("unsupported_claim", "d1", "REAJUSTE", "REAJUSTE_CHECK")
	if rej.Reason != "unsupported_claim" {
		t.Fatal()
	}
}
