import { beforeAll, describe, expect, it } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";

import type { PublicBrand } from "@/lib/api/models/app/brand/Brand";
import BrandPreview from "./BrandPreview";

const brand: PublicBrand = {
    name: "Northwind Growth",
    accent_color: "#dc2626",
    theme: { font: "serif", corners: "round", appearance: "dark", sign_in_layout: "split", sign_in_background: "accent", support_url: "https://northwind.test/help" },
    powered_by: { name: "Warmbly" },
};

// jsdom lays nothing out, so the stage is given the room a browser would report.
function withRoom(width: number, run: () => void) {
    const original = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "clientWidth");
    Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => width });
    try {
        run();
    } finally {
        if (original) Object.defineProperty(HTMLElement.prototype, "clientWidth", original);
    }
}

// jsdom has no element scrolling, which the tab strip uses to keep the active tab in view.
beforeAll(() => {
    Element.prototype.scrollBy = () => {};
});

describe("BrandPreview", () => {
    it("offers the four pages for an agency's brand and draws each", () => {
        withRoom(440, () => {
            const { container } = render(<BrandPreview brand={brand} fallbackName="Agency" />);
            const picture = () => container.querySelector("[inert]") as HTMLElement;
            expect(picture().textContent).toContain("Welcome to Northwind Growth");
            for (const [tab, text] of [
                ["Dashboard", "New campaign"],
                ["Connect page", "Connect your mailbox"],
                ["Report", "Outreach report"],
                ["Sign-in", "Welcome back"],
            ]) {
                act(() => fireEvent.click(screen.getByRole("button", { name: tab })));
                expect(picture().textContent).toContain(text);
            }
        });
    });

    it("offers only the client pages for a brand with no portal of its own", () => {
        render(<BrandPreview brand={{ name: "Acme" }} fallbackName="Acme" tabs={["connect", "report"]} />);
        expect(screen.queryByRole("button", { name: "Sign-in" })).toBeNull();
        expect(screen.queryByRole("button", { name: "Dashboard" })).toBeNull();
        expect(screen.getByRole("button", { name: "Connect page" })).toHaveAttribute("aria-pressed", "true");
    });

    it("opens in the brand's default appearance and lets the viewer flip it", () => {
        withRoom(440, () => {
            const { container } = render(<BrandPreview brand={brand} fallbackName="Agency" tab="dashboard" />);
            expect(screen.getByRole("radio", { name: "Dark" })).toHaveAttribute("aria-checked", "true");
            expect(container.querySelector("[inert] .dark, [inert] > .dark")).not.toBeNull();
            act(() => fireEvent.click(screen.getByRole("radio", { name: "Light" })));
            expect(screen.getByRole("radio", { name: "Light" })).toHaveAttribute("aria-checked", "true");
            expect(container.querySelector("[inert] .dark, [inert] > .dark")).toBeNull();
        });
    });

    it("names the brand by the fallback until it has a name", () => {
        withRoom(440, () => {
            const { container } = render(<BrandPreview brand={{ name: "" }} fallbackName="Northwind" tab="dashboard" />);
            expect(container.querySelector("[inert]")?.textContent).toContain("Northwind");
        });
    });
});
