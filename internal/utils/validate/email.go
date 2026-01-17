package validate

import (
	"net/mail"
	"regexp"
	"strings"

	"github.com/warmbly/warmbly/internal/errx"
)

func Email(email string) bool {
	_, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	return true
}

func EmailBulk(emails []string) bool {
	for i := range emails {
		if !Email(emails[i]) {
			return false
		}
	}
	return true
}

var nameRE = regexp.MustCompile(`^\p{L}[\p{L}\p{M}\p{N}'\-\.\s]{0,98}\p{L}$`)

func EmailName(name *string) bool {
	*name = strings.TrimSpace(*name)
	if *name == "" {
		return false
	}
	runes := []rune(*name)
	if len(runes) < 2 {
		return false
	}
	if len(runes) > 100 {
		return false
	}
	if !nameRE.MatchString(*name) {
		return false
	}
	return true
}

func ValidateTrackingDomain(domain string) *errx.Error {
	if domain == "" {
		return errx.ErrEmailTrackingDomain
	}
	if len(domain) > 253 {
		return errx.ErrEmailTrackingDomainLength
	}

	domain = strings.ToLower(domain)

	domainRegex := regexp.MustCompile(`^([a-z0-9-]+\.)+[a-z]{2,}$`)

	if !domainRegex.MatchString(domain) {
		return errx.ErrEmailTrackingDomain
	}

	if strings.HasPrefix(domain, "-") || strings.HasSuffix(domain, "-") {
		return errx.ErrEmailTrackingDomain
	}

	return nil
}
