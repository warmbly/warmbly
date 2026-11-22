package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// An account a partner signs in gets in only through the partner, so every route that adds a way in refuses it.
func TestRoutesThatAddASignInMethodRefusePartnerAccounts(t *testing.T) {
	want := map[string]bool{
		"protectedAuth POST /me/password":             false,
		"protectedAuth POST /me/password/link":        false,
		"protectedAuth POST /passkey/register/begin":  false,
		"protectedAuth POST /passkey/register/finish": false,
		"protectedAuth POST /2fa/enroll/start":        false,
		"protectedAuth POST /2fa/enroll/confirm":      false,
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
				if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "RefusePartnerBound" {
					want[key] = true
				}
			}
		}
		return true
	})
	for key, gated := range want {
		if !gated {
			t.Errorf("%s is registered without RefusePartnerBound", key)
		}
	}
}
