// /v1/store: domains and mailboxes bought inside Warmbly. Writes that charge or change a charge
// carry an Idempotency-Key; a reauth_required answer is retried by Request after the prompt.
import type {
    ConnectMethod,
    CreateOrderRequest,
    DnsCheckResult,
    DnsRecord,
    DnsRecordInput,
    DnsRecordPatch,
    DomainSearchResponse,
    MailboxSignIn,
    MicrosoftConsentLink,
    List,
    Order,
    Quote,
    QuoteItem,
    Registrant,
    StoreDomain,
    StoreDomainPatch,
    StoreMailbox,
    StorePhoto,
    StoreStatus,
} from "@/lib/api/models/app/store/Store";
import Request from "../../Request";

const idem = (key?: string) => ({ "Idempotency-Key": key ?? crypto.randomUUID() });

export async function getStoreStatus(signal?: AbortSignal): Promise<StoreStatus> {
    return await Request<StoreStatus>({ method: "GET", url: "/store/status", authorization: true, signal });
}

/** `exactOnly` skips suggestions, which answers much sooner. */
export async function searchStoreDomains(q: string, exactOnly: boolean, signal?: AbortSignal): Promise<DomainSearchResponse> {
    const params = exactOnly ? { q, suggestions: "false" } : { q };
    return await Request<DomainSearchResponse>({ method: "GET", url: "/store/domains/search", params, authorization: true, signal });
}

export async function putRegistrant(r: Registrant): Promise<Registrant> {
    return await Request<Registrant>({ method: "PUT", url: "/store/registrant", data: r, headers: idem(), authorization: true });
}

export async function createQuote(items: QuoteItem[], signal?: AbortSignal): Promise<Quote> {
    return await Request<Quote>({ method: "POST", url: "/store/quotes", data: { items }, headers: idem(), authorization: true, signal });
}

export async function createOrder(body: CreateOrderRequest, idempotencyKey: string): Promise<Order> {
    return await Request<Order>({ method: "POST", url: "/store/orders", data: body, headers: idem(idempotencyKey), authorization: true });
}

/** Newest first; `cursor` is the previous page's next_cursor. */
export async function listOrders(cursor: string | undefined, limit: number, signal?: AbortSignal): Promise<List<Order>> {
    return await Request<List<Order>>({ method: "GET", url: "/store/orders", params: { limit, cursor: cursor || undefined }, authorization: true, signal });
}

export async function getOrder(id: string, signal?: AbortSignal): Promise<Order> {
    return await Request<Order>({ method: "GET", url: `/store/orders/${encodeURIComponent(id)}`, authorization: true, signal });
}

export async function listStoreDomains(signal?: AbortSignal): Promise<List<StoreDomain>> {
    return await Request<List<StoreDomain>>({ method: "GET", url: "/store/domains", params: { limit: 100 }, authorization: true, signal });
}

export async function getStoreDomain(id: string, signal?: AbortSignal): Promise<StoreDomain> {
    return await Request<StoreDomain>({ method: "GET", url: `/store/domains/${encodeURIComponent(id)}`, authorization: true, signal });
}

export async function patchStoreDomain(id: string, patch: StoreDomainPatch): Promise<StoreDomain> {
    return await Request<StoreDomain>({ method: "PATCH", url: `/store/domains/${encodeURIComponent(id)}`, data: patch, headers: idem(), authorization: true });
}

export async function renewStoreDomain(id: string, idempotencyKey: string): Promise<StoreDomain> {
    return await Request<StoreDomain>({ method: "POST", url: `/store/domains/${encodeURIComponent(id)}/renew`, data: {}, headers: idem(idempotencyKey), authorization: true });
}

export async function transferOutStoreDomain(id: string, idempotencyKey: string): Promise<{ auth_code: string }> {
    return await Request<{ auth_code: string }>({
        method: "POST",
        url: `/store/domains/${encodeURIComponent(id)}/transfer-out`,
        data: {},
        headers: idem(idempotencyKey),
        authorization: true,
    });
}

export async function listStoreMailboxes(signal?: AbortSignal): Promise<List<StoreMailbox>> {
    return await Request<List<StoreMailbox>>({ method: "GET", url: "/store/mailboxes", params: { limit: 100 }, authorization: true, signal });
}

export async function patchStoreMailbox(id: string, patch: { first_name?: string; last_name?: string }): Promise<StoreMailbox> {
    return await Request<StoreMailbox>({ method: "PATCH", url: `/store/mailboxes/${encodeURIComponent(id)}`, data: patch, headers: idem(), authorization: true });
}

/** Uploads a PNG or JPG (cropped and re-encoded by the picker) as a mailbox photo. */
export async function uploadStorePhoto(file: Blob): Promise<StorePhoto> {
    const fd = new FormData();
    fd.append("file", file, "photo.jpg");
    return await Request<StorePhoto>({ method: "POST", url: "/store/photos", data: fd, headers: idem(), authorization: true });
}

/** Copies the signed-in member's own profile photo; answers 404 profile_photo_missing when there is none. */
export async function storePhotoFromProfile(): Promise<StorePhoto> {
    return await Request<StorePhoto>({ method: "POST", url: "/store/photos", data: { from: "profile" }, headers: idem(), authorization: true });
}

/** An uploaded photo's bytes, for a preview: the object is private, so it is read with the session. */
export async function getStorePhoto(id: string, signal?: AbortSignal): Promise<Blob> {
    return await Request<Blob>({ method: "GET", url: `/store/photos/${encodeURIComponent(id)}`, authorization: true, responseType: "blob", signal });
}

/** Makes an uploaded photo the mailbox's profile photo at Google or Microsoft. */
export async function setStoreMailboxPhoto(id: string, photoId: string): Promise<StoreMailbox> {
    return await Request<StoreMailbox>({
        method: "PUT",
        url: `/store/mailboxes/${encodeURIComponent(id)}/photo`,
        data: { photo_id: photoId },
        headers: idem(),
        authorization: true,
    });
}

export async function cancelStoreMailbox(id: string): Promise<StoreMailbox> {
    return await Request<StoreMailbox>({ method: "DELETE", url: `/store/mailboxes/${encodeURIComponent(id)}`, headers: idem(), authorization: true });
}

export async function resumeStoreMailbox(id: string): Promise<StoreMailbox> {
    return await Request<StoreMailbox>({ method: "POST", url: `/store/mailboxes/${encodeURIComponent(id)}/resume`, data: {}, headers: idem(), authorization: true });
}

const domainUrl = (id: string) => `/store/domains/${encodeURIComponent(id)}`;

/** Adds a domain registered elsewhere; nameservers (recommended) or manual DNS records. */
export async function connectStoreDomain(domain: string, method: ConnectMethod, idempotencyKey: string): Promise<StoreDomain> {
    return await Request<StoreDomain>({ method: "POST", url: "/store/domains/connect", data: { domain, method }, headers: idem(idempotencyKey), authorization: true });
}

export async function checkStoreDomainConnection(id: string): Promise<StoreDomain> {
    return await Request<StoreDomain>({ method: "POST", url: `${domainUrl(id)}/connection/check`, data: {}, headers: idem(), authorization: true });
}

export async function regenerateStoreDomainConnection(id: string): Promise<StoreDomain> {
    return await Request<StoreDomain>({ method: "POST", url: `${domainUrl(id)}/connection/regenerate`, data: {}, headers: idem(), authorization: true });
}

/** Connected domains only; a purchased one is transferred out instead. */
export async function removeStoreDomain(id: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: domainUrl(id), headers: idem(), authorization: true });
}

export async function addDnsRecord(id: string, record: DnsRecordInput): Promise<DnsRecord> {
    return await Request<DnsRecord>({ method: "POST", url: `${domainUrl(id)}/dns`, data: record, headers: idem(), authorization: true });
}

export async function patchDnsRecord(id: string, recordId: string, patch: DnsRecordPatch): Promise<DnsRecord> {
    return await Request<DnsRecord>({
        method: "PATCH",
        url: `${domainUrl(id)}/dns/${encodeURIComponent(recordId)}`,
        data: patch,
        headers: idem(),
        authorization: true,
    });
}

export async function deleteDnsRecord(id: string, recordId: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `${domainUrl(id)}/dns/${encodeURIComponent(recordId)}`, headers: idem(), authorization: true });
}

export async function checkDnsRecords(id: string): Promise<DnsCheckResult> {
    return await Request<DnsCheckResult>({ method: "POST", url: `${domainUrl(id)}/dns/check`, data: {}, headers: idem(), authorization: true });
}

/** A fresh sign-in for the mailbox; the caller shows it once and keeps no copy. */
export async function signInStoreMailbox(id: string): Promise<MailboxSignIn> {
    return await Request<MailboxSignIn>({ method: "POST", url: `/store/mailboxes/${encodeURIComponent(id)}/sign-in`, data: {}, headers: idem(), authorization: true });
}

/** The Microsoft 365 approval for a purchased domain; the administrator lands back on the order, or the domains page. */
export async function startStoreMicrosoftConsent(domainId: string, orderId?: string): Promise<MicrosoftConsentLink> {
    return await Request<MicrosoftConsentLink>({
        method: "POST",
        url: `/store/domains/${encodeURIComponent(domainId)}/microsoft-consent`,
        data: orderId ? { order_id: orderId } : {},
        headers: idem(),
        authorization: true,
    });
}
