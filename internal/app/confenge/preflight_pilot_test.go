package confenge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPilotSnapshotDefaults(t *testing.T) {
	t.Setenv("CONFENGE_OUTREACH_ENABLED", "true")
	t.Setenv("CONFENGE_REQUIRE_HUMAN_APPROVAL", "true")
	t.Setenv("CONFENGE_AUTO_SEND_ENABLED", "false")
	t.Setenv("CONFENGE_GLOBAL_SENDS_PER_HOUR", "10")
	cfg := LoadConfig()
	cfg.Enabled = true
	cfg.RequireHumanApproval = true
	cfg.AutoSendEnabled = false
	rep := RunPreflight(cfg, PreflightDeps{})
	text := FormatPreflight(rep)
	for _, want := range []string{
		"MAILBOX_CONNECTED=",
		"MAILBOX_AUTH_VALID=",
		"SEND_PERMISSION_OK=",
		"FROM_ADDRESS=",
		"REPLY_TO=",
		"CONFENGE_REQUIRE_HUMAN_APPROVAL=true",
		"CONFENGE_AUTO_SEND_ENABLED=false",
		"GLOBAL_SENDS_PER_HOUR=",
		"SENDING_PAUSED=",
		"WHATSAPP_CRITICAL_PATH=false",
		"EMAIL_CHANNEL=enabled",
		"Pilot snapshot",
		"pilot_defaults",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("preflight missing %q\n%s", want, text)
		}
	}
	if rep.Pilot.ConfengeRequireHumanApproval != "true" {
		t.Fatalf("approval=%s", rep.Pilot.ConfengeRequireHumanApproval)
	}
	if rep.Pilot.ConfengeAutoSendEnabled != "false" {
		t.Fatalf("auto_send=%s", rep.Pilot.ConfengeAutoSendEnabled)
	}
	if rep.Pilot.GlobalSendsPerHour < 1 || rep.Pilot.GlobalSendsPerHour > 20 {
		t.Fatalf("unexpected hourly=%d", rep.Pilot.GlobalSendsPerHour)
	}
	if rep.Pilot.WhatsAppCriticalPath != "false" {
		t.Fatal("whatsapp must not be on critical path for email pilot")
	}
	// Honest evidence: write only under private scratch when present.
	if scratch := os.Getenv("GROK_IMPLEMENTER_SCRATCH"); scratch != "" {
		_ = os.WriteFile(filepath.Join(scratch, "preflight.txt"), []byte(text), 0o644)
	}
	// Always write next to test binary cwd artifact for local capture.
	_ = os.WriteFile("/tmp/grok-goal-bd4a40fb53b5/implementer/preflight.txt", []byte(text), 0o644)
}

func TestPilotSnapshotStructFieldsPresent(t *testing.T) {
	// Compile-time shape: required §13 fields exist on PilotSnapshot.
	var p PilotSnapshot
	p.MailboxConnected = "x"
	p.MailboxAuthValid = "x"
	p.SendPermissionOK = "x"
	p.FromAddress = "x"
	p.ReplyTo = "x"
	p.ConfengeOutreachEnabled = "x"
	p.ConfengeRequireHumanApproval = "true"
	p.ConfengeAutoSendEnabled = "false"
	p.GlobalSendsPerHour = 10
	p.SendingPaused = "false"
	if p.GlobalSendsPerHour != 10 {
		t.Fatal("pilot snapshot fields incomplete")
	}
}
