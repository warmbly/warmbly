import { fireEvent, render, screen } from "@testing-library/react";
import { type ComponentProps, type ReactNode, useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import useContactBrowseState from "./useContactBrowseState";
import FilterBar from "./FilterBar";

vi.mock("@/lib/api/hooks/app/campaigns/useCampaigns", () => ({ default: () => ({ campaigns: [], data: { pages: [] } }) }));
vi.mock("@/lib/api/hooks/app/contacts/useCustomFieldKeys", () => ({ default: () => ({ data: [] }) }));
vi.mock("@/lib/api/hooks/app/segments", () => ({ useSegments: () => ({ data: [] }) }));

function Owner({ children }: { children: ReactNode }) {
    const value = { user: { id: "member", categories: [] } } as unknown as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}
function List({ resource = "contacts:all" }: { resource?: string }) {
    const [filters, setFilters] = useContactBrowseState({ initialSort: () => ({ sort_by: "created_at", reverse: false }) });
    const [resetToken, setResetToken] = useState(0);
    return <>
        <button onClick={() => setResetToken((v) => v + 1)}>Reset view</button>
        <FilterBar browseName={resource} filters={filters} setFilters={setFilters} total={0} loading={false} resetToken={resetToken} />
    </>;
}

describe("blank filter pill persistence", () => {
    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });
    it("keeps blank pills after remount, only clears on an actual reset, and isolates list resources", () => {
        const page = render(<List />, { wrapper: Owner });
        fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
        fireEvent.click(screen.getByText("Address verification"));
        expect(screen.getByRole("button", { name: "Remove Verification filter" })).toBeInTheDocument();
        page.unmount();
        const refreshed = render(<List />, { wrapper: Owner });
        expect(screen.getByRole("button", { name: "Remove Verification filter" })).toBeInTheDocument();
        refreshed.rerender(<List resource="contacts:segments:s:members" />);
        expect(screen.queryByRole("button", { name: "Remove Verification filter" })).not.toBeInTheDocument();
        refreshed.rerender(<List />);
        expect(screen.getByRole("button", { name: "Remove Verification filter" })).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Reset view" }));
        expect(screen.queryByRole("button", { name: "Remove Verification filter" })).not.toBeInTheDocument();
        refreshed.unmount();
        render(<List />, { wrapper: Owner });
        expect(screen.queryByRole("button", { name: "Remove Verification filter" })).not.toBeInTheDocument();
    });
});
