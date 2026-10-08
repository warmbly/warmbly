import { act, fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import type Form from "@/lib/api/models/app/forms/Form";
import ShareTab from "./ShareTab";

const contacts = vi.hoisted(() => vi.fn(() => ({ contacts: [], isFetching: false })));
vi.mock("@/lib/api/hooks/app/contacts/useSearchContacts", () => ({ default: contacts }));
vi.mock("./FormsDomainCard", () => ({ default: () => null }));

function Owner({ children }: { children: ReactNode }) {
    const value = { user: { id: "user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}
const form = (id: string) => ({ id, public_id: id, name: id, status: "published", allowed_domains: [] }) as unknown as Form;
const search = () => screen.getByPlaceholderText("Search contacts by name or email…");

describe("personalized form-link search", () => {
    beforeEach(() => {
        vi.useFakeTimers();
        vi.clearAllMocks();
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });
    afterEach(() => vi.useRealTimers());

    it("restores the canonical search and initializes the first API query from it", () => {
        const page = render(<ShareTab form={form("one")} baseUrl="https://forms.example" />, { wrapper: Owner });
        fireEvent.change(search(), { target: { value: "Alice" } });
        act(() => vi.advanceTimersByTime(300));
        page.unmount();
        contacts.mockClear();
        const refreshed = render(<ShareTab form={form("one")} baseUrl="https://forms.example" />, { wrapper: Owner });
        expect(search()).toHaveValue("Alice");
        expect(contacts).toHaveBeenCalledWith(expect.objectContaining({ enabled: true, options: expect.objectContaining({ query: "Alice" }) }));
        fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
        refreshed.unmount();
        render(<ShareTab form={form("one")} baseUrl="https://forms.example" />, { wrapper: Owner });
        expect(search()).toHaveValue("");
    });

    it("does not issue the previous resource or workspace's debounced search after switching", () => {
        const page = render(<ShareTab form={form("one")} baseUrl="https://forms.example" />, { wrapper: Owner });
        fireEvent.change(search(), { target: { value: "Alice" } });
        act(() => vi.advanceTimersByTime(300));
        contacts.mockClear();
        page.rerender(<ShareTab form={form("two")} baseUrl="https://forms.example" />);
        expect(search()).toHaveValue("");
        expect(contacts).not.toHaveBeenCalledWith(expect.objectContaining({ options: expect.objectContaining({ query: "Alice" }) }));
        page.rerender(<ShareTab form={form("one")} baseUrl="https://forms.example" />);
        expect(search()).toHaveValue("Alice");
        contacts.mockClear();
        act(() => useAppStore.setState({ currentOrganization: { id: "another", name: "Another", role: "owner" } }));
        expect(search()).toHaveValue("");
        expect(contacts).not.toHaveBeenCalledWith(expect.objectContaining({ options: expect.objectContaining({ query: "Alice" }) }));
    });
});
