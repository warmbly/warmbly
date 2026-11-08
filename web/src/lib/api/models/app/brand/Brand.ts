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

/** What a public page renders. */
export interface PublicBrand {
    name: string;
    logo_url?: string;
    accent_color?: string;
    website_url?: string;
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

/** The agency PUT also takes where clients sign in; absent keeps the stored choice. */
export type AgencyBrandInput = BrandInput & { portal_sign_in?: PortalSignIn };

export interface AgencyBrandView {
    brand: BrandSettings;
    full_white_label: boolean;
    can_manage: boolean;
    portal_domain: FormsDomainStatus;
    portal_base_url: string;
    powered_by?: { name: string; url?: string };
    portal_sign_in: PortalSignIn;
}
