import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import type * as Router from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import { BROWSE_STORAGE_PREFIX } from "@/lib/browseState";
import { EMPTY_TASK_SEARCH } from "@/lib/api/models/app/crm/SearchTasks";
import { EMPTY_DEAL_SEARCH } from "@/lib/api/models/app/crm/SearchDeals";
import type Pipeline from "@/lib/api/models/app/crm/Pipeline";
import TasksPage from "@/app/app/crm/tasks/page";
import DealsPage from "@/app/app/crm/deals/page";
import MeetingsPage from "@/app/app/crm/meetings/page";
import DealsTable from "./DealsTable";

const mocks = vi.hoisted(() => ({
    tasks: vi.fn(), deals: vi.fn(), dealSummary: vi.fn(), meetings: vi.fn(), pipelines: vi.fn(),
}));
vi.mock("@/lib/api/hooks/app/crm/tasks/useSearchTasks", () => ({ default: mocks.tasks }));
vi.mock("@/lib/api/hooks/app/crm/tasks/useTasksSummary", () => ({ default: () => ({}) }));
vi.mock("@/lib/api/hooks/app/crm/deals/useSearchDeals", () => ({ default: mocks.deals }));
vi.mock("@/lib/api/hooks/app/crm/deals/useDealsSummary", () => ({ default: mocks.dealSummary }));
vi.mock("@/lib/api/hooks/app/meetings/useSearchMeetings", () => ({ default: mocks.meetings }));
vi.mock("@/lib/api/hooks/app/meetings/useMeetingsSummary", () => ({ default: () => ({}) }));
vi.mock("@/lib/api/client/app/crm/pipelines/listPipelines", () => ({ default: mocks.pipelines }));
vi.mock("@/lib/api/hooks/app/organizations/useMembers", () => ({ default: () => ({ data: [] }) }));
vi.mock("@/lib/api/hooks/app/teams/useTeams", () => ({ default: () => ({ data: [] }) }));
vi.mock("@/lib/api/hooks/app/crm/taskTypes/useTaskTypes", () => ({ default: () => ({ data: [] }) }));
vi.mock("@/hooks/useCrmProvider", () => ({ default: () => ({ isExternal: false, crm: {} }) }));
vi.mock("@/hooks/useLivePatch", () => ({ useLivePatch: () => ({ pushPatch: vi.fn() }) }));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => vi.fn() }));
vi.mock("@/hooks/PresenceProvider", () => ({ usePresenceResource: vi.fn() }));
vi.mock("@/components/app/presence/GlobalCursors", () => ({ useSuppressGlobalCursors: vi.fn() }));
vi.mock("@tanstack/react-router", async (importOriginal) => ({
    ...await importOriginal<typeof Router>(),
    Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
}));

let client: QueryClient;
let userId = "user";
function Owner({ children }: { children: ReactNode }) {
    const profile = { user: { id: userId } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={profile}><QueryClientProvider client={client}>{children}</QueryClientProvider></UserContext.Provider>;
}
function save(name: string, value: unknown, workspace = "workspace") {
    sessionStorage.setItem(`${BROWSE_STORAGE_PREFIX}user:${workspace}:${name}`, JSON.stringify({ value }));
}
function pipeline(id: string, organization_id = "workspace"): Pipeline {
    return { id, organization_id, name: `Pipeline ${id}`, position: 0, stages: [], created_at: new Date(), updated_at: new Date() };
}

describe("CRM page browsing persistence", () => {
    beforeEach(() => {
        sessionStorage.clear();
        userId = "user";
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
        client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
        vi.clearAllMocks();
        mocks.tasks.mockReturnValue({ tasks: [], total: 0, hasNextPage: false });
        mocks.deals.mockReturnValue({ deals: [], total: 0 });
        mocks.dealSummary.mockReturnValue({});
        mocks.meetings.mockReturnValue({ meetings: [], total: 0, isLoading: false });
        mocks.pipelines.mockResolvedValue([pipeline("first"), pipeline("second")]);
    });
    afterEach(() => {
        cleanup();
        client.clear();
        vi.useRealTimers();
    });

    it("restores task search, status and view on the very first query after remount", () => {
        const page = render(<TasksPage />, { wrapper: Owner });
        fireEvent.change(screen.getByPlaceholderText("Search tasks…"), { target: { value: "Call Alice" } });
        fireEvent.click(screen.getByRole("button", { name: "Active" }));
        fireEvent.click(screen.getByRole("button", { name: "Grouped by due date" }));
        page.unmount();
        mocks.tasks.mockClear();
        render(<TasksPage />, { wrapper: Owner });
        expect(mocks.tasks.mock.calls[0][0].filters).toEqual({ ...EMPTY_TASK_SEARCH, query: "Call Alice", statuses: ["in_progress"] });
        expect(screen.getByPlaceholderText("Search tasks…")).toHaveValue("Call Alice");
        expect(screen.getByRole("button", { name: "Grouped by due date" })).toHaveClass("text-slate-900");
        fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
        cleanup();
        mocks.tasks.mockClear();
        render(<TasksPage />, { wrapper: Owner });
        expect(mocks.tasks.mock.calls[0][0].filters).toEqual(EMPTY_TASK_SEARCH);
    });

    it("keeps missing task types removable without waiting for option metadata", () => {
        save("crm.tasks.filters", { ...EMPTY_TASK_SEARCH, types: ["Retired type"], sort_by: "title", reverse: true });
        render(<TasksPage />, { wrapper: Owner });
        expect(mocks.tasks.mock.calls[0][0].filters.types).toEqual(["Retired type"]);
        fireEvent.click(screen.getByRole("button", { name: /1 type/ }));
        fireEvent.click(screen.getByText("Retired type"));
        expect(mocks.tasks.mock.lastCall?.[0].filters).toEqual({ ...EMPTY_TASK_SEARCH, sort_by: "title", reverse: true });
    });

    it("remembers the task popover's partial clear without erasing unrelated filters or sort", () => {
        const kept = { ...EMPTY_TASK_SEARCH, query: "Call", statuses: ["pending"], types: ["Email"], sort_by: "title", reverse: true };
        save("crm.tasks.filters", { ...kept, priorities: ["urgent"], due_after: "2026-10-01", overdue: true });
        const page = render(<TasksPage />, { wrapper: Owner });
        fireEvent.click(screen.getByRole("button", { name: /Filters/ }));
        fireEvent.click(screen.getByRole("button", { name: "Clear" }));
        page.unmount();
        mocks.tasks.mockClear();
        render(<TasksPage />, { wrapper: Owner });
        expect(mocks.tasks.mock.calls[0][0].filters).toEqual(kept);
    });

    it("restores the deal table across unmounts and retains a removable deleted pipeline", () => {
        save("crm.deals.table.filters", { ...EMPTY_DEAL_SEARCH, pipeline_ids: ["gone"], min_value: 0, sort_by: "value", reverse: true });
        const page = render(<DealsTable pipelines={[]} onOpenDeal={vi.fn()} />, { wrapper: Owner });
        fireEvent.change(screen.getByPlaceholderText("Search deals…"), { target: { value: "Renewal" } });
        fireEvent.click(screen.getByRole("button", { name: "Won" }));
        page.unmount();
        mocks.deals.mockClear();
        render(<DealsTable pipelines={[]} onOpenDeal={vi.fn()} />, { wrapper: Owner });
        expect(mocks.deals.mock.calls[0][0].filters).toEqual({
            ...EMPTY_DEAL_SEARCH, query: "Renewal", statuses: ["won"], pipeline_ids: ["gone"], min_value: 0, sort_by: "value", reverse: true,
        });
        fireEvent.click(screen.getByRole("button", { name: /1 pipeline/ }));
        fireEvent.click(screen.getByText("Selected pipeline gone"));
        expect(mocks.deals.mock.lastCall?.[0].filters.pipeline_ids).toEqual([]);
    });

    it("starts meetings with the restored trimmed query, not a temporary default request", () => {
        vi.useFakeTimers();
        const page = render(<MeetingsPage />, { wrapper: Owner });
        fireEvent.change(screen.getByPlaceholderText("Search name, email, or event…"), { target: { value: "  Discovery  " } });
        fireEvent.click(screen.getByRole("button", { name: "Past" }));
        act(() => vi.advanceTimersByTime(250));
        page.unmount();
        mocks.meetings.mockClear();
        render(<MeetingsPage />, { wrapper: Owner });
        expect(mocks.meetings.mock.calls[0][0].filters).toEqual({ timeframe: "past", q: "Discovery" });
        expect(screen.getByPlaceholderText("Search name, email, or event…")).toHaveValue("  Discovery  ");
        act(() => vi.advanceTimersByTime(250));
        expect(mocks.meetings.mock.lastCall?.[0].filters).toEqual({ timeframe: "past", q: "Discovery" });
    });

    it("rejects nonfinite deal value input and persists the advanced-filter partial clear", () => {
        const kept = { ...EMPTY_DEAL_SEARCH, query: "Renewal", statuses: ["open"], pipeline_ids: ["first"], sort_by: "value", reverse: true };
        save("crm.deals.table.filters", { ...kept, min_value: 0, max_value: 500, close_after: "2026-10-01" });
        const page = render(<DealsTable pipelines={[pipeline("first")]} onOpenDeal={vi.fn()} />, { wrapper: Owner });
        fireEvent.click(screen.getByRole("button", { name: /Filters/ }));
        fireEvent.change(screen.getByPlaceholderText("Min"), { target: { value: "Infinity" } });
        expect(mocks.deals.mock.lastCall?.[0].filters.min_value).toBe(0);
        fireEvent.click(screen.getByRole("button", { name: "Clear" }));
        page.unmount();
        mocks.deals.mockClear();
        render(<DealsTable pipelines={[pipeline("first")]} onOpenDeal={vi.fn()} />, { wrapper: Owner });
        expect(mocks.deals.mock.calls[0][0].filters).toEqual(kept);
    });

    it("does not leak a pending meetings debounce into another workspace or signed-in user", () => {
        vi.useFakeTimers();
        save("crm.meetings.search", " receiving ", "another");
        save("crm.meetings.timeframe", "all", "another");
        const page = render(<MeetingsPage />, { wrapper: Owner });
        fireEvent.change(screen.getByPlaceholderText("Search name, email, or event…"), { target: { value: "old workspace" } });
        mocks.meetings.mockClear();
        act(() => useAppStore.setState({ currentOrganization: { id: "another", name: "Another", role: "owner" } }));
        expect(mocks.meetings.mock.calls[0][0].filters).toEqual({ timeframe: "", q: "receiving" });
        act(() => vi.advanceTimersByTime(250));
        expect(mocks.meetings.mock.lastCall?.[0].filters.q).toBe("receiving");
        userId = "other-user";
        mocks.meetings.mockClear();
        page.rerender(<MeetingsPage />);
        expect(mocks.meetings.mock.calls[0][0].filters).toEqual({ timeframe: "upcoming", q: undefined });
    });

    it("waits for authoritative board metadata before replacing a deleted pipeline", async () => {
        let resolve!: (pipelines: Pipeline[]) => void;
        mocks.pipelines.mockReturnValue(new Promise<Pipeline[]>((done) => { resolve = done; }));
        save("crm.deals.view", "board");
        save("crm.deals.board.pipeline", "gone");
        save("crm.deals.board.search", "Board query");
        render(<DealsPage />, { wrapper: Owner });
        expect(mocks.dealSummary.mock.lastCall?.[0].pipeline_ids).toEqual(["gone"]);
        expect(JSON.parse(sessionStorage.getItem(`${BROWSE_STORAGE_PREFIX}user:workspace:crm.deals.board.pipeline`)!).value).toBe("gone");
        await act(async () => resolve([pipeline("first"), pipeline("second")]));
        await waitFor(() => expect(mocks.dealSummary.mock.lastCall?.[0].pipeline_ids).toEqual(["first"]));
        for (const input of screen.getAllByPlaceholderText("Search deals…")) expect(input).toHaveValue("Board query");
    });

    it("clears a missing board pipeline only on successful empty metadata without an update loop", async () => {
        mocks.pipelines.mockResolvedValue([]);
        save("crm.deals.board.pipeline", "gone");
        render(<DealsPage />, { wrapper: Owner });
        await waitFor(() => expect(mocks.dealSummary.mock.lastCall?.[0].pipeline_ids).toEqual([]));
        expect(mocks.dealSummary.mock.calls.length).toBeLessThan(10);
        expect(JSON.parse(sessionStorage.getItem(`${BROWSE_STORAGE_PREFIX}user:workspace:crm.deals.board.pipeline`)!).value).toBeUndefined();
    });

    it("does not discard a restored board pipeline on failed metadata", async () => {
        mocks.pipelines.mockRejectedValue(new Error("Metadata unavailable"));
        save("crm.deals.board.pipeline", "remembered");
        render(<DealsPage />, { wrapper: Owner });
        await screen.findByText("No pipelines yet");
        expect(mocks.dealSummary.mock.lastCall?.[0].pipeline_ids).toEqual(["remembered"]);
        expect(JSON.parse(sessionStorage.getItem(`${BROWSE_STORAGE_PREFIX}user:workspace:crm.deals.board.pipeline`)!).value).toBe("remembered");
    });

    it("restores board choices, retains table filters across view toggles and never defaults to previous workspace metadata", async () => {
        save("crm.deals.board.pipeline", "second");
        save("crm.deals.table.filters", { ...EMPTY_DEAL_SEARCH, query: "table query", statuses: ["lost"] });
        const page = render(<DealsPage />, { wrapper: Owner });
        await screen.findByPlaceholderText("Search deals…");
        fireEvent.click(screen.getByRole("button", { name: "Board" }));
        fireEvent.change(screen.getAllByPlaceholderText("Search deals…")[0], { target: { value: "board query" } });
        fireEvent.click(screen.getByRole("button", { name: "Table" }));
        expect(screen.getByPlaceholderText("Search deals…")).toHaveValue("table query");
        fireEvent.click(screen.getByRole("button", { name: "Board" }));
        page.unmount();
        render(<DealsPage />, { wrapper: Owner });
        await screen.findAllByPlaceholderText("Search deals…");
        for (const input of screen.getAllByPlaceholderText("Search deals…")) expect(input).toHaveValue("board query");
        expect(mocks.dealSummary.mock.lastCall?.[0].pipeline_ids).toEqual(["second"]);
        let resolve!: (pipelines: Pipeline[]) => void;
        mocks.pipelines.mockReturnValue(new Promise<Pipeline[]>((done) => { resolve = done; }));
        save("crm.deals.board.pipeline", "new", "another");
        save("crm.deals.view", "board", "another");
        act(() => useAppStore.setState({ currentOrganization: { id: "another", name: "Another", role: "owner" } }));
        expect(mocks.dealSummary.mock.lastCall?.[0].pipeline_ids).toEqual(["new"]);
        await act(async () => resolve([pipeline("new", "another")]));
        await waitFor(() => expect(screen.getAllByRole("button", { name: /Pipeline new/ })).toHaveLength(2));
        expect(mocks.dealSummary.mock.lastCall?.[0].pipeline_ids).toEqual(["new"]);
    });
});
