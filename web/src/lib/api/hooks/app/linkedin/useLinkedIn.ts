// LinkedIn reads and writes, under ['linkedin'] so the LINKEDIN_* realtime events and the linkedin_* audit
// spine refresh every teammate's view. Search results live outside it, so nothing refreshes a page someone paid for.
import { infiniteQueryOptions, keepPreviousData, queryOptions, useInfiniteQuery, useMutation, useQuery, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import toast from "react-hot-toast/headless";
import {
    answerConnection,
    cancelConnection,
    chooseConnectionMethod,
    completeLinkedInLink,
    createConnection,
    createSavedSearch,
    deleteSavedSearch,
    finderAi,
    finderTypeahead,
    getConnection,
    getConnectQuote,
    getFinderCatalog,
    getLinkedInAccount,
    getLinkedInProfile,
    getLinkedInStatus,
    importFinderHits,
    linkedInAccountAction,
    listLinkedInAccounts,
    listSavedSearches,
    parseFinderUrl,
    patchLinkedInAccount,
    patchSavedSearch,
    removeLinkedInAccount,
    resendConnectionCode,
    runFinderSearch,
    signInConnection,
    startLinkedInLink,
    unlinkLinkedIn,
    type AccountAction,
} from "@/lib/api/client/app/linkedin/linkedin";
import type { AppError } from "@/lib/api/client/normalizeError";
import type {
    CheckpointAnswer,
    ConnectSession,
    CreateConnection,
    FinderQuery,
    FinderSource,
    ImportRequest,
    LinkedInAccount,
    LinkedInAccountPatch,
    LinkedInList,
    LinkedInStatus,
    SavedSearch,
    SearchPage,
    SignIn,
} from "@/lib/api/models/app/linkedin/LinkedIn";
import { linkedInErrorMessage } from "@/lib/linkedin/errors";

export const LINKEDIN_KEY = ["linkedin"] as const;

export const linkedInStatusQuery = queryOptions({
    queryKey: [...LINKEDIN_KEY, "status"],
    queryFn: ({ signal }) => getLinkedInStatus(signal),
    staleTime: 30_000,
});

export function useLinkedInStatus(enabled = true) {
    return useQuery({ ...linkedInStatusQuery, enabled, retry: 1 });
}

export const linkedInAccountsQuery = queryOptions({
    queryKey: [...LINKEDIN_KEY, "accounts", "list"],
    queryFn: ({ signal }) => listLinkedInAccounts(signal),
    staleTime: 15_000,
});

export function useLinkedInAccounts(enabled = true) {
    return useQuery({ ...linkedInAccountsQuery, enabled });
}

export const linkedInAccountQuery = (id: string) =>
    queryOptions({
        queryKey: [...LINKEDIN_KEY, "accounts", id],
        queryFn: ({ signal }) => getLinkedInAccount(id, signal),
        staleTime: 15_000,
    });

/** One account, seeded from the list so the drawer opens with it already there. */
export function useLinkedInAccount(id: string | null) {
    const qc = useQueryClient();
    return useQuery({
        ...linkedInAccountQuery(id ?? ""),
        enabled: !!id,
        initialData: () => qc.getQueryData<LinkedInList<LinkedInAccount>>(linkedInAccountsQuery.queryKey)?.data.find((a) => a.id === id),
        initialDataUpdatedAt: () => qc.getQueryState(linkedInAccountsQuery.queryKey)?.dataUpdatedAt,
    });
}

export const connectQuoteQuery = (accountId: string | null) =>
    queryOptions({
        queryKey: [...LINKEDIN_KEY, "quote", accountId ?? "new"],
        queryFn: ({ signal }) => getConnectQuote(accountId, signal),
        staleTime: 30_000,
        retry: false,
    });

export function useConnectQuote(accountId: string | null, enabled: boolean) {
    return useQuery({ ...connectQuoteQuery(accountId), enabled });
}

/** A session waits on someone outside the browser only while LinkedIn can be approved in its app. */
export const isWaitingCheckpoint = (s: ConnectSession | undefined) =>
    s?.status === "checkpoint" && (s.checkpoint?.type === "app_approval" || s.checkpoint?.type === "otp_or_app");

export const connectionQuery = (id: string) =>
    queryOptions({
        queryKey: [...LINKEDIN_KEY, "connections", id],
        queryFn: ({ signal }) => getConnection(id, signal),
        staleTime: 2_000,
    });

/** A connect session. Realtime moves it forward; the 3 s re-read runs only while a waiting checkpoint is on screen. */
export function useConnection(id: string | null) {
    return useQuery({
        ...connectionQuery(id ?? ""),
        enabled: !!id,
        refetchInterval: (q) => (isWaitingCheckpoint(q.state.data) ? 3_000 : false),
    });
}

function setSession(qc: ReturnType<typeof useQueryClient>, s: ConnectSession) {
    qc.setQueryData(connectionQuery(s.id).queryKey, s);
    if (s.status === "succeeded") {
        void qc.invalidateQueries({ queryKey: [...LINKEDIN_KEY, "accounts"] });
        void qc.invalidateQueries({ queryKey: [...LINKEDIN_KEY, "status"] });
    }
}

/** Starts a connect or reconnect session; a new seat is charged here, so nothing is optimistic. */
export function useCreateConnection() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ body, idempotencyKey }: { body: CreateConnection; idempotencyKey: string }) => createConnection(body, idempotencyKey),
        onSuccess: (r) => {
            setSession(qc, r.session);
            void qc.invalidateQueries({ queryKey: [...LINKEDIN_KEY, "status"] });
            if (r.charged_micros > 0) void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
        },
    });
}

export function useSignInConnection() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, body }: { id: string; body: SignIn }) => signInConnection(id, body),
        onSuccess: (s) => setSession(qc, s),
    });
}

export function useAnswerConnection() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, body }: { id: string; body: CheckpointAnswer }) => answerConnection(id, body),
        onSuccess: (s) => setSession(qc, s),
    });
}

export function useChooseConnectionMethod() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, challenge }: { id: string; challenge: string }) => chooseConnectionMethod(id, challenge),
        onSuccess: (s) => setSession(qc, s),
    });
}

export function useResendConnectionCode() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => resendConnectionCode(id),
        onSuccess: (s) => {
            setSession(qc, s);
            toast.success("A new code is on its way.");
        },
        onError: (e: AppError) => toast.error(linkedInErrorMessage(e)),
    });
}

export function useCancelConnection() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => cancelConnection(id),
        onSettled: (_r, _e, id) => {
            qc.removeQueries({ queryKey: connectionQuery(id).queryKey });
            void qc.invalidateQueries({ queryKey: [...LINKEDIN_KEY, "accounts"] });
            void qc.invalidateQueries({ queryKey: [...LINKEDIN_KEY, "status"] });
        },
    });
}

function patchAccountInCache(qc: ReturnType<typeof useQueryClient>, id: string, fn: (a: LinkedInAccount) => LinkedInAccount) {
    qc.setQueryData<LinkedInList<LinkedInAccount>>(linkedInAccountsQuery.queryKey, (prev) => (prev ? { ...prev, data: prev.data.map((a) => (a.id === id ? fn(a) : a)) } : prev));
    qc.setQueryData<LinkedInAccount>(linkedInAccountQuery(id).queryKey, (prev) => (prev ? fn(prev) : prev));
}

function snapshotAccount(qc: ReturnType<typeof useQueryClient>, id: string) {
    return {
        list: qc.getQueryData<LinkedInList<LinkedInAccount>>(linkedInAccountsQuery.queryKey),
        one: qc.getQueryData<LinkedInAccount>(linkedInAccountQuery(id).queryKey),
        id,
    };
}

function restoreAccount(qc: ReturnType<typeof useQueryClient>, ctx: ReturnType<typeof snapshotAccount> | undefined) {
    if (!ctx) return;
    if (ctx.list) qc.setQueryData(linkedInAccountsQuery.queryKey, ctx.list);
    if (ctx.one) qc.setQueryData(linkedInAccountQuery(ctx.id).queryKey, ctx.one);
}

/** Limits, hours and warm-up. Optimistic: the account changes first and rolls back on a refusal. */
export function usePatchLinkedInAccount() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, patch }: { id: string; patch: LinkedInAccountPatch }) => patchLinkedInAccount(id, patch),
        onMutate: async ({ id, patch }) => {
            await qc.cancelQueries({ queryKey: [...LINKEDIN_KEY, "accounts"] });
            const ctx = snapshotAccount(qc, id);
            patchAccountInCache(qc, id, (a) => ({
                ...a,
                limits: { ...a.limits, ...patch.limits },
                weekly_limits: { ...a.weekly_limits, ...patch.weekly_limits },
                working_hours: patch.working_hours ?? a.working_hours,
                timezone: patch.timezone ?? a.timezone,
                warmup: patch.warmup ? { ...(a.warmup ?? {}), ...patch.warmup } : a.warmup,
                withdraw_invites_after_days: patch.withdraw_invites_after_days ?? a.withdraw_invites_after_days,
            }));
            return ctx;
        },
        onError: (e: AppError, _v, ctx) => {
            restoreAccount(qc, ctx);
            toast.error(linkedInErrorMessage(e));
        },
        onSuccess: (a) => patchAccountInCache(qc, a.id, () => a),
        onSettled: (_d, _e, { id }) => {
            void qc.invalidateQueries({ queryKey: linkedInAccountsQuery.queryKey });
            void qc.invalidateQueries({ queryKey: linkedInAccountQuery(id).queryKey });
        },
    });
}

/** Pause and resume are predictable and flip the status first; a resync only asks, so it waits. */
export function useLinkedInAccountAction() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, action }: { id: string; action: AccountAction }) => linkedInAccountAction(id, action),
        onMutate: async ({ id, action }) => {
            if (action === "resync") return undefined;
            await qc.cancelQueries({ queryKey: [...LINKEDIN_KEY, "accounts"] });
            const ctx = snapshotAccount(qc, id);
            patchAccountInCache(qc, id, (a) => ({ ...a, status: action === "pause" ? "paused" : "active" }));
            return ctx;
        },
        onError: (e: AppError, _v, ctx) => {
            restoreAccount(qc, ctx);
            toast.error(linkedInErrorMessage(e));
        },
        onSuccess: (a, { action }) => {
            if (a && typeof a === "object" && "id" in a) patchAccountInCache(qc, a.id, () => a);
            if (action === "resync") toast.success("Refreshing this account from LinkedIn.");
        },
        onSettled: () => void qc.invalidateQueries({ queryKey: [...LINKEDIN_KEY, "accounts"] }),
    });
}

/** Removes a seat; it ends billing, so it waits for the answer. */
export function useRemoveLinkedInAccount() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => removeLinkedInAccount(id),
        onSuccess: (_r, id) => {
            qc.setQueryData<LinkedInList<LinkedInAccount>>(linkedInAccountsQuery.queryKey, (prev) => (prev ? { ...prev, data: prev.data.filter((a) => a.id !== id) } : prev));
            qc.removeQueries({ queryKey: linkedInAccountQuery(id).queryKey });
        },
        onSettled: () => {
            void qc.invalidateQueries({ queryKey: [...LINKEDIN_KEY, "accounts"] });
            void qc.invalidateQueries({ queryKey: [...LINKEDIN_KEY, "status"] });
        },
    });
}

// ---------------------------------------------------------------- GraphKit link

export function useStartLinkedInLink() {
    return useMutation({
        mutationFn: () => startLinkedInLink(),
        onError: (e: AppError) => toast.error(linkedInErrorMessage(e)),
    });
}

export function useCompleteLinkedInLink() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (session: string) => completeLinkedInLink(session),
        onSuccess: (s: LinkedInStatus) => {
            qc.setQueryData(linkedInStatusQuery.queryKey, s);
            void qc.invalidateQueries({ queryKey: LINKEDIN_KEY });
        },
    });
}

export function useUnlinkLinkedIn() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: () => unlinkLinkedIn(),
        onSettled: () => void qc.invalidateQueries({ queryKey: LINKEDIN_KEY }),
    });
}

// ---------------------------------------------------------------- search

export const finderCatalogQuery = queryOptions({
    queryKey: [...LINKEDIN_KEY, "catalog"],
    queryFn: ({ signal }) => getFinderCatalog(signal),
    staleTime: 10 * 60_000,
});

export function useFinderCatalog(enabled = true) {
    return useQuery({ ...finderCatalogQuery, enabled });
}

/**
 * The results of one query on one account. Every page is a LinkedIn search the account pays for out of its daily
 * budget, so the key sits outside ['linkedin'] and nothing ever refetches it: not focus, not mount, not realtime.
 */
export const finderSearchQuery = (accountId: string | null, query: FinderQuery | null) =>
    infiniteQueryOptions<SearchPage, AppError, InfiniteData<SearchPage, string | null>, readonly unknown[], string | null>({
        queryKey: ["linkedin-search", accountId, query ? JSON.stringify(query) : null] as const,
        queryFn: ({ pageParam, signal }) => runFinderSearch({ account_id: accountId, query: query as FinderQuery, cursor: pageParam }, signal),
        initialPageParam: null,
        getNextPageParam: (last) => last.next_cursor ?? undefined,
        staleTime: Infinity,
        gcTime: 30 * 60_000,
        refetchOnWindowFocus: false,
        refetchOnMount: false,
        refetchOnReconnect: false,
        retry: false,
    });

export function useFinderSearch(accountId: string | null, query: FinderQuery | null) {
    return useInfiniteQuery({ ...finderSearchQuery(accountId, query), enabled: !!query });
}

export function useFinderTypeahead(p: { source: FinderSource; filter: string; q: string; account_id: string | null }, enabled: boolean) {
    return useQuery({
        queryKey: ["linkedin-typeahead", p.source, p.filter, p.q, p.account_id],
        queryFn: ({ signal }) => finderTypeahead(p, signal),
        enabled,
        staleTime: 5 * 60_000,
        placeholderData: keepPreviousData,
        retry: false,
    });
}

export function useParseFinderUrl() {
    return useMutation({ mutationFn: (url: string) => parseFinderUrl(url) });
}

/** Charges the AI balance by what it costs, so the answer is reviewed before anything changes. */
export function useFinderAi() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: finderAi,
        onSuccess: (r) => {
            if (r.cost_micros > 0) void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
        },
    });
}

export function useImportFinderHits() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ body, idempotencyKey }: { body: ImportRequest; idempotencyKey: string }) => importFinderHits(body, idempotencyKey),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: ["contacts"] });
            void qc.invalidateQueries({ queryKey: ["segments"] });
        },
    });
}

export const savedSearchesQuery = queryOptions({
    queryKey: [...LINKEDIN_KEY, "saved-searches"],
    queryFn: ({ signal }) => listSavedSearches(signal),
    staleTime: 60_000,
});

export function useSavedSearches(enabled = true) {
    return useQuery({ ...savedSearchesQuery, enabled });
}

export function useCreateSavedSearch() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: createSavedSearch,
        onSuccess: (s) => qc.setQueryData<SavedSearch[]>(savedSearchesQuery.queryKey, (prev) => [s, ...(prev ?? []).filter((x) => x.id !== s.id)]),
        onError: (e: AppError) => toast.error(linkedInErrorMessage(e)),
        onSettled: () => void qc.invalidateQueries({ queryKey: savedSearchesQuery.queryKey }),
    });
}

/** Rename or overwrite. Optimistic: the list changes first and rolls back on a refusal. */
export function usePatchSavedSearch() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, body }: { id: string; body: { name?: string; query?: FinderQuery } }) => patchSavedSearch(id, body),
        onMutate: async ({ id, body }) => {
            await qc.cancelQueries({ queryKey: savedSearchesQuery.queryKey });
            const prev = qc.getQueryData<SavedSearch[]>(savedSearchesQuery.queryKey);
            qc.setQueryData<SavedSearch[]>(savedSearchesQuery.queryKey, (list) => list?.map((s) => (s.id === id ? { ...s, ...body } : s)));
            return { prev };
        },
        onError: (e: AppError, _v, ctx) => {
            if (ctx?.prev) qc.setQueryData(savedSearchesQuery.queryKey, ctx.prev);
            toast.error(linkedInErrorMessage(e));
        },
        onSettled: () => void qc.invalidateQueries({ queryKey: savedSearchesQuery.queryKey }),
    });
}

/** Optimistic: the row leaves first and returns on a refusal. */
export function useDeleteSavedSearch() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteSavedSearch(id),
        onMutate: async (id) => {
            await qc.cancelQueries({ queryKey: savedSearchesQuery.queryKey });
            const prev = qc.getQueryData<SavedSearch[]>(savedSearchesQuery.queryKey);
            qc.setQueryData<SavedSearch[]>(savedSearchesQuery.queryKey, (list) => list?.filter((s) => s.id !== id));
            return { prev };
        },
        onError: (e: AppError, _v, ctx) => {
            if (ctx?.prev) qc.setQueryData(savedSearchesQuery.queryKey, ctx.prev);
            toast.error(linkedInErrorMessage(e));
        },
        onSettled: () => void qc.invalidateQueries({ queryKey: savedSearchesQuery.queryKey }),
    });
}

/** A full profile for the preview panel; each read is a profile visit, so it is cached and never refetched. */
export function useLinkedInProfile(identifier: string | null, accountId: string | null) {
    return useQuery({
        queryKey: ["linkedin-profile", identifier, accountId],
        queryFn: ({ signal }) => getLinkedInProfile(identifier as string, accountId, signal),
        enabled: !!identifier,
        staleTime: Infinity,
        gcTime: 30 * 60_000,
        refetchOnWindowFocus: false,
        refetchOnMount: false,
        retry: false,
    });
}
