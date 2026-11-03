package tmplvars

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template/parse"

	"github.com/warmbly/warmbly/internal/pkg/tmplfuncs"
)

// ActionPattern matches a single {{ ... }} action (no nested braces).
var ActionPattern = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// FormLinkMarker matches the {{form_link:<public_id>}} marker the campaign
// editor inserts, which the send pipeline resolves before the template renders.
// Keep in sync with FORM_LINK_RE in web/src/lib/templateVars.ts.
var FormLinkMarker = regexp.MustCompile(`\{\{\s*form_link:([a-z0-9]{1,64})\s*\}\}`)

// spacedFieldRefAt matches, at the start of the slice, a dotted reference whose
// key has an internal space or dash (".job title", ".first-name"), which is not
// a valid selector and is rewritten to `(index . "key")`.
var spacedFieldRefAt = regexp.MustCompile(`^\.[A-Za-z0-9_]+(?:[ \-]+[A-Za-z0-9_]+)+`)

// builtinFuncs are text/template's own functions, which a bare name may call.
var builtinFuncs = map[string]bool{
	"and": true, "call": true, "html": true, "index": true, "slice": true, "js": true, "len": true,
	"not": true, "or": true, "print": true, "printf": true, "println": true, "urlquery": true,
	"eq": true, "ge": true, "gt": true, "le": true, "lt": true, "ne": true,
}

func isFunc(name string) bool {
	_, ok := tmplfuncs.FuncMap()[name]
	return ok || builtinFuncs[name]
}

// Normalize rewrites the accepted spellings of campaign copy into the form the
// engine compiles: spaced and dashed custom-field keys become index calls, a
// bare built-in name ({{first_name}}) becomes a field of the root, and Sender
// fields take their canonical names. Text outside actions and string literals
// are never touched. The error names a reference that can never resolve; the
// returned source is usable either way, so a renderer may ignore it.
func Normalize(src string) (string, error) {
	if !strings.Contains(src, "{{") {
		return src, nil
	}
	src = ActionPattern.ReplaceAllStringFunc(src, rewriteSpacedInAction)
	tree := parse.New("normalize")
	tree.Mode = parse.SkipFuncCheck
	trees := map[string]*parse.Tree{}
	if _, err := tree.Parse(src, "", "", trees); err != nil {
		return src, nil // the compiler reports the syntax error itself
	}
	w := &walker{src: src, vars: map[string]scope{"$": scopeRoot}}
	names := make([]string, 0, len(trees))
	for name := range trees {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if t := trees[name]; t != nil && t.Root != nil {
			w.node(t.Root, scopeRoot)
		}
	}
	return w.apply(), w.err
}

// Check reports whether campaign copy compiles as written, naming the problem
// and its line when it does not.
func Check(src string) error {
	if src == "" {
		return nil
	}
	// A marker is dropped for the check, keeping its line breaks so line numbers stay true.
	norm, err := Normalize(FormLinkMarker.ReplaceAllStringFunc(src, func(m string) string {
		return strings.Repeat("\n", strings.Count(m, "\n"))
	}))
	if err != nil {
		return err
	}
	if _, err := tmplfuncs.Compile("copy", norm); err != nil {
		return errors.New(strings.Replace(err.Error(), "template: copy:", "line ", 1))
	}
	return nil
}

// Part is one named piece of copy to check.
type Part struct{ Name, Text string }

// CheckCopy checks each part in order and reports the first problem with its part's name.
func CheckCopy(parts ...Part) error {
	for _, p := range parts {
		if err := Check(p.Text); err != nil {
			return fmt.Errorf("the %s has a template error: %w", p.Name, err)
		}
	}
	return nil
}

type edit struct {
	start, end int
	text       string
}

// scope is what a dot or a variable holds, as far as the walker can tell.
type scope int

const (
	scopeOther  scope = iota // a value whose fields the walker does not know
	scopeRoot                // the template data
	scopeSender              // the typed Sender value
)

type walker struct {
	src   string
	edits []edit
	err   error
	vars  map[string]scope
}

func (w *walker) fail(pos int, format string, args ...any) {
	if w.err == nil {
		line := strings.Count(w.src[:min(pos, len(w.src))], "\n") + 1
		w.err = fmt.Errorf("line %d: %s", line, fmt.Sprintf(format, args...))
	}
}

func (w *walker) node(n parse.Node, dot scope) {
	switch x := n.(type) {
	case *parse.ListNode:
		if x == nil {
			return
		}
		for _, c := range x.Nodes {
			w.node(c, dot)
		}
	case *parse.ActionNode:
		w.pipe(x.Pipe, dot, false)
	case *parse.IfNode:
		w.pipe(x.Pipe, dot, false)
		w.node(x.List, dot)
		w.node(x.ElseList, dot)
	case *parse.RangeNode:
		w.pipe(x.Pipe, dot, true)
		w.node(x.List, scopeOther)
		w.node(x.ElseList, dot)
	case *parse.WithNode:
		inner := w.pipe(x.Pipe, dot, false)
		w.node(x.List, inner)
		w.node(x.ElseList, dot)
	case *parse.TemplateNode:
		w.pipe(x.Pipe, dot, false)
	}
}

// pipe visits a pipeline, records what its declared variables hold, and
// returns what the pipeline evaluates to.
func (w *walker) pipe(p *parse.PipeNode, dot scope, ranged bool) scope {
	if p == nil {
		return scopeOther
	}
	for _, cmd := range p.Cmds {
		for i, arg := range cmd.Args {
			w.arg(arg, i == 0 && len(cmd.Args) > 1, dot)
		}
	}
	value := w.valueOf(p, dot)
	for _, v := range p.Decl {
		if ranged {
			w.vars[v.Ident[0]] = scopeOther
		} else {
			w.vars[v.Ident[0]] = value
		}
	}
	return value
}

// valueOf is what a pipeline of one bare operand holds; anything else is scopeOther.
func (w *walker) valueOf(p *parse.PipeNode, dot scope) scope {
	if len(p.Cmds) != 1 || len(p.Cmds[0].Args) != 1 {
		return scopeOther
	}
	switch x := p.Cmds[0].Args[0].(type) {
	case *parse.DotNode:
		return dot
	case *parse.IdentifierNode:
		if topNames[x.Ident] == SenderField {
			return scopeSender
		}
	case *parse.FieldNode:
		if dot == scopeRoot && len(x.Ident) == 1 && topNames[x.Ident[0]] == SenderField {
			return scopeSender
		}
	case *parse.VariableNode:
		held := w.vars[x.Ident[0]]
		if len(x.Ident) == 1 {
			return held
		}
		if held == scopeRoot && len(x.Ident) == 2 && topNames[x.Ident[1]] == SenderField {
			return scopeSender
		}
	}
	return scopeOther
}

// arg visits one operand; calls is whether it is a command head given arguments.
func (w *walker) arg(n parse.Node, calls bool, dot scope) {
	switch x := n.(type) {
	case *parse.PipeNode:
		w.pipe(x, dot, false)
	case *parse.IdentifierNode:
		w.identifier(x, calls)
	case *parse.FieldNode:
		w.fields(int(x.Pos), "."+strings.Join(x.Ident, "."), "", dot, x.Ident)
	case *parse.VariableNode:
		name := x.Ident[0]
		held, known := w.vars[name]
		if len(x.Ident) == 1 {
			return
		}
		if !known || held == scopeOther {
			w.fail(int(x.Pos), "fields of %s cannot be checked; refer to the field directly, like {{.Sender.Email}}", name)
			return
		}
		w.fields(int(x.Pos), strings.Join(x.Ident, "."), name, held, x.Ident[1:])
	case *parse.ChainNode:
		switch inner := x.Node.(type) {
		case *parse.IdentifierNode:
			if topNames[inner.Ident] == SenderField {
				text := inner.Ident + "." + strings.Join(x.Field, ".")
				w.fields(int(inner.Pos), text, "$", scopeRoot, append([]string{inner.Ident}, x.Field...))
				return
			}
			w.identifier(inner, false)
		case *parse.PipeNode:
			w.pipe(inner, dot, false)
		}
	}
}

func (w *walker) identifier(x *parse.IdentifierNode, calls bool) {
	if isFunc(x.Ident) {
		return
	}
	pos := int(x.Pos)
	if _, ok := topNames[x.Ident]; !ok || calls {
		if calls {
			w.fail(pos, "function %q is not defined", x.Ident)
			return
		}
		w.fail(pos, "%s", unknownVariable(x.Ident))
		return
	}
	if strings.HasPrefix(w.src[pos:], x.Ident) {
		// The alias spelling is kept, so a custom field of that name still wins.
		w.edits = append(w.edits, edit{pos, pos + len(x.Ident), "$." + x.Ident})
	}
}

// fields checks a selector chain on a value held in scope and canonicalises the
// part that reaches into Sender, whose field set is fixed. text is the chain as
// written, head the variable it starts from ("" for dot).
func (w *walker) fields(pos int, text, head string, held scope, idents []string) {
	var sender []string
	prefix := head
	switch held {
	case scopeRoot:
		if len(idents) < 2 || topNames[idents[0]] != SenderField {
			return
		}
		sender, prefix = idents[1:], head+"."+SenderField
	case scopeSender:
		sender = idents
	default:
		return
	}
	start := w.locate(pos, text)
	if start < 0 {
		return
	}
	field, ok := senderNames[sender[0]]
	if !ok {
		w.fail(start, "unknown sender field %q; the sender fields are %s", sender[0], senderList)
		return
	}
	if len(sender) > 1 {
		w.fail(start, "sender field %s has no field %q", field, sender[1])
		return
	}
	if rewritten := prefix + "." + field; rewritten != text {
		w.edits = append(w.edits, edit{start, start + len(text), rewritten})
	}
}

// locate finds where text, which the parser reported at pos, starts in the
// source. A chained reference is reported at a later segment, so search back.
func (w *walker) locate(pos int, text string) int {
	for s := min(pos, len(w.src)); s >= 0 && s >= pos-len(text); s-- {
		if strings.HasPrefix(w.src[s:], text) {
			return s
		}
	}
	return -1
}

func (w *walker) apply() string {
	if len(w.edits) == 0 {
		return w.src
	}
	sort.Slice(w.edits, func(i, j int) bool { return w.edits[i].start < w.edits[j].start })
	var b strings.Builder
	last := 0
	for _, e := range w.edits {
		if e.start < last {
			continue
		}
		b.WriteString(w.src[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.WriteString(w.src[last:])
	return b.String()
}

func unknownVariable(name string) string {
	return fmt.Sprintf("unknown variable %q: write a contact field with a leading dot, like {{.%s}}; the built-in fields are %s", name, name, canonicalList)
}

// rewriteSpacedInAction rewrites `.job title` and `.first-name` inside one
// action to `(index . "key")`, skipping string literals.
func rewriteSpacedInAction(action string) string {
	var b strings.Builder
	b.Grow(len(action))
	var quote byte // 0 = not in a string literal; '"' or '`' otherwise
	for i := 0; i < len(action); {
		c := action[i]
		switch {
		case quote != 0:
			b.WriteByte(c)
			if c == '\\' && quote == '"' && i+1 < len(action) {
				b.WriteByte(action[i+1]) // keep an escaped char verbatim
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			i++
		case c == '"' || c == '`':
			quote = c
			b.WriteByte(c)
			i++
		case c == '.':
			// A nested struct selector must not consume the following function argument.
			fieldStart := i == 0 || strings.ContainsRune("{( \t\r\n", rune(action[i-1]))
			if m := spacedFieldRefAt.FindString(action[i:]); fieldStart && m != "" {
				b.WriteString(`(index . "` + m[1:] + `")`)
				i += len(m)
				continue
			}
			b.WriteByte(c)
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}
