package whatsapp

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/nyaruka/phonenumbers"
)

// PhoneResult is the outcome of normalization. Original is always preserved.
type PhoneResult struct {
	Original   string `json:"original"`
	E164       string `json:"e164,omitempty"`        // +E.164
	E164Digits string `json:"e164_digits,omitempty"` // digits only (no +)
	Country    string `json:"country,omitempty"`     // ISO region, e.g. BR
	Valid      bool   `json:"valid"`
	Error      string `json:"error,omitempty"`
}

// NormalizePhone parses raw into E.164 using libphonenumber.
// defaultRegion is used when the number lacks a country code (e.g. "BR").
// Empty defaultRegion defaults to BR for CONFENGE commercial use.
func NormalizePhone(raw, defaultRegion string) PhoneResult {
	out := PhoneResult{Original: raw}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		out.Error = "empty phone"
		return out
	}
	if defaultRegion == "" {
		defaultRegion = "BR"
	}

	// Strip common noise; keep leading + for international form.
	cleaned := stripPhoneNoise(raw)

	num, err := phonenumbers.Parse(cleaned, defaultRegion)
	if err != nil {
		out.Error = fmt.Sprintf("parse: %v", err)
		return out
	}
	if !phonenumbers.IsValidNumber(num) {
		// Still surface the candidate format for operators; mark invalid.
		formatted := phonenumbers.Format(num, phonenumbers.E164)
		out.E164 = formatted
		out.E164Digits = strings.TrimPrefix(formatted, "+")
		region := phonenumbers.GetRegionCodeForNumber(num)
		out.Country = region
		out.Error = "invalid phone number"
		return out
	}
	formatted := phonenumbers.Format(num, phonenumbers.E164)
	out.E164 = formatted
	out.E164Digits = strings.TrimPrefix(formatted, "+")
	out.Country = phonenumbers.GetRegionCodeForNumber(num)
	out.Valid = true
	return out
}

// stripPhoneNoise removes spaces, parentheses, hyphens, dots, and trunk zeros
// quirks while preserving a leading +.
func stripPhoneNoise(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range s {
		if r == '+' && i == 0 {
			b.WriteRune(r)
			continue
		}
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
		// drop spaces, (), -, ., and letters like "ext"
	}
	out := b.String()
	// Brazilian operator trunk "0" after country code is handled by libphonenumber
	// when region is BR; for national forms like 048999999999, Parse with BR works.
	return out
}

// DigitsOnly strips non-digits (and leading +) for provider APIs that want raw MSISDN.
func DigitsOnly(e164 string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, e164)
}
