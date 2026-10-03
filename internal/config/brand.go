package config

import (
	"os"
	"strings"
)

// Brand is the deployment's own identity in everything it shows the outside
// world: the transactional email footer, the sign-in screen, public form pages,
// a shared stats card.
//
// The hosted defaults are hosted-only. A self-host that set no EMAIL_BRAND_*
// gets empty strings, and every surface that reads them renders nothing rather
// than another company's name, terms and website. Those are not a sensible
// fallback for somebody else's server: they send that operator's users and
// leads to a site with no relationship to the mail or the form they just saw.
//
// EMAIL_BRAND_* is the historic prefix (the footer was the first surface) and
// stays the name of the setting, because renaming it would silently un-brand
// every install that already sets it.
type BrandConfig struct {
	// Name is the product's name, which is true of a self-host too, so unlike
	// everything else here it keeps its default.
	Name string

	// The Companies Act 2006 identification line, hosted-only.
	LegalEntity   string
	CompanyNumber string
	PlaceOfReg    string
	Address       string

	// Public links and the support address, hosted-only.
	WebsiteURL   string
	TermsURL     string
	PrivacyURL   string
	SupportEmail string
}

// Brand resolves the deployment's branding from the environment.
func Brand() BrandConfig {
	return BrandConfig{
		Name:          brandEnv("EMAIL_BRAND_NAME", "Cloudsnow"),
		LegalEntity:   brandEnv("EMAIL_BRAND_LEGAL_ENTITY", hostedOnly("Mindroot Ltd")),
		CompanyNumber: brandEnv("EMAIL_BRAND_COMPANY_NUMBER", hostedOnly("16543299")),
		PlaceOfReg:    brandEnv("EMAIL_BRAND_PLACE_OF_REG", hostedOnly("England and Wales")),
		Address:       brandEnv("EMAIL_BRAND_ADDRESS", hostedOnly("71-75 Shelton Street, London, England, WC2H 9JQ")),
		WebsiteURL:    brandEnv("EMAIL_BRAND_WEBSITE_URL", hostedOnly("https://warmbly.com")),
		TermsURL:      brandEnv("EMAIL_BRAND_TERMS_URL", hostedOnly("https://warmbly.com/terms")),
		PrivacyURL:    brandEnv("EMAIL_BRAND_PRIVACY_URL", hostedOnly("https://warmbly.com/privacy")),
		SupportEmail:  brandEnv("EMAIL_BRAND_SUPPORT_EMAIL", hostedOnly("team@warmbly.com")),
	}
}

// WebsiteLabel is the display text for a link to WebsiteURL, derived from the
// URL so a rebranded install never renders "warmbly.com" pointing elsewhere.
func (b BrandConfig) WebsiteLabel() string {
	label := strings.TrimPrefix(strings.TrimPrefix(b.WebsiteURL, "https://"), "http://")
	return strings.TrimSuffix(label, "/")
}

func brandEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// hostedOnly is a default that only applies to the hosted service.
func hostedOnly(def string) string {
	if SelfHosted() {
		return ""
	}
	return def
}
