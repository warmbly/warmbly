package signuprisk

import "testing"

func TestNormalizeEmailCanonicalizesGmailAliases(t *testing.T) {
	got := NormalizeEmail("User.Name+trial@googlemail.com")
	if got != "username@gmail.com" {
		t.Fatalf("NormalizeEmail() = %q", got)
	}
}

func TestDisposableDomain(t *testing.T) {
	if !IsDisposableDomain("mailinator.com") {
		t.Fatal("expected known disposable domain")
	}
	if IsDisposableDomain("example.com") {
		t.Fatal("ordinary domain classified as disposable")
	}
}
