package confenge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestOperatorEditSignalPackedIntoValidationJSON(t *testing.T) {
	orig := "Olá, temos sinergia e vocês têm R$ a receber. Podemos marcar uma call?"
	edited := "Olá, pelo contrato publicado há um ponto a conferir. Posso mandar o checklist?"
	sig := NewOperatorEditSignal("draft-1", orig, edited)
	if len(sig.Codes) == 0 {
		t.Fatal("expected codes")
	}
	val := ValidationResult{OK: true, DoctrineVersion: OutreachDoctrineVersion}
	val.OperatorEdit = &sig
	b, err := json.Marshal(val)
	if err != nil {
		t.Fatal(err)
	}
	var back ValidationResult
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.OperatorEdit == nil || len(back.OperatorEdit.Codes) == 0 {
		t.Fatal("operator_edit not round-tripped")
	}
}

func TestOperatorRejectReasonPacked(t *testing.T) {
	rej := NewOperatorRejection("too_salesy", "d1", "REAJUSTE", "REAJUSTE_CHECK")
	val := ValidationResult{OK: false}
	val.OperatorReject = &rej
	b, _ := json.Marshal(val)
	if !strings.Contains(string(b), "too_salesy") {
		t.Fatalf("%s", b)
	}
	if !ValidRejectionReason(MustPlaybook(), "unsupported_claim") {
		t.Fatal("valid reason")
	}
}

// Ensures EditTouchpoint path shape: original vs edited bodies produce learning codes.
func TestEditLearningCodesMatchApproveEditPath(t *testing.T) {
	// Mirrors approve&queue which calls editConfengeTouchpoint before approve.
	orig := "A CONFENGE é especializada e pode maximizar resultados. Gostaria de agendar 30 minutos?"
	edited := "Pelo contrato publicado, há um ponto a conferir. Posso te mandar o checklist?"
	codes := ClassifyEditSignals(orig, edited)
	wantAny := map[string]bool{"shortened": true, "softened_claim": true, "changed_cta": true, "removed_jargon": true, "other": true}
	ok := false
	for _, c := range codes {
		if wantAny[c] {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("codes %v", codes)
	}
	_ = uuid.New()
	_ = models.OutreachDraftNeedsReview
}
