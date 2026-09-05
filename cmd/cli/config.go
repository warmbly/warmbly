package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/warmbly/warmbly/internal/cli/config"
)

func newConfigCmd(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config <command>",
		Short:   "Read and write the CLI's own settings",
		GroupID: groupSetup,
		Long: `Read and write ` + config.ConfigPath() + `.

These are preferences, not credentials: credentials live in hosts.yml and are
managed with ` + "`warmbly auth`" + `.`,
	}

	get := &cobra.Command{
		Use:   "get <key>",
		Short: "Print one setting",
		Args:  cobra.ExactArgs(1),
		RunE:  func(*cobra.Command, []string) error { return nil },
	}
	get.RunE = func(c *cobra.Command, args []string) error {
		cfg, err := f.Config()
		if err != nil {
			return err
		}
		if !knownKey(args[0]) {
			return unknownKey(args[0])
		}
		f.IO.Println(cfg.Get(args[0]))
		return nil
	}

	set := &cobra.Command{
		Use:     "set <key> <value>",
		Short:   "Change one setting",
		Example: "  $ warmbly config set output json\n  $ warmbly config set confirm always",
		Args:    cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			if err := cfg.Set(args[0], args[1]); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			f.IO.Errorf("%s %s = %s\n", f.IO.Tick(), args[0], args[1])
			return nil
		},
	}

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Show every setting and what it does",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			for _, k := range config.Keys {
				value := cfg.Get(k.Name)
				if value == "" {
					value = f.IO.Gray("(unset, " + k.Default + ")")
				}
				f.IO.Printf("%-14s %s\n", k.Name, value)
				f.IO.Printf("%-14s %s\n", "", f.IO.Gray(k.Help))
			}
			f.IO.Println()
			f.IO.Printf("%s %s\n", f.IO.Gray("config:"), config.ConfigPath())
			f.IO.Printf("%s %s\n", f.IO.Gray("hosts: "), config.HostsPath())
			return nil
		},
	}

	clear := &cobra.Command{
		Use:   "clear <key>",
		Short: "Reset one setting to its default",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			if !knownKey(args[0]) {
				return unknownKey(args[0])
			}
			// Set validates values, so clearing goes through the zero value
			// directly rather than through a value it would reject.
			switch args[0] {
			case "active_host":
				cfg.ActiveHost = ""
			case "output":
				cfg.Output = ""
			case "confirm":
				cfg.Confirm = ""
			case "pager":
				cfg.Pager = ""
			case "browser":
				cfg.Browser = ""
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			f.IO.Errorf("%s %s reset\n", f.IO.Tick(), args[0])
			return nil
		},
	}

	cmd.AddCommand(get, set, list, clear)
	return cmd
}

func knownKey(name string) bool {
	for _, k := range config.Keys {
		if k.Name == name {
			return true
		}
	}
	return false
}

func unknownKey(name string) error {
	names := make([]string, 0, len(config.Keys))
	for _, k := range config.Keys {
		names = append(names, k.Name)
	}
	sort.Strings(names)
	return fmt.Errorf("unknown config key %q. Settable keys: %s", name, strings.Join(names, ", "))
}
