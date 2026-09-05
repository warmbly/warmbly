package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/warmbly/warmbly/internal/cli/api"
	"github.com/warmbly/warmbly/internal/cli/output"
)

// The typed commands are a table. Dispatch, flags, help, the request and the
// rendering all come from one row, so covering a new endpoint is adding a row
// rather than writing a command. `warmbly api` covers anything the table does
// not, which is what keeps the table from having to be exhaustive to be useful.

type bodyMode int

const (
	bodyNone     bodyMode = iota // the endpoint takes no body
	bodyOptional                 // a body may be sent
	bodyRequired                 // a body must be sent
)

type flagKind int

const (
	flagString flagKind = iota
	flagInt
	flagBool
	flagStrings
)

// argSpec is one positional argument, filling the next {} in the path.
type argSpec struct {
	Name string
	Help string
}

// flagSpec is one flag. Query flags become query parameters; the rest become
// body fields, so `campaign create --name X` needs no JSON.
type flagSpec struct {
	Name  string
	Short string
	Help  string
	Kind  flagKind
	Query bool
	// Key overrides the body field or query parameter name, which otherwise
	// is the flag name with dashes turned into underscores.
	Key string
}

func (f flagSpec) key() string {
	if f.Key != "" {
		return f.Key
	}
	return strings.ReplaceAll(f.Name, "-", "_")
}

type endpoint struct {
	Name    string
	Aliases []string
	Short   string
	Long    string
	Example string

	Method string
	// Path is /v1-relative and carries one {name} per positional argument.
	Path string
	Args []argSpec
	Flag []flagSpec
	Body bodyMode

	// Sends marks a command that puts real mail on the wire. Those confirm.
	Sends bool
	// Paginate offers --all, which walks the cursor.
	Paginate bool
	// Idempotent offers --idempotency-key.
	Idempotent bool

	Table output.Table
	// Success is what to say on a terminal when there is nothing to tabulate.
	Success string
}

type resource struct {
	Name      string
	Aliases   []string
	Short     string
	Long      string
	Group     string
	Endpoints []endpoint
}

func resourceCommands(f *Factory) []*cobra.Command {
	specs := resourceSpecs()
	out := make([]*cobra.Command, 0, len(specs))
	for _, r := range specs {
		out = append(out, buildResource(f, r))
	}
	return out
}

func buildResource(f *Factory, r resource) *cobra.Command {
	cmd := &cobra.Command{
		Use:     r.Name + " <command>",
		Aliases: r.Aliases,
		Short:   r.Short,
		Long:    r.Long,
		GroupID: r.Group,
	}
	for _, e := range r.Endpoints {
		cmd.AddCommand(buildEndpoint(f, r, e))
	}
	return cmd
}

func buildEndpoint(f *Factory, r resource, e endpoint) *cobra.Command {
	use := e.Name
	for _, a := range e.Args {
		use += " <" + a.Name + ">"
	}

	long := e.Long
	if long == "" {
		long = e.Short + "."
	}
	if len(e.Args) > 0 {
		var lines []string
		for _, a := range e.Args {
			lines = append(lines, fmt.Sprintf("  <%s>  %s", a.Name, a.Help))
		}
		long += "\n\nArguments:\n" + strings.Join(lines, "\n")
	}
	if e.Sends {
		long += "\n\nThis command sends real mail. It asks before doing so; --yes skips the question."
	}

	cmd := &cobra.Command{
		Use:     use,
		Aliases: e.Aliases,
		Short:   e.Short,
		Long:    long,
		Example: e.Example,
		Args:    cobra.ExactArgs(len(e.Args)),
	}

	// Flag values are held here so the runner reads whatever cobra parsed.
	strs := map[string]*string{}
	ints := map[string]*int{}
	bools := map[string]*bool{}
	slices := map[string]*[]string{}
	for _, fl := range e.Flag {
		switch fl.Kind {
		case flagInt:
			ints[fl.Name] = cmd.Flags().IntP(fl.Name, fl.Short, 0, fl.Help)
		case flagBool:
			bools[fl.Name] = cmd.Flags().BoolP(fl.Name, fl.Short, false, fl.Help)
		case flagStrings:
			slices[fl.Name] = cmd.Flags().StringSliceP(fl.Name, fl.Short, nil, fl.Help)
		default:
			strs[fl.Name] = cmd.Flags().StringP(fl.Name, fl.Short, "", fl.Help)
		}
	}

	var (
		input    string
		rawField []string
		typField []string
		all      bool
		maxPages int
		idemKey  string
	)
	if e.Body != bodyNone {
		cmd.Flags().StringVar(&input, "input", "", "Request body: JSON, @file, or - for stdin")
		cmd.Flags().StringArrayVarP(&rawField, "raw-field", "f", nil, "Body field as a string: key=value")
		cmd.Flags().StringArrayVarP(&typField, "field", "F", nil, "Body field with a guessed type: key=value")
	}
	if e.Paginate {
		cmd.Flags().BoolVar(&all, "all", false, "Fetch every page, not just the first")
		cmd.Flags().IntVar(&maxPages, "max-pages", 100, "Stop after this many pages when --all is set")
	}
	if e.Idempotent {
		cmd.Flags().StringVar(&idemKey, "idempotency-key", "", "Idempotency-Key header for a safely retryable write")
	}

	cmd.RunE = func(c *cobra.Command, args []string) error {
		path, err := fillPath(e.Path, e.Args, args)
		if err != nil {
			return err
		}

		query := url.Values{}
		body := map[string]any{}
		for _, fl := range e.Flag {
			if !c.Flags().Changed(fl.Name) {
				continue
			}
			var value any
			switch fl.Kind {
			case flagInt:
				value = *ints[fl.Name]
			case flagBool:
				value = *bools[fl.Name]
			case flagStrings:
				value = *slices[fl.Name]
			default:
				value = *strs[fl.Name]
			}
			if fl.Query {
				query.Set(fl.key(), queryString(value))
				continue
			}
			if err := assign(body, fl.key(), value); err != nil {
				return err
			}
		}

		raw, err := bodyFromArg(f.IO.In, input)
		if err != nil {
			return err
		}
		fields, err := buildFields(rawField, typField, f.IO.In)
		if err != nil {
			return err
		}
		for k, v := range fields {
			body[k] = v
		}

		var payload []byte
		switch {
		case raw != nil && len(body) > 0:
			// Merging a literal body with flags would silently pick a winner.
			return fmt.Errorf("pass --input or the field flags, not both")
		case raw != nil:
			if !json.Valid(raw) {
				return fmt.Errorf("the request body is not valid JSON")
			}
			payload = raw
		case len(body) > 0:
			payload, err = json.Marshal(body)
			if err != nil {
				return err
			}
		case e.Body == bodyRequired:
			return fmt.Errorf("%s %s needs a body.\nSupply one with the flags above, with -f key=value, or with --input @file.json", r.Name, e.Name)
		case e.Body == bodyOptional && e.Method != http.MethodGet:
			payload = []byte("{}")
		}

		if e.Sends {
			if err := f.ConfirmSend(fmt.Sprintf("`warmbly %s %s`", r.Name, e.Name)); err != nil {
				return err
			}
		} else if e.Method == http.MethodDelete {
			if err := f.ConfirmMutation(fmt.Sprintf("Run `warmbly %s %s`", r.Name, e.Name)); err != nil {
				return err
			}
		}

		client, err := f.Client()
		if err != nil {
			return err
		}
		req := api.Request{
			Method:         e.Method,
			Path:           path,
			Query:          query,
			Body:           payload,
			IdempotencyKey: idemKey,
		}

		printer := f.Printer()
		if all {
			merged, perr := client.Paginate(c.Context(), req, maxPages)
			if perr != nil {
				return perr
			}
			return printer.Print(merged, e.Table)
		}

		resp, err := client.Do(c.Context(), req)
		if err != nil {
			return err
		}
		// Nothing to tabulate and a terminal to talk to: say what happened
		// rather than printing an empty object.
		if len(e.Table.Columns) == 0 && e.Success != "" && !printer.JSON && printer.Template == "" && f.IO.IsStdoutTTY() {
			f.IO.Printf("%s %s\n", f.IO.Tick(), e.Success)
			return nil
		}
		return printer.Print(resp.Body, e.Table)
	}

	return cmd
}

// fillPath substitutes positional arguments into the {} markers, in order.
func fillPath(path string, specs []argSpec, args []string) (string, error) {
	for i, spec := range specs {
		marker := "{" + spec.Name + "}"
		if !strings.Contains(path, marker) {
			return "", fmt.Errorf("internal: path %q has no %s", path, marker)
		}
		value := strings.TrimSpace(args[i])
		if value == "" {
			return "", fmt.Errorf("<%s> cannot be empty", spec.Name)
		}
		path = strings.ReplaceAll(path, marker, url.PathEscape(value))
	}
	if strings.Contains(path, "{") {
		return "", fmt.Errorf("internal: path %q still has an unfilled placeholder", path)
	}
	return path, nil
}

func queryString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprint(t)
	case []string:
		return strings.Join(t, ",")
	default:
		return fmt.Sprint(t)
	}
}
