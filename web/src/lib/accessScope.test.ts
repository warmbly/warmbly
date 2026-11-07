import { describe, expect, it } from "vitest";
import {
    CLIENT_ROLE_PRESETS,
    isScopedPath,
    presetForRole,
    restrictedHome,
    roleScopeSafe,
    SCOPE_ACTION_PERMISSIONS,
    SCOPE_STEP_ACTIONS,
    scopeCanAct,
} from "./accessScope";
import { PERMISSION_BITS } from "./permissions";

describe("isScopedPath", () => {
    it("opens the surfaces a restricted scope filters", () => {
        for (const p of ["/app/campaigns", "/app/campaigns/abc", "/app/campaigns/abc/steps", "/app/campaigns/abc/schedule", "/app/unibox/inbox/t1", "/app/analytics", "/app/settings/profile"]) {
            expect(isScopedPath(p)).toBe(true);
        }
    });

    it("keeps everything else closed, including campaign tabs that read contacts or settings", () => {
        for (const p of ["/app/emails", "/app/contacts", "/app/settings/members", "/app/settings/roles", "/app/campaigns/abc/leads", "/app/campaigns/abc/preferences", "/app/campaignsx"]) {
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

describe("scopeCanAct", () => {
    it("acts only with what a role that acts in scope gives", () => {
        const org = { scope_actions: PERMISSION_BITS.ACCESS_UNIBOX | PERMISSION_BITS.VIEW_CAMPAIGNS };
        expect(scopeCanAct(org, PERMISSION_BITS.ACCESS_UNIBOX)).toBe(true);
        expect(scopeCanAct(org, PERMISSION_BITS.MANAGE_CAMPAIGNS)).toBe(false);
        expect(scopeCanAct({}, PERMISSION_BITS.ACCESS_UNIBOX)).toBe(false);
        expect(scopeCanAct(null, PERMISSION_BITS.ACCESS_UNIBOX)).toBe(false);
    });

    it("never acts with a workspace-wide permission, whatever the server sends", () => {
        const org = { scope_actions: 0xffff };
        for (const bit of [PERMISSION_BITS.MANAGE_TEAM, PERMISSION_BITS.MANAGE_SETTINGS, PERMISSION_BITS.MANAGE_EMAILS, PERMISSION_BITS.VIEW_CONTACTS, PERMISSION_BITS.USE_AI, 0]) {
            expect(scopeCanAct(org, bit)).toBe(false);
        }
    });
});

describe("client role presets", () => {
    it("stay inside what a restricted member can hold", () => {
        for (const p of CLIENT_ROLE_PRESETS) {
            expect(p.permissions & ~SCOPE_ACTION_PERMISSIONS).toBe(0);
            expect(roleScopeSafe(p.permissions, p.acts_in_scope)).toBe(true);
            expect(presetForRole({ name: p.name })).toBe(p);
        }
    });

    it("go from reading to replying to running campaigns", () => {
        const [view, reply, campaigns] = CLIENT_ROLE_PRESETS;
        expect(view.acts_in_scope).toBe(false);
        expect(reply.acts_in_scope && (reply.permissions & PERMISSION_BITS.MANAGE_CAMPAIGNS) === 0).toBe(true);
        expect(campaigns.permissions & (PERMISSION_BITS.MANAGE_CAMPAIGNS | PERMISSION_BITS.SEND_CAMPAIGNS)).toBe(
            PERMISSION_BITS.MANAGE_CAMPAIGNS | PERMISSION_BITS.SEND_CAMPAIGNS,
        );
    });

    it("reads a workspace role as not client-safe", () => {
        expect(roleScopeSafe(PERMISSION_BITS.MANAGE_TEAM | PERMISSION_BITS.VIEW_CAMPAIGNS, true)).toBe(false);
        expect(roleScopeSafe(PERMISSION_BITS.MANAGE_CAMPAIGNS, false)).toBe(false);
    });

    it("lets a client set only steps that stay inside the campaign", () => {
        expect([...SCOPE_STEP_ACTIONS].sort()).toEqual(["end", "wait"]);
    });
});
