// /emails/domains: the workspace's sending domains, their tracking host and root redirect.
import Request from "@/lib/api/client/Request";
import type {
    BulkDomainResult,
    BulkDomainSetupRequest,
    DomainRedirect,
    SendingDomain,
    SetDomainRedirectRequest,
    SetDomainTrackingResult,
    TrackingSuggestion,
    VendorDomainLink,
} from "@/lib/api/models/app/emails/SendingDomain";

const base = (domain: string) => `/emails/domains/${encodeURIComponent(domain)}`;

export async function listSendingDomains(): Promise<{ data: SendingDomain[] }> {
    return await Request<{ data: SendingDomain[] }>({ method: "GET", url: "/emails/domains", authorization: true });
}

// Probes DNS for the usual tracking labels, so it can take a few seconds.
export async function getTrackingSuggestion(domain: string): Promise<TrackingSuggestion> {
    return await Request<TrackingSuggestion>({
        method: "GET",
        url: `${base(domain)}/tracking-suggestion`,
        authorization: true,
        timeout: 60_000,
    });
}

// One tracking host for every mailbox on the domain; "" clears it. Saved before DNS exists.
export async function setDomainTracking(domain: string, host: string): Promise<SetDomainTrackingResult> {
    return await Request<SetDomainTrackingResult>({
        method: "PUT",
        url: `${base(domain)}/tracking`,
        data: { host },
        authorization: true,
        timeout: 30_000,
    });
}

export async function setDomainRedirect(domain: string, body: SetDomainRedirectRequest): Promise<DomainRedirect> {
    return await Request<DomainRedirect>({
        method: "PUT",
        url: `${base(domain)}/redirect`,
        data: body,
        authorization: true,
        timeout: 30_000,
    });
}

// Checks DNS now instead of waiting for the background check.
export async function verifyDomainRedirect(domain: string): Promise<DomainRedirect> {
    return await Request<DomainRedirect>({
        method: "POST",
        url: `${base(domain)}/redirect/verify`,
        authorization: true,
        timeout: 30_000,
    });
}

export async function deleteDomainRedirect(domain: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `${base(domain)}/redirect`, authorization: true });
}

// The vendor holding the domain forwards its root; "" removes it where the vendor allows.
export async function setDomainVendorForwarding(domain: string, url: string): Promise<VendorDomainLink> {
    return await Request<VendorDomainLink>({
        method: "PUT",
        url: `${base(domain)}/vendor-forwarding`,
        data: { url },
        authorization: true,
        timeout: 60_000,
    });
}

// The vendor writes the tracking CNAME, then every mailbox on the domain uses the host. Safe to retry.
export async function setDomainVendorTracking(domain: string, host: string): Promise<SetDomainTrackingResult> {
    return await Request<SetDomainTrackingResult>({
        method: "POST",
        url: `${base(domain)}/vendor-tracking`,
        data: { host },
        authorization: true,
        timeout: 60_000,
    });
}

// Every domain runs vendor calls and a DNS probe, so a full chunk can take a minute.
export async function bulkDomainSetup(body: BulkDomainSetupRequest): Promise<{ data: BulkDomainResult[] }> {
    return await Request<{ data: BulkDomainResult[] }>({
        method: "POST",
        url: "/emails/domains/bulk",
        data: body,
        authorization: true,
        timeout: 180_000,
    });
}
