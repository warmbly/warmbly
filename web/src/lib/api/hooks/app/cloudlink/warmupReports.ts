import type { QueryClient } from "@tanstack/react-query";

export const CLOUD_LINK_KEY = ["cloud-link"];

// Capture the scope before the write, since the user can switch workspaces while it is pending.
export function cloudLinkScope(client: QueryClient): readonly unknown[] {
    const organization = client.getQueryData<{ id: string }>(["organizations", "current"]);
    return organization?.id ? [...CLOUD_LINK_KEY, organization.id] : CLOUD_LINK_KEY;
}

export function cloudLinkScopeCurrent(client: QueryClient, scope: readonly unknown[]): boolean {
    return scope[1] === cloudLinkScope(client)[1];
}

export async function invalidateWarmupReports(client: QueryClient, scope: readonly unknown[]) {
    await Promise.all([
        client.invalidateQueries({ queryKey: scope }),
        cloudLinkScopeCurrent(client, scope) && client.invalidateQueries({ queryKey: ["analytics", "accounts"] }),
    ]);
}
