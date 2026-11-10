package tmplvars

import (
	"maps"
	"sort"
	"strings"
	"text/template/parse"
)

// BlankGaps reports the built-in contact fields src prints with nothing to
// fall back on, so an empty value leaves a hole: outside an {{if}} or {{with}}
// that tests the field, and not through default or or. ok is false when src
// does not parse.
func BlankGaps(src string) (gaps map[string]bool, ok bool) {
	if !strings.Contains(src, "{{") {
		return map[string]bool{}, true
	}
	norm, err := Normalize(maskMarkers(src))
	if err != nil {
		return nil, false
	}
	tree := parse.New("gaps")
	tree.Mode = parse.SkipFuncCheck
	trees := map[string]*parse.Tree{}
	if _, err := tree.Parse(norm, "", "", trees); err != nil {
		return nil, false
	}
	g := gapWalker{gaps: map[string]bool{}}
	names := make([]string, 0, len(trees))
	for name := range trees {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if t := trees[name]; t != nil && t.Root != nil {
			root := operand{root: true}
			g.node(t.Root, &frame{dot: root, vars: map[string]operand{"$": root}, safe: map[string]bool{}})
		}
	}
	return g.gaps, true
}

// operand is what a value holds as far as the gap walk can tell: the template
// data itself, the bare text of one built-in field, or something else.
type operand struct {
	root  bool
	field string
}

// frame is the walk's state at one point: what dot and each variable hold,
// and which fields are known not to be empty there.
type frame struct {
	dot  operand
	vars map[string]operand
	safe map[string]bool
}

func (f *frame) with(dot operand, safe map[string]bool) *frame {
	merged := maps.Clone(f.safe)
	for k := range safe {
		merged[k] = true
	}
	return &frame{dot: dot, vars: maps.Clone(f.vars), safe: merged}
}

type gapWalker struct{ gaps map[string]bool }

// passThrough are the functions whose output is empty when their input is, so
// printing one of them over a blank field still leaves a hole.
var passThrough = map[string]bool{
	"lower": true, "upper": true, "trim": true, "title": true, "and": true,
	"print": true, "printf": true, "println": true, "html": true, "js": true, "urlquery": true,
}

func (g *gapWalker) node(n parse.Node, f *frame) {
	switch x := n.(type) {
	case *parse.ListNode:
		if x == nil {
			return
		}
		for _, c := range x.Nodes {
			g.node(c, f)
		}
	case *parse.ActionNode:
		if len(x.Pipe.Decl) > 0 {
			g.declare(x.Pipe, f)
			return
		}
		for field := range g.printed(x.Pipe, f) {
			if !f.safe[field] {
				g.gaps[field] = true
			}
		}
	case *parse.IfNode:
		whenTrue, whenFalse := g.cond(x.Pipe, f)
		body := f.with(f.dot, whenTrue)
		g.declare(x.Pipe, body)
		g.node(x.List, body)
		g.node(x.ElseList, f.with(f.dot, whenFalse))
	case *parse.WithNode:
		whenTrue, whenFalse := g.cond(x.Pipe, f)
		body := f.with(g.value(x.Pipe, f), whenTrue)
		g.declare(x.Pipe, body)
		g.node(x.List, body)
		g.node(x.ElseList, f.with(f.dot, whenFalse))
	case *parse.RangeNode:
		body := f.with(operand{}, nil)
		for _, v := range x.Pipe.Decl {
			body.vars[v.Ident[0]] = operand{}
		}
		g.node(x.List, body)
		g.node(x.ElseList, f.with(f.dot, nil))
	}
}

// declare binds the variables a pipeline declares or assigns to its value.
func (g *gapWalker) declare(p *parse.PipeNode, f *frame) {
	if p == nil {
		return
	}
	v := g.value(p, f)
	for _, d := range p.Decl {
		f.vars[d.Ident[0]] = v
	}
}

// value is what a pipeline of one operand holds.
func (g *gapWalker) value(p *parse.PipeNode, f *frame) operand {
	if p == nil || len(p.Cmds) != 1 || len(p.Cmds[0].Args) != 1 {
		return operand{}
	}
	return g.operandOf(p.Cmds[0].Args[0], f)
}

func (g *gapWalker) operandOf(n parse.Node, f *frame) operand {
	switch x := n.(type) {
	case *parse.DotNode:
		return f.dot
	case *parse.PipeNode:
		return g.value(x, f)
	case *parse.FieldNode:
		if f.dot.root && len(x.Ident) == 1 {
			return builtinField(x.Ident[0])
		}
	case *parse.VariableNode:
		held := f.vars[x.Ident[0]]
		if len(x.Ident) == 1 {
			return held
		}
		if held.root && len(x.Ident) == 2 {
			return builtinField(x.Ident[1])
		}
	}
	return operand{}
}

// builtinField is the operand for a top-level name: a built-in text field, or nothing.
func builtinField(name string) operand {
	canonical, ok := topNames[name]
	if !ok || canonical == SenderField || canonical == UnsubscribeLink {
		return operand{}
	}
	return operand{field: canonical}
}

// printed is the set of fields whose emptiness reaches the pipeline's output.
func (g *gapWalker) printed(p *parse.PipeNode, f *frame) map[string]bool {
	var cur map[string]bool
	for _, cmd := range p.Cmds {
		cur = g.command(cmd, cur, f)
	}
	return cur
}

func (g *gapWalker) command(cmd *parse.CommandNode, piped map[string]bool, f *frame) map[string]bool {
	if len(cmd.Args) == 0 {
		return nil
	}
	head, isFunc := cmd.Args[0].(*parse.IdentifierNode)
	if !isFunc {
		if p, ok := cmd.Args[0].(*parse.PipeNode); ok && len(cmd.Args) == 1 {
			return g.printed(p, f)
		}
		if v := g.operandOf(cmd.Args[0], f); v.field != "" {
			return map[string]bool{v.field: true}
		}
		return nil
	}
	if !passThrough[head.Ident] {
		return nil // default, or, comparisons and the rest never print a blank field as-is
	}
	out := maps.Clone(piped)
	if out == nil {
		out = map[string]bool{}
	}
	for _, arg := range cmd.Args[1:] {
		if p, ok := arg.(*parse.PipeNode); ok {
			maps.Copy(out, g.printed(p, f))
		} else if v := g.operandOf(arg, f); v.field != "" {
			out[v.field] = true
		}
	}
	return out
}

// cond is the set of fields known not to be empty when the condition holds,
// and when it does not.
func (g *gapWalker) cond(p *parse.PipeNode, f *frame) (whenTrue, whenFalse map[string]bool) {
	if p == nil || len(p.Cmds) == 0 {
		return nil, nil
	}
	cmd := p.Cmds[0]
	if len(p.Cmds) > 1 {
		// {{if .FirstName | trim}}: the head decides whether there is anything to test.
		if v := g.value(&parse.PipeNode{Cmds: []*parse.CommandNode{cmd}}, f); v.field != "" {
			return map[string]bool{v.field: true}, nil
		}
		return nil, nil
	}
	return g.condCmd(cmd, f)
}

func (g *gapWalker) condCmd(cmd *parse.CommandNode, f *frame) (whenTrue, whenFalse map[string]bool) {
	head, isFunc := cmd.Args[0].(*parse.IdentifierNode)
	if !isFunc {
		if len(cmd.Args) == 1 {
			return g.condArg(cmd.Args[0], f)
		}
		return nil, nil
	}
	args := cmd.Args[1:]
	switch head.Ident {
	case "not":
		if len(args) == 1 {
			t, fl := g.condArg(args[0], f)
			return fl, t
		}
	case "and":
		whenTrue = map[string]bool{}
		for _, a := range args {
			t, _ := g.condArg(a, f)
			maps.Copy(whenTrue, t)
		}
		return whenTrue, nil
	case "or":
		whenFalse = map[string]bool{}
		for _, a := range args {
			_, fl := g.condArg(a, f)
			maps.Copy(whenFalse, fl)
		}
		return nil, whenFalse
	case "ne", "eq":
		if len(args) == 2 {
			field := g.comparedToEmpty(args[0], args[1], f)
			if field == "" {
				field = g.comparedToEmpty(args[1], args[0], f)
			}
			if field != "" {
				set := map[string]bool{field: true}
				if head.Ident == "ne" {
					return set, nil
				}
				return nil, set
			}
		}
	}
	return nil, nil
}

func (g *gapWalker) condArg(n parse.Node, f *frame) (whenTrue, whenFalse map[string]bool) {
	if p, ok := n.(*parse.PipeNode); ok {
		return g.cond(p, f)
	}
	if v := g.operandOf(n, f); v.field != "" {
		return map[string]bool{v.field: true}, nil
	}
	return nil, nil
}

// comparedToEmpty is the field a compares against an empty string literal b.
func (g *gapWalker) comparedToEmpty(a, b parse.Node, f *frame) string {
	s, ok := b.(*parse.StringNode)
	if !ok || s.Text != "" {
		return ""
	}
	return g.operandOf(a, f).field
}
