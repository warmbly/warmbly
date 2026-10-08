import React, { type ComponentProps, type ReactNode } from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores";
import { BROWSE_STORAGE_PREFIX } from "@/lib/browseState";
import ComposeHistoryPanel from "./compose/ComposeHistoryPanel";
import MailboxPicker from "./compose/MailboxPicker";
import ContactRecipientField from "./compose/ContactRecipientField";
import TemplatePickerContent from "./TemplatePicker";
import type Template from "@/lib/api/models/app/templates/Template";

const mocks = vi.hoisted(() => ({
  search: vi.fn(() => ({ emails: [], isPending: false, hasNextPage: false })),
  contacts: vi.fn(() => ({ contacts: [], isLoading: false, isFetching: false })),
  categories: [{ id: "label", title: "Sales", color: "#000000" }],
}));
vi.mock("@/lib/api/hooks/app/unibox/useUniboxSearch", () => ({ default: mocks.search }));
vi.mock("@/lib/api/hooks/app/contacts/useSearchContacts", () => ({ default: mocks.contacts }));
vi.mock("@/hooks/context/user", async (original) => ({
  ...await original<Record<string, unknown>>(),
  useUserProfile: () => ({ user: { categories: mocks.categories } }),
}));
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));
vi.mock("framer-motion", async (original) => ({
  ...await original<Record<string, unknown>>(),
  AnimatePresence: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

function Owner({ children }: { children: ReactNode }) {
  return <UserContext.Provider value={{ user: { id: "user" } } as ComponentProps<typeof UserContext.Provider>["value"]}>{children}</UserContext.Provider>;
}
function mount(children: ReactNode) { return render(<Owner>{children}</Owner>); }
const storageKey = (field: string) => `${BROWSE_STORAGE_PREFIX}user:workspace:${field}`;
const stored = (field: string) => JSON.parse(sessionStorage.getItem(storageKey(field))!).value;

beforeEach(() => {
  sessionStorage.clear();
  mocks.search.mockClear();
  mocks.contacts.mockClear();
  mocks.categories = [{ id: "label", title: "Sales", color: "#000000" }];
  useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" }, emails: [], tags: [{ id: "tag", title: "Team", color: "#000000", position: 0, created_at: new Date(), updated_at: new Date() }] });
});
afterEach(cleanup);

describe("nested inbox browsing refresh", () => {
  it("restores the autocomplete label without persisting typed recipient text and re-resolves its title", () => {
    const field = <ContactRecipientField browseKey="compose.to" value={[]} onChange={() => {}} placeholder="Recipient" />;
    const editor = mount(field);
    const input = screen.getByPlaceholderText("Recipient");
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: "Sales" } });
    fireEvent.mouseDown(screen.getByRole("button", { name: /Sales\s*filter by label/ }));
    expect(stored("unibox.recipients.compose.to.autocomplete-category")).toEqual(mocks.categories[0]);
    editor.unmount();
    mocks.categories = [{ id: "label", title: "Renamed", color: "#ffffff" }];
    mount(field);
    expect(screen.getByRole("button", { name: "Clear Renamed filter" })).toBeTruthy();
    expect(screen.getByRole("textbox")).toHaveValue("");
    fireEvent.click(screen.getByRole("button", { name: "Clear Renamed filter" }));
    expect(stored("unibox.recipients.compose.to.autocomplete-category")).toBeNull();
  });
  it("restores history tabs/search per normalized recipient, not whichever panel was last open", () => {
    const panel = mount(<ComposeHistoryPanel address="A@Example.com" />);
    fireEvent.click(screen.getByRole("button", { name: "Sent" }));
    fireEvent.change(screen.getByPlaceholderText("Search subjects…"), { target: { value: "invoice" } });
    panel.unmount();
    const refreshed = mount(<ComposeHistoryPanel address="a@example.com" />);
    expect(screen.getByPlaceholderText("Search subjects…")).toHaveValue("invoice");
    expect(mocks.search).toHaveBeenLastCalledWith(expect.objectContaining({ direction: "sent", query: "invoice" }), expect.any(String), true);
    refreshed.rerender(<Owner><ComposeHistoryPanel address="b@example.com" /></Owner>);
    expect(screen.getByPlaceholderText("Search subjects…")).toHaveValue("");
    expect(mocks.search).toHaveBeenLastCalledWith(expect.objectContaining({ direction: undefined, query: undefined }), expect.any(String), true);
    refreshed.rerender(<Owner><ComposeHistoryPanel address="a@example.com" /></Owner>);
    expect(screen.getByPlaceholderText("Search subjects…")).toHaveValue("invoice");
  });

  it("keeps template search visible when refreshed template counts shrink and honors explicit clear", () => {
    const templates: Template[] = Array.from({ length: 6 }, (_, index) => ({ id: `${index}`, name: `Template ${index}`, subject: "", body_html: "", body_plain: "", organization_id: "workspace", user_id: "user", position: index, created_at: new Date(), updated_at: new Date() }));
    const query = (data: Template[]) => ({ data, isPending: false, isError: false } as ComponentProps<typeof TemplatePickerContent>["query"]);
    const picker = (data: Template[], browseKey = "compose") => <TemplatePickerContent browseKey={browseKey} query={query(data)} onPick={() => {}} onClose={() => {}} />;
    const menu = mount(picker(templates));
    fireEvent.change(screen.getByPlaceholderText("Search templates"), { target: { value: "absent" } });
    menu.unmount();
    const refreshed = mount(picker(templates.slice(0, 1)));
    expect(screen.getByPlaceholderText("Search templates")).toHaveValue("absent");
    fireEvent.click(screen.getAllByRole("button", { name: "Clear search" })[0]);
    expect(stored("unibox.templates.compose.search")).toBe("");
    refreshed.unmount();
    mount(picker(templates, "reply"));
    expect(screen.getByPlaceholderText("Search templates")).toHaveValue("");
  });

  it("closed mailbox menus do not erase search/tag state and unavailable tags remain removable", () => {
    const picker = (browseKey = "compose") => <MailboxPicker browseKey={browseKey} value="auto" autoTag={null} onChange={() => {}} candidates={undefined} />;
    const menu = mount(picker());
    fireEvent.click(screen.getByRole("button", { name: /no active mailbox/ }));
    fireEvent.change(screen.getByPlaceholderText("Search mailboxes…"), { target: { value: "sender" } });
    fireEvent.click(screen.getByRole("button", { name: "All tags" }));
    fireEvent.click(screen.getByRole("button", { name: "Team" }));
    menu.unmount();
    act(() => useAppStore.setState({ tags: [] }));
    const refreshed = mount(picker());
    expect(screen.queryByPlaceholderText("Search mailboxes…")).toBeNull();
    expect(stored("unibox.mailbox-picker.compose.search")).toBe("sender");
    expect(stored("unibox.mailbox-picker.compose.tag")).toBe("tag");
    fireEvent.click(screen.getByRole("button", { name: /no active mailbox/ }));
    expect(screen.getByPlaceholderText("Search mailboxes…")).toHaveValue("sender");
    fireEvent.click(screen.getByRole("button", { name: "Unavailable tag" }));
    fireEvent.click(screen.getByRole("button", { name: "All tags" }));
    expect(stored("unibox.mailbox-picker.compose.tag")).toBeNull();
    refreshed.unmount();
    mount(picker("reply"));
    expect(stored("unibox.mailbox-picker.reply.search")).toBe("");
  });

  it("restores recipient browse query/category/sort before querying, while To/Cc and recipients remain independent", () => {
    const field = (browseKey = "compose.to") => <ContactRecipientField browseKey={browseKey} value={[]} onChange={() => {}} placeholder="Recipient" />;
    const editor = mount(field());
    fireEvent.click(screen.getByTitle("Browse contacts"));
    fireEvent.change(screen.getByPlaceholderText("Search contacts…"), { target: { value: "Acme" } });
    fireEvent.click(screen.getByRole("button", { name: "All labels" }));
    fireEvent.click(screen.getByRole("button", { name: "Sales" }));
    fireEvent.click(screen.getByRole("button", { name: "Recent" }));
    fireEvent.click(screen.getByRole("button", { name: "Email" }));
    fireEvent.change(screen.getByPlaceholderText("Recipient"), { target: { value: "not-saved@example.com" } });
    editor.unmount();
    mocks.contacts.mockClear();
    const refreshed = mount(field());
    expect(screen.getByPlaceholderText("Recipient")).toHaveValue("");
    expect(screen.queryByPlaceholderText("Search contacts…")).toBeNull();
    fireEvent.click(screen.getByTitle("Browse contacts"));
    expect(screen.getByPlaceholderText("Search contacts…")).toHaveValue("Acme");
    expect(screen.getByRole("button", { name: "Sales" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Email" })).toBeTruthy();
    expect(mocks.contacts).toHaveBeenLastCalledWith(expect.objectContaining({ options: expect.objectContaining({ query: "Acme", category_ids: ["label"], sort_by: "email" }), enabled: true }));
    mocks.categories = [];
    refreshed.rerender(<Owner>{field()}</Owner>);
    expect(screen.getByRole("button", { name: "Unavailable filter" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Unavailable filter" }));
    fireEvent.click(screen.getByRole("button", { name: "All labels" }));
    expect(stored("unibox.recipients.compose.to.category")).toBeNull();
    refreshed.unmount();
    mount(field("compose.cc"));
    fireEvent.click(screen.getByTitle("Browse contacts"));
    expect(screen.getByPlaceholderText("Search contacts…")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Recent" })).toBeTruthy();
  });
});
