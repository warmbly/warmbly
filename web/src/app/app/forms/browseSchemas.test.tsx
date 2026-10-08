import { act, renderHook } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it } from "vitest";
import type { z } from "zod";
import useBrowseState from "@/hooks/useBrowseState";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import {
    aiUsageRangeSchema, auditActionSchema, auditDateSchema, auditEntitySchema, billingIntervalSchema,
    browseFlagSchema, browseSearchSchema, formAnalyticsRangeSchema, formsSortSchema, formsStatusSchema,
    oauthAppsTabSchema, slackTabSchema, submissionsStatusSchema, webhookStatusSchema, webhookTabSchema,
} from "@/lib/browse-other-lists";

function Owner({ children }: { children: ReactNode }) {
    const value = { user: { id: "user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}

const cases: { name: string; schema: z.ZodType<unknown>; initial: unknown; changed: unknown; invalid: unknown[] }[] = [
    { name: "search", schema: browseSearchSchema, initial: "", changed: "Alice", invalid: [null, 12, {}] },
    { name: "review", schema: browseFlagSchema, initial: false, changed: true, invalid: [null, "true", 1] },
    { name: "forms.status", schema: formsStatusSchema, initial: "all", changed: "published", invalid: [null, "live"] },
    { name: "forms.sort", schema: formsSortSchema, initial: { key: "created", dir: -1 }, changed: { key: "name", dir: 1 }, invalid: [null, [], { key: "__proto__", dir: 1 }, { key: "name", dir: 0 }, { key: "name", dir: "1" }, { key: "name", dir: Infinity }, { key: "name", dir: 1, stale: true }] },
    { name: "form.submissions.status", schema: submissionsStatusSchema, initial: "all", changed: "junk", invalid: [null, "draft"] },
    { name: "form.analytics.range", schema: formAnalyticsRangeSchema, initial: "30d", changed: "90d", invalid: [30, "365d"] },
    { name: "audit.action", schema: auditActionSchema.optional(), initial: undefined, changed: "export", invalid: [null, "unknown", "__proto__"] },
    { name: "audit.entity", schema: auditEntitySchema.optional(), initial: undefined, changed: "crm_deal", invalid: [null, "unknown"] },
    { name: "audit.date", schema: auditDateSchema, initial: "", changed: "2024-02-29", invalid: [null, 0, "2025-02-29", "2026-04-31", "2026-10-8", "2026-10-08T00:00:00Z"] },
    { name: "billing.interval", schema: billingIntervalSchema, initial: "annual", changed: "monthly", invalid: [null, "weekly"] },
    { name: "billing.usage.range", schema: aiUsageRangeSchema, initial: 30, changed: 90, invalid: [null, "30", 31, NaN, Infinity] },
    { name: "oauth.tab", schema: oauthAppsTabSchema, initial: "apps", changed: "authorized", invalid: [null, "history"] },
    { name: "webhook.tab", schema: webhookTabSchema, initial: "overview", changed: "deliveries", invalid: [null, "history"] },
    { name: "webhook.status", schema: webhookStatusSchema, initial: "", changed: "failed", invalid: [null, "success"] },
    { name: "slack.tab", schema: slackTabSchema, initial: "overview", changed: "members", invalid: [null, "settings"] },
];

describe("other-list browsing schemas", () => {
    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it.each(cases)("restores and explicitly clears $name on remount", ({ name, schema, initial, changed }) => {
        const mounted = renderHook(() => useBrowseState(name, initial, schema), { wrapper: Owner });
        act(() => mounted.result.current[1](changed));
        mounted.unmount();
        const refreshed = renderHook(() => useBrowseState(name, initial, schema), { wrapper: Owner });
        expect(refreshed.result.current[0]).toEqual(changed);
        act(() => refreshed.result.current[1](initial));
        refreshed.unmount();
        const cleared = renderHook(() => useBrowseState(name, initial, schema), { wrapper: Owner });
        expect(cleared.result.current[0]).toEqual(initial);
    });

    it.each(cases)("rejects stale and malformed $name values without coercion", ({ schema, invalid }) => {
        for (const value of invalid) expect(schema.safeParse(value).success).toBe(false);
    });
});
