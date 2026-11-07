import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { LoginCodeExemption } from "@/lib/api/models/admin";
import { ConfirmProvider } from "@/components/ConfirmDialog";
import TestersPage from "./TestersPage";

const access = vi.hoisted(() => ({ canManage: true }));
vi.mock("@/hooks/useAdminPerm", () => ({
    useAdminPerm: () => access.canManage,
}));

function render(tester: Partial<LoginCodeExemption>) {
    const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
    client.setQueryData(["admin", "testers"], {
        data: [{ user_id: "reviewer", email: "reviewer@example.test", password_expires_at: "2100-01-01T00:00:00Z", ...tester }],
    });
    const html = renderToStaticMarkup(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/testers"]}><ConfirmProvider><TestersPage /></ConfirmProvider></MemoryRouter></QueryClientProvider>);
    client.clear();
    const document = new DOMParser().parseFromString(html, "text/html");
    return document;
}

describe("tester password re-issue", () => {
    beforeEach(() => { access.canManage = true; });

    it("offers a new password only on accounts created as testers, to managers", () => {
        const selector = 'button[aria-label^="Issue new password for"]';
        expect(render({ created_as_tester: true }).querySelector(selector)).not.toBeNull();
        expect(render({ created_as_tester: false }).querySelector(selector)).toBeNull();
        access.canManage = false;
        expect(render({ created_as_tester: true }).querySelector(selector)).toBeNull();
    });
});

describe("tester sample-data controls", () => {
    beforeEach(() => { access.canManage = true; });

    it("offers sample data for an active owned Test workspace", () => {
        const document = render({ test_workspace_id: "test-workspace" });
        const button = document.querySelector<HTMLButtonElement>('button[aria-label="Add sample data for reviewer@example.test"]');
        expect(button?.disabled).toBe(false);
        expect(button?.textContent).toBe("Add sample data");
        expect(document.body.textContent).toContain("Mailboxes must be connected separately");
    });

    it("does not offer sample data to attached customer-workspace testers or read-only admins", () => {
        expect(render({}).querySelector('button[aria-label^="Add sample data for"]')).toBeNull();
        access.canManage = false;
        expect(render({ test_workspace_id: "test-workspace" }).querySelector('button[aria-label^="Add sample data for"]')).toBeNull();
    });

    it("disables populated, expired and passwordless Test workspaces", () => {
        for (const tester of [
            { sample_data_seeded_at: "2026-01-01T00:00:00Z" },
            { password_expires_at: "2020-01-01T00:00:00Z" },
            { password_expires_at: null },
        ]) {
            const document = render({ test_workspace_id: "test-workspace", ...tester });
            const button = document.querySelector<HTMLButtonElement>('button[aria-label^="Add sample data for"]');
            expect(button?.disabled).toBe(true);
            if (tester.sample_data_seeded_at) expect(document.body.textContent).toContain("Sample data added Jan 1, 2026");
        }
    });
});
