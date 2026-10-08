import { fireEvent, render, screen } from "@testing-library/react";
import { useState, type ComponentProps, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import type { IntegrationCatalogEntry } from "@/lib/api/models/app/integrations/Integration";
import SearchBox from "./SearchBox";
import { builtinItem, readFilters, writeFilters } from "./model";

function Owner({ children }: { children: ReactNode }) {
    const value = { user: { id: "user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}

const props = { items: [], onOpenItem: vi.fn(), onOpenCategory: vi.fn(), onSearchAll: vi.fn() };

describe("integration search persistence", () => {
    beforeEach(() => {
        sessionStorage.clear();
        vi.clearAllMocks();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("restores suggestion search, isolates routes and persists an explicit clear", () => {
        const page = render(<SearchBox {...props} browseKey="home" />, { wrapper: Owner });
        fireEvent.change(screen.getByPlaceholderText("Search apps"), { target: { value: "Slack" } });
        page.unmount();
        const refreshed = render(<SearchBox {...props} browseKey="home" />, { wrapper: Owner });
        expect(screen.getByPlaceholderText("Search apps")).toHaveValue("Slack");
        expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
        refreshed.rerender(<SearchBox {...props} browseKey="app/slack" />);
        expect(screen.getByPlaceholderText("Search apps")).toHaveValue("");
        fireEvent.change(screen.getByPlaceholderText("Search apps"), { target: { value: "calendar" } });
        refreshed.rerender(<SearchBox {...props} browseKey="home" />);
        expect(screen.getByPlaceholderText("Search apps")).toHaveValue("Slack");
        fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
        refreshed.unmount();
        render(<SearchBox {...props} browseKey="home" />, { wrapper: Owner });
        expect(screen.getByPlaceholderText("Search apps")).toHaveValue("");
    });

    it("clears the stored suggestion text on selection without persisting its dropdown", () => {
        const slack = builtinItem({ provider: "slack", name: "Slack", tagline: "Team chat", category: "notifications", auth_method: "oauth", configured: true } as IntegrationCatalogEntry);
        const page = render(<SearchBox {...props} items={[slack]} browseKey="home" />, { wrapper: Owner });
        fireEvent.change(screen.getByPlaceholderText("Search apps"), { target: { value: "Slack" } });
        fireEvent.click(screen.getByRole("option", { name: /Team chat/ }));
        expect(props.onOpenItem).toHaveBeenCalledWith(slack);
        page.unmount();
        render(<SearchBox {...props} items={[slack]} browseKey="home" />, { wrapper: Owner });
        expect(screen.getByPlaceholderText("Search apps")).toHaveValue("");
        expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    });

    it("leaves URL filters authoritative and removes q on explicit clear", () => {
        sessionStorage.setItem("warmbly:browse:v1:user:workspace:integrations.store.all.search", JSON.stringify({ value: "stale session" }));
        function URLSearch({ query }: { query: string }) {
            const [filters, setFilters] = useState(() => readFilters(new URLSearchParams(query)));
            const url = writeFilters(filters);
            return <>
                <SearchBox {...props} browseKey="all" filter={{ value: filters.q, onChange: (q) => setFilters((old) => ({ ...old, q })) }} />
                <output data-testid="url">{url.toString()}</output>
            </>;
        }
        const linked = render(<URLSearch query="q=deep+link&view=list" />, { wrapper: Owner });
        expect(screen.getByPlaceholderText("Search apps")).toHaveValue("deep link");
        fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
        expect(screen.getByTestId("url")).toHaveTextContent("view=list");
        expect(screen.getByTestId("url").textContent).not.toContain("q=");
        linked.unmount();
        render(<URLSearch query="view=list" />, { wrapper: Owner });
        expect(screen.getByPlaceholderText("Search apps")).toHaveValue("");
    });
});
