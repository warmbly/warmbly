import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    createOrgExport,
    createOrgImport,
    deleteOrgExport,
    getOrgTransferGroups,
    listOrgExports,
    listOrgImports,
    preflightOrgImport,
} from "@/lib/api/client/app/orgtransfer/orgTransfer";
import type {
    CreateOrgExportPayload,
    CreateOrgImportOptions,
    OrgExportJob,
    OrgImportJob,
} from "@/lib/api/models/app/orgtransfer/OrgTransfer";
import { useCurrentOrg } from "@/stores/useAppStore";

/** True while a job is still moving, so the list knows to keep polling. */
function isActive(job: { status: string }): boolean {
    return job.status === "queued" || job.status === "running";
}

// A running transfer has no realtime event of its own — the audit spine only
// fires when a job is created or finishes — so an in-flight job is polled for
// its progress bar. The interval drops to nothing the moment none are active.
function progressInterval<T extends { status: string }>(jobs: T[] | undefined): number | false {
    return jobs?.some(isActive) ? 2000 : false;
}

export function useOrgTransferGroups() {
    const org = useCurrentOrg();
    return useQuery({
        queryKey: ["organizations", "transfer-groups", org?.id ?? null],
        queryFn: getOrgTransferGroups,
        staleTime: 5 * 60_000,
        enabled: !!org,
    });
}

export function useOrgExports() {
    const org = useCurrentOrg();
    return useQuery({
        queryKey: ["organizations", "exports", org?.id ?? null],
        queryFn: listOrgExports,
        enabled: !!org,
        refetchInterval: (query) => progressInterval(query.state.data as OrgExportJob[] | undefined),
    });
}

export function useOrgImports() {
    const org = useCurrentOrg();
    return useQuery({
        queryKey: ["organizations", "imports", org?.id ?? null],
        queryFn: listOrgImports,
        enabled: !!org,
        refetchInterval: (query) => progressInterval(query.state.data as OrgImportJob[] | undefined),
    });
}

export function useCreateOrgExport() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: CreateOrgExportPayload) => createOrgExport(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["organizations", "exports"] });
        },
    });
}

export function useDeleteOrgExport() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteOrgExport(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["organizations", "exports"] });
        },
    });
}

export function usePreflightOrgImport() {
    return useMutation({
        mutationFn: ({ file, passphrase }: { file: File; passphrase: string }) =>
            preflightOrgImport(file, passphrase),
    });
}

export function useCreateOrgImport() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({
            file,
            options,
            passphrase,
        }: {
            file: File;
            options: CreateOrgImportOptions;
            passphrase: string;
        }) => createOrgImport(file, options, passphrase),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["organizations", "imports"] });
        },
    });
}
