package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/warmbly/warmbly/internal/cli/config"
	"github.com/warmbly/warmbly/internal/cli/update"
	"github.com/warmbly/warmbly/internal/version"
)

func newUpgradeCmd(f *Factory) *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:     "upgrade",
		Aliases: []string{"update", "self-update"},
		Short:   "Update the CLI to the newest release",
		GroupID: groupSetup,
		Long: `Replace this binary with the newest published release.

When the CLI came from a package manager it says which command to run instead,
because overwriting a file Homebrew or Scoop owns produces a version that
reverts on their next upgrade.`,
		Example: `  $ warmbly upgrade
  $ warmbly upgrade --check`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return runUpgrade(c.Context(), f, check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Only report whether a newer release exists")
	return cmd
}

func runUpgrade(ctx context.Context, f *Factory, checkOnly bool) error {
	io := f.IO
	current := version.String()

	io.Errorf("%s\n", io.Gray("Checking for a newer release"))
	latest, err := update.LatestVersion(ctx, 15*time.Second)
	if err != nil {
		return fmt.Errorf("could not reach the release feed: %w", err)
	}

	// The state file is refreshed here too, so an explicit check silences the
	// automatic reminder for the next day.
	state := config.LoadState()
	state.LastUpdateCheck = time.Now().UTC()
	state.LatestVersion = latest
	_ = state.Save()

	if !update.IsNewer(current, latest) {
		if current == latest {
			io.Printf("%s warmbly %s is the newest release\n", io.Tick(), io.Bold(current))
		} else {
			// A dev build is not behind, it is simply not one of ours.
			io.Printf("%s running %s; the newest release is %s\n", io.Tick(), io.Bold(current), io.Bold(latest))
		}
		return nil
	}

	io.Printf("%s %s is available (you have %s)\n", io.Yellow("↑"), io.Bold(latest), current)
	if checkOnly {
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not find this binary on disk: %w", err)
	}
	if cmd := update.DetectMethod(executable).UpgradeCommand(); cmd != "" {
		io.Println()
		io.Printf("This CLI was installed with a package manager. Upgrade it with:\n\n    %s\n", io.Bold(cmd))
		return nil
	}

	if !f.AssumeYes && io.IsStdinTTY() {
		ok, cerr := io.Confirm(fmt.Sprintf("Replace %s with %s?", executable, latest), true)
		if cerr != nil {
			return cerr
		}
		if !ok {
			return errCancelled
		}
	}

	if err := update.Replace(ctx, executable, func(step string) {
		io.Errorf("%s %s\n", io.Gray("…"), step)
	}); err != nil {
		return err
	}

	io.Printf("%s Upgraded to %s\n", io.Tick(), io.Bold(latest))
	return nil
}

// nudgeAboutUpdates prints a one-line reminder after a command has finished,
// at most once a day, and only when someone is watching.
//
// It runs after the command's own output so it never delays a result, and
// every failure is swallowed: a version check is not worth an error message,
// and an air-gapped machine must not pay for one on every run.
func nudgeAboutUpdates(ctx context.Context, f *Factory) {
	if !f.IO.IsStdoutTTY() || !f.IO.IsStdinTTY() {
		return
	}
	if f.JSONOut || os.Getenv("WARMBLY_NO_UPDATE_CHECK") != "" || os.Getenv("CI") != "" {
		return
	}

	state := config.LoadState()
	if latest := state.LatestVersion; latest != "" && update.IsNewer(version.String(), latest) {
		f.IO.Errorf("\n%s %s is available. Run %s\n",
			f.IO.Yellow("↑"), f.IO.Bold(latest), f.IO.Bold("warmbly upgrade"))
	}
	if time.Since(state.LastUpdateCheck) < update.CheckInterval {
		return
	}

	// Two seconds is the whole budget: this happens after the user already has
	// what they asked for, and a slow network must not make the CLI feel slow.
	latest, err := update.LatestVersion(ctx, 2*time.Second)
	if err != nil {
		return
	}
	state.LastUpdateCheck = time.Now().UTC()
	state.LatestVersion = latest
	_ = state.Save()
}
