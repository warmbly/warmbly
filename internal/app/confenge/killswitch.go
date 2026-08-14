package confenge

import (
	"os"
	"path/filepath"
	"strings"
)

// Env and defaults for the operational kill switch.
const (
	EnvSendingPaused  = "CONFENGE_SENDING_PAUSED"
	EnvKillSwitchPath = "CONFENGE_KILL_SWITCH_PATH"
	DefaultKillPath   = "data/confenge-kill-switch"
)

// KillSwitchPath resolves the file used by make confenge-stop-sending.
func KillSwitchPath() string {
	p := strings.TrimSpace(os.Getenv(EnvKillSwitchPath))
	if p == "" {
		return DefaultKillPath
	}
	return p
}

// FileKillSwitchActive reports whether the operator kill-switch file exists.
func FileKillSwitchActive() bool {
	_, err := os.Stat(KillSwitchPath())
	if err == nil {
		return true
	}
	// Only a confirmed absence permits transport. Permission, path and
	// filesystem failures must not turn a safety control into fail-open.
	return !os.IsNotExist(err)
}

// EngageKillSwitch writes the kill-switch file (idempotent).
func EngageKillSwitch() error {
	path := KillSwitchPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("paused\n"), 0o600)
}

// ReleaseKillSwitch removes the kill-switch file (idempotent).
func ReleaseKillSwitch() error {
	err := os.Remove(KillSwitchPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SendingAllowed reports whether CONFENGE outbound enroll/send may proceed.
func (c Config) SendingAllowed() bool {
	if c.SendingPaused {
		return false
	}
	if FileKillSwitchActive() {
		return false
	}
	return true
}
