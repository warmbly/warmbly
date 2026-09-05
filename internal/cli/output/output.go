// Package output renders an API response for whoever is reading it.
//
// A terminal gets a table, a pipe gets the JSON the API sent, and --template
// gets a Go template. The default flips on whether stdout is a terminal, so
// `warmbly campaign list` is readable and `warmbly campaign list > f.json` is
// parseable without anyone passing a flag.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/warmbly/warmbly/internal/cli/iostreams"
)

// Column is one table column: a header and where to read it from.
type Column struct {
	Header string
	// Path is dotted, so "organization.name" and "counts.sent" both work.
	Path string
	// Format names a renderer: time, date, bool, status, bytes, or empty for
	// the value as it stands.
	Format string
	// Truncate caps the rendered width; 0 means no cap.
	Truncate int
}

// Table describes how one endpoint's payload becomes rows.
type Table struct {
	// Root is the dotted path to the array of rows. Empty means the payload is
	// itself the array, or a single object rendered as one row.
	Root    string
	Columns []Column
	// Empty is what to say when there are no rows, phrased for the resource.
	Empty string
}

// Printer holds the choice of renderer for one invocation.
type Printer struct {
	IO       *iostreams.IOStreams
	JSON     bool
	Template string
	// Fields narrows a table to named columns (--fields id,name).
	Fields []string
}

// Print renders payload. table may be empty, in which case JSON is the only
// honest rendering and is used regardless of the terminal.
func (p *Printer) Print(payload []byte, table Table) error {
	if p.Template != "" {
		return p.renderTemplate(payload)
	}
	if p.JSON || len(table.Columns) == 0 || !p.IO.IsStdoutTTY() {
		return p.renderJSON(payload)
	}
	return p.renderTable(payload, table)
}

func (p *Printer) renderJSON(payload []byte) error {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		fmt.Fprintln(p.IO.Out, "{}")
		return nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, trimmed, "", "  "); err != nil {
		// Some endpoints stream a file. Pass it through untouched.
		_, werr := p.IO.Out.Write(payload)
		return werr
	}
	fmt.Fprintln(p.IO.Out, buf.String())
	return nil
}

func (p *Printer) renderTemplate(payload []byte) error {
	tmpl, err := template.New("out").Funcs(templateFuncs).Parse(p.Template)
	if err != nil {
		return fmt.Errorf("the --template is not a valid Go template: %w", err)
	}
	var data any
	if err := json.Unmarshal(bytes.TrimSpace(payload), &data); err != nil {
		return fmt.Errorf("the response is not JSON, so --template has nothing to walk: %w", err)
	}
	if err := tmpl.Execute(p.IO.Out, data); err != nil {
		return err
	}
	fmt.Fprintln(p.IO.Out)
	return nil
}

var templateFuncs = template.FuncMap{
	"join": func(sep string, in []any) string {
		parts := make([]string, 0, len(in))
		for _, v := range in {
			parts = append(parts, fmt.Sprint(v))
		}
		return strings.Join(parts, sep)
	},
	"pluck": func(field string, rows []any) []any {
		out := make([]any, 0, len(rows))
		for _, r := range rows {
			if m, ok := r.(map[string]any); ok {
				out = append(out, m[field])
			}
		}
		return out
	},
	"timeago": func(v any) string { return relative(fmt.Sprint(v)) },
}

func (p *Printer) renderTable(payload []byte, table Table) error {
	var doc any
	if err := json.Unmarshal(bytes.TrimSpace(payload), &doc); err != nil {
		return p.renderJSON(payload)
	}

	node := doc
	if table.Root != "" {
		node = dig(doc, table.Root)
	}
	var rows []any
	switch v := node.(type) {
	case []any:
		rows = v
	case map[string]any:
		rows = []any{v}
	case nil:
		rows = nil
	default:
		return p.renderJSON(payload)
	}

	columns := table.Columns
	if len(p.Fields) > 0 {
		columns = filterColumns(columns, p.Fields)
		if len(columns) == 0 {
			return fmt.Errorf("none of the requested fields exist here. Available: %s", strings.Join(headerNames(table.Columns), ", "))
		}
	}

	if len(rows) == 0 {
		empty := table.Empty
		if empty == "" {
			empty = "Nothing here yet."
		}
		fmt.Fprintln(p.IO.Out, p.IO.Gray(empty))
		return nil
	}

	cells := make([][]string, 0, len(rows)+1)
	header := make([]string, len(columns))
	for i, c := range columns {
		header[i] = strings.ToUpper(c.Header)
	}
	cells = append(cells, header)
	for _, r := range rows {
		row := make([]string, len(columns))
		for i, c := range columns {
			row[i] = render(dig(r, c.Path), c)
		}
		cells = append(cells, row)
	}

	p.writeTable(cells)
	return nil
}

// writeTable pads to the widest cell per column, then drops trailing columns
// that no longer fit rather than wrapping: a wrapped table is unreadable and
// the JSON is one flag away.
func (p *Printer) writeTable(cells [][]string) {
	if len(cells) == 0 {
		return
	}
	cols := len(cells[0])
	widths := make([]int, cols)
	for _, row := range cells {
		for i, cell := range row {
			if n := len([]rune(iostreams.StripANSI(cell))); n > widths[i] {
				widths[i] = n
			}
		}
	}

	limit := p.IO.TerminalWidth()
	keep := cols
	used := 0
	for i := 0; i < cols; i++ {
		next := used + widths[i] + 2
		if i > 0 && next > limit {
			keep = i
			break
		}
		used = next
	}
	if keep < 1 {
		keep = 1
	}

	for r, row := range cells {
		var line strings.Builder
		for i := 0; i < keep; i++ {
			cell := row[i]
			if r == 0 {
				cell = p.IO.Gray(cell)
			}
			line.WriteString(cell)
			if i < keep-1 {
				pad := widths[i] - len([]rune(iostreams.StripANSI(row[i]))) + 2
				line.WriteString(strings.Repeat(" ", pad))
			}
		}
		fmt.Fprintln(p.IO.Out, strings.TrimRight(line.String(), " "))
	}
}

func filterColumns(cols []Column, want []string) []Column {
	keep := make(map[string]bool, len(want))
	for _, w := range want {
		keep[strings.ToLower(strings.TrimSpace(w))] = true
	}
	out := make([]Column, 0, len(cols))
	for _, c := range cols {
		if keep[strings.ToLower(c.Header)] || keep[strings.ToLower(c.Path)] {
			out = append(out, c)
		}
	}
	return out
}

func headerNames(cols []Column) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, strings.ToLower(c.Header))
	}
	return out
}

// dig walks a dotted path through decoded JSON. A numeric segment indexes an
// array, so "data.0.name" works the way anyone would expect it to.
func dig(node any, path string) any {
	if path == "" {
		return node
	}
	cur := node
	for _, seg := range strings.Split(path, ".") {
		if cur == nil {
			return nil
		}
		switch v := cur.(type) {
		case map[string]any:
			cur = v[seg]
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil
			}
			cur = v[idx]
		default:
			return nil
		}
	}
	return cur
}

func render(v any, c Column) string {
	s := stringify(v)
	switch c.Format {
	case "time":
		s = relative(s)
	case "date":
		s = shortDate(s)
	case "bool":
		if b, ok := v.(bool); ok {
			if b {
				return "yes"
			}
			return "no"
		}
	case "int":
		if f, ok := v.(float64); ok {
			return strconv.FormatInt(int64(f), 10)
		}
	}
	if c.Truncate > 0 && len([]rune(s)) > c.Truncate {
		s = string([]rune(s)[:c.Truncate-1]) + "…"
	}
	return s
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return "-"
	case string:
		if t == "" {
			return "-"
		}
		return t
	case bool:
		if t {
			return "yes"
		}
		return "no"
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', 2, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, stringify(item))
		}
		if len(parts) == 0 {
			return "-"
		}
		return strings.Join(parts, ",")
	case map[string]any:
		// A nested object in a table cell is noise; name it if it has a name.
		for _, key := range []string{"name", "email", "title", "id"} {
			if s, ok := t[key].(string); ok && s != "" {
				return s
			}
		}
		return "{…}"
	default:
		return fmt.Sprint(t)
	}
}

// relative turns a timestamp into "3h ago", which is what a person reading a
// list actually wants to know.
func relative(raw string) string {
	if raw == "" || raw == "-" {
		return "-"
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	d := time.Since(ts)
	future := ""
	if d < 0 {
		d = -d
		future = "in "
	}
	suffix := " ago"
	if future != "" {
		suffix = ""
	}
	switch {
	case d < time.Minute:
		if future != "" {
			return "in a moment"
		}
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%s%dm%s", future, int(d.Minutes()), suffix)
	case d < 24*time.Hour:
		return fmt.Sprintf("%s%dh%s", future, int(d.Hours()), suffix)
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%s%dd%s", future, int(d.Hours()/24), suffix)
	default:
		return ts.Local().Format("2 Jan 2006")
	}
}

func shortDate(raw string) string {
	if raw == "" || raw == "-" {
		return "-"
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return ts.Local().Format("2006-01-02 15:04")
}
