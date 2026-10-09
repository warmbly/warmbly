import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type Tag from "@/lib/api/models/app/Tag";
import type * as UserModule from "@/hooks/context/user";
import AddressesPage from "./page";

const { tags, rows } = vi.hoisted(() => ({
    tags: [
        { id: "sending", title: "Sending account", color: "#0088cc", position: 0 },
        { id: "serveblink", title: "Serveblink.com", color: "#008800", position: 1 },
        { id: "virevow", title: "Virevow.com", color: "#8800cc", position: 2 },
    ] as Tag[],
    rows: [
        { id: "sara", email: "sara@virevow.example", tags: ["virevow", "sending"] },
        { id: "kavya", email: "kavya@serveblink.example", tags: ["serveblink", "sending"] },
        { id: "ananya", email: "ananya@serveblink.example", tags: ["sending", "serveblink"] },
        { id: "julie", email: "julie@serveblink.example", tags: ["serveblink", "sending"] },
    ].map((row) => ({ ...row, name: row.id, status: "active", provider: "gmail", campaign_limit: 50 })) as Inbox[],
}));

vi.mock("@/hooks/context/user", async (original) => ({
    ...await original<typeof UserModule>(),
    useUserProfile: () => ({ user: { id: "tag-order-user", tags } }),
}));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => ({ show: vi.fn() }) }));
vi.mock("@/hooks/usePermission", () => ({ usePermission: () => true }));
vi.mock("@/lib/api/hooks/app/emails/useEmails", () => ({
    default: ({ tag }: { tag: string }) => ({ emails: tag ? rows.filter((row) => row.tags.includes(tag)) : rows }),
}));
vi.mock("@/lib/api/hooks/app/analytics/useAccountStatuses", () => ({ default: () => ({ data: [] }) }));
vi.mock("@/lib/api/hooks/app/subscription/useFeatureStatus", () => ({ default: () => ({ data: { can_use_warmup: true } }) }));
vi.mock("@/lib/api/hooks/auth/useAuthConfig", () => ({ default: () => ({ data: {}, isLoading: false }) }));
vi.mock("@/lib/api/hooks/app/emails/useMailboxGrants", () => ({ useSigninMigration: () => ({ data: { data: [] } }) }));
vi.mock("@/lib/api/hooks/app/advisor/useAdvisor", () => ({ useAdvisorEntityIndex: () => ({ get: () => [] }) }));
vi.mock("@/hooks/useCloudPool", () => ({ default: () => ({ selfHosted: false, connected: false }) }));
vi.mock("@/lib/api/hooks/app/cloudlink/useCloudLink", () => ({
    useEnrollCloudLinkMailbox: () => ({}), useUnenrollCloudLinkMailbox: () => ({}), useCloudLinkMailboxLifecycle: () => ({}),
}));
vi.mock("@/lib/api/hooks/app/emails/useWarmupLifecycle", () => ({ default: () => ({}) }));
vi.mock("@/lib/api/hooks/app/emails/useRemoveEmail", () => ({ default: () => ({}) }));
vi.mock("@/components/app/emails/useMailboxSwitch", () => ({ default: () => ({}), switchOffPrompt: vi.fn() }));
vi.mock("@/components/app/emails/InboxDetails", () => ({ default: () => null }));
vi.mock("@/components/app/emails/BulkWarmupDialog", () => ({ default: () => null }));
vi.mock("@/components/app/emails/migration/SigninMigrationDialog", () => ({ default: () => null }));
vi.mock("@/components/app/emails/import/grants/MailboxGrantDialog", () => ({ default: () => null }));
vi.mock("@/components/app/cloud/CloudConnectDialog", () => ({ default: () => null }));
vi.mock("@/components/app/emails/import/MailboxImportsMenu", () => ({ default: () => null }));
vi.mock("@/components/app/emails/CloudPathsPanel", () => ({ default: () => null }));
vi.mock("@/components/app/advisor/AdvisorRowFlag", () => ({ default: () => null }));
vi.mock("@/components/app/advisor/AdvisorSummaryBar", () => ({ default: () => null }));

async function renderPage(tag = "") {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({
        routeTree: createRootRoute({ component: AddressesPage }),
        history: createMemoryHistory({ initialEntries: [`/${tag ? `?tag=${tag}` : ""}`] }),
    });
    await router.load();
    return render(
        <QueryClientProvider client={client}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
}

function orderedEmails() {
    return screen.getAllByRole("row").slice(1).map((row) => within(row).getByText(/@/).textContent);
}

describe("mailbox ordering with a tag filter", () => {
    beforeEach(() => {
        sessionStorage.clear();
        vi.spyOn(window, "scrollTo").mockImplementation(() => {});
    });
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("keeps the existing All Accounts tag-set grouping and email order", async () => {
        await renderPage();
        expect(orderedEmails()).toEqual(["ananya@serveblink.example", "julie@serveblink.example", "kavya@serveblink.example", "sara@virevow.example"]);
    });

    it("keeps that order when a specific tag returns rows in creation order", async () => {
        await renderPage("serveblink");
        expect(orderedEmails()).toEqual(["ananya@serveblink.example", "julie@serveblink.example", "kavya@serveblink.example"]);
        expect(rows.map((row) => row.id)).toEqual(["sara", "kavya", "ananya", "julie"]);
    });

    it("honors explicit ascending and descending column sorts with a tag selected", async () => {
        await renderPage("serveblink");
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        expect(orderedEmails()).toEqual(["ananya@serveblink.example", "julie@serveblink.example", "kavya@serveblink.example"]);
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        expect(orderedEmails()).toEqual(["kavya@serveblink.example", "julie@serveblink.example", "ananya@serveblink.example"]);
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        expect(orderedEmails()).toEqual(["ananya@serveblink.example", "julie@serveblink.example", "kavya@serveblink.example"]);
    });

    it("preserves grouped order through All Accounts, a tag, and back to All Accounts", async () => {
        await renderPage();
        const all = orderedEmails();
        fireEvent.click(screen.getByRole("button", { name: "All accounts" }));
        fireEvent.click(screen.getByRole("menuitem", { name: "Serveblink.com" }));
        await waitFor(() => expect(orderedEmails()).toEqual(all.filter((email) => email?.endsWith("@serveblink.example"))));
        fireEvent.click(screen.getByRole("button", { name: "Serveblink.com" }));
        fireEvent.click(screen.getByRole("menuitem", { name: "All accounts" }));
        await waitFor(() => expect(orderedEmails()).toEqual(all));
    });

    it("retains an explicit column sort when changing tags", async () => {
        await renderPage();
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        const all = orderedEmails();
        fireEvent.click(screen.getByRole("button", { name: "All accounts" }));
        fireEvent.click(screen.getByRole("menuitem", { name: "Serveblink.com" }));
        await waitFor(() => expect(orderedEmails()).toEqual(all.filter((email) => email?.endsWith("@serveblink.example"))));
        expect(screen.getByRole("columnheader", { name: "Mailbox" })).toHaveAttribute("aria-sort", "descending");
    });
});
