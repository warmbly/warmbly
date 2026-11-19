import React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cartReducer, emptyCart, type Cart, type CartAction } from "@/lib/buy/cart";
import OrderList from "./OrderList";
import Senders from "./Senders";

function Harness({ start }: { start: Cart }) {
    const [cart, dispatch] = React.useReducer(cartReducer, start);
    const limits = { max: 5, recommended: 3 };
    return (
        <>
            <Senders cart={cart} dispatch={dispatch} limits={limits} providers={["google", "microsoft"]} photos={false} profileUrl={null} nudged={false} />
            <OrderList cart={cart} dispatch={dispatch} limits={limits} providers={["google", "microsoft"]} />
        </>
    );
}

function mount(actions: CartAction[]) {
    const cart = actions.reduce(cartReducer, emptyCart({ first: "Ada", last: "Lovelace" }));
    render(
        <QueryClientProvider client={new QueryClient()}>
            <Harness start={cart} />
        </QueryClientProvider>,
    );
}

const customizeButton = () => within(screen.getByRole("region", { name: "Senders" })).getByRole("button", { name: /^Customize$/ });
const customize = () => fireEvent.click(customizeButton());

afterEach(() => vi.mocked(localStorage.getItem).mockReset());

const chips = (domain: string) =>
    within(screen.getByRole("list", { name: `Mailboxes on ${domain}` }))
        .getAllByRole("listitem")
        .map((li) => li.textContent);

describe("senders fill every domain", () => {
    it("starts one sender at three address patterns and adapts as senders are added", async () => {
        mount([{ type: "add", domain: "getacme.com" }, { type: "add", domain: "tryacme.com" }]);
        await waitFor(() => expect(chips("getacme.com")).toEqual(["ALada@", "ALada.l@", "ALada.lovelace@"]));

        fireEvent.click(screen.getByRole("button", { name: /Add sender/ }));
        fireEvent.change(screen.getByLabelText("Sender 2 first name"), { target: { value: "Grace" } });
        fireEvent.change(screen.getByLabelText("Sender 2 last name"), { target: { value: "Hopper" } });
        await waitFor(() => expect(chips("tryacme.com")).toEqual(["ALada.lovelace@", "ALada@", "GHgrace.hopper@", "GHgrace@"]));

        fireEvent.click(screen.getByRole("button", { name: /Add sender/ }));
        fireEvent.change(screen.getByLabelText("Sender 3 first name"), { target: { value: "Alan" } });
        fireEvent.change(screen.getByLabelText("Sender 3 last name"), { target: { value: "Turing" } });
        await waitFor(() => expect(chips("tryacme.com")).toEqual(["ALada.lovelace@", "GHgrace.hopper@", "ATalan.turing@"]));
        expect(screen.getByText("3 mailboxes per domain · ada.lovelace@, grace.hopper@, alan.turing@ · Google · Signature: Default")).toBeInTheDocument();
    });

    it("shows only names and photos, one summary line, and keeps Customize closed until opened", () => {
        mount([{ type: "add", domain: "getacme.com" }]);
        expect(screen.getByText("3 mailboxes per domain · ada@, ada.l@, ada.lovelace@ · Google · Signature: Default")).toBeInTheDocument();
        expect(screen.queryByRole("textbox", { name: /Goes by|Job title|Phone/ })).not.toBeInTheDocument();
        expect(screen.queryByRole("button", { name: /^Details/ })).not.toBeInTheDocument();
        const toggle = customizeButton();
        expect(toggle).toHaveAttribute("aria-expanded", "false");
        expect(screen.queryByRole("group", { name: "Customize the mailboxes" })).not.toBeInTheDocument();
        customize();
        expect(toggle).toHaveAttribute("aria-expanded", "true");
        const panel = screen.getByRole("group", { name: "Customize the mailboxes" });
        expect(within(panel).getByRole("button", { name: /^Address patterns:/ })).toBeInTheDocument();
        expect(within(panel).getByRole("radiogroup", { name: "Mail provider for the order" })).toBeInTheDocument();
        expect(within(panel).getByRole("radiogroup", { name: "Signature" })).toBeInTheDocument();
        expect(localStorage.setItem).toHaveBeenCalledWith("warmbly.store.customize", "1");
    });

    it("opens Customize when it was left open", () => {
        vi.mocked(localStorage.getItem).mockImplementation((k) => (k === "warmbly.store.customize" ? "1" : null));
        mount([]);
        expect(screen.getByRole("group", { name: "Customize the mailboxes" })).toBeInTheDocument();
    });

    it("writes a custom signature in the mailbox editor, with sender placeholders and a preview for the first mailbox", async () => {
        mount([{ type: "add", domain: "getacme.com" }]);
        customize();
        const panel = screen.getByRole("group", { name: "Customize the mailboxes" });
        expect(within(panel).getByLabelText("Signature preview for ada@getacme.com")).toBeInTheDocument();
        fireEvent.click(within(panel).getByRole("radio", { name: "Custom" }));
        await waitFor(() => expect(document.getElementById("buy-signature")).not.toBeNull());
        expect(screen.getByText(/· Signature: Custom$/)).toBeInTheDocument();
        fireEvent.click(within(panel).getByRole("button", { name: /Insert placeholder/ }));
        const menu = await screen.findByRole("menu", { name: "Insert placeholder" });
        expect(within(menu).getAllByRole("menuitem").map((m) => m.textContent)).toEqual([
            "First name{{.Sender.FirstName}}",
            "Last name{{.Sender.LastName}}",
            "Full name{{.Sender.Name}}",
            "Email{{.Sender.Email}}",
        ]);
        fireEvent.click(within(menu).getByRole("menuitem", { name: /^Email/ }));
        await waitFor(() => expect(document.getElementById("buy-signature")?.innerHTML).toBe("{{.Sender.Name}} {{.Sender.Email}}"));
        expect(screen.queryByText(/not filled in a signature/)).not.toBeInTheDocument();

        fireEvent.click(within(panel).getByRole("radio", { name: "None" }));
        await waitFor(() => expect(within(panel).queryByLabelText(/^Signature preview/)).not.toBeInTheDocument());
        expect(screen.getByText(/· Signature: None$/)).toBeInTheDocument();
    });

    it("picks and orders address patterns in a list that closes on Escape", async () => {
        mount([{ type: "per_sender", count: 2 }, { type: "add", domain: "getacme.com" }]);
        customize();
        fireEvent.click(screen.getByRole("button", { name: /^Address patterns: ada@, ada\.l@, ada\.lovelace@/ }));
        const list = screen.getByRole("dialog", { name: "Address patterns" });
        fireEvent.click(within(list).getByRole("checkbox", { name: /^First name, like ada@/ }));
        fireEvent.click(within(list).getByRole("button", { name: "Move ada.lovelace@ earlier" }));
        await waitFor(() => expect(chips("getacme.com")).toEqual(["ALada.lovelace@", "ALada.l@"]));
        expect(within(list).getByRole("button", { name: /Use the suggested/ })).toBeInTheDocument();
        fireEvent.keyDown(document, { key: "Escape" });
        await waitFor(() => expect(screen.queryByRole("dialog", { name: "Address patterns" })).not.toBeInTheDocument());
    });

    it("keeps a hand-set count, explains when it is lowered, and never blocks adding a sender", async () => {
        mount([{ type: "add", domain: "getacme.com" }]);
        customize();
        fireEvent.click(screen.getByRole("button", { name: "One more mailbox per sender" }));
        await waitFor(() => expect(chips("getacme.com")).toHaveLength(4));
        fireEvent.click(screen.getByRole("button", { name: /Add sender/ }));
        await waitFor(() => expect(chips("getacme.com")).toHaveLength(4));
        expect(screen.getByText(/Lowered from 4 to 2 per sender/)).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: /Use the suggested 2/ }));
        await waitFor(() => expect(screen.queryByText(/Lowered from/)).not.toBeInTheDocument());
        expect(screen.getByText("Suggested for 2 senders")).toBeInTheDocument();
    });

    it("keeps a customized domain to itself", async () => {
        mount([{ type: "per_sender", count: 1 }, { type: "add", domain: "getacme.com" }, { type: "add", domain: "tryacme.com" }]);
        const card = screen.getByRole("article", { name: "getacme.com" });
        fireEvent.click(within(card).getByRole("button", { name: /Customize/ }));
        fireEvent.change(within(card).getByLabelText("Address 1 on getacme.com"), { target: { value: "founder" } });
        await waitFor(() => expect(chips("getacme.com")).toEqual(["ALfounder@"]));
        await waitFor(() => expect(chips("tryacme.com")).toEqual(["ALada@"]));
        expect(within(card).getByText("Customized")).toBeInTheDocument();

        fireEvent.click(within(card).getByRole("button", { name: /Follow the senders/ }));
        await waitFor(() => expect(chips("getacme.com")).toEqual(["ALada@"]));
    });

    it("flags a duplicate address inline", () => {
        mount([{ type: "add", domain: "getacme.com" }]);
        const card = screen.getByRole("article", { name: "getacme.com" });
        fireEvent.click(within(card).getByRole("button", { name: /Customize/ }));
        fireEvent.change(within(card).getByLabelText("Address 1 on getacme.com"), { target: { value: "sales" } });
        fireEvent.change(within(card).getByLabelText("Address 2 on getacme.com"), { target: { value: "sales" } });
        expect(within(card).getByText("This address is already in the list.")).toBeInTheDocument();
        expect(within(card).getByRole("button", { name: /One address needs a fix/ })).toBeInTheDocument();
    });
});
