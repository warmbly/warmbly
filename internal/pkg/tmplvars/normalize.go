package tmplvars

import (
	"errors"
	"fmt"
	htmltemplate "html/template"
	"maps"
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

var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

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

// mode is what the walker's rewrites are for.
type mode int

const (
	// forRender keeps a bare alias's spelling, so a custom field of that name wins.
	forRender mode = iota
	// forStore writes a bare alias as its canonical field, which every release renders.
	forStore
)

// Normalize rewrites the accepted spellings of campaign copy into the form the
// engine compiles: spaced and dashed custom-field keys become index calls, a
// bare built-in name ({{first_name}}) becomes a field of the root, and Sender
// fields take their canonical names. Text outside actions and string literals
// are never touched. The error names a reference that can never resolve; the
// returned source is usable either way, so a renderer may ignore it.
func Normalize(src string) (string, error) {
	w, rewritten, _ := analyze(src, forRender)
	if w == nil {
		return rewritten, nil // the compiler reports a syntax error itself
	}
	return applyEdits(rewritten, w.edits), w.err
}

// Canonicalize is the copy to store for src: bare built-in aliases become their
// canonical field ({{first_name}} is {{.FirstName}}) and Sender fields take
// their canonical names, so the previous release renders it too. Dotted aliases
// and everything outside actions are kept as written.
func Canonicalize(src string) string {
	w, _, spans := analyze(src, forStore)
	if w == nil || w.err != nil || len(w.edits) == 0 {
		return src
	}
	edits := make([]edit, 0, len(w.edits))
	for _, e := range w.edits {
		start, ok1 := toOriginal(e.start, spans)
		end, ok2 := toOriginal(e.end, spans)
		if ok1 && ok2 {
			edits = append(edits, edit{start, end, e.text})
		}
	}
	return applyEdits(src, edits)
}

// CanonicalizeAll canonicalises each non-nil copy field in place.
func CanonicalizeAll(fields ...*string) {
	for _, f := range fields {
		if f != nil {
			*f = Canonicalize(*f)
		}
	}
}

// References lists the canonical built-in fields src reads from the contact.
func References(src string) map[string]bool {
	w, _, _ := analyze(src, forRender)
	if w == nil {
		return nil
	}
	return w.refs
}

// analyze parses src with spaced references rewritten and form-link markers
// blanked, and walks it. A nil walker means src does not parse.
func analyze(src string, m mode) (*walker, string, []span) {
	if !strings.Contains(src, "{{") {
		return nil, src, nil
	}
	rewritten, spans := rewriteSpaced(src)
	tree := parse.New("normalize")
	tree.Mode = parse.SkipFuncCheck
	trees := map[string]*parse.Tree{}
	if _, err := tree.Parse(maskMarkers(rewritten), "", "", trees); err != nil {
		return nil, rewritten, spans
	}
	w := &walker{src: rewritten, mode: m, vars: map[string]scope{"$": scopeRoot}, refs: map[string]bool{}}
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
	return w, rewritten, spans
}

// maskMarkers blanks each form-link marker, keeping offsets and line breaks.
func maskMarkers(src string) string {
	return FormLinkMarker.ReplaceAllStringFunc(src, blank)
}

func blank(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		return ' '
	}, s)
}

// Check reports whether campaign copy compiles as written, naming the problem
// and its line when it does not.
func Check(src string) error {
	if src == "" {
		return nil
	}
	norm, err := Normalize(maskMarkers(src))
	if err != nil {
		return err
	}
	if _, err := tmplfuncs.Compile("copy", norm); err != nil {
		return errors.New(strings.Replace(err.Error(), "template: copy:", "line ", 1))
	}
	return nil
}

// CheckHTML reports an HTML body the HTML renderer refuses although it parses:
// a predefined escaper (html, urlquery) placed before a pipe.
func CheckHTML(src string) error {
	if !strings.Contains(src, "{{") {
		return nil
	}
	// Comments are shielded as the renderer shields them: a comment holding an action renders as text.
	src = htmlComment.ReplaceAllStringFunc(src, func(c string) string {
		if strings.Contains(c, "{{") {
			return "    " + c[4:len(c)-3] + "   "
		}
		return blank(c)
	})
	norm, err := Normalize(maskMarkers(src))
	if err != nil {
		return nil // Check reports it
	}
	t, err := tmplfuncs.CompileHTML("copy", norm)
	if err != nil {
		return nil // Check reports it
	}
	_, err = tmplfuncs.Execute(t, map[string]any{})
	var he *htmltemplate.Error
	if errors.As(err, &he) && he.ErrorCode == htmltemplate.ErrPredefinedEscaper {
		return fmt.Errorf("line %d: %s", he.Line, he.Description)
	}
	return nil
}

// Part is one named piece of copy to check; HTML marks a body rendered as HTML.
type Part struct {
	Name, Text string
	HTML       bool
}

// CheckCopy checks each part in order and reports the first problem with its part's name.
func CheckCopy(parts ...Part) error {
	for _, p := range parts {
		err := Check(p.Text)
		if err == nil && p.HTML {
			err = CheckHTML(p.Text)
		}
		if err != nil {
			return fmt.Errorf("the %s has a template error: %w", p.Name, err)
		}
	}
	return nil
}

type edit struct {
	start, end int
	text       string
}

// span is one spaced-reference rewrite: [origStart, origEnd) became [newStart, newEnd).
type span struct{ origStart, origEnd, newStart, newEnd int }

// toOriginal maps an offset in the rewritten source back to src; false inside a rewrite.
func toOriginal(pos int, spans []span) (int, bool) {
	delta := 0
	for _, s := range spans {
		if pos <= s.newStart {
			break
		}
		if pos < s.newEnd {
			return 0, false
		}
		delta += (s.newEnd - s.newStart) - (s.origEnd - s.origStart)
	}
	return pos - delta, true
}

func applyEdits(src string, edits []edit) string {
	if len(edits) == 0 {
		return src
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var b strings.Builder
	last := 0
	for _, e := range edits {
		if e.start < last {
			continue
		}
		b.WriteString(src[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.WriteString(src[last:])
	return b.String()
}

// scope is what a dot or a variable holds, as far as the walker can tell.
type scope int

const (
	scopeOther  scope = iota // a value whose fields the walker does not know
	scopeRoot                // the template data
	scopeSender              // the typed Sender value
)

type walker struct {
	src      string
	mode     mode
	edits    []edit
	err      error
	vars     map[string]scope
	assigned []string // variables reassigned with =, in walk order
	refs     map[string]bool
}

func (w *walker) fail(pos int, format string, args ...any) {
	if w.err == nil {
		line := strings.Count(w.src[:min(max(pos, 0), len(w.src))], "\n") + 1
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
		w.structure(&x.BranchNode, dot, func(scope) scope { return dot }, false)
	case *parse.WithNode:
		w.structure(&x.BranchNode, dot, func(v scope) scope { return v }, false)
	case *parse.RangeNode:
		w.structure(&x.BranchNode, dot, func(scope) scope { return scopeOther }, true)
	case *parse.TemplateNode:
		w.pipe(x.Pipe, dot, false)
	}
}

// structure walks an if, with or range. Variables declared inside it end with
// it; the body and the else branch start from the same bindings; a variable
// either branch reassigns is unknown afterwards.
func (w *walker) structure(b *parse.BranchNode, dot scope, bodyDot func(scope) scope, ranged bool) {
	outer, mark := w.vars, len(w.assigned)
	w.vars = maps.Clone(outer)
	value := w.pipe(b.Pipe, dot, ranged)
	declared := w.vars
	w.vars = maps.Clone(declared)
	w.node(b.List, bodyDot(value))
	w.vars = maps.Clone(declared)
	w.node(b.ElseList, dot)
	w.vars = outer
	for _, name := range w.assigned[mark:] {
		if _, ok := outer[name]; ok {
			outer[name] = scopeOther
		}
	}
}

// pipe visits a pipeline, records what its variables hold, and returns what
// the pipeline evaluates to.
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
	if ranged {
		value = scopeOther
	}
	for _, v := range p.Decl {
		w.vars[v.Ident[0]] = value
		if p.IsAssign {
			w.assigned = append(w.assigned, v.Ident[0])
		}
	}
	return value
}

// valueOf is what a pipeline of one operand holds; anything else is scopeOther.
func (w *walker) valueOf(p *parse.PipeNode, dot scope) scope {
	if len(p.Cmds) != 1 || len(p.Cmds[0].Args) != 1 {
		return scopeOther
	}
	switch x := p.Cmds[0].Args[0].(type) {
	case *parse.PipeNode:
		return w.valueOf(x, dot)
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
		w.identifier(x, calls, dot)
	case *parse.FieldNode:
		w.fields(int(x.Pos), "."+strings.Join(x.Ident, "."), "", dot, x.Ident)
	case *parse.VariableNode:
		if len(x.Ident) > 1 {
			w.fields(int(x.Pos), strings.Join(x.Ident, "."), x.Ident[0], w.vars[x.Ident[0]], x.Ident[1:])
		}
	case *parse.ChainNode:
		switch inner := x.Node.(type) {
		case *parse.IdentifierNode:
			if _, builtin := topNames[inner.Ident]; !builtin {
				w.identifier(inner, false, dot)
				return
			}
			head := "$"
			if dot == scopeRoot {
				head = ""
			}
			text := inner.Ident + "." + strings.Join(x.Field, ".")
			w.fields(int(inner.Pos), text, head, scopeRoot, append([]string{inner.Ident}, x.Field...))
		case *parse.PipeNode:
			w.pipe(inner, dot, false)
		}
	}
}

func (w *walker) identifier(x *parse.IdentifierNode, calls bool, dot scope) {
	if isFunc(x.Ident) {
		return
	}
	pos := int(x.Pos)
	canonical, ok := topNames[x.Ident]
	if !ok || calls {
		if calls {
			w.fail(pos, "function %q is not defined", x.Ident)
			return
		}
		w.fail(pos, "%s", unknownVariable(x.Ident))
		return
	}
	w.refs[canonical] = true
	if !strings.HasPrefix(w.src[pos:], x.Ident) {
		return
	}
	prefix := "$."
	if dot == scopeRoot {
		prefix = "."
	}
	name := x.Ident // the alias spelling, so a custom field of that name still wins
	if w.mode == forStore {
		name = canonical
	}
	w.edits = append(w.edits, edit{pos, pos + len(x.Ident), prefix + name})
}

// fields checks a selector chain on a value held in scope and canonicalises the
// part that reaches into Sender, whose field set is fixed. text is the chain as
// written, head the variable it starts from ("" for dot).
func (w *walker) fields(pos int, text, head string, held scope, idents []string) {
	start := w.locate(pos, text)
	if start < 0 {
		start = pos
	}
	switch held {
	case scopeRoot:
		canonical, builtin := topNames[idents[0]]
		if builtin {
			w.refs[canonical] = true
		}
		if builtin && canonical == SenderField {
			if len(idents) > 1 {
				w.sender(start, text, head+"."+SenderField, idents[1:])
			}
			return
		}
		// Every other field of the data is text, which has no fields.
		if len(idents) > 1 {
			w.fail(start, "field %s has no field %q", idents[0], idents[1])
		}
	case scopeSender:
		w.sender(start, text, head, idents)
	default:
		what := "this value"
		if head != "" {
			what = head
		}
		w.fail(start, "fields of %s cannot be checked; refer to the field directly, like {{.Sender.Email}}", what)
	}
}

// sender checks the selectors after a Sender value and canonicalises the field
// name; prefix is the canonical way to reach that value.
func (w *walker) sender(start int, text, prefix string, fields []string) {
	field, ok := senderNames[fields[0]]
	if !ok {
		w.fail(start, "unknown sender field %q; the sender fields are %s", fields[0], senderList)
		return
	}
	if len(fields) > 1 {
		w.fail(start, "sender field %s has no field %q", field, fields[1])
		return
	}
	if rewritten := prefix + "." + field; rewritten != text && strings.HasPrefix(w.src[start:], text) {
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

func unknownVariable(name string) string {
	return fmt.Sprintf("unknown variable %q: write a contact field with a leading dot, like {{.%s}}; the built-in fields are %s", name, name, canonicalList)
}

// rewriteSpaced rewrites `.job title` and `.first-name` inside each action to
// `(index . "key")`, skipping string literals, and reports where it did.
func rewriteSpaced(src string) (string, []span) {
	var b strings.Builder
	b.Grow(len(src))
	var spans []span
	last := 0
	for _, loc := range ActionPattern.FindAllStringIndex(src, -1) {
		b.WriteString(src[last:loc[0]])
		rewriteSpacedInAction(&b, src, loc[0], loc[1], &spans)
		last = loc[1]
	}
	b.WriteString(src[last:])
	return b.String(), spans
}

func rewriteSpacedInAction(b *strings.Builder, src string, from, to int, spans *[]span) {
	var quote byte // 0 = not in a string literal; '"' or '`' otherwise
	for i := from; i < to; {
		c := src[i]
		switch {
		case quote != 0:
			b.WriteByte(c)
			if c == '\\' && quote == '"' && i+1 < to {
				b.WriteByte(src[i+1]) // keep an escaped char verbatim
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
			fieldStart := i == from || strings.ContainsRune("{( \t\r\n", rune(src[i-1]))
			if m := spacedFieldRefAt.FindString(src[i:to]); fieldStart && m != "" {
				repl := `(index . "` + m[1:] + `")`
				*spans = append(*spans, span{i, i + len(m), b.Len(), b.Len() + len(repl)})
				b.WriteString(repl)
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
}
