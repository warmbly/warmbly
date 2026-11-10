// White-label brands. Mirrors models.BrandSettings and brandkit.Resolution.
import type FormsDomainStatus from "@/lib/api/models/app/forms/FormsDomain";

export interface BrandSettings {
    display_name: string;
    logo_url: string;
    /** #rrggbb, or empty for the default. */
    accent_color: string;
    /** https only, or empty. */
    website_url: string;
    hide_powered_by: boolean;
    updated_at?: string;
}

/** The fields PUT replaces; the logo has its own upload route. */
export type BrandInput = Pick<BrandSettings, "display_name" | "accent_color" | "website_url" | "hide_powered_by">;

export type PortalFont = "" | "system" | "geometric" | "serif";
export type PortalCorners = "" | "sharp" | "round";
export type PortalAppearance = "" | "light" | "dark";
/** "" is a centred card, "split" a brand panel beside the form, "cover" a full-page background under a floating card. */
export type PortalSignInLayout = "" | "split" | "cover";
/** "" is plain; "image" is the uploaded sign-in image and falls back to the accent when there is none. */
export type PortalSignInBackground = "" | "accent" | "dark" | "image";

/** How an agency's portal looks and reads. Mirrors models.PortalTheme: every field optional, empty means the default. */
export interface PortalTheme {
    font?: PortalFont;
    corners?: PortalCorners;
    /** The appearance for a viewer who has not chosen one. */
    appearance?: PortalAppearance;
    sign_in_layout?: PortalSignInLayout;
    sign_in_background?: PortalSignInBackground;
    /** Plain text, 60 characters at most. */
    sign_in_heading?: string;
    /** Plain text, 200 characters at most. */
    sign_in_message?: string;
    support_email?: string;
    /** https only, as are the two legal links. */
    support_url?: string;
    terms_url?: string;
    privacy_url?: string;
}

/** What a public page renders. */
export interface PublicBrand {
    name: string;
    logo_url?: string;
    /** The square mark for the browser tab; the logo stands in when there is none. */
    icon_url?: string;
    sign_in_image_url?: string;
    accent_color?: string;
    website_url?: string;
    theme?: PortalTheme;
    powered_by?: { name: string; url?: string };
}

export interface BrandResolution {
    brand: PublicBrand;
    source: "workspace" | "agency" | "deployment";
    agency_id?: string;
    portal_host?: string;
    /** Whether "Powered by" may be removed on this plan. */
    full_white_label: boolean;
}

export interface WorkspaceBrandView {
    brand: BrandSettings;
    /** The agency's brand shown while the workspace sets none. */
    inherited?: BrandSettings;
    effective: BrandResolution;
    portal_host: string;
    portal_source: "agency" | "shared";
    portal_base_url: string;
    /** The deployment's attribution, hidden or not; absent when it configured none. */
    powered_by?: { name: string; url?: string };
}

/** Where an agency's clients sign in: on its portal domain, or on the main address. */
export type PortalSignIn = "portal" | "warmbly";

/** The agency PUT also takes where clients sign in and the theme; absent keeps the stored one, a theme replaces all of it. */
export type AgencyBrandInput = BrandInput & { portal_sign_in?: PortalSignIn; theme?: PortalTheme };

/** An image on an agency's brand that has its own upload route. */
export type AgencyBrandImage = "logo" | "icon" | "sign-in-image";

export interface AgencyBrandView {
    brand: BrandSettings;
    full_white_label: boolean;
    can_manage: boolean;
    portal_domain: FormsDomainStatus;
    portal_base_url: string;
    powered_by?: { name: string; url?: string };
    portal_sign_in: PortalSignIn;
    icon_url: string;
    sign_in_image_url: string;
    /** Always present; {} when nothing is set. */
    theme: PortalTheme;
}
