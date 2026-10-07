package mailhdr

import (
	"net/mail"
	"net/url"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

// ReceivedEvidence records bounded claims; mailbox retrieval is not header attestation.
func ReceivedEvidence(headers map[string][]string, source string) *models.ReceivedEvidence {
	e := models.UnknownReceivedEvidence()
	e.Source, e.Trust = source, "unverified_headers"
	normalized := make(map[string][]string)
	for key, values := range headers {
		normalized[strings.ToLower(key)] = append(normalized[strings.ToLower(key)], values...)
	}
	e.AuthenticationResultsPresent = len(normalized["authentication-results"]) > 0
	addressDomain := func(name string) string {
		v := normalized[name]
		if len(v) != 1 || len(v[0]) > 1024 {
			return ""
		}
		a, err := mail.ParseAddress(v[0])
		if err != nil {
			return ""
		}
		_, domain, ok := strings.Cut(a.Address, "@")
		if !ok {
			return ""
		}
		return safeClaim(domain)
	}
	e.FromDomain, e.EnvelopeDomain = addressDomain("from"), addressDomain("return-path")
	post := normalized["list-unsubscribe-post"]
	list := normalized["list-unsubscribe"]
	if len(post) == 1 && len(list) == 1 && strings.EqualFold(strings.TrimSpace(post[0]), "List-Unsubscribe=One-Click") && len(list[0]) <= 2048 {
		httpsURIs := 0
		for _, candidate := range strings.Split(list[0], ",") {
			candidate = strings.TrimSpace(candidate)
			if !strings.HasPrefix(candidate, "<") || !strings.HasSuffix(candidate, ">") {
				continue
			}
			u, err := url.Parse(strings.Trim(candidate, "<>"))
			if err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil {
				httpsURIs++
			}
		}
		e.OneClickHeaders = httpsURIs == 1
	}
	for _, signature := range normalized["dkim-signature"] {
		if len(signature) > 8192 {
			continue
		}
		tags := make(map[string]string)
		duplicate := false
		for _, part := range strings.Split(signature, ";") {
			key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				continue
			}
			key = strings.ToLower(key)
			if _, exists := tags[key]; exists {
				duplicate = true
			}
			tags[key] = strings.TrimSpace(value)
		}
		if duplicate {
			continue
		}
		if e.SigningDomain == "" {
			e.SigningDomain, e.Selector = safeClaim(tags["d"]), safeClaim(tags["s"])
		}
		var listSigned, postSigned bool
		for _, name := range strings.Split(strings.ToLower(tags["h"]), ":") {
			switch strings.TrimSpace(name) {
			case "list-unsubscribe":
				listSigned = true
			case "list-unsubscribe-post":
				postSigned = true
			}
		}
		if e.OneClickHeaders && listSigned && postSigned {
			e.OneClickSigningClaim = true
		}
	}
	return e
}

func safeClaim(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) == 0 || len(value) > 253 {
		return ""
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '.' || char == '-' || char == '_') {
			return ""
		}
	}
	return value
}
