package config

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// State is the CLI's own bookkeeping: nothing the user sets, nothing secret.
// Kept apart from config.yml so a hand-edited config is never fighting with
// something the tool rewrites on its own.
type State struct {
	// LastUpdateCheck is when the release check last ran. It is what keeps the
	// check to once a day rather than once a command.
	LastUpdateCheck time.Time `yaml:"last_update_check,omitempty"`
	// LatestVersion is the newest release seen, so the reminder can be printed
	// without a network call on every run.
	LatestVersion string `yaml:"latest_version,omitempty"`
}

func statePath() string { return filepath.Join(Dir(), "state.yml") }

// LoadState never fails in a way a caller has to handle: a missing or corrupt
// state file means "we know nothing", which is always a safe answer.
func LoadState() *State {
	s := &State{}
	raw, err := os.ReadFile(statePath())
	if err != nil {
		return s
	}
	if err := yaml.Unmarshal(raw, s); err != nil {
		return &State{}
	}
	return s
}

// Save is best effort on purpose. This file holds a timestamp and a version
// string; a read-only home directory or a container with no writable HOME must
// cost the user a redundant version check, not a failed command.
func (s *State) Save() error {
	raw, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	if err := writeFile(statePath(), raw, 0o600); err != nil && !errors.Is(err, os.ErrPermission) {
		return err
	}
	return nil
}

// StatePath is exported for `warmbly config list`, which shows where every
// file the CLI owns lives.
func StatePath() string { return statePath() }
