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
	// Theme is read only by the agency PUT; nil keeps the stored theme, a value replaces it whole.
	Theme *PortalTheme `json:"theme,omitempty"`
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
	// IconURL and SignInImageURL are set only by their upload routes.
	IconURL        string      `json:"icon_url"`
	SignInImageURL string      `json:"sign_in_image_url"`
	Theme          PortalTheme `json:"theme"`
}

// AgencyBrandImage names one uploaded image on an agency's brand.
type AgencyBrandImage string

const (
	AgencyBrandLogo        AgencyBrandImage = "logo"
	AgencyBrandIcon        AgencyBrandImage = "icon"
	AgencyBrandSignInImage AgencyBrandImage = "sign_in_image"
)

// PortalTheme is how an agency's portal looks and reads beyond its name, logo and accent, stored as agency_brands.theme.
// Every field is optional and an empty one means the default.
type PortalTheme struct {
	Font             PortalFont             `json:"font,omitempty"`
	Corners          PortalCorners          `json:"corners,omitempty"`
	Appearance       PortalAppearance       `json:"appearance,omitempty"`
	SignInLayout     PortalSignInLayout     `json:"sign_in_layout,omitempty"`
	SignInBackground PortalSignInBackground `json:"sign_in_background,omitempty"`
	SignInHeading    string                 `json:"sign_in_heading,omitempty"`
	SignInMessage    string                 `json:"sign_in_message,omitempty"`
	SupportEmail     string                 `json:"support_email,omitempty"`
	SupportURL       string                 `json:"support_url,omitempty"`
	TermsURL         string                 `json:"terms_url,omitempty"`
	PrivacyURL       string                 `json:"privacy_url,omitempty"`
}

// Empty reports whether the theme changes nothing.
func (t PortalTheme) Empty() bool { return t == PortalTheme{} }

// PortalFont is the typeface the portal is set in.
type PortalFont string

const (
	PortalFontSystem    PortalFont = "system"
	PortalFontGeometric PortalFont = "geometric"
	PortalFontSerif     PortalFont = "serif"
)

func (f PortalFont) Valid() bool {
	return f == "" || f == PortalFontSystem || f == PortalFontGeometric || f == PortalFontSerif
}

// PortalCorners is how rounded the portal's buttons, fields and cards are.
type PortalCorners string

const (
	PortalCornersSharp PortalCorners = "sharp"
	PortalCornersRound PortalCorners = "round"
)

func (c PortalCorners) Valid() bool {
	return c == "" || c == PortalCornersSharp || c == PortalCornersRound
}

// PortalAppearance is the colour scheme a client sees before choosing one.
type PortalAppearance string

const (
	PortalAppearanceLight PortalAppearance = "light"
	PortalAppearanceDark  PortalAppearance = "dark"
)

func (a PortalAppearance) Valid() bool {
	return a == "" || a == PortalAppearanceLight || a == PortalAppearanceDark
}

// PortalSignInLayout is how the portal's sign-in page is arranged.
type PortalSignInLayout string

const (
	PortalSignInLayoutSplit PortalSignInLayout = "split"
	PortalSignInLayoutCover PortalSignInLayout = "cover"
)

func (l PortalSignInLayout) Valid() bool {
	return l == "" || l == PortalSignInLayoutSplit || l == PortalSignInLayoutCover
}

// PortalSignInBackground is what fills the sign-in page behind or beside the form.
type PortalSignInBackground string

const (
	PortalSignInBackgroundAccent PortalSignInBackground = "accent"
	PortalSignInBackgroundDark   PortalSignInBackground = "dark"
	PortalSignInBackgroundImage  PortalSignInBackground = "image"
)

func (b PortalSignInBackground) Valid() bool {
	return b == "" || b == PortalSignInBackgroundAccent || b == PortalSignInBackgroundDark || b == PortalSignInBackgroundImage
}
