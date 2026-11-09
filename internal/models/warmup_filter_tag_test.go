package models

import (
	"regexp"
	"strings"
	"testing"
)

func TestNewWarmupFilterTagIsAValidUnambiguousCode(t *testing.T) {
	shape := regexp.MustCompile(`^[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}$`)
	seen := map[string]bool{}
	for range 200 {
		tag := NewWarmupFilterTag()
		if !shape.MatchString(tag) || strings.ContainsAny(tag, "ilo01") {
			t.Fatalf("generated tag %q is not a clean code", tag)
		}
		if cleaned, err := CleanWarmupFilterTag(tag); err != nil || cleaned != tag {
			t.Fatalf("a generated tag fails its own check: %q, %v", cleaned, err)
		}
		if seen[tag] {
			t.Fatalf("tag %q repeated within 200 draws", tag)
		}
		seen[tag] = true
	}
}

func TestCleanWarmupFilterTag(t *testing.T) {
	if got, err := CleanWarmupFilterTag("  acme \n  inbox_77 "); err != nil || got != "acme inbox_77" {
		t.Fatalf("CleanWarmupFilterTag = %q, %v", got, err)
	}
	for _, bad := range []string{"abc", strings.Repeat("a", WarmupFilterTagMaxLength+1), "tag<b>bold", "tag;line", "naïve-tag"} {
		if _, err := CleanWarmupFilterTag(bad); err == nil {
			t.Errorf("CleanWarmupFilterTag(%q) accepted", bad)
		}
	}
}

func TestAppendWarmupFilterTag(t *testing.T) {
	plain, html := "Thanks for the notes.\n\nAnna\n\n", "<html><body><p>Thanks</p></BODY></html>"

	// No tag is the default: the bodies leave exactly as they came.
	if p, h := AppendWarmupFilterTag(plain, html, ""); p != plain || h != html {
		t.Fatal("an empty tag changed the mail")
	}
	p, h := AppendWarmupFilterTag(plain, html, "k7q2-mx94-ab3d")
	if p != "Thanks for the notes.\n\nAnna\n\nk7q2-mx94-ab3d" {
		t.Errorf("plain = %q", p)
	}
	if h != "<html><body><p>Thanks</p><p>k7q2-mx94-ab3d</p></BODY></html>" {
		t.Errorf("html = %q", h)
	}
	if _, h := AppendWarmupFilterTag(plain, "", "k7q2-mx94-ab3d"); h != "" {
		t.Error("an absent HTML part was created")
	}
	if _, h := AppendWarmupFilterTag("", "<p>Hi</p>", "k7q2-mx94-ab3d"); h != "<p>Hi</p><p>k7q2-mx94-ab3d</p>" {
		t.Errorf("html without a body element = %q", h)
	}
	// İ and the Kelvin sign shrink when lower-cased; the tag still lands right before </body>.
	if _, h := AppendWarmupFilterTag("", "<body>İstanbul 300K</body>", "k7q2-mx94-ab3d"); h != "<body>İstanbul 300K<p>k7q2-mx94-ab3d</p></body>" {
		t.Errorf("html with case-changing characters = %q", h)
	}
}
