import { beforeEach, describe, expect, it } from "vitest";
import { defaultClientRole, overlayClientView, readClientView } from "./clientView";
import { ALL_PERMISSIONS, PERMISSION_BITS } from "./permissions";
import { useAppStore } from "@/stores";

const VIEW = PERMISSION_BITS.VIEW_CAMPAIGNS | PERMISSION_BITS.VIEW_CONTACTS;
const SEND = PERMISSION_BITS.SEND_CAMPAIGNS;

describe("overlayClientView", () => {
    it("narrows the member to the role", () => {
        const org = overlayClientView({ id: "a", role: "admin", permissions: VIEW | SEND }, { orgId: "a", roleId: "viewer", roleName: "Viewer", permissions: VIEW });
        expect(org.role).toBe("viewer");
        expect(org.permissions).toBe(VIEW);
    });

    it("never shows more than the member holds", () => {
        const org = overlayClientView({ id: "a", role: "member", permissions: VIEW }, { orgId: "a", roleId: "r", roleName: "Admin", permissions: VIEW | SEND });
        expect(org.permissions).toBe(VIEW);
    });

    it("keeps a non-owner a non-owner in the owner view", () => {
        const org = overlayClientView({ id: "a", role: "admin", permissions: VIEW }, { orgId: "a", roleId: "owner", roleName: "Owner", permissions: ALL_PERMISSIONS });
        expect(org.role).toBe("admin");
        expect(org.permissions).toBe(VIEW);
    });

    it("leaves another workspace alone", () => {
        const member = { id: "b", role: "owner" };
        expect(overlayClientView(member, { orgId: "a", roleId: "viewer", roleName: "Viewer", permissions: 0 })).toBe(member);
    });
});

describe("defaultClientRole", () => {
    it("picks the role most members hold, then the narrower one", () => {
        const roles = [
            { id: "admin", member_count: 2, permissions: VIEW | SEND },
            { id: "viewer", member_count: 2, permissions: VIEW },
            { id: "solo", member_count: 1, permissions: 0 },
        ];
        expect(defaultClientRole(roles)?.id).toBe("viewer");
    });
});

describe("client view in the store", () => {
    const a = { id: "a", name: "A", role: "admin", permissions: VIEW | SEND };
    const b = { id: "b", name: "B", role: "owner" };

    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.getState().setClientView(null);
        useAppStore.getState().setCurrentOrganization(a);
    });

    it("narrows the current workspace and survives a refreshed membership list", () => {
        useAppStore.getState().setClientView({ orgId: "a", roleId: "viewer", roleName: "Viewer", permissions: VIEW });
        expect(useAppStore.getState().currentOrganization?.permissions).toBe(VIEW);
        useAppStore.getState().setOrganizations([{ ...a, name: "A renamed" }, b]);
        expect(useAppStore.getState().currentOrganization).toMatchObject({ name: "A renamed", permissions: VIEW });
        expect(useAppStore.getState().memberOrganization?.permissions).toBe(VIEW | SEND);
        expect(readClientView()?.orgId).toBe("a");
    });

    it("ends when the tab moves to another workspace", () => {
        useAppStore.getState().setClientView({ orgId: "a", roleId: "viewer", roleName: "Viewer", permissions: VIEW });
        useAppStore.getState().setCurrentOrganization(b);
        expect(useAppStore.getState().clientView).toBeNull();
        expect(useAppStore.getState().currentOrganization).toEqual(b);
        expect(readClientView()).toBeNull();
    });

    it("ignores a view for a workspace that is not open", () => {
        useAppStore.getState().setClientView({ orgId: "b", roleId: "viewer", roleName: "Viewer", permissions: VIEW });
        expect(useAppStore.getState().clientView).toBeNull();
        expect(useAppStore.getState().currentOrganization).toEqual(a);
    });
});

describe("client view identity", () => {
    it("keeps the same object when nothing changed, so effects that depend on it settle", () => {
        const a = { id: "a", name: "A", role: "admin", permissions: VIEW | SEND };
        useAppStore.getState().setCurrentOrganization(a);
        useAppStore.getState().setClientView({ orgId: "a", roleId: "viewer", roleName: "Viewer", permissions: VIEW });
        const shown = useAppStore.getState().currentOrganization;
        useAppStore.getState().setOrganizations([a]);
        useAppStore.getState().setOrganizations([a]);
        expect(useAppStore.getState().currentOrganization).toBe(shown);
    });
});
