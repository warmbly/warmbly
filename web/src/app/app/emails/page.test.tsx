import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type Tag from "@/lib/api/models/app/Tag";
import type * as UserModule from "@/hooks/context/user";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import AddressesPage from "./page";

const { tags, rows, statusState } = vi.hoisted(() => ({
    statusState: { data: [] as AccountStatus[], isLoading: false, isFetching: false, isError: false, cloudLoading: false, cloudUnavailable: false },
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
vi.mock("@/lib/api/hooks/app/analytics/useAccountStatuses", () => ({ default: () => statusState }));
vi.mock("@/lib/api/hooks/app/subscription/useFeatureStatus", () => ({ default: () => ({ data: { can_use_warmup: true } }) }));
vi.mock("@/lib/api/hooks/auth/useAuthConfig", () => ({ default: () => ({ data: {}, isLoading: false }) }));
vi.mock("@/lib/api/hooks/app/emails/useMailboxGrants", () => ({ useSigninMigration: () => ({ data: { data: [] } }) }));
vi.mock("@/lib/api/hooks/app/advisor/useAdvisor", () => ({ useAdvisorEntityIndex: () => ({ get: () => [] }) }));
vi.mock("@/hooks/useCloudPool", () => ({ default: () => ({ selfHosted: false, connected: false, loading: statusState.cloudLoading, unavailable: statusState.cloudUnavailable, observedAt: 0 }) }));
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

function measuredStatus(row: Inbox): AccountStatus {
    return { id: row.id, email: row.email, provider: "gmail", status: "active", last_synced_at: null, health: { status: "healthy", score: 100 }, errors: [], daily_usage: { date: "2026-10-09", campaign_sent: 0, campaign_limit: 50 }, in_campaign: false };
}

describe("mailbox ordering with a tag filter", () => {
    beforeEach(() => {
        sessionStorage.clear();
        statusState.data = [];
        statusState.isLoading = false;
        statusState.isFetching = false;
        statusState.isError = false;
        statusState.cloudLoading = false;
        statusState.cloudUnavailable = false;
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

describe("mailbox status while account and Cloud checks settle", () => {
    beforeEach(() => {
        sessionStorage.clear();
        vi.spyOn(window, "scrollTo").mockImplementation(() => {});
        statusState.data = [];
        statusState.isLoading = false;
        statusState.isFetching = false;
        statusState.isError = false;
        statusState.cloudLoading = false;
        statusState.cloudUnavailable = false;
    });
    afterEach(() => { cleanup(); vi.restoreAllMocks(); });

    it("does not show Idle or Healthy while Cloud and account status are loading, then shows the real error", async () => {
        statusState.cloudLoading = true;
        statusState.data = rows.map(measuredStatus);
        await renderPage();
        expect(screen.getAllByText("Checking…").length).toBeGreaterThan(4);
        expect(screen.queryByText("Idle")).not.toBeInTheDocument();
        expect(screen.queryByText("Healthy 100")).not.toBeInTheDocument();
        expect(screen.queryByTitle("0 of 50 campaign emails sent today")).not.toBeInTheDocument();

        statusState.cloudLoading = false;
        statusState.data[0] = { ...measuredStatus(rows[0]), health: { status: "error", score: 40 }, errors: [{ id: "error-1", error_code: "PROVIDER_UNAVAILABLE", severity: "critical", title: "Provider unavailable", message: "Provider refused the last attempt", created_at: new Date("2026-10-09T00:00:00Z") }] };
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        const sara = screen.getAllByRole("row").find((row) => row.textContent?.includes(rows[0].email));
        expect(sara).toBeDefined();
        expect(within(sara!).getByText("Issue 40")).toBeInTheDocument();
        expect(within(sara!).getAllByText("Error").length).toBeGreaterThan(0);
    });

    it("leaves partial, stale and failed status checks unknown instead of counting them healthy", async () => {
        statusState.data = [measuredStatus(rows[0])];
        statusState.isFetching = true;
        await renderPage();
        expect(screen.queryByText("Healthy 100")).not.toBeInTheDocument();
        statusState.isFetching = false;
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        expect(screen.getAllByText("Unavailable").length).toBeGreaterThan(0);
        statusState.isError = true;
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        expect(screen.queryByText("Healthy 100")).not.toBeInTheDocument();
    });
});
