package plathealth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunbookCoversSixClassesAndSections(t *testing.T) {
	raw, err := os.ReadFile(findRunbook(t))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	classes := []string{
		"## Control plane",
		"## DB",
		"## Cache",
		"## NATS",
		"## Email provider",
		"## Contract drift",
	}
	for _, h := range classes {
		if !strings.Contains(doc, h) {
			t.Fatalf("runbook missing class heading %q", h)
		}
	}
	// Each class section must carry the operator blocks before the next class.
	for i, h := range classes {
		start := strings.Index(doc, h)
		end := len(doc)
		if i+1 < len(classes) {
			end = strings.Index(doc, classes[i+1])
		}
		if start < 0 || end <= start {
			t.Fatalf("cannot slice section %s", h)
		}
		section := doc[start:end]
		for _, need := range []string{
			"### Diagnose",
			"### Mitigation",
			"### Rollback",
			"### Evidence capture",
			"### Commands",
			"### Owners",
		} {
			if !strings.Contains(section, need) {
				t.Fatalf("%s missing %s", h, need)
			}
		}
		if !strings.Contains(section, "```") {
			t.Fatalf("%s has no fenced command block", h)
		}
	}
	for _, needle := range []string{
		ClassControlPlane,
		ClassDB,
		ClassCache,
		ClassNATS,
		ClassEmailProvider,
		ClassContractDrift,
		"/live",
		"/ready",
		"/health/deps",
		"opsprobe",
	} {
		if !strings.Contains(doc, needle) {
			t.Fatalf("runbook missing %q", needle)
		}
	}
}

func TestRunbookRegisteredInMeta(t *testing.T) {
	meta, err := os.ReadFile(filepath.Join(findDocsDev(t), "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(meta), "incident-runbook") {
		t.Fatal("docs/content/docs/development/meta.json must list incident-runbook")
	}
}

func findRunbook(t *testing.T) string {
	t.Helper()
	return filepath.Join(findDocsDev(t), "incident-runbook.mdx")
}

func findDocsDev(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("docs", "content", "docs", "development"),
		filepath.Join("..", "..", "..", "docs", "content", "docs", "development"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	t.Fatal("could not find docs/content/docs/development")
	return ""
}
