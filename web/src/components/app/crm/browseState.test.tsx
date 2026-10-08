import { act, renderHook } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it } from "vitest";
import useBrowseState from "@/hooks/useBrowseState";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import { BROWSE_STORAGE_PREFIX } from "@/lib/browseState";
import {
    taskBrowseFiltersSchema, taskBrowseViewSchema,
    dealBrowseFiltersSchema, dealBrowseViewSchema, dealBoardPipelineSchema,
    crmBrowseSearchSchema, meetingBrowseTimeframeSchema,
} from "@/lib/browse-crm";
import { EMPTY_TASK_SEARCH } from "@/lib/api/models/app/crm/SearchTasks";
import { EMPTY_DEAL_SEARCH } from "@/lib/api/models/app/crm/SearchDeals";

function Owner({ children }: { children: ReactNode }) {
    const profile = { user: { id: "user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={profile}>{children}</UserContext.Provider>;
}

describe("CRM browsing schemas and restoration", () => {
    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("restores every task filter and view on remount and keeps functional partial resets", () => {
        const usePreferences = () => ({
            filters: useBrowseState("crm.tasks.filters", EMPTY_TASK_SEARCH, taskBrowseFiltersSchema),
            view: useBrowseState("crm.tasks.view", "flat", taskBrowseViewSchema),
        });
        const changed = {
            ...EMPTY_TASK_SEARCH, query: " follow up ", statuses: ["in_progress" as const],
            priorities: ["urgent" as const], types: ["Call"], assigned_to: ["former-member"], team_ids: ["former-team"],
            due_after: "2026-10-01T00:00:00.000Z", due_before: "2026-10-30T00:00:00+02:00",
            overdue: true, sort_by: "due_date" as const, reverse: true,
        };
        const page = renderHook(usePreferences, { wrapper: Owner });
        act(() => {
            page.result.current.filters[1](changed);
            page.result.current.view[1]("grouped");
        });
        page.unmount();
        const refreshed = renderHook(usePreferences, { wrapper: Owner });
        expect(refreshed.result.current.filters[0]).toEqual(changed);
        expect(refreshed.result.current.view[0]).toBe("grouped");
        act(() => refreshed.result.current.filters[1]((f) => ({
            ...f, priorities: [], due_after: undefined, due_before: undefined, overdue: undefined,
        })));
        refreshed.unmount();
        const cleared = renderHook(usePreferences, { wrapper: Owner });
        expect(cleared.result.current.filters[0]).toEqual({
            ...changed, priorities: [], due_after: undefined, due_before: undefined, overdue: undefined,
        });
        act(() => cleared.result.current.filters[1](EMPTY_TASK_SEARCH));
        cleared.unmount();
        expect(renderHook(usePreferences, { wrapper: Owner }).result.current.filters[0]).toEqual(EMPTY_TASK_SEARCH);
    });

    it("restores independent board and table state without storing derived board API scopes", () => {
        const usePreferences = () => ({
            filters: useBrowseState("crm.deals.table.filters", EMPTY_DEAL_SEARCH, dealBrowseFiltersSchema),
            pipeline: useBrowseState("crm.deals.board.pipeline", undefined, dealBoardPipelineSchema),
            search: useBrowseState("crm.deals.board.search", "", crmBrowseSearchSchema),
            view: useBrowseState("crm.deals.view", "table", dealBrowseViewSchema),
        });
        const changed = {
            ...EMPTY_DEAL_SEARCH, query: "table query", statuses: ["won" as const], pipeline_ids: ["deleted-pipeline"],
            min_value: 0, max_value: 2500.5, close_after: "2026-10-01", close_before: "2026-10-30T00:00:00Z",
            sort_by: "value" as const, reverse: true,
        };
        const page = renderHook(usePreferences, { wrapper: Owner });
        act(() => {
            page.result.current.filters[1](changed);
            page.result.current.pipeline[1]("board-pipeline");
            page.result.current.search[1]("board query");
            page.result.current.view[1]("board");
        });
        page.unmount();
        const refreshed = renderHook(usePreferences, { wrapper: Owner });
        expect(refreshed.result.current.filters[0]).toEqual(changed);
        expect(refreshed.result.current.pipeline[0]).toBe("board-pipeline");
        expect(refreshed.result.current.search[0]).toBe("board query");
        expect(refreshed.result.current.view[0]).toBe("board");
    });

    it("restores meeting tabs and exact raw input, and isolates workspaces while mounted", () => {
        const usePreferences = () => ({
            search: useBrowseState("crm.meetings.search", "", crmBrowseSearchSchema),
            timeframe: useBrowseState("crm.meetings.timeframe", "upcoming", meetingBrowseTimeframeSchema),
        });
        const page = renderHook(usePreferences, { wrapper: Owner });
        act(() => {
            page.result.current.search[1]("  booked call  ");
            page.result.current.timeframe[1]("past");
        });
        act(() => useAppStore.setState({ currentOrganization: { id: "another", name: "Another", role: "owner" } }));
        expect(page.result.current.search[0]).toBe("");
        expect(page.result.current.timeframe[0]).toBe("upcoming");
        act(() => useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } }));
        expect(page.result.current.search[0]).toBe("  booked call  ");
        expect(page.result.current.timeframe[0]).toBe("past");
        page.unmount();
        const refreshed = renderHook(usePreferences, { wrapper: Owner });
        expect(refreshed.result.current.search[0]).toBe("  booked call  ");
        expect(refreshed.result.current.timeframe[0]).toBe("past");
    });

    it("merges partial known filters into defaults without reviving hidden API scope", () => {
        expect(taskBrowseFiltersSchema.parse({ query: "a", contact_id: "contact", deal_id: "deal" }))
            .toEqual({ ...EMPTY_TASK_SEARCH, query: "a" });
        expect(dealBrowseFiltersSchema.parse({ query: "b", stage_ids: ["stage"], assigned_to: ["owner"], created_after: "bad" }))
            .toEqual({ ...EMPTY_DEAL_SEARCH, query: "b" });
    });

    it.each([
        { statuses: ["done"] }, { priorities: ["important"] }, { types: null }, { assigned_to: [1] },
        { team_ids: "team" }, { reverse: "true" }, { sort_by: "deprecated" }, { overdue: null },
        { due_after: null }, { due_before: "2026-02-30" }, { due_after: "October 8" },
    ])("rejects malformed task preferences %j", (value) => {
        expect(taskBrowseFiltersSchema.safeParse(value).success).toBe(false);
    });

    it.each([
        null, [], { statuses: ["archived"] }, { pipeline_ids: "pipeline" }, { min_value: null },
        { max_value: NaN }, { min_value: Infinity }, { max_value: -Infinity }, { close_after: null },
        { close_before: "2026-02-30T00:00:00Z" }, { sort_by: "priority" }, { query: false },
    ])("rejects malformed deal preferences %j", (value) => {
        expect(dealBrowseFiltersSchema.safeParse(value).success).toBe(false);
    });

    it("rejects stale selections and safely falls back on a malformed stored filter", () => {
        expect(taskBrowseViewSchema.safeParse("board").success).toBe(false);
        expect(dealBrowseViewSchema.safeParse("grouped").success).toBe(false);
        expect(meetingBrowseTimeframeSchema.safeParse("today").success).toBe(false);
        expect(dealBoardPipelineSchema.safeParse("").success).toBe(false);
        expect(dealBoardPipelineSchema.safeParse(null).success).toBe(false);
        sessionStorage.setItem(`${BROWSE_STORAGE_PREFIX}user:workspace:crm.deals.table.filters`, JSON.stringify({
            value: { ...EMPTY_DEAL_SEARCH, query: "bad numeric shape", min_value: null },
        }));
        const page = renderHook(() => useBrowseState("crm.deals.table.filters", EMPTY_DEAL_SEARCH, dealBrowseFiltersSchema), { wrapper: Owner });
        expect(page.result.current[0]).toEqual(EMPTY_DEAL_SEARCH);
    });
});
