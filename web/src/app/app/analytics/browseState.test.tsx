import { act, renderHook } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it } from "vitest";
import type { z } from "zod";
import useBrowseState from "@/hooks/useBrowseState";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import { BROWSE_STORAGE_PREFIX } from "@/lib/browseState";
import * as schemas from "@/lib/browse-accounts-analytics";

function Owner({ children }: { children: ReactNode }) {
    const profile = { user: { id: "account-user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={profile}>{children}</UserContext.Provider>;
}

const cases: { name: string; schema: z.ZodType; initial: unknown; value: unknown }[] = [
    { name: "analytics.range", schema: schemas.analyticsRange, initial: "7d", value: "90d" },
    { name: "analytics.hiddenMetrics", schema: schemas.analyticsHiddenMetrics, initial: [], value: ["sent", "replies"] },
    { name: "analytics.campaigns", schema: schemas.analyticsCampaignFilter, initial: { campaigns: [], folders: [] }, value: { campaigns: ["c1", "c2"], folders: ["f1"] } },
    { name: "deliverability.view", schema: schemas.deliverabilityView, initial: { range: "7d", hidden: [] }, value: { range: "30d", hidden: ["providers", "mailboxes"] } },
    { name: "deliverability.hiddenMetrics", schema: schemas.deliverabilityHiddenMetrics, initial: ["sent"], value: ["opens", "replies"] },
    { name: "emails.search", schema: schemas.browseString, initial: "", value: "alice" },
    { name: "emails.sort", schema: schemas.mailboxSort, initial: null, value: { by: "health", reverse: true } },
    { name: "emails.mailbox.a.tab", schema: schemas.mailboxTab, initial: "overview", value: "analytics" },
    { name: "emails.domains.filter", schema: schemas.domainFilter, initial: "all", value: "vendor" },
    { name: "emails.domain.a.test.tab", schema: schemas.domainTab, initial: "overview", value: "tracking" },
    { name: "emails.import.a.results.filter", schema: schemas.importFilter, initial: { status: "", cause: "" }, value: { status: "failed", cause: "password" } },
    { name: "emails.import.vendor.a.pick.filter", schema: schemas.importPickFilter, initial: "all", value: "moving" },
    { name: "emails.domains.bulkSetup.list.limit", schema: schemas.domainListLimit, initial: 20, value: 70 },
    { name: "emails.mailbox.a.placement.range", schema: schemas.placementRange, initial: "30d", value: "14d" },
    { name: "emails.mailbox.a.placement.group", schema: schemas.placementGroupFilter, initial: "all", value: "yahoo" },
    { name: "deliverability.placement.providerExpanded", schema: schemas.placementGroup.nullable(), initial: null, value: "google" },
    { name: "deliverability.placement.showAll", schema: schemas.browseBoolean, initial: false, value: true },
    { name: "placement.batch.a.tab", schema: schemas.batchTab, initial: "mailboxes", value: "recipients" },
    { name: "placement.batch.a.senders.sort", schema: schemas.batchSenderSort, initial: "worst", value: "status" },
    { name: "placement.batch.a.senders.status", schema: schemas.batchSenderStatus, initial: "", value: "cancelled" },
];

describe("accounts and analytics browse schemas", () => {
    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it.each(cases)("restores $name synchronously after change and remount", ({ name, schema, initial, value }) => {
        const page = renderHook(() => useBrowseState(name, initial, schema), { wrapper: Owner });
        act(() => page.result.current[1](value));
        page.unmount();
        const refreshed = renderHook(() => useBrowseState(name, initial, schema), { wrapper: Owner });
        expect(refreshed.result.current[0]).toEqual(value);
        act(() => refreshed.result.current[1](initial));
        refreshed.unmount();
        const cleared = renderHook(() => useBrowseState(name, initial, schema), { wrapper: Owner });
        expect(cleared.result.current[0]).toEqual(initial);
    });

    it("isolates import history filters by resource without erasing a remembered cause", () => {
        const page = renderHook(({ id }) => useBrowseState(`emails.import.${id}.results.filter`, { status: "", cause: "" }, schemas.importFilter), {
            wrapper: Owner, initialProps: { id: "a" },
        });
        act(() => page.result.current[1]({ status: "failed", cause: "password" }));
        page.rerender({ id: "b" });
        expect(page.result.current[0]).toEqual({ status: "", cause: "" });
        act(() => page.result.current[1]({ status: "needs_signin", cause: "" }));
        page.rerender({ id: "a" });
        expect(page.result.current[0]).toEqual({ status: "failed", cause: "password" });
    });

    it("lets initial navigation override a remembered resource tab and remembers explicit clearing", () => {
        const key = `${BROWSE_STORAGE_PREFIX}account-user:workspace:emails.mailbox.a.tab`;
        sessionStorage.setItem(key, JSON.stringify({ value: "settings" }));
        const page = renderHook(() => useBrowseState("emails.mailbox.a.tab", "overview", schemas.mailboxTab, { initialOverride: "analytics" }), { wrapper: Owner });
        expect(page.result.current[0]).toBe("analytics");
        act(() => page.result.current[1]("overview"));
        page.unmount();
        const refreshed = renderHook(() => useBrowseState("emails.mailbox.a.tab", "overview", schemas.mailboxTab), { wrapper: Owner });
        expect(refreshed.result.current[0]).toBe("overview");
    });

    it("rejects stale enums, malformed structures, duplicate metrics and hidden-all metrics", () => {
        expect(schemas.mailboxSort.safeParse({ by: "health", reverse: false, obsolete: true }).success).toBe(false);
        expect(schemas.mailboxSort.safeParse({ by: "old", reverse: false }).success).toBe(false);
        expect(schemas.mailboxSort.safeParse({ by: "health", reverse: "yes" }).success).toBe(false);
        expect(schemas.mailboxTab.safeParse("old").success).toBe(false);
        expect(schemas.domainTab.safeParse("old").success).toBe(false);
        expect(schemas.batchSenderStatus.safeParse("needs_signin").success).toBe(false);
        expect(schemas.analyticsCampaignFilter.safeParse({ campaigns: [], folders: [], tags: [] }).success).toBe(false);
        expect(schemas.analyticsCampaignFilter.safeParse({ campaigns: "c1", folders: [] }).success).toBe(false);
        expect(schemas.analyticsHiddenMetrics.safeParse(["sent", "sent"]).success).toBe(false);
        expect(schemas.analyticsHiddenMetrics.safeParse(["sent", "opens", "clicks", "replies"]).success).toBe(false);
        expect(schemas.deliverabilityHiddenMetrics.safeParse(["sent", "opens", "replies", "bounces", "complaints"]).success).toBe(false);
        expect(schemas.deliverabilityView.safeParse({ range: "30d", hidden: ["old"] }).success).toBe(false);
        expect(schemas.deliverabilityView.safeParse({ range: "30d", hidden: ["chart", "chart"] }).success).toBe(false);
        expect(schemas.importFilter.safeParse({ status: "connected", cause: "password" }).success).toBe(false);
        expect(schemas.importFilter.safeParse({ status: "failed", cause: "password", cursor: "a" }).success).toBe(false);
    });

    it.each([0, -1, 1.5, Infinity, NaN, null, "20"])("rejects invalid display limits: %s", (value) => {
        expect(schemas.domainListLimit.safeParse(value).success).toBe(false);
    });
});
