import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    cancelToolMigration,
    createToolMigration,
    dismissToolMigration,
    getToolMigration,
    linkToolMigrationSenders,
    listToolMigrationProviders,
    listToolMigrations,
    startToolMigration,
} from "@/lib/api/client/app/toolmigrate/toolMigrations";
import {
    isToolMigrationActive,
    type NewToolMigration,
    type StartToolMigration,
    type ToolMigration,
    type ToolMigrationList,
    type ToolMigrationScope,
} from "@/lib/api/models/app/toolmigrate/ToolMigration";

// Every key sits under ["tool-migrations"], which the audit spine invalidates on create, start, finish and cancel.
export const TOOL_MIGRATIONS_KEY = ["tool-migrations"] as const;

const scopeKey = (scope: ToolMigrationScope) => (scope.kind === "agency" ? `agency:${scope.agencyId}` : "workspace");

// No realtime event carries progress, so a migration still scanning or running is polled.
const PROGRESS_MS = 3000;

export const toolMigrationProvidersQuery = (scope: ToolMigrationScope) =>
    queryOptions({
        queryKey: [...TOOL_MIGRATIONS_KEY, scopeKey(scope), "providers"],
        queryFn: ({ signal }) => listToolMigrationProviders(scope, signal),
        staleTime: 10 * 60_000,
    });

export const toolMigrationsQuery = (scope: ToolMigrationScope) =>
    queryOptions({
        queryKey: [...TOOL_MIGRATIONS_KEY, scopeKey(scope), "list"],
        queryFn: ({ signal }) => listToolMigrations(scope, undefined, signal),
        staleTime: 10_000,
        refetchInterval: (query) => (query.state.data?.data.some((m) => isToolMigrationActive(m.status)) ? PROGRESS_MS : false),
    });

export const toolMigrationQuery = (scope: ToolMigrationScope, id: string) =>
    queryOptions({
        queryKey: [...TOOL_MIGRATIONS_KEY, scopeKey(scope), "one", id],
        queryFn: ({ signal }) => getToolMigration(scope, id, signal),
        refetchInterval: (query) => (query.state.data && isToolMigrationActive(query.state.data.status) ? PROGRESS_MS : false),
    });

export function useToolMigrationProviders(scope: ToolMigrationScope, enabled = true) {
    return useQuery({ ...toolMigrationProvidersQuery(scope), enabled });
}

export function useToolMigrations(scope: ToolMigrationScope, enabled = true) {
    return useQuery({ ...toolMigrationsQuery(scope), enabled });
}

export function useToolMigration(scope: ToolMigrationScope, id: string | null) {
    return useQuery({ ...toolMigrationQuery(scope, id ?? ""), enabled: !!id });
}

const time = (d: Date | string) => new Date(d).getTime();

// A fresh answer replaces the row in the list and the single read at once.
function useStore(scope: ToolMigrationScope) {
    const qc = useQueryClient();
    return (m: ToolMigration) => {
        qc.setQueryData(toolMigrationQuery(scope, m.id).queryKey, m);
        qc.setQueryData<ToolMigrationList>(toolMigrationsQuery(scope).queryKey, (prev) =>
            prev ? { ...prev, data: [m, ...prev.data.filter((x) => x.id !== m.id)].sort((a, b) => time(b.created_at) - time(a.created_at)) } : prev,
        );
    };
}

export function useCreateToolMigration(scope: ToolMigrationScope) {
    const qc = useQueryClient();
    const store = useStore(scope);
    return useMutation({
        mutationFn: ({ data, idempotencyKey }: { data: NewToolMigration; idempotencyKey?: string }) => createToolMigration(scope, data, idempotencyKey),
        onSuccess: store,
        onSettled: () => qc.invalidateQueries({ queryKey: toolMigrationsQuery(scope).queryKey }),
    });
}

export function useStartToolMigration(scope: ToolMigrationScope) {
    const qc = useQueryClient();
    const store = useStore(scope);
    return useMutation({
        mutationFn: ({ id, data, idempotencyKey }: { id: string; data: StartToolMigration; idempotencyKey?: string }) =>
            startToolMigration(scope, id, data, idempotencyKey),
        onSuccess: (m) => {
            store(m);
            // New client workspaces may have been created.
            if (scope.kind === "agency") void qc.invalidateQueries({ queryKey: ["agency", scope.agencyId] });
        },
        onSettled: () => qc.invalidateQueries({ queryKey: toolMigrationsQuery(scope).queryKey }),
    });
}

export function useCancelToolMigration(scope: ToolMigrationScope) {
    const qc = useQueryClient();
    const store = useStore(scope);
    return useMutation({
        mutationFn: (id: string) => cancelToolMigration(scope, id),
        onSuccess: store,
        onSettled: () => qc.invalidateQueries({ queryKey: toolMigrationsQuery(scope).queryKey }),
    });
}

export function useLinkToolMigrationSenders(scope: ToolMigrationScope) {
    const store = useStore(scope);
    return useMutation({
        mutationFn: (id: string) => linkToolMigrationSenders(scope, id),
        onSuccess: store,
    });
}

// Removing a row is predictable, so it leaves the list at once and comes back if the server refuses.
export function useDismissToolMigration(scope: ToolMigrationScope) {
    const qc = useQueryClient();
    const key = toolMigrationsQuery(scope).queryKey;
    return useMutation({
        mutationFn: (id: string) => dismissToolMigration(scope, id),
        onMutate: async (id) => {
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData<ToolMigrationList>(key);
            if (prev) qc.setQueryData<ToolMigrationList>(key, { ...prev, data: prev.data.filter((m) => m.id !== id) });
            return { prev };
        },
        onError: (_e, _id, ctx) => {
            if (ctx?.prev) qc.setQueryData(key, ctx.prev);
        },
        onSuccess: (_d, id) => qc.removeQueries({ queryKey: toolMigrationQuery(scope, id).queryKey }),
        onSettled: () => qc.invalidateQueries({ queryKey: key }),
    });
}
