import { useMemo, useSyncExternalStore } from "react";
import {
    infiniteQueryOptions,
    queryOptions,
    useInfiniteQuery,
    useMutation,
    useQuery,
    useQueryClient,
    type InfiniteData,
    type QueryClient,
} from "@tanstack/react-query";
import {
    approveCampaignLeads,
    deleteLeadEmail,
    getCampaignReview,
    getCampaignReviewLeads,
    getLeadEmails,
    putLeadEmail,
    reopenCampaignLeads,
    setCampaignReview,
    writeCampaignReviewAI,
    writeLeadEmailAI,
} from "@/lib/api/client/app/campaigns/review";
import type {
    CampaignReviewSummary,
    LeadEmails,
    ReviewLead,
    ReviewLeadsFilter,
    ReviewLeadsPage,
    ReviewStatus,
} from "@/lib/api/models/app/campaigns/Review";

// Every key starts with ["campaigns", id], so the audit spine's campaign and
// campaign_lead entries refresh the Review tab for every teammate.
export const reviewKey = (campaignId: string) => ["campaigns", campaignId, "review"] as const;

// The list the tab opens on: waiting leads in review mode, else leads not yet emailed, else everyone.
export function openingReviewStatus(s: CampaignReviewSummary): "all" | ReviewStatus {
    if (s.review_leads && s.counts.waiting > 0) return "waiting";
    return s.counts.ready > 0 ? "ready" : "all";
}

export const campaignReviewQuery = (campaignId: string) =>
    queryOptions({
        queryKey: [...reviewKey(campaignId), "summary"],
        queryFn: () => getCampaignReview(campaignId),
        staleTime: 30_000,
        // Background writing has no event of its own, so its progress is read while it runs.
        refetchInterval: (q) => (q.state.data?.ai.auto && q.state.data.ai.pending > 0 ? 10_000 : false),
    });

export const reviewLeadsQuery = (campaignId: string, filter: ReviewLeadsFilter) =>
    infiniteQueryOptions<ReviewLeadsPage, Error, InfiniteData<ReviewLeadsPage, string | null>, readonly unknown[], string | null>({
        queryKey: [...reviewKey(campaignId), "leads", filter.status, filter.missing ?? "", filter.q ?? ""],
        queryFn: ({ pageParam }) => getCampaignReviewLeads(campaignId, filter, pageParam),
        initialPageParam: null,
        getNextPageParam: (last) => (last.pagination.has_more ? last.pagination.next_cursor : undefined),
        staleTime: 30_000,
    });

export const leadEmailsQuery = (campaignId: string, contactId: string) =>
    queryOptions({
        queryKey: [...reviewKey(campaignId), "emails", contactId],
        queryFn: () => getLeadEmails(campaignId, contactId),
        staleTime: 30_000,
    });

export function useCampaignReview(campaignId: string) {
    return useQuery({ ...campaignReviewQuery(campaignId), enabled: !!campaignId });
}

export function useReviewLeads(campaignId: string, filter: ReviewLeadsFilter) {
    const query = useInfiniteQuery({ ...reviewLeadsQuery(campaignId, filter), enabled: !!campaignId });
    const leads = useMemo(() => query.data?.pages.flatMap((p) => p.data) ?? [], [query.data]);
    return { ...query, leads };
}

export function useLeadEmails(campaignId: string, contactId: string | null) {
    return useQuery({ ...leadEmailsQuery(campaignId, contactId ?? ""), enabled: !!campaignId && !!contactId });
}

function refreshReview(qc: QueryClient, campaignId: string) {
    void qc.invalidateQueries({ queryKey: reviewKey(campaignId) });
    void qc.invalidateQueries({ queryKey: ["campaigns", campaignId], exact: true });
    // Holds show on the Leads list and the contact drawer.
    void qc.invalidateQueries({ queryKey: ["contacts"] });
}

// Approving is predictable, so the rows leave "waiting" at once and come back on failure.
function patchStatus(qc: QueryClient, campaignId: string, ids: Set<string> | "all", from: ReviewStatus, to: ReviewStatus) {
    const prefix = [...reviewKey(campaignId), "leads"];
    const snapshot = qc.getQueriesData<InfiniteData<ReviewLeadsPage, string | null>>({ queryKey: prefix });
    const moves = (l: ReviewLead) => l.status === from && (ids === "all" || ids.has(l.contact_id));
    for (const [key, data] of snapshot) {
        if (!data) continue;
        // A list filtered to one state loses the rows that leave it, so they animate out at once.
        const filter = key[4];
        qc.setQueryData<InfiniteData<ReviewLeadsPage, string | null>>(key, {
            ...data,
            pages: data.pages.map((p) => ({
                ...p,
                data:
                    filter === from
                        ? p.data.filter((l) => !moves(l))
                        : p.data.map((l) => (moves(l) ? { ...l, status: to, hold: to === "waiting" ? l.hold : null } : l)),
            })),
        });
    }
    // Undo only this change, so a concurrent approval that succeeded stays applied.
    return () => {
        for (const [key, before] of snapshot) {
            const now = qc.getQueryData<InfiniteData<ReviewLeadsPage, string | null>>(key);
            if (!before || !now) continue;
            const current = new Map(now.pages.flatMap((p) => p.data).map((l) => [l.contact_id, l]));
            qc.setQueryData<InfiniteData<ReviewLeadsPage, string | null>>(key, {
                ...before,
                pages: before.pages.map((p) => ({
                    ...p,
                    data: p.data.flatMap((l) => {
                        if (moves(l)) return [l];
                        const cur = current.get(l.contact_id);
                        return cur ? [cur] : [];
                    }),
                })),
            });
        }
    };
}

export function useApproveLeads(campaignId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (body: { contact_ids?: string[]; all?: boolean }) => approveCampaignLeads(campaignId, body),
        onMutate: async (body) => {
            await qc.cancelQueries({ queryKey: [...reviewKey(campaignId), "leads"] });
            return { restore: patchStatus(qc, campaignId, body.all ? "all" : new Set(body.contact_ids), "waiting", "ready") };
        },
        onError: (_e, _v, ctx) => ctx?.restore(),
        onSettled: () => refreshReview(qc, campaignId),
    });
}

export function useReopenLeads(campaignId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (contactIds: string[]) => reopenCampaignLeads(campaignId, contactIds),
        onMutate: async (ids) => {
            await qc.cancelQueries({ queryKey: [...reviewKey(campaignId), "leads"] });
            return { restore: patchStatus(qc, campaignId, new Set(ids), "ready", "waiting") };
        },
        onError: (_e, _v, ctx) => ctx?.restore(),
        onSettled: () => refreshReview(qc, campaignId),
    });
}

// Turning review off approves every waiting lead, which the server decides; it waits for the answer.
export function useSetReview(campaignId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (enabled: boolean) => setCampaignReview(campaignId, enabled),
        onSettled: () => refreshReview(qc, campaignId),
    });
}

export function usePersonalizeLeadEmail(campaignId: string, contactId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ stepId, ...body }: { stepId: string; subject: string; body_html: string; body_plain: string }) =>
            putLeadEmail(campaignId, contactId, stepId, body),
        onSettled: () => refreshReview(qc, campaignId),
    });
}

export function useResetLeadEmail(campaignId: string, contactId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (stepId: string) => deleteLeadEmail(campaignId, contactId, stepId),
        onSettled: () => refreshReview(qc, campaignId),
    });
}

export function useWriteReviewAIPass(campaignId: string) {
    return useMutation({
        mutationFn: (body: { step_id?: string; contact_ids?: string[]; waiting_only?: boolean; after?: string }) =>
            writeCampaignReviewAI(campaignId, body),
    });
}

export { refreshReview };

const aiWrites = new Map<string, Promise<LeadEmails>>();
// Writes in flight per lead, and those that overlapped another write for the same lead.
const leadWrites = new Map<string, Set<object>>();
const overlapped = new WeakSet<object>();
const aiWriteListeners = new Set<() => void>();
let aiWritesVersion = 0;
function aiWritesChanged() {
    aiWritesVersion++;
    aiWriteListeners.forEach((l) => l());
}

// Writes one lead's AI copy for one step and keeps their emails in the cache.
// A write already running for the same lead and step is joined, not repeated,
// so opening a lead the background is writing waits for that one. Copy already
// written costs nothing: the server answers from what it has.
export function writeLeadAIOnce(qc: QueryClient, campaignId: string, contactId: string, stepId: string, rewrite = false): Promise<LeadEmails> {
    const key = `${campaignId}:${contactId}:${stepId}`;
    const running = aiWrites.get(key);
    if (running && !rewrite) return running;
    const token = {};
    const lead = `${campaignId}:${contactId}`;
    const send = () => {
        const peers = leadWrites.get(lead) ?? new Set<object>();
        if (peers.size > 0) {
            peers.forEach((t) => overlapped.add(t));
            overlapped.add(token);
        }
        peers.add(token);
        leadWrites.set(lead, peers);
        return writeLeadEmailAI(campaignId, contactId, stepId, rewrite).finally(() => {
            peers.delete(token);
            if (peers.size === 0 && leadWrites.get(lead) === peers) leadWrites.delete(lead);
        });
    };
    // A rewrite waits for the write already running for this step, so the entry spans both.
    const p = (running ? running.catch(() => undefined).then(send) : send())
        .then((data) => {
            const emailsKey = leadEmailsQuery(campaignId, contactId).queryKey;
            // Each answer carries every step, so one that raced another write for the lead may be stale.
            if (overlapped.has(token)) void qc.invalidateQueries({ queryKey: emailsKey, exact: true });
            else qc.setQueryData(emailsKey, data);
            void qc.invalidateQueries({ queryKey: [...reviewKey(campaignId), "summary"] });
            void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
            return data;
        })
        .catch((err: unknown) => {
            // Blocks written before the failure stay saved and charged, so what shows must be reread.
            void qc.invalidateQueries({ queryKey: leadEmailsQuery(campaignId, contactId).queryKey, exact: true });
            void qc.invalidateQueries({ queryKey: [...reviewKey(campaignId), "summary"] });
            void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
            throw err;
        })
        .finally(() => {
            if (aiWrites.get(key) === p) {
                aiWrites.delete(key);
                aiWritesChanged();
            }
        });
    aiWrites.set(key, p);
    aiWritesChanged();
    return p;
}

export function aiWriteRunning(campaignId: string, contactId: string, stepId: string): boolean {
    return aiWrites.has(`${campaignId}:${contactId}:${stepId}`);
}

// The leads of a campaign whose AI copy is being written right now, for any step.
export function useLeadsBeingWritten(campaignId: string): Set<string> {
    const version = useSyncExternalStore(
        (l) => {
            aiWriteListeners.add(l);
            return () => aiWriteListeners.delete(l);
        },
        () => aiWritesVersion,
    );
    return useMemo(() => {
        void version;
        const ids = new Set<string>();
        for (const key of aiWrites.keys()) {
            const [c, contact] = key.split(":");
            if (c === campaignId) ids.add(contact);
        }
        return ids;
    }, [campaignId, version]);
}
