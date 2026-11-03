package config

import (
	"os"
	"strconv"
	"strings"
)

// Automatic inbox tagging is OPTIONAL and off by default.
//
// It sends message content to TypeSafe, so an operator has to supply a key and
// turn the feature on. An instance that sets neither never calls the service.
//
// TYPESAFE_API_KEY       the key. No key means the feature cannot run.
// INBOX_TAGGING_ENABLED  the switch. Default false even when a key is present,
//
//	so a key configured for a staging trial does not
//	silently start classifying production mail.
func TypeSafeAPIKey() string {
	return strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY"))
}

// TypeSafeEndpoint overrides the TypeSafe API URL; empty keeps the default.
func TypeSafeEndpoint() string {
	return strings.TrimSpace(os.Getenv("TYPESAFE_ENDPOINT"))
}

// InboxTaggingEnabled reports whether the feature should run. Both halves are
// required, and the key check is here rather than at the call site so there is
// one answer to "is this on" for the API, the consumer and the dashboard.
func InboxTaggingEnabled() bool {
	if TypeSafeAPIKey() == "" {
		return false
	}
	v, err := strconv.ParseBool(strings.TrimSpace(os.Getenv("INBOX_TAGGING_ENABLED")))
	return err == nil && v
}
