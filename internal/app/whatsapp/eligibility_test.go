package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func baseState() ContactChannelState {
	return ContactChannelState{
		ContactID:      uuid.New(),
		OrganizationID: uuid.New(),
		PhoneE164:      "+5548999999999",
		PhoneSource:    "official_company_site",
		ConsentStatus:  ConsentUnknown,
	}
}

func TestPublicPhoneNoOptInNeverSends(t *testing.T) {
	mock := NewMockProvider()
	svc := NewService(Config{Enabled: true, AutoSendEnabled: true, CrossChannelInterval: 24 * time.Hour, ServiceWindow: 24 * time.Hour}, mock, nil)

	for _, consent := range []string{ConsentUnknown, ConsentNoOptIn, ""} {
		t.Run(consent, func(t *testing.T) {
			mock.Reset()
			st := baseState()
			st.ConsentStatus = consent
			intent := SendIntent{
				Mode:            ModeFreeText,
				Automated:       true,
				AutoSendEnabled: true,
				FeatureEnabled:  true,
				Now:             time.Now().UTC(),
			}
			ext, err := svc.Send(context.Background(), st, intent, &SendTextRequest{
				ToE164: "5548999999999",
				Body:   "Olá",
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if ext.Decision.Allowed {
				t.Fatalf("public phone with consent=%q must not allow send", consent)
			}
			if mock.SendCount() != 0 {
				t.Fatalf("invariant broken: provider send count=%d want 0", mock.SendCount())
			}
		})
	}
}

func TestOptedInWithProvenanceAllowsInWindow(t *testing.T) {
	mock := NewMockProvider()
	svc := NewService(Config{Enabled: true, AutoSendEnabled: true, CrossChannelInterval: 0, ServiceWindow: 24 * time.Hour}, mock, nil)
	now := time.Now().UTC()
	in := now.Add(-1 * time.Hour)
	st := baseState()
	st.ConsentStatus = ConsentOptedIn
	st.ConsentProvenanceOK = true
	st.ConsentSource = "website_form"
	st.LastInboundAt = &in
	st.ServiceWindowUntil = ptrTime(now.Add(20 * time.Hour))

	ext, err := svc.Send(context.Background(), st, SendIntent{
		Mode: ModeFreeText, Automated: true, AutoSendEnabled: true, FeatureEnabled: true, Now: now,
	}, &SendTextRequest{ToE164: "5548999999999", Body: "Oi"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ext.Decision.Allowed {
		t.Fatalf("expected allowed, got %+v", ext.Decision)
	}
	if mock.SendCount() != 1 {
		t.Fatalf("send count=%d", mock.SendCount())
	}
}

func TestOptedInWithoutProvenanceBlocked(t *testing.T) {
	mock := NewMockProvider()
	svc := NewService(Config{Enabled: true, AutoSendEnabled: true}, mock, nil)
	st := baseState()
	st.ConsentStatus = ConsentOptedIn
	st.ConsentProvenanceOK = false
	ext, err := svc.Send(context.Background(), st, SendIntent{
		Mode: ModeFreeText, Automated: true, AutoSendEnabled: true, FeatureEnabled: true,
	}, &SendTextRequest{ToE164: "5548999999999", Body: "Oi"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ext.Decision.Allowed || mock.SendCount() != 0 {
		t.Fatalf("bare opted_in without provenance must block: %+v count=%d", ext.Decision, mock.SendCount())
	}
}

func TestOptedOutAndDNCBlock(t *testing.T) {
	mock := NewMockProvider()
	svc := NewService(Config{Enabled: true, AutoSendEnabled: true}, mock, nil)
	now := time.Now().UTC()

	for _, st := range []ContactChannelState{
		func() ContactChannelState {
			s := baseState()
			s.ConsentStatus = ConsentOptedOut
			s.OptOutAt = &now
			return s
		}(),
		func() ContactChannelState {
			s := baseState()
			s.ConsentStatus = ConsentDoNotContact
			s.DoNotContact = true
			return s
		}(),
	} {
		mock.Reset()
		ext, err := svc.Send(context.Background(), st, SendIntent{
			Mode: ModeFreeText, Automated: true, AutoSendEnabled: true, FeatureEnabled: true, Now: now,
		}, &SendTextRequest{ToE164: "5548999999999", Body: "Oi"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if ext.Decision.Allowed || mock.SendCount() != 0 {
			t.Fatalf("opt-out/DNC must block: %+v", ext.Decision)
		}
	}
}

func TestUserInitiatedOpensServiceWindow(t *testing.T) {
	mock := NewMockProvider()
	svc := NewService(Config{Enabled: true, AutoSendEnabled: true, CrossChannelInterval: 0, ServiceWindow: 24 * time.Hour}, mock, nil)
	now := time.Now().UTC()
	st := baseState()
	st.ConsentStatus = ConsentUnknown
	OpenServiceWindowFromInbound(&st, now, 24*time.Hour)
	if st.ConsentStatus != ConsentUserInitiated {
		t.Fatalf("consent=%s", st.ConsentStatus)
	}
	ext, err := svc.Send(context.Background(), st, SendIntent{
		Mode: ModeFreeText, Automated: true, AutoSendEnabled: true, FeatureEnabled: true, Now: now.Add(time.Minute),
	}, &SendTextRequest{ToE164: "5548999999999", Body: "Resposta"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ext.Decision.Allowed {
		t.Fatalf("user-initiated window should allow: %+v", ext.Decision)
	}
}

func TestCrossChannelCooldown(t *testing.T) {
	mock := NewMockProvider()
	svc := NewService(Config{Enabled: true, AutoSendEnabled: true, CrossChannelInterval: 24 * time.Hour, ServiceWindow: 24 * time.Hour}, mock, nil)
	now := time.Now().UTC()
	emailAt := now.Add(-1 * time.Hour)
	st := baseState()
	st.ConsentStatus = ConsentOptedIn
	st.ConsentProvenanceOK = true
	st.LastEmailOutboundAt = &emailAt
	st.LastInboundAt = &now
	st.ServiceWindowUntil = ptrTime(now.Add(20 * time.Hour))

	ext, err := svc.Send(context.Background(), st, SendIntent{
		Mode: ModeFreeText, Automated: true, AutoSendEnabled: true, FeatureEnabled: true, Now: now,
		CrossChannelMin: 24 * time.Hour,
	}, &SendTextRequest{ToE164: "5548999999999", Body: "Oi"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ext.Decision.Allowed || ext.Decision.Reason != "cross_channel_cooldown" {
		t.Fatalf("expected cooldown block, got %+v", ext.Decision)
	}
	if mock.SendCount() != 0 {
		t.Fatal("must not send during cooldown")
	}
}

func TestStickyOptOutSurvivesImport(t *testing.T) {
	now := time.Now().UTC()
	existing := baseState()
	ApplyOptOut(&existing, now, "inbound_phrase:parar")
	merged := MergeImportConsent(existing, ConsentOptedIn, "website", &now, true)
	if merged.ConsentStatus != ConsentOptedOut {
		t.Fatalf("opt-out must stick over import: %s", merged.ConsentStatus)
	}
	// Public import without provenance must not invent opt-in
	fresh := baseState()
	fresh.ConsentStatus = ConsentUnknown
	merged2 := MergeImportConsent(fresh, ConsentOptedIn, "public_site", nil, false)
	if merged2.ConsentStatus == ConsentOptedIn {
		t.Fatal("must not invent opt-in from public import without provenance")
	}
}

func TestTemplateNotApprovedBlocked(t *testing.T) {
	mock := NewMockProvider()
	cat := NewStaticTemplateCatalog()
	cat.Set("hello", "pt_BR", TemplatePaused)
	svc := NewService(Config{Enabled: true, AutoSendEnabled: true, CrossChannelInterval: 0}, mock, cat)
	st := baseState()
	st.ConsentStatus = ConsentOptedIn
	st.ConsentProvenanceOK = true
	ext, err := svc.Send(context.Background(), st, SendIntent{
		Mode: ModeTemplate, Automated: true, AutoSendEnabled: true, FeatureEnabled: true,
	}, nil, &SendTemplateRequest{ToE164: "5548999999999", TemplateName: "hello", Language: "pt_BR"})
	if err != nil {
		t.Fatal(err)
	}
	if ext.Decision.Allowed || mock.SendCount() != 0 {
		t.Fatalf("paused template must not send: %+v", ext.Decision)
	}
}

func TestFeatureDisabledBlocks(t *testing.T) {
	mock := NewMockProvider()
	svc := NewService(Config{Enabled: false}, mock, nil)
	st := baseState()
	st.ConsentStatus = ConsentOptedIn
	st.ConsentProvenanceOK = true
	ext, err := svc.Send(context.Background(), st, SendIntent{Mode: ModeFreeText, Automated: true}, &SendTextRequest{Body: "x", ToE164: "55"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ext.Decision.Allowed {
		t.Fatal("disabled feature must block")
	}
}

func TestOutsideServiceWindowTemplateOnly(t *testing.T) {
	st := baseState()
	st.ConsentStatus = ConsentOptedIn
	st.ConsentProvenanceOK = true
	// no inbound, window closed
	d := EvaluateEligibility(st, SendIntent{
		Mode: ModeFreeText, Automated: false, FeatureEnabled: true, Now: time.Now().UTC(),
	})
	if d.Allowed || d.Eligibility != EligTemplateOnly {
		t.Fatalf("expected template-only outside window: %+v", d)
	}
}

// USER_INITIATED after the service window expires must not allow free-text.
// Free-text requires an open window; outside it is template-only.
func TestUserInitiatedOutsideWindowBlocksFreeText(t *testing.T) {
	now := time.Now().UTC()
	inbound := now.Add(-48 * time.Hour)
	until := now.Add(-24 * time.Hour) // expired
	st := baseState()
	st.ConsentStatus = ConsentUserInitiated
	st.ConsentProvenanceOK = true
	st.ConsentSource = "inbound_whatsapp"
	st.LastInboundAt = &inbound
	st.ServiceWindowUntil = &until

	d := EvaluateEligibility(st, SendIntent{
		Mode: ModeFreeText, Automated: false, FeatureEnabled: true, Now: now,
		ServiceWindow: 24 * time.Hour,
	})
	if d.Allowed {
		t.Fatalf("free-text must be blocked outside service window: %+v", d)
	}
	if d.Eligibility != EligTemplateOnly || !d.UseTemplate {
		t.Fatalf("expected template-only: %+v", d)
	}
	if d.OpenServiceWindow {
		t.Fatal("OpenServiceWindow must be false when window expired")
	}

	// Approved template may still send outside window for USER_INITIATED.
	d2 := EvaluateEligibility(st, SendIntent{
		Mode: ModeTemplate, TemplateApproved: true, Automated: false, FeatureEnabled: true, Now: now,
		ServiceWindow: 24 * time.Hour,
	})
	if !d2.Allowed || !d2.UseTemplate {
		t.Fatalf("approved template should be allowed: %+v", d2)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
