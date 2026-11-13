import Request from "../../Request";
import type {
    NewToolMigration,
    StartToolMigration,
    ToolMigration,
    ToolMigrationList,
    ToolMigrationScope,
    ToolProviderList,
} from "@/lib/api/models/app/toolmigrate/ToolMigration";

const idem = () => ({ "Idempotency-Key": crypto.randomUUID() });

// A workspace's migrations live under /migrations, an agency's under /agency/:id/migrations.
const base = (scope: ToolMigrationScope) =>
    scope.kind === "agency" ? `/agency/${encodeURIComponent(scope.agencyId)}/migrations` : "/migrations";

export const listToolMigrationProviders = (scope: ToolMigrationScope, signal?: AbortSignal) =>
    Request<ToolProviderList>({ method: "GET", url: `${base(scope)}/providers`, authorization: true, signal });

export const listToolMigrations = (scope: ToolMigrationScope, cursor?: string, signal?: AbortSignal) =>
    Request<ToolMigrationList>({
        method: "GET",
        url: base(scope),
        params: { cursor: cursor || undefined },
        authorization: true,
        signal,
    });

export const getToolMigration = (scope: ToolMigrationScope, id: string, signal?: AbortSignal) =>
    Request<ToolMigration>({ method: "GET", url: `${base(scope)}/${encodeURIComponent(id)}`, authorization: true, signal });

/** The key names one submission, so a retry after a lost answer does not scan twice. */
export const createToolMigration = (scope: ToolMigrationScope, data: NewToolMigration, idempotencyKey?: string) =>
    Request<ToolMigration>({
        method: "POST",
        url: base(scope),
        data,
        headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : idem(),
        authorization: true,
    });

export const startToolMigration = (scope: ToolMigrationScope, id: string, data: StartToolMigration, idempotencyKey?: string) =>
    Request<ToolMigration>({
        method: "POST",
        url: `${base(scope)}/${encodeURIComponent(id)}/start`,
        data,
        headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : idem(),
        authorization: true,
    });

export const cancelToolMigration = (scope: ToolMigrationScope, id: string) =>
    Request<ToolMigration>({ method: "POST", url: `${base(scope)}/${encodeURIComponent(id)}/cancel`, authorization: true });

export const linkToolMigrationSenders = (scope: ToolMigrationScope, id: string) =>
    Request<ToolMigration>({ method: "POST", url: `${base(scope)}/${encodeURIComponent(id)}/link-senders`, authorization: true });

export const dismissToolMigration = (scope: ToolMigrationScope, id: string) =>
    Request<void>({ method: "DELETE", url: `${base(scope)}/${encodeURIComponent(id)}`, authorization: true });
