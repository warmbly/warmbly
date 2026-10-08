import { act, renderHook } from "@testing-library/react";
import { type ComponentProps, type ReactNode, useState } from "react";
import { beforeEach, describe, expect, it } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import { BROWSE_STORAGE_PREFIX } from "@/lib/browseState";
import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import useContactBrowseState from "./useContactBrowseState";
import useBrowseDebouncedValue from "@/hooks/useBrowseDebouncedValue";
import { scopeSearch } from "./helpers";

let userId = "member";
function Owner({ children }: { children: ReactNode }) {
    const value = { user: { id: userId } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}
const initialSort = () => ({ sort_by: "created_at" as const, reverse: false });
const key = (name: string) => `${BROWSE_STORAGE_PREFIX}member:workspace:${name}`;

describe("contact list browsing persistence", () => {
    beforeEach(() => {
        sessionStorage.clear();
        userId = "member";
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("restores all canonical filters, blank custom pills and actual Dates on the first remount", () => {
        const page = renderHook(() => useContactBrowseState({ initialSort }), { wrapper: Owner });
        const changed: SearchContacts = {
            ...page.result.current[0], query: "alice", campaign_ids: ["selected-campaign"], category_ids: ["label"], segment_ids: ["selected-segment"],
            custom_field_filters: [{ name: "", value: "", type: "contains" }, { name: "unlisted-key", value: "yes", type: "equal" }],
            subscribed: false, verification_status: "risky", mail_hosts: ["", "gmail"], min_campaigns: 0, max_campaigns: 12,
            created_after: new Date("2026-10-01T00:00:00Z"), created_before: new Date("2026-10-02T00:00:00Z"),
            updated_after: new Date("2026-10-03T00:00:00Z"), updated_before: new Date("2026-10-04T00:00:00Z"),
        };
        act(() => page.result.current[1](changed));
        page.unmount();
        const refreshed = renderHook(() => useContactBrowseState({ initialSort }), { wrapper: Owner });
        expect(refreshed.result.current[0]).toEqual({ ...changed, lead_status: undefined, engagement: undefined });
        expect(refreshed.result.current[0].created_after?.toISOString()).toBe("2026-10-01T00:00:00.000Z");
    });

    it("keeps consecutive functional updates and distinguishes full reset from filter-bar clearing", () => {
        const page = renderHook(() => useContactBrowseState({ campaignId: "a", initialSort }), { wrapper: Owner });
        act(() => {
            page.result.current[1]((s) => ({ ...s, query: "alice" }));
            page.result.current[1]((s) => ({ ...s, lead_status: "unsubscribed", engagement: "bounced" }));
        });
        page.unmount();
        const refreshed = renderHook(() => useContactBrowseState({ campaignId: "a", initialSort }), { wrapper: Owner });
        expect(refreshed.result.current[0]).toMatchObject({ query: "alice", lead_status: "unsubscribed", engagement: "bounced", campaign_ids: ["a"] });
        act(() => refreshed.result.current[1]((s) => ({ ...scopeSearch({ campaignId: "a" }), query: s.query })));
        expect(refreshed.result.current[0].query).toBe("alice");
        act(() => refreshed.result.current[1](scopeSearch({ campaignId: "a" })));
        refreshed.unmount();
        const cleared = renderHook(() => useContactBrowseState({ campaignId: "a", initialSort }), { wrapper: Owner });
        expect(cleared.result.current[0]).toMatchObject({ query: "", custom_field_filters: [], campaign_ids: ["a"] });
        expect(cleared.result.current[0].lead_status).toBeUndefined();
    });

    it("isolates campaign and segment resources while mounted and always recomposes route IDs", () => {
        const page = renderHook(({ campaignId, segmentId }: { campaignId?: string; segmentId?: string }) => useContactBrowseState({ campaignId, segmentId, initialSort }),
            { wrapper: Owner, initialProps: { campaignId: "a", segmentId: undefined } as { campaignId?: string; segmentId?: string } });
        act(() => page.result.current[1]((s) => ({ ...s, query: "campaign-a", campaign_ids: ["wrong"], segment_ids: ["linked"], lead_status: "paused" })));
        expect(page.result.current[0].campaign_ids).toEqual(["a"]);
        page.rerender({ campaignId: "b", segmentId: undefined });
        expect(page.result.current[0]).toMatchObject({ query: "", campaign_ids: ["b"] });
        page.rerender({ campaignId: undefined, segmentId: "s" });
        act(() => page.result.current[1]((s) => ({ ...s, query: "segment", segment_ids: ["wrong"] })));
        expect(page.result.current[0].segment_ids).toEqual(["s"]);
        page.rerender({ campaignId: "a", segmentId: undefined });
        expect(page.result.current[0]).toMatchObject({ query: "campaign-a", campaign_ids: ["a"], segment_ids: ["linked"], lead_status: "paused" });
    });

    it("lets initial and mounted URL intents win without erasing unrelated filters and removes cleared parameters", () => {
        sessionStorage.setItem(key("contacts:all:filters"), JSON.stringify({ value: { query: "saved", custom_field_filters: [], campaign_ids: [] } }));
        sessionStorage.setItem(key("contacts:all:labels"), JSON.stringify({ value: ["saved-label"] }));
        const page = renderHook(({ navigationCategory }) => {
            const [params, setParams] = useState(new URLSearchParams("category=linked&contact=deep-contact"));
            const browse = useContactBrowseState({ initialSort, category: navigationCategory ?? params.get("category") ?? undefined,
                clearCategoryIntent: () => setParams((prev) => { const next = new URLSearchParams(prev); next.delete("category"); return next; }) });
            return { browse, params };
        }, { wrapper: Owner, initialProps: { navigationCategory: undefined as string | undefined } });
        expect(page.result.current.browse[0]).toMatchObject({ query: "saved", category_ids: ["linked"] });
        page.rerender({ navigationCategory: "another-link" });
        expect(page.result.current.browse[0].category_ids).toEqual(["another-link"]);
        page.rerender({ navigationCategory: undefined });
        act(() => page.result.current.browse[1]((s) => ({ ...s, category_ids: undefined })));
        expect(page.result.current.params.has("category")).toBe(false);
        expect(page.result.current.params.get("contact")).toBe("deep-contact");
        page.unmount();
        const refreshed = renderHook(() => useContactBrowseState({ initialSort }), { wrapper: Owner });
        expect(refreshed.result.current[0].category_ids).toBeUndefined();
    });

    it("does not store sort in browse filters or override the existing saved-sort authority", () => {
        const page = renderHook(() => useContactBrowseState({ initialSort }), { wrapper: Owner });
        act(() => page.result.current[1]((s) => ({ ...s, query: "saved", sort_by: "email", reverse: true })));
        expect(JSON.parse(sessionStorage.getItem(key("contacts:all:filters"))!).value).not.toHaveProperty("sort_by");
        page.unmount();
        const refreshed = renderHook(() => useContactBrowseState({ initialSort: () => ({ sort_by: "company", reverse: false }) }), { wrapper: Owner });
        expect(refreshed.result.current[0]).toMatchObject({ query: "saved", sort_by: "company", reverse: false });
    });

    it("restores ownership synchronously, including debounce mirrors, on workspace and user changes", () => {
        const page = renderHook(() => {
            const browse = useContactBrowseState({ initialSort });
            return { browse, debounced: useBrowseDebouncedValue(browse[0].query, "contacts:all") };
        }, { wrapper: Owner });
        act(() => page.result.current.browse[1]((s) => ({ ...s, query: "workspace-one" })));
        act(() => useAppStore.setState({ currentOrganization: { id: "two", name: "Two", role: "owner" } }));
        expect(page.result.current.browse[0].query).toBe("");
        expect(page.result.current.debounced).toBe("");
        act(() => page.result.current.browse[1]((s) => ({ ...s, query: "workspace-two" })));
        userId = "other-member";
        page.rerender();
        expect(page.result.current.browse[0].query).toBe("");
        userId = "member";
        page.rerender();
        expect(page.result.current.debounced).toBe("workspace-two");
    });
});
