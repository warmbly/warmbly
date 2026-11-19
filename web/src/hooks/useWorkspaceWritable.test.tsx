import { describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import useWorkspaceWritable, { workspaceWritable } from "./useWorkspaceWritable";
import { useWriteGuard } from "./usePermission";
import { currentOrganizationQuery } from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import type { PartnerManaged } from "@/lib/api/models/app/organizations/Organization";

const graphkit: PartnerManaged = { display_name: "GraphKit", state: "active" };

function withWorkspace(partner?: PartnerManaged) {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    client.setQueryData(currentOrganizationQuery.queryKey, { id: "org", name: "Acme", partner } as never);
    return ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe("workspaceWritable", () => {
    it("takes writes in an unmanaged or active managed workspace", () => {
        expect(workspaceWritable(null)).toEqual({ writable: true, reason: undefined });
        expect(workspaceWritable(graphkit).writable).toBe(true);
    });

    it("holds writes with the banner's wording while suspended or cancelled", () => {
        const suspended = workspaceWritable({ ...graphkit, state: "suspended", suspended_reason: "billing" });
        expect(suspended.writable).toBe(false);
        expect(suspended.reason).toBe(
            "Paused: billing through GraphKit. Sending and warmup are stopped and this workspace is read-only. It resumes as soon as GraphKit reactivates it.",
        );
        const cancelled = workspaceWritable({ ...graphkit, state: "cancelled", deletes_at: "2026-12-18T00:00:00Z" });
        expect(cancelled.writable).toBe(false);
        expect(cancelled.reason).toMatch(/^Paused by GraphKit\. .*unless GraphKit restores it\.$/);
    });
});

describe("useWorkspaceWritable", () => {
    it("reads the current workspace", () => {
        expect(renderHook(() => useWorkspaceWritable(), { wrapper: withWorkspace(graphkit) }).result.current.writable).toBe(true);
        const paused = renderHook(() => useWorkspaceWritable(), { wrapper: withWorkspace({ ...graphkit, state: "suspended" }) });
        expect(paused.result.current.writable).toBe(false);
    });

    it("locks every write guard while paused and explains why instead of acting", () => {
        const { result } = renderHook(() => useWriteGuard("MANAGE_CAMPAIGNS"), { wrapper: withWorkspace({ ...graphkit, state: "suspended" }) });
        expect(result.current.locked).toBe(true);
        expect(result.current.reason).toMatch(/^Paused by GraphKit\./);

        const denied = vi.fn();
        window.addEventListener("permission-denied", denied);
        const act = vi.fn();
        result.current.guard(act)({});
        window.removeEventListener("permission-denied", denied);
        expect(act).not.toHaveBeenCalled();
        expect((denied.mock.calls[0][0] as CustomEvent).detail).toMatchObject({ code: "organization_suspended", message: result.current.reason });
    });

    it("leaves write guards to permissions in an active workspace", () => {
        const { result } = renderHook(() => useWriteGuard("MANAGE_CAMPAIGNS"), { wrapper: withWorkspace(graphkit) });
        expect(result.current).toMatchObject({ allowed: true, locked: false, reason: undefined });
    });
});
