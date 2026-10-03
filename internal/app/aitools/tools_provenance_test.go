package aitools

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// recordingAudit captures LogAction rows; the rest of AuditService is embedded
// so an accidental call to anything else panics rather than passing quietly.
type recordingAudit struct {
	audit.AuditService
	rows []capturedAuditRow
}

type capturedAuditRow struct {
	orgID    uuid.UUID
	actorID  uuid.UUID
	action   models.AuditAction
	entity   models.AuditEntityType
	metadata map[string]string
}

func (r *recordingAudit) LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ip, userAgent string, changes, metadata map[string]string) {
	r.rows = append(r.rows, capturedAuditRow{orgID: orgID, actorID: actorID, action: action, entity: entityType, metadata: metadata})
}

// TestLogAuditProvenance pins the merge behavior every AI surface's audit rows
// rely on: provenance keys ride along, call-site keys still win, and an
// invocation with no provenance leaves the metadata exactly as before.
func TestLogAuditProvenance(t *testing.T) {
	org, user := uuid.New(), uuid.New()

	tests := []struct {
		name    string
		inv     Invocation
		meta    map[string]string
		want    map[string]string
		wantNil bool
	}{
		{
			name: "dashboard agent approved write carries full provenance",
			inv: Invocation{
				OrgID: org, UserID: user,
				AISurface: models.AISurfaceAgent, AIDecision: models.AIDecisionHumanApproved,
				SessionID: "sess-1", MessageID: "msg-1",
			},
			want: map[string]string{
				models.MetaKeyAISurface:  "agent",
				models.MetaKeyAIDecision: "human_approved",
				models.MetaKeyAISession:  "sess-1",
				models.MetaKeyAIMessage:  "msg-1",
			},
		},
		{
			name: "mcp permission-bit call carries surface and decision only",
			inv: Invocation{
				OrgID: org, UserID: user, IsAPIKey: true,
				AISurface: models.AISurfaceMCP, AIDecision: models.AIDecisionPermissionBit,
			},
			want: map[string]string{
				models.MetaKeyAISurface:  "mcp",
				models.MetaKeyAIDecision: "permission_bit",
			},
		},
		{
			name:    "no provenance and no meta passes nil through",
			inv:     Invocation{OrgID: org, UserID: user},
			wantNil: true,
		},
		{
			name: "no provenance leaves call-site metadata untouched",
			inv:  Invocation{OrgID: org, UserID: user},
			meta: map[string]string{"source": "inbox_agent"},
			want: map[string]string{"source": "inbox_agent"},
		},
		{
			name: "call-site keys win over provenance on collision",
			inv:  Invocation{OrgID: org, UserID: user, AISurface: models.AISurfaceAgent},
			meta: map[string]string{models.MetaKeyAISurface: "hand-set"},
			want: map[string]string{models.MetaKeyAISurface: "hand-set"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recordingAudit{}
			Deps{Audit: rec}.logAudit(context.Background(), tt.inv, models.AuditActionUpdate, models.AuditEntityUnibox, nil, tt.meta)
			if len(rec.rows) != 1 {
				t.Fatalf("expected exactly one audit row, got %d", len(rec.rows))
			}
			row := rec.rows[0]
			if row.orgID != org || row.actorID != user {
				t.Fatalf("org/actor not forwarded: org=%s actor=%s", row.orgID, row.actorID)
			}
			if tt.wantNil {
				if row.metadata != nil {
					t.Fatalf("expected nil metadata, got %v", row.metadata)
				}
				return
			}
			if row.metadata == nil && len(tt.want) > 0 {
				t.Fatalf("expected metadata %v, got nil", tt.want)
			}
			for k, v := range tt.want {
				if row.metadata[k] != v {
					t.Fatalf("metadata[%s] = %q, want %q", k, row.metadata[k], v)
				}
			}
			// A surface always implies a decision and vice versa on stamped calls.
			if (row.metadata[models.MetaKeyAISurface] == "") != (row.metadata[models.MetaKeyAIDecision] == "") {
				if tt.inv.AISurface != "" && tt.inv.AIDecision != "" {
					t.Fatalf("provenance partially merged: %v", row.metadata)
				}
			}
		})
	}
}

// riskLiterals maps the risk-class identifier names as they appear in tool
// literals (generation.RiskSend) onto the real constants: the AST only sees
// the identifier, not the constant's underlying value.
var riskLiterals = map[string]generation.RiskClass{
	"RiskRead":  generation.RiskRead,
	"RiskWrite": generation.RiskWrite,
	"RiskSend":  generation.RiskSend,
}

// TestWriteToolHandlersAudit walks the package source and asserts every tool
// registered with a risk above read fires an audit entry from its handler,
// either through Deps.logAudit or (the placement tools, registered outside
// BuildRegistry) through their own audit service. A write or send tool whose
// handler skips the audit fails here instead of shipping unattributable.
func TestWriteToolHandlersAudit(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !fi.IsDir() && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	var pkg *ast.Package
	for _, p := range pkgs {
		pkg = p
	}
	if pkg == nil {
		t.Fatal("no package found")
	}

	// auditFns holds the names of functions whose body fires an audit entry
	// (directly or through an audited helper); fnCalls records each function's
	// calls so the audit property can propagate across delegation.
	auditFns := map[string]bool{}
	fnCalls := map[string][]string{}
	// tools maps tool name -> risk class; handlers maps tool name -> handler
	// selector name.
	risk := map[string]generation.RiskClass{}
	handler := map[string]string{}

	for _, file := range pkg.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if !isToolLiteral(lit) {
				return true
			}
			var name string
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch key.Name {
				case "Name":
					if s, ok := kv.Value.(*ast.BasicLit); ok {
						name = strings.Trim(s.Value, `"`)
					}
				case "Risk":
					sel, ok := kv.Value.(*ast.SelectorExpr)
					if !ok {
						t.Errorf("tool %q: Risk is not a generation.Risk* selector", name)
						continue
					}
					rc, ok := riskLiterals[sel.Sel.Name]
					if !ok {
						t.Errorf("tool %q: unknown risk class %q — add it to riskLiterals", name, sel.Sel.Name)
						continue
					}
					risk[name] = rc
				case "Handler":
					if sel, ok := kv.Value.(*ast.SelectorExpr); ok {
						handler[name] = sel.Sel.Name
					}
				}
			}
			return true
		})

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			fires := false
			// calls collects helper methods this function invokes, so the
			// audit check below can follow one delegation hop (addTag ->
			// tagOp -> logAudit).
			var calls []string
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name == "logAudit" {
					fires = true
					return false
				}
				if sel.Sel.Name == "LogAction" {
					if x, ok := sel.X.(*ast.SelectorExpr); ok && x.Sel.Name == "audit" {
						fires = true
						return false
					}
				}
				calls = append(calls, sel.Sel.Name)
				return true
			})
			if fires {
				auditFns[fn.Name.Name] = true
			}
			// Store every function's calls even when it does not fire itself,
			// so the propagation below can reach it through its callees.
			fnCalls[fn.Name.Name] = calls
		}
	}

	// Follow helper calls transitively (bounded passes: delegation in this
	// package is at most one or two hops) so a shared audit helper satisfies
	// every tool that routes through it.
	for pass := 0; pass < 3; pass++ {
		grew := false
		for name, calls := range fnCalls {
			if auditFns[name] {
				continue
			}
			for _, callee := range calls {
				if auditFns[callee] {
					auditFns[name] = true
					grew = true
					break
				}
			}
		}
		if !grew {
			break
		}
	}

	if len(risk) < 60 {
		t.Fatalf("expected the full registry in source, found only %d tools", len(risk))
	}

	// auditExempt names write-class tools whose handler can never mutate
	// anything, with the reason. If the handler ever starts doing work, it
	// needs a real audit call instead of an entry here.
	auditExempt := map[string]string{
		"create_api_key": "the handler unconditionally refuses; key creation requires the HTTP route's fresh-auth gate",
	}

	// The send-class set is an authority invariant: exactly these tools can
	// transmit, and every other surface (MCP, REST) refuses them.
	sendWant := map[string]bool{
		"send_reply": true, "compose_email": true,
		"run_placement_test": true, "run_placement_batch": true,
	}
	sendGot := map[string]bool{}
	for name, r := range risk {
		if r == generation.RiskSend {
			sendGot[name] = true
		}
		if r == generation.RiskRead {
			continue
		}
		h := handler[name]
		if h == "" {
			t.Errorf("tool %q has risk %s but no named handler", name, r)
			continue
		}
		if !auditFns[h] {
			if _, ok := auditExempt[name]; ok {
				continue
			}
			t.Errorf("tool %q (risk %s, handler %s) never fires an audit entry", name, r, h)
		}
	}
	for name := range sendWant {
		if !sendGot[name] {
			t.Errorf("expected %q to be send-class", name)
		}
	}
	for name := range sendGot {
		if !sendWant[name] {
			t.Errorf("unexpected send-class tool %q: update the send-set invariant if this is intended", name)
		}
	}
}

// isToolLiteral reports whether the composite literal declares a Tool (in or
// out of package: Tool{...} and aitools.Tool{...}).
func isToolLiteral(lit *ast.CompositeLit) bool {
	switch t := lit.Type.(type) {
	case *ast.Ident:
		return t.Name == "Tool"
	case *ast.SelectorExpr:
		return t.Sel.Name == "Tool"
	}
	return false
}
