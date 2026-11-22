// What an empty Mailboxes page promises: the store only where it is on, and in a workspace a
// reseller manages, the reseller (its customers buy there) and connecting here while that works.
import type { PartnerManaged } from "@/lib/api/models/app/organizations/Organization";

export interface AddMailboxesCopy {
    body: string;
    /** The connect button's label; null when there is no connect button to show. */
    connectLabel: string | null;
}

export function addMailboxesCopy({
    partner,
    storeOn,
    writable,
}: {
    partner: PartnerManaged | null;
    storeOn: boolean;
    writable: boolean;
}): AddMailboxesCopy {
    if (partner) {
        const via = `Mailboxes are added to this workspace through ${partner.display_name}.`;
        if (!writable) return { body: via, connectLabel: null };
        return {
            body: `${via} You can also connect one you already have here; it stays off until ${partner.display_name} turns it on.`,
            connectLabel: "Connect a mailbox",
        };
    }
    if (storeOn) {
        return {
            body: "Buy a domain with mailboxes that set themselves up, or connect a mailbox you already have, to start warming up and sending campaigns.",
            connectLabel: "Connect a mailbox",
        };
    }
    return { body: "Connect a mailbox you already have to start warming up and sending campaigns.", connectLabel: "Add account" };
}
