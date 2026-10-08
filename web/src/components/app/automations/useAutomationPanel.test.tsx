import { act, renderHook } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import useAutomationPanel from "./useAutomationPanel";

function Owner({ children }: { children: ReactNode }) {
    const value = { user: { id: "user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}

describe("automation history browsing", () => {
    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("restores history after remount but remembers closing or selecting an editor node", () => {
        const first = renderHook(() => useAutomationPanel("one"), { wrapper: Owner });
        act(() => first.result.current.toggleHistory());
        expect(first.result.current.panel).toBe("history");
        first.unmount();
        const restored = renderHook(() => useAutomationPanel("one"), { wrapper: Owner });
        expect(restored.result.current.panel).toBe("history");
        act(() => restored.result.current.closePanel());
        restored.unmount();
        const closed = renderHook(() => useAutomationPanel("one"), { wrapper: Owner });
        expect(closed.result.current.panel).toBeNull();
    });

    it("keeps test panels transient and closes remembered history when testing", () => {
        const first = renderHook(() => useAutomationPanel("one"), { wrapper: Owner });
        act(() => first.result.current.toggleHistory());
        act(() => first.result.current.showTest());
        expect(first.result.current.panel).toBe("test");
        expect([...Array(sessionStorage.length)].map((_, i) => sessionStorage.getItem(sessionStorage.key(i)!))).toEqual([JSON.stringify({ value: false })]);
        first.unmount();
        const restored = renderHook(() => useAutomationPanel("one"), { wrapper: Owner });
        expect(restored.result.current.panel).toBeNull();
        act(() => restored.result.current.toggleTest());
        expect(restored.result.current.panel).toBe("test");
        act(() => restored.result.current.toggleHistory());
        expect(restored.result.current.panel).toBe("history");
        act(() => restored.result.current.toggleHistory());
        expect(restored.result.current.panel).toBeNull();
    });

    it("isolates history across resource and workspace switches", () => {
        const page = renderHook(({ id }) => useAutomationPanel(id), { wrapper: Owner, initialProps: { id: "one" } });
        act(() => page.result.current.toggleHistory());
        page.rerender({ id: "two" });
        expect(page.result.current.panel).toBeNull();
        page.rerender({ id: "one" });
        expect(page.result.current.panel).toBe("history");
        act(() => useAppStore.setState({ currentOrganization: { id: "another", name: "Another", role: "owner" } }));
        expect(page.result.current.panel).toBeNull();
        act(() => useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } }));
        expect(page.result.current.panel).toBe("history");
    });
});
