package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// Each of these changes who can get in or moves money or ownership, so its registration carries RequireFreshAuth.
func TestRoutesThatNeedAFreshConfirmation(t *testing.T) {
	want := map[string]bool{
		"org DELETE /members/:id":                     false,
		"org POST /roles":                             false,
		"org PATCH /roles/:id":                        false,
		"org DELETE /roles/:id":                       false,
		"subscriptions POST /checkout":                false,
		"subscriptions POST /portal":                  false,
		"subscriptions POST /cancel":                  false,
		"subscriptions POST /change-plan":             false,
		"subscriptions POST /offers/:planId/checkout": false,
		"subscriptions POST /credits/checkout":        false,
		"subscriptions PATCH /credits/settings":       false,
		"poolLink POST /checkout":                     false,
		"apiKeys POST ":                               false,
		"apiKeys PATCH /:id":                          false,
		"webhooks POST ":                              false,
		"webhooks PATCH /:id":                         false,
		"webhooks POST /:id/rotate-secret":            false,
		"org POST /members/invite":                    false,
		"org POST /transfer-ownership":                false,
		"org POST /current/danger-zone/delete":        false,
		"adminRoutes POST /testers/:id/password":      false,
		"protectedAuth POST /passkey/register/finish": false,
	}
	file, err := parser.ParseFile(token.NewFileSet(), "routes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		group, ok := sel.X.(*ast.Ident)
		lit, isLit := call.Args[0].(*ast.BasicLit)
		if !ok || !isLit || lit.Kind != token.STRING {
			return true
		}
		path, _ := strconv.Unquote(lit.Value)
		key := group.Name + " " + sel.Sel.Name + " " + path
		if _, tracked := want[key]; !tracked {
			return true
		}
		for _, arg := range call.Args[1:] {
			if c, ok := arg.(*ast.CallExpr); ok {
				if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "RequireFreshAuth" {
					want[key] = true
				}
			}
		}
		return true
	})
	for key, gated := range want {
		if !gated {
			t.Errorf("%s is registered without RequireFreshAuth", key)
		}
	}
}
