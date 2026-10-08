import { act, fireEvent, render, screen } from "@testing-library/react";
import { useState, type ComponentProps, type ReactNode } from "react";
import { beforeEach, describe, expect, it } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import PickTable, { type PickItem } from "../import/PickTable";
import { PerDomainList, type PerDomainRow } from "./PerDomainList";

function Owner({ children }: { children: ReactNode }) {
    const profile = { user: { id: "account-user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={profile}>{children}</UserContext.Provider>;
}

const items: PickItem[] = [
    { id: "a", email: "alice@a.test", name: "Alice", connected: true, group: "A" },
    { id: "b", email: "bob@b.test", name: "Bob", connected: false, group: "B" },
];

function Picker({ scope = "emails.import.vendor.a.pick", loading = false }: { scope?: string; loading?: boolean }) {
    const [selected, setSelected] = useState<Set<string>>(new Set());
    return <Owner><PickTable browseScope={scope} items={loading ? undefined : items} loading={loading} selected={selected} setSelected={setSelected} noun={{ one: "mailbox", many: "mailboxes" }} /></Owner>;
}

const rows: PerDomainRow[] = Array.from({ length: 80 }, (_, i) => ({
    domain: `domain-${i}.test`, canTrack: false, track: false, host: "", redirect: false, url: "", urlPlaceholder: "", custom: false,
}));
function DomainList({ scope = "emails.domains.bulkSetup.list" }: { scope?: string }) {
    return <Owner><PerDomainList browseScope={scope} rows={rows} onChange={() => {}} onReset={() => {}} /></Owner>;
}

describe("nested account browsing persistence", () => {
    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("restores picker search, filter and group after loading/remount without restoring selected rows", () => {
        const picker = render(<Picker />);
        fireEvent.change(screen.getByPlaceholderText("Search mailboxes"), { target: { value: "alice" } });
        fireEvent.click(screen.getByRole("button", { name: /^Connected/ }));
        fireEvent.click(screen.getByRole("button", { name: /^A\s?1$/ }));
        fireEvent.click(screen.getByRole("button", { name: /alice@a.test/ }));
        picker.unmount();
        const refreshed = render(<Picker loading />);
        refreshed.rerender(<Picker />);
        expect(screen.getByPlaceholderText("Search mailboxes")).toHaveValue("alice");
        expect(screen.getByRole("button", { name: /^Connected/ })).toHaveClass("bg-sky-50");
        expect(screen.getByRole("button", { name: /^A\s?1$/ })).toHaveAttribute("aria-pressed", "true");
        expect(screen.getByRole("button", { name: /alice@a.test/ })).toHaveAttribute("aria-pressed", "false");
        expect(screen.queryByText("bob@b.test")).not.toBeInTheDocument();
    });

    it("isolates connection scopes and persists explicit picker clearing", () => {
        const picker = render(<Picker />);
        fireEvent.change(screen.getByPlaceholderText("Search mailboxes"), { target: { value: "alice" } });
        picker.rerender(<Picker scope="emails.import.vendor.b.pick" />);
        expect(screen.getByPlaceholderText("Search mailboxes")).toHaveValue("");
        picker.rerender(<Picker />);
        expect(screen.getByPlaceholderText("Search mailboxes")).toHaveValue("alice");
        fireEvent.change(screen.getByPlaceholderText("Search mailboxes"), { target: { value: "" } });
        picker.unmount();
        render(<Picker />);
        expect(screen.getByPlaceholderText("Search mailboxes")).toHaveValue("");
    });

    it("restores expanded domain lists, search and show-more without persisting editor values", () => {
        const list = render(<DomainList />);
        fireEvent.click(screen.getByRole("button", { name: /^Change per domain/ }));
        fireEvent.click(screen.getByRole("button", { name: /Show more/ }));
        fireEvent.change(screen.getByPlaceholderText("Find a domain…"), { target: { value: "domain" } });
        list.unmount();
        const refreshed = render(<DomainList />);
        expect(screen.getByRole("button", { name: /^Change per domain/ })).toHaveAttribute("aria-expanded", "true");
        expect(screen.getByPlaceholderText("Find a domain…")).toHaveValue("domain");
        expect(screen.getByText("domain-69.test")).toBeInTheDocument();
        expect(screen.queryByText("domain-70.test")).not.toBeInTheDocument();
        refreshed.rerender(<DomainList scope="emails.import.file.domainChoices" />);
        expect(screen.getByRole("button", { name: /^Change per domain/ })).toHaveAttribute("aria-expanded", "false");
        refreshed.rerender(<DomainList />);
        expect(screen.getByPlaceholderText("Find a domain…")).toHaveValue("domain");
        act(() => useAppStore.setState({ currentOrganization: { id: "other", name: "Other", role: "owner" } }));
        expect(screen.getByRole("button", { name: /^Change per domain/ })).toHaveAttribute("aria-expanded", "false");
    });
});
