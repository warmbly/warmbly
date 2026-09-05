package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/cli/iostreams"
)

func testPrinter() (*Printer, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	io := iostreams.System()
	io.Out = buf
	io.SetColor(false)
	return &Printer{IO: io}, buf
}

func TestDigWalksObjectsAndArrays(t *testing.T) {
	doc := map[string]any{
		"data": []any{
			map[string]any{"name": "first", "counts": map[string]any{"sent": 4.0}},
		},
	}
	if got := dig(doc, "data.0.name"); got != "first" {
		t.Errorf("dig name = %v", got)
	}
	if got := dig(doc, "data.0.counts.sent"); got != 4.0 {
		t.Errorf("dig nested = %v", got)
	}
	if got := dig(doc, "data.9.name"); got != nil {
		t.Errorf("out of range should be nil, got %v", got)
	}
	if got := dig(doc, "missing.key"); got != nil {
		t.Errorf("missing path should be nil, got %v", got)
	}
}

func TestTableRendersRowsAndHeaders(t *testing.T) {
	p, buf := testPrinter()
	// A table is only rendered for a terminal; force it directly instead.
	err := p.renderTable([]byte(`{"data":[{"id":"1","name":"Alpha","status":"active"},{"id":"2","name":"Beta","status":"draft"}]}`),
		Table{Root: "data", Columns: []Column{{Header: "ID", Path: "id"}, {Header: "NAME", Path: "name"}, {Header: "STATUS", Path: "status"}}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"ID", "NAME", "STATUS", "Alpha", "Beta", "draft"} {
		if !strings.Contains(out, want) {
			t.Errorf("table is missing %q:\n%s", want, out)
		}
	}
	if lines := strings.Count(strings.TrimSpace(out), "\n"); lines != 2 {
		t.Errorf("want a header and two rows, got:\n%s", out)
	}
}

func TestEmptyListSaysSomethingUseful(t *testing.T) {
	p, buf := testPrinter()
	if err := p.renderTable([]byte(`{"data":[]}`), Table{Root: "data", Columns: []Column{{Header: "ID", Path: "id"}}, Empty: "No campaigns yet."}); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "No campaigns yet.") {
		t.Errorf("an empty list should explain itself, got %q", buf.String())
	}
}

func TestFieldsNarrowTheTable(t *testing.T) {
	p, buf := testPrinter()
	p.Fields = []string{"name"}
	if err := p.renderTable([]byte(`{"data":[{"id":"1","name":"Alpha"}]}`),
		Table{Root: "data", Columns: []Column{{Header: "ID", Path: "id"}, {Header: "NAME", Path: "name"}}}); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "ID") {
		t.Errorf("--fields name should drop the ID column:\n%s", out)
	}
	if !strings.Contains(out, "Alpha") {
		t.Errorf("--fields name dropped the row:\n%s", out)
	}
}

func TestJSONIsPassedThroughWhenNotAnObject(t *testing.T) {
	p, buf := testPrinter()
	if err := p.renderJSON([]byte("not json at all")); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "not json at all") {
		t.Errorf("a non-JSON body must pass through untouched, got %q", buf.String())
	}
}

func TestTemplateRendering(t *testing.T) {
	p, buf := testPrinter()
	p.Template = "{{range .data}}{{.name}} {{end}}"
	if err := p.Print([]byte(`{"data":[{"name":"a"},{"name":"b"}]}`), Table{}); err != nil {
		t.Fatalf("template: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "a b" {
		t.Errorf("template output = %q", buf.String())
	}
}

func TestStringifyKeepsCellsReadable(t *testing.T) {
	if got := stringify(nil); got != "-" {
		t.Errorf("nil = %q, want -", got)
	}
	if got := stringify([]any{"a@x.com", "b@x.com"}); got != "a@x.com,b@x.com" {
		t.Errorf("list = %q", got)
	}
	if got := stringify(map[string]any{"id": "x", "name": "Jane"}); got != "Jane" {
		t.Errorf("object cell = %q, want the name", got)
	}
	if got := stringify(4.0); got != "4" {
		t.Errorf("whole float = %q, want 4", got)
	}
}
