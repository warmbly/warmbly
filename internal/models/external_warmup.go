package models

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/warmbly/warmbly/internal/config"
)

const (
	// ExternalWarmupTagMinLength keeps a tag specific enough not to match ordinary mail.
	ExternalWarmupTagMinLength = 4
	ExternalWarmupTagMaxLength = 120
	ExternalWarmupMaxTags      = 20
)

// ExternalWarmupSettings keeps warmup mail sent by another service out of a workspace's inbox.
type ExternalWarmupSettings struct {
	// HideWarmblyMarked hides mail carrying a Warmbly warmup marker this instance cannot verify.
	HideWarmblyMarked bool `json:"hide_warmbly_marked"`
	// Tags are the codes other warmup services put in each warmup email, matched case-insensitively.
	Tags []string `json:"tags"`
}

// Enabled reports whether the rule can hide anything.
func (s ExternalWarmupSettings) Enabled() bool {
	return s.HideWarmblyMarked || len(s.Tags) > 0
}

// LowerTags is the tag list in the form every match compares on.
func (s ExternalWarmupSettings) LowerTags() []string {
	out := make([]string, 0, len(s.Tags))
	for _, t := range s.Tags {
		out = append(out, strings.ToLower(t))
	}
	return out
}

// Matches reports whether a message falls under the rule.
func (s ExternalWarmupSettings) Matches(m *EmailMessageStoreData) bool {
	if m == nil {
		return false
	}
	if s.HideWarmblyMarked && HasWarmupMarker(m.Flags) {
		return true
	}
	if len(s.Tags) == 0 {
		return false
	}
	text := strings.ToLower(m.Subject + "\n" + m.Snippet + "\n" + m.BodyText)
	for _, tag := range s.LowerTags() {
		if strings.Contains(text, tag) {
			return true
		}
	}
	return false
}

// WarmupMarkerHeaders are the header names a Warmbly warmup send carries, current first.
var WarmupMarkerHeaders = []string{config.WarmupVerifyHeader, "X-Warmbly-Token"}

// HasWarmupMarker reports whether the sync recorded a Warmbly warmup header among the flags.
func HasWarmupMarker(flags []string) bool {
	for _, flag := range flags {
		name, value, ok := strings.Cut(flag, ":")
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		for _, h := range WarmupMarkerHeaders {
			if strings.EqualFold(strings.TrimSpace(name), h) {
				return true
			}
		}
	}
	return false
}

func (s *ExternalWarmupSettings) normalize() {
	// Never nil, so the API answers "tags": [] rather than null.
	out := make([]string, 0, len(s.Tags))
	for _, t := range s.Tags {
		t = strings.TrimSpace(t)
		if t == "" || slicesContainFold(out, t) {
			continue
		}
		out = append(out, t)
	}
	s.Tags = out
}

func (s ExternalWarmupSettings) validate() error {
	if len(s.Tags) > ExternalWarmupMaxTags {
		return fmt.Errorf("at most %d warmup tags can be hidden", ExternalWarmupMaxTags)
	}
	for _, t := range s.Tags {
		switch n := utf8.RuneCountInString(t); {
		case n < ExternalWarmupTagMinLength:
			return fmt.Errorf("warmup tag %q is too short: use at least %d characters so it cannot match ordinary mail", t, ExternalWarmupTagMinLength)
		case n > ExternalWarmupTagMaxLength:
			return fmt.Errorf("a warmup tag can be at most %d characters", ExternalWarmupTagMaxLength)
		}
	}
	return nil
}

func slicesContainFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}
