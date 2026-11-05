// Warmup own copy. Everything lives under ["warmup-copy"], which the
// warmup_template audit spine invalidates, so a teammate's change lands live.

import { keepPreviousData, queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    checkWarmupTemplate,
    createWarmupTemplate,
    deleteWarmupTemplate,
    getWarmupCopySettings,
    getWarmupTemplateStats,
    getWarmupTemplate,
    listWarmupCampaignCopy,
    listWarmupTemplates,
    putWarmupCopySettings,
} from "@/lib/api/client/app/warmup/warmupCopy";
import type { WarmupCopySettings, WarmupTemplateInput, WarmupTemplateList } from "@/lib/api/models/app/warmup/WarmupCopy";
import { patchQueries, restoreQueries, settle } from "@/lib/api/hooks/optimistic";

export const WARMUP_COPY_KEY = ["warmup-copy"] as const;

export const warmupCopySettingsQuery = queryOptions({
    queryKey: [...WARMUP_COPY_KEY, "settings"],
    queryFn: getWarmupCopySettings,
});

export function useWarmupCopySettings() {
    return useQuery(warmupCopySettingsQuery);
}

const settingsMutationKey = [...WARMUP_COPY_KEY, "settings", "update"];

// Optimistic: the saved settings are exactly what was sent.
export function useUpdateWarmupCopySettings() {
    const qc = useQueryClient();
    return useMutation({
        mutationKey: settingsMutationKey,
        mutationFn: (settings: WarmupCopySettings) => putWarmupCopySettings(settings),
        onMutate: (settings) => patchQueries(qc, [warmupCopySettingsQuery.queryKey], () => settings),
        onError: (_err, _settings, snapshot) => restoreQueries(qc, snapshot),
        onSettled: () => settle(qc, settingsMutationKey, [warmupCopySettingsQuery.queryKey]),
    });
}

export const warmupCampaignCopyQuery = (days: number) =>
    queryOptions({
        queryKey: [...WARMUP_COPY_KEY, "campaign-copy", days],
        queryFn: () => listWarmupCampaignCopy(days),
    });

export function useWarmupCampaignCopy(days: number) {
    const query = useQuery({ ...warmupCampaignCopyQuery(days), placeholderData: keepPreviousData });
    return { ...query, steps: query.data?.data ?? [] };
}

export const warmupTemplatesQuery = (days: number) =>
    queryOptions({
        queryKey: [...WARMUP_COPY_KEY, "templates", days],
        queryFn: () => listWarmupTemplates(days),
    });

export function useWarmupTemplates(days: number) {
    const query = useQuery({ ...warmupTemplatesQuery(days), placeholderData: keepPreviousData });
    return { ...query, templates: query.data?.templates ?? [] };
}

export const warmupTemplateStatsQuery = (id: string, days: number) =>
    queryOptions({
        queryKey: [...WARMUP_COPY_KEY, "template-stats", id, days],
        queryFn: () => getWarmupTemplateStats(id, days),
    });

export function useWarmupTemplateStats(id: string | null, days: number) {
    return useQuery({
        ...warmupTemplateStatsQuery(id ?? "", days),
        enabled: !!id,
        placeholderData: keepPreviousData,
    });
}

export const warmupTemplateQuery = (id: string) =>
    queryOptions({
        queryKey: [...WARMUP_COPY_KEY, "template", id],
        queryFn: () => getWarmupTemplate(id),
    });

export function useWarmupTemplate(id: string | null) {
    return useQuery({ ...warmupTemplateQuery(id ?? ""), enabled: !!id });
}

// Outside WARMUP_COPY_KEY so a teammate's audit event does not re-run a draft's analysis.
export function useWarmupTemplateCheck(draft: { subject: string; body: string } | null) {
    return useQuery({
        queryKey: ["warmup-template-check", draft?.subject ?? "", draft?.body ?? ""],
        queryFn: ({ signal }) => checkWarmupTemplate(draft as { subject: string; body: string }, signal),
        enabled: draft != null,
        placeholderData: keepPreviousData,
        retry: false,
        staleTime: Infinity,
    });
}

export function useCreateWarmupTemplate() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (body: WarmupTemplateInput) => createWarmupTemplate(body),
        onSuccess: () => qc.invalidateQueries({ queryKey: [...WARMUP_COPY_KEY, "templates"] }),
    });
}

const templatesKey = [...WARMUP_COPY_KEY, "templates"];
const deleteMutationKey = [...WARMUP_COPY_KEY, "templates", "delete"];

// Optimistic: the row leaves every cached window at once.
export function useDeleteWarmupTemplate() {
    const qc = useQueryClient();
    return useMutation({
        mutationKey: deleteMutationKey,
        mutationFn: (id: string) => deleteWarmupTemplate(id),
        onMutate: (id) =>
            patchQueries(qc, [templatesKey], (data) => {
                const list = data as WarmupTemplateList | undefined;
                if (!list?.templates?.some((t) => t.id === id)) return data;
                return { ...list, templates: list.templates.filter((t) => t.id !== id) };
            }),
        onError: (_err, _id, snapshot) => restoreQueries(qc, snapshot),
        onSettled: () => settle(qc, deleteMutationKey, [templatesKey]),
    });
}
