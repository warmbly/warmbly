package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// storeGate is what one store route asks of its caller.
type storeGate struct {
	scope   string // READ_STORE or MANAGE_STORE
	billing bool   // the member also needs manage_billing
	fresh   bool   // a session also needs a recent confirmation
}

// Every /v1/store route takes an explicit store scope for keys and OAuth apps,
// the matching member permission for everyone, and manage_billing plus a fresh
// session wherever money moves. A route missing from this table fails too.
func TestStoreRoutesCarryTheirScopes(t *testing.T) {
	r, m, b := storeGate{scope: "READ_STORE"}, storeGate{scope: "MANAGE_STORE"}, storeGate{scope: "MANAGE_STORE", billing: true, fresh: true}
	want := map[string]storeGate{
		"GET /status":                             r,
		"GET /domains/search":                     r,
		"PUT /registrant":                         {scope: "MANAGE_STORE", billing: true},
		"POST /quotes":                            m,
		"POST /orders":                            b,
		"GET /orders":                             r,
		"GET /orders/:id":                         r,
		"GET /domains":                            r,
		"GET /domains/:id":                        r,
		"PATCH /domains/:id":                      m,
		"POST /domains/:id/renew":                 b,
		"POST /domains/:id/transfer-out":          b,
		"POST /domains/connect":                   m,
		"POST /domains/:id/connection/check":      m,
		"POST /domains/:id/connection/regenerate": m,
		"POST /domains/:id/microsoft-consent":     m,
		"DELETE /domains/:id":                     m,
		"POST /domains/:id/dns":                   m,
		"PATCH /domains/:id/dns/:record_id":       m,
		"DELETE /domains/:id/dns/:record_id":      m,
		"POST /domains/:id/dns/check":             r,
		"GET /mailboxes":                          r,
		"PATCH /mailboxes/:id":                    m,
		"PUT /mailboxes/:id/photo":                m,
		"POST /photos":                            m,
		"GET /photos/:id":                         r,
		"DELETE /mailboxes/:id":                   b,
		"POST /mailboxes/:id/resume":              b,
		"POST /mailboxes/:id/sign-in":             {scope: "MANAGE_STORE", fresh: true},
	}
	file, err := parser.ParseFile(token.NewFileSet(), "routes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]storeGate{}
	ast.Inspect(file, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		// The gates a block names, e.g. read := m.RequireAccess(models.PermManageEmails, models.APIPermReadStore).
		local := map[string]*ast.CallExpr{}
		for _, stmt := range block.List {
			if as, ok := stmt.(*ast.AssignStmt); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 {
				if id, ok := as.Lhs[0].(*ast.Ident); ok {
					if call, ok := as.Rhs[0].(*ast.CallExpr); ok {
						local[id.Name] = call
					}
				}
			}
		}
		for _, stmt := range block.List {
			es, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := es.X.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			group, ok := sel.X.(*ast.Ident)
			if !ok || (group.Name != "storeRoutes" && group.Name != "storeSecrets") {
				continue
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				continue
			}
			path, _ := strconv.Unquote(lit.Value)
			var g storeGate
			for _, arg := range call.Args[1:] {
				gate, _ := arg.(*ast.CallExpr)
				if id, ok := arg.(*ast.Ident); ok {
					gate = local[id.Name]
				}
				if gate == nil {
					continue
				}
				name := selName(gate.Fun)
				switch name {
				case "RequireFreshAuth":
					g.fresh = true
				case "RequireAccess":
					if len(gate.Args) != 2 {
						t.Fatalf("%s %s: RequireAccess with %d arguments", sel.Sel.Name, path, len(gate.Args))
					}
					org, api := selName(gate.Args[0]), selName(gate.Args[1])
					switch api {
					case "APIPermReadStore":
						g.scope = maxScope(g.scope, "READ_STORE")
					case "APIPermManageStore":
						g.scope = maxScope(g.scope, "MANAGE_STORE")
					default:
						t.Errorf("%s %s: gated on %s, not a store scope", sel.Sel.Name, path, api)
					}
					if org == "PermManageBilling" {
						g.billing = true
					} else if org != "PermManageEmails" {
						t.Errorf("%s %s: member permission %s", sel.Sel.Name, path, org)
					}
				}
			}
			got[sel.Sel.Name+" "+path] = g
		}
		return true
	})
	var keys []string
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		g, ok := got[k]
		if !ok {
			t.Errorf("%s is not registered", k)
			continue
		}
		if g != want[k] {
			t.Errorf("%s gates = %+v, want %+v", k, g, want[k])
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("%s is registered but not in this table; give it a store scope here", k)
		}
	}
}

func selName(e ast.Expr) string {
	if s, ok := e.(*ast.SelectorExpr); ok {
		return s.Sel.Name
	}
	return ""
}

func maxScope(a, b string) string {
	if strings.HasPrefix(a, "MANAGE") {
		return a
	}
	return b
}
