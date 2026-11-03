package models

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// emitCalls are the functions that hand an event to webhook delivery. A new
// emit helper has to be added here for its call sites to count.
var emitCalls = map[string]bool{
	"Dispatch":          true,
	"emit":              true,
	"EmitCampaignEvent": true,
	"emitMailboxEvent":  true,
	"announce":          true,
}

// TestCatalogEventsAreEmitted holds the catalog to the code: every event a
// customer can pick has a place that delivers it, and every deprecated one has none.
func TestCatalogEventsAreEmitted(t *testing.T) {
	names := webhookEventConstants(t)
	emitted := map[WebhookEventType][]string{}
	fset := token.NewFileSet()
	for _, root := range []string{"..", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			inModels := file.Name.Name == "models"
			record := func(e ast.Expr) {
				if v, ok := eventConstant(e, inModels, names); ok {
					emitted[v] = append(emitted[v], fset.Position(e.Pos()).String())
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					if emitCalls[calleeName(n.Fun)] {
						for _, a := range n.Args {
							record(a)
						}
					}
				case *ast.ReturnStmt:
					for _, r := range n.Results {
						record(r)
					}
				case *ast.AssignStmt:
					for _, r := range n.Rhs {
						record(r)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(emitted) == 0 {
		t.Fatal("found no emit sites; the scan is broken")
	}
	for _, d := range WebhookEventCatalog {
		sites := emitted[d.Type]
		switch {
		case d.Deprecated && len(sites) > 0:
			t.Errorf("%s is deprecated but emitted at %v; drop it from deprecatedEvents", d.Type, sites)
		case !d.Deprecated && len(sites) == 0:
			t.Errorf("%s is in the catalog but nothing emits it; emit it through one of %v or mark it deprecated", d.Type, emitCallNames())
		}
	}
}

func TestCatalogListsEveryOutboundEvent(t *testing.T) {
	inCatalog := map[WebhookEventType]bool{}
	for _, d := range WebhookEventCatalog {
		inCatalog[d.Type] = true
	}
	for _, e := range AllWebhookEventTypes {
		if e != WebhookEventInboundWebhook && !inCatalog[e] {
			t.Errorf("%s is a valid event type but missing from WebhookEventCatalog", e)
		}
	}
	for e := range deprecatedEvents {
		if !inCatalog[e] {
			t.Errorf("deprecated event %s is not in the catalog", e)
		}
	}
}

func TestDeprecatedEventsStayValidButLeaveTheWildcard(t *testing.T) {
	all := AppSubscribedEventTypes(nil, ^uint64(0))
	for e := range deprecatedEvents {
		if !IsValidWebhookEventType(string(e)) {
			t.Errorf("deprecated %s must stay valid so saved subscriptions keep saving", e)
		}
		for _, got := range all {
			if got == string(e) {
				t.Errorf("an empty filter materialized deprecated %s", e)
			}
		}
	}
}

// webhookEventConstants maps each WebhookEvent* constant name to its value.
func webhookEventConstants(t *testing.T) map[string]WebhookEventType {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "webhook.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]WebhookEventType{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || !strings.HasPrefix(name.Name, "WebhookEvent") {
					continue
				}
				v, _ := strconv.Unquote(lit.Value)
				out[name.Name] = WebhookEventType(v)
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no WebhookEvent constants found in webhook.go")
	}
	return out
}

func eventConstant(e ast.Expr, inModels bool, names map[string]WebhookEventType) (WebhookEventType, bool) {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		if pkg, ok := x.X.(*ast.Ident); ok && pkg.Name == "models" {
			v, ok := names[x.Sel.Name]
			return v, ok
		}
	case *ast.Ident:
		if inModels {
			v, ok := names[x.Name]
			return v, ok
		}
	}
	return "", false
}

func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func emitCallNames() []string {
	out := make([]string, 0, len(emitCalls))
	for name := range emitCalls {
		out = append(out, name)
	}
	return out
}
