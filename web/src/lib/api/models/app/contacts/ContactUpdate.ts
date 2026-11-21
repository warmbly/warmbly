import type { ContactSocialPatch } from "@/lib/api/models/app/social/Social";
import type Contact from "./Contact";

export default interface ContactUpdate extends Omit<Contact, "campaigns" | "categories" | "social"> {
    campaigns: string[],
    categories: string[],
    // Channels to set; null clears one.
    social: ContactSocialPatch,
}
