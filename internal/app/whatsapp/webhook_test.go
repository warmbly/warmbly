package whatsapp

import (
	"context"
	"testing"
	"time"
)

func TestWebhookAuthSecret(t *testing.T) {
	a := WebhookAuth{Secret: "s3cret", MaxBytes: 1024}
	if err := a.ValidateHeaders("Bearer s3cret", "", "application/json", 10); err != nil {
		t.Fatal(err)
	}
	if err := a.ValidateHeaders("Bearer wrong", "", "application/json", 10); err == nil {
		t.Fatal("expected invalid secret")
	}
	if err := a.ValidateHeaders("", "s3cret", "application/json", 10); err != nil {
		t.Fatal(err)
	}
	if err := a.ValidateHeaders("Bearer s3cret", "", "text/plain", 10); err == nil {
		t.Fatal("expected media type error")
	}
	if err := a.ValidateHeaders("Bearer s3cret", "", "application/json", 5000); err == nil {
		t.Fatal("expected oversize")
	}
}

func TestWebhookIdempotencyAndOptOut(t *testing.T) {
	svc := NewService(Config{Enabled: true, ServiceWindow: 24 * time.Hour}, NewMockProvider(), nil)
	st := ContactChannelState{
		PhoneE164:     "+5548999999999",
		ConsentStatus: ConsentUnknown,
	}
	ev := ChannelEvent{
		Channel:           ChannelWhatsApp,
		Provider:          ProviderEvolution,
		EventType:         EventMessageReceived,
		ExternalMessageID: "msg-1",
		ExternalEventID:   "msg-1:MESSAGE_RECEIVED",
		FromE164:          "+5548999999999",
		OccurredAt:        time.Now().UTC(),
		Content:           Content{Type: ContentText, Text: "não tenho interesse"},
	}
	r1, err := svc.ProcessInbound(context.Background(), &st, ev)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Duplicate || !r1.OptOut.Matched || !r1.StopSequences {
		t.Fatalf("first inbound: %+v opt=%+v", r1, r1.OptOut)
	}
	if st.ConsentStatus != ConsentOptedOut {
		t.Fatalf("consent=%s", st.ConsentStatus)
	}
	r2, err := svc.ProcessInbound(context.Background(), &st, ev)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Duplicate {
		t.Fatal("duplicate event must be detected")
	}
}

func TestConfigBaileysProductionGuard(t *testing.T) {
	t.Setenv(EnvEnabled, "true")
	t.Setenv(EnvEvolutionAllowBaileys, "true")
	t.Setenv(EnvAppEnv, "production")
	t.Setenv(EnvEvolutionBaseURL, "https://evo.example")
	t.Setenv(EnvEvolutionAPIKey, "k")
	t.Setenv(EnvEvolutionInstance, "i")
	t.Setenv(EnvWebhookSecret, "w")
	cfg := LoadConfig()
	if err := cfg.ValidateStartup(); err == nil {
		t.Fatal("production + baileys must fail validation")
	}
}

func TestConfigDefaultsOff(t *testing.T) {
	t.Setenv(EnvEnabled, "")
	t.Setenv(EnvAutoSend, "")
	t.Setenv(EnvAutoReply, "")
	t.Setenv(EnvEvolutionAllowBaileys, "")
	cfg := LoadConfig()
	if cfg.Enabled || cfg.AutoSendEnabled || cfg.AutoReplyEnabled || cfg.AllowBaileys {
		t.Fatalf("defaults must be off: %+v", cfg)
	}
}
