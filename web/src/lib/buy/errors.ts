// Every store refusal in Warmbly's own words, in one place. A code not listed gets the generic sentence,
// never the server's raw message, so nothing from behind the store reaches the screen.
import type { AppError } from "@/lib/api/client/normalizeError";
import type { Quote } from "@/lib/api/models/app/store/Store";
import { PURCHASED_LOCKED } from "./purchased";
import { REGISTRANT_FIELDS, type RegistrantField } from "./registrant";

const COPY: Record<string, string> = {
    store_disabled: "Buying domains and mailboxes is not available on this workspace.",
    store_sales_paused: "Buying new mailboxes is paused for now. It will be available again soon.",
    store_unavailable: "The store is taking a moment. Try again shortly.",
    rate_limited: "Too many requests at once. Wait a moment and try again.",
    registrant_required: "Add the domain registrant's details to see the total.",
    contact_invalid: "Some registrant details were not accepted. Check the highlighted fields.",
    quote_not_found: "Those prices lapsed. Review the new total.",
    price_changed: "Prices updated since you started. Review the new total.",
    payment_method_required: "Add a card to finish paying.",
    card_declined: "Your card did not go through, so nothing was charged. Update it under Billing and try again.",
    authentication_required: "Your bank wants to confirm this payment. Update the card under Billing and try again.",
    payment_failed: "The payment did not go through, so nothing was charged. Try again or update the card under Billing.",
    payment_processing: "Your card payment is still going through. Nothing was ordered yet. Try again in a minute.",
    charge_in_progress: "Another payment is still going through. Try again in a minute.",
    insufficient_balance: "The balance cannot cover this. Add money under Billing and try again.",
    store_items_invalid: "Something in the cart cannot be ordered as it is. Check the domains and addresses.",
    invalid_query: "Enter a word or a domain name to search for.",
    invalid_cursor: "That list could not be read. Refresh and try again.",
    invalid_limit: "That list could not be read. Refresh and try again.",
    forwarding_url_invalid: "Enter a full https:// address for the domain to forward to.",
    domain_unavailable: "That domain was just taken. Pick another one.",
    domain_premium: "That domain is a premium name, which the store does not sell. Try a variation.",
    tld_unsupported: "That domain ending is not offered. Try another one.",
    provider_mixed: "A domain can use either Google or Microsoft, not both.",
    provider_unavailable: "Mailboxes from that provider aren't available right now. Choose another provider.",
    mailbox_exists: "One of these addresses already exists. Change it and try again.",
    local_part_invalid: "One of the addresses cannot be used. Use letters, digits, dots, dashes or underscores.",
    mailbox_limit: "That is more mailboxes than one domain can hold.",
    domain_not_ready: "That domain is still being set up. Try again in a few minutes.",
    domain_not_renewable: "This domain cannot be renewed here.",
    renewal_not_due: "This domain cannot be renewed yet. Renewal opens closer to its expiry.",
    renewal_window_closed: "This domain can no longer be renewed here. Nothing was charged.",
    renewal_in_progress: "This domain is already being renewed. The new date shows once it lands.",
    transfer_locked: "This domain cannot leave yet. Domains stay put for 60 days after registration or a transfer.",
    mailbox_not_cancellable: "This mailbox cannot be changed that way now. Refresh to see where it stands.",
    reauth_required: "Confirm it is you, then try again.",
    invalid_name: "That name cannot be used. Use a plain name, without links or addresses.",
    idempotency_conflict: "This was already submitted. Refresh to see where it stands.",
    not_found: "That could not be found. It may belong to another workspace.",
    plan_required: "Buying domains and mailboxes needs a Warmbly plan. Choose a plan to continue.",
    domain_registered_elsewhere: "This domain is registered elsewhere, so renewals and transfers happen at its registrar.",
    domain_registered_here: "This domain was bought through Warmbly. Transfer it out instead of removing it.",
    domain_in_use: "This domain still has mailboxes. Cancel them first, then remove the domain.",
    dns_record_managed: "Warmbly keeps that record for mail delivery, so it cannot be changed.",
    dns_record_conflict: "That record clashes with one mail needs. Remove or change the clashing record first.",
    dns_record_invalid: "That record is not valid. Check the name and the value.",
    dns_not_hosted: "Your DNS provider holds this domain's records. Change them there.",
    sign_in_unavailable: "This mailbox has no sign-in to show.",
    store_managed_by_partner: "This workspace's domains and mailboxes are bought through its partner.",
    store_state_conflict: "That cannot be done right now. Refresh to see where it stands.",
    purchased_mailbox: PURCHASED_LOCKED,
    photo_invalid: "Use a PNG or JPG photo up to 2 MB.",
    photo_not_found: "That photo is no longer available. Choose it again.",
    photo_limit_reached: "Too many photos were uploaded today. Try again tomorrow.",
    photo_storage_unavailable: "Photos cannot be uploaded on this instance.",
    profile_photo_missing: "You have no profile photo to use.",
    image_unreadable: "That file could not be read as a photo. Use a PNG or JPG.",
    image_format_not_allowed: "Use a PNG or JPG photo.",
    image_too_large: "Use a photo up to 2 MB.",
    image_dimensions_too_large: "Use a photo up to 2048 px on each side.",
};

/** The reason a domain_unavailable answer gives: top-level `reason`, or `details.reason` from an older backend. */
export function unavailableReason(err: unknown): string | undefined {
    const e = (err ?? {}) as Partial<AppError>;
    if (e.code !== "domain_unavailable") return undefined;
    const r = e.reason ?? e.details?.reason;
    return typeof r === "string" ? r : undefined;
}

const UNAVAILABLE_REASON: Record<string, string> = {
    already_added: "This domain is already in this workspace.",
    registered: "This domain was bought through Warmbly, so it cannot be added again.",
    connected_elsewhere: "Another workspace is already using this domain.",
};

/** True for the 402 that means the workspace needs a plan before it can buy. */
export function isPlanRequired(err: unknown): boolean {
    return storeErrorCode(err) === "plan_required";
}

/** Why a domain cannot be connected, from details.reason on domain_unavailable. */
export function connectErrorMessage(err: unknown): string {
    const e = (err ?? {}) as Partial<AppError>;
    if (e.code === "domain_unavailable") {
        const reason = unavailableReason(e);
        if (reason && UNAVAILABLE_REASON[reason]) return UNAVAILABLE_REASON[reason];
        return "This domain cannot be added. Check the spelling, or try another one.";
    }
    if (e.code === "store_items_invalid") return "Enter a domain name, like acme.com.";
    return storeErrorMessage(err);
}

const GENERIC = "That did not go through. Try again in a moment.";

/** One friendly sentence for any store error, with the request id when there is one to quote. */
export function storeErrorMessage(err: unknown): string {
    // The fresh-sign-in prompt was closed: Request rejects with a plain Error and nothing was sent.
    if (err instanceof Error && err.message === "cancelled") return "Nothing changed. This needs you to confirm it is you.";
    const e = (err ?? {}) as Partial<AppError>;
    if (e.status === 403 && !e.code) return "You do not have permission to do that here.";
    if (e.error === "Network Error") return "You look offline. Check the connection and try again.";
    const known = e.code ? COPY[e.code] : undefined;
    if (known) return known;
    return e.request_id ? `${GENERIC} (${e.request_id})` : GENERIC;
}

export function storeErrorCode(err: unknown): string | undefined {
    return (err as Partial<AppError> | null)?.code;
}

/** The fresh quote a price_changed answer carries in details.quote. */
export function repricedQuote(err: unknown): Quote | null {
    const q = (err as Partial<AppError> | null)?.details?.quote as Partial<Quote> | undefined;
    return q && typeof q.id === "string" && typeof q.due_now_micros === "number" && Array.isArray(q.lines) ? (q as Quote) : null;
}

/** The registrant fields a contact_invalid answer refused, from details.fields (or the comma list in reason). */
export function refusedRegistrantFields(err: unknown): RegistrantField[] {
    const e = (err ?? {}) as Partial<AppError>;
    if (e.code !== "contact_invalid") return [];
    const listed = e.details?.fields;
    const raw = Array.isArray(listed) ? listed : (e.reason ?? "").split(",");
    const known = new Set<string>(REGISTRANT_FIELDS);
    return raw.filter((f): f is RegistrantField => typeof f === "string" && known.has(f.trim())).map((f) => f.trim() as RegistrantField);
}
