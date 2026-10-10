import { act } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import MailboxesPage from "./MailboxesPage";

const calls = vi.hoisted(() => ({ search: vi.fn(), queries: vi.fn() }));
vi.mock("@/lib/api/client/admin/mailboxes", () => ({ searchMailboxes: calls.search }));
vi.mock("@/lib/api/client/admin/fleetNodes", () => ({ listFleetNodes: vi.fn() }));
vi.mock("@/components/data/DataTable", () => ({ DataTable: () => <div>Mailbox results</div> }));
vi.mock("@tanstack/react-query", () => ({ keepPreviousData: vi.fn(), useQuery: (options: { queryKey: string[]; queryFn: () => unknown }) => {
    calls.queries(options);
    if (options.queryKey[1] === "mailboxes") options.queryFn();
    return { data: { data: [], pagination: {} }, isLoading: false };
} }));
afterEach(() => { vi.clearAllMocks(); vi.unstubAllGlobals(); });

it("looks up an exact mailbox across inactive/moved assignments without old filters or placeholder rows", async () => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    const mailbox = "33333333-3333-4333-8333-333333333333";
    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);
    try {
        await act(async () => root.render(<MemoryRouter initialEntries={[`/mailboxes?mailbox_id=${mailbox}&worker=old-worker&q=old-search&org=old-org`]}><MailboxesPage /></MemoryRouter>));
        expect(calls.search).toHaveBeenCalledWith({ mailbox_id: mailbox, status: "all", limit: 50 });
        expect(calls.queries.mock.calls.filter(([options]) => options.queryKey[1] === "mailboxes").every(([options]) => options.placeholderData === undefined)).toBe(true);
        expect(host.textContent).toContain("Exact identity lookup");
        expect(host.textContent).toContain(mailbox);
        expect(host.querySelector("input")).toBeNull();
    } finally {
        await act(async () => root.unmount());
        host.remove();
    }
});
