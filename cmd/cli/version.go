package main

import (
	"encoding/json"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/warmbly/warmbly/internal/version"
)

func newVersionCmd(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Print the CLI's version",
		GroupID: groupSetup,
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			info := version.Current()
			if f.JSONOut {
				payload, err := json.MarshalIndent(map[string]string{
					"version": info.Version,
					"commit":  info.Commit,
					"built":   info.BuiltAt,
					"go":      runtime.Version(),
					"os":      runtime.GOOS,
					"arch":    runtime.GOARCH,
				}, "", "  ")
				if err != nil {
					return err
				}
				f.IO.Println(string(payload))
				return nil
			}
			f.IO.Printf("warmbly %s (%s/%s)\n", info.Version, runtime.GOOS, runtime.GOARCH)
			if c := version.ShortCommit(); c != "" {
				f.IO.Printf("commit %s\n", c)
			}
			if info.BuiltAt != "" {
				f.IO.Printf("built %s\n", info.BuiltAt)
			}
			return nil
		},
	}
}
