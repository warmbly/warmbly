// The scope rail's sections fold, their rows can be hidden, and any row can be
// starred into Favorites.
//
// Both are per-browser preferences that live in the persisted store, so what is
// pinned here is the part that is easy to get wrong: the fold survives, a
// folded section still shows the scope you are on, hiding a row only takes it
// off the rail (the active scope never disappears), and a stored value that is
// not what the setters would have written cannot reach the screen.

import React from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup, act } from "@testing-library/react";
import toast from "react-hot-toast/headless";
import { Toaster } from "@/components/ui/toaster";
import { useAppStore } from "@/stores";
import {
  applyRailOrder,
  cleanUniboxRailFavoriteName,
  sanitizeUniboxRailFavorites,
  sanitizeUniboxRailHidden,
  sanitizeUniboxRailOrder,
} from "@/stores/slices/uiSlice";
import { ScopeRail, type UniboxScope } from "./ScopeRail";
import { UserContext } from "@/hooks/context/user";

const overview = vi.hoisted(() => ({
  data: {
    total: 12,
    unread: 3,
    today: 0,
    week: 0,
    snoozed: 0,
    awaiting_reply: 0,
    automated: 0,
    automated_unread: 0,
    awaiting_agent_draft: 0,
    scheduled_pending: 0,
    scheduled_pending_max: 100,
    folders: [
      { folder: "inbox", unread: 3, total: 9 },
      { folder: "spam", unread: 0, total: 1 },
    ],
    mailboxes: [{ id: "m1", email: "me@example.com", name: "Me", unread: 2, total: 5 }],
    tags: [],
    categories: [{ id: "c1", title: "Interested", color: "#0ea5e9", unread: 1, total: 4 }],
  },
}));

vi.mock("@/lib/api/hooks/app/unibox/useUniboxOverview", () => ({
  default: () => ({ data: overview.data, isPending: false }),
}));
vi.mock("@/lib/api/hooks/app/unibox/useMarkSeen", () => ({
  default: () => ({ mutate: () => {} }),
}));
vi.mock("@/components/app/unibox/compose/ComposeDraftsItem", () => ({
  default: () => null,
}));
// Exit animations never finish in jsdom; a closed menu unmounts at once instead.
vi.mock("framer-motion", async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
// The count-up tween needs a real animation frame; a plain number is enough here.
vi.mock("@/components/ui/AnimatedNumber", () => ({
  default: ({ value }: { value: number }) => <>{value}</>,
}));

// The toggle's name grows by the dot's screen-reader text when a highlighted
// count is folded away, so match on how it starts.
const MAIL_TOGGLE = /^Mail(?!boxes| section)/;
const DOT = /highlighted count folded away/;

// The header's always-visible pencil.
function startEditing(section: "Mail" | "Views") {
  fireEvent.click(screen.getByRole("button", { name: `Edit ${section} rows` }));
}

function mountRail(scope: UniboxScope = { kind: "all" }) {
  return render(<ScopeRail scope={scope} onChange={() => {}} />);
}

beforeEach(() => {
  sessionStorage.clear();
  overview.data.mailboxes = [{ id: "m1", email: "me@example.com", name: "Me", unread: 2, total: 5 }];
  useAppStore.setState({
    uniboxRailFolded: {},
    uniboxRailHidden: [],
    uniboxRailOrder: {},
    uniboxRailSectionOrder: [],
    uniboxRailFavorites: [],
  });
});

describe("ScopeRail browsing persistence", () => {
  function Owner({ children }: { children: React.ReactNode }) {
    return <UserContext.Provider value={{ user: { id: "user" } } as React.ComponentProps<typeof UserContext.Provider>["value"]}>{children}</UserContext.Provider>;
  }
  function directory() {
    overview.data.mailboxes = Array.from({ length: 10 }, (_, index) => ({ id: `m${index}`, email: `mail${index}@example.com`, name: `Mailbox ${index}`, unread: 0, total: 0 }));
  }
  it("restores search and Show all across remount and keeps active search visible for smaller directories", () => {
    directory();
    const mount = () => render(<Owner><ScopeRail scope={{ kind: "all" }} onChange={() => {}} /></Owner>);
    const rail = mount();
    fireEvent.click(screen.getByRole("button", { name: /Show all/ }));
    fireEvent.change(screen.getByPlaceholderText("Filter mailboxes"), { target: { value: "Mailbox" } });
    rail.unmount();
    const refreshed = mount();
    expect(screen.getByPlaceholderText("Filter mailboxes")).toHaveValue("Mailbox");
    expect(screen.getByRole("button", { name: "Show less" })).toBeTruthy();
    overview.data.mailboxes = overview.data.mailboxes.slice(0, 2);
    refreshed.rerender(<Owner><ScopeRail scope={{ kind: "all" }} onChange={() => {}} /></Owner>);
    expect(screen.getByPlaceholderText("Filter mailboxes")).toHaveValue("Mailbox");
  });
  it("desktop and mobile instances cannot overwrite each other's restored filters", () => {
    directory();
    const rail = render(<Owner><div><ScopeRail scope={{ kind: "all" }} onChange={() => {}} /><ScopeRail browseKey="mobile" scope={{ kind: "all" }} onChange={() => {}} /></div></Owner>);
    const fields = screen.getAllByPlaceholderText("Filter mailboxes");
    fireEvent.change(fields[0], { target: { value: "mail1" } });
    fireEvent.change(fields[1], { target: { value: "mail2" } });
    rail.unmount();
    render(<Owner><div><ScopeRail scope={{ kind: "all" }} onChange={() => {}} /><ScopeRail browseKey="mobile" scope={{ kind: "all" }} onChange={() => {}} /></div></Owner>);
    const restored = screen.getAllByPlaceholderText("Filter mailboxes");
    expect(restored[0]).toHaveValue("mail1");
    expect(restored[1]).toHaveValue("mail2");
  });
});

afterEach(() => {
  cleanup();
  act(() => toast.remove());
});

describe("ScopeRail sections", () => {
  it("starts expanded with nothing hidden and a Mail header over the mail rows", () => {
    mountRail();
    expect(screen.getByRole("button", { name: "Mail", expanded: true })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Views", expanded: true })).toBeTruthy();
    for (const label of ["All mail", "Inbox", "Spam", "Trash", "Scheduled"]) {
      expect(screen.getByText(label)).toBeTruthy();
    }
  });

  it("folds a section, hides its rows and remembers it in the persisted store", () => {
    mountRail();
    fireEvent.click(screen.getByRole("button", { name: MAIL_TOGGLE }));

    expect(useAppStore.getState().uniboxRailFolded.mail).toBe(true);
    expect(screen.getByRole("button", { name: MAIL_TOGGLE, expanded: false })).toBeTruthy();
    expect(screen.queryByText("Spam")).toBeNull();
    expect(screen.queryByText("Inbox")).toBeNull();

    // Part of what the store writes to storage, not just in-memory state.
    const persisted = useAppStore.persist.getOptions().partialize?.(useAppStore.getState()) as
      | { uniboxRailFolded?: Record<string, boolean> }
      | undefined;
    expect(persisted?.uniboxRailFolded).toEqual({ mail: true });

    fireEvent.click(screen.getByRole("button", { name: MAIL_TOGGLE }));
    expect(screen.getByText("Spam")).toBeTruthy();
  });

  it("keeps the row you are on when the section is folded", () => {
    useAppStore.setState({ uniboxRailFolded: { mail: true } });
    mountRail({ kind: "folder", folder: "spam" });
    expect(screen.getByText("Spam")).toBeTruthy();
    expect(screen.queryByText("Inbox")).toBeNull();
  });

  it("keeps the active mailbox visible in a folded Mailboxes section", () => {
    useAppStore.setState({ uniboxRailFolded: { mailboxes: true } });
    mountRail({ kind: "mailbox", mailboxId: "m1" });
    expect(screen.getByText("me@example.com")).toBeTruthy();

    cleanup();
    mountRail();
    expect(screen.queryByText("me@example.com")).toBeNull();
  });

  it("flags a highlighted count folded out of sight with a dot, and drops it when the section opens", () => {
    useAppStore.setState({ uniboxRailFolded: { mail: true } });
    mountRail({ kind: "folder", folder: "spam" });
    expect(screen.getByRole("button", { name: "Mail, highlighted count folded away" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: MAIL_TOGGLE }));
    expect(screen.queryByText(DOT)).toBeNull();
  });

  it("says highlighted count, not unread, because Scheduled raises the dot too", () => {
    const saved = overview.data;
    overview.data = {
      ...saved,
      unread: 0,
      scheduled_pending: 5,
      folders: [
        { folder: "inbox", unread: 0, total: 9 },
        { folder: "spam", unread: 0, total: 1 },
      ],
    };
    try {
      useAppStore.setState({ uniboxRailFolded: { mail: true } });
      mountRail({ kind: "folder", folder: "spam" });
      expect(screen.getByText(DOT)).toBeTruthy();
      expect(screen.queryByText(/unread/i)).toBeNull();
    } finally {
      overview.data = saved;
    }
  });

  it("does not raise the dot for rows the user hid", () => {
    useAppStore.setState({
      uniboxRailFolded: { mail: true },
      uniboxRailHidden: ["folder:inbox", "unread"],
    });
    mountRail({ kind: "folder", folder: "spam" });
    expect(screen.queryByText(DOT)).toBeNull();
  });

  it("marks the row you are on with aria-current", () => {
    mountRail({ kind: "unread" });
    const current = document.querySelectorAll("[aria-current]");
    expect(current).toHaveLength(1);
    expect(current[0].textContent).toContain("Unread");
  });

  it("folds Views on its own", () => {
    mountRail();
    expect(screen.getByText("Hot leads")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Views" }));
    expect(useAppStore.getState().uniboxRailFolded.views).toBe(true);
    expect(screen.queryByText("Hot leads")).toBeNull();
    // Another section is untouched.
    expect(screen.getByText("Inbox")).toBeTruthy();
  });
});

describe("ScopeRail edit mode", () => {
  it("hides a row through Edit, uncheck, Done", () => {
    mountRail();
    startEditing("Mail");

    // Every row is a checkbox, all checked to start with.
    const spam = screen.getByRole("checkbox", { name: "Spam" });
    expect(spam.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(spam);
    expect(screen.getByRole("checkbox", { name: "Spam" }).getAttribute("aria-checked")).toBe("false");
    // Still listed while editing, so it can be turned back on.
    expect(screen.getByText("Spam")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Done editing Mail" }));
    expect(useAppStore.getState().uniboxRailHidden).toEqual(["folder:spam"]);
    expect(screen.queryByText("Spam")).toBeNull();
    expect(screen.getByText("Trash")).toBeTruthy();
  });

  it("the pencil does not fold the section", () => {
    mountRail();
    fireEvent.click(screen.getByRole("button", { name: "Edit Mail rows" }));
    expect(useAppStore.getState().uniboxRailFolded.mail).toBeUndefined();
    expect(screen.getByText("Spam")).toBeTruthy();
    // One click enters edit mode: no menu in between.
    expect(screen.queryByRole("menuitem")).toBeNull();
    expect(screen.getByRole("button", { name: "Done editing Mail" })).toBeTruthy();
  });

  it("counts hidden rows beside the pencil, open or folded, and drops the count when they are shown", () => {
    mountRail();
    expect(screen.queryByText(/\d+ hidden/)).toBeNull();

    startEditing("Mail");
    fireEvent.click(screen.getByRole("checkbox", { name: "Spam" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Trash" }));
    // Not shown while editing: every row is on screen then.
    expect(screen.queryByText("2 hidden")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Done editing Mail" }));
    expect(screen.getByText("2 hidden")).toBeTruthy();
    // Tabbing to the pencil hears the number too.
    expect(
      screen.getByRole("button", { name: "Edit Mail rows", description: "2 hidden" }),
    ).toBeTruthy();
    // Views has none of its own.
    expect(screen.queryByText("1 hidden")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: MAIL_TOGGLE }));
    expect(screen.getByText("2 hidden")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: MAIL_TOGGLE }));

    startEditing("Mail");
    fireEvent.click(screen.getByRole("checkbox", { name: "Spam" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Trash" }));
    fireEvent.click(screen.getByRole("button", { name: "Done editing Mail" }));
    expect(screen.queryByText(/\d+ hidden/)).toBeNull();
  });

  it("does not navigate while editing and drops the folder menu", () => {
    const onChange = vi.fn();
    render(<ScopeRail scope={{ kind: "all" }} onChange={onChange} />);
    expect(screen.getByLabelText("Inbox folder actions")).toBeTruthy();

    startEditing("Mail");
    fireEvent.click(screen.getByRole("checkbox", { name: "Inbox" }));
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.queryByLabelText("Inbox folder actions")).toBeNull();
  });

  it("shows hidden rows and unfolds the section while editing", () => {
    useAppStore.setState({ uniboxRailFolded: { mail: true }, uniboxRailHidden: ["folder:trash"] });
    mountRail();
    expect(screen.queryByText("Trash")).toBeNull();

    startEditing("Mail");
    expect(screen.getByRole("checkbox", { name: "Trash" }).getAttribute("aria-checked")).toBe("false");
    expect(screen.getByRole("checkbox", { name: "Inbox" })).toBeTruthy();
  });

  it("ends edit mode on Escape", () => {
    mountRail();
    startEditing("Mail");
    expect(screen.getAllByRole("checkbox").length).toBeGreaterThan(0);

    fireEvent.keyDown(screen.getByRole("checkbox", { name: "Inbox" }), { key: "Escape" });
    expect(screen.queryAllByRole("checkbox")).toHaveLength(0);
    expect(screen.getByRole("button", { name: "Edit Mail rows" })).toBeTruthy();
  });

  it("holds the fold while editing, so Done never folds by surprise", () => {
    mountRail();
    startEditing("Mail");
    const toggle = screen.getByRole("button", { name: MAIL_TOGGLE });
    expect((toggle as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(toggle);
    expect(useAppStore.getState().uniboxRailFolded.mail).toBeUndefined();

    fireEvent.click(screen.getByRole("button", { name: "Done editing Mail" }));
    expect(screen.getByText("Spam")).toBeTruthy();
  });

  it("moves focus into the checkboxes and back to the options button", () => {
    mountRail();
    startEditing("Mail");
    expect(document.activeElement).toBe(screen.getByRole("checkbox", { name: "All mail" }));

    fireEvent.keyDown(document.activeElement as Element, { key: "Escape" });
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Edit Mail rows" }));

    startEditing("Mail");
    fireEvent.click(screen.getByRole("button", { name: "Done editing Mail" }));
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Edit Mail rows" }));
  });

  it("edits Views separately from Mail", () => {
    mountRail();
    startEditing("Views");
    fireEvent.click(screen.getByRole("checkbox", { name: "Hot leads" }));
    fireEvent.click(screen.getByRole("button", { name: "Done editing Views" }));
    expect(useAppStore.getState().uniboxRailHidden).toEqual(["view:hot"]);
    expect(screen.queryByText("Hot leads")).toBeNull();
    expect(screen.getByText("Follow up")).toBeTruthy();
  });

  it("keeps the header, and so Edit, when every row is hidden", () => {
    useAppStore.setState({
      uniboxRailHidden: [
        "all", "folder:inbox", "unread", "awaiting", "agent_drafts", "snoozed",
        "folder:drafts", "folder:sent", "scheduled", "folder:archive", "folder:spam", "folder:trash",
      ],
    });
    mountRail({ kind: "mailbox", mailboxId: "m1" });
    expect(screen.queryByText("Inbox")).toBeNull();
    expect(screen.getByRole("button", { name: "Edit Mail rows" })).toBeTruthy();
  });
});

describe("hidden rows", () => {
  it("never hide the active scope", () => {
    useAppStore.setState({ uniboxRailHidden: ["folder:spam"] });
    mountRail({ kind: "folder", folder: "spam" });
    expect(screen.getByText("Spam")).toBeTruthy();

    cleanup();
    mountRail({ kind: "all" });
    expect(screen.queryByText("Spam")).toBeNull();
  });
});

describe("sanitizeUniboxRailHidden", () => {
  it("drops anything that is not a string and removes duplicates", () => {
    expect(sanitizeUniboxRailHidden(["folder:spam", 4, null, {}, "folder:spam", "view:hot"])).toEqual([
      "folder:spam",
      "view:hot",
    ]);
    expect(sanitizeUniboxRailHidden("folder:spam")).toEqual([]);
    expect(sanitizeUniboxRailHidden(undefined)).toEqual([]);
  });

  it("runs on rehydration, where the setters are bypassed", async () => {
    const original = useAppStore.persist.getOptions().storage;
    useAppStore.persist.setOptions({
      storage: {
        getItem: () =>
          ({
            state: {
              uniboxRailHidden: ["folder:spam", 7, "folder:spam"],
              uniboxRailFolded: { mail: true, views: "yes" },
            },
          }) as never,
        setItem: () => {},
        removeItem: () => {},
      },
    });
    try {
      await useAppStore.persist.rehydrate();
      expect(useAppStore.getState().uniboxRailHidden).toEqual(["folder:spam"]);
      expect(useAppStore.getState().uniboxRailFolded).toEqual({ mail: true });
    } finally {
      useAppStore.persist.setOptions({ storage: original });
    }
  });
});

const MAIL_DEFAULT = [
  "all", "folder:inbox", "unread", "awaiting", "agent_drafts", "snoozed",
  "folder:drafts", "folder:sent", "scheduled", "folder:archive", "folder:spam", "folder:trash",
];

// The rail row element around a label, as the keyboard sees it.
const rowOf = (label: string) => screen.getByText(label).closest("[data-rail-row]") as HTMLElement;

// Headers in the order they are on screen.
const headerOrder = () =>
  screen.getAllByRole("button", { expanded: true }).map((b) => b.textContent?.replace(/\d+$/, ""));

describe("row order", () => {
  it("moves a row with the grip's arrow keys while editing, and says where it went", () => {
    mountRail();
    startEditing("Mail");
    fireEvent.keyDown(screen.getByRole("button", { name: /^Move Spam/ }), { key: "ArrowUp" });

    const order = useAppStore.getState().uniboxRailOrder.mail;
    expect(order.indexOf("folder:spam")).toBe(order.indexOf("folder:archive") - 1);
    expect(screen.getByText("Spam moved to position 10 of 12")).toBeTruthy();

    // Checkboxes follow the new order.
    const names = screen.getAllByRole("checkbox").map((c) => c.textContent);
    expect(names.indexOf("Spam")).toBeLessThan(names.indexOf("Archive"));
  });

  it("moves a row with Alt+arrow outside edit mode, stepping over a hidden neighbour", () => {
    useAppStore.setState({ uniboxRailHidden: ["unread"] });
    mountRail();
    fireEvent.keyDown(rowOf("Inbox"), { key: "ArrowDown", altKey: true });
    expect(useAppStore.getState().uniboxRailOrder.mail.slice(0, 4)).toEqual([
      "all", "unread", "awaiting", "folder:inbox",
    ]);
  });

  it("stores nothing once a row is moved back to where it started", () => {
    mountRail();
    fireEvent.keyDown(rowOf("Inbox"), { key: "ArrowDown", altKey: true });
    expect(useAppStore.getState().uniboxRailOrder.mail).toBeDefined();
    fireEvent.keyDown(rowOf("Inbox"), { key: "ArrowUp", altKey: true });
    expect(useAppStore.getState().uniboxRailOrder.mail).toBeUndefined();
  });

  it("renders a stored order", () => {
    useAppStore.setState({ uniboxRailOrder: { mail: ["folder:trash", ...MAIL_DEFAULT.slice(0, -1)] } });
    mountRail();
    const rows = Array.from(document.querySelectorAll("[data-rail-row]")).map((r) => r.textContent);
    expect(rows[0]).toContain("Trash");
  });

  it("resets order and hidden rows from the edit footer", () => {
    useAppStore.setState({
      uniboxRailHidden: ["folder:spam", "view:hot"],
      uniboxRailOrder: { mail: ["folder:trash", ...MAIL_DEFAULT.slice(0, -1)] },
    });
    mountRail();
    startEditing("Mail");
    fireEvent.click(screen.getByRole("button", { name: "Reset" }));
    expect(useAppStore.getState().uniboxRailOrder.mail).toBeUndefined();
    // Only this section's rows come back.
    expect(useAppStore.getState().uniboxRailHidden).toEqual(["view:hot"]);
    expect((screen.getByRole("button", { name: "Reset" }) as HTMLButtonElement).disabled).toBe(true);
  });
});

describe("row menu", () => {
  it("hides a row from its menu", () => {
    mountRail();
    fireEvent.click(screen.getByLabelText("Spam folder actions"));
    fireEvent.click(screen.getByRole("menuitem", { name: "Hide from rail" }));
    expect(useAppStore.getState().uniboxRailHidden).toEqual(["folder:spam"]);
    expect(screen.queryByText("Spam")).toBeNull();
  });

  it("opens the same menu on right-click, without opening the scope", () => {
    const onChange = vi.fn();
    render(<ScopeRail scope={{ kind: "all" }} onChange={onChange} />);
    fireEvent.contextMenu(rowOf("Unread"), { clientX: 40, clientY: 80 });
    expect(screen.getByRole("menuitem", { name: /Move up/ })).toBeTruthy();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("closes on a press anywhere else, and on Escape", () => {
    mountRail();
    fireEvent.contextMenu(rowOf("Unread"), { clientX: 40, clientY: 80 });
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole("menu")).toBeNull();

    fireEvent.contextMenu(rowOf("Unread"), { clientX: 40, clientY: 80 });
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("keeps one menu open: a second right-click or a section menu replaces the first", () => {
    mountRail();
    fireEvent.contextMenu(rowOf("Unread"), { clientX: 40, clientY: 80 });
    // Keyboard context-menu key: no pointer press to close the first one.
    fireEvent.contextMenu(rowOf("Inbox"));
    expect(screen.getAllByRole("menu")).toHaveLength(1);
    expect(screen.getByRole("menuitem", { name: "Mark all as read" })).toBeTruthy();

    fireEvent.click(screen.getByLabelText("Mail section options"));
    expect(screen.getAllByRole("menu")).toHaveLength(1);
    expect(screen.getByRole("menuitem", { name: "Fold section" })).toBeTruthy();
  });

  it("leaves the count room beside the always-visible phone \"…\"", () => {
    mountRail();
    const row = rowOf("Inbox");
    expect(row.className.split(" ")).toContain("pr-7");
    expect(row.className.split(" ")).not.toContain("pr-2");
  });

  it("disables moves the row cannot make", () => {
    mountRail();
    fireEvent.click(screen.getByLabelText("All mail actions"));
    expect((screen.getByRole("menuitem", { name: /Move up/ }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("menuitem", { name: /Move down/ }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("walks its items with the arrow keys", async () => {
    mountRail();
    fireEvent.click(screen.getByLabelText("Inbox folder actions"));
    const menu = screen.getByRole("menu");
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement?.textContent).toBe("Mark all as read");
    fireEvent.keyDown(document.activeElement as Element, { key: "ArrowDown" });
    expect(document.activeElement?.textContent).toContain("Move up");
    fireEvent.keyDown(document.activeElement as Element, { key: "End" });
    expect(document.activeElement?.textContent).toContain("Edit Mail rows");
  });
});

describe("section menu", () => {
  it("moves a section and remembers it", () => {
    mountRail();
    expect(headerOrder().slice(0, 2)).toEqual(["Mail", "Views"]);
    fireEvent.click(screen.getByLabelText("Mail section options"));
    fireEvent.click(screen.getByRole("menuitem", { name: "Move section down" }));
    const stored = useAppStore.getState().uniboxRailSectionOrder;
    expect(stored.indexOf("views")).toBeLessThan(stored.indexOf("mail"));
    expect(headerOrder().slice(0, 2)).toEqual(["Views", "Mail"]);
  });

  it("folds every other section", () => {
    mountRail();
    fireEvent.click(screen.getByLabelText("Views section options"));
    fireEvent.click(screen.getByRole("menuitem", { name: "Fold other sections" }));
    const folded = useAppStore.getState().uniboxRailFolded;
    expect(folded).toMatchObject({ mail: true, views: false, mailboxes: true, labels: true });
    expect(screen.getByText("Hot leads")).toBeTruthy();
    expect(screen.queryByText("Inbox")).toBeNull();
  });

  it("offers to show the hidden rows", () => {
    useAppStore.setState({ uniboxRailHidden: ["folder:spam", "folder:trash"] });
    mountRail();
    fireEvent.click(screen.getByLabelText("Mail section options"));
    fireEvent.click(screen.getByRole("menuitem", { name: "Show 2 hidden rows" }));
    expect(useAppStore.getState().uniboxRailHidden).toEqual([]);
    expect(screen.getByText("Spam")).toBeTruthy();
  });

  it("starts editing from the menu", () => {
    mountRail();
    fireEvent.click(screen.getByLabelText("Views section options"));
    fireEvent.click(screen.getByRole("menuitem", { name: "Edit rows…" }));
    expect(screen.getByRole("checkbox", { name: "Hot leads" })).toBeTruthy();
  });
});

describe("edit mode ends", () => {
  it("on a click outside the section", () => {
    mountRail();
    startEditing("Mail");
    fireEvent.mouseDown(screen.getByText("me@example.com"));
    expect(screen.queryAllByRole("checkbox")).toHaveLength(0);
  });

  it("not on a click inside it", () => {
    mountRail();
    startEditing("Mail");
    fireEvent.mouseDown(screen.getByRole("checkbox", { name: "Spam" }));
    expect(screen.getAllByRole("checkbox").length).toBeGreaterThan(0);
  });
});

describe("arrow keys between rows", () => {
  it("move focus down the rail and across sections", () => {
    mountRail();
    rowOf("All mail").focus();
    fireEvent.keyDown(rowOf("All mail"), { key: "ArrowDown" });
    expect(document.activeElement).toBe(rowOf("Inbox"));
    fireEvent.keyDown(document.activeElement as Element, { key: "End" });
    expect(document.activeElement?.textContent).toContain("Interested");
  });
});

describe("applyRailOrder", () => {
  it("keeps the stored order, drops keys that are gone, and slots new ones after their default neighbour", () => {
    expect(applyRailOrder(["a", "b", "c"], ["c", "a", "b"])).toEqual(["c", "a", "b"]);
    expect(applyRailOrder(["a", "b"], ["b", "gone", "a"])).toEqual(["b", "a"]);
    expect(applyRailOrder(["a", "b", "new", "c"], ["c", "b", "a"])).toEqual(["c", "b", "new", "a"]);
    expect(applyRailOrder(["first", "a"], ["a"])).toEqual(["first", "a"]);
    expect(applyRailOrder(["a"], undefined)).toEqual(["a"]);
  });
});

describe("sanitizeUniboxRailOrder", () => {
  it("keeps string lists only, deduplicated", () => {
    expect(sanitizeUniboxRailOrder({ mail: ["a", "a", 3, "b"], views: "x", tags: [] })).toEqual({
      mail: ["a", "b"],
      tags: [],
    });
    expect(sanitizeUniboxRailOrder(["mail"])).toEqual({});
  });

  it("is persisted with the section order", () => {
    useAppStore.setState({ uniboxRailOrder: { mail: ["unread"] }, uniboxRailSectionOrder: ["views"] });
    const persisted = useAppStore.persist.getOptions().partialize?.(useAppStore.getState()) as {
      uniboxRailOrder?: unknown;
      uniboxRailSectionOrder?: unknown;
    };
    expect(persisted.uniboxRailOrder).toEqual({ mail: ["unread"] });
    expect(persisted.uniboxRailSectionOrder).toEqual(["views"]);
  });
});

describe("keyboard moves keep their place", () => {
  // Browsers blur a focused node that is moved in the DOM (React restores it); jsdom does not, so mimic it.
  const insertBefore = Node.prototype.insertBefore;
  beforeEach(() => {
    Node.prototype.insertBefore = function <T extends Node>(this: Node, node: T, child: Node | null): T {
      const focused = document.activeElement;
      if (node.isConnected && focused && node.contains(focused)) (focused as HTMLElement).blur();
      return insertBefore.call(this, node, child) as T;
    };
  });
  afterEach(() => {
    Node.prototype.insertBefore = insertBefore;
  });

  it("keeps focus on the row it moved", () => {
    mountRail();
    rowOf("Inbox").focus();
    fireEvent.keyDown(rowOf("Inbox"), { key: "ArrowDown", altKey: true });
    expect(document.activeElement).toBe(rowOf("Inbox"));
    fireEvent.keyDown(rowOf("Inbox"), { key: "ArrowDown", altKey: true });
    expect(useAppStore.getState().uniboxRailOrder.mail.slice(0, 4)).toEqual([
      "all", "unread", "awaiting", "folder:inbox",
    ]);
  });

  it("keeps focus on the grip it moved", () => {
    mountRail();
    startEditing("Mail");
    const grip = () => screen.getByRole("button", { name: /^Move Inbox/ });
    grip().focus();
    fireEvent.keyDown(grip(), { key: "ArrowDown" });
    expect(document.activeElement).toBe(grip());
  });

  it("offers no moves in a folded section", () => {
    useAppStore.setState({ uniboxRailFolded: { mail: true } });
    mountRail({ kind: "unread" });
    fireEvent.click(screen.getByLabelText("Unread actions"));
    expect((screen.getByRole("menuitem", { name: /Move up/ }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("menuitem", { name: /Move down/ }) as HTMLButtonElement).disabled).toBe(true);
  });
});

describe("dropdown keys", () => {
  it("jump to the first matching item from the panel, and never reach global shortcuts", () => {
    const globalKeys = vi.fn();
    window.addEventListener("keydown", globalKeys);
    try {
      mountRail();
      fireEvent.click(screen.getByLabelText("Mail section options"));
      fireEvent.keyDown(screen.getByRole("menu"), { key: "f" });
      expect(document.activeElement?.textContent).toBe("Fold section");
      fireEvent.keyDown(document.activeElement as Element, { key: "e" });
      expect(document.activeElement?.textContent).toBe("Edit rows…");
      expect(globalKeys).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener("keydown", globalKeys);
    }
  });

  it("leave Tab alone", () => {
    mountRail();
    fireEvent.click(screen.getByLabelText("Mail section options"));
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Tab" });
    expect(screen.getByRole("menu")).toBeTruthy();
  });
});

const FAVORITES_TOGGLE = /^Favorites(,|$)/;

// The Favorites section's rows, as the user reads them.
const favoritesPanel = () =>
  document.getElementById(
    screen.getByRole("button", { name: FAVORITES_TOGGLE }).getAttribute("aria-controls") as string,
  ) as HTMLElement;
const favoriteLabels = () =>
  Array.from(favoritesPanel().querySelectorAll("[data-rail-row]")).map((r) => r.textContent?.replace(/\d+$/, ""));

function favorite(label: string, noun = "") {
  fireEvent.click(screen.getByLabelText(`${label} ${noun ? `${noun} ` : ""}actions`));
  fireEvent.click(screen.getByRole("menuitem", { name: "Add to Favorites" }));
}

describe("Favorites", () => {
  it("is not on the rail until something is starred", () => {
    mountRail();
    expect(screen.queryByRole("button", { name: FAVORITES_TOGGLE })).toBeNull();
    expect(screen.queryByText("Favorites")).toBeNull();
  });

  it("gathers rows from Mail, Views, Mailboxes and Labels at the top, in the order they were added", () => {
    mountRail();
    favorite("Inbox", "folder");
    favorite("Hot leads", "view");
    favorite("me@example.com", "mailbox");
    favorite("Interested", "label");

    expect(headerOrder()[0]).toBe("Favorites");
    expect(favoriteLabels()).toEqual(["Inbox", "Hot leads", "me@example.com", "Interested"]);
    // Each row stays in its own section too.
    expect(screen.getAllByText("Inbox")).toHaveLength(2);
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([
      { key: "folder:inbox" },
      { key: "view:hot" },
      { key: "mailbox:m1" },
      { key: "category:c1" },
    ]);
    const persisted = useAppStore.persist.getOptions().partialize?.(useAppStore.getState()) as {
      uniboxRailFavorites?: unknown;
    };
    expect(persisted.uniboxRailFavorites).toEqual(useAppStore.getState().uniboxRailFavorites);
  });

  it("carries the row's live count", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "mailbox:m1" }] });
    mountRail();
    expect(favoritesPanel().textContent).toContain("2");
  });

  it("offers Remove from Favorites once a row is in", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "folder:spam" }] });
    mountRail();
    fireEvent.click(screen.getByLabelText("Spam folder actions"));
    expect(screen.queryByRole("menuitem", { name: "Add to Favorites" })).toBeNull();
    fireEvent.click(screen.getByRole("menuitem", { name: "Remove from Favorites" }));
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([]);
    expect(screen.queryByRole("button", { name: FAVORITES_TOGGLE })).toBeNull();
  });

  it("stars a row from the Mail edit mode", () => {
    mountRail();
    startEditing("Mail");
    const star = screen.getByRole("button", { name: "Favorite Sent" });
    expect(star.getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(star);
    expect(screen.getByRole("button", { name: "Favorite Sent" }).getAttribute("aria-pressed")).toBe("true");
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([{ key: "folder:sent" }]);
  });

  it("highlights one row: the favorite, or the home row when that is where the scope was opened", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "folder:inbox" }] });
    const onChange = vi.fn();
    const { rerender } = render(<ScopeRail scope={{ kind: "folder", folder: "inbox" }} onChange={onChange} />);
    let current = document.querySelectorAll("[aria-current]");
    expect(current).toHaveLength(1);
    expect(favoritesPanel().contains(current[0])).toBe(true);

    // The Mail section's own Inbox row.
    const homeInbox = screen.getAllByText("Inbox").map((n) => n.closest("[data-rail-row]") as HTMLElement)[1];
    fireEvent.click(homeInbox);
    expect(onChange).toHaveBeenCalledWith({ kind: "folder", folder: "inbox" });
    rerender(<ScopeRail scope={{ kind: "folder", folder: "inbox" }} onChange={onChange} />);
    current = document.querySelectorAll("[aria-current]");
    expect(current).toHaveLength(1);
    expect(favoritesPanel().contains(current[0])).toBe(false);

    fireEvent.click(favoritesPanel().querySelector("[data-rail-row]") as HTMLElement);
    current = document.querySelectorAll("[aria-current]");
    expect(favoritesPanel().contains(current[0])).toBe(true);
  });

  it("highlights in Favorites again once the scope is left and reached some other way", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "folder:inbox" }] });
    const onChange = vi.fn();
    const { rerender } = render(<ScopeRail scope={{ kind: "folder", folder: "inbox" }} onChange={onChange} />);
    const homeInbox = () => screen.getAllByText("Inbox").map((n) => n.closest("[data-rail-row]") as HTMLElement)[1];
    fireEvent.click(homeInbox());
    rerender(<ScopeRail scope={{ kind: "folder", folder: "inbox" }} onChange={onChange} />);
    expect(favoritesPanel().querySelector("[aria-current]")).toBeNull();

    // A shortcut away and back, with no click in the rail.
    rerender(<ScopeRail scope={{ kind: "unread" }} onChange={onChange} />);
    rerender(<ScopeRail scope={{ kind: "folder", folder: "inbox" }} onChange={onChange} />);
    expect(favoritesPanel().querySelector("[aria-current]")).toBeTruthy();
    expect(document.querySelectorAll("[aria-current]")).toHaveLength(1);
  });

  it("names what a renamed view points at on hover", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "view:needs_reply", name: "Follow-ups" }] });
    mountRail();
    expect(favoriteLabels()).toEqual(["Follow-ups"]);
    fireEvent.focus(favoritesPanel().querySelector("[data-rail-row]") as HTMLElement);
    expect(screen.getAllByText(/^Needs a reply: /).length).toBeGreaterThan(0);
  });

  it("folds, keeps the favorite you are on, and remembers the fold", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "folder:inbox" }, { key: "unread" }] });
    mountRail({ kind: "unread" });
    fireEvent.click(screen.getByRole("button", { name: FAVORITES_TOGGLE }));
    expect(useAppStore.getState().uniboxRailFolded.favorites).toBe(true);
    expect(favoriteLabels()).toEqual(["Unread"]);
    // Inbox has a highlighted count folded out of sight.
    expect(screen.getByRole("button", { name: "Favorites, highlighted count folded away" })).toBeTruthy();
  });

  it("puts itself on top of a section order stored before it existed", () => {
    useAppStore.setState({
      uniboxRailFavorites: [{ key: "folder:inbox" }],
      uniboxRailSectionOrder: ["views", "mail", "mailboxes", "labels", "tags"],
    });
    mountRail();
    expect(headerOrder().slice(0, 3)).toEqual(["Favorites", "Views", "Mail"]);
  });

  it("moves a favorite with Alt+arrow without touching its home section", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "folder:inbox" }, { key: "view:hot" }] });
    mountRail();
    const inbox = favoritesPanel().querySelector('[data-rail-row="folder:inbox"]') as HTMLElement;
    fireEvent.keyDown(inbox, { key: "ArrowDown", altKey: true });
    expect(favoriteLabels()).toEqual(["Hot leads", "Inbox"]);
    expect(useAppStore.getState().uniboxRailOrder.mail).toBeUndefined();
  });

  it("keeps a favorite that does not resolve yet when the others move", () => {
    useAppStore.setState({
      uniboxRailFavorites: [{ key: "folder:inbox" }, { key: "mailbox:gone" }, { key: "view:hot" }],
    });
    mountRail();
    expect(favoriteLabels()).toEqual(["Inbox", "Hot leads"]);
    fireEvent.keyDown(favoritesPanel().querySelector('[data-rail-row="view:hot"]') as HTMLElement, {
      key: "ArrowUp",
      altKey: true,
    });
    expect(useAppStore.getState().uniboxRailFavorites.map((f) => f.key)).toEqual([
      "view:hot",
      "folder:inbox",
      "mailbox:gone",
    ]);
  });
});

describe("Favorites rename", () => {
  const renameFromMenu = (label: string) => {
    fireEvent.click(screen.getByLabelText(`${label} favorite actions`));
    fireEvent.click(screen.getByRole("menuitem", { name: /Rename/ }));
  };

  it("gives a favorite its own name, leaving the home row alone", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "mailbox:m1" }] });
    mountRail();
    renameFromMenu("me@example.com");
    const input = screen.getByRole("textbox", { name: "Name for me@example.com in Favorites" });
    expect((input as HTMLInputElement).value).toBe("me@example.com");
    fireEvent.change(input, { target: { value: "  Inbox   work " } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useAppStore.getState().uniboxRailFavorites).toEqual([{ key: "mailbox:m1", name: "Inbox work" }]);
    expect(favoriteLabels()).toEqual(["Inbox work"]);
    // The Mailboxes row still reads the address, and the favorite says where it points.
    expect(screen.getByText("me@example.com")).toBeTruthy();
    expect(screen.getByTitle("Inbox work (me@example.com)")).toBeTruthy();
    // Focus goes back to the row.
    expect(document.activeElement?.getAttribute("data-rail-row")).toBe("mailbox:m1");
  });

  it("goes back to the row's own name when cleared or set to it", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "folder:inbox", name: "Work" }] });
    mountRail();
    renameFromMenu("Work");
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "   " } });
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([{ key: "folder:inbox" }]);

    renameFromMenu("Inbox");
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "Inbox" } });
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([{ key: "folder:inbox" }]);
  });

  it("drops the edit on Escape and keeps it on blur", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "folder:inbox" }] });
    mountRail();
    renameFromMenu("Inbox");
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "Nope" } });
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Escape" });
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([{ key: "folder:inbox" }]);
    expect(screen.queryByRole("textbox")).toBeNull();

    renameFromMenu("Inbox");
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "Private" } });
    fireEvent.blur(screen.getByRole("textbox"));
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([{ key: "folder:inbox", name: "Private" }]);
  });

  it("opens with F2 on the focused favorite", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "unread" }] });
    mountRail();
    fireEvent.keyDown(favoritesPanel().querySelector('[data-rail-row="unread"]') as HTMLElement, { key: "F2" });
    expect(screen.getByRole("textbox", { name: "Name for Unread in Favorites" })).toBeTruthy();
    // F2 on a home row does nothing.
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Escape" });
    fireEvent.keyDown(rowOf("Inbox"), { key: "F2" });
    expect(screen.queryByRole("textbox")).toBeNull();
  });
});

describe("Favorites edit mode", () => {
  it("unticks a favorite, keeps it listed until Done, and ticks it back in place with its name", () => {
    useAppStore.setState({
      uniboxRailFavorites: [{ key: "folder:inbox", name: "Work" }, { key: "unread" }, { key: "view:hot" }],
    });
    mountRail();
    fireEvent.click(screen.getByRole("button", { name: "Edit favorites" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Work" }));
    expect(useAppStore.getState().uniboxRailFavorites.map((f) => f.key)).toEqual(["unread", "view:hot"]);
    expect(screen.getByRole("checkbox", { name: "Work" }).getAttribute("aria-checked")).toBe("false");

    fireEvent.click(screen.getByRole("checkbox", { name: "Work" }));
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([
      { key: "folder:inbox", name: "Work" },
      { key: "unread" },
      { key: "view:hot" },
    ]);

    fireEvent.click(screen.getByRole("checkbox", { name: "Unread" }));
    fireEvent.click(screen.getByRole("button", { name: "Done editing Favorites" }));
    expect(favoriteLabels()).toEqual(["Work", "Hot leads"]);
  });

  it("keeps a favorite starred elsewhere while editing when the section next writes", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "unread" }, { key: "view:hot" }] });
    mountRail();
    fireEvent.click(screen.getByRole("button", { name: "Edit favorites" }));
    // Starred from another row's menu by keyboard, which leaves edit mode open.
    act(() => useAppStore.getState().toggleUniboxRailFavorite("folder:sent"));
    fireEvent.click(screen.getByRole("checkbox", { name: "Unread" }));
    expect(useAppStore.getState().uniboxRailFavorites.map((f) => f.key)).toEqual(["view:hot", "folder:sent"]);
    expect(screen.getByRole("checkbox", { name: "Sent" }).getAttribute("aria-checked")).toBe("true");
  });

  it("stays on the rail while every favorite is unticked, and leaves it on Done", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "unread" }] });
    mountRail();
    fireEvent.click(screen.getByRole("button", { name: "Edit favorites" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Unread" }));
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([]);
    expect(screen.getByRole("button", { name: "Done editing Favorites" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Done editing Favorites" }));
    expect(screen.queryByRole("button", { name: FAVORITES_TOGGLE })).toBeNull();
  });

  it("reorders with the grip and renames from the row", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "unread" }, { key: "folder:inbox" }] });
    mountRail();
    fireEvent.click(screen.getByRole("button", { name: "Edit favorites" }));
    const panel = favoritesPanel();
    const grip = Array.from(panel.querySelectorAll("button")).find((b) =>
      b.getAttribute("aria-label")?.startsWith("Move Inbox"),
    ) as HTMLElement;
    fireEvent.keyDown(grip, { key: "ArrowUp" });
    expect(useAppStore.getState().uniboxRailFavorites.map((f) => f.key)).toEqual(["folder:inbox", "unread"]);

    fireEvent.click(screen.getByRole("button", { name: "Rename Unread" }));
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "To read" } });
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([
      { key: "folder:inbox" },
      { key: "unread", name: "To read" },
    ]);
    // Still editing: the renamed row is a checkbox again.
    expect(screen.getByRole("checkbox", { name: "To read" })).toBeTruthy();
  });

  it("keeps a rename in progress when a click outside ends editing", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "unread" }] });
    mountRail();
    fireEvent.click(screen.getByRole("button", { name: "Edit favorites" }));
    fireEvent.click(screen.getByRole("button", { name: "Rename Unread" }));
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "Later" } });
    fireEvent.mouseDown(screen.getByText("me@example.com"));
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([{ key: "unread", name: "Later" }]);
    expect(screen.queryByRole("button", { name: "Done editing Favorites" })).toBeNull();
  });

  it("has no Reset and no hidden count", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "unread" }] });
    mountRail();
    fireEvent.click(screen.getByRole("button", { name: "Edit favorites" }));
    expect(screen.getByText("Drag to reorder, untick to remove")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Reset" })).toBeNull();
    fireEvent.click(screen.getByRole("checkbox", { name: "Unread" }));
    fireEvent.click(screen.getByRole("button", { name: "Done editing Favorites" }));
    expect(screen.queryByText(/\d+ hidden/)).toBeNull();
  });
});

describe("Favorites and hiding", () => {
  it("says a row hides at once when the scope you are on is highlighted in Favorites", () => {
    useAppStore.setState({ uniboxRailFavorites: [{ key: "folder:inbox" }] });
    render(
      <>
        <ScopeRail scope={{ kind: "folder", folder: "inbox" }} onChange={() => {}} />
        <Toaster />
      </>,
    );
    fireEvent.click(screen.getByLabelText("Inbox folder actions"));
    fireEvent.click(screen.getByRole("menuitem", { name: "Hide from rail" }));
    expect(screen.getByText("Inbox is hidden from the rail.")).toBeTruthy();
    // Gone from Mail, still in Favorites.
    expect(screen.getAllByText("Inbox")).toHaveLength(1);
  });
});

describe("Favorites undo", () => {
  it("puts a removed favorite back where it was, with its name", () => {
    useAppStore.setState({
      uniboxRailFavorites: [{ key: "unread" }, { key: "folder:inbox", name: "Work" }, { key: "view:hot" }],
    });
    render(
      <>
        <ScopeRail scope={{ kind: "all" }} onChange={() => {}} />
        <Toaster />
      </>,
    );
    fireEvent.click(screen.getByLabelText("Work favorite actions"));
    fireEvent.click(screen.getByRole("menuitem", { name: "Remove from Favorites" }));
    expect(favoriteLabels()).toEqual(["Unread", "Hot leads"]);

    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([
      { key: "unread" },
      { key: "folder:inbox", name: "Work" },
      { key: "view:hot" },
    ]);
  });

  it("clears every favorite from the section menu, and brings them back", () => {
    const all = [{ key: "unread" }, { key: "folder:inbox", name: "Work" }];
    useAppStore.setState({ uniboxRailFavorites: all });
    render(
      <>
        <ScopeRail scope={{ kind: "all" }} onChange={() => {}} />
        <Toaster />
      </>,
    );
    fireEvent.click(screen.getByLabelText("Favorites section options"));
    fireEvent.click(screen.getByRole("menuitem", { name: "Remove all favorites" }));
    expect(useAppStore.getState().uniboxRailFavorites).toEqual([]);
    expect(screen.queryByRole("button", { name: FAVORITES_TOGGLE })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    expect(useAppStore.getState().uniboxRailFavorites).toEqual(all);
  });
});

describe("sanitizeUniboxRailFavorites", () => {
  it("keeps entries with a string key, one per key, and a clean name only", () => {
    expect(
      sanitizeUniboxRailFavorites([
        { key: "folder:inbox", name: "  Work  " },
        { key: "folder:inbox", name: "Again" },
        "unread",
        { key: 3 },
        { key: "" },
        null,
        { key: "view:hot", name: "   " },
        { key: "mailbox:m1", name: 42 },
      ]),
    ).toEqual([{ key: "folder:inbox", name: "Work" }, { key: "view:hot" }, { key: "mailbox:m1" }]);
    expect(sanitizeUniboxRailFavorites({ key: "unread" })).toEqual([]);
  });

  it("caps a name and collapses its whitespace", () => {
    expect(cleanUniboxRailFavoriteName("a\n\tb")).toBe("a b");
    expect(cleanUniboxRailFavoriteName("x".repeat(80))).toHaveLength(40);
    expect(cleanUniboxRailFavoriteName("")).toBeUndefined();
    // An emoji straddling the cap is kept whole or dropped, never split.
    const capped = cleanUniboxRailFavoriteName(`${"x".repeat(39)}🔥🔥`) as string;
    expect(Array.from(capped)).toHaveLength(40);
    expect(capped.endsWith("🔥")).toBe(true);
  });

  it("runs on rehydration", async () => {
    const original = useAppStore.persist.getOptions().storage;
    useAppStore.persist.setOptions({
      storage: {
        getItem: () =>
          ({ state: { uniboxRailFavorites: [{ key: "unread" }, { key: "unread" }, 5] } }) as never,
        setItem: () => {},
        removeItem: () => {},
      },
    });
    try {
      await useAppStore.persist.rehydrate();
      expect(useAppStore.getState().uniboxRailFavorites).toEqual([{ key: "unread" }]);
    } finally {
      useAppStore.persist.setOptions({ storage: original });
    }
  });
});
