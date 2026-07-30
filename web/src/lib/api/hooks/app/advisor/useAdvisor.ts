import { useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast";
import type {
    AdvisorEntityIndex,
    AdvisorFinding,
    AdvisorFindingsQuery,
    AdvisorSettings,
    AdvisorSummary,
    AdvisorSurface,
} from "@/lib/api/models/app/advisor/Advisor";
import { indexByEntity } from "@/lib/api/models/app/advisor/Advisor";
import {
    applyAdvisorFinding,
    dismissAdvisorFinding,
    getAdvisorFindings,
    getAdvisorSettings,
    getAdvisorSummary,
    refreshAdvisor,
    snoozeAdvisorFinding,
    submitAdvisorFeedback,
    undoAdvisorFinding,
    updateAdvisorSettings,
} from "@/lib/api/client/app/advisor/advisor";

// Everything is keyed under ["advisor", ...] so one spine entry in
// useRealtimeEvents keeps every strip and nav badge live. No polling: the
// backend audits each evaluation that changes something, which broadcasts
// org-wide.
const ADVISOR_KEY = ["advisor"] as const;

export function useAdvisorSummary(enabled = true) {
    return useQuery<AdvisorSummary>({
        queryKey: [...ADVISOR_KEY, "summary"],
        queryFn: getAdvisorSummary,
        enabled,
        // The findings themselves only move on the backend's own schedule, so a
        // long stale time keeps navigation from refetching on every route change.
        staleTime: 60_000,
    });
}

export function useAdvisorFindings(query: AdvisorFindingsQuery = {}, enabled = true) {
    return useQuery<AdvisorFinding[]>({
        queryKey: [...ADVISOR_KEY, "findings", query],
        queryFn: () => getAdvisorFindings(query),
        enabled,
        staleTime: 60_000,
    });
}

// A surface page pulls its whole run in one request. It has to be the whole
// run: a page that fetched only what fits on screen would leave rows below the
// fold silently unflagged, which is worse than not flagging at all.
export const SURFACE_FETCH_LIMIT = 200;

// useAdvisorEntityIndex gives a list page one request for the whole surface and
// an index every row reads its own advice from. Rows must never fetch: twenty
// mailboxes would mean twenty requests, and the flags would pop in one by one.
//
// The query shape matches what AdvisorStrip asks for, so the page-level summary
// and the row flags share a single cache entry rather than racing each other.
export function useAdvisorEntityIndex(surface: AdvisorSurface, enabled = true): AdvisorEntityIndex {
    const { data } = useAdvisorFindings({ surface, limit: SURFACE_FETCH_LIMIT }, enabled);
    return useMemo(() => indexByEntity(data ?? []), [data]);
}

// invalidateAdvisor refreshes every advisor query plus whatever the applied fix
// changed, so the mailbox row or campaign header updates in the same tick as
// the card clearing.
function useAdvisorInvalidator() {
    const queryClient = useQueryClient();
    return (finding?: AdvisorFinding) => {
        void queryClient.invalidateQueries({ queryKey: ADVISOR_KEY });
        if (!finding) return;
        switch (finding.entity_type) {
            case "email_account":
                void queryClient.invalidateQueries({ queryKey: ["emails"] });
                void queryClient.invalidateQueries({ queryKey: ["analytics"] });
                break;
            case "campaign":
                void queryClient.invalidateQueries({ queryKey: ["campaigns"] });
                break;
            case "step":
                void queryClient.invalidateQueries({ queryKey: ["campaigns"] });
                break;
        }
    };
}

export function useApplyAdvisorFinding() {
    const invalidate = useAdvisorInvalidator();
    return useMutation({
        mutationFn: (id: string) => applyAdvisorFinding(id),
        onSuccess: (finding) => {
            invalidate(finding);
            toast.success("Applied");
        },
    });
}

export function useUndoAdvisorFinding() {
    const invalidate = useAdvisorInvalidator();
    return useMutation({
        mutationFn: (id: string) => undoAdvisorFinding(id),
        onSuccess: (finding) => {
            invalidate(finding);
            toast.success("Reverted");
        },
    });
}

export function useSnoozeAdvisorFinding() {
    const invalidate = useAdvisorInvalidator();
    return useMutation({
        mutationFn: ({ id, days }: { id: string; days: number }) => snoozeAdvisorFinding(id, days),
        onSuccess: () => invalidate(),
    });
}

export function useDismissAdvisorFinding() {
    const invalidate = useAdvisorInvalidator();
    return useMutation({
        mutationFn: ({ id, reason }: { id: string; reason: string }) => dismissAdvisorFinding(id, reason),
        onSuccess: () => invalidate(),
    });
}

export function useAdvisorFeedback() {
    return useMutation({
        mutationFn: ({ id, helpful, reason }: { id: string; helpful: boolean; reason?: string }) =>
            submitAdvisorFeedback(id, helpful, reason ?? ""),
    });
}

export function useRefreshAdvisor() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: refreshAdvisor,
        onSuccess: () => {
            void queryClient.invalidateQueries({ queryKey: ADVISOR_KEY });
        },
    });
}

export function useAdvisorSettings() {
    return useQuery<AdvisorSettings>({
        queryKey: [...ADVISOR_KEY, "settings"],
        queryFn: getAdvisorSettings,
    });
}

export function useUpdateAdvisorSettings() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (s: Partial<AdvisorSettings>) => updateAdvisorSettings(s),
        onSuccess: (data) => {
            queryClient.setQueryData([...ADVISOR_KEY, "settings"], data);
            void queryClient.invalidateQueries({ queryKey: ADVISOR_KEY });
        },
    });
}
