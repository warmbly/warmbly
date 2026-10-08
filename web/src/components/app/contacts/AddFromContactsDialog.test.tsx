import { fireEvent, render, screen } from "@testing-library/react";
import { type ComponentProps, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import AddFromContactsDialog from "./AddFromContactsDialog";

const searchContacts = vi.hoisted(() => vi.fn((_options: unknown) => ({ data: { pages: [] }, isLoading: false })));
vi.mock("@/lib/api/hooks/app/contacts/useSearchContacts", () => ({ default: searchContacts }));
vi.mock("@/lib/api/hooks/app/contacts/useUpdateContactsBulk", () => ({ default: () => ({ isPending: false }) }));
vi.mock("@/lib/api/hooks/app/segments", () => ({ useSetSegmentMembers: () => ({ isPending: false }) }));
vi.mock("@/hooks/useAlert", () => ({ default: () => ({ error: vi.fn() }) }));
vi.mock("@/components/app/contacts/CategoryPicker", () => ({ default: ({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) =>
    <button onClick={() => onChange(value.length ? [] : ["label"])}>{value.join(",") || "Choose labels"}</button> }));

function Owner({ children }: { children: ReactNode }) {
    const value = { user: { id: "member" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}
const onClose = vi.fn();
const query = () => screen.getByPlaceholderText("Search name, email, company…");

describe("add-from contacts browsing filters", () => {
    beforeEach(() => {
        sessionStorage.clear();
        searchContacts.mockClear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });
    it("preserves filters through closing, initial closed mount, and refresh, with the restored first API query", () => {
        const page = render(<AddFromContactsDialog open campaign={{ id: "a", name: "A" }} onClose={onClose} />, { wrapper: Owner });
        fireEvent.change(query(), { target: { value: "alice" } });
        fireEvent.click(screen.getByText("Choose labels"));
        page.rerender(<AddFromContactsDialog open={false} campaign={{ id: "a", name: "A" }} onClose={onClose} />);
        page.unmount();
        searchContacts.mockClear();
        const refreshed = render(<AddFromContactsDialog open={false} campaign={{ id: "a", name: "A" }} onClose={onClose} />, { wrapper: Owner });
        expect(searchContacts.mock.calls[0][0]).toMatchObject({ options: { query: "alice", category_ids: ["label"] } });
        refreshed.rerender(<AddFromContactsDialog open campaign={{ id: "a", name: "A" }} onClose={onClose} />);
        expect(query()).toHaveValue("alice");
        expect(screen.getByText("label")).toBeInTheDocument();
        refreshed.rerender(<AddFromContactsDialog open campaign={{ id: "b", name: "B" }} onClose={onClose} />);
        expect(query()).toHaveValue("");
        expect(screen.getByText("Choose labels")).toBeInTheDocument();
        refreshed.rerender(<AddFromContactsDialog open target={{ kind: "segment", segment: { id: "a", name: "A" } }} onClose={onClose} />);
        expect(query()).toHaveValue("");
        refreshed.rerender(<AddFromContactsDialog open campaign={{ id: "a", name: "A" }} onClose={onClose} />);
        expect(query()).toHaveValue("alice");
    });
});
