import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import type Form from "@/lib/api/models/app/forms/Form";
import SubmissionsTab from "./SubmissionsTab";

vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => ({ show: vi.fn() }) }));
vi.mock("@/hooks/usePermission", () => ({ useWriteGuard: () => ({ guard: (fn: () => void) => fn }) }));
vi.mock("@/lib/api/hooks/app/forms", () => ({
    useFormSubmissions: () => ({ data: { data: [{ id: "response", contact_name: "Alice", data: {}, created_at: new Date(), triage: "good" }] } }),
    useFormStats: () => ({ data: { identified: [] } }),
    useDeleteFormSubmission: () => ({}),
}));

function Owner({ children }: { children: ReactNode }) {
    const value = { user: { id: "user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}
const form = (id: string, triage = true) => ({ id, fields: [], status: "published", triage_enabled: triage }) as unknown as Form;

describe("form submission browsing", () => {
    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("restores resource-scoped search and keeps a removable Junk filter when triage is disabled", () => {
        const page = render(<SubmissionsTab form={form("one")} />, { wrapper: Owner });
        fireEvent.change(screen.getByPlaceholderText("Search responses…"), { target: { value: "Alice" } });
        fireEvent.click(screen.getByRole("button", { name: /Junk/ }));
        page.unmount();
        const restored = render(<SubmissionsTab form={form("one", false)} />, { wrapper: Owner });
        expect(screen.getByPlaceholderText("Search responses…")).toHaveValue("Alice");
        expect(screen.getByRole("button", { name: /Junk/ })).toHaveClass("bg-white");
        restored.rerender(<SubmissionsTab form={form("two", false)} />);
        expect(screen.getByPlaceholderText("Search responses…")).toHaveValue("");
        expect(screen.queryByRole("button", { name: /Junk/ })).not.toBeInTheDocument();
        restored.rerender(<SubmissionsTab form={form("one", false)} />);
        expect(screen.getByPlaceholderText("Search responses…")).toHaveValue("Alice");
        fireEvent.click(screen.getByRole("button", { name: /^All/ }));
        fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
        restored.unmount();
        render(<SubmissionsTab form={form("one", false)} />, { wrapper: Owner });
        expect(screen.getByPlaceholderText("Search responses…")).toHaveValue("");
        expect(screen.queryByRole("button", { name: /Junk/ })).not.toBeInTheDocument();
    });
});
