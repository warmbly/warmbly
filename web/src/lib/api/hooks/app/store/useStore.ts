// The store's reads and writes, all under ['store'] so the STORE_* realtime events and the
// store_order / store_domain / store_mailbox audit spine refresh every teammate's view.
import { useEffect, useState } from "react";
import { infiniteQueryOptions, keepPreviousData, queryOptions, useInfiniteQuery, useMutation, useQuery, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import toast from "react-hot-toast/headless";
import {
    addDnsRecord,
    cancelStoreMailbox,
    checkDnsRecords,
    checkStoreDomainConnection,
    connectStoreDomain,
    createOrder,
    createQuote,
    deleteDnsRecord,
    getOrder,
    getStoreDomain,
    getStorePhoto,
    getStoreStatus,
    listOrders,
    listStoreDomains,
    listStoreMailboxes,
    patchDnsRecord,
    patchStoreDomain,
    putRegistrant,
    regenerateStoreDomainConnection,
    removeStoreDomain,
    renewStoreDomain,
    resumeStoreMailbox,
    searchStoreDomains,
    setStoreMailboxPhoto,
    transferOutStoreDomain,
} from "@/lib/api/client/app/store/store";
import type { AppError } from "@/lib/api/client/normalizeError";
import type {
    ConnectMethod,
    CreateOrderRequest,
    DnsRecord,
    DnsRecordInput,
    DnsRecordPatch,
    List,
    Order,
    Quote,
    QuoteItem,
    Registrant,
    StoreDomain,
    StoreDisconnected,
    StoreDomainPatch,
    StoreMailbox,
    StoreStatus,
} from "@/lib/api/models/app/store/Store";
import { storeErrorMessage } from "@/lib/buy/errors";
import { orderPollMs } from "@/lib/buy/track";
import { useConnectionStatus } from "@/stores";
import usePartnerManaged from "@/hooks/usePartnerManaged";

export const STORE_KEY = ["store"] as const;

export const storeStatusQuery = queryOptions({
    queryKey: [...STORE_KEY, "status"],
    queryFn: ({ signal }) => getStoreStatus(signal),
    staleTime: 30_000,
});

/** The store's switch, prices and the buyer's balance. A failed read leaves the store hidden. */
export function useStoreStatus(enabled = true) {
    return useQuery({ ...storeStatusQuery, enabled, retry: 1 });
}

/** True only once the instance has said the store is on, and never in a workspace a reseller manages (its customers buy there). */
export function useStoreEnabled(enabled = true): { enabled: boolean; selling: boolean; status: StoreStatus | undefined } {
    const managed = usePartnerManaged() !== null;
    const q = useStoreStatus(enabled && !managed);
    const on = !managed && q.data?.enabled === true;
    return { enabled: on, selling: on && !storeSalesPaused(q.data), status: managed ? undefined : q.data };
}

/** The store is on but the operator paused new purchases; what is already bought is still managed as usual. */
export const storeSalesPaused = (status: StoreStatus | undefined) => status?.enabled === true && status.sales_paused === true;

/**
 * The store as this workspace sees it: on, locked (a self-host with no GraphKit connection yet, shown as a feature to set up),
 * or hidden (a hosted instance not configured yet, a reseller manages the workspace, the read failed or is pending, or an older backend).
 */
export type StoreState = { kind: "on"; status: StoreStatus } | { kind: "locked"; status: StoreStatus } | { kind: "hidden" };

export function storeState(status: StoreStatus | undefined, managed: boolean): StoreState {
    if (managed || !status) return { kind: "hidden" };
    if (status.enabled) return { kind: "on", status };
    if (status.reason === "not_configured" && status.locked === true) return { kind: "locked", status };
    return { kind: "hidden" };
}

export function useStoreState(enabled = true): StoreState {
    const managed = usePartnerManaged() !== null;
    const q = useStoreStatus(enabled && !managed);
    return storeState(q.data, managed);
}

/** The live purchases a locked store still holds, or null; their reads keep working while GraphKit is not configured. */
export function storeDisconnected(s: StoreState): StoreDisconnected | null {
    return s.kind === "locked" ? (s.status.disconnected ?? null) : null;
}

/** Whether the workspace's purchases can be listed: the store is on, or it is locked with live purchases. */
export const storeListable = (s: StoreState) => s.kind === "on" || storeDisconnected(s) !== null;

export const domainSearchQuery = (q: string, exactOnly = false) =>
    queryOptions({
        queryKey: [...STORE_KEY, "search", q, exactOnly ? "exact" : "all"],
        queryFn: ({ signal }) => searchStoreDomains(q, exactOnly, signal),
        staleTime: 60_000,
        retry: false,
    });

/**
 * The exact matches come back in a second or two and show first; the full search, with suggestions,
 * replaces them when it lands. Loading and errors follow the full search unless the exact one is all there is.
 */
export function useDomainSearch(q: string) {
    const enabled = q.length >= 2;
    const exact = useQuery({ ...domainSearchQuery(q, true), enabled, placeholderData: keepPreviousData });
    const full = useQuery({ ...domainSearchQuery(q), enabled, placeholderData: keepPreviousData });
    const current = (r: typeof full) => r.data !== undefined && !r.isPlaceholderData;
    const data = current(full) ? full.data : current(exact) ? exact.data : (full.data ?? exact.data);
    return {
        data,
        isFetching: full.isFetching,
        isPending: full.isPending && exact.isPending,
        isError: full.isError && !current(exact),
        error: full.error ?? exact.error,
        refetch: () => Promise.all([exact.refetch(), full.refetch()]),
    };
}

// A quote holds its prices for 15 minutes. It is re-asked for a little before it lapses, and on
// returning to the tab when it is close, so the total on screen is always one the server will honour.
const QUOTE_REFRESH_LEAD_MS = 45_000;
const QUOTE_FOCUS_WINDOW_MS = 3 * 60_000;

function msLeft(q: { state: { data?: Quote } }): number | null {
    const exp = q.state.data?.expires_at;
    if (!exp) return null;
    const t = (exp instanceof Date ? exp : new Date(exp)).getTime();
    return Number.isNaN(t) ? null : t - Date.now();
}

export const quoteQuery = (items: QuoteItem[]) =>
    queryOptions({
        queryKey: [...STORE_KEY, "quote", JSON.stringify(items)],
        queryFn: ({ signal }) => createQuote(items, signal),
        staleTime: (q) => {
            const left = msLeft(q);
            return left === null ? 0 : Math.max(0, left - QUOTE_REFRESH_LEAD_MS);
        },
        refetchInterval: (q) => {
            const left = msLeft(q);
            return left === null ? false : Math.max(5_000, left - QUOTE_REFRESH_LEAD_MS);
        },
        refetchOnWindowFocus: (q) => {
            const left = msLeft(q);
            return left !== null && left < QUOTE_FOCUS_WINDOW_MS ? "always" : false;
        },
        retry: false,
    });

export function useQuote(items: QuoteItem[], enabled: boolean) {
    return useQuery({ ...quoteQuery(items), enabled: enabled && items.length > 0 });
}

export const orderQuery = (id: string) =>
    queryOptions({
        queryKey: [...STORE_KEY, "orders", id],
        queryFn: ({ signal }) => getOrder(id, signal),
        staleTime: 5_000,
    });

// A safety net under realtime: an order still being set up re-reads itself this often.
const ORDER_FALLBACK_MS = 30_000;

export function useOrder(id: string | undefined) {
    const queryClient = useQueryClient();
    // With the socket down nothing pushes the order forward, so it re-reads itself every couple of seconds instead.
    const socketLive = useConnectionStatus().status === "connected";
    const query = useQuery({
        ...orderQuery(id ?? ""),
        enabled: !!id,
        refetchInterval: (q) => orderPollMs(q.state.data, socketLive),
    });
    // Every newly connected mailbox refreshes the mailbox list, so it is there on arrival even without the socket.
    const connected = query.data?.mailboxes.filter((m) => m.connect_state === "connected").length ?? 0;
    useEffect(() => {
        if (connected > 0) void queryClient.invalidateQueries({ queryKey: ["emails", "list"] });
    }, [connected, queryClient]);
    return query;
}

const ORDERS_PAGE = 25;

/** The workspace's orders, newest first, a page at a time. Realtime and the store_order spine refresh it under ['store', 'orders']. */
export const storeOrdersQuery = infiniteQueryOptions<List<Order>, Error, InfiniteData<List<Order>, string | null>, readonly ["store", "orders", "list"], string | null>({
    queryKey: [...STORE_KEY, "orders", "list"] as const,
    queryFn: ({ pageParam, signal }) => listOrders(pageParam ?? undefined, ORDERS_PAGE, signal),
    initialPageParam: null,
    getNextPageParam: (last) => (last.pagination.has_more ? last.pagination.next_cursor : undefined),
    staleTime: 15_000,
});

export const isOpenOrder = (o: Pick<Order, "status">) => o.status === "charging" || o.status === "processing";

export function useStoreOrders(enabled = true) {
    return useInfiniteQuery({
        ...storeOrdersQuery,
        enabled,
        // The same safety net as one order: while any is still being set up, the list re-reads itself.
        refetchInterval: (q) => (q.state.data?.pages.some((p) => p.data.some(isOpenOrder)) ? ORDER_FALLBACK_MS : false),
    });
}

export const storeDomainsQuery = queryOptions({
    queryKey: [...STORE_KEY, "domains", "list"],
    queryFn: ({ signal }) => listStoreDomains(signal),
    staleTime: 30_000,
});

export function useStoreDomains(enabled = true) {
    return useQuery({ ...storeDomainsQuery, enabled });
}

export const storeDomainQuery = (id: string) =>
    queryOptions({
        queryKey: [...STORE_KEY, "domains", id],
        queryFn: ({ signal }) => getStoreDomain(id, signal),
        staleTime: 30_000,
    });

// A safety net under realtime: a connection still waiting on DNS re-reads itself this often.
const CONNECTION_FALLBACK_MS = 60_000;

export function useStoreDomain(id: string | null) {
    return useQuery({
        ...storeDomainQuery(id ?? ""),
        enabled: !!id,
        refetchInterval: (q) => (q.state.data?.connection?.status === "pending" ? CONNECTION_FALLBACK_MS : false),
    });
}

export const storeMailboxesQuery = queryOptions({
    queryKey: [...STORE_KEY, "mailboxes", "list"],
    queryFn: ({ signal }) => listStoreMailboxes(signal),
    staleTime: 30_000,
});

export function useStoreMailboxes(enabled = true) {
    return useQuery({ ...storeMailboxesQuery, enabled });
}

export function useSaveRegistrant() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (r: Registrant) => putRegistrant(r),
        onSuccess: (saved) => {
            qc.setQueryData<StoreStatus>(storeStatusQuery.queryKey, (prev) => (prev ? { ...prev, registrant: saved } : prev));
        },
    });
}

/** Places the order. The server decides everything here, so nothing is optimistic. */
export function useCreateOrder() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ body, idempotencyKey }: { body: CreateOrderRequest; idempotencyKey: string }) => createOrder(body, idempotencyKey),
        onSuccess: (order) => {
            qc.setQueryData(orderQuery(order.id).queryKey, order);
            void qc.invalidateQueries({ queryKey: [...STORE_KEY, "status"] });
            void qc.invalidateQueries({ queryKey: [...STORE_KEY, "orders", "list"] });
            void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
        },
    });
}

function patchDomainInLists(qc: ReturnType<typeof useQueryClient>, id: string, fn: (d: StoreDomain) => StoreDomain) {
    qc.setQueryData<List<StoreDomain>>(storeDomainsQuery.queryKey, (prev) => (prev ? { ...prev, data: prev.data.map((d) => (d.id === id ? fn(d) : d)) } : prev));
    qc.setQueryData<StoreDomain>(storeDomainQuery(id).queryKey, (prev) => (prev ? fn(prev) : prev));
}

/** Auto-renew and forwarding. Optimistic: the row changes first and rolls back on a refusal. */
export function usePatchStoreDomain() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, patch }: { id: string; patch: StoreDomainPatch }) => patchStoreDomain(id, patch),
        onMutate: async ({ id, patch }) => {
            await qc.cancelQueries({ queryKey: [...STORE_KEY, "domains"] });
            const list = qc.getQueryData<List<StoreDomain>>(storeDomainsQuery.queryKey);
            const one = qc.getQueryData<StoreDomain>(storeDomainQuery(id).queryKey);
            patchDomainInLists(qc, id, (d) => ({ ...d, ...patch }));
            return { list, one, id };
        },
        onError: (e: AppError, _v, ctx) => {
            if (ctx?.list) qc.setQueryData(storeDomainsQuery.queryKey, ctx.list);
            if (ctx?.one) qc.setQueryData(storeDomainQuery(ctx.id).queryKey, ctx.one);
            toast.error(storeErrorMessage(e));
        },
        onSettled: (_d, _e, { id }) => {
            void qc.invalidateQueries({ queryKey: [...STORE_KEY, "domains", "list"] });
            void qc.invalidateQueries({ queryKey: storeDomainQuery(id).queryKey });
        },
    });
}

export function useRenewStoreDomain() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, idempotencyKey }: { id: string; idempotencyKey: string }) => renewStoreDomain(id, idempotencyKey),
        onSuccess: (d) => {
            patchDomainInLists(qc, d.id, (prev) => ({ ...prev, ...d }));
            void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
            void qc.invalidateQueries({ queryKey: [...STORE_KEY, "status"] });
        },
    });
}

export function useTransferOutStoreDomain() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, idempotencyKey }: { id: string; idempotencyKey: string }) => transferOutStoreDomain(id, idempotencyKey),
        onSettled: (_d, _e, { id }) => {
            void qc.invalidateQueries({ queryKey: [...STORE_KEY, "domains", "list"] });
            void qc.invalidateQueries({ queryKey: storeDomainQuery(id).queryKey });
        },
    });
}

function replaceMailbox(qc: ReturnType<typeof useQueryClient>, next: StoreMailbox) {
    qc.setQueryData<List<StoreMailbox>>(storeMailboxesQuery.queryKey, (prev) =>
        prev ? { ...prev, data: prev.data.map((m) => (m.id === next.id ? next : m)) } : prev,
    );
}

/** Cancels at the end of the paid month; what the server answers is what is shown. */
export function useCancelStoreMailbox() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => cancelStoreMailbox(id),
        onSuccess: (m) => replaceMailbox(qc, m),
        onSettled: () => void qc.invalidateQueries({ queryKey: [...STORE_KEY, "mailboxes"] }),
    });
}

// Photo bytes sit outside ['store'] so the store's realtime refreshes never refetch an image that cannot change.
const storePhotoKey = (id: string) => ["store-photo", id] as const;

/** Seeds a just-uploaded photo, so its preview shows without a round trip. */
export function useSeedStorePhoto() {
    const qc = useQueryClient();
    return (id: string, blob: Blob) => qc.setQueryData(storePhotoKey(id), blob);
}

/** An object URL for an uploaded photo, read with the session because the object is private; null until loaded. */
export function useStorePhotoUrl(id: string | null | undefined): string | null {
    const q = useQuery({
        queryKey: storePhotoKey(id ?? ""),
        queryFn: ({ signal }) => getStorePhoto(id as string, signal),
        enabled: !!id,
        staleTime: Infinity,
        gcTime: 30 * 60_000,
        retry: false,
    });
    const blob = id ? q.data : undefined;
    const [url, setUrl] = useState<string | null>(null);
    useEffect(() => {
        if (!blob) {
            setUrl(null);
            return;
        }
        const u = URL.createObjectURL(blob);
        setUrl(u);
        return () => URL.revokeObjectURL(u);
    }, [blob]);
    return url;
}

/** Sets an uploaded photo on purchased mailboxes, one request each; the server decides, so nothing is optimistic. */
export function useSetStoreMailboxPhoto() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async ({ ids, photoId }: { ids: string[]; photoId: string }) => {
            const settled = await Promise.allSettled(ids.map((id) => setStoreMailboxPhoto(id, photoId)));
            const done = settled.flatMap((r) => (r.status === "fulfilled" ? [r.value] : []));
            const failed = settled.find((r): r is PromiseRejectedResult => r.status === "rejected");
            return { done, failed: settled.length - done.length, error: failed?.reason as unknown };
        },
        onSuccess: ({ done }) => done.forEach((m) => replaceMailbox(qc, m)),
        onSettled: () => {
            void qc.invalidateQueries({ queryKey: [...STORE_KEY, "mailboxes"] });
            // The dashboard's mailbox avatars come from the mailboxes list.
            void qc.invalidateQueries({ queryKey: ["emails"] });
        },
    });
}

export function useResumeStoreMailbox() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => resumeStoreMailbox(id),
        onSuccess: (m) => replaceMailbox(qc, m),
        onSettled: () => void qc.invalidateQueries({ queryKey: [...STORE_KEY, "mailboxes"] }),
    });
}


/** Writes a domain the server answered with into the list and its detail, keeping the dns it did not send. */
function storeDomainAnswer(qc: ReturnType<typeof useQueryClient>, d: StoreDomain) {
    qc.setQueryData<List<StoreDomain>>(storeDomainsQuery.queryKey, (prev) => {
        if (!prev) return prev;
        const has = prev.data.some((x) => x.id === d.id);
        return { ...prev, data: has ? prev.data.map((x) => (x.id === d.id ? { ...x, ...d } : x)) : [d, ...prev.data] };
    });
    qc.setQueryData<StoreDomain>(storeDomainQuery(d.id).queryKey, (prev) => (prev ? { ...prev, ...d, dns: d.dns ?? prev.dns } : d));
}

/** Adds a domain registered elsewhere. The server decides whether it can, so nothing is optimistic. */
export function useConnectStoreDomain() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ domain, method, idempotencyKey }: { domain: string; method: ConnectMethod; idempotencyKey: string }) =>
            connectStoreDomain(domain, method, idempotencyKey),
        onSuccess: (d) => storeDomainAnswer(qc, d),
        onSettled: () => void qc.invalidateQueries({ queryKey: [...STORE_KEY, "domains", "list"] }),
    });
}

export function useCheckStoreDomainConnection() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => checkStoreDomainConnection(id),
        onSuccess: (d) => storeDomainAnswer(qc, d),
        onError: (e: AppError) => toast.error(storeErrorMessage(e)),
    });
}

export function useRegenerateStoreDomainConnection() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => regenerateStoreDomainConnection(id),
        onSuccess: (d) => storeDomainAnswer(qc, d),
        onError: (e: AppError) => toast.error(storeErrorMessage(e)),
    });
}

/** Removes a connected domain; refused while it has mailboxes, so it waits for the answer. */
export function useRemoveStoreDomain() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => removeStoreDomain(id),
        onSuccess: (_r, id) => {
            qc.setQueryData<List<StoreDomain>>(storeDomainsQuery.queryKey, (prev) => (prev ? { ...prev, data: prev.data.filter((d) => d.id !== id) } : prev));
            qc.removeQueries({ queryKey: storeDomainQuery(id).queryKey });
        },
        onSettled: () => {
            void qc.invalidateQueries({ queryKey: [...STORE_KEY, "domains", "list"] });
            void qc.invalidateQueries({ queryKey: ["sending-domains"] });
        },
    });
}

function patchDomainDns(qc: ReturnType<typeof useQueryClient>, id: string, fn: (dns: DnsRecord[]) => DnsRecord[]) {
    qc.setQueryData<StoreDomain>(storeDomainQuery(id).queryKey, (prev) => (prev ? { ...prev, dns: fn(prev.dns ?? []) } : prev));
}

/** A new record is validated by the server (conflicts, managed names), so it lands when answered. */
export function useAddDnsRecord() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ domainId, record }: { domainId: string; record: DnsRecordInput }) => addDnsRecord(domainId, record),
        onSuccess: (r, { domainId }) => patchDomainDns(qc, domainId, (dns) => [...dns.filter((x) => x.id !== r.id), r]),
        onSettled: (_r, _e, { domainId }) => void qc.invalidateQueries({ queryKey: storeDomainQuery(domainId).queryKey }),
    });
}

/** An edit to an unmanaged record. Optimistic: the row changes first and rolls back on a refusal. */
export function usePatchDnsRecord() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ domainId, recordId, patch }: { domainId: string; recordId: string; patch: DnsRecordPatch }) => patchDnsRecord(domainId, recordId, patch),
        onMutate: async ({ domainId, recordId, patch }) => {
            await qc.cancelQueries({ queryKey: storeDomainQuery(domainId).queryKey });
            const prev = qc.getQueryData<StoreDomain>(storeDomainQuery(domainId).queryKey);
            patchDomainDns(qc, domainId, (dns) => dns.map((r) => (r.id === recordId ? { ...r, ...patch } : r)));
            return { prev };
        },
        onError: (_e, { domainId }, ctx) => {
            if (ctx?.prev) qc.setQueryData(storeDomainQuery(domainId).queryKey, ctx.prev);
        },
        onSuccess: (r, { domainId }) => patchDomainDns(qc, domainId, (dns) => dns.map((x) => (x.id === r.id ? r : x))),
        onSettled: (_r, _e, { domainId }) => void qc.invalidateQueries({ queryKey: storeDomainQuery(domainId).queryKey }),
    });
}

/** Deletes a record (or a conflict). Optimistic: the row leaves first and returns on a refusal. */
export function useDeleteDnsRecord() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ domainId, recordId }: { domainId: string; recordId: string }) => deleteDnsRecord(domainId, recordId),
        onMutate: async ({ domainId, recordId }) => {
            await qc.cancelQueries({ queryKey: storeDomainQuery(domainId).queryKey });
            const prev = qc.getQueryData<StoreDomain>(storeDomainQuery(domainId).queryKey);
            qc.setQueryData<StoreDomain>(storeDomainQuery(domainId).queryKey, (d) =>
                d ? { ...d, dns: (d.dns ?? []).filter((r) => r.id !== recordId), dns_conflicts: (d.dns_conflicts ?? []).filter((c) => c.record_id !== recordId) } : d,
            );
            return { prev };
        },
        onError: (e: AppError, { domainId }, ctx) => {
            if (ctx?.prev) qc.setQueryData(storeDomainQuery(domainId).queryKey, ctx.prev);
            toast.error(storeErrorMessage(e));
        },
        onSettled: (_r, _e, { domainId }) => {
            void qc.invalidateQueries({ queryKey: storeDomainQuery(domainId).queryKey });
            void qc.invalidateQueries({ queryKey: [...STORE_KEY, "domains", "list"] });
        },
    });
}

/** What public DNS serves for each record right now; the answer is shown, not cached. */
export function useCheckDnsRecords() {
    return useMutation({ mutationFn: (domainId: string) => checkDnsRecords(domainId), gcTime: 0 });
}
