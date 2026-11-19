// How a mailbox is connected: bought in the store, the inbox vendor it came from, the admin grant it signs in through, or its host.
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import { vendorLabel } from "@/lib/api/models/app/emails/MailboxSources";
import { mailboxConnectionLabel, mailHostLogo } from "@/lib/mailHost";

export type MailboxSourceBox = Pick<Inbox, "provider" | "mail_host" | "auth_method" | "vendor" | "purchased">;

export interface MailboxSource {
    kind: "purchased" | "vendor" | "grant" | "host";
    /** The id ProviderLogo draws. */
    logo: string;
    label: string;
    title: string;
}

export function mailboxSource(box: MailboxSourceBox): MailboxSource {
    const connection = mailboxConnectionLabel(box);
    if (box.purchased) {
        const brand = mailboxBrand(box);
        const logo = brand === "smtp_imap" ? box.mail_host || box.provider || "" : brand;
        return { kind: "purchased", logo, label: "Purchased", title: `Bought in Warmbly${connection ? ` · ${connection}` : ""}` };
    }
    if (box.vendor) {
        const name = vendorLabel(box.vendor);
        return { kind: "vendor", logo: box.vendor, label: `via ${name}`, title: `Imported from ${name}${connection ? ` · ${connection}` : ""}` };
    }
    if (box.auth_method === "delegated") {
        const microsoft = mailHostLogo(box.mail_host) === "microsoft" || box.provider === "outlook";
        return {
            kind: "grant",
            logo: microsoft ? "microsoft" : "google",
            label: "Admin grant",
            title: `Connected through a ${microsoft ? "Microsoft 365" : "Google Workspace"} admin grant`,
        };
    }
    return { kind: "host", logo: box.mail_host || box.provider || "", label: connection, title: connection };
}

/** The mark a mailbox's avatar shows: Google, Microsoft, or a generic SMTP server. */
export function mailboxBrand(box: Pick<Inbox, "provider" | "mail_host">): "google" | "microsoft" | "smtp_imap" {
    return mailHostLogo(box.mail_host) ?? (box.provider === "gmail" ? "google" : box.provider === "outlook" ? "microsoft" : "smtp_imap");
}
