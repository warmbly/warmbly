// Social reads and writes, under ['social'] so the SOCIAL_* realtime events and the social_* audit spine refresh
// every teammate's view. LinkedIn search results live outside it, so nothing refreshes a page someone paid for.
import { useEffect } from "react";
import { infiniteQueryOptions, keepPreviousData, queryOptions, useInfiniteQuery, useMutation, useQuery, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import toast from "react-hot-toast/headless";
import {
    answerConnection,
    cancelConnection,
    chooseConnectionMethod,
    completeSocialLink,
    createConnection,
    createSavedSearch,
    deleteSavedSearch,
    finderAi,
    finderTypeahead,
    getCampaignSocialAccounts,
    getConnection,
    getConnectQuote,
    getSocialContactActivity,
    getSocialConversation,
    getLinkedInImport,
    getFinderCatalog,
    getSocialAccount,
    getLinkedInProfile,
    getSocialChannels,
    getSocialStatus,
    importFinderHits,
    socialAccountAction,
    listSocialAccounts,
    listSocialConversations,
    listSocialMessages,
    listSavedSearches,
    markSocialConversationRead,
    parseFinderUrl,
    patchSocialAccount,
    patchSavedSearch,
    putCampaignSocialAccounts,
    removeSocialAccount,
    resendConnectionCode,
    runFinderSearch,
    sendSocialMessage,
    signInConnection,
    startSocialLink,
    unlinkSocial,
    uploadSocialAudio,
    type AccountAction,
} from "@/lib/api/client/app/social/social";
import type { AppError } from "@/lib/api/client/normalizeError";
import type {
    ChannelSpec,
    CheckpointAnswer,
    ConnectSession,
    CreateConnection,
    FinderQuery,
    FinderSource,
    ImportRequest,
    SocialAccount,
    SocialAccountPatch,
    SocialChannel,
    SocialConversation,
    LinkedInImport,
    SocialList,
    SocialMessage,
    SocialStatus,
    SavedSearch,
    SearchPage,
    SignIn,
} from "@/lib/api/models/app/social/Social";
import { socialErrorMessage } from "@/lib/social/errors";

export const SOCIAL_KEY = ["social"] as const;

export const socialStatusQuery = queryOptions({
    queryKey: [...SOCIAL_KEY, "status"],
    queryFn: ({ signal }) => getSocialStatus(signal),
    staleTime: 30_000,
});

export function useSocialStatus(enabled = true) {
    return useQuery({ ...socialStatusQuery, enabled, retry: 1 });
}

/** The channel catalog. It changes only with a GraphKit release, so it is read once and kept. */
export const socialChannelsQuery = queryOptions({
    queryKey: [...SOCIAL_KEY, "channels"],
    queryFn: ({ signal }) => getSocialChannels(signal),
    staleTime: 30 * 60_000,
    retry: 1,
});

export function useSocialChannels(enabled = true) {
    return useQuery({ ...socialChannelsQuery, enabled });
}

/** One channel's catalog entry, when the catalog has loaded. */
export function useChannelSpec(channel: SocialChannel | null | undefined): ChannelSpec | undefined {
    const q = useSocialChannels(!!channel);
    return channel ? q.data?.find((c) => c.channel === channel) : undefined;
}

export const socialAccountsQuery = queryOptions({
    queryKey: [...SOCIAL_KEY, "accounts", "list"],
    queryFn: ({ signal }) => listSocialAccounts(signal),
    staleTime: 15_000,
});

export function useSocialAccounts(enabled = true) {
    return useQuery({ ...socialAccountsQuery, enabled });
}

export const socialAccountQuery = (id: string) =>
    queryOptions({
        queryKey: [...SOCIAL_KEY, "accounts", id],
        queryFn: ({ signal }) => getSocialAccount(id, signal),
        staleTime: 15_000,
    });

/** One account, seeded from the list so the drawer opens with it already there. */
export function useSocialAccount(id: string | null) {
    const qc = useQueryClient();
    return useQuery({
        ...socialAccountQuery(id ?? ""),
        enabled: !!id,
        initialData: () => qc.getQueryData<SocialList<SocialAccount>>(socialAccountsQuery.queryKey)?.data.find((a) => a.id === id),
        initialDataUpdatedAt: () => qc.getQueryState(socialAccountsQuery.queryKey)?.dataUpdatedAt,
    });
}

export const connectQuoteQuery = (accountId: string | null) =>
    queryOptions({
        queryKey: [...SOCIAL_KEY, "quote", accountId ?? "new"],
        queryFn: ({ signal }) => getConnectQuote(accountId, signal),
        staleTime: 30_000,
        retry: false,
    });

export function useConnectQuote(accountId: string | null, enabled: boolean) {
    return useQuery({ ...connectQuoteQuery(accountId), enabled });
}

/** A session waits on someone outside the browser (an app approval, a QR scan, a typed pairing code) or on the provider preparing the linked session. */
export const isWaitingCheckpoint = (s: ConnectSession | undefined) =>
    s?.status === "checkpoint" &&
    (s.checkpoint?.type === "app_approval" ||
        s.checkpoint?.type === "otp_or_app" ||
        s.checkpoint?.type === "qr_code" ||
        s.checkpoint?.type === "pairing_code" ||
        s.checkpoint?.type === "loading");

/** A QR code rotates, so it is re-read every 2 s; the other waits every 3 s. */
const waitInterval = (s: ConnectSession | undefined) => (!isWaitingCheckpoint(s) ? false : s?.checkpoint?.type === "qr_code" ? 2_000 : 3_000);

export const connectionQuery = (id: string) =>
    queryOptions({
        queryKey: [...SOCIAL_KEY, "connections", id],
        queryFn: ({ signal }) => getConnection(id, signal),
        staleTime: 2_000,
    });

/** A connect session. Realtime moves it forward; the re-read runs only while a waiting checkpoint is on screen. */
export function useConnection(id: string | null) {
    const qc = useQueryClient();
    const q = useQuery({
        ...connectionQuery(id ?? ""),
        enabled: !!id,
        refetchInterval: (q) => waitInterval(q.state.data),
    });
    // A QR or pairing sign-in finishes on a re-read, with no mutation or socket event to refresh the list.
    const succeeded = q.data?.status === "succeeded";
    useEffect(() => {
        if (!succeeded) return;
        void qc.invalidateQueries({ queryKey: [...SOCIAL_KEY, "accounts"] });
        void qc.invalidateQueries({ queryKey: [...SOCIAL_KEY, "status"] });
    }, [succeeded, qc]);
    return q;
}

function setSession(qc: ReturnType<typeof useQueryClient>, s: ConnectSession) {
    qc.setQueryData(connectionQuery(s.id).queryKey, s);
    if (s.status === "succeeded") {
        void qc.invalidateQueries({ queryKey: [...SOCIAL_KEY, "accounts"] });
        void qc.invalidateQueries({ queryKey: [...SOCIAL_KEY, "status"] });
    }
}

/** Starts a connect or reconnect session; a new seat is charged here, so nothing is optimistic. */
export function useCreateConnection() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ body, idempotencyKey }: { body: CreateConnection; idempotencyKey: string }) => createConnection(body, idempotencyKey),
        onSuccess: (r) => {
            setSession(qc, r.session);
            void qc.invalidateQueries({ queryKey: [...SOCIAL_KEY, "status"] });
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
        onError: (e: AppError) => toast.error(socialErrorMessage(e)),
    });
}

export function useCancelConnection() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => cancelConnection(id),
        onSettled: (_r, _e, id) => {
            qc.removeQueries({ queryKey: connectionQuery(id).queryKey });
            void qc.invalidateQueries({ queryKey: [...SOCIAL_KEY, "accounts"] });
            void qc.invalidateQueries({ queryKey: [...SOCIAL_KEY, "status"] });
        },
    });
}

function patchAccountInCache(qc: ReturnType<typeof useQueryClient>, id: string, fn: (a: SocialAccount) => SocialAccount) {
    qc.setQueryData<SocialList<SocialAccount>>(socialAccountsQuery.queryKey, (prev) => (prev ? { ...prev, data: prev.data.map((a) => (a.id === id ? fn(a) : a)) } : prev));
    qc.setQueryData<SocialAccount>(socialAccountQuery(id).queryKey, (prev) => (prev ? fn(prev) : prev));
}

function snapshotAccount(qc: ReturnType<typeof useQueryClient>, id: string) {
    return {
        list: qc.getQueryData<SocialList<SocialAccount>>(socialAccountsQuery.queryKey),
        one: qc.getQueryData<SocialAccount>(socialAccountQuery(id).queryKey),
        id,
    };
}

function restoreAccount(qc: ReturnType<typeof useQueryClient>, ctx: ReturnType<typeof snapshotAccount> | undefined) {
    if (!ctx) return;
    if (ctx.list) qc.setQueryData(socialAccountsQuery.queryKey, ctx.list);
    if (ctx.one) qc.setQueryData(socialAccountQuery(ctx.id).queryKey, ctx.one);
}

/** Limits, hours and warm-up. Optimistic: the account changes first and rolls back on a refusal. */
export function usePatchSocialAccount() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, patch }: { id: string; patch: SocialAccountPatch }) => patchSocialAccount(id, patch),
        onMutate: async ({ id, patch }) => {
            await qc.cancelQueries({ queryKey: [...SOCIAL_KEY, "accounts"] });
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
            toast.error(socialErrorMessage(e));
        },
        onSuccess: (a) => patchAccountInCache(qc, a.id, () => a),
        onSettled: (_d, _e, { id }) => {
            void qc.invalidateQueries({ queryKey: socialAccountsQuery.queryKey });
            void qc.invalidateQueries({ queryKey: socialAccountQuery(id).queryKey });
        },
    });
}

/** Pause and resume are predictable and flip the status first; a resync only asks, so it waits. */
export function useSocialAccountAction() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, action }: { id: string; action: AccountAction }) => socialAccountAction(id, action),
        onMutate: async ({ id, action }) => {
            if (action === "resync") return undefined;
            await qc.cancelQueries({ queryKey: [...SOCIAL_KEY, "accounts"] });
            const ctx = snapshotAccount(qc, id);
            patchAccountInCache(qc, id, (a) => ({ ...a, status: action === "pause" ? "paused" : "active" }));
            return ctx;
        },
        onError: (e: AppError, _v, ctx) => {
            restoreAccount(qc, ctx);
            toast.error(socialErrorMessage(e));
        },
        onSuccess: (a, { action }) => {
            if (a && typeof a === "object" && "id" in a) patchAccountInCache(qc, a.id, () => a);
            if (action === "resync") toast.success("Refreshing this account.");
        },
        onSettled: () => void qc.invalidateQueries({ queryKey: [...SOCIAL_KEY, "accounts"] }),
    });
}

/** Removes a seat; it ends billing, so it waits for the answer. */
export function useRemoveSocialAccount() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => removeSocialAccount(id),
        onSuccess: (_r, id) => {
            qc.setQueryData<SocialList<SocialAccount>>(socialAccountsQuery.queryKey, (prev) => (prev ? { ...prev, data: prev.data.filter((a) => a.id !== id) } : prev));
            qc.removeQueries({ queryKey: socialAccountQuery(id).queryKey });
        },
        onSettled: () => {
            void qc.invalidateQueries({ queryKey: [...SOCIAL_KEY, "accounts"] });
            void qc.invalidateQueries({ queryKey: [...SOCIAL_KEY, "status"] });
        },
    });
}

// ---------------------------------------------------------------- GraphKit link

export function useStartSocialLink() {
    return useMutation({
        mutationFn: () => startSocialLink(),
        onError: (e: AppError) => toast.error(socialErrorMessage(e)),
    });
}

export function useCompleteSocialLink() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (session: string) => completeSocialLink(session),
        onSuccess: (s: SocialStatus) => {
            qc.setQueryData(socialStatusQuery.queryKey, s);
            void qc.invalidateQueries({ queryKey: SOCIAL_KEY });
        },
    });
}

export function useUnlinkSocial() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: () => unlinkSocial(),
        onSettled: () => void qc.invalidateQueries({ queryKey: SOCIAL_KEY }),
    });
}

// ---------------------------------------------------------------- search

export const finderCatalogQuery = queryOptions({
    queryKey: [...SOCIAL_KEY, "catalog"],
    queryFn: ({ signal }) => getFinderCatalog(signal),
    staleTime: 10 * 60_000,
});

export function useFinderCatalog(enabled = true) {
    return useQuery({ ...finderCatalogQuery, enabled });
}

/**
 * The results of one query on one account. Every page is a LinkedIn search the account pays for out of its daily
 * budget, so the key sits outside ['social'] and nothing ever refetches it: not focus, not mount, not realtime.
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
        onSuccess: (r) => {
            // A background import refreshes contacts when it finishes, not when it is queued.
            if ("import_id" in r) return;
            void qc.invalidateQueries({ queryKey: ["contacts"] });
            void qc.invalidateQueries({ queryKey: ["segments"] });
        },
    });
}

export const linkedInImportQuery = (id: string) =>
    queryOptions({
        queryKey: [...SOCIAL_KEY, "imports", id],
        queryFn: ({ signal }) => getLinkedInImport(id, signal),
        staleTime: 2_000,
    });

export const importFinished = (i: LinkedInImport | undefined) => i?.status === "done" || i?.status === "failed";

/** A background import. SOCIAL_IMPORT_UPDATED moves it; a 5 s re-read covers a missed event until it finishes. */
export function useLinkedInImport(id: string | null) {
    const qc = useQueryClient();
    const q = useQuery({
        ...linkedInImportQuery(id ?? ""),
        enabled: !!id,
        refetchInterval: (query) => (importFinished(query.state.data) ? false : 5_000),
    });
    const done = importFinished(q.data);
    useEffect(() => {
        if (!done) return;
        void qc.invalidateQueries({ queryKey: ["contacts"] });
        void qc.invalidateQueries({ queryKey: ["segments"] });
        void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
    }, [done, qc]);
    return q;
}

export const savedSearchesQuery = queryOptions({
    queryKey: [...SOCIAL_KEY, "saved-searches"],
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
        onError: (e: AppError) => toast.error(socialErrorMessage(e)),
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
            toast.error(socialErrorMessage(e));
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
            toast.error(socialErrorMessage(e));
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

// ---------------------------------------------------------------- conversations

export interface ConversationFilter {
    accountId?: string | null;
    contactId?: string | null;
    channel?: SocialChannel | null;
    unread?: boolean;
}

type ConversationPages = InfiniteData<SocialList<SocialConversation>, string | null>;
type MessagePages = InfiniteData<SocialList<SocialMessage>, string | null>;

const CONVERSATIONS_LIST = [...SOCIAL_KEY, "conversations", "list"] as const;

export const socialConversationsQuery = (f: ConversationFilter = {}) =>
    infiniteQueryOptions({
        queryKey: [...CONVERSATIONS_LIST, f.accountId ?? null, f.contactId ?? null, f.channel ?? null, !!f.unread] as const,
        queryFn: ({ pageParam, signal }) => listSocialConversations({ cursor: pageParam, ...f }, signal),
        initialPageParam: null as string | null,
        getNextPageParam: (last) => (last.pagination?.has_more ? (last.pagination.next_cursor ?? undefined) : undefined),
        staleTime: 15_000,
        retry: false,
    });

/** The workspace's social conversations, latest activity first. SOCIAL_MESSAGE refreshes it. */
export function useSocialConversations(f: ConversationFilter = {}, enabled = true) {
    return useInfiniteQuery({ ...socialConversationsQuery(f), enabled });
}

/** Whether the unibox offers its Social scope (only once an account exists), and the unread count for its rail row. */
export function useSocialUnibox(overviewUnread: number | undefined): { visible: boolean; unread: number } {
    const status = useSocialStatus();
    const has = !!status.data?.available && (status.data.accounts ?? 0) > 0;
    // A backend that counts it in the overview spares the list read.
    const unreadList = useSocialConversations({ unread: true }, has && overviewUnread === undefined);
    const unread = overviewUnread ?? (unreadList.data?.pages[0]?.data ?? []).reduce((n, c) => n + (c.unread_count ?? 0), 0);
    return { visible: has || unread > 0, unread };
}

export const socialConversationQuery = (id: string) =>
    queryOptions({
        queryKey: [...SOCIAL_KEY, "conversations", id],
        queryFn: ({ signal }) => getSocialConversation(id, signal),
        staleTime: 15_000,
    });

function findConversation(qc: ReturnType<typeof useQueryClient>, id: string): SocialConversation | undefined {
    for (const [, data] of qc.getQueriesData<ConversationPages>({ queryKey: CONVERSATIONS_LIST })) {
        const hit = data?.pages.flatMap((p) => p.data).find((c) => c.id === id);
        if (hit) return hit;
    }
    return undefined;
}

/** One conversation, seeded from whichever list holds it so the thread opens with its header already drawn. */
export function useSocialConversation(id: string | null) {
    const qc = useQueryClient();
    return useQuery({
        ...socialConversationQuery(id ?? ""),
        enabled: !!id,
        initialData: () => (id ? findConversation(qc, id) : undefined),
        initialDataUpdatedAt: 0,
    });
}

export const socialMessagesQuery = (id: string) =>
    infiniteQueryOptions({
        queryKey: [...SOCIAL_KEY, "conversations", id, "messages"] as const,
        queryFn: ({ pageParam, signal }) => listSocialMessages(id, pageParam, signal),
        initialPageParam: null as string | null,
        getNextPageParam: (last) => (last.pagination?.has_more ? (last.pagination.next_cursor ?? undefined) : undefined),
        staleTime: 10_000,
    });

/** A conversation's messages, newest first per page. */
export function useSocialMessages(id: string | null) {
    return useInfiniteQuery({ ...socialMessagesQuery(id ?? ""), enabled: !!id });
}

function patchConversation(qc: ReturnType<typeof useQueryClient>, id: string, fn: (c: SocialConversation) => SocialConversation) {
    qc.setQueriesData<ConversationPages>({ queryKey: CONVERSATIONS_LIST }, (prev) =>
        prev ? { ...prev, pages: prev.pages.map((p) => ({ ...p, data: p.data.map((c) => (c.id === id ? fn(c) : c)) })) } : prev,
    );
    qc.setQueryData<SocialConversation>(socialConversationQuery(id).queryKey, (prev) => (prev ? fn(prev) : prev));
}

/** Clears a conversation's unread count first; the read is not worth a toast if the channel refuses it. */
export function useMarkSocialRead() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => markSocialConversationRead(id),
        onMutate: async (id) => {
            await qc.cancelQueries({ queryKey: CONVERSATIONS_LIST });
            const before = findConversation(qc, id)?.unread_count ?? qc.getQueryData<SocialConversation>(socialConversationQuery(id).queryKey)?.unread_count ?? 0;
            patchConversation(qc, id, (c) => ({ ...c, unread_count: 0 }));
            return { before };
        },
        onError: (_e, id, ctx) => {
            if (ctx?.before) patchConversation(qc, id, (c) => ({ ...c, unread_count: ctx.before }));
        },
        onSettled: () => {
            void qc.invalidateQueries({ queryKey: CONVERSATIONS_LIST });
            void qc.invalidateQueries({ queryKey: ["unibox", "overview"] });
        },
    });
}

export const PENDING_PREFIX = "pending:";

/** A reply. The pending bubble shows at once; a refusal leaves it marked failed with the reason, ready to retry. */
export function useSendSocialMessage(conversationId: string) {
    const qc = useQueryClient();
    const key = socialMessagesQuery(conversationId).queryKey;
    const replace = (tempId: string, fn: (m: SocialMessage) => SocialMessage) =>
        qc.setQueryData<MessagePages>(key, (prev) =>
            prev ? { ...prev, pages: prev.pages.map((p) => ({ ...p, data: p.data.map((m) => (m.id === tempId ? fn(m) : m)) })) } : prev,
        );
    return useMutation({
        mutationFn: ({ body, idempotencyKey }: { body: string; idempotencyKey: string; tempId: string; userId?: string | null }) =>
            sendSocialMessage(conversationId, body, idempotencyKey),
        onMutate: async ({ body, tempId, userId }) => {
            await qc.cancelQueries({ queryKey: key });
            const now = new Date().toISOString();
            const pending: SocialMessage = {
                id: tempId,
                conversation_id: conversationId,
                direction: "outbound",
                body,
                sent_at: null,
                status: "pending",
                kind: "message",
                sent_by: userId ?? null,
                created_at: now,
            };
            qc.setQueryData<MessagePages>(key, (prev) => {
                if (!prev || prev.pages.length === 0) return { pages: [{ data: [pending] }], pageParams: [null] };
                const [first, ...rest] = prev.pages;
                return { ...prev, pages: [{ ...first, data: [pending, ...first.data.filter((m) => m.id !== tempId)] }, ...rest] };
            });
            patchConversation(qc, conversationId, (c) => ({ ...c, last_message_preview: body.replace(/\s+/g, " ").slice(0, 140), last_message_at: now }));
        },
        onSuccess: (m, { tempId }) => replace(tempId, () => m),
        onError: (_e, { tempId }) => replace(tempId, (m) => ({ ...m, status: "failed" })),
        onSettled: (_d, e) => {
            if (!e) void qc.invalidateQueries({ queryKey: key });
            void qc.invalidateQueries({ queryKey: CONVERSATIONS_LIST });
        },
    });
}

/** Drops a failed pending bubble, before a retry or when the reply is given up. */
export function useDiscardPendingSocialMessage(conversationId: string) {
    const qc = useQueryClient();
    return (tempId: string) =>
        qc.setQueryData<MessagePages>(socialMessagesQuery(conversationId).queryKey, (prev) =>
            prev ? { ...prev, pages: prev.pages.map((p) => ({ ...p, data: p.data.filter((m) => m.id !== tempId) })) } : prev,
        );
}

// ---------------------------------------------------------------- contact activity

export const socialContactActivityQuery = (contactId: string) =>
    queryOptions({
        queryKey: [...SOCIAL_KEY, "contacts", contactId, "activity"],
        queryFn: ({ signal }) => getSocialContactActivity(contactId, signal),
        staleTime: 30_000,
        retry: false,
    });

export function useSocialContactActivity(contactId: string, enabled = true) {
    return useQuery({ ...socialContactActivityQuery(contactId), enabled: enabled && !!contactId });
}

// ---------------------------------------------------------------- campaign senders

export const campaignSocialAccountsQuery = (campaignId: string) =>
    queryOptions({
        queryKey: ["campaigns", campaignId, "social-accounts"],
        queryFn: ({ signal }) => getCampaignSocialAccounts(campaignId, signal),
        staleTime: 30_000,
        retry: false,
    });

export function useCampaignSocialAccounts(campaignId: string, enabled = true) {
    return useQuery({ ...campaignSocialAccountsQuery(campaignId), enabled: enabled && !!campaignId });
}

/** Replaces the campaign's social senders across channels; the campaign detail carries the same ids, so it refreshes too. */
export function usePutCampaignSocialAccounts(campaignId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (accountIds: string[]) => putCampaignSocialAccounts(campaignId, accountIds),
        onSuccess: (ids) => {
            qc.setQueryData(campaignSocialAccountsQuery(campaignId).queryKey, ids);
            qc.setQueryData(["campaigns", campaignId], (old: unknown) => (old ? { ...(old as object), social_account_ids: ids } : old));
        },
        onSettled: () => void qc.invalidateQueries({ queryKey: ["campaigns", campaignId] }),
    });
}

// ---------------------------------------------------------------- voice note audio

/** Stores a voice note recording; the step keeps the returned id. */
export function useUploadSocialAudio() {
    return useMutation({
        mutationFn: ({ file, durationSeconds, filename }: { file: Blob; durationSeconds: number; filename?: string }) => uploadSocialAudio(file, durationSeconds, filename),
        onError: (e: AppError) => toast.error(socialErrorMessage(e)),
    });
}
