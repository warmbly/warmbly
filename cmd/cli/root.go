package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// Command groups, so `warmbly --help` reads as a product rather than an
// alphabetical dump of forty nouns.
const (
	groupCore    = "core"
	groupWork    = "work"
	groupData    = "data"
	groupDevelop = "develop"
	groupSetup   = "setup"
)

func newRootCmd(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "warmbly <command> <subcommand> [flags]",
		Short: "Warmbly from the command line",
		Long: `Work with Warmbly from your terminal.

Sign in once with ` + "`warmbly auth login`" + `, then drive campaigns, contacts,
mailboxes and the inbox as yourself. Everything the CLI can do is bounded by
the scopes you approved, on the hosted service or on your own instance.`,
		Example: `  $ warmbly auth login
  $ warmbly campaign list
  $ warmbly mailbox list --json
  $ warmbly inbox list --unseen
  $ warmbly api "/campaigns?limit=10"`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// A bare `warmbly` is a request for the help, not an error.
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return c.Help()
			}
			return fmt.Errorf("unknown command %q. Run `warmbly --help` for the full list.", args[0])
		},
	}

	p := cmd.PersistentFlags()
	p.StringVar(&f.HostFlag, "host", "", "Signed-in host to use (default: the active one)")
	p.BoolVar(&f.JSONOut, "json", false, "Print the API response as JSON")
	p.StringVar(&f.Template, "template", "", "Format the response with a Go template")
	p.StringSliceVar(&f.Fields, "fields", nil, "Table columns to keep, comma separated")
	p.BoolVar(&f.AssumeYes, "yes", false, "Answer every prompt with yes, including sends")
	p.BoolVar(&f.NoColor, "no-color", false, "Never colourise output")
	p.BoolVar(&f.Debug, "debug", false, "Print each request to stderr")

	cmd.PersistentPreRun = func(*cobra.Command, []string) {
		if f.NoColor {
			f.IO.SetColor(false)
		}
	}

	cmd.AddGroup(
		&cobra.Group{ID: groupCore, Title: "Getting started"},
		&cobra.Group{ID: groupWork, Title: "Doing the work"},
		&cobra.Group{ID: groupData, Title: "Your data"},
		&cobra.Group{ID: groupDevelop, Title: "Building on Warmbly"},
		&cobra.Group{ID: groupSetup, Title: "Setting up the CLI"},
	)

	cmd.AddCommand(newAuthCmd(f))
	cmd.AddCommand(newStatusCmd(f))
	cmd.AddCommand(newBrowseCmd(f))
	cmd.AddCommand(newAPICmd(f))
	cmd.AddCommand(newEventsCmd(f))
	cmd.AddCommand(newConfigCmd(f))
	cmd.AddCommand(newAliasCmd(f))
	cmd.AddCommand(newVersionCmd(f))
	cmd.AddCommand(newUpgradeCmd(f))
	for _, rc := range resourceCommands(f) {
		cmd.AddCommand(rc)
	}

	cmd.SetOut(f.IO.Out)
	cmd.SetErr(f.IO.ErrOut)
	// The generated completion command has its own group so it does not sit
	// under "Getting started" pretending to be a first step.
	cmd.SetHelpCommandGroupID(groupSetup)
	cmd.SetCompletionCommandGroupID(groupSetup)
	return cmd
}

// expandAliases rewrites the argument list when the first word is a user
// alias. Aliases are plain command lines, so `warmbly alias set hot 'campaign
// list --status active'` makes `warmbly hot --json` work.
func expandAliases(f *Factory, args []string) []string {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return args
	}
	cfg, err := f.Config()
	if err != nil || len(cfg.Aliases) == 0 {
		return args
	}
	expansion, ok := cfg.Aliases[args[0]]
	if !ok {
		return args
	}
	parts, err := splitArgs(expansion)
	if err != nil || len(parts) == 0 {
		return args
	}
	return append(parts, args[1:]...)
}

// splitArgs is shell-ish word splitting: quotes group, nothing else is
// special. An alias is a command line, not a shell script.
func splitArgs(in string) ([]string, error) {
	var out []string
	var cur strings.Builder
	var quote rune
	started := false
	for _, r := range in {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t':
			if started || cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unbalanced quote in %q", in)
	}
	if started || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out, nil
}
