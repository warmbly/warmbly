import { act, fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import TemplatesPage from "./page";

const query = vi.hoisted(() => vi.fn(() => ({ data: [] })));
vi.mock("@/lib/api/hooks/app/templates/useTemplates", () => ({ default: query }));
vi.mock("@/lib/api/hooks/app/templates/useCreateTemplate", () => ({ default: () => ({}) }));
vi.mock("@/lib/api/hooks/app/templates/useUpdateTemplate", () => ({ default: () => ({}) }));
vi.mock("@/lib/api/hooks/app/templates/useReorderTemplates", () => ({ default: () => ({}) }));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => vi.fn() }));
vi.mock("@/hooks/PresenceProvider", () => ({ usePresenceResource: () => {} }));
vi.mock("@/components/app/presence/GlobalCursors", () => ({ useSuppressGlobalCursors: () => {} }));

function Owner({ children }: { children: ReactNode }) {
    const value = { user: { id: "user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}
const search = () => screen.getByPlaceholderText("Search by name or subject…");

describe("template search browsing", () => {
    beforeEach(() => {
        vi.useFakeTimers();
        vi.clearAllMocks();
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });
    afterEach(() => vi.useRealTimers());

    it("restores the initial debounced API query and remembers explicit search clearing", () => {
        const page = render(<TemplatesPage />, { wrapper: Owner });
        fireEvent.change(search(), { target: { value: "Welcome" } });
        act(() => vi.advanceTimersByTime(200));
        page.unmount();
        query.mockClear();
        const refreshed = render(<TemplatesPage />, { wrapper: Owner });
        expect(search()).toHaveValue("Welcome");
        expect(query).toHaveBeenNthCalledWith(1, "Welcome");
        fireEvent.click(screen.getByLabelText("Clear search"));
        refreshed.unmount();
        render(<TemplatesPage />, { wrapper: Owner });
        expect(search()).toHaveValue("");
    });

    it("does not query the previous workspace's debounced search on a scope change", () => {
        sessionStorage.setItem("warmbly:browse:v1:user:workspace:templates.list.search", JSON.stringify({ value: "Welcome" }));
        render(<TemplatesPage />, { wrapper: Owner });
        query.mockClear();
        act(() => useAppStore.setState({ currentOrganization: { id: "another", name: "Another", role: "owner" } }));
        expect(search()).toHaveValue("");
        expect(query).not.toHaveBeenCalledWith("Welcome");
        query.mockClear();
        act(() => useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } }));
        expect(search()).toHaveValue("Welcome");
        expect(query).not.toHaveBeenCalledWith(undefined);
    });
});
