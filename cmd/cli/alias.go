package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// Aliases are plain command lines stored in config.yml. They are expanded
// before cobra sees the arguments, so an alias can carry flags and the user
// can still add more on the end.
func newAliasCmd(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "alias <command>",
		Short:   "Shortcuts for command lines you type often",
		GroupID: groupSetup,
		Long: `Save a command line under a shorter name.

Anything after the alias on the command line is appended, so an alias can be a
starting point rather than a fixed command.`,
		Example: `  $ warmbly alias set hot "campaign list --status active"
  $ warmbly hot --json`,
	}

	set := &cobra.Command{
		Use:   "set <name> <expansion>",
		Short: "Create or replace an alias",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			name, expansion := args[0], args[1]
			if strings.ContainsAny(name, " \t") {
				return fmt.Errorf("an alias name cannot contain spaces")
			}
			// Shadowing a real command would make it unreachable, and the
			// person who did it would have no way to tell why.
			for _, existing := range c.Root().Commands() {
				if existing.Name() == name {
					return fmt.Errorf("%q is already a warmbly command, so an alias for it would hide it", name)
				}
			}
			if _, err := splitArgs(expansion); err != nil {
				return err
			}
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			if cfg.Aliases == nil {
				cfg.Aliases = map[string]string{}
			}
			cfg.Aliases[name] = expansion
			if err := cfg.Save(); err != nil {
				return err
			}
			f.IO.Errorf("%s %s = %s\n", f.IO.Tick(), name, expansion)
			return nil
		},
	}

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Show every alias",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			if len(cfg.Aliases) == 0 {
				f.IO.Println(f.IO.Gray("No aliases yet. Try: warmbly alias set hot \"campaign list --status active\""))
				return nil
			}
			names := make([]string, 0, len(cfg.Aliases))
			for name := range cfg.Aliases {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				f.IO.Printf("%-12s %s\n", name, cfg.Aliases[name])
			}
			return nil
		},
	}

	del := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm"},
		Short:   "Remove an alias",
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			if _, ok := cfg.Aliases[args[0]]; !ok {
				return fmt.Errorf("no alias called %q", args[0])
			}
			delete(cfg.Aliases, args[0])
			if err := cfg.Save(); err != nil {
				return err
			}
			f.IO.Errorf("%s removed %s\n", f.IO.Tick(), args[0])
			return nil
		},
	}

	cmd.AddCommand(set, list, del)
	return cmd
}
