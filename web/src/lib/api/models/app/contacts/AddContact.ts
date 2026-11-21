import type { ContactSocialPatch } from "@/lib/api/models/app/social/Social";

export default interface AddContact {
    first_name: string;
    last_name: string;
    email: string;
    company: string;
    phone: string;
    social?: ContactSocialPatch;
    campaigns: string[];
    categories?: string[];

    // A verdict you already hold for the address, in Warmbly's vocabulary or
    // any known provider's (ZeroBounce, MillionVerifier, NeverBounce, ...).
    verification_status?: string;
    verification_provider?: string;
    custom_fields: Record<string, string>;

    // First-touch source hint. The dashboard may say "manual" or "campaign"
    // (added from a campaign's Leads tab); the server decides everything else.
    source?: "manual" | "campaign";
}
