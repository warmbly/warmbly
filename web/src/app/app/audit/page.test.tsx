import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import AuditPage from "./page";

const query = vi.hoisted(() => vi.fn(() => ({ data: { data: [], pagination: {} } })));
vi.mock("@/hooks/useFeatureAccess", () => ({ default: () => ({ loading: false, canManage: true }) }));
vi.mock("@/lib/api/hooks/app/audit/useAuditLogs", () => ({ default: query }));

function Owner({ children }: { children: ReactNode }) {
    const value = { user: { id: "user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}
const key = (field: string) => `warmbly:browse:v1:user:workspace:audit.list.${field}`;

describe("audit browsing reset", () => {
    beforeEach(() => {
        sessionStorage.clear();
        vi.clearAllMocks();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("restores search and preserves the intentional reset that clears server filters but not search", () => {
        const page = render(<AuditPage />, { wrapper: Owner });
        fireEvent.change(screen.getAllByRole("textbox")[0], { target: { value: "Alice" } });
        page.unmount();
        sessionStorage.setItem(key("action"), JSON.stringify({ value: "create" }));
        sessionStorage.setItem(key("entity-type"), JSON.stringify({ value: "contact" }));
        sessionStorage.setItem(key("date"), JSON.stringify({ value: "2026-10-08" }));
        const restored = render(<AuditPage />, { wrapper: Owner });
        expect(screen.getAllByRole("textbox")[0]).toHaveValue("Alice");
        expect(query).toHaveBeenCalledWith({ action: "create", entity_type: "contact", date: "2026-10-08", cursor: undefined, limit: 50 });
        fireEvent.click(screen.getAllByTitle("Clear filters")[0]);
        restored.unmount();
        query.mockClear();
        render(<AuditPage />, { wrapper: Owner });
        expect(screen.getAllByRole("textbox")[0]).toHaveValue("Alice");
        expect(query).toHaveBeenCalledWith({ action: undefined, entity_type: undefined, date: undefined, cursor: undefined, limit: 50 });
    });
});
