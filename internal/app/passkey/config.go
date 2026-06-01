package passkey

import "time"

const (
	// CeremonyTTL bounds how long a begin→finish round trip may take before
	// the stored challenge expires (and is consumed single-use on finish).
	CeremonyTTL = 5 * time.Minute

	// MaxPasskeyName caps a passkey's friendly name length (in runes).
	MaxPasskeyName = 60
)
