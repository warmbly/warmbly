package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── Base template (shared chrome) ───────────────────────────────

func TestBaseTemplate_Structure(t *testing.T) {
	html, err := GenerateLoginCodeHTML("000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []string{
		"<!DOCTYPE html>",
		"<html lang=\"en\">",
		// Inline SVG logo, dashboard slate fill.
		"<svg",
		"M222.805 644.772",
		"fill=\"#0f172a\"",
		// Brae chrome: cream wrapper + white card + hairline border.
		"#f5f6f8",
		"#ffffff",
		"#e2e8f0",
		"border-radius:8px",
	}

	for _, s := range checks {
		if !strings.Contains(html, s) {
			t.Errorf("base template: expected HTML to contain %q", s)
		}
	}
}

func TestBaseTemplate_BusinessDetails(t *testing.T) {
	html, err := GenerateLoginCodeHTML("000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []string{
		// Branding
		CompanyName,
		"warmbly.com",
		"Privacy",
		"Terms",
		TermsURL,
		PrivacyURL,
		// Companies Act 2006 required details
		LegalEntity,
		CompanyNumber,
		PlaceOfReg,
		RegisteredAddr,
	}

	for _, s := range checks {
		if !strings.Contains(html, s) {
			t.Errorf("footer: expected HTML to contain %q", s)
		}
	}
}

// ─── Login Code ──────────────────────────────────────────────────

func TestGenerateLoginCodeHTML(t *testing.T) {
	code := "123456"
	html, err := GenerateLoginCodeHTML(code)
	if err != nil {
		t.Fatalf("GenerateLoginCodeHTML returned error: %v", err)
	}

	if html == "" {
		t.Fatal("GenerateLoginCodeHTML returned empty string")
	}

	checks := []struct {
		name   string
		substr string
	}{
		{"contains the verification code", code},
		{"code monospace tracking", "letter-spacing:6px"},
		{"heading", "Your login code"},
		{"sign-in helper", "finish logging in"},
		{"expiry notice", "Expires in 15 minutes"},
		{"safety notice", "safely ignore"},
		{"title tag", "<title>Your Login Code</title>"},
	}

	for _, c := range checks {
		if !strings.Contains(html, c.substr) {
			t.Errorf("%s: expected HTML to contain %q", c.name, c.substr)
		}
	}
}

func TestGenerateLoginCodeHTML_DifferentCodes(t *testing.T) {
	codes := []string{"000000", "999999", "ABC123", "1"}
	for _, code := range codes {
		html, err := GenerateLoginCodeHTML(code)
		if err != nil {
			t.Fatalf("GenerateLoginCodeHTML(%q) returned error: %v", code, err)
		}
		if !strings.Contains(html, code) {
			t.Errorf("GenerateLoginCodeHTML(%q): code not found in output", code)
		}
	}
}

// ─── Registration Code ──────────────────────────────────────────

func TestGenerateRegistrationCodeHTML(t *testing.T) {
	code := "789012"
	html, err := GenerateRegistrationCodeHTML(code)
	if err != nil {
		t.Fatalf("GenerateRegistrationCodeHTML returned error: %v", err)
	}

	if html == "" {
		t.Fatal("GenerateRegistrationCodeHTML returned empty string")
	}

	checks := []struct {
		name   string
		substr string
	}{
		{"contains the verification code", code},
		{"welcome heading", "Welcome to Warmbly"},
		{"registration prompt", "finish creating your account"},
		{"expiry notice", "Expires in 15 minutes"},
		{"title tag", "<title>Your Verification Code</title>"},
	}

	for _, c := range checks {
		if !strings.Contains(html, c.substr) {
			t.Errorf("%s: expected HTML to contain %q", c.name, c.substr)
		}
	}
}

func TestGenerateRegistrationCodeHTML_DifferentCodes(t *testing.T) {
	codes := []string{"000000", "999999", "ABC123", "1"}
	for _, code := range codes {
		html, err := GenerateRegistrationCodeHTML(code)
		if err != nil {
			t.Fatalf("GenerateRegistrationCodeHTML(%q) returned error: %v", code, err)
		}
		if !strings.Contains(html, code) {
			t.Errorf("GenerateRegistrationCodeHTML(%q): code not found in output", code)
		}
	}
}

// ─── Reset Password ─────────────────────────────────────────────

func TestGenerateResetPasswordHTML(t *testing.T) {
	url := "https://app.warmbly.com/reset?token=abc123def456"
	html, err := GenerateResetPasswordHTML("", url)
	if err != nil {
		t.Fatalf("GenerateResetPasswordHTML returned error: %v", err)
	}

	if html == "" {
		t.Fatal("GenerateResetPasswordHTML returned empty string")
	}

	checks := []struct {
		name   string
		substr string
	}{
		{"reset URL in button href", url},
		{"button text", "Reset password</a>"},
		{"reset prompt", "Reset your password"},
		{"ignore notice", "safely ignore this email"},
		{"expiry notice", "expires in 4 hours"},
		{"title tag", "<title>Reset Your Password</title>"},
	}

	for _, c := range checks {
		if !strings.Contains(html, c.substr) {
			t.Errorf("%s: expected HTML to contain %q", c.name, c.substr)
		}
	}

	// The reset URL should appear at least twice (button href + plaintext link)
	count := strings.Count(html, url)
	if count < 2 {
		t.Errorf("expected reset URL to appear at least 2 times, got %d", count)
	}
}

func TestGenerateResetPasswordHTML_URLEncoding(t *testing.T) {
	url := "https://app.warmbly.com/reset?token=abc&user=test@example.com"
	html, err := GenerateResetPasswordHTML("", url)
	if err != nil {
		t.Fatalf("GenerateResetPasswordHTML returned error: %v", err)
	}
	if !strings.Contains(html, "app.warmbly.com/reset") {
		t.Error("expected HTML to contain the reset URL domain")
	}
}

// ─── Cross-template checks ──────────────────────────────────────

func TestTemplatesProduceDistinctOutput(t *testing.T) {
	loginHTML, err := GenerateLoginCodeHTML("111111")
	if err != nil {
		t.Fatal(err)
	}

	regHTML, err := GenerateRegistrationCodeHTML("111111")
	if err != nil {
		t.Fatal(err)
	}

	resetHTML, err := GenerateResetPasswordHTML("", "https://example.com/reset")
	if err != nil {
		t.Fatal(err)
	}

	if loginHTML == regHTML {
		t.Error("login and registration templates should produce different output")
	}
	if loginHTML == resetHTML {
		t.Error("login and reset templates should produce different output")
	}
	if regHTML == resetHTML {
		t.Error("registration and reset templates should produce different output")
	}
}

func TestLoginCodeDoesNotContainResetContent(t *testing.T) {
	html, _ := GenerateLoginCodeHTML("123456")
	if strings.Contains(html, "Reset Password</a>") {
		t.Error("login code template should not contain reset password button")
	}
	if strings.Contains(html, "reset your password") {
		t.Error("login code template should not contain reset password text")
	}
}

func TestResetPasswordDoesNotContainCodeBlock(t *testing.T) {
	html, _ := GenerateResetPasswordHTML("", "https://example.com/reset")
	if strings.Contains(html, "letter-spacing:8px") {
		t.Error("reset password template should not contain a code display block")
	}
	if strings.Contains(html, "verify your login") {
		t.Error("reset password template should not contain login verify text")
	}
}

// ─── Constants ──────────────────────────────────────────────────

// ─── Preview — writes HTML files to a temp dir and prints paths ──

func TestPreview(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "warmbly-email-preview")
	os.MkdirAll(dir, 0755)

	templates := []struct {
		name string
		gen  func() (string, error)
	}{
		{"login-code.html", func() (string, error) { return GenerateLoginCodeHTML("123456") }},
		{"registration-code.html", func() (string, error) { return GenerateRegistrationCodeHTML("789012") }},
		{"reset-password.html", func() (string, error) {
			return GenerateResetPasswordHTML("", "https://app.warmbly.com/reset?token=abc123def456")
		}},
	}

	for _, tmpl := range templates {
		html, err := tmpl.gen()
		if err != nil {
			t.Fatalf("%s: %v", tmpl.name, err)
		}
		path := filepath.Join(dir, tmpl.name)
		if err := os.WriteFile(path, []byte(html), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("Preview: %s", path)
	}

	t.Logf("Open all: open %s/*.html", dir)
}

// ─── Constants ──────────────────────────────────────────────────

func TestBusinessConstants(t *testing.T) {
	if CompanyName == "" {
		t.Error("CompanyName should not be empty")
	}
	if LegalEntity == "" {
		t.Error("LegalEntity should not be empty")
	}
	if CompanyNumber == "" {
		t.Error("CompanyNumber should not be empty")
	}
	if PlaceOfReg == "" {
		t.Error("PlaceOfReg should not be empty")
	}
	if RegisteredAddr == "" {
		t.Error("RegisteredAddr should not be empty")
	}
	if WebsiteURL == "" {
		t.Error("WebsiteURL should not be empty")
	}
	if !strings.HasPrefix(WebsiteURL, "https://") {
		t.Error("WebsiteURL should start with https://")
	}
	if !strings.HasPrefix(TermsURL, "https://") {
		t.Error("TermsURL should start with https://")
	}
	if !strings.HasPrefix(PrivacyURL, "https://") {
		t.Error("PrivacyURL should start with https://")
	}
}
