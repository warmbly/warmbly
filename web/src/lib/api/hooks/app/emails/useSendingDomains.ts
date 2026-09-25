// Sending domains under their own root; the email_account, domain_redirect
// and mailbox_vendor audit spine keeps every teammate's view live.
import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    bulkDomainSetup,
    deleteDomainRedirect,
    getTrackingSuggestion,
    listSendingDomains,
    setDomainRedirect,
    setDomainTracking,
    setDomainVendorForwarding,
    setDomainVendorTracking,
    verifyDomainRedirect,
} from "@/lib/api/client/app/emails/sendingDomains";
import type {
    BulkDomainResult,
    BulkDomainSetupRequest,
    DomainRedirect,
    SendingDomain,
    SetDomainRedirectRequest,
    VendorDomainLink,
} from "@/lib/api/models/app/emails/SendingDomain";
import { BULK_DOMAINS_CHUNK } from "@/lib/api/models/app/emails/SendingDomain";

export const SENDING_DOMAINS_KEY = ["sending-domains"] as const;
// Separate from the list on purpose: each read probes DNS, and every mailbox audit would re-run it.
const SUGGESTION_KEY = "tracking-suggestion";

export function useSendingDomains(enabled = true) {
    return useQuery({
        queryKey: [...SENDING_DOMAINS_KEY, "list"],
        queryFn: listSendingDomains,
        enabled,
        staleTime: 15_000,
    });
}

const suggestionQuery = (domain: string) => ({
    queryKey: [SUGGESTION_KEY, domain],
    queryFn: () => getTrackingSuggestion(domain),
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
    retry: false,
});

export function useTrackingSuggestion(domain: string | null) {
    return useQuery({ ...suggestionQuery(domain ?? ""), enabled: !!domain });
}

/** One suggestion per domain, for the import wizards' per-domain choices. */
export function useTrackingSuggestions(domains: string[]) {
    return useQueries({ queries: domains.map((d) => suggestionQuery(d)) });
}

// Writes a redirect into the list so the drawer shows it before the refetch lands.
function useStoreRedirect() {
    const qc = useQueryClient();
    return (domain: string, r: DomainRedirect | null) => {
        qc.setQueryData<{ data: SendingDomain[] }>([...SENDING_DOMAINS_KEY, "list"], (prev) =>
            prev ? { data: prev.data.map((d) => (d.domain === domain ? { ...d, redirect: r } : d)) } : prev,
        );
    };
}

export function useSetDomainTracking() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ domain, host }: { domain: string; host: string }) => setDomainTracking(domain, host),
        onSuccess: (_, { domain }) => {
            qc.invalidateQueries({ queryKey: SENDING_DOMAINS_KEY });
            qc.invalidateQueries({ queryKey: ["emails", "list"] });
            qc.invalidateQueries({ queryKey: [SUGGESTION_KEY, domain] });
        },
    });
}

export function useSetDomainRedirect() {
    const qc = useQueryClient();
    const store = useStoreRedirect();
    return useMutation({
        mutationFn: ({ domain, body }: { domain: string; body: SetDomainRedirectRequest }) => setDomainRedirect(domain, body),
        onSuccess: (r, { domain }) => {
            store(domain, r);
            qc.invalidateQueries({ queryKey: SENDING_DOMAINS_KEY });
        },
    });
}

// No refetch after: the answer carries this probe's per-record results, the list does not.
export function useVerifyDomainRedirect() {
    const store = useStoreRedirect();
    return useMutation({
        mutationFn: (domain: string) => verifyDomainRedirect(domain),
        onSuccess: (r, domain) => store(domain, r),
    });
}

export function useDeleteDomainRedirect() {
    const qc = useQueryClient();
    const store = useStoreRedirect();
    return useMutation({
        mutationFn: (domain: string) => deleteDomainRedirect(domain),
        onSuccess: (_, domain) => {
            store(domain, null);
            qc.invalidateQueries({ queryKey: SENDING_DOMAINS_KEY });
        },
    });
}

// Writes the vendor's answer into the list so the drawer shows it before the refetch lands.
export function useSetVendorForwarding() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ domain, url }: { domain: string; url: string }) => setDomainVendorForwarding(domain, url),
        onSuccess: (link: VendorDomainLink, { domain }) => {
            qc.setQueryData<{ data: SendingDomain[] }>([...SENDING_DOMAINS_KEY, "list"], (prev) =>
                prev ? { data: prev.data.map((d) => (d.domain === domain ? { ...d, vendor_domain: link } : d)) } : prev,
            );
            qc.invalidateQueries({ queryKey: SENDING_DOMAINS_KEY });
        },
    });
}

export function useSetVendorTracking() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ domain, host }: { domain: string; host: string }) => setDomainVendorTracking(domain, host),
        onSuccess: (_, { domain }) => {
            qc.invalidateQueries({ queryKey: SENDING_DOMAINS_KEY });
            qc.invalidateQueries({ queryKey: ["emails", "list"] });
            qc.invalidateQueries({ queryKey: [SUGGESTION_KEY, domain] });
        },
    });
}

/** Runs a bulk setup in chunks the server takes, reporting each chunk's rows as they land. */
export function useBulkDomainSetup() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async ({ body, onChunk }: { body: BulkDomainSetupRequest; onChunk?: (rows: BulkDomainResult[]) => void }) => {
            const out: BulkDomainResult[] = [];
            for (let i = 0; i < body.domains.length; i += BULK_DOMAINS_CHUNK) {
                const res = await bulkDomainSetup({ ...body, domains: body.domains.slice(i, i + BULK_DOMAINS_CHUNK) });
                out.push(...res.data);
                onChunk?.(res.data);
            }
            return out;
        },
        onSettled: (_, __, { body }) => {
            qc.invalidateQueries({ queryKey: SENDING_DOMAINS_KEY });
            qc.invalidateQueries({ queryKey: ["emails", "list"] });
            for (const d of body.domains) qc.invalidateQueries({ queryKey: [SUGGESTION_KEY, d] });
        },
    });
}
