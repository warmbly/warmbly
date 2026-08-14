package confenge

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestSignAndVerifyOutcomeHMAC(t *testing.T) {
	secret := "whsec_test"
	body := []byte(`{"event_type":"REPLIED"}`)
	ts := time.Unix(1700000000, 0).UTC()
	hdr := SignOutcomeHMAC(secret, ts, body)
	if !VerifyOutcomeHMAC(secret, hdr, body, ts, 5*time.Minute) {
		t.Fatal("valid signature rejected")
	}
	if VerifyOutcomeHMAC("other", hdr, body, ts, 5*time.Minute) {
		t.Fatal("wrong secret accepted")
	}
	if VerifyOutcomeHMAC(secret, hdr, []byte(`{}`), ts, 5*time.Minute) {
		t.Fatal("tampered body accepted")
	}
	// Outside skew window
	if VerifyOutcomeHMAC(secret, hdr, body, ts.Add(10*time.Minute), 5*time.Minute) {
		t.Fatal("stale timestamp accepted")
	}
}

func TestOutcomeBackoffGrows(t *testing.T) {
	if OutcomeBackoff(1) != 30*time.Second {
		t.Fatal(OutcomeBackoff(1))
	}
	if OutcomeBackoff(6) != time.Hour {
		t.Fatal(OutcomeBackoff(6))
	}
}

func TestBuildOutcomeEnvelopePromotesActivationTopLevel(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"service_code":              "reajuste",
		"moment_code":               "NEW_CONTRACT",
		"activation_policy_version": "confenge-activation-v1",
		"activation_score":          82.4,
		"activation_reason_codes":   []string{"NEW_AMENDMENT_OR_TERM"},
		"activation_source_hash":    "sha256abc",
		"generated_context_hash":    "ctx123",
		"touchpoint_ordinal":        2,
		"channel":                   "EMAIL",
	})
	ev := &models.OutreachOutcome{
		EventID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		IdempotencyKey: "k1",
		SourceLeadID:   "lead-1",
		CNPJ14:         "11222333000181",
		ContactEmail:   "a@b.com",
		EventType:      OutcomeContacted,
		Payload:        payload,
		OccurredAt:     time.Unix(1700000000, 0).UTC(),
	}
	env := BuildOutcomeEnvelope(ev)
	if env.ServiceCode != "reajuste" {
		t.Fatalf("service_code=%q", env.ServiceCode)
	}
	if env.MomentCode != "NEW_CONTRACT" {
		t.Fatalf("moment_code=%q", env.MomentCode)
	}
	if env.ActivationPolicyVersion != "confenge-activation-v1" {
		t.Fatalf("policy=%q", env.ActivationPolicyVersion)
	}
	if env.ActivationScore != 82.4 {
		t.Fatalf("score=%v", env.ActivationScore)
	}
	if len(env.ActivationReasonCodes) != 1 || env.ActivationReasonCodes[0] != "NEW_AMENDMENT_OR_TERM" {
		t.Fatalf("reasons=%v", env.ActivationReasonCodes)
	}
	if env.ActivationSourceHash != "sha256abc" {
		t.Fatalf("source_hash=%q", env.ActivationSourceHash)
	}
	if env.GeneratedContextHash != "ctx123" {
		t.Fatalf("gen_hash=%q", env.GeneratedContextHash)
	}
	if env.TouchpointOrdinal != 2 {
		t.Fatalf("ordinal=%d", env.TouchpointOrdinal)
	}
	if env.Channel != "EMAIL" {
		t.Fatalf("channel=%q", env.Channel)
	}
	// Metadata still present for back-compat
	if env.Metadata["service_code"] != "reajuste" {
		t.Fatal("metadata should retain service_code")
	}
}
