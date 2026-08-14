package confenge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jobBlock returns the YAML body of a top-level job key in a GitHub Actions workflow.
func jobBlock(src, jobKey string) string {
	marker := "\n  " + jobKey + ":\n"
	idx := strings.Index(src, marker)
	if idx < 0 {
		// file may start with the job
		prefix := "  " + jobKey + ":\n"
		if strings.HasPrefix(src, prefix) {
			idx = 0
			marker = prefix
		} else {
			return ""
		}
	}
	start := idx + 1 // keep leading spaces of job key line via re-slice from idx+1? use idx+len("\n")
	if idx == 0 {
		start = 0
	} else {
		start = idx + 1
	}
	rest := src[start:]
	// Find next top-level job: line starting with exactly two spaces + key + colon,
	// after the first line of this job.
	lines := strings.Split(rest, "\n")
	var b strings.Builder
	b.WriteString(lines[0])
	b.WriteByte('\n')
	for _, line := range lines[1:] {
		if len(line) >= 3 && line[0] == ' ' && line[1] == ' ' && line[2] != ' ' && strings.Contains(line, ":") {
			// next job key
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestConfengeProductAcceptanceIsHardCIGate prevents reintroducing continue-on-error
// on the CONFENGE product acceptance job and ensures CI Status fails the workflow
// when that job fails, cancels, or times out.
func TestConfengeProductAcceptanceIsHardCIGate(t *testing.T) {
	root := findRepoRoot(t)
	ciPath := filepath.Join(root, ".github", "workflows", "ci.yml")
	raw, err := os.ReadFile(ciPath)
	if err != nil {
		t.Fatalf("CI workflow missing at %s: %v", ciPath, err)
	}
	src := string(raw)

	job := jobBlock(src, "confenge-product-acceptance")
	if job == "" {
		t.Fatal("confenge-product-acceptance job missing from .github/workflows/ci.yml")
	}
	if strings.Contains(job, "continue-on-error:") {
		// Any continue-on-error under this job reopens the false-green path.
		t.Error("confenge-product-acceptance must not set continue-on-error (Playwright FAIL must fail CI)")
	}
	if !strings.Contains(job, "name: CONFENGE product acceptance") {
		t.Error("CONFENGE product acceptance job name missing")
	}
	if !strings.Contains(job, "pnpm test:e2e:confenge:live") {
		t.Error("CONFENGE product acceptance must run pnpm test:e2e:confenge:live")
	}

	status := jobBlock(src, "ci-status")
	if status == "" {
		t.Fatal("ci-status job missing from .github/workflows/ci.yml")
	}
	if !strings.Contains(status, "confenge-product-acceptance") {
		t.Error("CI Status must list confenge-product-acceptance in needs")
	}
	for _, needle := range []string{
		`needs.confenge-product-acceptance.result`,
		`"failure"`,
		`"cancelled"`,
		`"timed_out"`,
	} {
		if !strings.Contains(status, needle) {
			t.Errorf("CI Status must treat confenge-product-acceptance %s as workflow failure", needle)
		}
	}
}
