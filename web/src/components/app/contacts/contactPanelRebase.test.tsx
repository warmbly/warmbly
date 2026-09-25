// Issue #415: with the contact 360 panel open on an unsubscribed contact,
// lifting their suppression on the Overview tab re-subscribes them. The list
// query refetches, the panel's `contact` prop comes back with
// `subscribed: true`, and the draft was still sitting on `false` — so the
// panel called itself dirty and closing it asked to discard changes nobody
// made. This mounts the real panel and walks that sequence, and pins the other
// half: an edit the user did make still has to be guarded.

import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type Contact from "@/lib/api/models/app/contacts/Contact";

const requested: { url?: string; data?: unknown }[] = [];
vi.mock("@/lib/api/client/Request", () => ({
    default: (cfg: { url?: string; method?: string; data?: unknown }) => {
        if (cfg?.method === "PATCH") {
            requested.push({ url: cfg.url, data: cfg.data });
            return Promise.resolve({});
        }
        return new Promise(() => {});
    },
}));

const confirmShow = vi.fn();
vi.mock("@/hooks/context/confirm", () => ({
    useConfirm: () => ({ show: confirmShow }),
}));
vi.mock("@/hooks/PresenceProvider", () => ({
    usePresenceResource: () => {},
}));
vi.mock("@/components/app/presence/ResourceViewers", () => ({ default: () => null }));
vi.mock("@/components/app/integrations/BookACallButton", () => ({ default: () => null }));
vi.mock("@/components/app/meetings/NewMeetingDialog", () => ({ default: () => null }));

// The tabs are covered separately; this is about the panel chrome. The Details
// stand-in is the only way into the draft, so it exposes one edit.
vi.mock("./contact-edit/OverviewTab", () => ({ default: () => <div>overview</div> }));
vi.mock("./contact-edit/ActivityTab", () => ({ default: () => <div>activity</div> }));
vi.mock("./contact-edit/NotesTab", () => ({ default: () => <div>notes</div> }));
vi.mock("./contact-edit/ResearchTab", () => ({ default: () => <div>research</div> }));
type CF = { name: string; value: string };
vi.mock("./contact-edit/DetailsTab", () => ({
    default: ({
        setFirstName,
        setCategoryIds,
        customFields,
        setCustomFields,
    }: {
        setFirstName: (v: string) => void;
        setCategoryIds: (v: string[]) => void;
        customFields: CF[];
        setCustomFields: (f: (prev: CF[]) => CF[]) => void;
    }) => (
        <>
            <button type="button" onClick={() => setFirstName("Edited")}>
                rename
            </button>
            <button type="button" onClick={() => setCategoryIds(["cat-2"])}>
                recategorise
            </button>
            <button
                type="button"
                onClick={() => setCustomFields((prev) => [...prev, { name: "", value: "half typed" }])}
            >
                add field
            </button>
            <button type="button" onClick={() => setCustomFields((prev) => prev.filter((f) => f.name !== "industry"))}>
                drop industry
            </button>
            <ul>
                {customFields.map((f, i) => (
                    <li key={i}>{`row ${f.name}=${f.value}`}</li>
                ))}
            </ul>
        </>
    ),
}));

const ContactEdit = (await import("./ContactEdit")).default;

function contact(overrides: Partial<Contact> = {}): Contact {
    return {
        id: "contact-1",
        first_name: "Test",
        last_name: "Demo",
        email: "test.demo@fixture.invalid",
        company: "Acme Freight",
        phone: "",
        custom_fields: {},
        subscribed: false,
        campaigns: [],
        categories: [],
        updated_at: new Date(),
        created_at: new Date(),
        ...overrides,
    } as Contact;
}

function Panel({ contacts }: { contacts: Contact[] }) {
    const [active, setActive] = React.useState("contact-1");
    const client = React.useMemo(
        () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
        [],
    );
    return (
        <QueryClientProvider client={client}>
            <ContactEdit contacts={contacts} active={active} setActive={setActive} initialTab="details" />
        </QueryClientProvider>
    );
}

function clickBackdrop(container: HTMLElement) {
    fireEvent.mouseDown(container.querySelector(".fixed.inset-0") as Element);
}

describe("the contact 360 panel", () => {
    beforeEach(() => {
        confirmShow.mockClear();
        requested.length = 0;
    });

    it("closes without asking when the contact was re-subscribed elsewhere", () => {
        const { rerender, container } = render(<Panel contacts={[contact()]} />);
        expect(screen.getByText("Unsubscribed")).toBeTruthy();

        // The suppression lift lands: the list refetches and the panel is
        // handed a fresh record, subscribed again.
        rerender(<Panel contacts={[contact({ subscribed: true })]} />);
        expect(screen.queryByText("Unsubscribed")).toBeNull();
        expect(screen.queryByText("Unsaved")).toBeNull();

        clickBackdrop(container);
        expect(confirmShow).not.toHaveBeenCalled();
    });

    it("still asks before discarding an edit the user made", () => {
        const { container } = render(<Panel contacts={[contact()]} />);
        fireEvent.click(screen.getByText("rename"));
        expect(screen.getByText("Unsaved")).toBeTruthy();

        clickBackdrop(container);
        expect(confirmShow).toHaveBeenCalledTimes(1);
        expect(confirmShow.mock.calls[0][0]).toContain("Discard unsaved changes?");
    });

    it("keeps the user's edit when the server changes the same field", () => {
        const { rerender, container } = render(<Panel contacts={[contact()]} />);
        fireEvent.click(screen.getByText("rename"));
        expect(screen.getByText("Edited Demo")).toBeTruthy();

        // A teammate renames them meanwhile. The draft wins, and the panel is
        // still dirty because there is something unsaved to lose.
        rerender(<Panel contacts={[contact({ first_name: "Renamed" })]} />);
        expect(screen.getByText("Edited Demo")).toBeTruthy();

        clickBackdrop(container);
        expect(confirmShow).toHaveBeenCalledTimes(1);
    });

    it("adopts an untouched field while a different one is being edited", () => {
        const { rerender } = render(<Panel contacts={[contact()]} />);
        fireEvent.click(screen.getByText("rename"));

        rerender(<Panel contacts={[contact({ subscribed: true })]} />);
        // The name edit survives; the subscription follows the server.
        expect(screen.getByText("Edited Demo")).toBeTruthy();
        expect(screen.queryByText("Unsubscribed")).toBeNull();
    });

    it("asks before closing on a custom-field row that is typed but not named", () => {
        const { container } = render(<Panel contacts={[contact()]} />);
        fireEvent.click(screen.getByText("add field"));
        // Nothing to save: the row has no name to save the value under. Still
        // the user's work, so leaving has to ask.
        expect((screen.getByText("Save changes") as HTMLButtonElement).disabled).toBe(true);
        expect(screen.getByText("Unsaved")).toBeTruthy();

        clickBackdrop(container);
        expect(confirmShow).toHaveBeenCalledTimes(1);
    });

    it("does not throw away a custom-field row that is still being typed", () => {
        const { rerender } = render(<Panel contacts={[contact()]} />);
        fireEvent.click(screen.getByText("add field"));
        expect(screen.getByText("row =half typed")).toBeTruthy();

        // A row with no name yet saves as nothing, so the save-shaped
        // comparison calls the draft untouched. The rebase must not.
        rerender(<Panel contacts={[contact({ company: "Globex" })]} />);
        expect(screen.getByText("row =half typed")).toBeTruthy();
    });
});

describe("saving the contact 360 panel", () => {
    beforeEach(() => {
        confirmShow.mockClear();
        requested.length = 0;
    });

    it("sends only the fields the user changed", async () => {
        render(<Panel contacts={[contact({ categories: [{ id: "cat-1", title: "Agency", color: "#38bdf8" }] })]} />);
        fireEvent.click(screen.getByText("recategorise"));
        fireEvent.click(screen.getByText("Save changes"));

        await waitFor(() => expect(requested.length).toBe(1));
        expect(requested[0].url).toBe("/contacts/contact-1");
        expect(requested[0].data).toEqual({ categories: ["cat-2"] });
    });

    // The save is a diff, so a teammate's field that arrives mid-edit must not
    // be sent as a removal.
    it("keeps a custom field a teammate added while the user edited", async () => {
        const first = contact({ custom_fields: { industry: "Freight" } });
        const { rerender } = render(<Panel contacts={[first]} />);
        fireEvent.click(screen.getByText("drop industry"));
        rerender(<Panel contacts={[contact({ custom_fields: { industry: "Freight", tier: "A" } })]} />);
        fireEvent.click(screen.getByText("Save changes"));

        await waitFor(() => expect(requested.length).toBe(1));
        expect(requested[0].data).toEqual({ custom_fields: { industry: "" } });
    });

    // The server merges custom_fields, so a field that is simply left out is
    // kept; removing one has to send it empty.
    it("sends a removed custom field as empty so the server drops it", async () => {
        render(<Panel contacts={[contact({ custom_fields: { industry: "Freight", tier: "A" } })]} />);
        fireEvent.click(screen.getByText("drop industry"));
        fireEvent.click(screen.getByText("Save changes"));

        await waitFor(() => expect(requested.length).toBe(1));
        expect(requested[0].data).toEqual({ custom_fields: { industry: "" } });
    });
});
