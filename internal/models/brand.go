package models

import (
	"time"

	"github.com/google/uuid"
)

// BrandAssetKeyPrefix is where brand logos live: brand-assets/<organization or agency id>/<name>.<ext>, served from /public.
const BrandAssetKeyPrefix = "brand-assets/"

// BrandSettings is one stored brand: a workspace's (workspace_brands) or an agency's (agency_brands).
type BrandSettings struct {
	DisplayName   string     `json:"display_name"`
	LogoURL       string     `json:"logo_url"`
	AccentColor   string     `json:"accent_color"`
	WebsiteURL    string     `json:"website_url"`
	HidePoweredBy bool       `json:"hide_powered_by"`
	UpdatedAt     time.Time  `json:"updated_at"`
	UpdatedBy     *uuid.UUID `json:"updated_by,omitempty"`
}

// Set reports whether the brand names anything, so it replaces the one below it.
func (b *BrandSettings) Set() bool {
	return b != nil && (b.DisplayName != "" || b.LogoURL != "" || b.AccentColor != "" || b.WebsiteURL != "")
}

// BrandInput is the body of PUT /organization/brand and PUT /agency/brand.
type BrandInput struct {
	DisplayName   string `json:"display_name"`
	AccentColor   string `json:"accent_color"`
	WebsiteURL    string `json:"website_url"`
	HidePoweredBy bool   `json:"hide_powered_by"`
	// PortalSignIn is read only by the agency PUT; nil keeps the stored choice.
	PortalSignIn *PortalSignIn `json:"portal_sign_in,omitempty"`
}

// PortalSignIn is where an agency's clients sign in to its portal domain.
type PortalSignIn string

const (
	// PortalSignInPortal signs clients in on the agency's own domain.
	PortalSignInPortal PortalSignIn = "portal"
	// PortalSignInWarmbly signs them in on the main dashboard address, which hands the portal a session.
	PortalSignInWarmbly PortalSignIn = "warmbly"
)

func (p PortalSignIn) Valid() bool {
	return p == PortalSignInPortal || p == PortalSignInWarmbly
}

// AgencyBrand is an agency's brand plus the portal host every client workspace's public pages answer on.
type AgencyBrand struct {
	AgencyID uuid.UUID `json:"agency_id"`
	BrandSettings
	PortalDomain           string       `json:"portal_domain"`
	PortalDomainVerified   bool         `json:"portal_domain_verified"`
	PortalDomainVerifiedAt *time.Time   `json:"portal_domain_verified_at,omitempty"`
	PortalSignIn           PortalSignIn `json:"portal_sign_in"`
}
