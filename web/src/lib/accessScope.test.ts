import { describe, expect, it } from "vitest";
import { isScopedPath, restrictedHome } from "./accessScope";
import { PERMISSION_BITS } from "./permissions";

describe("isScopedPath", () => {
    it("opens the surfaces a restricted scope filters", () => {
        for (const p of ["/app/campaigns", "/app/campaigns/abc", "/app/campaigns/abc/steps", "/app/unibox/inbox/t1", "/app/analytics", "/app/settings/profile"]) {
            expect(isScopedPath(p)).toBe(true);
        }
    });

    it("keeps everything else closed, including campaign tabs that read contacts or settings", () => {
        for (const p of ["/app/emails", "/app/contacts", "/app/settings/members", "/app/campaigns/abc/leads", "/app/campaigns/abc/preferences", "/app/campaignsx"]) {
            expect(isScopedPath(p)).toBe(false);
        }
    });
});

describe("restrictedHome", () => {
    it("lands on the first surface the roles can read", () => {
        expect(restrictedHome({ permissions: PERMISSION_BITS.VIEW_CAMPAIGNS })).toBe("/app/campaigns");
        expect(restrictedHome({ permissions: PERMISSION_BITS.ACCESS_UNIBOX })).toBe("/app/unibox");
        expect(restrictedHome({ permissions: 0 })).toBe("/app/settings/profile");
    });
});
