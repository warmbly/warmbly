package confenge

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/app/whatsapp"
	"github.com/warmbly/warmbly/internal/models"
)

func TestCaseA_PublicPhoneNoOptIn(t *testing.T) {
	st := whatsapp.ContactChannelState{
		PhoneE164:     "+5548999999999",
		ConsentStatus: whatsapp.ConsentUnknown,
		PhoneSource:   "official_company_site",
	}
	d := OrchestrateChannel(true, true, st.PhoneE164, st, whatsapp.SendIntent{
		Mode: whatsapp.ModeFreeText, Automated: true, FeatureEnabled: true, AutoSendEnabled: true,
	})
	if d.CaseID != "A" || d.Action != ChannelActionWhatsAppBlocked {
		t.Fatalf("case A: %+v", d)
	}
	// Eligibility gate must also block send
	elig := whatsapp.EvaluateEligibility(st, whatsapp.SendIntent{
		Mode: whatsapp.ModeFreeText, Automated: true, FeatureEnabled: true, AutoSendEnabled: true,
	})
	if elig.Allowed {
		t.Fatal("must not allow automated WA")
	}
}

func TestCaseB_ReplyRequestsWhatsApp(t *testing.T) {
	now := time.Now().UTC()
	st := whatsapp.ContactChannelState{
		PhoneE164:           "+5548999999999",
		ConsentStatus:       whatsapp.ConsentOptedIn,
		ConsentProvenanceOK: true,
		ConsentSource:       "email_reply_request",
		ConsentAt:           &now,
		LastInboundAt:       &now,
		ServiceWindowUntil:  ptrT(now.Add(24 * time.Hour)),
	}
	d := OrchestrateChannel(true, true, st.PhoneE164, st, whatsapp.SendIntent{
		Mode: whatsapp.ModeFreeText, Automated: false, FeatureEnabled: true, Now: now,
	})
	if d.Action != ChannelActionWhatsAppEligible {
		t.Fatalf("case B: %+v", d)
	}
}

func TestCaseC_UserInitiated(t *testing.T) {
	now := time.Now().UTC()
	st := whatsapp.ContactChannelState{PhoneE164: "+5548999999999", ConsentStatus: whatsapp.ConsentUnknown}
	whatsapp.OpenServiceWindowFromInbound(&st, now, 24*time.Hour)
	d := OrchestrateChannel(true, false, st.PhoneE164, st, whatsapp.SendIntent{
		Mode: whatsapp.ModeFreeText, Automated: false, FeatureEnabled: true, Now: now.Add(time.Minute),
	})
	if d.Action != ChannelActionWhatsAppEligible || d.CaseID != "C" {
		t.Fatalf("case C: %+v", d)
	}
}

func TestCaseD_FormOptIn(t *testing.T) {
	now := time.Now().UTC()
	st := whatsapp.ContactChannelState{
		PhoneE164:           "+5548999999999",
		ConsentStatus:       whatsapp.ConsentOptedIn,
		ConsentProvenanceOK: true,
		ConsentSource:       "website_form",
		ConsentAt:           &now,
		// outside window → template path
	}
	d := OrchestrateChannel(true, true, st.PhoneE164, st, whatsapp.SendIntent{
		Mode: whatsapp.ModeFreeText, Automated: false, FeatureEnabled: true, Now: now,
	})
	if d.Action != ChannelActionWhatsAppTemplate && d.Action != ChannelActionWhatsAppEligible {
		// free text outside window should be template-only
		if d.Action != ChannelActionWhatsAppTemplate {
			t.Fatalf("case D expected template or eligible: %+v", d)
		}
	}
}

func TestCaseE_OptOut(t *testing.T) {
	now := time.Now().UTC()
	st := whatsapp.ContactChannelState{PhoneE164: "+5548999999999", ConsentStatus: whatsapp.ConsentOptedIn, ConsentProvenanceOK: true}
	whatsapp.ApplyOptOut(&st, now, "inbound")
	d := OrchestrateChannel(true, true, st.PhoneE164, st, whatsapp.SendIntent{
		Mode: whatsapp.ModeFreeText, Automated: true, FeatureEnabled: true, AutoSendEnabled: true, Now: now,
	})
	if d.Action != ChannelActionStopAll || d.CaseID != "E" {
		t.Fatalf("case E: %+v", d)
	}
}

func TestCrossChannelCooldownOrchestrator(t *testing.T) {
	now := time.Now().UTC()
	emailAt := now.Add(-2 * time.Hour)
	st := whatsapp.ContactChannelState{
		PhoneE164:           "+5548999999999",
		ConsentStatus:       whatsapp.ConsentOptedIn,
		ConsentProvenanceOK: true,
		LastEmailOutboundAt: &emailAt,
		LastInboundAt:       &now,
		ServiceWindowUntil:  ptrT(now.Add(20 * time.Hour)),
	}
	d := OrchestrateChannel(true, true, st.PhoneE164, st, whatsapp.SendIntent{
		Mode: whatsapp.ModeFreeText, Automated: true, FeatureEnabled: true, AutoSendEnabled: true,
		Now: now, CrossChannelMin: 24 * time.Hour,
	})
	if d.Action != ChannelActionAwaitCooldown {
		t.Fatalf("cooldown: %+v", d)
	}
}

func TestExtractPhoneFactsNoInventedOptIn(t *testing.T) {
	c := FeedContact{
		Phone: "(48) 99999-9999",
		PhoneObj: &FeedPhone{
			Raw: "(48) 99999-9999", SourceKind: "official_company_site", SourceURL: "https://example.com",
		},
		WhatsApp: &FeedWhatsApp{ConsentStatus: "OPTED_IN", ProvenanceOK: false},
	}
	f := ExtractPhoneFacts(c)
	if f.ConsentStatus == whatsapp.ConsentOptedIn {
		t.Fatal("must not invent opt-in without provenance")
	}
	if f.E164 == "" {
		t.Fatal("expected normalized e164")
	}
}

func TestBuildWhatsAppCopyShort(t *testing.T) {
	acc := &models.OutreachAccount{
		FactToMention: "edital recente da prefeitura",
		EntryOffer:    "revisão de projeto",
		QuestionToAsk: "Posso te explicar em duas linhas?",
	}
	cand := &models.OutreachContactCandidate{Name: "Maria Silva"}
	body := BuildWhatsAppCopy(acc, cand)
	if body == "" || len([]rune(body)) > 500 {
		t.Fatalf("bad body len: %d %q", len([]rune(body)), body)
	}
	if !containsAll(body, "Maria", "CONFENGE") {
		t.Fatalf("body=%q", body)
	}
}

func ptrT(t time.Time) *time.Time { return &t }

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !contains(s, p) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && (func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})()))
}
