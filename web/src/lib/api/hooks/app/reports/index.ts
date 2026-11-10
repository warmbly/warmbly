import { useMemo } from "react";
import { infiniteQueryOptions, queryOptions, useInfiniteQuery, useMutation, useQuery, useQueryClient, type InfiniteData, type QueryClient } from "@tanstack/react-query";
import {
    createClientReport,
    createClientReportLink,
    deleteClientReport,
    deleteClientReportSchedule,
    getClientReport,
    listClientReports,
    previewClientReport,
    revokeClientReportLink,
    setClientReportSchedule,
    updateClientReport,
} from "@/lib/api/client/app/reports";
import useAllPages from "@/lib/api/hooks/useAllPages";
import { patchQueries, removeEntities, restoreQueries, settle, updateEntity } from "@/lib/api/hooks/optimistic";
import type ClientReport from "@/lib/api/models/app/reports/ClientReport";
import type { ClientReportInput, ClientReportPage, ClientReportRange, ClientReportScheduleInput } from "@/lib/api/models/app/reports/ClientReport";

// Every report read lives under ["reports"], the prefix the audit spine invalidates on a client_report write.
const reportsMutationKey = ["reports", "write"];
const LIST_KEY = ["reports", "list"];

export const reportsListQuery = infiniteQueryOptions<
    ClientReportPage,
    Error,
    InfiniteData<ClientReportPage, string | null>,
    string[],
    string | null
>({
    queryKey: LIST_KEY,
    queryFn: ({ pageParam, signal }) => listClientReports(pageParam ?? undefined, 100, signal),
    initialPageParam: null,
    getNextPageParam: (last) => (last.pagination.has_more ? (last.pagination.next_cursor ?? undefined) : undefined),
});

export function useReports(enabled = true) {
    const query = useInfiniteQuery({ ...reportsListQuery, enabled });
    const rest = useAllPages(query, enabled);
    const reports = useMemo(() => query.data?.pages.flatMap((p) => p.data ?? []) ?? [], [query.data]);
    return { ...query, reports, ...rest };
}

export const reportQuery = (id: string) =>
    queryOptions({ queryKey: ["reports", id], queryFn: ({ signal }) => getClientReport(id, signal) });

export function useReport(id: string | undefined) {
    return useQuery({ ...reportQuery(id ?? ""), enabled: !!id });
}

export const reportPreviewQuery = (id: string, range: ClientReportRange | undefined) =>
    queryOptions({
        queryKey: ["reports", id, "preview", range ?? "default"],
        queryFn: ({ signal }) => previewClientReport(id, range, signal),
    });

export function useReportPreview(id: string | undefined, range: ClientReportRange | undefined) {
    return useQuery({ ...reportPreviewQuery(id ?? "", range), enabled: !!id, placeholderData: (prev) => prev });
}

// Writes the server's copy into the detail and every list row, so a refetch is not needed to show it.
function storeReport(qc: QueryClient, report: ClientReport) {
    qc.setQueryData(["reports", report.id], report);
    void patchQueries(qc, [LIST_KEY], updateEntity<ClientReport>(report.id, () => report));
}

export function useCreateReport() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: ClientReportInput) => createClientReport(input),
        onSuccess: (report) => {
            qc.setQueryData(["reports", report.id], report);
            void qc.invalidateQueries({ queryKey: LIST_KEY });
        },
    });
}

// Rename, sections, range and scope are predictable, so the cache takes the patch on the click.
export function useUpdateReport() {
    const qc = useQueryClient();
    return useMutation({
        mutationKey: reportsMutationKey,
        mutationFn: ({ id, input }: { id: string; input: ClientReportInput }) => updateClientReport(id, input),
        onMutate: ({ id, input }) =>
            patchQueries(qc, [LIST_KEY, ["reports", id]], updateEntity<ClientReport>(id, (r) => ({ ...r, ...input }))),
        onError: (_e, _v, snapshot) => restoreQueries(qc, snapshot),
        onSettled: (_d, _e, { id }) => settle(qc, reportsMutationKey, [LIST_KEY, ["reports", id]]),
    });
}

export function useDeleteReport() {
    const qc = useQueryClient();
    return useMutation({
        mutationKey: reportsMutationKey,
        mutationFn: (id: string) => deleteClientReport(id),
        onMutate: (id) => patchQueries(qc, [LIST_KEY], removeEntities([id])),
        onError: (_e, _id, snapshot) => restoreQueries(qc, snapshot),
        onSuccess: (_d, id) => qc.removeQueries({ queryKey: ["reports", id], type: "inactive" }),
        onSettled: () => settle(qc, reportsMutationKey, [LIST_KEY]),
    });
}

// Minting is decided by the server (and replaces any earlier link), so it waits for the answer.
export function useCreateReportLink() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, expiresInDays }: { id: string; expiresInDays?: number }) => createClientReportLink(id, expiresInDays),
        onSuccess: (res) => storeReport(qc, res.report),
    });
}

export function useRevokeReportLink() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => revokeClientReportLink(id),
        onSuccess: (report) => storeReport(qc, report),
    });
}

export function useSetReportSchedule() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, input }: { id: string; input: ClientReportScheduleInput }) => setClientReportSchedule(id, input),
        onSuccess: (report) => storeReport(qc, report),
    });
}

// Pausing only flips one flag on a schedule that already exists, so it is optimistic.
export function usePauseReportSchedule() {
    const qc = useQueryClient();
    return useMutation({
        mutationKey: reportsMutationKey,
        mutationFn: ({ report, paused }: { report: ClientReport; paused: boolean }) => {
            const s = report.schedule;
            if (!s) throw new Error("This report has no schedule.");
            return setClientReportSchedule(report.id, {
                recipients: s.recipients,
                cadence: s.cadence,
                weekday: s.weekday,
                month_day: s.month_day,
                hour: s.hour,
                timezone: s.timezone,
                paused,
            });
        },
        onMutate: ({ report, paused }) =>
            patchQueries(
                qc,
                [LIST_KEY, ["reports", report.id]],
                updateEntity<ClientReport>(report.id, (r) => (r.schedule ? { ...r, schedule: { ...r.schedule, paused } } : r)),
            ),
        onError: (_e, _v, snapshot) => restoreQueries(qc, snapshot),
        onSuccess: (report) => storeReport(qc, report),
        onSettled: (_d, _e, { report }) => settle(qc, reportsMutationKey, [LIST_KEY, ["reports", report.id]]),
    });
}

export function useDeleteReportSchedule() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteClientReportSchedule(id),
        onSuccess: (report) => storeReport(qc, report),
    });
}
