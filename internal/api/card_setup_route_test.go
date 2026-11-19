package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// Saving a card in the checkout is a billing action: the workspace, manage_billing and a fresh session, like every balance route.
func TestCardSetupIntentRouteIsABillingRoute(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "routes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "POST" {
			return true
		}
		group, ok := sel.X.(*ast.Ident)
		lit, isLit := call.Args[0].(*ast.BasicLit)
		if !ok || group.Name != "subscriptions" || !isLit {
			return true
		}
		if path, _ := strconv.Unquote(lit.Value); path != "/balance/setup-intent" {
			return true
		}
		found = true
		var org, billing, fresh bool
		for _, arg := range call.Args[1:] {
			c, ok := arg.(*ast.CallExpr)
			if !ok {
				continue
			}
			s, ok := c.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			switch s.Sel.Name {
			case "RequireOrganization":
				org = true
			case "RequireFreshAuth":
				fresh = true
			case "RequirePermission":
				if len(c.Args) == 1 {
					if p, ok := c.Args[0].(*ast.SelectorExpr); ok && p.Sel.Name == "PermManageBilling" {
						billing = true
					}
				}
			}
		}
		if !org || !billing || !fresh {
			t.Errorf("POST /subscription/balance/setup-intent gates: organization %v, manage_billing %v, fresh auth %v", org, billing, fresh)
		}
		return true
	})
	if !found {
		t.Fatal("POST /subscription/balance/setup-intent is not registered")
	}
}
