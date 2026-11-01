import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type Tag from "@/lib/api/models/app/Tag";
import type * as UserModule from "@/hooks/context/user";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import type { CloudLinkMailboxRow } from "@/lib/api/models/app/cloudlink/CloudLink";
import AddressesPage from "./page";

const { tags, rows, statusState, confirm } = vi.hoisted(() => ({
    confirm: vi.fn(),
    statusState: { data: [] as AccountStatus[], observations: [] as { status: AccountStatus; observedAt: number; current: boolean }[], isLoading: false, isFetching: false, isError: false, cloudLoading: false, cloudUnavailable: false, cloudConnected: false, cloudRows: [] as CloudLinkMailboxRow[], emailsLoading: false, emailsError: false, emailsIncomplete: false },
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
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => ({ show: confirm }) }));
vi.mock("@/hooks/usePermission", () => ({ usePermission: () => true }));
vi.mock("@/lib/api/hooks/app/emails/useEmails", () => ({
    default: ({ tag }: { tag: string }) => ({ emails: statusState.emailsLoading || statusState.emailsError ? [] : tag ? rows.filter((row) => row.tags.includes(tag)) : rows, isLoading: statusState.emailsLoading, isPending: statusState.emailsLoading, isError: statusState.emailsError || statusState.emailsIncomplete, isIncomplete: statusState.emailsIncomplete, refetch: vi.fn() }),
}));
vi.mock("@/lib/api/hooks/app/analytics/useAccountStatuses", () => ({ default: () => statusState }));
vi.mock("@/lib/api/hooks/app/subscription/useFeatureStatus", () => ({ default: () => ({ data: { can_use_warmup: true } }) }));
vi.mock("@/lib/api/hooks/auth/useAuthConfig", () => ({ default: () => ({ data: {}, isLoading: false }) }));
vi.mock("@/lib/api/hooks/app/emails/useMailboxGrants", () => ({ useSigninMigration: () => ({ data: { data: [] } }) }));
vi.mock("@/lib/api/hooks/app/advisor/useAdvisor", () => ({ useAdvisorEntityIndex: () => ({ get: () => [] }) }));
vi.mock("@/hooks/useCloudPool", () => ({ default: () => ({ selfHosted: statusState.cloudConnected, connected: statusState.cloudConnected, loading: statusState.cloudLoading, unavailable: statusState.cloudUnavailable, observedAt: 0, rowFor: (id: string) => statusState.cloudRows.find((row) => row.id === id) }) }));
vi.mock("@/lib/api/hooks/app/cloudlink/useCloudLink", () => ({
    useEnrollCloudLinkMailbox: () => ({}), useUnenrollCloudLinkMailbox: () => ({}), useCloudLinkMailboxLifecycle: () => ({}),
}));
vi.mock("@/lib/api/hooks/app/emails/useWarmupLifecycle", () => ({ default: () => ({}) }));
const lifecycle = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client/app/emails/warmupLifecycle", () => ({ default: lifecycle }));
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

async function renderPage(tag = "", client = new QueryClient({ defaultOptions: { queries: { retry: false } } }), observe?: () => void) {
    function PageWithReports() {
        useQuery({ queryKey: ["cloud-link", "workspace", "mailboxes"], queryFn: async () => { observe?.(); return statusState.cloudRows; }, enabled: !!observe, staleTime: Infinity });
        return <AddressesPage />;
    }
    const router = createRouter({
        routeTree: createRootRoute({ component: PageWithReports }),
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
        confirm.mockReset();
        sessionStorage.clear();
        statusState.data = [];
        statusState.observations = [];
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

    it.each(["cloudLoading", "cloudUnavailable"] as const)("defers disconnect and bulk removal while ownership is %s", async (unknown) => {
        statusState[unknown] = true;
        await renderPage();
        const row = screen.getAllByRole("row")[1];
        fireEvent.click(within(row).getByRole("checkbox"));
        expect(screen.getByRole("button", { name: "Remove" })).toBeDisabled();
        fireEvent.click(within(row).getByRole("button", { name: "Mailbox actions" }));
        const disconnect = screen.getByRole("menuitem", { name: "Disconnect mailbox" });
        expect(disconnect).toBeDisabled();
        fireEvent.click(disconnect);
        expect(confirm).not.toHaveBeenCalled();
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

describe("Cloud warmup values", () => {
    beforeEach(() => { sessionStorage.clear(); statusState.isLoading = statusState.isFetching = statusState.isError = statusState.cloudLoading = statusState.cloudUnavailable = false; });
    afterEach(() => { cleanup(); statusState.cloudConnected = false; statusState.cloudRows = []; });

    it("sorts the Warmup column by the displayed Cloud counts without local warmup anchors", async () => {
        statusState.data = rows.map(measuredStatus);
        statusState.cloudConnected = true;
        statusState.cloudRows = rows.map((row, i) => ({ id: row.id, enrolled: true, cloud: { sent_today: [1, 9, 4, 2][i], health: { state: "healthy" }, warmup: { enabled: true, paused: false, target_volume: 10 } } } as CloudLinkMailboxRow));
        await renderPage();
        fireEvent.click(screen.getByTitle("Sort by Warmup"));
        expect(orderedEmails()).toEqual(["kavya@serveblink.example", "ananya@serveblink.example", "julie@serveblink.example", "sara@virevow.example"]);
    });

    it("keeps filtered health totals separate from the workspace Cloud inventory", async () => {
        statusState.data = rows.map(measuredStatus);
        statusState.cloudConnected = true;
        statusState.cloudRows = rows.map((row) => ({ id: row.id, enrolled: true, cloud: { sent_today: 5, health: { state: "healthy" }, warmup: { enabled: true, paused: false, target_volume: 10 } } } as CloudLinkMailboxRow));
        await renderPage("serveblink");
        const warming = screen.getAllByText("Warming", { selector: "span" })[0].parentElement?.parentElement;
        expect(warming).toHaveTextContent("3");
        expect(orderedEmails()).toHaveLength(3);
    });

    it("isolates a missing Cloud health report to its mailbox while keeping summary counts unavailable", async () => {
        statusState.data = rows.map(measuredStatus);
        statusState.cloudConnected = true;
        statusState.cloudRows = [
            { id: rows[0].id, enrolled: true, cloud: { sent_today: 3, settings: { base: 10 }, warmup: { enabled: true, target_volume: 10 } } },
            { id: rows[1].id, enrolled: true, cloud: { sent_today: 7, settings: { base: 10 }, health: { state: "healthy" }, warmup: { enabled: true, target_volume: 10 } } },
        ] as CloudLinkMailboxRow[];
        await renderPage();

        const rowFor = (email: string) => screen.getAllByRole("row").find((row) => row.textContent?.includes(email))!;
        expect(within(rowFor(rows[0].email)).getAllByText("Unavailable").length).toBeGreaterThan(0);
        expect(within(rowFor(rows[0].email)).queryByText("Healthy 100")).not.toBeInTheDocument();
        expect(within(rowFor(rows[1].email)).getByText("Healthy 100")).toBeInTheDocument();
        expect(within(rowFor(rows[1].email)).getAllByTitle(/7 of 10 warmup emails sent today/).length).toBeGreaterThan(0);
        expect(within(rowFor(rows[2].email)).getByText("Healthy 100")).toBeInTheDocument();
        const healthy = screen.getAllByText("Healthy", { selector: "span" })[0].parentElement?.parentElement;
        expect(healthy).toHaveTextContent("Unavailable");
    });

    it.each([false, true])("uses Resume for a Cloud warmup that is off, and disables lifecycle actions without a report (%s)", async (missing) => {
        statusState.data = rows.map(measuredStatus);
        statusState.cloudConnected = true;
        statusState.cloudRows = rows.map((row) => ({ id: row.id, enrolled: true, cloud: missing ? null : { sent_today: 0, health: { state: "healthy" }, settings: { base: 10 }, warmup: null } } as CloudLinkMailboxRow));
        await renderPage();
        fireEvent.click(within(screen.getAllByRole("row")[1]).getByRole("button", { name: "Warmup actions" }));
        if (missing) expect(screen.getByRole("menuitem", { name: "Cloud warmup status unavailable" })).toBeDisabled();
        else expect(screen.getByRole("menuitem", { name: "Resume in Warmbly Cloud" })).toBeEnabled();
    });

    it.each([false, true])("keeps the Cloud action available for a locally disabled instance executor (paused: %s)", async (paused) => {
        const originalStatus = rows[0].status;
        rows[0].status = "inactive";
        try {
            statusState.data = rows.map(measuredStatus);
            statusState.cloudConnected = true;
            statusState.cloudRows = [{ id: rows[0].id, enrolled: true, executor: "instance", cloud: {
                sent_today: 5, health: { state: "healthy" }, settings: { base: 10 }, warmup: { enabled: true, paused },
            } } as CloudLinkMailboxRow];
            await renderPage();
            const row = screen.getAllByRole("row").find((element) => element.textContent?.includes(rows[0].email))!;
            fireEvent.click(within(row).getByRole("button", { name: "Warmup actions" }));
            expect(screen.getByRole("menuitem", { name: paused ? "Resume in Warmbly Cloud" : "Pause in Warmbly Cloud" })).toBeEnabled();
        } finally {
            rows[0].status = originalStatus;
        }
    });

    it("refreshes Cloud reports after a partially successful bulk pause without a socket", async () => {
        statusState.data = rows.map(measuredStatus);
        statusState.cloudConnected = true;
        statusState.cloudRows = rows.map((row) => ({ id: row.id, enrolled: true, cloud: { sent_today: 0, health: { state: "healthy" }, settings: { base: 10 }, warmup: { enabled: true, paused: false } } } as CloudLinkMailboxRow));
        lifecycle.mockImplementation(async (id: string) => {
            if (id === "sara") throw { code: "cloud_unavailable" };
            return { id };
        });
        const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
        client.setQueryData(["organizations", "current"], { id: "workspace" });
        const observed = vi.fn();
        await renderPage("", client, observed);
        await waitFor(() => expect(observed).toHaveBeenCalledOnce());
        fireEvent.click(within(screen.getAllByRole("row")[0]).getByRole("checkbox"));
        fireEvent.click(screen.getByRole("button", { name: "Pause warmup" }));
        await waitFor(() => expect(lifecycle).toHaveBeenCalledTimes(4));
        await waitFor(() => expect(observed).toHaveBeenCalledTimes(2));
    });
});

describe("mailbox status while account and Cloud checks settle", () => {
    beforeEach(() => {
        sessionStorage.clear();
        vi.spyOn(window, "scrollTo").mockImplementation(() => {});
        statusState.data = [];
        statusState.observations = [];
        statusState.isLoading = false;
        statusState.isFetching = false;
        statusState.isError = false;
        statusState.cloudLoading = false;
        statusState.cloudUnavailable = false;
        statusState.emailsLoading = false;
        statusState.emailsError = false;
        statusState.emailsIncomplete = false;
    });
    afterEach(() => { cleanup(); vi.restoreAllMocks(); });

    it("keeps summary measurements pending until the mailbox inventory loads", async () => {
        statusState.emailsLoading = true;
        await renderPage();
        expect(screen.getAllByText("Checking…").length).toBeGreaterThanOrEqual(4);
        expect(screen.queryByText("Healthy 100")).not.toBeInTheDocument();
    });

    it("shows failed or partial mailbox inventory as unavailable rather than measured zeros", async () => {
        statusState.emailsError = true;
        await renderPage();
        expect(screen.getAllByText("Unavailable").length).toBeGreaterThanOrEqual(4);
        expect(screen.getAllByText("Mailboxes unavailable").length).toBeGreaterThan(0);
        expect(screen.queryByText("No email accounts yet")).not.toBeInTheDocument();
        expect(screen.queryByText("0 mailboxes")).not.toBeInTheDocument();
        cleanup();

        statusState.emailsError = false;
        statusState.emailsIncomplete = true;
        statusState.data = rows.map(measuredStatus);
        await renderPage();
        expect(screen.getAllByText("Unavailable").length).toBeGreaterThanOrEqual(4);
    });

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

    it("does not assume local-only health when the Cloud connection lookup failed", async () => {
        statusState.cloudUnavailable = true;
        statusState.data = rows.map(measuredStatus);
        statusState.data[0] = { ...measuredStatus(rows[0]), health: { status: "error", score: 40 }, errors: [{ id: "error-1", error_code: "PROVIDER_UNAVAILABLE", severity: "critical", title: "Provider unavailable", message: "Provider refused the last attempt", created_at: new Date("2026-10-09T00:00:00Z") }] };
        await renderPage();
        expect(screen.queryByText("Healthy 100")).not.toBeInTheDocument();
        const sara = screen.getAllByRole("row").find((row) => row.textContent?.includes(rows[0].email));
        expect(within(sara!).getByText("Issue 40")).toBeInTheDocument();
        expect(screen.getAllByText("Unavailable").length).toBeGreaterThan(0);
    });

    it("retains a recently observed error as historical evidence while refreshing, then clears it on a fresh result", async () => {
        statusState.data = rows.map(measuredStatus);
        statusState.data[0] = { ...measuredStatus(rows[0]), health: { status: "error", score: 40 }, errors: [{ id: "error-1", error_code: "PROVIDER_UNAVAILABLE", severity: "critical", title: "Provider unavailable", message: "Provider refused the last attempt", created_at: new Date("2026-10-09T00:00:00Z") }] };
        statusState.observations = statusState.data.map((status) => ({ status, observedAt: Date.now(), current: true }));
        await renderPage();
        expect(screen.getByText("Issue 40")).toBeInTheDocument();
        statusState.isFetching = true;
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        expect(screen.getAllByText("Last reported issue").length).toBeGreaterThan(0);
        expect(screen.getByText("Last observed: Issue 40")).toBeInTheDocument();
        expect(screen.queryByTitle("0 of 50 campaign emails sent today")).not.toBeInTheDocument();

        statusState.isFetching = false;
        statusState.cloudLoading = true;
        statusState.data = [];
        statusState.observations = [];
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        expect(screen.getAllByText("Last reported issue").length).toBeGreaterThan(0);

        statusState.data = rows.map(measuredStatus);
        statusState.observations = statusState.data.map((status) => ({ status, observedAt: Date.now(), current: true }));
        statusState.cloudLoading = false;
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        await waitFor(() => expect(screen.queryAllByText("Last reported issue")).toHaveLength(0));
        expect(screen.getAllByText("Healthy 100").length).toBeGreaterThan(0);
    });

    it("expires an old unverified issue rather than displaying it indefinitely", async () => {
        statusState.data = rows.map(measuredStatus);
        statusState.data[0] = { ...measuredStatus(rows[0]), health: { status: "error", score: 40 }, errors: [{ id: "error-1", error_code: "PROVIDER_UNAVAILABLE", severity: "critical", title: "Provider unavailable", message: "Provider refused the last attempt", created_at: new Date("2026-10-09T00:00:00Z") }] };
        const observedAt = Date.now();
        statusState.observations = statusState.data.map((status) => ({ status, observedAt, current: true }));
        await renderPage();
        statusState.isFetching = true;
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        expect(screen.getAllByText("Last reported issue").length).toBeGreaterThan(0);
        const clock = vi.spyOn(Date, "now").mockReturnValue(observedAt + 5 * 60_000 + 1);
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        expect(screen.queryAllByText("Last reported issue")).toHaveLength(0);
        clock.mockRestore();
    });

    it("does not resurrect a recovered mailbox's old issue when another mailbox is missing", async () => {
        statusState.data = rows.map(measuredStatus);
        statusState.data[0] = { ...measuredStatus(rows[0]), health: { status: "error", score: 40 }, errors: [{ id: "error-1", error_code: "PROVIDER_UNAVAILABLE", severity: "critical", title: "Provider unavailable", message: "Provider refused the last attempt", created_at: new Date("2026-10-09T00:00:00Z") }] };
        statusState.observations = statusState.data.map((status) => ({ status, observedAt: Date.now(), current: true }));
        await renderPage();
        statusState.data = [measuredStatus(rows[0])];
        statusState.observations = statusState.data.map((status) => ({ status, observedAt: Date.now(), current: true }));
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        await waitFor(() => expect(screen.getByText("Healthy 100")).toBeInTheDocument());
        statusState.isFetching = true;
        statusState.observations = statusState.observations.map((observation) => ({ ...observation, current: false }));
        fireEvent.click(screen.getByRole("button", { name: "Mailbox" }));
        expect(screen.queryAllByText("Last reported issue")).toHaveLength(0);
        expect(screen.queryByText("Last observed: Issue 40")).not.toBeInTheDocument();
    });

    it("counts a mailbox slowed by its warmup band as warming, not as needing attention", async () => {
        const original = { ...rows[0] };
        Object.assign(rows[0], { warmup: new Date("2026-09-24T00:00:00Z"), warmup_paused_at: null, test_mode: null });
        try {
            const band = { state: "watch", score: 0, spam_score: 0, reason: "13% landed in spam", partner_mailboxes_7d: 0, partner_domains_7d: 0, partner_organizations_7d: 0, received_7d: 0, senders_7d: 0 } as const;
            statusState.data = rows.map(measuredStatus);
            statusState.data[0] = { ...measuredStatus(rows[0]), health: { status: "warning", score: 87, issues: ["Warmup reputation needs watching"] }, warmup_health: band };
            statusState.observations = statusState.data.map((status) => ({ status, observedAt: Date.now(), current: true }));
            await renderPage();
            expect(screen.getByText("ramping up, 1 slowed")).toBeInTheDocument();
            expect(screen.getByText("Needs attention").closest("div")?.parentElement).toHaveTextContent(/^Needs attention0/);
        } finally {
            Object.keys(rows[0]).forEach((key) => delete (rows[0] as unknown as Record<string, unknown>)[key]);
            Object.assign(rows[0], original);
        }
    });
});
