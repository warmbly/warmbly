import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import FormsPage from "./page";

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => vi.fn() }));
vi.mock("@/hooks/usePermission", () => ({ usePermission: () => true, useWriteGuard: () => ({ guard: (fn: () => void) => fn }) }));
vi.mock("@/lib/api/hooks/app/forms", () => ({
    useForms: () => ({ data: [
        { id: "one", name: "Signup", status: "published", category_ids: ["label"], created_at: new Date(), updated_at: new Date(), views_count: 1, starts_count: 1, submissions_count: 1 },
        { id: "two", name: "Contact", status: "draft", category_ids: [], created_at: new Date(), updated_at: new Date(), views_count: 0, starts_count: 0, submissions_count: 0 },
    ] }),
    useCreateForm: () => ({}), useUpdateForm: () => ({}), useDeleteForm: () => ({}),
}));
vi.mock("@/components/ui/select-menu", () => ({
    SelectMenu: ({ value, onChange, options, "aria-label": label }: { value: string; onChange: (v: string) => void; options: { value: string; label: string }[]; "aria-label": string }) =>
        <select aria-label={label} value={value} onChange={(e) => onChange(e.target.value)}>{options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</select>,
}));

function Owner({ children, categories = [{ id: "label", title: "Leads" }] }: { children: ReactNode; categories?: { id: string; title: string }[] }) {
    const value = { user: { id: "user", categories } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}
const key = (field: string) => `warmbly:browse:v1:user:workspace:forms.list.${field}`;

describe("forms list browsing", () => {
    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("restores search, status, label and sorting but not row selections, and remembers clearing", () => {
        const page = render(<FormsPage />, { wrapper: Owner });
        fireEvent.click(screen.getByRole("button", { name: "Name" }));
        fireEvent.change(screen.getByPlaceholderText("Search forms…"), { target: { value: "Signup" } });
        fireEvent.click(screen.getByRole("button", { name: /Published/ }));
        fireEvent.change(screen.getByLabelText("Filter by label"), { target: { value: "label" } });
        fireEvent.click(screen.getAllByRole("checkbox")[0]);
        expect(screen.getByText("1 selected")).toBeInTheDocument();
        page.unmount();
        const refreshed = render(<FormsPage />, { wrapper: Owner });
        expect(screen.getByPlaceholderText("Search forms…")).toHaveValue("Signup");
        expect(screen.getByLabelText("Filter by label")).toHaveValue("label");
        expect(screen.getByRole("button", { name: /Published/ })).toHaveClass("bg-white");
        expect(JSON.parse(sessionStorage.getItem(key("sort"))!)).toEqual({ value: { key: "name", dir: 1 } });
        expect(screen.queryByText(/selected$/)).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
        fireEvent.click(screen.getByRole("button", { name: /^All/ }));
        fireEvent.change(screen.getByLabelText("Filter by label"), { target: { value: "" } });
        refreshed.unmount();
        render(<FormsPage />, { wrapper: Owner });
        expect(screen.getByPlaceholderText("Search forms…")).toHaveValue("");
        expect(screen.getByLabelText("Filter by label")).toHaveValue("");
        expect(JSON.parse(sessionStorage.getItem(key("status"))!)).toEqual({ value: "all" });
    });

    it("retains a removable label when profile metadata is absent instead of resetting it", () => {
        sessionStorage.setItem(key("category"), JSON.stringify({ value: "label" }));
        const page = render(<Owner categories={[]}><FormsPage /></Owner>);
        expect(screen.getByLabelText("Filter by label")).toHaveValue("label");
        expect(screen.getByRole("option", { name: "Unavailable label" })).toBeInTheDocument();
        page.rerender(<Owner><FormsPage /></Owner>);
        expect(screen.getByLabelText("Filter by label")).toHaveValue("label");
        expect(screen.getByRole("option", { name: "Leads" })).toBeInTheDocument();
        page.rerender(<Owner categories={[]}><FormsPage /></Owner>);
        fireEvent.change(screen.getByLabelText("Filter by label"), { target: { value: "" } });
        expect(JSON.parse(sessionStorage.getItem(key("category"))!)).toEqual({ value: "" });
    });

    it("rejects a stale sort key before sorting live rows", () => {
        sessionStorage.setItem(key("sort"), JSON.stringify({ value: { key: "__proto__", dir: 1 } }));
        render(<FormsPage />, { wrapper: Owner });
        expect(screen.getByText("Signup")).toBeInTheDocument();
        expect(screen.getByText("Contact")).toBeInTheDocument();
        expect(JSON.parse(sessionStorage.getItem(key("sort"))!)).toEqual({ value: { key: "created", dir: -1 } });
    });
});
