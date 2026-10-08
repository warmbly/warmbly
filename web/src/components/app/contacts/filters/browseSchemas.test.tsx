import { act, renderHook } from "@testing-library/react";
import { type ComponentProps, type ReactNode } from "react";
import { beforeEach, describe, expect, it } from "vitest";
import { z } from "zod";
import { UserContext } from "@/hooks/context/user";
import useBrowseState from "@/hooks/useBrowseState";
import { useAppStore } from "@/stores/useAppStore";
import {
    activityFiltersSchema, browseIds, browseText, campaignPeriodSchema, campaignSortSchema, campaignStatusSchema,
    contactExtrasSchema, contactFiltersSchema, contactGeneralExtrasSchema, contactTabSchema, hiddenMetricsSchema, importViewSchema,
} from "@/lib/browse-contacts-campaigns";

function Owner({ children }: { children: ReactNode }) {
    const value = { user: { id: "member" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}
const filters = { query: "", custom_field_filters: [], campaign_ids: [] };

describe("contacts and campaigns browse schemas", () => {
    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it.each([null, "2026-02-30T00:00:00Z", "not-a-date", 0, new Date(NaN)])("rejects unsafe persisted dates: %s", (date) => {
        expect(contactFiltersSchema.safeParse({ ...filters, created_after: date }).success).toBe(false);
    });
    it.each([NaN, Infinity, -1, 1.5, "2", null])("rejects invalid campaign counts: %s", (count) => {
        expect(contactFiltersSchema.safeParse({ ...filters, min_campaigns: count }).success).toBe(false);
        expect(contactFiltersSchema.safeParse({ ...filters, max_campaigns: count }).success).toBe(false);
    });
    it("validates required arrays, booleans, free custom keys and the full chip enums", () => {
        expect(contactFiltersSchema.safeParse({ ...filters, campaign_ids: null }).success).toBe(false);
        expect(contactFiltersSchema.safeParse({ ...filters, subscribed: "false" }).success).toBe(false);
        expect(contactFiltersSchema.safeParse({ ...filters, subscribed: undefined }).success).toBe(true);
        expect(contactFiltersSchema.safeParse({ ...filters, verification_status: "stale", mail_hosts: ["stale"] }).success).toBe(false);
        expect(contactFiltersSchema.safeParse({ ...filters, lead_status: "stale", engagement: "stale" }).success).toBe(false);
        const custom_field_filters = [{ name: "", value: "", type: "equal" }, { name: "no longer suggested", value: "v", type: "contains" }];
        for (const engagement of ["opened", "not_opened", "clicked", "not_clicked", "replied", "not_replied", "bounced"]) {
            expect(contactFiltersSchema.safeParse({ ...filters, custom_field_filters, lead_status: "unsubscribed", engagement }).success).toBe(true);
        }
    });
    it("validates real ordered calendar days for activity and campaign reporting", () => {
        for (const from of ["2026-02-30", "2026-01-01T00:00:00Z", null]) {
            expect(activityFiltersSchema.safeParse({ type: "all", query: "", from, to: "" }).success).toBe(false);
            expect(campaignPeriodSchema.safeParse({ key: "custom", from, to: "2026-03-01" }).success).toBe(false);
        }
        expect(activityFiltersSchema.safeParse({ type: "website", query: "", from: "2026-10-02", to: "2026-10-01" }).success).toBe(false);
        expect(campaignPeriodSchema.safeParse({ key: "custom", from: "2026-10-02", to: "2026-10-01" }).success).toBe(false);
        expect(campaignPeriodSchema.safeParse({ key: "custom", from: "2024-02-29", to: "2024-02-29" }).success).toBe(true);
    });
    it("rejects stale tabs, views, list enums, duplicate extras and hiding every chart metric", () => {
        for (const schema of [campaignStatusSchema, campaignSortSchema, contactTabSchema, importViewSchema]) {
            expect(schema.safeParse("obsolete").success).toBe(false);
        }
        expect(contactExtrasSchema.safeParse(["provider", "provider"]).success).toBe(false);
        expect(contactGeneralExtrasSchema.safeParse(["lead_status"]).success).toBe(false);
        expect(contactGeneralExtrasSchema.safeParse(["engagement"]).success).toBe(false);
        expect(hiddenMetricsSchema.safeParse(["sent", "opens", "clicks", "replies"]).success).toBe(false);
        expect(hiddenMetricsSchema.safeParse(["opens", "opens"]).success).toBe(false);
    });

    const cases: { name: string; schema: z.ZodType; initial: unknown; changed: unknown }[] = [
        { name: "campaigns:list:folder", schema: browseText, initial: "", changed: "folder-id" },
        { name: "campaigns:list:query", schema: browseText, initial: "", changed: "renewal" },
        { name: "campaigns:list:status", schema: campaignStatusSchema, initial: "all", changed: "paused" },
        { name: "campaigns:list:sort", schema: campaignSortSchema, initial: "newest", changed: "name" },
        { name: "contacts:labels:query", schema: browseText, initial: "", changed: "vip" },
        { name: "contacts:segments:query", schema: browseText, initial: "", changed: "europe" },
        { name: "contacts:suppressions:query", schema: browseText, initial: "", changed: "example.com" },
        { name: "campaigns:a:analytics:period", schema: campaignPeriodSchema, initial: { key: "all" }, changed: { key: "custom", from: "2026-10-01", to: "2026-10-07" } },
        { name: "campaigns:a:analytics:hidden-metrics", schema: hiddenMetricsSchema, initial: [], changed: ["opens", "clicks", "replies"] },
        { name: "contacts:a:panel:tab", schema: contactTabSchema, initial: "overview", changed: "research" },
        { name: "contacts:a:activity:filters", schema: activityFiltersSchema, initial: { type: "all", query: "", from: "", to: "" }, changed: { type: "replies", query: "subject", from: "2026-10-01", to: "2026-10-07" } },
        { name: "contacts:crm-import:a:lists:query", schema: browseText, initial: "", changed: "accounts" },
        { name: "contacts:imports:a:failures:query", schema: browseText, initial: "", changed: "invalid" },
        { name: "contacts:imports:a:mapping:view", schema: importViewSchema, initial: "columns", changed: "preview" },
        { name: "contacts:imports:a:mapping:query", schema: browseText, initial: "", changed: "email" },
        { name: "contacts:imports:a:mapping:unmapped-only", schema: z.boolean(), initial: false, changed: true },
        { name: "contacts:add-from:segment:a:labels", schema: browseIds, initial: [], changed: ["unlisted-label"] },
    ];
    it.each(cases)("restores $name after changing and remounting, without leaking to another resource", ({ name, schema, initial, changed }) => {
        const page = renderHook(({ resource }) => useBrowseState(resource, initial, schema), { wrapper: Owner, initialProps: { resource: name } });
        expect(page.result.current[0]).toEqual(initial);
        act(() => page.result.current[1](changed));
        page.unmount();
        const refreshed = renderHook(({ resource }) => useBrowseState(resource, initial, schema), { wrapper: Owner, initialProps: { resource: name } });
        expect(refreshed.result.current[0]).toEqual(changed);
        refreshed.rerender({ resource: `${name}:other` });
        expect(refreshed.result.current[0]).toEqual(initial);
    });
});
